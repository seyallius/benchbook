// Package collect. collect - provenance collection: where a run happened (git) and
// on what machine (hardware fingerprint). Both halves sit behind interfaces, so
// tests can fake a dirty tree or a different CPU without touching the real world.
package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/runner"
)

// ---------------------------------------------- Types ----------------------------------------- //

// UnknownValue is the placeholder recorded when a hardware fact cannot be read.
// It is deliberately not "": an empty string would look like a missing field,
// while "unknown" tells the reader that the probe ran and failed.
const UnknownValue = "unknown"

// Options configures a Collector.
type Options struct {
	// Exec runs the git commands; defaults to a real process runner.
	Exec runner.Runner
	// Dir is the directory git is queried in (normally the project root).
	Dir string
	// Environment supplies host facts; defaults to the real machine.
	Environment Environment
	// IncludeHostname records the hostname in Provenance.Hostname. Off by default
	// because generated documentation is usually published.
	IncludeHostname bool
	// BenchArgs is recorded verbatim so a reader knows which flags produced a run.
	BenchArgs string
	// SkipHardware records OS/arch/toolchain but leaves the CPU facts unknown. It
	// exists for `run --no-hw`: a container or a shared machine where the CPU
	// probes would be meaningless. Such a run deliberately gets a different
	// fingerprint from a probed one, because benchbook cannot prove they ran on
	// the same hardware.
	SkipHardware bool
	// IgnorePaths are directories whose changes do not make a run "dirty". They are
	// the directories benchbook itself writes (the data directory and the site),
	// because otherwise every run would immediately look dirty — and the dirty flag
	// is supposed to mean "the code you measured is not committed", not "the
	// generated documentation is newer than the commit".
	IgnorePaths []string
}

// Collector gathers provenance. It is stateless between calls, so one instance can
// serve several runs.
type Collector struct {
	exec            runner.Runner
	dir             string
	environment     Environment
	includeHostname bool
	benchArgs       string
	skipHardware    bool
	ignorePaths     []string
}

// Environment reports host facts. A machine that cannot answer a question returns
// UnknownValue or 0 rather than an error: provenance must never block a run.
type Environment interface {
	// GoVersion reports the Go toolchain version, e.g. "go1.22.5".
	GoVersion(ctx context.Context) string
	// GOOS reports the operating system.
	GOOS() string
	// GOARCH reports the architecture.
	GOARCH() string
	// CPUModel reports the CPU marketing name, or UnknownValue.
	CPUModel(ctx context.Context) string
	// CPUCores reports the number of logical CPUs.
	CPUCores() int
	// MemoryBytes reports the installed physical memory, or 0 when unknown.
	MemoryBytes(ctx context.Context) uint64
	// Hostname reports the machine name.
	Hostname() string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// New returns a Collector. Zero-valued options fall back to the real world, which
// is what the CLI wants; tests pass a runner.Fake and a fake Environment.
func New(options Options) *Collector {
	collector := &Collector{
		exec:            options.Exec,
		dir:             options.Dir,
		environment:     options.Environment,
		includeHostname: options.IncludeHostname,
		benchArgs:       options.BenchArgs,
		skipHardware:    options.SkipHardware,
		ignorePaths:     options.IgnorePaths,
	}
	if collector.exec == nil {
		collector.exec = runner.NewExec()
	}
	if collector.environment == nil {
		collector.environment = NewLocalEnvironment()
	}
	return collector
}

// ----------------------------------------- Public API ----------------------------------------- //

// Collect gathers the full provenance block. It never fails because of a missing
// git repository or an unreadable CPU model: those are recorded as unknown, and
// the safety rules of `benchbook milestone` decide what to do about it.
func (c *Collector) Collect(ctx context.Context) (model.Provenance, error) {
	provenance := model.Provenance{
		GoVersion: c.environment.GoVersion(ctx),
		GOOS:      c.environment.GOOS(),
		GOARCH:    c.environment.GOARCH(),
		CPUModel:  UnknownValue,
		BenchArgs: c.benchArgs,
	}
	if !c.skipHardware {
		provenance.CPUModel = c.environment.CPUModel(ctx)
		provenance.CPUCores = c.environment.CPUCores()
		provenance.MemoryBytes = c.environment.MemoryBytes(ctx)
	}
	if c.includeHostname {
		provenance.Hostname = c.environment.Hostname()
	}
	git, err := c.collectGit(ctx)
	if err != nil {
		return model.Provenance{}, err
	}
	provenance.GitSHA = git.GitSHA
	provenance.GitBranch = git.GitBranch
	provenance.GitDirty = git.GitDirty
	provenance.HardwareFingerprint = Fingerprint(provenance)
	return provenance, nil
}

// Fingerprint hashes the facts that make two machines comparable: OS, architecture,
// CPU model and core count. Hostname, memory size and the Go version are
// deliberately excluded — a hostname says nothing about the CPU, and the Go version
// is warned about separately because it changes code generation without making the
// numbers meaningless.
func Fingerprint(provenance model.Provenance) string {
	payload := strings.Join([]string{
		provenance.GOOS,
		provenance.GOARCH,
		provenance.CPUModel,
		fmt.Sprintf("%d", provenance.CPUCores),
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// collectGit reads the git state behind the Runner interface. A missing repository
// is not an error: it yields an empty SHA and a dirty flag, which the milestone
// rules interpret as "cannot prove this is committable".
func (c *Collector) collectGit(ctx context.Context) (model.Provenance, error) {
	sha, err := c.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return model.Provenance{}, nil
	}
	branch := "HEAD"
	if name, err := c.git(ctx, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		branch = name
	}
	provenance := model.Provenance{GitSHA: sha, GitBranch: branch}
	status, err := c.git(ctx, "status", "--porcelain")
	if err != nil {
		// Without a readable status we cannot claim the tree is clean.
		provenance.GitDirty = true
		return provenance, nil
	}
	provenance.GitDirty = c.hasRelevantChanges(ctx, status)
	return provenance, nil
}

// hasRelevantChanges reports whether the status output contains a change outside the
// directories benchbook owns.
func (c *Collector) hasRelevantChanges(ctx context.Context, status string) bool {
	ignored := c.repoRelativeIgnorePaths(ctx)
	for _, line := range strings.Split(status, "\n") {
		path, ok := statusPath(line)
		if !ok {
			continue
		}
		if isIgnored(path, ignored) {
			continue
		}
		return true
	}
	return false
}

// repoRelativeIgnorePaths converts the configured ignore paths into repository
// relative paths. Git reports porcelain paths relative to the repository root, so
// anything outside the repository is simply not relevant and is dropped.
func (c *Collector) repoRelativeIgnorePaths(ctx context.Context) []string {
	if len(c.ignorePaths) == 0 {
		return nil
	}
	toplevel, err := c.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil || toplevel == "" {
		return nil
	}
	relative := make([]string, 0, len(c.ignorePaths))
	for _, path := range c.ignorePaths {
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(c.dir, absolute)
		}
		converted, err := filepath.Rel(toplevel, absolute)
		if err != nil || strings.HasPrefix(converted, "..") {
			continue
		}
		relative = append(relative, filepath.ToSlash(converted))
	}
	return relative
}

// statusPath extracts the path of one `git status --porcelain` line. The second
// return value is false for empty lines and for malformed input.
//
// The leading status column must not be trimmed away: the first path character of an
// unstaged modification sits at a fixed offset, and stripping the leading space of
// " M path" would silently turn the path into garbage and make every run dirty.
func statusPath(line string) (string, bool) {
	trimmed := strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(trimmed) == "" || len(trimmed) <= statusFlagWidth {
		return "", false
	}
	// The first three columns are the two status letters and a space; renames carry
	// "old -> new", and the new path is the one that exists.
	path := strings.TrimSpace(trimmed[statusFlagWidth:])
	if index := strings.LastIndex(path, renameArrow); index >= 0 {
		path = strings.TrimSpace(path[index+len(renameArrow):])
	}
	path = strings.Trim(path, `"`)
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "", false
	}
	return path, true
}

// isIgnored reports whether a status path is inside one of the ignore prefixes.
func isIgnored(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// Width of the status/space prefix of a porcelain line, and the rename marker.
const (
	// statusFlagWidth is the length of the "XY " prefix in porcelain output.
	statusFlagWidth = 3
	// renameArrow separates the old and the new path of a renamed entry.
	renameArrow = " -> "
)

// git runs one git command and returns its output with the trailing newline removed.
// Only the right-hand side is trimmed: `git status --porcelain` lines start with a
// meaningful status column, so trimming the left would corrupt the first path.
func (c *Collector) git(ctx context.Context, args ...string) (string, error) {
	result, err := c.exec.Run(ctx, runner.GitCommand(c.dir, args...))
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(result.Stdout), "\r\n"), nil
}

// LocalOS reports the OS this binary was built for; exposed for tests and reports.
func LocalOS() string { return runtime.GOOS }
