// Package cli. cli - the application shell: the injectable App, the exit-code
// contract and the cobra entry point. Every subcommand implementation lives in its
// own file in this package and is pure orchestration: it wires config, runner,
// collectors, store, compare and render together, and never contains statistics,
// parsing or rendering logic itself.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seyallius/benchbook/internal/collect"
	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/runner"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Exit codes. They are part of the tool's contract, so scripts can tell a broken
// command line (2) from a refused promotion (3) without parsing English.
const (
	// ExitOK means the command did what it said.
	ExitOK = 0
	// ExitError means a runtime failure: a missing file, a failing toolchain.
	ExitError = 1
	// ExitUsage means the command line or the configuration is wrong.
	ExitUsage = 2
	// ExitRefused means a safety rule stopped the operation.
	ExitRefused = 3
)

// Version is the released version of benchbook. It is a constant rather than an
// injected build variable so that generated output stays reproducible from source.
const Version = "0.1.0"

// Command names. They are constants so the help screen, the flags and the tests can
// never disagree about the spelling of a subcommand.
const (
	// cmdBenchbook is the program name used as the root command.
	cmdBenchbook = "benchbook"
	// cmdInit writes the configuration file.
	cmdInit = "init"
	// cmdRun records the working run.
	cmdRun = "run"
	// cmdProfile captures cpu and heap profiles.
	cmdProfile = "profile"
	// cmdStatus compares the working run with the last milestone.
	cmdStatus = "status"
	// cmdMilestone promotes the working run into the history.
	cmdMilestone = "milestone"
	// cmdList lists the milestones.
	cmdList = "list"
	// cmdCompare compares two runs.
	cmdCompare = "compare"
	// cmdSummary prints a ready-to-paste snippet.
	cmdSummary = "summary"
	// cmdBuild regenerates the documentation site.
	cmdBuild = "build"
	// cmdServe serves the generated site locally.
	cmdServe = "serve"
	// cmdVersion prints the tool version.
	cmdVersion = "version"
	// cmdHelp shows the help of the program or of one command.
	cmdHelp = "help"
)

// Options configures an App. Everything that touches the outside world is
// injectable, which is what lets the integration tests drive the whole tool in a
// temporary directory without a Go toolchain.
type Options struct {
	// Stdout receives normal output.
	Stdout io.Writer
	// Stderr receives warnings and errors.
	Stderr io.Writer
	// Dir is the working directory; empty means the process working directory.
	Dir string
	// Exec runs external commands; nil means real processes.
	Exec runner.Runner
	// Environment supplies hardware facts; nil means the real machine.
	Environment collect.Environment
	// Now returns the current time; nil means time.Now. Injecting the clock is what
	// makes golden-file tests possible.
	Now func() time.Time
	// IsTerminal reports whether stdout is a TTY; when nil the app detects it.
	IsTerminal func() bool
	// LookupEnv reads environment variables; nil means os.LookupEnv.
	LookupEnv func(string) (string, bool)
}

// App is a configured benchbook invocation.
type App struct {
	stdout      io.Writer
	stderr      io.Writer
	dir         string
	exec        runner.Runner
	environment collect.Environment
	now         func() time.Time
	isTerminal  func() bool
	lookupEnv   func(string) (string, bool)
	// noColor is bound to the global --no-color flag by the command tree.
	noColor bool
	// color is resolved once per invocation from --no-color, NO_COLOR and the TTY.
	color bool
	// config caches the loaded configuration for the duration of the invocation.
	config *config.Config
}

// usageError marks an error that should exit with ExitUsage.
type usageError struct {
	// err is the user-facing cause.
	err error
}

// Error implements error.
func (u *usageError) Error() string { return u.err.Error() }

// Unwrap exposes the cause.
func (u *usageError) Unwrap() error { return u.err }

// refusalError marks an error that should exit with ExitRefused.
type refusalError struct {
	// err is the user-facing cause, including the override flag when one exists.
	err error
}

// Error implements error.
func (r *refusalError) Error() string { return r.err.Error() }

// Unwrap exposes the cause.
func (r *refusalError) Unwrap() error { return r.err }

// ---------------------------------------- Constructor(s) -------------------------------------- //

// New returns an App with the given options.
func New(options Options) *App {
	app := &App{
		stdout:      options.Stdout,
		stderr:      options.Stderr,
		dir:         options.Dir,
		exec:        options.Exec,
		environment: options.Environment,
		now:         options.Now,
		isTerminal:  options.IsTerminal,
		lookupEnv:   options.LookupEnv,
	}
	if app.stdout == nil {
		app.stdout = os.Stdout
	}
	if app.stderr == nil {
		app.stderr = os.Stderr
	}
	if app.exec == nil {
		app.exec = runner.NewExec()
	}
	if app.environment == nil {
		app.environment = collect.NewLocalEnvironment()
	}
	if app.now == nil {
		app.now = time.Now
	}
	if app.isTerminal == nil {
		app.isTerminal = func() bool { return isCharDevice(os.Stdout) }
	}
	if app.lookupEnv == nil {
		app.lookupEnv = os.LookupEnv
	}
	if app.dir == "" {
		workingDir, err := os.Getwd()
		if err == nil {
			app.dir = workingDir
		}
	}
	return app
}

// ----------------------------------------- Public API ----------------------------------------- //

// Run parses one command line, runs the matching command and returns the process
// exit code. It is the only entry point main() needs.
func (a *App) Run(ctx context.Context, args []string) int {
	root := a.newRootCommand()
	root.SetArgs(args)
	return a.report(root.ExecuteContext(ctx))
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// report turns a command's error into an exit code and prints it. The three sentinel
// wrapper types map onto the three documented non-zero codes; anything else is a
// runtime failure. A nil error means success, which is why no command has to carry an
// exit code around just to return it unchanged.
func (a *App) report(err error) int {
	if err == nil {
		return ExitOK
	}
	var usage *usageError
	if errors.As(err, &usage) {
		a.errorf("%s", usage.Error())
		return ExitUsage
	}
	var refusal *refusalError
	if errors.As(err, &refusal) {
		a.errorf("%s", refusal.Error())
		return ExitRefused
	}
	a.errorf("%s", err.Error())
	return ExitError
}

// resolveColour decides whether ANSI escapes are emitted. It runs after the flags
// are parsed, because --no-color, NO_COLOR and the terminal check all have to be
// known before the first line of output.
func (a *App) resolveColour() {
	a.color = a.colorEnabled(a.noColor)
}

// noColorFlag turns off ANSI colour explicitly.
const noColorFlag = "no-color"

// noColorEnv is the conventional environment variable honoured by many CLI tools.
const noColorEnv = "NO_COLOR"

// colorEnabled decides whether to emit ANSI escapes: only for a TTY, never when
// NO_COLOR is set, never with --no-color. Piping benchbook into a file therefore
// produces clean, diffable text.
func (a *App) colorEnabled(noColor bool) bool {
	if noColor {
		return false
	}
	if _, present := a.lookupEnv(noColorEnv); present {
		return false
	}
	return a.isTerminal()
}

// isCharDevice reports whether a file is a character device, which is how a TTY
// looks on every platform benchbook supports. It avoids a dependency on
// golang.org/x/term for a single boolean.
func isCharDevice(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// loadConfig finds and loads benchbook.toml, walking up from the working directory
// so the tool can be invoked from a subdirectory like git.
func (a *App) loadConfig() (*config.Config, error) {
	if a.config != nil {
		return a.config, nil
	}
	dir, err := findConfigDir(a.dir)
	if err != nil {
		return nil, &usageError{err: err}
	}
	loaded, err := config.Load(dir)
	if err != nil {
		return nil, &usageError{err: err}
	}
	a.config = loaded
	return loaded, nil
}

// findConfigDir walks from start upwards looking for benchbook.toml.
func findConfigDir(start string) (string, error) {
	dir := start
	for {
		candidate := filepath.Join(dir, config.FileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent directory (run `benchbook init` to create one)",
				config.FileName, start)
		}
		dir = parent
	}
}

// projectPath resolves a path relative to the configuration directory.
func (a *App) projectPath(config *config.Config, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(config.Dir, path)
}

// colorize wraps text in an ANSI escape when colour is enabled.
func (a *App) colorize(code, text string) string {
	if !a.color {
		return text
	}
	return code + text + resetCode
}

// ANSI escape sequences used for terminal output.
const (
	// resetCode clears all attributes.
	resetCode = "\x1b[0m"
	// greenCode marks improvements.
	greenCode = "\x1b[32m"
	// redCode marks regressions.
	redCode = "\x1b[31m"
	// yellowCode marks warnings.
	yellowCode = "\x1b[33m"
	// dimCode de-emphasises metadata.
	dimCode = "\x1b[2m"
)

// printf writes formatted output to stdout, trimming a single trailing newline
// from the format's result so callers control spacing.
func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.stdout, format, args...)
}

// errorf writes an error line, prefixed with a mark that survives plain ASCII
// terminals.
func (a *App) errorf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "❌ "+format+"\n", args...)
}

// warnf writes a warning line.
func (a *App) warnf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "⚠️  "+format+"\n", args...)
}

// info writes an informational line.
func (a *App) info(format string, args ...any) {
	fmt.Fprintf(a.stdout, "   "+format+"\n", args...)
}

// normalizeLabel trims a label or note coming from the command line.
func normalizeLabel(label string) string { return strings.TrimSpace(label) }
