// Package runner. commands_test - tests for the pure command builders: the exact
// argument vectors handed to `go test`, `go tool pprof` and `git`, including the
// guarantee that profiling never leaves a compiled test binary in the repository.
package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------- Types ----------------------------------------- //

// benchFixture returns a benchmark spec that exercises every optional field.
func benchFixture() BenchSpec {
	return BenchSpec{
		Dir:      filepath.Join("out", "demo"),
		Packages: []string{"./..."},
		Args:     []string{"-benchmem"},
		Count:    5,
		Timeout:  3 * time.Minute,
		Bench:    "Concat",
		GoFlags:  []string{"-tags=integration"},
	}
}

// profileFixture returns the profile spec that mirrors what the CLI builds.
func profileFixture(t *testing.T) ProfileSpec {
	t.Helper()
	return ProfileSpec{
		BenchSpec: benchFixture(),
		Package:   "./internal/core",
		CPUOut:    filepath.Join("out", "raw", "cpu.prof"),
		MemOut:    filepath.Join("out", "raw", "mem.prof"),
	}
}

// argsAfter returns the token following the first occurrence of flag, or "".
func argsAfter(args []string, flag string) string {
	index := slices.Index(args, flag)
	if index < 0 || index+1 >= len(args) {
		return ""
	}
	return args[index+1]
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// contains reports whether args holds the exact token.
func contains(args []string, token string) bool {
	return slices.Contains(args, token)
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestBenchCommandPinsEveryFlag checks that a benchmark run is reproducible: tests are
// disabled, -benchtime is fixed, and the caller's extra flags survive.
func TestBenchCommandPinsEveryFlag(t *testing.T) {
	command := BenchCommand(benchFixture())
	if command.Name != GoBinary {
		t.Fatalf("Name = %q, want %q", command.Name, GoBinary)
	}
	if command.Dir != benchFixture().Dir {
		t.Errorf("Dir = %q, want %q", command.Dir, benchFixture().Dir)
	}
	for _, want := range []string{"test", runNone, "-benchmem", "-count", "5", "-timeout", "3m0s", "./..."} {
		if !contains(command.Args, want) {
			t.Errorf("args = %v, missing %q", command.Args, want)
		}
	}
	if got := argsAfter(command.Args, flagRun); got != runNone {
		t.Errorf("args after %s = %q, want %q", flagRun, got, runNone)
	}
	if got := argsAfter(command.Args, flagBench); got != "Concat" {
		t.Errorf("args after %s = %q, want Concat", flagBench, got)
	}
	// -benchtime is always explicit so old and new milestones stay comparable.
	if got := argsAfter(command.Args, flagBenchTime); got != defaultBenchTime {
		t.Errorf("args after %s = %q, want %q", flagBenchTime, got, defaultBenchTime)
	}
	if contains(command.Args, flagCPUProfile) || contains(command.Args, flagMemProfile) {
		t.Errorf("a plain benchmark run must not profile: %v", command.Args)
	}
}

// TestBenchCommandDefaultsToAllBenchmarks checks the empty-filter and zero-count cases.
func TestBenchCommandDefaultsToAllBenchmarks(t *testing.T) {
	spec := BenchSpec{Dir: "out", Packages: []string{"./..."}}
	command := BenchCommand(spec)
	if got := argsAfter(command.Args, flagBench); got != allBenchmarks {
		t.Errorf("args after %s = %q, want %q", flagBench, got, allBenchmarks)
	}
	if got := argsAfter(command.Args, flagCount); got != "1" {
		t.Errorf("args after %s = %q, want 1", flagCount, got)
	}
	if contains(command.Args, flagTimeout) {
		t.Errorf("a zero timeout must not be passed: %v", command.Args)
	}
}

// TestProfileTestCommandDiscardsTheTestBinary is the regression guard for the dirty
// working tree: `go test -o` must point at the null device, never at the package
// directory, because a leftover `pkg.test` blocks the next `benchbook milestone`.
func TestProfileTestCommandDiscardsTheTestBinary(t *testing.T) {
	command := ProfileTestCommand(profileFixture(t))
	if got := argsAfter(command.Args, flagOutput); got != os.DevNull {
		t.Fatalf("args after %s = %q, want %q (args = %v)", flagOutput, got, os.DevNull, command.Args)
	}
	for _, arg := range command.Args {
		if strings.HasSuffix(arg, ".test") || strings.HasSuffix(arg, ".exe") {
			t.Errorf("binary path %q must not be written into the repository (args = %v)", arg, command.Args)
		}
	}
	if got := argsAfter(command.Args, flagMemProfileRate); got != everyAllocation {
		t.Errorf("args after %s = %q, want %q", flagMemProfileRate, got, everyAllocation)
	}
	if got := argsAfter(command.Args, flagCPUProfile); got != filepath.Join("out", "raw", "cpu.prof") {
		t.Errorf("args after %s = %q, want the configured CPU profile", flagCPUProfile, got)
	}
	if got := command.Args[len(command.Args)-1]; got != "./internal/core" {
		t.Errorf("last arg = %q, want the single profiled package", got)
	}
}

// TestProfileTopCommandSymbolisesWithSampleIndex checks the pprof vector, including the
// rule that CPU profiles must not receive -sample_index.
func TestProfileTopCommandSymbolisesWithSampleIndex(t *testing.T) {
	heap := ProfileTopCommand("out", "raw/mem.prof", sampleIndexObjects, 12)
	if heap.Name != GoBinary {
		t.Errorf("Name = %q, want %q", heap.Name, GoBinary)
	}
	// -top is a boolean switch, so it must be the third token with nothing between it
	// and `tool pprof`.
	if !slices.Equal(heap.Args[:3], []string{"tool", "pprof", flagTop}) {
		t.Errorf("Args = %v, want to start with `tool pprof -top`", heap.Args)
	}
	if got := argsAfter(heap.Args, flagSampleIndex); got != sampleIndexObjects {
		t.Errorf("args after %s = %q, want %q", flagSampleIndex, got, sampleIndexObjects)
	}
	if got := argsAfter(heap.Args, flagNodeCount); got != "12" {
		t.Errorf("args after %s = %q, want 12", flagNodeCount, got)
	}
	cpu := ProfileTopCommand("out", "raw/cpu.prof", "", 12)
	if contains(cpu.Args, flagSampleIndex) {
		t.Errorf("CPU profiles must not get %s: %v", flagSampleIndex, cpu.Args)
	}
	if got := cpu.Args[len(cpu.Args)-1]; got != "raw/cpu.prof" {
		t.Errorf("last arg = %q, want the profile path", got)
	}
}

// TestSampleIndexFor pins the kind-to-index mapping, with the unknown kind falling back
// to "let pprof pick".
func TestSampleIndexFor(t *testing.T) {
	cases := map[string]string{
		"mem_space":   sampleIndexSpace,
		"mem_objects": sampleIndexObjects,
		"cpu":         "",
		"":            "",
	}
	for kind, want := range cases {
		if got := SampleIndexFor(kind); got != want {
			t.Errorf("SampleIndexFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestGitCommandKeepsArgumentsVerbatim checks the pass-through wrapper.
func TestGitCommandKeepsArgumentsVerbatim(t *testing.T) {
	command := GitCommand("out", "status", "--porcelain")
	if command.Name != GitBinary || command.Dir != "out" {
		t.Fatalf("command = %+v, want git in out", command)
	}
	if !slices.Equal(command.Args, []string{"status", "--porcelain"}) {
		t.Errorf("Args = %v, want [status --porcelain]", command.Args)
	}
}
