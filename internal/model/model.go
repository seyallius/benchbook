// Package model. model - canonical, storage-independent schema types shared by every
// other package. This file is the source of truth for the on-disk JSON shape.
package model

import "time"

// ---------------------------------------------- Types ----------------------------------------- //

// SchemaVersion is the version of the on-disk JSON schema this binary understands.
// It is written into every file and checked on every load.
const SchemaVersion = 1

// Kind of profile supported by benchbook. The values mirror `go test` output kinds
// and are used as stable identifiers in JSON, config and CLI flags.
type ProfileKind = string

// Supported profile kinds.
const (
	// ProfileKindCPU is the pprof CPU profile ("cpu").
	ProfileKindCPU ProfileKind = "cpu"
	// ProfileKindMemSpace is the pprof heap profile measured in bytes ("mem_space").
	ProfileKindMemSpace ProfileKind = "mem_space"
	// ProfileKindMemObjects is the pprof heap profile measured in object counts ("mem_objects").
	ProfileKindMemObjects ProfileKind = "mem_objects"
)

// ProfileTargetFull is the target name used for a profile covering the whole suite.
const ProfileTargetFull = "full"

// DefaultVariant is the synthetic variant name given to benchmarks that carry no
// sub-benchmark, so every workload always has at least one (unnamed) variant.
const DefaultVariant = "default"

// MetricNames of the metrics every benchmark sample always carries.
const (
	// MetricNsOp is wall time per operation, in nanoseconds.
	MetricNsOp = "ns_op"
	// MetricBytesOp is allocated bytes per operation.
	MetricBytesOp = "bytes_op"
	// MetricAllocsOp is the number of allocations per operation.
	MetricAllocsOp = "allocs_op"
)

// Run is one benchmark session. A run with Seq == 0 and an empty ID is the
// disposable "current" run; anything else is an immutable milestone.
type Run struct {
	// SchemaVersion is the schema revision of this document; see SchemaVersion.
	SchemaVersion int `json:"schema_version"`
	// ID is the milestone identifier ("m002"); empty for the current run.
	ID string `json:"id"`
	// Seq is the milestone sequence number; 0 for the current run.
	Seq int `json:"seq"`
	// Label is the human title of the milestone; empty for the current run.
	Label string `json:"label"`
	// Note is free text describing what changed and why.
	Note string `json:"note"`
	// CreatedAt is the moment the run finished. Injected, never read from the clock
	// inside renderers, so that output stays deterministic.
	CreatedAt time.Time `json:"created_at"`
	// ContentHash is the sha256 of the canonical benchmark+profile payload. It
	// deliberately excludes Label, Note and Provenance so it stays stable across
	// hand edits and machine changes.
	ContentHash string `json:"content_hash"`
	// IndicativeOnly marks runs with too few samples to be promoted (e.g. --quick).
	IndicativeOnly bool `json:"indicative_only"`
	// Forced marks milestones that were promoted despite a broken safety rule.
	Forced bool `json:"forced"`
	// Provenance records where and how this run was produced.
	Provenance Provenance `json:"provenance"`
	// Benchmarks holds one entry per benchmark id, sorted by ID.
	Benchmarks []Benchmark `json:"benchmarks"`
	// Profiles holds one entry per (kind, target), sorted by kind then target.
	Profiles []Profile `json:"profiles"`
}

// IsCurrent reports whether the run is the disposable working run rather than a
// promoted milestone.
func (r *Run) IsCurrent() bool { return r.ID == "" && r.Seq == 0 }

// Provenance describes the environment a run was produced in. Everything here is
// metadata: it never influences the numbers, only whether they are comparable.
type Provenance struct {
	// GitSHA is the full commit hash, or empty when git is unavailable.
	GitSHA string `json:"git_sha"`
	// GitBranch is the checked-out branch name, or empty when unknown.
	GitBranch string `json:"git_branch"`
	// GitDirty is true when the working tree had uncommitted changes.
	GitDirty bool `json:"git_dirty"`
	// GoVersion is the toolchain version, e.g. "go1.22.5".
	GoVersion string `json:"go_version"`
	// GOOS is the target operating system.
	GOOS string `json:"goos"`
	// GOARCH is the target architecture.
	GOARCH string `json:"goarch"`
	// CPUModel is the marketing name of the CPU, or "unknown".
	CPUModel string `json:"cpu_model"`
	// CPUCores is the number of logical CPUs visible to the process.
	CPUCores int `json:"cpu_cores"`
	// MemoryBytes is the installed physical memory, or 0 when unknown.
	MemoryBytes uint64 `json:"memory_bytes"`
	// Hostname is empty unless the config opts in via include_hostname; docs are
	// public by default. It is intentionally excluded from the fingerprint.
	Hostname string `json:"hostname"`
	// HardwareFingerprint is the sha256 of goos|goarch|cpu_model|cpu_cores.
	HardwareFingerprint string `json:"hardware_fingerprint"`
	// BenchArgs is the effective `go test` argument string used for the run.
	BenchArgs string `json:"bench_args"`
}

// Benchmark is a single benchmark id (package + name) with every raw sample that
// was observed for it. Medians are derived later and never stored exclusively.
type Benchmark struct {
	// ID is Package + "." + Name, so equal names in different packages never collide.
	ID string `json:"id"`
	// Package is the import path the benchmark lives in.
	Package string `json:"package"`
	// Name is the benchmark name including any sub-benchmark path, e.g. "BenchmarkFindUser/result".
	Name string `json:"name"`
	// Workload is everything before the last "/" of Name, with the "Benchmark" prefix removed.
	Workload string `json:"workload"`
	// Variant is the last "/" segment of Name, or DefaultVariant when there is none.
	Variant string `json:"variant"`
	// Samples holds one entry per -count repetition, in observation order.
	Samples []Sample `json:"samples"`
}

// Sample is one repetition of one benchmark as reported by the harness.
type Sample struct {
	// Iterations is the loop count the harness chose (the "N" column).
	Iterations int64 `json:"iterations"`
	// NsOp is nanoseconds per operation.
	NsOp float64 `json:"ns_op"`
	// BytesOp is allocated bytes per operation, or nil when -benchmem was not used.
	BytesOp *float64 `json:"bytes_op"`
	// AllocsOp is allocations per operation, or nil when -benchmem was not used.
	AllocsOp *float64 `json:"allocs_op"`
	// Extra holds arbitrary "<float> <unit>" metrics reported after the standard
	// ones, keyed by unit (e.g. {"MB/s": 512.3}).
	Extra map[string]float64 `json:"extra"`
}

// Profile is one parsed pprof -top table. All numeric values are normalized to
// base units: nanoseconds for CPU, bytes for mem_space, object counts for
// mem_objects. Display formatting is the renderer's business.
type Profile struct {
	// Kind is one of the ProfileKind constants.
	Kind ProfileKind `json:"kind"`
	// Target is ProfileTargetFull or a benchmark name.
	Target string `json:"target"`
	// Meta holds the raw header lines of the pprof table, in file order.
	Meta []string `json:"meta"`
	// Rows holds the table rows, in the order pprof printed them.
	Rows []ProfileRow `json:"rows"`
}

// ProfileRow is one function row of a pprof -top table, in base units.
type ProfileRow struct {
	// Flat is the value attributed to this function alone, in base units.
	Flat float64 `json:"flat"`
	// FlatPct is the percentage of total value attributed to this function alone.
	FlatPct float64 `json:"flat_pct"`
	// SumPct is the running cumulative percentage of flat values, as printed by pprof.
	SumPct float64 `json:"sum_pct"`
	// Cum is the value including callees, in base units.
	Cum float64 `json:"cum"`
	// CumPct is the percentage of total value including callees.
	CumPct float64 `json:"cum_pct"`
	// Name is the function or symbol name.
	Name string `json:"name"`
}
