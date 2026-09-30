// Package compare. compare - turn two runs into claims. Every label it produces
// comes from stats (a significance test plus a minimum effect), never from a bare
// percentage, and every metric carries the direction in which "better" lies.
package compare

import (
	"fmt"
	"math"
	"sort"

	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/stats"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Classification is the verdict for one benchmark and one metric.
type Classification = string

// The complete set of verdicts. They are the only labels any renderer may show.
const (
	// Improved means the test was significant and the change is an improvement.
	Improved Classification = "improved"
	// Regressed means the test was significant and the change is a regression.
	Regressed Classification = "regressed"
	// Unchanged means the test ran and found nothing beyond noise.
	Unchanged Classification = "unchanged"
	// Insufficient means there were too few samples to test at all.
	Insufficient Classification = "insufficient"
	// New means the benchmark exists only in the newer run.
	New Classification = "new"
	// Removed means the benchmark exists only in the older run.
	Removed Classification = "removed"
	// Incomparable means the runs came from different hardware fingerprints.
	Incomparable Classification = "incomparable"
)

// Options are the thresholds and switches of a comparison.
type Options struct {
	// Alpha is the significance level of the two-sided U test.
	Alpha float64
	// MinEffectPct is the smallest relative change worth reporting.
	MinEffectPct float64
	// MinSamples is the smallest sample count worth testing.
	MinSamples int
	// ForceIncomparable computes the numbers even when the hardware fingerprints
	// differ. The comparison is still flagged, so a report can warn about it.
	ForceIncomparable bool
}

// MetricResult is the verdict for one metric of one benchmark.
type MetricResult struct {
	// Metric is the metric id ("ns_op" or an extra-metric unit such as "MB/s").
	Metric model.MetricID `json:"metric"`
	// Classification is the verdict.
	Classification Classification `json:"classification"`
	// FromMedian is the median on the older side.
	FromMedian float64 `json:"from_median"`
	// ToMedian is the median on the newer side.
	ToMedian float64 `json:"to_median"`
	// DeltaPct is the relative change; meaningless when DeltaDefined is false.
	DeltaPct float64 `json:"delta_pct"`
	// DeltaDefined is false when the older median is zero, which happens for the
	// common "0 allocs/op" benchmarks; reports show an absolute change instead.
	DeltaDefined bool `json:"delta_defined"`
	// P is the two-sided p-value, or 0 when no test was run.
	P float64 `json:"p"`
	// Tested reports whether the U test actually ran.
	Tested bool `json:"tested"`
	// TestMethod names the computation path ("exact" or "normal").
	TestMethod string `json:"test_method"`
	// FromSamples and ToSamples are the sample counts on each side.
	FromSamples int `json:"from_samples"`
	// ToSamples is the sample count on the newer side.
	ToSamples int `json:"to_samples"`
	// FromSummary and ToSummary are the descriptive statistics of each side.
	FromSummary stats.Summary `json:"from_summary"`
	// ToSummary is the descriptive summary of the newer side.
	ToSummary stats.Summary `json:"to_summary"`
}

// BenchmarkComparison is the full verdict for one benchmark id.
type BenchmarkComparison struct {
	// ID is the benchmark id.
	ID string `json:"id"`
	// Name is the benchmark name including sub-benchmark path.
	Name string `json:"name"`
	// DisplayName is the name without the "Benchmark" prefix, for reports.
	DisplayName string `json:"display_name"`
	// Workload is the workload the benchmark belongs to.
	Workload string `json:"workload"`
	// Variant is the variant of the workload.
	Variant string `json:"variant"`
	// Classification is the headline verdict, taken from the primary metric.
	Classification Classification `json:"classification"`
	// Metrics holds one entry per metric present on either side.
	Metrics []MetricResult `json:"metrics"`
	// Present contains one or neither side.
	Presence Presence `json:"presence"`
}

// Presence records which sides the benchmark exists on.
type Presence struct {
	// InFrom reports whether the older run has the benchmark.
	InFrom bool `json:"in_from"`
	// InTo reports whether the newer run has the benchmark.
	InTo bool `json:"in_to"`
}

// MetricGeomean is the headline aggregate for one metric.
type MetricGeomean struct {
	// Metric is the metric id.
	Metric model.MetricID `json:"metric"`
	// Ratio is the geometric mean of the per-benchmark median ratios (to/from).
	Ratio float64 `json:"ratio"`
	// DeltaPct is the ratio expressed as a percentage change.
	DeltaPct float64 `json:"delta_pct"`
	// Count is the number of benchmarks the geomean was computed over.
	Count int `json:"count"`
	// HigherIsBetter records the direction of the metric.
	HigherIsBetter bool `json:"higher_is_better"`
}

// Comparison is the result of comparing two runs.
type Comparison struct {
	// From is the older run.
	From *model.Run `json:"from"`
	// To is the newer run.
	To *model.Run `json:"to"`
	// Incomparable reports that the hardware fingerprints differ.
	Incomparable bool `json:"incomparable"`
	// IncomparableReason explains the mismatch in one line.
	IncomparableReason string `json:"incomparable_reason"`
	// Forced reports that an incomparable comparison was computed anyway.
	Forced bool `json:"forced"`
	// Results holds one entry per benchmark, sorted by id.
	Results []BenchmarkComparison `json:"results"`
	// Geomeans holds the headline aggregate per metric, in metric order.
	Geomeans []MetricGeomean `json:"geomeans"`
	// Counts is the number of benchmarks per headline classification.
	Counts map[Classification]int `json:"counts"`
}

// SameCommit reports whether both runs carry the same git SHA with clean trees,
// which makes the comparison a rerun rather than a change. The CLI uses it to say
// so instead of letting the user chase noise.
func (c *Comparison) SameCommit() bool {
	if c.From == nil || c.To == nil {
		return false
	}
	from, to := c.From.Provenance, c.To.Provenance
	if from.GitSHA == "" || to.GitSHA == "" {
		return false
	}
	return from.GitSHA == to.GitSHA && !from.GitDirty && !to.GitDirty
}

// Finding is a single notable metric change, used for the "top improvements" and
// "regressions" lines of the status report.
type Finding struct {
	// Benchmark is the display name of the affected benchmark.
	Benchmark string `json:"benchmark"`
	// ID is the benchmark id.
	ID string `json:"id"`
	// Result is the metric verdict.
	Result MetricResult `json:"result"`
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// DefaultAlpha, DefaultMinEffectPct and DefaultMinSamples mirror the config
// defaults; compare is also usable without a config (tests, JS parity checks).
const (
	// DefaultAlpha is the default significance level.
	DefaultAlpha = 0.05
	// DefaultMinEffectPct is the default minimum reported effect.
	DefaultMinEffectPct = 1.0
	// DefaultMinSamples is the default minimum sample count for promotion.
	DefaultMinSamples = 6
)

// withDefaults fills unset options with the documented defaults, so a zero Options
// behaves like a plain config file.
func withDefaults(options Options) Options {
	if options.Alpha <= 0 || options.Alpha >= 1 {
		options.Alpha = DefaultAlpha
	}
	if options.MinEffectPct <= 0 {
		options.MinEffectPct = DefaultMinEffectPct
	}
	if options.MinSamples < 1 {
		options.MinSamples = DefaultMinSamples
	}
	return options
}

// ----------------------------------------- Public API ----------------------------------------- //

// Between compares two runs and classifies every benchmark and metric. The older
// run is From, the newer one is To: a negative ns/op delta means the newer run is
// faster. When the hardware fingerprints differ the result is flagged and, unless
// forced, no percentages are computed at all — a percentage between two different
// machines is a lie with a number attached.
func Between(from, to *model.Run, options Options) *Comparison {
	options = withDefaults(options)
	comparison := &Comparison{
		From:   from,
		To:     to,
		Counts: map[Classification]int{},
	}
	comparison.Incomparable, comparison.IncomparableReason = incomparable(from, to)
	comparison.Forced = comparison.Incomparable && options.ForceIncomparable
	computeNumbers := !comparison.Incomparable || options.ForceIncomparable

	ids := unionBenchmarkIDs(from, to)
	comparison.Results = make([]BenchmarkComparison, 0, len(ids))
	for _, id := range ids {
		result := compareBenchmarks(model.FindBenchmark(from, id), model.FindBenchmark(to, id), options, computeNumbers)
		comparison.Results = append(comparison.Results, result)
		comparison.Counts[result.Classification]++
	}
	comparison.Geomeans = geomeans(comparison.Results, computeNumbers)
	return comparison
}

// incomparable reports whether two runs were produced on different hardware.
func incomparable(from, to *model.Run) (bool, string) {
	if from == nil || to == nil {
		return false, ""
	}
	fromFingerprint := from.Provenance.HardwareFingerprint
	toFingerprint := to.Provenance.HardwareFingerprint
	switch {
	case fromFingerprint == "" && toFingerprint == "":
		// Neither run recorded hardware: they are comparable with each other, which
		// is the whole point of `run --no-hw` (record on a machine you cannot probe).
		return false, ""
	case fromFingerprint == "":
		return true, fmt.Sprintf("%s has no hardware fingerprint: re-record it, or compare with %s", from.ID, forceHint)
	case toFingerprint == "":
		return true, fmt.Sprintf("%s has no hardware fingerprint: re-record it, or compare with %s", to.ID, forceHint)
	case fromFingerprint == toFingerprint:
		return false, ""
	}
	return true, fmt.Sprintf("hardware fingerprint %s → %s (%s %d× %s vs %s %d× %s)",
		short(fromFingerprint), short(toFingerprint),
		from.Provenance.GOOS, from.Provenance.CPUCores, from.Provenance.CPUModel,
		to.Provenance.GOOS, to.Provenance.CPUCores, to.Provenance.CPUModel)
}

// forceHint names the flag that computes an incomparable pair anyway.
const forceHint = "--force-incomparable"

// short abbreviates a fingerprint for display.
func short(fingerprint string) string {
	if len(fingerprint) <= shortHashChars {
		return fingerprint
	}
	return fingerprint[:shortHashChars]
}

// shortHashChars is the length of the abbreviated hash used in messages.
const shortHashChars = 8

// unionBenchmarkIDs returns every benchmark id present in either run, sorted.
func unionBenchmarkIDs(from, to *model.Run) []string {
	seen := map[string]bool{}
	for _, run := range []*model.Run{from, to} {
		if run == nil {
			continue
		}
		for _, benchmark := range run.Benchmarks {
			seen[benchmark.ID] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// compareBenchmarks classifies one benchmark. A missing side yields New or
// Removed; otherwise every metric of either side is evaluated.
func compareBenchmarks(from, to *model.Benchmark, options Options, computeNumbers bool) BenchmarkComparison {
	result := BenchmarkComparison{
		Presence: Presence{InFrom: from != nil, InTo: to != nil},
	}
	switch {
	case to != nil:
		result.ID, result.Name, result.Workload, result.Variant = to.ID, to.Name, to.Workload, to.Variant
	case from != nil:
		result.ID, result.Name, result.Workload, result.Variant = from.ID, from.Name, from.Workload, from.Variant
	}
	result.DisplayName = model.DisplayName(result.Name)
	switch {
	case from == nil:
		result.Classification = New
		result.Metrics = evaluateMissing(to.Samples, New)
		return result
	case to == nil:
		result.Classification = Removed
		result.Metrics = evaluateMissing(from.Samples, Removed)
		return result
	}
	if !computeNumbers {
		result.Classification = Incomparable
		result.Metrics = evaluateWithoutNumbers(from.Samples, to.Samples)
		return result
	}
	metrics := model.OrderedMetrics(metricsOfBoth(from.Samples, to.Samples))
	for _, metric := range metrics {
		result.Metrics = append(result.Metrics, evaluate(from.Samples, to.Samples, metric, options))
	}
	result.Classification = headline(result.Metrics)
	return result
}

// evaluateMissing builds the metric list for a benchmark that exists on one side
// only: there is nothing to test, and reporting percentages would be meaningless.
func evaluateMissing(samples []model.Sample, classification Classification) []MetricResult {
	metrics := model.MetricsOf(samples)
	results := make([]MetricResult, 0, len(metrics))
	for _, metric := range metrics {
		values := model.SampleValues(samples, metric)
		summary, err := stats.Summarize(values)
		if err != nil {
			continue
		}
		results = append(results, MetricResult{
			Metric:         metric,
			Classification: classification,
			FromSummary:    summary,
			ToSummary:      summary,
			FromSamples:    len(values),
			ToSamples:      len(values),
		})
	}
	return results
}

// evaluateWithoutNumbers builds the metric list for an incomparable pair: the
// medians are kept (they are facts) but no delta, p-value or classification is
// produced.
func evaluateWithoutNumbers(fromSamples, toSamples []model.Sample) []MetricResult {
	metrics := model.OrderedMetrics(metricsOfBoth(fromSamples, toSamples))
	results := make([]MetricResult, 0, len(metrics))
	for _, metric := range metrics {
		fromValues := model.SampleValues(fromSamples, metric)
		toValues := model.SampleValues(toSamples, metric)
		fromSummary, _ := stats.Summarize(fromValues)
		toSummary, _ := stats.Summarize(toValues)
		results = append(results, MetricResult{
			Metric:         metric,
			Classification: Incomparable,
			FromSummary:    fromSummary,
			ToSummary:      toSummary,
			FromSamples:    len(fromValues),
			ToSamples:      len(toValues),
		})
	}
	return results
}

// metricsOfBoth returns the metrics to evaluate: everything either side reported,
// so a metric that only the old run had still shows up as "removed".
func metricsOfBoth(fromSamples, toSamples []model.Sample) map[model.MetricID]bool {
	set := map[model.MetricID]bool{}
	for _, metric := range model.MetricsOf(fromSamples) {
		set[metric] = true
	}
	for _, metric := range model.MetricsOf(toSamples) {
		set[metric] = true
	}
	return set
}

// evaluate classifies one metric between two sample sets. This is the single place
// where a percentage becomes a claim, and it follows the same recipe every time:
// test first, then require a minimum effect, then apply the direction of the
// metric.
func evaluate(fromSamples, toSamples []model.Sample, metric model.MetricID, options Options) MetricResult {
	fromValues := model.SampleValues(fromSamples, metric)
	toValues := model.SampleValues(toSamples, metric)
	result := MetricResult{
		Metric:      metric,
		FromSamples: len(fromValues),
		ToSamples:   len(toValues),
	}
	if len(fromValues) == 0 {
		result.Classification = New
		result.ToSummary, _ = stats.Summarize(toValues)
		result.ToMedian = result.ToSummary.Median
		return result
	}
	if len(toValues) == 0 {
		result.Classification = Removed
		result.FromSummary, _ = stats.Summarize(fromValues)
		result.FromMedian = result.FromSummary.Median
		return result
	}
	result.FromSummary, _ = stats.Summarize(fromValues)
	result.ToSummary, _ = stats.Summarize(toValues)
	result.FromMedian = result.FromSummary.Median
	result.ToMedian = result.ToSummary.Median
	result.DeltaPct, result.DeltaDefined = stats.PercentChange(result.FromSummary.Median, result.ToSummary.Median)

	if len(fromValues) < stats.MinSamplesForSignificance || len(toValues) < stats.MinSamplesForSignificance {
		result.Classification = Insufficient
		return result
	}
	test, err := stats.MannWhitneyU(fromValues, toValues)
	if err != nil {
		// The only error is the insufficient-samples rule, already handled above.
		result.Classification = Insufficient
		return result
	}
	result.Tested = true
	result.P = test.P
	result.TestMethod = test.Method
	if test.P >= options.Alpha {
		result.Classification = Unchanged
		return result
	}
	if !result.DeltaDefined {
		// A zero median on the older side makes a ratio undefined, but the test
		// already proved the samples differ; the report will show "0 → 3 allocs".
		result.Classification = direction(metric, 1)
		return result
	}
	if math.Abs(result.DeltaPct) < options.MinEffectPct {
		result.Classification = Unchanged
		return result
	}
	result.Classification = direction(metric, result.DeltaPct)
	return result
}

// direction maps the sign of a change onto a verdict, honouring the metric's
// direction table: for MB/s a positive change is an improvement, for ns/op it is a
// regression.
func direction(metric model.MetricID, deltaPct float64) Classification {
	if deltaPct == 0 {
		return Unchanged
	}
	better := deltaPct < 0
	if model.IsHigherBetter(metric) {
		better = deltaPct > 0
	}
	if better {
		return Improved
	}
	return Regressed
}

// headline picks the benchmark-level verdict: the primary metric's verdict when
// present, otherwise the first metric that has one. ns/op is primary because a
// benchmark exists to measure time; allocation changes are reported per metric in
// the tables.
func headline(metrics []MetricResult) Classification {
	primary := primaryMetric()
	for _, metric := range metrics {
		if metric.Metric == primary && metric.Classification != "" {
			return metric.Classification
		}
	}
	for _, metric := range metrics {
		if metric.Classification != "" {
			return metric.Classification
		}
	}
	return Insufficient
}

// primaryMetric returns the metric that decides a benchmark's headline verdict.
func primaryMetric() model.MetricID { return model.MetricNsOp }

// geomeans computes the headline aggregate per metric. It includes every benchmark
// present on both sides with a non-zero median, significant or not (this is what
// benchstat does too), and reports how many benchmarks went into each figure.
func geomeans(results []BenchmarkComparison, computeNumbers bool) []MetricGeomean {
	byMetric := map[model.MetricID][]float64{}
	if computeNumbers {
		for _, result := range results {
			for _, metric := range result.Metrics {
				if !metric.DeltaDefined || metric.FromSummary.Median <= 0 || metric.ToSummary.Median <= 0 {
					continue
				}
				byMetric[metric.Metric] = append(byMetric[metric.Metric], metric.ToSummary.Median/metric.FromSummary.Median)
			}
		}
	}
	order := model.OrderedMetrics(metricSet(results, byMetric))
	out := make([]MetricGeomean, 0, len(order))
	for _, metric := range order {
		ratios := byMetric[metric]
		ratio, count := stats.Geomean(ratios)
		geomean := MetricGeomean{
			Metric:         metric,
			Count:          count,
			HigherIsBetter: model.IsHigherBetter(metric),
		}
		if count > 0 {
			geomean.Ratio = ratio
			geomean.DeltaPct = (ratio - 1) * 100
		}
		out = append(out, geomean)
	}
	return out
}

// metricSet collects every metric seen, whether or not it could be aggregated, so
// a metric that is always zero still appears in the report (with count 0).
func metricSet(results []BenchmarkComparison, aggregated map[model.MetricID][]float64) map[model.MetricID]bool {
	set := map[model.MetricID]bool{}
	for _, result := range results {
		for _, metric := range result.Metrics {
			set[metric.Metric] = true
		}
	}
	for metric := range aggregated {
		set[metric] = true
	}
	return set
}

// Findings returns the most notable changes with the given verdict, sorted by
// magnitude of change. Ties break on the benchmark id so the order is stable.
func (c *Comparison) Findings(classification Classification, limit int) []Finding {
	findings := []Finding{}
	for _, result := range c.Results {
		for _, metric := range result.Metrics {
			if metric.Classification != classification || !metric.DeltaDefined {
				continue
			}
			findings = append(findings, Finding{
				Benchmark: result.DisplayName,
				ID:        result.ID,
				Result:    metric,
			})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		left := math.Abs(findings[i].Result.DeltaPct)
		right := math.Abs(findings[j].Result.DeltaPct)
		if left != right {
			return left > right
		}
		return findings[i].ID < findings[j].ID
	})
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	return findings
}

// Geomean returns the aggregate for a metric, or false when that metric has no
// usable benchmarks.
func (c *Comparison) Geomean(metric model.MetricID) (MetricGeomean, bool) {
	for _, geomean := range c.Geomeans {
		if geomean.Metric == metric {
			return geomean, true
		}
	}
	return MetricGeomean{}, false
}

// BenchmarksWithClassification counts the benchmarks carrying a verdict; it is what
// the one-line summary of the status command prints.
func (c *Comparison) BenchmarksWithClassification(classification Classification) int {
	return c.Counts[classification]
}
