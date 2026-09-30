// Package collect. environment - the local Environment implementation. Facts that
// are portable come from the runtime package; the two that are not (CPU model and
// physical memory) are implemented per operating system and degrade to "unknown"
// instead of failing a run.
package collect

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/seyallius/benchbook/internal/runner"
)

// ---------------------------------------------- Types ----------------------------------------- //

// goVersionFields is the number of whitespace-separated fields in `go version`
// output; the version itself is the third field ("go version go1.22.5 linux/amd64").
const goVersionFields = 3

// goVersionIndex is the zero-based index of the version field.
const goVersionIndex = 2

// OSEnvironment reads facts from the machine benchbook runs on.
type OSEnvironment struct {
	// exec runs the probes that need an external command (go version, sysctl, ...).
	exec runner.Runner
}

// StaticEnvironment is an Environment with fixed answers, used by tests and by
// `--no-hw` runs that must not touch the host (CI, containers).
type StaticEnvironment struct {
	// Version is returned by GoVersion.
	Version string
	// OS is returned by GOOS.
	OS string
	// Arch is returned by GOARCH.
	Arch string
	// Model is returned by CPUModel.
	Model string
	// Cores is returned by CPUCores.
	Cores int
	// Memory is returned by MemoryBytes.
	Memory uint64
	// Host is returned by Hostname.
	Host string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// NewLocalEnvironment returns an Environment backed by the real machine.
func NewLocalEnvironment() *OSEnvironment { return &OSEnvironment{exec: runner.NewExec()} }

// NewEnvironmentWithRunner returns an OSEnvironment whose external probes go
// through the given Runner; tests use it to keep hardware collection hermetic.
func NewEnvironmentWithRunner(exec runner.Runner) *OSEnvironment { return &OSEnvironment{exec: exec} }

// NewStaticEnvironment returns an Environment with fixed answers.
func NewStaticEnvironment(static StaticEnvironment) *StaticEnvironment {
	if strings.TrimSpace(static.Model) == "" {
		static.Model = UnknownValue
	}
	if strings.TrimSpace(static.Version) == "" {
		static.Version = UnknownValue
	}
	if static.OS == "" {
		static.OS = runtime.GOOS
	}
	if static.Arch == "" {
		static.Arch = runtime.GOARCH
	}
	return &static
}

// ----------------------------------------- Public API ----------------------------------------- //

// GoVersion asks the toolchain in PATH for its version, because that is the
// toolchain that will compile the benchmarks. When the probe fails it falls back to
// the runtime version, which is at worst a slightly stale but honest answer.
func (e *OSEnvironment) GoVersion(ctx context.Context) string {
	result, err := e.exec.Run(ctx, runner.Command{Name: runner.GoBinary, Args: []string{"version"}})
	if err != nil {
		return fallbackGoVersion()
	}
	fields := strings.Fields(string(result.Stdout))
	if len(fields) < goVersionFields {
		return fallbackGoVersion()
	}
	return fields[goVersionIndex]
}

// fallbackGoVersion returns the version benchbook itself was built with.
func fallbackGoVersion() string {
	version := runtime.Version()
	if strings.TrimSpace(version) == "" {
		return UnknownValue
	}
	return version
}

// GOOS reports the operating system the binaries run on.
func (e *OSEnvironment) GOOS() string { return runtime.GOOS }

// GOARCH reports the architecture the binaries run on.
func (e *OSEnvironment) GOARCH() string { return runtime.GOARCH }

// CPUModel reports the CPU marketing name, or UnknownValue when the probe fails.
func (e *OSEnvironment) CPUModel(ctx context.Context) string {
	model, err := e.cpuModel(ctx)
	trimmed := strings.TrimSpace(model)
	if err != nil || trimmed == "" {
		return UnknownValue
	}
	return trimmed
}

// CPUCores reports the number of logical CPUs available to the process, which is
// what the Go runtime actually uses when it schedules benchmark goroutines.
func (e *OSEnvironment) CPUCores() int { return runtime.NumCPU() }

// MemoryBytes reports the installed physical memory, or 0 when unknown.
func (e *OSEnvironment) MemoryBytes(ctx context.Context) uint64 {
	memory, err := e.memoryBytes(ctx)
	if err != nil {
		return 0
	}
	return memory
}

// Hostname reports the machine name, or UnknownValue when it cannot be read.
func (e *OSEnvironment) Hostname() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return UnknownValue
	}
	return hostname
}

// GoVersion implements Environment with a fixed answer.
func (s *StaticEnvironment) GoVersion(context.Context) string { return s.Version }

// GOOS implements Environment with a fixed answer.
func (s *StaticEnvironment) GOOS() string { return s.OS }

// GOARCH implements Environment with a fixed answer.
func (s *StaticEnvironment) GOARCH() string { return s.Arch }

// CPUModel implements Environment with a fixed answer.
func (s *StaticEnvironment) CPUModel(context.Context) string { return s.Model }

// CPUCores implements Environment with a fixed answer.
func (s *StaticEnvironment) CPUCores() int { return s.Cores }

// MemoryBytes implements Environment with a fixed answer.
func (s *StaticEnvironment) MemoryBytes(context.Context) uint64 { return s.Memory }

// Hostname implements Environment with a fixed answer.
func (s *StaticEnvironment) Hostname() string { return s.Host }
