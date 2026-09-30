// Package model. order - deterministic ordering helpers. Every writer and renderer
// goes through these so identical inputs always produce identical bytes.
package model

import (
	"sort"
	"strings"
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// sortMetrics sorts metric ids by the stable order defined in MetricOrder, then
// alphabetically for extras. It is the single place that defines metric ordering.
func sortMetrics(metrics []MetricID) {
	rank := func(metric MetricID) int {
		for i, known := range MetricOrder {
			if metric == known {
				return i
			}
		}
		return len(MetricOrder)
	}
	sort.SliceStable(metrics, func(i, j int) bool {
		ri, rj := rank(metrics[i]), rank(metrics[j])
		if ri != rj {
			return ri < rj
		}
		return metrics[i] < metrics[j]
	})
}

// ----------------------------------------- Public API ----------------------------------------- //

// SortBenchmarks orders benchmarks by their id, in place. Sorting by id (which is
// package-qualified) keeps JSON diffs stable across machines and runs.
func SortBenchmarks(benchmarks []Benchmark) {
	sort.SliceStable(benchmarks, func(i, j int) bool { return benchmarks[i].ID < benchmarks[j].ID })
}

// SortProfiles orders profiles by kind then target, in place.
func SortProfiles(profiles []Profile) {
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Kind != profiles[j].Kind {
			return profiles[i].Kind < profiles[j].Kind
		}
		return profiles[i].Target < profiles[j].Target
	})
}

// SortExtraKeys returns the keys of an extra-metric map in ascending order.
func SortExtraKeys(extra map[string]float64) []string {
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// FindBenchmark returns the benchmark with the given id, or nil when absent.
func FindBenchmark(run *Run, id string) *Benchmark {
	if run == nil {
		return nil
	}
	for i := range run.Benchmarks {
		if run.Benchmarks[i].ID == id {
			return &run.Benchmarks[i]
		}
	}
	return nil
}

// FindProfile returns the profile with the given kind and target, or nil when absent.
func FindProfile(run *Run, kind ProfileKind, target string) *Profile {
	if run == nil {
		return nil
	}
	for i := range run.Profiles {
		if run.Profiles[i].Kind == kind && run.Profiles[i].Target == target {
			return &run.Profiles[i]
		}
	}
	return nil
}

// WorkloadVariant splits a benchmark name into its workload and variant parts.
// Everything before the last "/" is the workload, the last segment is the variant
// (DefaultVariant when the name has no "/"). This is the idiomatic Go
// sub-benchmark convention; it needs no heuristics and never guesses.
func WorkloadVariant(name string) (workload, variant string) {
	trimmed := strings.TrimPrefix(name, BenchPrefix)
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[:idx], trimmed[idx+1:]
	}
	return trimmed, DefaultVariant
}

// BenchPrefix is the mandatory prefix of every Go benchmark function name.
const BenchPrefix = "Benchmark"

// DisplayName returns a benchmark name without the mandatory "Benchmark" prefix,
// which is how reports refer to it ("FindUser/result").
func DisplayName(name string) string { return strings.TrimPrefix(name, BenchPrefix) }

// Workloads returns the distinct workload names of a run, in ascending order.
func Workloads(benchmarks []Benchmark) []string {
	seen := make(map[string]bool, len(benchmarks))
	for _, benchmark := range benchmarks {
		seen[benchmark.Workload] = true
	}
	out := make([]string, 0, len(seen))
	for workload := range seen {
		out = append(out, workload)
	}
	sort.Strings(out)
	return out
}
