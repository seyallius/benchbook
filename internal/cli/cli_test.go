// Package cli. cli_test - integration tests that drive the whole tool inside a
// temporary project directory. Every external process (git and the Go toolchain) is
// answered by runner.Fake, so the suite never runs a real benchmark, never needs a
// Go toolchain and always produces the same numbers.
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/seyallius/benchbook/internal/collect"
	"github.com/seyallius/benchbook/internal/runner"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// project is a temporary benchbook project wired to a scripted runner.
type project struct {
	// t is the owning test.
	t *testing.T
	// dir is the project root.
	dir string
	// fake answers every external command.
	fake *runner.Fake
	// now is the injected clock.
	now time.Time
	// env answers the hardware probes; tests swap it to simulate another machine.
	env collect.Environment
	// calls is the number of go test invocations already scripted.
	runs int
}

// benchmarkText is a realistic `go test -bench` capture with the requested number of
// samples per benchmark. The values differ per sample so the statistics have real
// variance to work with, and scaleFactor moves every number at once so a second
// capture can be a deliberate improvement or regression.
func benchmarkText(samples int, scaleFactor float64) string {
	names := []struct {
		name   string
		nsOp   float64
		bOp    int
		allocs int
	}{
		{"BenchmarkConcat", 66.3, 32, 1},
		{"BenchmarkParseJSON", 8400, 4160, 51},
		{"BenchmarkFindUser", 2260, 512, 3},
	}
	var builder strings.Builder
	builder.WriteString("goos: linux\ngoarch: amd64\npkg: example.com/demo\ncpu: Test CPU\n")
	for _, bench := range names {
		builder.WriteString("Benchmark" + strings.TrimPrefix(bench.name, "Benchmark") + "\n")
		builder.WriteString("\n")
		for sample := 0; sample < samples; sample++ {
			// A small alternating jitter keeps every sample distinct, which is what
			// the rank-based test needs to take its exact path.
			jitter := 1 + 0.004*float64(sample+1)
			if sample%2 == 1 {
				jitter = 1 - 0.004*float64(sample+1)
			}
			builder.WriteString(itoaF(bench.name) + "-8\t" +
				formatIterations(120+sample) + "\t" +
				formatFloat(bench.nsOp*scaleFactor*jitter) + " ns/op\t" +
				itoa(bench.bOp) + " B/op\t" +
				itoa(bench.allocs) + " allocs/op\n")
		}
	}
	return builder.String()
}

// newProject creates a temporary project with a benchbook.toml written by the tool
// itself and returns it ready to drive.
func newProject(t *testing.T, configBody string) *project {
	t.Helper()
	dir := t.TempDir()
	if configBody == "" {
		configBody = defaultConfigBody
	}
	if err := os.WriteFile(filepath.Join(dir, configFileTestName), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return &project{
		t:    t,
		dir:  dir,
		fake: runner.NewFake(),
		now:  time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		env: collect.NewStaticEnvironment(collect.StaticEnvironment{
			Version: "go1.22.5", OS: "linux", Arch: "amd64", Model: "Test CPU", Cores: 8,
		}),
	}
}

// configFileTestName is the configuration file the tool looks for.
const configFileTestName = "benchbook.toml"

// defaultConfigBody is a minimal, valid configuration: a project name and a sample
// count, everything else defaulted.
const defaultConfigBody = `[project]
name = "demo"

[bench]
count = 6
`

// -------------------------------------- Internal Helpers -------------------------------------- //

// app returns an App bound to the project, with every outside dependency injected and
// stdout pretending not to be a terminal.
func (p *project) app() (*App, *bytes.Buffer, *bytes.Buffer) {
	return p.appWithTerminal(false, func(string) (string, bool) { return "", false })
}

// appWithTerminal returns an App whose terminal detection and environment lookups are
// fixed, so colour handling can be tested without a real terminal.
func (p *project) appWithTerminal(isTerminal bool, lookupEnv func(string) (string, bool)) (*App, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := New(Options{
		Stdout:      stdout,
		Stderr:      stderr,
		Dir:         p.dir,
		Exec:        p.fake,
		Environment: p.env,
		Now:         func() time.Time { return p.now },
		IsTerminal:  func() bool { return isTerminal },
		LookupEnv:   lookupEnv,
	})
	return app, stdout, stderr
}

// runWithTerminal executes one command line with colour available.
func (p *project) runWithTerminal(args ...string) (int, string, string) {
	p.t.Helper()
	app, stdout, stderr := p.appWithTerminal(true, func(string) (string, bool) { return "", false })
	code := app.Run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

// run executes one command line and returns the exit code and the two streams.
func (p *project) run(args ...string) (int, string, string) {
	p.t.Helper()
	app, stdout, stderr := p.app()
	code := app.Run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

// mustRun executes a command that has to succeed.
func (p *project) mustRun(args ...string) string {
	p.t.Helper()
	code, stdout, stderr := p.run(args...)
	if code != ExitOK {
		p.t.Fatalf("benchbook %s: exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout + stderr
}

// scriptGit makes the fake answer like a clean repository at the given commit. An
// empty sha means "not a git repository": every git command fails.
func (p *project) scriptGit(sha, branch, status string) {
	if sha == "" {
		failure := runner.FailedCommand(runner.Command{Name: runner.GitBinary, Args: []string{"rev-parse", "HEAD"}, Dir: p.dir}, 128, "fatal: not a git repository")
		p.fake.Answer("git rev-parse", runner.FakeAnswer{Err: failure})
		p.fake.Answer("git status", runner.FakeAnswer{Err: failure})
		return
	}
	p.fake.AnswerStdout("git rev-parse HEAD", sha+"\n")
	p.fake.AnswerStdout("git rev-parse --abbrev-ref HEAD", branch+"\n")
	p.fake.AnswerStdout("git rev-parse --show-toplevel", p.dir+"\n")
	p.fake.AnswerStdout("git status --porcelain", status)
}

// scriptBench makes the next `go test -bench` invocation answer with the given
// capture. Each call replaces the answer, so a test can walk through a sequence of
// improvements and regressions.
func (p *project) scriptBench(text string) {
	p.runs++
	p.fake.Answer("go test", runner.FakeAnswer{Result: runner.Result{Stdout: []byte(text)}})
}

// recordRun scripts one capture and records it with `benchbook run --no-build`.
func (p *project) recordRun(text string, extra ...string) string {
	p.t.Helper()
	p.scriptBench(text)
	args := append([]string{"run", "--no-build"}, extra...)
	return p.mustRun(args...)
}

// readFile reads a file inside the project.
func (p *project) readFile(relative string) string {
	p.t.Helper()
	content, err := os.ReadFile(filepath.Join(p.dir, filepath.FromSlash(relative)))
	if err != nil {
		p.t.Fatalf("read %s: %v", relative, err)
	}
	return string(content)
}

// exists reports whether a project-relative path exists.
func (p *project) exists(relative string) bool {
	_, err := os.Stat(filepath.Join(p.dir, filepath.FromSlash(relative)))
	return err == nil
}

// tree reads every file under a project-relative directory into a map.
func (p *project) tree(relative string) map[string]string {
	p.t.Helper()
	root := filepath.Join(p.dir, filepath.FromSlash(relative))
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(key)] = string(content)
		return nil
	})
	if err != nil {
		p.t.Fatalf("walk %s: %v", relative, err)
	}
	return files
}

// formatIterations renders an iteration count the way go test does: plain digits.
func formatIterations(value int) string { return strconv.Itoa(value) }

// formatFloat renders a benchmark magnitude with two decimals, dropping trailing
// zeroes so the capture looks like real go test output.
func formatFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 2, 64), "0"), ".")
}

// itoa renders an integer inside the synthesised capture.
func itoa(value int) string { return strconv.Itoa(value) }

// itoaF is the benchmark name as it appears in the capture.
func itoaF(value string) string { return value }

// ----------------------------------------- Public API ----------------------------------------- //

// TestIntegrationRunMilestoneBuild walks the documented workflow end to end: init,
// run, promote a baseline, run again, promote, compare, summarise and build.
func TestIntegrationRunMilestoneBuild(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")

	if code, _, _ := project.run("init", "--name", "demo", "--force"); code != ExitOK {
		t.Fatalf("init exited with %d", code)
	}
	if !project.exists(configFileTestName) || !project.exists(".gitignore") {
		t.Fatal("init did not write benchbook.toml and .gitignore")
	}

	// First run and baseline.
	output := project.recordRun(benchmarkText(6, 1.0))
	if !strings.Contains(output, "recorded") {
		t.Errorf("run did not report a recording:\n%s", output)
	}
	if !project.exists("benchbook/current.json") {
		t.Fatal("run did not write current.json")
	}
	if !strings.Contains(project.readFile("benchbook/current.json"), `"git_sha": "1111111111111111111111111111111111111111"`) {
		t.Error("current.json does not record the commit it came from")
	}
	output = project.mustRun("milestone", "-m", "baseline")
	if !strings.Contains(output, "promoted m001") {
		t.Errorf("milestone did not promote m001:\n%s", output)
	}
	if !project.exists("benchbook/history/m001-baseline.json") {
		t.Error("the milestone file is not named m001-baseline.json")
	}

	// Second run: a 20% improvement on ns/op, promoted.
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.8))
	output = project.mustRun("milestone", "-m", "pool the buffers")
	if !strings.Contains(output, "promoted m002") {
		t.Errorf("second milestone did not promote m002:\n%s", output)
	}
	if !strings.Contains(project.readFile("benchbook/history/m002-pool-the-buffers.json"), `"label": "pool the buffers"`) {
		t.Error("the milestone does not carry its label")
	}

	// status, list, compare and summary all read the same history.
	output = project.mustRun("status")
	if !strings.Contains(output, "m002") {
		t.Errorf("status does not mention the last milestone:\n%s", output)
	}
	output = project.mustRun("list")
	if !strings.Contains(output, "m001") || !strings.Contains(output, "m002") {
		t.Errorf("list does not print both milestones:\n%s", output)
	}
	output = project.mustRun("compare", "m001", "m002")
	if !strings.Contains(output, "faster") && !strings.Contains(output, "-") {
		t.Errorf("compare printed nothing about the improvement:\n%s", output)
	}
	output = project.mustRun("summary", "--from", "m001", "--to", "m002", "--format", "text")
	if !strings.Contains(output, "geomean") {
		t.Errorf("summary has no headline:\n%s", output)
	}

	// The site is generated from the same data, and a second build is byte-identical.
	project.mustRun("build", "--quiet")
	first := project.tree("docs")
	if len(first) == 0 {
		t.Fatal("build produced no files")
	}
	for _, want := range []string{"index.html", "manifest.json", "data/index.json", "content/benchmarks.md"} {
		if _, ok := first[want]; !ok {
			t.Errorf("the built site has no %s", want)
		}
	}
	project.mustRun("build", "--quiet")
	second := project.tree("docs")
	if len(second) != len(first) {
		t.Fatalf("second build wrote %d files, want %d", len(second), len(first))
	}
	for path, content := range first {
		if second[path] != content {
			t.Errorf("%s differs between two builds of the same data", path)
		}
	}
	// Generated Markdown must announce itself so nobody edits it by hand.
	if !strings.HasPrefix(first["content/benchmarks.md"], "<!-- generated by benchbook: DO NOT EDIT -->") {
		t.Error("the generated benchmark page has no DO-NOT-EDIT header")
	}
}

// TestIntegrationPromotionIsIdempotent covers the documented promise that running
// the same promotion twice is a no-op rather than an error.
func TestIntegrationPromotionIsIdempotent(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")

	// Same content, different label: still the same milestone.
	if code, stdout, stderr := project.run("milestone", "-m", "baseline again"); code != ExitOK || !strings.Contains(stdout+stderr, "already promoted") {
		t.Fatalf("re-promotion exited with %d:\n%s%s", code, stdout, stderr)
	}
	if project.exists("benchbook/history/m002-baseline-again.json") {
		t.Error("re-promotion created a second milestone for identical content")
	}
	entries, err := os.ReadDir(filepath.Join(project.dir, "benchbook", "history"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("history has %d entries, want 1", len(entries))
	}
}

// TestIntegrationRefusesDirtyTree covers rule one: a milestone must point at a
// commit, unless the user explicitly accepts a snapshot.
func TestIntegrationRefusesDirtyTree(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", " M internal/core/concat.go\n")
	project.recordRun(benchmarkText(6, 1.0))

	code, _, stderr := project.run("milestone", "-m", "baseline")
	if code != ExitRefused {
		t.Fatalf("dirty-tree promotion exited with %d, want %d", code, ExitRefused)
	}
	if !strings.Contains(stderr, "--allow-dirty") {
		t.Errorf("the refusal does not name the override:\n%s", stderr)
	}

	output := project.mustRun("milestone", "-m", "baseline", "--allow-dirty")
	if !strings.Contains(output, "dirty working tree") {
		t.Errorf("an accepted dirty tree is not warned about:\n%s", output)
	}
	if !strings.Contains(project.readFile("benchbook/history/m001-baseline.json"), `"git_dirty": true`) {
		t.Error("the milestone does not record that the tree was dirty")
	}
}

// TestIntegrationRefusesWithoutGit covers the second promotion rule and its escape
// hatch.
func TestIntegrationRefusesWithoutGit(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("", "", "")
	project.recordRun(benchmarkText(6, 1.0))

	code, _, stderr := project.run("milestone", "-m", "baseline")
	if code != ExitRefused {
		t.Fatalf("promotion without git exited with %d, want %d", code, ExitRefused)
	}
	if !strings.Contains(stderr, "git could not identify the current commit") {
		t.Errorf("the refusal does not explain the cause:\n%s", stderr)
	}
	project.mustRun("milestone", "-m", "baseline", "--allow-dirty")
	if !project.exists("benchbook/history/m001-baseline.json") {
		t.Error("--allow-dirty did not promote the run")
	}
}

// TestIntegrationRefusesSparseSamples covers the sample-count rules: a quick run and
// an under-sampled benchmark both need --force, and a forced promotion is marked.
func TestIntegrationRefusesSparseSamples(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")

	// --quick records a single sample per benchmark: indicative only.
	project.recordRun(benchmarkText(1, 1.0), "--quick")
	code, _, stderr := project.run("milestone", "-m", "baseline")
	if code != ExitRefused {
		t.Fatalf("indicative promotion exited with %d, want %d", code, ExitRefused)
	}
	if !strings.Contains(stderr, "indicative only") || !strings.Contains(stderr, "--force") {
		t.Errorf("the indicative refusal is not actionable:\n%s", stderr)
	}
	output := project.mustRun("milestone", "-m", "baseline", "--force")
	if !strings.Contains(output, "overridden") {
		t.Errorf("a forced promotion is not warned about:\n%s", output)
	}
	if !strings.Contains(project.readFile("benchbook/history/m001-baseline.json"), `"forced": true`) {
		t.Error("the milestone does not record that the rules were overridden")
	}

	// A full run whose benchmarks are below stats.min_samples is refused too.
	project2 := newProject(t, "")
	project2.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project2.recordRun(benchmarkText(3, 1.0))
	code, _, stderr = project2.run("milestone", "-m", "baseline")
	if code != ExitRefused {
		t.Fatalf("sparse promotion exited with %d, want %d", code, ExitRefused)
	}
	if !strings.Contains(stderr, "stats.min_samples") {
		t.Errorf("the sparse refusal does not explain the threshold:\n%s", stderr)
	}
}

// TestIntegrationMarksHardwareChange covers the comparability rule: a second
// milestone recorded on a different machine is stored, warned about, and excluded
// from percentages by default.
func TestIntegrationMarksHardwareChange(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")

	// The same project, a new commit, and a different CPU.
	project = project.withEnvironment(collect.StaticEnvironment{Version: "go1.22.5", OS: "darwin", Arch: "arm64", Model: "Apple M2 Pro", Cores: 12})
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.scriptBench(benchmarkText(6, 0.9))
	output := project.mustRun("run")
	if !strings.Contains(output, "not directly comparable") {
		t.Errorf("a hardware change was not reported:\n%s", output)
	}
	output = project.mustRun("milestone", "-m", "new laptop")
	if !strings.Contains(output, "hardware fingerprint differs") {
		t.Errorf("the promotion does not warn about the new machine:\n%s", output)
	}

	code, stdout, stderr := project.run("compare", "m001", "m002")
	if code != ExitOK {
		t.Fatalf("compare exited with %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "not directly comparable") {
		t.Errorf("compare does not flag the hardware change:\n%s%s", stdout, stderr)
	}
	if strings.Contains(stdout, "%") && !strings.Contains(stdout, "--force-incomparable") {
		t.Errorf("compare printed percentages across machines without offering the override:\n%s", stdout)
	}
}

// TestIntegrationCommandLineContract covers the small promises the CLI makes: a
// version, a usage message, the three exit codes, and no ANSI colour when stdout is
// not a terminal.
func TestIntegrationCommandLineContract(t *testing.T) {
	project := newProject(t, "")

	output := project.mustRun("version")
	if !strings.Contains(output, Version) {
		t.Errorf("version does not contain %q:\n%s", Version, output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Errorf("version emitted ANSI colour without a TTY:\n%q", output)
	}
	code, stdout, _ := project.run()
	if code != ExitUsage {
		t.Errorf("a bare invocation exited with %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stdout, "usage") && !strings.Contains(stdout, "benchbook") {
		t.Errorf("a bare invocation printed no usage:\n%s", stdout)
	}
	code, _, stderr := project.run("nonsense")
	if code != ExitUsage || !strings.Contains(stderr, "unknown command") {
		t.Errorf("an unknown command exited with %d:\n%s", code, stderr)
	}
	code, _, stderr = project.run("milestone", "-m", "x")
	if code != ExitUsage || !strings.Contains(stderr, "benchbook run") {
		t.Errorf("promoting without a run exited with %d:\n%s", code, stderr)
	}
	code, _, stderr = project.run("status")
	if code != ExitUsage || !strings.Contains(stderr, "benchbook run") {
		t.Errorf("status without a run exited with %d:\n%s", code, stderr)
	}
}

// TestIntegrationSummaryFormats covers the three summary encodings: they must agree
// on the numbers and differ only in packaging.
func TestIntegrationSummaryFormats(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.75))

	text := project.mustRun("summary", "--from", "m001", "--to", "current", "--format", "text")
	if !strings.Contains(text, "geomean") || !strings.Contains(text, "ns_op") {
		t.Errorf("text summary is not a one-liner with a geomean:\n%s", text)
	}
	markdown := project.mustRun("summary", "--from", "m001", "--to", "current", "--format", "md")
	if !strings.HasPrefix(markdown, "<!-- generated by benchbook: DO NOT EDIT -->") {
		t.Errorf("markdown summary has no generated header:\n%s", markdown)
	}
	if !strings.Contains(markdown, "|") {
		t.Errorf("markdown summary has no table:\n%s", markdown)
	}
	jsonOutput := project.mustRun("summary", "--from", "m001", "--to", "current", "--format", "json")
	if !strings.HasPrefix(strings.TrimSpace(jsonOutput), "{") {
		t.Errorf("json summary is not an object:\n%s", jsonOutput)
	}
	for _, want := range []string{`"from"`, `"to"`, `"results"`, `"geomeans"`} {
		if !strings.Contains(jsonOutput, want) {
			t.Errorf("json summary has no %s field:\n%s", want, jsonOutput)
		}
	}
}

// TestIntegrationFromFileIsTraceable covers the ingest path used by CI and by
// anyone who runs the benchmarks themselves: the raw text is kept, and the recorded
// provenance says where it came from.
func TestIntegrationFromFileIsTraceable(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	capture := benchmarkText(6, 1.0)
	if err := os.WriteFile(filepath.Join(project.dir, "bench.txt"), []byte(capture), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	project.mustRun("run", "--from-file", "bench.txt")

	if !project.exists("benchbook/raw/last-run.txt") {
		t.Fatal("the ingested capture was not kept under benchbook/raw/")
	}
	if project.readFile("benchbook/raw/last-run.txt") != capture {
		t.Error("the kept capture is not byte-identical to the file that was ingested")
	}
	if !strings.Contains(project.readFile("benchbook/current.json"), "ingested from bench.txt") {
		t.Error("provenance does not say the run was ingested")
	}
	if project.fake.RanPrefix("go test") {
		t.Error("--from-file still ran the toolchain")
	}
}

// TestIntegrationSkipsHardwareOnDemand covers --no-hw: the run is still recorded, but
// it is explicitly marked as having unknown hardware.
func TestIntegrationSkipsHardwareOnDemand(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0), "--no-hw")
	current := project.readFile("benchbook/current.json")
	if !strings.Contains(current, `"cpu_model": "unknown"`) {
		t.Errorf("--no-hw did not record unknown hardware:\n%s", current)
	}
}

// TestIntegrationNoColourEnvironmentVariable covers the NO_COLOR convention: colour
// is off when the variable is present, even on a terminal.
func TestIntegrationNoColourEnvironmentVariable(t *testing.T) {
	project := newProject(t, "")
	stdout := &bytes.Buffer{}
	app := New(Options{
		Stdout:      stdout,
		Stderr:      &bytes.Buffer{},
		Dir:         project.dir,
		Exec:        project.fake,
		Environment: collect.NewStaticEnvironment(collect.StaticEnvironment{OS: "linux", Arch: "amd64", Model: "Test CPU", Cores: 8}),
		Now:         func() time.Time { return project.now },
		IsTerminal:  func() bool { return true },
		LookupEnv:   func(key string) (string, bool) { return "", key == noColorEnv },
	})
	if code := app.Run(context.Background(), []string{"version"}); code != ExitOK {
		t.Fatalf("version exited with %d", code)
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Errorf("NO_COLOR did not disable colour:\n%q", stdout.String())
	}
}

// TestIntegrationDoubleBuildIsByteIdentical is the determinism promise at the level
// users experience it: two builds of the same history produce the same bytes,
// including the timeline SVG.
func TestIntegrationDoubleBuildIsByteIdentical(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.85))
	project.mustRun("milestone", "-m", "faster")
	// A working run that has not been promoted: it must appear in the timeline as a
	// provisional point without being written into history.
	project.scriptGit("3333333333333333333333333333333333333333", "main", "")
	project.recordRun(benchmarkText(6, 0.8))

	project.mustRun("build", "--quiet")
	first := project.tree("docs")
	timeline := first["content/timeline.md"]
	if !strings.Contains(timeline, "<svg") {
		t.Error("the timeline page has no inline chart")
	}
	if !strings.Contains(timeline, "bb-chart-current") {
		t.Error("the unpromoted working run is not drawn as a provisional segment")
	}
	project.mustRun("build", "--quiet")
	second := project.tree("docs")
	for path, content := range first {
		if second[path] != content {
			t.Errorf("%s is not byte-identical between two builds", path)
		}
	}

	// Determinism also has to survive a fresh site directory: no leftovers from the
	// previous build may leak into the new one.
	if err := os.RemoveAll(filepath.Join(project.dir, "docs")); err != nil {
		t.Fatalf("remove docs: %v", err)
	}
	project.mustRun("build", "--quiet")
	third := project.tree("docs")
	for path, content := range first {
		if third[path] != content {
			t.Errorf("%s differs after a clean rebuild", path)
		}
	}
}

// TestIntegrationHistoryIsImmutable covers the store's promise: a milestone is
// written once and never rewritten, and the working run stays a separate file.
func TestIntegrationHistoryIsImmutable(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")
	before := project.readFile("benchbook/history/m001-baseline.json")

	// A new working run must not touch the milestone it was compared with.
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.9))
	if after := project.readFile("benchbook/history/m001-baseline.json"); after != before {
		t.Error("recording a new run rewrote an existing milestone")
	}
	// The working file is a different document, and it is the only one that changes.
	if project.readFile("benchbook/current.json") == before {
		t.Error("current.json and the milestone are the same document")
	}
}

// TestIntegrationHelpIsGeneratedFromTheCommandTree covers the promise that the help
// screen and the parser cannot drift apart: both come from the same command tree.
func TestIntegrationHelpIsGeneratedFromTheCommandTree(t *testing.T) {
	project := newProject(t, "")

	code, root, _ := project.run("--help")
	if code != ExitOK {
		t.Fatalf("--help exited with %d", code)
	}
	commands := []string{cmdInit, cmdRun, cmdProfile, cmdStatus, cmdMilestone, cmdList, cmdCompare, cmdSummary, cmdBuild, cmdServe, cmdVersion, cmdHelp}
	for _, name := range commands {
		if !strings.Contains(root, "\n  "+name) {
			t.Errorf("help does not list the %s command:\n%s", name, root)
		}
	}
	for _, group := range []string{groupSetUpTitle, groupMeasureTitle, groupHistoryTitle, groupPublishTitle} {
		if !strings.Contains(root, group) {
			t.Errorf("help is missing the %q group", group)
		}
	}
	if !strings.Contains(root, "--no-color") {
		t.Error("help does not document the global --no-color flag")
	}
	if !strings.Contains(root, "milestone is a commit") {
		t.Error("help does not explain the run-versus-milestone model")
	}

	// `benchbook help <command>` and `<command> --help` must be the same screen.
	for _, pair := range [][2][]string{
		{{"help", cmdRun}, {cmdRun, "--help"}},
		{{"help", cmdMilestone}, {cmdMilestone, "--help"}},
	} {
		viaHelp := project.mustRun(pair[0]...)
		viaFlag := project.mustRun(pair[1]...)
		if viaHelp != viaFlag {
			t.Errorf("`benchbook %s` and `benchbook %s` disagree",
				strings.Join(pair[0], " "), strings.Join(pair[1], " "))
		}
		if !strings.Contains(viaHelp, "Usage:") {
			t.Errorf("`benchbook %s` has no usage line", strings.Join(pair[0], " "))
		}
	}

	// Every command must be documentable, and every flag a command accepts must be
	// in its help: a flag nobody can discover is a flag nobody will use.
	for _, name := range commands {
		usage := project.mustRun("help", name)
		if !strings.Contains(usage, "Usage:") || !strings.Contains(usage, "benchbook "+name) {
			t.Errorf("`benchbook help %s` is not a help screen:\n%s", name, usage)
		}
		if strings.Contains(usage, "Additional Commands") {
			t.Errorf("`benchbook help %s` leaked cobra's default command list", name)
		}
	}
	runHelp := project.mustRun("help", cmdRun)
	for _, flag := range []string{"--count", "--filter", "--quick", "--from-file", "--no-hw", "--no-build"} {
		if !strings.Contains(runHelp, flag) {
			t.Errorf("`benchbook run --help` does not document %s", flag)
		}
	}
	if strings.Contains(runHelp, "--from-file go test") {
		t.Error("the --from-file help leaked a backtick-quoted placeholder")
	}
}

// TestIntegrationUsageErrorsExitTwo covers the exit-code contract at the command
// line: a mistake a human can fix is always 2, never 1, and it names the fix.
func TestIntegrationUsageErrorsExitTwo(t *testing.T) {
	project := newProject(t, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"lst"}, "did you mean \"list\"?"},
		{"unknown command without a suggestion", []string{"completion"}, "unknown command"},
		{"unknown flag", []string{"status", "--nope"}, "unknown flag: --nope"},
		{"missing positional", []string{"compare", "m001"}, "needs two runs to compare"},
		{"too many positionals", []string{"run", "extra"}, "takes no arguments"},
		{"unknown help topic", []string{"help", "nonsense"}, "unknown command"},
	}
	for _, testCase := range cases {
		code, stdout, stderr := project.run(testCase.args...)
		if code != ExitUsage {
			t.Errorf("%s: exit %d, want %d", testCase.name, code, ExitUsage)
		}
		if !strings.Contains(stderr, testCase.want) {
			t.Errorf("%s: stderr does not contain %q:\n%s", testCase.name, testCase.want, stderr)
		}
		if stdout != "" {
			t.Errorf("%s: wrote to stdout on a usage error:\n%s", testCase.name, stdout)
		}
	}

	// Help is never a usage error, even when there is no configuration to load.
	empty := t.TempDir()
	app := New(Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Dir: empty, Exec: runner.NewFake()})
	if code := app.Run(context.Background(), []string{"run", "--help"}); code != ExitOK {
		t.Errorf("run --help outside a project exited with %d, want %d", code, ExitOK)
	}
	if code := app.Run(context.Background(), []string{"--help"}); code != ExitOK {
		t.Errorf("--help outside a project exited with %d, want %d", code, ExitOK)
	}
	// Actually running a command outside a project is a usage error: the message has
	// to name the missing file and the command that creates it.
	code, _, stderr := projectRunNoConfig(t, empty, "status")
	if code != ExitUsage {
		t.Errorf("status outside a project exited with %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "benchbook.toml") || !strings.Contains(stderr, "benchbook init") {
		t.Errorf("the missing-config message is not actionable:\n%s", stderr)
	}
}

// TestIntegrationShorthandsAndFlagPlacement covers the conveniences a cobra command
// line brings: the -m shorthand, flags before or after positional arguments, and
// --flag=value.
func TestIntegrationShorthandsAndFlagPlacement(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))

	output := project.mustRun("milestone", "-m", "shorthand")
	if !strings.Contains(output, "promoted m001") {
		t.Fatalf("the -m shorthand did not promote a milestone:\n%s", output)
	}
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.9))
	output = project.mustRun("milestone", "--message", "long form")
	if !strings.Contains(output, "promoted m002") {
		t.Fatalf("the --message long form did not promote a milestone:\n%s", output)
	}

	// A positional before a flag, a flag before a positional, and --flag=value are
	// all the same command line.
	for _, args := range [][]string{
		{"compare", "m001", "m002", "--metric", "ns_op"},
		{"compare", "--metric", "ns_op", "m001", "m002"},
		{"compare", "--metric=ns_op", "m001", "m002"},
	} {
		together := project.mustRun(args...)
		if !strings.Contains(together, "comparing m001") {
			t.Errorf("`benchbook %s` did not compare the two runs:\n%s", strings.Join(args, " "), together)
		}
	}
}

// TestIntegrationNoColourFlag covers --no-color in both positions, and confirms that
// a terminal alone is enough to get colour.
func TestIntegrationNoColourFlag(t *testing.T) {
	project := newProject(t, "")
	project.scriptGit("1111111111111111111111111111111111111111", "main", "")
	project.recordRun(benchmarkText(6, 1.0))
	project.mustRun("milestone", "-m", "baseline")
	project.scriptGit("2222222222222222222222222222222222222222", "main", "")
	project.recordRun(benchmarkText(6, 0.8))

	code, coloured, _ := project.runWithTerminal("status")
	if code != ExitOK {
		t.Fatalf("status exited with %d", code)
	}
	if !strings.Contains(coloured, "\x1b[") {
		t.Fatalf("status did not colour its output on a terminal:\n%s", coloured)
	}
	// The flag works before the command and after it, because it is persistent.
	for _, args := range [][]string{
		{"--no-color", "status"},
		{"status", "--no-color"},
	} {
		code, plain, _ := project.runWithTerminal(args...)
		if code != ExitOK {
			t.Fatalf("`benchbook %s` exited with %d", strings.Join(args, " "), code)
		}
		if strings.Contains(plain, "\x1b[") {
			t.Errorf("`benchbook %s` emitted ANSI escapes:\n%q", strings.Join(args, " "), plain)
		}
		if strings.TrimSpace(plain) == "" {
			t.Errorf("`benchbook %s` printed nothing", strings.Join(args, " "))
		}
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// projectRunNoConfig runs one command in a directory without a benchbook.toml, which
// is the first thing a new user does by accident.
func projectRunNoConfig(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := New(Options{Stdout: stdout, Stderr: stderr, Dir: dir, Exec: runner.NewFake()})
	code := app.Run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

// withEnvironment returns a copy of the project that reports different hardware,
// which is how the tests simulate a second machine.
func (p *project) withEnvironment(environment collect.StaticEnvironment) *project {
	copy := *p
	copy.fake = p.fake
	copy.env = collect.NewStaticEnvironment(environment)
	return &copy
}

// ensureStore is a compile-time guard that the store package's file names still
// exist; a rename should fail here rather than deep inside a test.
var _ = store.CurrentFile
