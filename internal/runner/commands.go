// Package runner. commands - pure builders that turn benchbook configuration into
// `go test` and `go tool pprof` argument vectors. Keeping them pure (no I/O, no
// environment inspection) means the exact flags are unit-testable.
package runner

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names shared by the builders. They are constants rather than literals so a
// typo cannot silently produce a different command.
const (
	// flagRun disables tests; benchbook only wants benchmarks.
	flagRun = "-run"
	// runNone is the regex matching no test at all.
	runNone = "^$"
	// flagBench selects the benchmarks to run.
	flagBench = "-bench"
	// flagBenchTime pins the measurement time, which keeps runs comparable.
	flagBenchTime = "-benchtime"
	// flagCount sets the number of repetitions.
	flagCount = "-count"
	// flagTimeout bounds the whole test binary.
	flagTimeout = "-timeout"
	// flagCPUProfile writes a CPU profile.
	flagCPUProfile = "-cpuprofile"
	// flagMemProfile writes a heap profile.
	flagMemProfile = "-memprofile"
	// flagMemProfileRate records every allocation.
	flagMemProfileRate = "-memprofilerate"
	// flagOutput redirects the compiled test binary away from the package directory.
	flagOutput = "-o"
	// flagTop selects the pprof -top view.
	flagTop = "-top"
	// flagNodeCount limits how many pprof rows are printed.
	flagNodeCount = "-nodecount"
	// flagSampleIndex selects which heap metric to show.
	flagSampleIndex = "-sample_index"
)

// Special values for the go test command.
const (
	// allBenchmarks is the regex matching every benchmark.
	allBenchmarks = "."
	// everyAllocation is the memprofilerate value that records every allocation,
	// which makes allocation counts reproducible instead of sampled.
	everyAllocation = "1"
	// sampleIndexSpace selects the byte-based heap view.
	sampleIndexSpace = "inuse_space"
	// sampleIndexObjects selects the object-count heap view.
	sampleIndexObjects = "inuse_objects"
)

// testBinaryDiscard is where the profiling run sends the compiled test binary. The
// null device is used instead of a temp path because writing it into the package
// directory would leave an untracked file behind on every `benchbook profile`.
const testBinaryDiscard = os.DevNull

// Binary names invoked by the builders.
const (
	// goBinary is the toolchain entry point.
	goBinary = "go"
	// gitBinary is the VCS entry point.
	gitBinary = "git"
)

// GoBinary is the name of the Go toolchain binary; exported so the CLI can report
// the same name it executes.
const GoBinary = goBinary

// GitBinary is the name of the git binary.
const GitBinary = gitBinary

// BenchSpec describes a `go test -bench` invocation.
type BenchSpec struct {
	// Dir is the module directory the command runs in.
	Dir string
	// Packages are the package patterns.
	Packages []string
	// Args are extra flags from [bench].args.
	Args []string
	// Count is the number of samples; <= 0 means "use -count=1".
	Count int
	// Timeout bounds the test binary.
	Timeout time.Duration
	// Bench is the benchmark filter regex; empty means "all benchmarks".
	Bench string
	// GoFlags are flags passed to the `go` tool itself, e.g. -tags.
	GoFlags []string
}

// ProfileSpec describes a profiled `go test -bench` invocation.
type ProfileSpec struct {
	// BenchSpec is the benchmark run being profiled.
	BenchSpec BenchSpec
	// Package is the single package to profile.
	Package string
	// CPUOut is the path of the CPU profile.
	CPUOut string
	// MemOut is the path of the heap profile.
	MemOut string
}

// ----------------------------------------- Public API ----------------------------------------- //

// BenchCommand builds the `go test -run '^$' -bench ...` command. It always adds
// -run=-^$ so ordinary tests never run (they would pollute the timings and the
// output), and always passes -benchtime explicitly so runs stay comparable.
func BenchCommand(spec BenchSpec) Command {
	count := spec.Count
	if count <= 0 {
		count = 1
	}
	bench := strings.TrimSpace(spec.Bench)
	if bench == "" {
		bench = allBenchmarks
	}
	args := []string{}
	args = append(args, spec.GoFlags...)
	args = append(args, "test", flagRun, runNone, flagBench, bench, flagBenchTime, spec.BenchTime())
	args = append(args, spec.Args...)
	args = append(args, flagCount, fmt.Sprintf("%d", count))
	if spec.Timeout > 0 {
		args = append(args, flagTimeout, spec.Timeout.String())
	}
	args = append(args, spec.Packages...)
	return Command{Dir: spec.Dir, Name: goBinary, Args: args}
}

// BenchTime returns the -benchtime value used for every benchbook run. Fixing it
// explicitly (rather than relying on the default `1s`) keeps an old milestone
// comparable with a new one even if the Go default ever changes.
func (spec BenchSpec) BenchTime() string { return defaultBenchTime }

// defaultBenchTime is the fixed measurement time per benchmark.
const defaultBenchTime = "1s"

// ProfileTestCommand builds the `go test` invocation that writes both profiles for
// one package. -memprofilerate=1 is set so allocation counts are exact rather than
// extrapolated from a sample, and -o points the test binary at the null device so a
// multi-megabyte `pkg.test` is never dropped into the user's repository (which would
// also make the working tree dirty and block the next milestone).
func ProfileTestCommand(spec ProfileSpec) Command {
	count := spec.BenchSpec.Count
	if count <= 0 {
		count = 1
	}
	bench := strings.TrimSpace(spec.BenchSpec.Bench)
	if bench == "" {
		bench = allBenchmarks
	}
	args := []string{}
	args = append(args, spec.BenchSpec.GoFlags...)
	args = append(args, "test", flagRun, runNone, flagBench, bench, flagBenchTime, defaultBenchTime)
	args = append(args, spec.BenchSpec.Args...)
	args = append(args, flagCount, fmt.Sprintf("%d", count))
	args = append(args, flagCPUProfile, spec.CPUOut, flagMemProfile, spec.MemOut, flagMemProfileRate, everyAllocation)
	args = append(args, flagOutput, testBinaryDiscard)
	if spec.BenchSpec.Timeout > 0 {
		args = append(args, flagTimeout, spec.BenchSpec.Timeout.String())
	}
	args = append(args, spec.Package)
	return Command{Dir: spec.BenchSpec.Dir, Name: goBinary, Args: args}
}

// ProfileTopCommand builds the `go tool pprof -top` command for one profile file.
// The sample index selects bytes or objects for heap profiles; CPU profiles do not
// understand the flag and must not receive it.
func ProfileTopCommand(dir, profilePath, sampleIndex string, top int) Command {
	args := []string{"tool", "pprof", flagTop}
	if strings.TrimSpace(sampleIndex) != "" {
		args = append(args, flagSampleIndex, sampleIndex)
	}
	args = append(args, flagNodeCount, fmt.Sprintf("%d", top), profilePath)
	return Command{Dir: dir, Name: goBinary, Args: args}
}

// SampleIndexFor maps a profile kind onto the pprof sample index it needs.
func SampleIndexFor(kind string) string {
	switch kind {
	case "mem_space":
		return sampleIndexSpace
	case "mem_objects":
		return sampleIndexObjects
	default:
		return ""
	}
}

// GitCommand builds a `git` invocation in the given directory.
func GitCommand(dir string, args ...string) Command {
	return Command{Dir: dir, Name: gitBinary, Args: args}
}
