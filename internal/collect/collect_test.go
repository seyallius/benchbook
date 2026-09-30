// Package collect. collect_test - tests for provenance collection: the git state,
// the dirty-tree rule that ignores benchbook's own output, the hardware fingerprint
// and the --no-hw fallback. Everything runs against a scripted Runner.
package collect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/runner"
)

// ---------------------------------------------- Types ----------------------------------------- //

// statusFixture is realistic `git status --porcelain` output with a mixture of
// modified, deleted, renamed and untracked entries. The leading space of an unstaged
// modification is part of the record, not padding.
const statusFixture = ` M benchbook/current.json
 M docs/content/benchmarks.md
?? docs/content/milestones/m003.md
 M internal/core/core.go
 D old/removed.go
R  old/name.go -> new/name.go
 M benchbook/raw/last-run.txt
`

// generatedOnlyFixture is the same output with only benchbook's own files touched.
const generatedOnlyFixture = ` M benchbook/current.json
 M benchbook/raw/last-run.txt
 M docs/content/benchmarks.md
?? docs/content/milestones/m003.md
`

// ----------------------------------------- Public API ----------------------------------------- //

// TestCollectReadsGitState covers the happy path: sha, branch and a clean tree.
func TestCollectReadsGitState(t *testing.T) {
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "0123456789abcdef\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n").
		AnswerStdout("git status --porcelain", "")

	collector := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment()})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if provenance.GitSHA != "0123456789abcdef" {
		t.Errorf("sha = %q, want 0123456789abcdef", provenance.GitSHA)
	}
	if provenance.GitBranch != "main" {
		t.Errorf("branch = %q, want main", provenance.GitBranch)
	}
	if provenance.GitDirty {
		t.Error("dirty = true for a clean tree")
	}
	if provenance.GoVersion != "go1.22.5" || provenance.CPUModel != "Test CPU" || provenance.CPUCores != 8 {
		t.Errorf("environment not copied: %+v", provenance)
	}
	if provenance.Hostname != "" {
		t.Errorf("hostname = %q, want empty unless explicitly requested", provenance.Hostname)
	}
	if provenance.HardwareFingerprint == "" {
		t.Error("no hardware fingerprint")
	}
}

// TestCollectIncludesHostnameOnlyWhenAsked keeps hostnames out of the published
// history by default.
func TestCollectIncludesHostnameOnlyWhenAsked(t *testing.T) {
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "abc123\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n").
		AnswerStdout("git status --porcelain", "")

	collector := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment(), IncludeHostname: true})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if provenance.Hostname != "test-host" {
		t.Errorf("hostname = %q, want test-host", provenance.Hostname)
	}
}

// TestDirtyIgnoresBenchbooksOwnOutput is the rule that makes the tool usable: a run
// writes current.json and the site, and those must not mark the measured code dirty.
func TestDirtyIgnoresBenchbooksOwnOutput(t *testing.T) {
	provenance := collectWithStatus(t, statusFixture, []string{"/repo/benchbook", "/repo/docs"})
	if !provenance.GitDirty {
		t.Error("dirty = false although internal/core/core.go is modified")
	}
}

// TestCleanWhenOnlyGeneratedFilesChanged is the other half of that rule: with only
// current.json, the site and the raw output touched, the tree counts as clean.
func TestCleanWhenOnlyGeneratedFilesChanged(t *testing.T) {
	provenance := collectWithStatus(t, generatedOnlyFixture, []string{"/repo/benchbook", "/repo/docs"})
	if provenance.GitDirty {
		t.Error("dirty = true although only benchbook's own output changed")
	}
}

// TestIgnorePathsAreRelativeToTheRepositoryRoot covers running benchbook from a
// subdirectory: the ignore prefixes are converted with `git rev-parse --show-toplevel`
// so they are compared in the same coordinate system as the status paths.
func TestIgnorePathsAreRelativeToTheRepositoryRoot(t *testing.T) {
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "abc123\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n").
		AnswerStdout("git rev-parse --show-toplevel", "/repo\n").
		AnswerStdout("git status --porcelain", " M benchbook/current.json\n M tooling/bench/helper.go\n")

	collector := New(Options{
		Exec:        fake,
		Dir:         "/repo/tooling/bench",
		Environment: staticEnvironment(),
		IgnorePaths: []string{"/repo/benchbook", "/repo/docs"},
	})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !provenance.GitDirty {
		t.Error("dirty = false although tooling/bench/helper.go is modified")
	}
}

// TestUnreadableStatusCountsAsDirty is the pessimistic default: if git cannot tell us
// the tree is clean, the run must not claim it is.
func TestUnreadableStatusCountsAsDirty(t *testing.T) {
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "abc123\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n")
	fake.Answer("git status --porcelain", runner.FakeAnswer{
		Err: runner.FailedCommand(runner.Command{Name: runner.GitBinary, Args: []string{"status"}, Dir: "/repo"}, 128, "fatal"),
	})
	collector := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment()})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !provenance.GitDirty {
		t.Error("dirty = false although git status failed")
	}
}

// TestStatusPathParsingKeepsTheLeadingColumn is a regression test for a real bug: an
// earlier implementation trimmed the whole status line, which turned
// " M docs/x.md" into "cs/x.md" and made every run look dirty.
func TestStatusPathParsingKeepsTheLeadingColumn(t *testing.T) {
	cases := map[string]string{
		" M docs/content/benchmarks.md": "docs/content/benchmarks.md",
		"M  internal/core/core.go":      "internal/core/core.go",
		"?? docs/new.md":                "docs/new.md",
		"D  old/gone.go":                "old/gone.go",
		"R  old/name.go -> new/name.go": "new/name.go",
		"RM src/a.go -> src/b.go":       "src/b.go",
		" M \"quoted name.md\"":         "quoted name.md",
	}
	for line, want := range cases {
		got, ok := statusPath(line)
		if !ok {
			t.Errorf("statusPath(%q) failed to parse", line)
			continue
		}
		if got != want {
			t.Errorf("statusPath(%q) = %q, want %q", line, got, want)
		}
	}
	for _, line := range []string{"", "   ", "??", "M"} {
		if _, ok := statusPath(line); ok {
			t.Errorf("statusPath(%q) parsed, want failure", line)
		}
	}
}

// TestMissingRepositoryIsNotAnError keeps `benchbook run` working in a directory that
// is not a git repository: the provenance simply says so.
func TestMissingRepositoryIsNotAnError(t *testing.T) {
	fake := runner.NewFake()
	fake.Answer("git rev-parse HEAD", runner.FakeAnswer{
		Err: runner.FailedCommand(runner.Command{Name: runner.GitBinary, Args: []string{"rev-parse", "HEAD"}, Dir: "/not-a-repo"}, 128,
			"fatal: not a git repository (or any of the parent directories): .git"),
	})
	collector := New(Options{Exec: fake, Dir: "/not-a-repo", Environment: staticEnvironment()})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if provenance.GitSHA != "" || provenance.GitBranch != "" {
		t.Errorf("sha/branch = %q/%q, want empty", provenance.GitSHA, provenance.GitBranch)
	}
	if provenance.HardwareFingerprint == "" {
		t.Error("no hardware fingerprint")
	}
}

// TestSkipHardwareRecordsUnknowns covers --no-hw: the run still records OS, arch and
// the Go version, but no CPU details, and its fingerprint differs from a probed run.
func TestSkipHardwareRecordsUnknowns(t *testing.T) {
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "abc123\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n").
		AnswerStdout("git status --porcelain", "")

	probed, err := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment()}).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	skipped, err := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment(), SkipHardware: true}).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if skipped.CPUModel != UnknownValue || skipped.CPUCores != 0 || skipped.MemoryBytes != 0 {
		t.Errorf("--no-hw provenance = %+v, want an unknown CPU and zero counts", skipped)
	}
	if skipped.GoVersion != "go1.22.5" || skipped.GOOS != "linux" || skipped.GOARCH != "amd64" {
		t.Errorf("--no-hw dropped the toolchain facts: %+v", skipped)
	}
	if skipped.HardwareFingerprint == probed.HardwareFingerprint {
		t.Error("a --no-hw run has the same fingerprint as a probed run, which would make them comparable")
	}
}

// TestFingerprintCoversTheFactsThatMakeRunsComparable pins what is in and what is out
// of the fingerprint.
func TestFingerprintCoversTheFactsThatMakeRunsComparable(t *testing.T) {
	reference := model.Provenance{GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8}
	included := map[string]model.Provenance{
		"os":        {GOOS: "darwin", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8},
		"arch":      {GOOS: "linux", GOARCH: "arm64", CPUModel: "Test CPU", CPUCores: 8},
		"cpu model": {GOOS: "linux", GOARCH: "amd64", CPUModel: "Other CPU", CPUCores: 8},
		"cpu cores": {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 4},
	}
	if Fingerprint(reference) == "" {
		t.Fatal("Fingerprint returned an empty string")
	}
	for name, provenance := range included {
		if Fingerprint(provenance) == Fingerprint(reference) {
			t.Errorf("%s changed but the fingerprint did not", name)
		}
	}

	// Excluded on purpose: the fingerprint answers "same machine?", nothing else.
	excluded := map[string]model.Provenance{
		"hostname":   {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, Hostname: "laptop"},
		"memory":     {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, MemoryBytes: 1 << 34},
		"go version": {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, GoVersion: "go1.23"},
		"git sha":    {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, GitSHA: "deadbeef"},
		"git dirty":  {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, GitDirty: true},
		"bench args": {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, BenchArgs: "-count=1"},
		"git branch": {GOOS: "linux", GOARCH: "amd64", CPUModel: "Test CPU", CPUCores: 8, GitBranch: "feature"},
	}
	for name, provenance := range excluded {
		if Fingerprint(provenance) != Fingerprint(reference) {
			t.Errorf("%s is part of the fingerprint, want it excluded", name)
		}
	}
}

// TestGitOutputIsTrimmedOnTheRightOnly guards the second half of the leading-column
// bug: the command output loses its trailing newline and nothing else.
func TestGitOutputIsTrimmedOnTheRightOnly(t *testing.T) {
	fake := runner.NewFake().AnswerStdout("git status --porcelain", statusFixture)
	collector := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment()})
	output, err := collector.git(context.Background(), "status", "--porcelain")
	if err != nil {
		t.Fatalf("git: %v", err)
	}
	if strings.HasSuffix(output, "\n") {
		t.Error("git output still ends with a newline")
	}
	if !strings.HasPrefix(output, " M benchbook/current.json") {
		t.Errorf("git output lost its leading status column: %q", output)
	}
}

// TestStaticEnvironmentIsUsedByTheTests keeps the test double honest.
func TestStaticEnvironmentIsUsedByTheTests(t *testing.T) {
	environment := staticEnvironment()
	if environment.GOOS() != "linux" || environment.GOARCH() != "amd64" {
		t.Errorf("static environment = %s/%s", environment.GOOS(), environment.GOARCH())
	}
	if environment.CPUCores() != 8 {
		t.Errorf("cores = %d, want 8", environment.CPUCores())
	}
	if environment.CPUModel(context.Background()) != "Test CPU" {
		t.Errorf("cpu model = %q, want Test CPU", environment.CPUModel(context.Background()))
	}
	if environment.GoVersion(context.Background()) != "go1.22.5" {
		t.Errorf("go version = %q, want go1.22.5", environment.GoVersion(context.Background()))
	}
}

// TestLocalOSReturnsTheRealPlatform keeps the "other OS" fallback honest: it reports
// the platform the binary was built for.
func TestLocalOSReturnsTheRealPlatform(t *testing.T) {
	if LocalOS() == "" {
		t.Error("LocalOS returned an empty string")
	}
}

// TestStatusPathAgainstRealGitOutput compares the parser with the real tool when a
// git binary is available, so the fixture cannot drift away from reality.
func TestStatusPathAgainstRealGitOutput(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		if output, err := exec.Command(gitPath, append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v (%s)", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	status, err := exec.Command(gitPath, "-C", dir, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(status), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("git status returned nothing: %q", status)
	}
	path, ok := statusPath(lines[0])
	if !ok {
		t.Fatalf("statusPath failed on real output %q", lines[0])
	}
	if path != "tracked.txt" {
		t.Errorf("statusPath = %q, want tracked.txt (real output was %q)", path, lines[0])
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// collectWithStatus runs Collect against a scripted git whose status output is the
// given fixture.
func collectWithStatus(t *testing.T, status string, ignorePaths []string) model.Provenance {
	t.Helper()
	fake := runner.NewFake().
		AnswerStdout("git rev-parse HEAD", "abc123\n").
		AnswerStdout("git rev-parse --abbrev-ref HEAD", "main\n").
		AnswerStdout("git rev-parse --show-toplevel", "/repo\n").
		AnswerStdout("git status --porcelain", status)

	collector := New(Options{Exec: fake, Dir: "/repo", Environment: staticEnvironment(), IgnorePaths: ignorePaths})
	provenance, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return provenance
}

// staticEnvironment is a fixed Environment, so provenance tests never read the real
// machine.
func staticEnvironment() *StaticEnvironment {
	return NewStaticEnvironment(StaticEnvironment{
		Version: "go1.22.5",
		OS:      "linux",
		Arch:    "amd64",
		Model:   "Test CPU",
		Cores:   8,
		Memory:  16 << 30,
		Host:    "test-host",
	})
}
