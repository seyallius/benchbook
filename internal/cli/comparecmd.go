// Package cli. comparecmd - the `benchbook compare` command: two runs, one metric,
// a table of verdicts. It accepts milestone ids, "current", "latest" or a path to a
// run document, which makes it usable on a colleague's exported JSON.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the compare command.
const (
	// flagMetric selects the metric to compare.
	flagMetric = "metric"
	// flagForceIncomparable computes percentages across different hardware.
	flagForceIncomparable = "force-incomparable"
)

// Table headers of the comparison output.
var compareHeader = []string{"BENCHMARK", "FROM", "TO", "Δ", "P", "VERDICT"}

// compareOptions holds the parsed flags of `benchbook compare`.
type compareOptions struct {
	// metric is the metric the table is built around.
	metric string
	// force computes percentages even when the runs are not comparable.
	force bool
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newCompareCommand builds `benchbook compare`.
func (a *App) newCompareCommand() *cobra.Command {
	options := compareOptions{}
	cmd := &cobra.Command{
		Use:     cmdCompare + " <older> <newer>",
		GroupID: groupHistory,
		Short:   "Compare any two runs, cross-tabulated per benchmark",
		Long: `Compare two runs benchmark by benchmark for one metric, with the median, the number
of samples, the p-value and the verdict of the same Mann-Whitney U test the generated
pages use.

Either side may be "current" (the working run), "latest" (the newest milestone), a
milestone id such as m002, or a path to a JSON run document — which makes ad-hoc
comparisons possible without promoting anything.

When the two runs were measured on different hardware, percentages are suppressed and
the reason is printed. --force-incomparable shows them anyway, clearly marked.`,
		Example: `  benchbook compare m001 m002
  benchbook compare latest current --metric bytes_op
  benchbook compare m003 /tmp/other-machine.json`,
		Args: exactArgs(2, "two runs to compare, for example `benchbook compare m001 m002`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runCompare(options, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&options.metric, flagMetric, model.MetricNsOp, "metric to compare (ns_op, bytes_op, allocs_op or a unit such as MB/s)")
	cmd.Flags().BoolVar(&options.force, flagForceIncomparable, false, "compute percentages even when the hardware fingerprints differ")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runCompare compares two runs.
func (a *App) runCompare(options compareOptions, fromRef, toRef string) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	from, err := resolveRun(cfg, runs, fromRef, a.loadRunFile)
	if err != nil {
		return err
	}
	to, err := resolveRun(cfg, runs, toRef, a.loadRunFile)
	if err != nil {
		return err
	}
	thresholds := compareThresholds(cfg)
	thresholds.ForceIncomparable = options.force
	comparison := compare.Between(from, to, thresholds)
	selected := model.NormalizeMetric(model.MetricID(options.metric))
	a.printComparison(comparison, selected)
	return nil
}

// printComparison renders the comparison table and its headline.
func (a *App) printComparison(comparison *compare.Comparison, metric model.MetricID) {
	a.printf("📊 comparing %s → %s\n\n", runLabel(comparison.From), runLabel(comparison.To))
	if comparison.Incomparable {
		if comparison.Forced {
			a.warnf("forced view: %s", comparison.IncomparableReason)
		} else {
			a.warnf("not directly comparable: %s", comparison.IncomparableReason)
			a.info("re-run on the same machine, or force it with --force-incomparable")
		}
	}
	a.printf("   %s\n\n", a.geomeanLine(comparison))

	rows := [][]string{compareHeader}
	for _, result := range comparison.Results {
		found, ok := metricOf(result, metric)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			result.DisplayName,
			render.FormatMetric(metric, found.FromSummary.Median),
			render.FormatMetric(metric, found.ToSummary.Median),
			deltaCell(found),
			pCell(found),
			render.Badge(found),
		})
	}
	if len(rows) == 1 {
		a.printf("no benchmark reports %s in both runs\n", metric)
		return
	}
	for _, line := range renderColumns(rows) {
		a.printf("%s\n", line)
	}
	a.printf("\n   %s\n", a.classificationCounts(comparison))
	if metric != model.MetricNsOp {
		if geomean, ok := comparison.Geomean(metric); ok && geomean.Count > 0 {
			a.printf("   geomean over %s: %s\n", render.Pluralize(geomean.Count, "benchmark"), render.FormatSignedPercent(geomean.DeltaPct))
		}
	}
}

// metricOf finds one metric of a benchmark comparison.
func metricOf(result compare.BenchmarkComparison, metric model.MetricID) (compare.MetricResult, bool) {
	for _, candidate := range result.Metrics {
		if candidate.Metric == metric {
			return candidate, true
		}
	}
	return compare.MetricResult{}, false
}

// deltaCell renders the change of one metric, showing an absolute change when the
// baseline is zero and the ratio therefore undefined.
func deltaCell(result compare.MetricResult) string {
	if !result.DeltaDefined {
		return render.DashPlaceholder
	}
	return render.FormatSignedPercent(result.DeltaPct)
}

// pCell renders the p-value of one metric.
func pCell(result compare.MetricResult) string {
	if !result.Tested {
		return render.DashPlaceholder
	}
	return render.FormatPValue(result.P)
}

// loadRunFile reads a run document from an arbitrary path, so a comparison can
// include a run that belongs to another checkout.
func (a *App) loadRunFile(path string) (*model.Run, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, &usageError{err: fmt.Errorf("open %s: %w", path, err)}
	}
	defer file.Close()
	run, err := model.DecodeRun(file, path)
	if err != nil {
		return nil, err
	}
	return run, nil
}
