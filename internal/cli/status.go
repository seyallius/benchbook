// Package cli. status - the `benchbook status` command. This is the moment the
// whole model exists for: you see, in five lines, whether what you just measured is
// worth promoting, and you are told what to type next.
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Presentation constants of the status report.
const (
	// findingsShown is how many improvements and regressions are listed.
	findingsShown = 3
	// separator is the dot used between the geomean figures.
	separator = "  ·  "
	// dateLayout is the date format used in the header and the milestone table.
	dateLayout = "2006-01-02"
)

// Classification marks used in the one-line counts. They mirror the badges of the
// generated pages so the terminal and the site tell the same story.
const (
	// markImproved marks an improvement.
	markImproved = "⚡"
	// markRegressed marks a regression.
	markRegressed = "🐢"
	// markUnchanged marks a benchmark that did not change beyond noise.
	markUnchanged = "~"
	// markInsufficient marks a comparison that could not be tested.
	markInsufficient = "⚠"
	// markNew marks a benchmark that only the current run has.
	markNew = "＋"
	// markRemoved marks a benchmark that disappeared.
	markRemoved = "－"
)

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newStatusCommand builds `benchbook status`.
func (a *App) newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     cmdStatus,
		GroupID: groupHistory,
		Short:   "Compare the working run with the last milestone",
		Long: `Compare benchbook/current.json with the latest milestone: the geometric mean per
metric, how many benchmarks improved or regressed, the notable findings, and whether
the two runs are comparable at all.

This is the everyday command. A change is only labelled when it is significant and
larger than the configured minimum effect, so "unchanged" is a real answer here rather
than a failure to detect something.`,
		Example: "  benchbook status",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runStatus()
		},
	}
}

// ----------------------------------------- Public API ----------------------------------------- //

// runStatus compares the current run with the last milestone.
func (a *App) runStatus() error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	return a.printStatus(cfg, a.storeFor(cfg))
}

// printStatus loads both sides and prints the report. It is also called at the end
// of `benchbook run`, which is why it takes the already-open store.
func (a *App) printStatus(cfg *config.Config, runs *store.Store) error {
	current, err := runs.LoadCurrent()
	if err != nil {
		if errors.Is(err, store.ErrNoCurrent) {
			return &usageError{err: fmt.Errorf("%w: record one with `benchbook run`", err)}
		}
		return err
	}
	a.printf("📚 current run  (%s)\n", provenanceLine(current))
	if current.IndicativeOnly {
		a.printf("   %s\n", a.colorize(yellowCode, "indicative only: too few samples to promote"))
	}

	previous, err := runs.LatestMilestone()
	if err != nil {
		if !errors.Is(err, store.ErrNoMilestones) {
			return err
		}
		return a.printStatusWithoutHistory(current)
	}

	a.printf("📍 last milestone  %s  (%s, %s)\n", runLabel(previous),
		previous.CreatedAt.UTC().Format(dateLayout),
		render.ShortHash(previous.Provenance.GitSHA, shortSHALength))
	comparison := compare.Between(previous, current, compareThresholds(cfg))
	a.printComparisonSummary(comparison, previous, current)
	a.printHint(comparison, previous, current)
	return nil
}

// printStatusWithoutHistory explains what to do when there is nothing to compare
// against yet.
func (a *App) printStatusWithoutHistory(current *model.Run) error {
	a.printf("📍 no milestone yet — the history is empty\n\n")
	if len(current.Benchmarks) == 0 {
		a.printf("   this run has no benchmarks: check bench.packages and the filter\n")
		return nil
	}
	a.printf("💡 Nothing to compare against yet. Record the baseline:  benchbook milestone -m \"baseline\"\n")
	return nil
}

// printComparisonSummary prints the geomean headline, the classification counts,
// the notable findings and any comparability caveat.
func (a *App) printComparisonSummary(comparison *compare.Comparison, previous, current *model.Run) {
	if comparison.Incomparable && !comparison.Forced {
		a.printf("\n   %s\n", a.colorize(yellowCode, "⚠ not directly comparable: "+comparison.IncomparableReason))
		a.printf("   percentages are suppressed; inspect with `benchbook compare latest current --force-incomparable`\n")
	} else {
		a.printf("\n   geomean vs %s:  %s   %s\n",
			previous.ID,
			a.geomeanLine(comparison),
			a.colorize(dimCode, fmt.Sprintf("(%d benchmarks)", geomeanCount(comparison))))
		if comparison.Forced {
			a.printf("   %s\n", a.colorize(yellowCode, "⚠ forced view across hardware: "+comparison.IncomparableReason))
		}
	}
	a.printf("   %s\n\n", a.classificationCounts(comparison))
	a.printFindings(comparison)
	a.printProvenanceNotes(previous, current)
}

// geomeanLine renders "ns/op −7.2%  ·  B/op −31.0%  ·  allocs/op −28.4%".
func (a *App) geomeanLine(comparison *compare.Comparison) string {
	parts := []string{}
	for _, geomean := range comparison.Geomeans {
		if geomean.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", geomean.Metric, a.deltaText(geomean.DeltaPct)))
	}
	if len(parts) == 0 {
		return a.colorize(dimCode, "no comparable benchmarks")
	}
	return strings.Join(parts, separator)
}

// geomeanCount is the number of benchmarks behind the headline figure; ns/op is the
// primary metric, so its count is the headline count.
func geomeanCount(comparison *compare.Comparison) int {
	if geomean, ok := comparison.Geomean(model.MetricNsOp); ok && geomean.Count > 0 {
		return geomean.Count
	}
	for _, candidate := range comparison.Geomeans {
		if candidate.Count > 0 {
			return candidate.Count
		}
	}
	return 0
}

// classificationCounts renders "⚡ improved 9   🐢 regressed 1   ~ unchanged 30".
func (a *App) classificationCounts(comparison *compare.Comparison) string {
	order := []struct {
		classification compare.Classification
		mark           string
	}{
		{compare.Improved, markImproved},
		{compare.Regressed, markRegressed},
		{compare.Unchanged, markUnchanged},
		{compare.Insufficient, markInsufficient},
		{compare.New, markNew},
		{compare.Removed, markRemoved},
	}
	parts := []string{}
	for _, entry := range order {
		count := comparison.Counts[entry.classification]
		if count == 0 {
			continue
		}
		label := fmt.Sprintf("%s %s %d", entry.mark, entry.classification, count)
		parts = append(parts, a.colorize(classificationColor(entry.classification), label))
	}
	if len(parts) == 0 {
		return a.colorize(dimCode, "no benchmarks to compare")
	}
	return strings.Join(parts, "   ")
}

// classificationColor returns the ANSI code that matches a verdict.
func classificationColor(classification compare.Classification) string {
	switch classification {
	case compare.Improved:
		return greenCode
	case compare.Regressed:
		return redCode
	case compare.Insufficient, compare.Incomparable:
		return yellowCode
	default:
		return dimCode
	}
}

// printFindings lists the largest improvements and regressions.
func (a *App) printFindings(comparison *compare.Comparison) {
	improvements := comparison.Findings(compare.Improved, findingsShown)
	regressions := comparison.Findings(compare.Regressed, findingsShown)
	if len(improvements) > 0 {
		a.printf("   %s%s\n", a.colorize(dimCode, "top improvements:  "), a.findingLine(improvements, compare.Improved, comparison))
	}
	if len(regressions) > 0 {
		a.printf("   %s%s\n", a.colorize(dimCode, "regressions:       "), a.findingLine(regressions, compare.Regressed, comparison))
	}
	if len(improvements) == 0 && len(regressions) == 0 {
		a.printf("   %s\n", a.colorize(dimCode, "no significant changes beyond the noise floor"))
	}
}

// findingLine renders up to findingsShown findings, returning the ones it has
// actually printed so callers can report the exact set.
func (a *App) findingLine(findings []compare.Finding, classification compare.Classification, comparison *compare.Comparison) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		parts = append(parts, fmt.Sprintf("%s  %s %s (p=%s)",
			finding.Benchmark,
			finding.Result.Metric,
			a.deltaText(finding.Result.DeltaPct),
			render.FormatPValue(finding.Result.P)))
	}
	line := strings.Join(parts, "   ")
	if total := comparison.Counts[classification]; total > len(findings) {
		line += "   …"
	}
	return a.colorize(classificationColor(classification), line)
}

// deltaText renders a signed percentage in the colour that matches its direction,
// treating an undefined ratio as "n/a" rather than inventing a number.
func (a *App) deltaText(deltaPct float64) string {
	return render.FormatSignedPercent(deltaPct)
}

// printProvenanceNotes warns about the two traps that would otherwise waste an
// afternoon: rerunning the same commit, and changing machine or toolchain.
func (a *App) printProvenanceNotes(previous, current *model.Run) {
	notes := []string{}
	if previous.Provenance.GitSHA != "" && previous.Provenance.GitSHA == current.Provenance.GitSHA &&
		!previous.Provenance.GitDirty && !current.Provenance.GitDirty {
		notes = append(notes, fmt.Sprintf("same commit as %s, this is a rerun (differences are noise).", previous.ID))
	}
	if previous.Provenance.GoVersion != current.Provenance.GoVersion {
		notes = append(notes, fmt.Sprintf("Go version changed (%s → %s): re-measure the baseline before trusting deltas.",
			previous.Provenance.GoVersion, current.Provenance.GoVersion))
	}
	if previous.Provenance.HardwareFingerprint != current.Provenance.HardwareFingerprint {
		notes = append(notes, fmt.Sprintf("hardware changed since %s: percentages across machines are not comparable.", previous.ID))
	}
	if len(notes) == 0 {
		return
	}
	a.printf("\n")
	for _, note := range notes {
		a.printf("   %s %s\n", a.colorize(yellowCode, "⚠"), note)
	}
}

// printHint prints the actionable last line: promote, investigate, or nothing.
func (a *App) printHint(comparison *compare.Comparison, previous, current *model.Run) {
	improved := comparison.Counts[compare.Improved]
	regressed := comparison.Counts[compare.Regressed]
	switch {
	case regressed > 0:
		a.printf("\n⚠ Significant regression%s detected. Investigate before promoting:  benchbook compare %s current\n",
			pluralSuffix(regressed), previous.ID)
	case improved > 0:
		a.printf("\n💡 Significant improvement%s detected. Promote it:  benchbook milestone -m \"<what you changed>\"\n",
			pluralSuffix(improved))
	default:
		a.printf("\n%s\n", a.colorize(dimCode, "   no significant change vs "+previous.ID+" — keep iterating (｡•̀ᴗ-)✧"))
	}
	_ = current
}

// compareThresholds maps the configuration thresholds onto the compare package, so
// the CLI never constructs a threshold set of its own.
func compareThresholds(cfg *config.Config) compare.Options {
	return compare.Options{
		Alpha:        cfg.Stats.Alpha,
		MinEffectPct: cfg.Stats.MinEffectPct,
		MinSamples:   cfg.Stats.MinSamples,
	}
}
