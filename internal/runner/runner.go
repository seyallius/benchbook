// Package runner. runner - process execution behind the Runner interface, plus the
// pure command builders that turn configuration into argument vectors. Everything
// that shells out goes through here, so the CLI can be tested without running
// `go test` and without a toolchain.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ---------------------------------------------- Types ----------------------------------------- //

// errExit is the sentinel wrapped into CommandError so callers can test for "the
// command ran but failed" with errors.Is.
var errExit = errors.New("command exited with a non-zero status")

// Command is a process invocation request.
type Command struct {
	// Dir is the working directory; empty means the current one.
	Dir string
	// Name is the executable to run.
	Name string
	// Args are the arguments, already ordered.
	Args []string
	// Env holds extra environment entries in "KEY=VALUE" form, appended to the
	// inherited environment.
	Env []string
}

// String renders the command the way a user would type it, for error messages and
// the raw-output header.
func (c Command) String() string {
	parts := append([]string{c.Name}, c.Args...)
	return strings.Join(parts, " ")
}

// Result is the outcome of a command.
type Result struct {
	// Stdout is the captured standard output.
	Stdout []byte
	// Stderr is the captured standard error.
	Stderr []byte
	// ExitCode is the process exit status; 0 means success.
	ExitCode int
}

// Runner executes commands. It is the seam that lets tests replace `go test` and
// `git` with canned answers, and it is context-aware so a cancelled build cannot
// leave processes behind.
type Runner interface {
	// Run executes the command and returns its output. A non-zero exit status is
	// reported as a CommandError, not as a transport error.
	Run(ctx context.Context, command Command) (Result, error)
}

// CommandError reports a command that ran but failed.
type CommandError struct {
	// Command is the invocation that failed.
	Command Command
	// Result carries the captured output and exit code.
	Result Result
	// Err is the underlying cause, if any (for example a context deadline).
	Err error
}

// Error renders a message that includes the failing command and its stderr tail,
// which is what a user needs to fix it.
func (e *CommandError) Error() string {
	detail := strings.TrimSpace(string(e.Result.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(e.Result.Stdout))
	}
	if len(detail) > maxDetailChars {
		detail = detail[len(detail)-maxDetailChars:]
	}
	if detail == "" {
		return fmt.Sprintf("`%s` exited with status %d", e.Command.String(), e.Result.ExitCode)
	}
	return fmt.Sprintf("`%s` exited with status %d: %s", e.Command.String(), e.Result.ExitCode, detail)
}

// Unwrap exposes the sentinel and any context error for errors.Is.
func (e *CommandError) Unwrap() error { return errors.Join(errExit, e.Err) }

// maxDetailChars bounds how much command output is quoted in an error message.
const maxDetailChars = 800

// Exec is the production Runner: it starts real processes.
type Exec struct {
	// Env is appended to the inherited environment of every command.
	Env []string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// NewExec returns a Runner that executes real processes.
func NewExec() *Exec { return &Exec{} }

// ----------------------------------------- Public API ----------------------------------------- //

// Run executes the command, capturing stdout and stderr separately. A non-zero
// exit status produces a *CommandError carrying the output.
func (e *Exec) Run(ctx context.Context, command Command) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("run `%s`: %w", command.String(), err)
	}
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	process.Dir = command.Dir
	process.Env = append(os.Environ(), append(append([]string{}, e.Env...), command.Env...)...)
	process.Stdin = nil
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, &CommandError{Command: command, Result: result, Err: ctx.Err()}
	}
	return result, fmt.Errorf("run `%s`: %w", command.String(), err)
}
