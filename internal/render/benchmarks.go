// Package render. benchmarks - the benchmark report: one section per category, one
// table per workload, with the comparison against the baseline variant and against
// a reference run. The same renderer produces `benchmarks.md` and every
// `milestones/<id>.md`, because a milestone report is the same table with a
// different reference point.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/stats"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Reference describes what the "vs" column compares against, so the heading says it
// instead of the reader having to guess.
type Reference struct {
	// Label is the column wording, e.g. "vs last milestone" or "vs m002".
	Label string
	// Run is the run the deltas refer to, or nil when there is nothing to compare.
	Run *model.Run
}

// ----------------------------------------- Public API ----------------------------------------- //

// BenchmarksPage renders the full benchmark report for a run. The reference run
// provides the "vs" column; passing a nil reference renders the tables without it.
func BenchmarksPage(ctx Context, run *model.Run, reference Reference, options compare.Options) string {
	if run == nil {
		return GeneratedHeader + "\n\n# Benchmarks\n\nNo run recorded yet. Record one with `benchbook run`.\n"
	}
	var builder strings.Builder
	builder.WriteString(GeneratedHeader + "\n\n")
	builder.WriteString(fmt.Sprintf("# Benchmarks — %s\n\n", EscapeText(runLabelText(run))))
	if run.IndicativeOnly {
		builder.WriteString(banner("indicative only",
			"this run has a single sample per benchmark; treat every number as a hint, not a measurement."))
	}
	builder.WriteString(metaLine(run, reference))
	builder.WriteString(notesBlock(ctx, run, reference))
	builder.WriteString(benchmarkSections(ctx, run, reference, options))
	builder.WriteString(profilesSummary(ctx, run))
	builder.WriteString(HardwareBlock(run))
	return builder.String()
}

// profilesSummary renders the one-line-per-profile digest with a link to the full
// profiling page, so the benchmark page stays about benchmarks.
func profilesSummary(ctx Context, run *model.Run) string {
	if len(run.Profiles) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("## Profiles\n\n")
	builder.WriteString("Full tables live on the [profiling page](#/content/profiling.md); the digest is:\n\n")
	for _, profile := range run.Profiles {
		builder.WriteString("- " + EscapeText(ProfileSummaryLine(profile)) + "\n")
	}
	builder.WriteString("\n")
	return builder.String()
}

// metaLine renders the date and label line under the title.
func metaLine(run *model.Run, reference Reference) string {
	parts := []string{}
	if !run.CreatedAt.IsZero() {
		parts = append(parts, run.CreatedAt.UTC().Format(dateLayout))
	}
	if !run.IsCurrent() && run.Seq > 0 {
		parts = append(parts, fmt.Sprintf("sequence %d", run.Seq))
	}
	if run.IndicativeOnly {
		parts = append(parts, "indicative only")
	}
	if reference.Run != nil && !reference.Run.IsCurrent() {
		parts = append(parts, "compared with "+reference.Run.ID)
	}
	if len(parts) == 0 {
		return ""
	}
	return "_" + EscapeText(strings.Join(parts, " · ")) + "_\n\n"
}

// notesBlock explains how to read the tables before showing them.
func notesBlock(ctx Context, run *model.Run, reference Reference) string {
	var builder strings.Builder
	builder.WriteString("### How to read this\n\n")
	builder.WriteString(fmt.Sprintf("- Values are medians over %s; `±x%%` is the half-range divided by the median.\n", sampleRange(run)))
	builder.WriteString("- Lower is better for `ns/op`, `B/op` and `allocs/op`. Throughput metrics such as `MB/s` are the exception and are marked `↑`.\n")
	if reference.Label != "" {
		builder.WriteString(fmt.Sprintf("- `%s` is a two-sided Mann-Whitney U test at α = %.2f plus a minimum effect of %.1f%%: a change is only labelled when it is both significant and large enough to matter.\n",
			EscapeText(reference.Label), ctx.Options.Alpha, ctx.Options.MinEffectPct))
	}
	builder.WriteString("- Fewer than 4 samples per side can never reach significance, so those cells read `⚠ few samples` instead of inventing a percentage.\n")
	if ctx.NoiseFloorNote {
		builder.WriteString("- A laptop is not a quiet machine: keep at least 6 samples per benchmark before promoting a milestone.\n")
	}
	builder.WriteString("\n")
	return builder.String()
}

// banner renders a prominent callout, used for indicative-only runs.
func banner(title, text string) string {
	return fmt.Sprintf("> **%s** — %s\n\n", EscapeText(title), EscapeText(text))
}

// sampleRange renders "10 samples" or "6–10 samples" depending on the run.
func sampleRange(run *model.Run) string {
	counts := map[int]bool{}
	low, high := 0, 0
	for _, benchmark := range run.Benchmarks {
		count := len(benchmark.Samples)
		counts[count] = true
		if low == 0 || count < low {
			low = count
		}
		if count > high {
			high = count
		}
	}
	if len(counts) == 0 {
		return "no samples"
	}
	if len(counts) == 1 {
		return Pluralize(low, "sample")
	}
	return fmt.Sprintf("%d–%d samples", low, high)
}

// benchmarkSections renders one section per category and one table per workload.
func benchmarkSections(ctx Context, run *model.Run, reference Reference, options compare.Options) string {
	workloads := model.Workloads(run.Benchmarks)
	if len(workloads) == 0 {
		return "## Benchmarks\n\nThis run contains no benchmarks.\n\n"
	}
	byCategory := map[string][]string{}
	order := []string{}
	for _, workload := range workloads {
		category := ctx.CategoryFor(workload).Title
		if _, seen := byCategory[category]; !seen {
			order = append(order, category)
		}
		byCategory[category] = append(byCategory[category], workload)
	}
	referenceIndex := comparisonIndex(reference, run, options)

	var builder strings.Builder
	for _, category := range order {
		builder.WriteString(fmt.Sprintf("## %s\n\n", EscapeText(category)))
		for _, workload := range byCategory[category] {
			builder.WriteString(workloadTable(ctx, run, workload, reference, options, referenceIndex))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// workloadTable renders the variants of one workload side by side.
func workloadTable(ctx Context, run *model.Run, workload string, reference Reference, options compare.Options, referenceIndex map[string]compare.BenchmarkComparison) string {
	within := compare.WorkloadOf(run, workload, ctx.BaselineVariant, options)
	extraMetrics := extraMetricsOf(within)

	header := []string{"Variant", "Iterations", "ns/op (median ±spread)", "B/op", "allocs/op"}
	for _, metric := range extraMetrics {
		header = append(header, metricHeader(metric))
	}
	if within.HasBaseline {
		header = append(header, "vs "+ctx.BaselineVariant)
	}
	if reference.Label != "" {
		header = append(header, reference.Label)
	}

	rows := make([][]string, 0, len(within.Rows))
	for _, row := range within.Rows {
		cells := []string{
			row.DisplayName,
			iterationsCell(row.Benchmark),
			summaryCellFor(row.Benchmark, model.MetricNsOp),
			summaryCellFor(row.Benchmark, model.MetricBytesOp),
			summaryCellFor(row.Benchmark, model.MetricAllocsOp),
		}
		for _, metric := range extraMetrics {
			cells = append(cells, summaryCellFor(row.Benchmark, metric))
		}
		if within.HasBaseline {
			cells = append(cells, baselineCell(row))
		}
		if reference.Label != "" {
			cells = append(cells, referenceCell(referenceIndex[row.Benchmark.ID], model.MetricNsOp))
		}
		rows = append(rows, cells)
	}
	return fmt.Sprintf("### %s\n\n", EscapeText(workload)) + Table(tableIndentNone, header, rows)
}

// extraMetricsOf returns the non-standard metrics of a workload in ascending unit
// order, so a MB/s column always sits in the same place.
func extraMetricsOf(within compare.WorkloadComparison) []model.MetricID {
	seen := map[model.MetricID]bool{}
	for _, row := range within.Rows {
		for _, metric := range model.MetricsOf(row.Benchmark.Samples) {
			switch metric {
			case model.MetricNsOp, model.MetricBytesOp, model.MetricAllocsOp:
			default:
				seen[metric] = true
			}
		}
	}
	out := make([]model.MetricID, 0, len(seen))
	for metric := range seen {
		out = append(out, metric)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// metricHeader renders a column header including the direction of the metric.
func metricHeader(metric model.MetricID) string {
	if model.IsHigherBetter(metric) {
		return fmt.Sprintf("%s ↑ better", metric)
	}
	return string(metric)
}

// iterationsCell renders the iteration count of the first sample, which is the one
// the harness chose for the measured run.
func iterationsCell(benchmark model.Benchmark) string {
	if len(benchmark.Samples) == 0 {
		return DashPlaceholder
	}
	return HumanizeCount(benchmark.Samples[0].Iterations)
}

// summaryCellFor renders one metric cell of a benchmark, or a dash when the
// benchmark did not report that metric (e.g. B/op without -benchmem).
func summaryCellFor(benchmark model.Benchmark, metric model.MetricID) string {
	values := model.SampleValues(benchmark.Samples, metric)
	summary, err := stats.Summarize(values)
	if err != nil {
		return DashPlaceholder
	}
	return SummaryCell(summary, metric)
}

// baselineCell renders the verdict of a variant against the baseline variant.
func baselineCell(row compare.VariantRow) string {
	if row.IsBaseline {
		return "baseline"
	}
	if row.Comparison == nil {
		return DashPlaceholder
	}
	result, ok := metricResult(*row.Comparison, model.MetricNsOp)
	if !ok {
		return DashPlaceholder
	}
	return Badge(result)
}

// comparisonIndex compares the reference run with the given run and indexes the
// results by benchmark id. A nil reference yields an empty index.
func comparisonIndex(reference Reference, run *model.Run, options compare.Options) map[string]compare.BenchmarkComparison {
	index := map[string]compare.BenchmarkComparison{}
	if reference.Run == nil {
		return index
	}
	comparison := compare.Between(reference.Run, run, options)
	for _, result := range comparison.Results {
		index[result.ID] = result
	}
	return index
}

// referenceCell renders the verdict against the reference run.
func referenceCell(result compare.BenchmarkComparison, metric model.MetricID) string {
	if result.ID == "" {
		return DashPlaceholder
	}
	found, ok := metricResult(result, metric)
	if !ok {
		return DashPlaceholder
	}
	return Badge(found)
}

// metricResult finds one metric of a benchmark comparison.
func metricResult(result compare.BenchmarkComparison, metric model.MetricID) (compare.MetricResult, bool) {
	for _, candidate := range result.Metrics {
		if candidate.Metric == metric {
			return candidate, true
		}
	}
	return compare.MetricResult{}, false
}

// runLabelText renders "m002 — sync.Pool for buffers" or "current run".
func runLabelText(run *model.Run) string {
	if run == nil {
		return "unknown"
	}
	if run.IsCurrent() {
		return "current run"
	}
	if strings.TrimSpace(run.Label) == "" {
		return run.ID
	}
	return fmt.Sprintf("%s — %s", run.ID, run.Label)
}

// dateLayout is the date format used in generated pages.
const dateLayout = "2006-01-02"
