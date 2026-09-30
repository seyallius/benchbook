// Package compare. variant - comparisons inside a single run: every variant of a
// workload against the configured baseline variant. It reuses exactly the same
// machinery as run-versus-run comparison, so "⚡ 18.4% faster" means the same thing
// in both tables.
package compare

import (
	"sort"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// WorkloadComparison groups every variant of one workload, with the chosen baseline
// marked so a renderer can print a "baseline" cell instead of a delta.
type WorkloadComparison struct {
	// Workload is the workload name.
	Workload string `json:"workload"`
	// BaselineVariant is the variant everything else is compared against.
	BaselineVariant string `json:"baseline_variant"`
	// Rows are the variants in deterministic order (baseline first, then by name).
	Rows []VariantRow `json:"rows"`
	// HasBaseline reports whether the workload actually contains the baseline
	// variant. A workload without it gets no baseline column at all.
	HasBaseline bool `json:"has_baseline"`
}

// VariantRow is one variant of a workload with its verdict against the baseline.
type VariantRow struct {
	// Benchmark is the underlying benchmark.
	Benchmark model.Benchmark `json:"benchmark"`
	// DisplayName is the benchmark name without the "Benchmark" prefix.
	DisplayName string `json:"display_name"`
	// IsBaseline marks the variant the others are compared against.
	IsBaseline bool `json:"is_baseline"`
	// Comparison is the verdict against the baseline, nil for the baseline row.
	Comparison *BenchmarkComparison `json:"comparison"`
}

// ----------------------------------------- Public API ----------------------------------------- //

// WithinRun compares every variant of every workload against the baseline variant
// of that workload. A workload without the baseline variant is returned with
// HasBaseline false and no deltas, which is what makes single-variant workloads
// render without a baseline column.
func WithinRun(run *model.Run, baselineVariant string, options Options) []WorkloadComparison {
	options = withDefaults(options)
	if run == nil {
		return []WorkloadComparison{}
	}
	byWorkload := map[string][]model.Benchmark{}
	workloads := model.Workloads(run.Benchmarks)
	for _, benchmark := range run.Benchmarks {
		byWorkload[benchmark.Workload] = append(byWorkload[benchmark.Workload], benchmark)
	}
	out := make([]WorkloadComparison, 0, len(workloads))
	for _, workload := range workloads {
		out = append(out, compareWorkload(workload, byWorkload[workload], baselineVariant, options))
	}
	return out
}

// WorkloadOf compares the variants of a single workload against its baseline
// variant. Renderers use it to build one table without computing the whole run.
func WorkloadOf(run *model.Run, workload, baselineVariant string, options Options) WorkloadComparison {
	options = withDefaults(options)
	if run == nil {
		return WorkloadComparison{Workload: workload, BaselineVariant: baselineVariant}
	}
	benchmarks := []model.Benchmark{}
	for _, benchmark := range run.Benchmarks {
		if benchmark.Workload == workload {
			benchmarks = append(benchmarks, benchmark)
		}
	}
	return compareWorkload(workload, benchmarks, baselineVariant, options)
}

// compareWorkload builds one workload's rows.
func compareWorkload(workload string, benchmarks []model.Benchmark, baselineVariant string, options Options) WorkloadComparison {
	result := WorkloadComparison{Workload: workload, BaselineVariant: baselineVariant}
	baseline := findVariant(benchmarks, baselineVariant)
	result.HasBaseline = baseline != nil
	for _, benchmark := range benchmarks {
		row := VariantRow{
			Benchmark:   benchmark,
			DisplayName: model.DisplayName(benchmark.Name),
			IsBaseline:  baseline != nil && benchmark.ID == baseline.ID,
		}
		if baseline != nil && !row.IsBaseline {
			comparison := compareBenchmarks(baseline, &benchmark, options, true)
			row.Comparison = &comparison
		}
		result.Rows = append(result.Rows, row)
	}
	sort.SliceStable(result.Rows, func(i, j int) bool {
		if result.Rows[i].IsBaseline != result.Rows[j].IsBaseline {
			return result.Rows[i].IsBaseline
		}
		return result.Rows[i].Benchmark.Variant < result.Rows[j].Benchmark.Variant
	})
	return result
}

// findVariant returns the benchmark carrying the given variant, or nil.
func findVariant(benchmarks []model.Benchmark, variant string) *model.Benchmark {
	for i := range benchmarks {
		if benchmarks[i].Variant == variant {
			return &benchmarks[i]
		}
	}
	return nil
}
