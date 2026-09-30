// Package render. summary - the `benchbook summary` snippet: a post-ready digest
// of one comparison in text, Markdown or JSON. It is the only renderer that emits
// JSON, and it emits the same canonical shape the site consumes.
package render

import (
	"fmt"
	"strings"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/model/canon"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Format is an output format of the summary command.
type Format string

// Supported summary formats.
const (
	// FormatText is the plain-text snippet used in commit messages and chat.
	FormatText Format = "text"
	// FormatMarkdown is the table snippet used in blog posts and pull requests.
	FormatMarkdown Format = "md"
	// FormatJSON is the machine-readable form.
	FormatJSON Format = "json"
)

// SupportedFormats lists the formats accepted by --format.
var SupportedFormats = []string{string(FormatText), string(FormatMarkdown), string(FormatJSON)}

// Line and column budgets of the text snippet.
const (
	// summaryHeadlineLimit caps the "top improvements" list.
	summaryHeadlineLimit = 3
	// mdHeadlineLimit caps the table rows in Markdown mode.
	mdHeadlineLimit = 12
)

// ----------------------------------------- Public API ----------------------------------------- //

// Summary renders a comparison in the requested format.
func Summary(comparison *compare.Comparison, format Format, headlineLimit int) (string, error) {
	if comparison == nil || comparison.From == nil || comparison.To == nil {
		return "", fmt.Errorf("summary needs two runs to compare")
	}
	if headlineLimit <= 0 {
		headlineLimit = summaryHeadlineLimit
	}
	switch format {
	case FormatText:
		return summaryText(comparison, headlineLimit), nil
	case FormatMarkdown:
		return summaryMarkdown(comparison), nil
	case FormatJSON:
		return summaryJSON(comparison)
	default:
		return "", fmt.Errorf("unknown summary format %q: use one of %s", format, strings.Join(SupportedFormats, ", "))
	}
}

// summaryText renders the two-line snippet used in the terminal and in posts.
func summaryText(comparison *compare.Comparison, limit int) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("benchbook %s vs %s: %s (%s, %s)\n",
		runID(comparison.To), runID(comparison.From),
		geomeanSnippet(comparison),
		benchmarkPhrase(comparison),
		samplePhrase(comparison)))
	findings := comparison.Findings(compare.Improved, limit)
	if len(findings) == 0 {
		findings = comparison.Findings(compare.Regressed, limit)
	}
	for _, finding := range findings {
		builder.WriteString(fmt.Sprintf("  %s %s → %s (%s, p=%s)\n",
			finding.Benchmark,
			FormatMetric(finding.Result.Metric, finding.Result.FromMedian),
			FormatMetric(finding.Result.Metric, finding.Result.ToMedian),
			FormatSignedPercent(finding.Result.DeltaPct),
			FormatPValue(finding.Result.P)))
	}
	return builder.String()
}

// geomeanSnippet renders "geomean −12.0% ns/op, −40.2% allocs/op".
func geomeanSnippet(comparison *compare.Comparison) string {
	parts := []string{}
	for _, geomean := range comparison.Geomeans {
		if geomean.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", FormatSignedPercent(geomean.DeltaPct), geomean.Metric))
	}
	if len(parts) == 0 {
		return "no comparable metrics"
	}
	return "geomean " + strings.Join(parts, ", ")
}

// benchmarkPhrase renders "41 benchmarks".
func benchmarkPhrase(comparison *compare.Comparison) string {
	count := 0
	for _, result := range comparison.Results {
		if result.Presence.InFrom && result.Presence.InTo {
			count++
		}
	}
	return Pluralize(count, "benchmark")
}

// samplePhrase renders "10 samples" from the newer run.
func samplePhrase(comparison *compare.Comparison) string {
	if comparison.To == nil || len(comparison.To.Benchmarks) == 0 {
		return "no samples"
	}
	return Pluralize(len(comparison.To.Benchmarks[0].Samples), "sample")
}

// summaryMarkdown renders a table of the most notable changes plus the headline.
func summaryMarkdown(comparison *compare.Comparison) string {
	var builder strings.Builder
	builder.WriteString(GeneratedHeader + "\n\n")
	builder.WriteString(fmt.Sprintf("**benchbook %s vs %s** — %s over %s.\n\n",
		runID(comparison.To), runID(comparison.From), geomeanSnippet(comparison), benchmarkPhrase(comparison)))
	rows := [][]string{}
	for _, result := range comparison.Results {
		if !result.Presence.InFrom || !result.Presence.InTo {
			continue
		}
		delta, ok := metricResult(result, primaryMetricID())
		if !ok {
			continue
		}
		rows = append(rows, []string{
			result.DisplayName,
			FormatMetric(primaryMetricID(), delta.FromSummary.Median),
			FormatMetric(primaryMetricID(), delta.ToSummary.Median),
			FormatSignedPercent(delta.DeltaPct),
			Badge(delta),
		})
	}
	if len(rows) > mdHeadlineLimit {
		rows = rows[:mdHeadlineLimit]
	}
	builder.WriteString(Table(tableIndentNone,
		[]string{"Benchmark", "before", "after", "Δ", "verdict"}, rows))
	return builder.String()
}

// primaryMetricID is the metric that decides the headline verdict.
func primaryMetricID() model.MetricID { return model.MetricNsOp }

// summaryJSON renders the machine-readable form with a stable shape.
func summaryJSON(comparison *compare.Comparison) (string, error) {
	payload := struct {
		// From is the reference run's id.
		From string `json:"from"`
		// To is the newer run's id.
		To string `json:"to"`
		// Geomeans holds the headline figure per metric.
		Geomeans []compare.MetricGeomean `json:"geomeans"`
		// Counts holds the benchmark count per verdict.
		Counts map[compare.Classification]int `json:"counts"`
		// Results holds every benchmark verdict.
		Results []compare.BenchmarkComparison `json:"results"`
	}{
		From:     runID(comparison.From),
		To:       runID(comparison.To),
		Geomeans: comparison.Geomeans,
		Counts:   comparison.Counts,
		Results:  comparison.Results,
	}
	encoded, err := canon.Encode(payload, model.CanonicalIndent)
	if err != nil {
		return "", fmt.Errorf("encode summary JSON: %w", err)
	}
	return string(encoded), nil
}

// runID renders a run reference for a snippet: "m003", "current".
func runID(run *model.Run) string {
	if run == nil {
		return "unknown"
	}
	if run.IsCurrent() {
		return "current"
	}
	return run.ID
}
