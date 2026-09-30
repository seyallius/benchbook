// Package render. timeline - the milestone timeline: a headline chain per metric,
// one collapsible section per metric with the per-benchmark chain and an inline SVG
// chart, and the milestone table with its warning badges.
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

// Presentation constants of the timeline.
const (
	// chartYLabel is printed above the y axis of every chart.
	chartYLabel = "index, first milestone = 100"
	// chartCurrentLabel is the x label of the unpromoted working run.
	chartCurrentLabel = "current"
	// breakLegend explains a hardware-change marker.
	breakLegend = "> ⚠ The vertical dashed line marks a hardware change: the milestones on either side are not directly comparable."
	// milestoneLabelLimit keeps a long label from flooding a chain line.
	milestoneLabelLimit = 28
)

// TimelineEntry is one milestone in the history together with its comparison
// against the milestone before it.
type TimelineEntry struct {
	// Run is the milestone itself.
	Run *model.Run
	// Previous is the milestone before it, or nil for the first one.
	Previous *model.Run
	// Badges are the warning badges of the milestone.
	Badges []string
	// Geomeans are the headline figures against the previous milestone.
	Geomeans []compare.MetricGeomean
}

// trackedRow is one row of the tracked-benchmark table.
type trackedRow struct {
	// displayName is the benchmark name without the "Benchmark" prefix.
	displayName string
	// chain is the rendered "152 µs → 124 µs" sequence.
	chain string
	// stepDelta is the change against the previous milestone that contains it.
	stepDelta string
	// cumulative is the change against the first milestone that contains it.
	cumulative string
	// values are the raw medians behind the chain, used by the chart.
	values []float64
}

// ----------------------------------------- Public API ----------------------------------------- //

// TimelinePage renders the timeline. The current run, when given, appears as a
// dotted final point in every chart: seeing the unpromoted run next to the history
// is what makes the promote-or-not decision easy.
func TimelinePage(ctx Context, milestones []*model.Run, current *model.Run) string {
	var builder strings.Builder
	builder.WriteString(GeneratedHeader + "\n\n")
	builder.WriteString("# Timeline\n\n")
	if len(milestones) == 0 {
		builder.WriteString("No milestones yet. Record a baseline with `benchbook milestone -m \"baseline\"`.\n")
		return builder.String()
	}
	entries := buildEntries(ctx, milestones)
	builder.WriteString("Milestones are deliberate, immutable checkpoints. Every figure below comes from a\n")
	builder.WriteString("significance test plus a minimum effect, never from a bare percentage.\n\n")
	builder.WriteString(metricChains(ctx, entries, current))
	builder.WriteString(milestoneTable(entries))
	builder.WriteString(hardwareNote(entries))
	return builder.String()
}

// buildEntries pairs every milestone with its predecessor and computes the headline
// figures between them.
func buildEntries(ctx Context, milestones []*model.Run) []TimelineEntry {
	entries := make([]TimelineEntry, 0, len(milestones))
	for index, milestone := range milestones {
		entry := TimelineEntry{Run: milestone}
		if index > 0 {
			entry.Previous = milestones[index-1]
			entry.Geomeans = compare.Between(entry.Previous, milestone, ctx.Options).Geomeans
		}
		entry.Badges = milestoneBadges(milestone, entry.Previous)
		entries = append(entries, entry)
	}
	return entries
}

// milestoneBadges lists the warning badges of a milestone compared with its
// predecessor. The same set is shown by `benchbook list`, so the terminal and the
// site never disagree.
func milestoneBadges(milestone, previous *model.Run) []string {
	badges := []string{}
	if milestone.Provenance.GitDirty {
		badges = append(badges, "⚠ dirty tree")
	}
	if milestone.Forced {
		badges = append(badges, "⚠ forced")
	}
	if milestone.IndicativeOnly {
		badges = append(badges, "⚠ indicative only")
	}
	if previous != nil {
		if milestone.Provenance.GoVersion != previous.Provenance.GoVersion {
			badges = append(badges, "⚠ Go version changed")
		}
		if milestone.Provenance.HardwareFingerprint != previous.Provenance.HardwareFingerprint {
			badges = append(badges, "⚠ hardware changed")
		}
	}
	return badges
}

// metricChains renders one collapsible section per metric.
func metricChains(ctx Context, entries []TimelineEntry, current *model.Run) string {
	metrics := metricsAcrossMilestones(entries)
	if len(metrics) == 0 {
		return "_no comparable metrics yet_\n\n"
	}
	var builder strings.Builder
	for _, metric := range metrics {
		builder.WriteString(fmt.Sprintf("<details open>\n<summary><strong>%s</strong> — %s</summary>\n\n",
			EscapeText(metricTitle(metric)), EscapeText(headlineChain(entries, metric))))
		builder.WriteString("#### Headline (geometric mean of the median ratios)\n\n")
		builder.WriteString(headlineTable(entries, metric))
		builder.WriteString("\n#### Tracked benchmarks\n\n")
		builder.WriteString(trackedTable(ctx, entries, metric))
		builder.WriteString("\n")
		builder.WriteString(chartSection(ctx, entries, current, metric))
		builder.WriteString("</details>\n\n")
	}
	return builder.String()
}

// metricTitle renders the human title of a metric.
func metricTitle(metric model.MetricID) string {
	switch metric {
	case model.MetricNsOp:
		return "ns/op (time per operation)"
	case model.MetricBytesOp:
		return "B/op (bytes allocated)"
	case model.MetricAllocsOp:
		return "allocs/op (allocations)"
	default:
		if model.IsHigherBetter(metric) {
			return metric + " (higher is better)"
		}
		return metric
	}
}

// metricsAcrossMilestones collects every metric present in the history, in
// canonical order.
func metricsAcrossMilestones(entries []TimelineEntry) []model.MetricID {
	set := map[model.MetricID]bool{}
	for _, entry := range entries {
		for _, benchmark := range entry.Run.Benchmarks {
			for _, metric := range model.MetricsOf(benchmark.Samples) {
				set[metric] = true
			}
		}
	}
	return model.OrderedMetrics(set)
}

// headlineChain renders "m001 baseline → m002 sync.Pool (−18.4%)   ·   cumulative −27.9%".
func headlineChain(entries []TimelineEntry, metric model.MetricID) string {
	parts := make([]string, 0, len(entries))
	cumulative := 0.0
	cumulativeDefined := false
	for index, entry := range entries {
		label := milestoneHeadline(entry.Run)
		if index == 0 {
			parts = append(parts, label+" baseline")
			continue
		}
		geomean, ok := geomeanFor(entry, metric)
		if !ok || geomean.Count == 0 {
			parts = append(parts, label)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", label, FormatRatio(geomean.DeltaPct)))
		cumulative, cumulativeDefined = cumulativeFactor(entries, index, metric)
	}
	chain := strings.Join(parts, " → ")
	if cumulativeDefined {
		chain += fmt.Sprintf("   ·   cumulative %s", FormatRatio(cumulative))
	}
	return chain
}

// milestoneHeadline renders "m002 sync.Pool" for a chain.
func milestoneHeadline(run *model.Run) string {
	if run == nil {
		return ""
	}
	if strings.TrimSpace(run.Label) == "" {
		return run.ID
	}
	return fmt.Sprintf("%s %s", run.ID, truncate(EscapeText(run.Label), milestoneLabelLimit))
}

// truncate shortens a string, adding an ellipsis when it cuts.
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// cumulativeFactor multiplies the step ratios up to the given index. Combining
// percentages means multiplying factors — adding percentages would be wrong as soon
// as there is more than one step.
func cumulativeFactor(entries []TimelineEntry, index int, metric model.MetricID) (float64, bool) {
	factor := 1.0
	defined := false
	for position := 1; position <= index; position++ {
		geomean, ok := geomeanFor(entries[position], metric)
		if !ok || geomean.Count == 0 || geomean.Ratio <= 0 {
			continue
		}
		factor *= geomean.Ratio
		defined = true
	}
	return (factor - 1) * percentScale, defined
}

// geomeanFor returns the cached headline figure of an entry for one metric.
func geomeanFor(entry TimelineEntry, metric model.MetricID) (compare.MetricGeomean, bool) {
	for _, geomean := range entry.Geomeans {
		if geomean.Metric == metric {
			return geomean, true
		}
	}
	return compare.MetricGeomean{}, false
}

// headlineTable renders the milestone-by-milestone headline table.
func headlineTable(entries []TimelineEntry, metric model.MetricID) string {
	rows := make([][]string, 0, len(entries))
	for index, entry := range entries {
		step := DashPlaceholder
		cumulative := DashPlaceholder
		if index > 0 {
			if geomean, ok := geomeanFor(entry, metric); ok && geomean.Count > 0 {
				step = fmt.Sprintf("%s over %s", FormatRatio(geomean.DeltaPct), Pluralize(geomean.Count, "benchmark"))
			}
			if value, defined := cumulativeFactor(entries, index, metric); defined {
				cumulative = FormatRatio(value)
			}
		}
		rows = append(rows, []string{
			entry.Run.ID,
			entry.Run.CreatedAt.UTC().Format(dateLayout),
			entry.Run.Label,
			step,
			cumulative,
		})
	}
	return Table(tableIndentNone, []string{"Milestone", "Date", "Label", "Step Δ", "Cumulative Δ"}, rows)
}

// trackedTable renders one row per tracked benchmark for one metric.
func trackedTable(ctx Context, entries []TimelineEntry, metric model.MetricID) string {
	rows := trackedRows(ctx, entries, metric)
	if len(rows) == 0 {
		return "_no tracked benchmark has this metric_\n"
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{row.displayName, row.chain, row.stepDelta, row.cumulative})
	}
	return Table(tableIndentNone, []string{"Benchmark", "Chain", "Step Δ", "Cumulative Δ"}, tableRows)
}

// trackedRows resolves the configured (or derived) tracked benchmarks and builds
// their chains for one metric.
func trackedRows(ctx Context, entries []TimelineEntry, metric model.MetricID) []trackedRow {
	names := trackedNames(ctx, entries)
	rows := make([]trackedRow, 0, len(names))
	for _, name := range names {
		row := trackedRow{displayName: name, chain: DashPlaceholder, stepDelta: DashPlaceholder, cumulative: DashPlaceholder}
		labels := []string{}
		for _, entry := range entries {
			benchmark := findByName(entry.Run, name)
			if benchmark == nil {
				continue
			}
			value, ok := medianOf(*benchmark, metric)
			if !ok {
				continue
			}
			row.values = append(row.values, value)
			labels = append(labels, FormatMetric(metric, value))
		}
		if len(labels) > 0 {
			row.chain = strings.Join(labels, " → ")
		}
		if len(row.values) > 1 {
			row.stepDelta = FormatRatio(stats.RelativeChange(row.values[len(row.values)-2], row.values[len(row.values)-1]))
			row.cumulative = FormatRatio(stats.RelativeChange(row.values[0], row.values[len(row.values)-1]))
		}
		rows = append(rows, row)
	}
	return rows
}

// findByName finds the benchmark of a run whose display name matches.
func findByName(run *model.Run, name string) *model.Benchmark {
	if run == nil {
		return nil
	}
	for index := range run.Benchmarks {
		if model.DisplayName(run.Benchmarks[index].Name) == name {
			return &run.Benchmarks[index]
		}
	}
	return nil
}

// medianOf returns the median of one metric of a benchmark.
func medianOf(benchmark model.Benchmark, metric model.MetricID) (float64, bool) {
	summary, err := stats.Summarize(model.SampleValues(benchmark.Samples, metric))
	if err != nil {
		return 0, false
	}
	return summary.Median, true
}

// trackedNames returns the benchmark names to track: the configured list when it is
// not empty, otherwise the baseline variant of every workload (capped).
func trackedNames(ctx Context, entries []TimelineEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	latest := entries[len(entries)-1].Run
	if len(ctx.Tracked) > 0 {
		return capNames(ctx.Tracked, ctx.MaxTrackedOrDefault())
	}
	names := []string{}
	for _, workload := range model.Workloads(latest.Benchmarks) {
		names = append(names, representativeName(latest, workload, ctx.BaselineVariant))
	}
	return capNames(names, ctx.MaxTrackedOrDefault())
}

// representativeName picks the benchmark that represents a workload: the baseline
// variant when the workload has one, otherwise the first variant in name order.
func representativeName(run *model.Run, workload, baselineVariant string) string {
	fallback := ""
	for _, benchmark := range run.Benchmarks {
		if benchmark.Workload != workload {
			continue
		}
		if benchmark.Variant == baselineVariant {
			return model.DisplayName(benchmark.Name)
		}
		if fallback == "" || benchmark.Variant < fallback {
			fallback = model.DisplayName(benchmark.Name)
		}
	}
	return fallback
}

// capNames truncates a name list to a limit; a limit of zero or less means no cap.
func capNames(names []string, limit int) []string {
	if limit <= 0 || len(names) <= limit {
		return names
	}
	return names[:limit]
}

// chartSection renders the inline SVG chart for one metric.
func chartSection(ctx Context, entries []TimelineEntry, current *model.Run, metric model.MetricID) string {
	markup := buildChart(ctx, entries, current, metric)
	if markup == "" {
		return ""
	}
	return "#### Index chart\n\n" + markup + "\n"
}

// buildChart assembles the chart for one metric: one series per tracked benchmark,
// one x slot per milestone plus the working run, and break markers where the
// hardware changed.
func buildChart(ctx Context, entries []TimelineEntry, current *model.Run, metric model.MetricID) string {
	rows := trackedRows(ctx, entries, metric)
	labels := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		labels = append(labels, entry.Run.ID)
	}
	currentSlot := -1
	if current != nil {
		currentSlot = len(entries)
		labels = append(labels, chartCurrentLabel)
	}

	series := make([]ChartSeries, 0, len(rows))
	for _, row := range rows {
		points := make([]ChartPoint, 0, len(row.values)+1)
		for slot, value := range row.values {
			points = append(points, ChartPoint{X: slot, Value: value})
		}
		if currentSlot >= 0 {
			if benchmark := findByName(current, row.displayName); benchmark != nil {
				if value, ok := medianOf(*benchmark, metric); ok && value > 0 {
					points = append(points, ChartPoint{X: currentSlot, Value: value, Current: true})
				}
			}
		}
		if len(points) >= 2 {
			series = append(series, ChartSeries{Title: row.displayName, Points: points})
		}
	}
	if len(series) == 0 {
		return ""
	}

	breaks := []int{}
	for index, entry := range entries {
		if index > 0 && entry.Run.Provenance.HardwareFingerprint != entries[index-1].Run.Provenance.HardwareFingerprint {
			breaks = append(breaks, index)
		}
	}
	markup := LineChart(Chart{
		Title:   fmt.Sprintf("%s across milestones", metricTitle(metric)),
		XLabels: labels,
		Series:  series,
		YLabel:  chartYLabel,
		Breaks:  breaks,
	})
	if markup == "" {
		return ""
	}
	if len(breaks) > 0 {
		markup += "\n" + breakLegend + "\n"
	}
	if currentSlot >= 0 {
		markup += "\n_The dotted final point is the unpromoted current run._\n"
	}
	return markup
}

// milestoneTable renders the history table with badges and notes.
func milestoneTable(entries []TimelineEntry) string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{
			entry.Run.ID,
			entry.Run.CreatedAt.UTC().Format(dateLayout),
			entry.Run.Label,
			entry.Run.Note,
			ShortHash(entry.Run.Provenance.GitSHA, shortSHALength),
			strings.Join(entry.Badges, " "),
			headlineFor(entry),
		})
	}
	return "\n## Milestones\n\n" + Table(tableIndentNone,
		[]string{"ID", "Date", "Label", "Note", "Commit", "Badges", "Headline vs previous"}, rows)
}

// headlineFor renders the headline figures of a milestone for the history table.
func headlineFor(entry TimelineEntry) string {
	if entry.Previous == nil {
		return "baseline"
	}
	parts := []string{}
	for _, geomean := range entry.Geomeans {
		if geomean.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", FormatRatio(geomean.DeltaPct), geomean.Metric))
	}
	if len(parts) == 0 {
		return DashPlaceholder
	}
	return strings.Join(parts, ", ")
}

// hardwareNote explains which milestones are affected by a hardware change.
func hardwareNote(entries []TimelineEntry) string {
	changes := []string{}
	for index, entry := range entries {
		if index == 0 {
			continue
		}
		if entry.Run.Provenance.HardwareFingerprint != entries[index-1].Run.Provenance.HardwareFingerprint {
			changes = append(changes, fmt.Sprintf("%s → %s", entries[index-1].Run.ID, entry.Run.ID))
		}
	}
	if len(changes) == 0 {
		return ""
	}
	return fmt.Sprintf("\n> **Hardware changed** between %s. Those milestones are kept, but their percentages\n> are not directly comparable — re-measure a baseline on the new machine.\n",
		EscapeText(strings.Join(changes, ", ")))
}

// SortMilestones orders milestones by sequence number, ascending.
func SortMilestones(milestones []*model.Run) {
	sort.SliceStable(milestones, func(i, j int) bool { return milestones[i].Seq < milestones[j].Seq })
}
