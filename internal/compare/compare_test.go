// Package compare. compare_test - tests for the comparison rules: what counts as
// improved, regressed, unchanged, insufficient, new, removed and incomparable, plus
// the geomean aggregate and the within-run variant table.
package compare

import (
	"math"
	"testing"
	"time"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// benchmarkSpec describes one benchmark in a fixture run: a name plus one value per
// metric, repeated with a fixed jitter to create samples.
type benchmarkSpec struct {
	// name is the raw benchmark name, including any sub-benchmark path.
	name string
	// nsOp is the per-operation time of every sample.
	nsOp float64
	// bytesOp and allocsOp are optional -benchmem columns; nil means "not reported".
	bytesOp  *float64
	allocsOp *float64
	// extra carries additional metric values, keyed by unit.
	extra map[string]float64
}

// fixtureRun builds a run with the given benchmarks, each repeated sampleCount
// times, and a hardware fingerprint.
func fixtureRun(id string, fingerprint string, sampleCount int, specs ...benchmarkSpec) *model.Run {
	run := model.NewCurrent(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), model.Provenance{
		GitSHA:              id + "sha",
		HardwareFingerprint: fingerprint,
		GoVersion:           "go1.22.5",
		GOOS:                "linux",
		GOARCH:              "amd64",
	})
	run.ID = id
	for _, spec := range specs {
		benchmark := model.Benchmark{
			ID:       "example.com/demo." + spec.name,
			Package:  "example.com/demo",
			Name:     spec.name,
			Workload: "",
			Variant:  model.DefaultVariant,
		}
		benchmark.Workload, benchmark.Variant = model.WorkloadVariant(spec.name)
		for index := 0; index < sampleCount; index++ {
			// A fixed +/-1% pattern: real benchmark samples never repeat exactly, and
			// distinct values are what route a comparison through the exact test. The
			// pattern is symmetric, so the median stays at the nominal value.
			factor := 1 + jitter(index)
			sample := model.Sample{NsOp: spec.nsOp * factor, Extra: map[string]float64{}}
			if spec.bytesOp != nil {
				value := *spec.bytesOp
				sample.BytesOp = &value
			}
			if spec.allocsOp != nil {
				value := *spec.allocsOp * factor
				sample.AllocsOp = &value
			}
			for unit, value := range spec.extra {
				sample.Extra[unit] = value * factor
			}
			benchmark.Samples = append(benchmark.Samples, sample)
		}
		run.Benchmarks = append(run.Benchmarks, benchmark)
	}
	return run.Normalized()
}

// jitter is the relative offset of a sample in a fixture run: an alternating,
// increasing pattern, so no two samples of a run are equal (distinct values are what
// route a comparison through the exact test) while the ratio between two runs stays
// exactly the ratio of their nominal values.
func jitter(index int) float64 {
	offset := 0.005 * float64(index+1)
	if index%2 == 1 {
		return -offset
	}
	return offset
}

// metricResult returns the comparison entry of one metric, failing the test when it
// is missing.
func metricResult(t *testing.T, comparison *Comparison, benchmarkID string, metric model.MetricID) MetricResult {
	t.Helper()
	for _, result := range comparison.Results {
		if result.ID != benchmarkID {
			continue
		}
		for _, metricEntry := range result.Metrics {
			if metricEntry.Metric == metric {
				return metricEntry
			}
		}
		t.Fatalf("benchmark %s has no %s entry (%v)", benchmarkID, metric, result.Metrics)
	}
	t.Fatalf("benchmark %s missing from the comparison", benchmarkID)
	return MetricResult{}
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestUnchangedWhenSamplesAreIdentical is the honest baseline: no difference, no
// verdict, and no claim of improvement.
func TestUnchangedWhenSamplesAreIdentical(t *testing.T) {
	bytesOp, allocsOp := 16.0, 1.0
	from := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100, bytesOp: &bytesOp, allocsOp: &allocsOp})
	to := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100, bytesOp: &bytesOp, allocsOp: &allocsOp})
	comparison := Between(from, to, Options{})
	result := metricResult(t, comparison, "example.com/demo.BenchmarkConcat", model.MetricNsOp)
	if result.Classification != Unchanged {
		t.Errorf("classification = %q, want %q (p = %v, delta = %v)", result.Classification, Unchanged, result.P, result.DeltaPct)
	}
	if comparison.Counts[Unchanged] != 1 || comparison.Counts[Improved] != 0 {
		t.Errorf("counts = %v, want one unchanged benchmark", comparison.Counts)
	}
}

// TestImprovedRequiresBothSignificanceAndEffect covers the two gates: a change that
// is real but tiny stays "unchanged" because the minimum effect is not met.
func TestImprovedRequiresBothSignificanceAndEffect(t *testing.T) {
	before := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkFast", nsOp: 130})
	after := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkFast", nsOp: 100})
	improved := metricResult(t, Between(before, after, Options{}), "example.com/demo.BenchmarkFast", model.MetricNsOp)
	if improved.Classification != Improved {
		t.Errorf("30%% faster = %q, want %q", improved.Classification, Improved)
	}
	if !improved.DeltaDefined || improved.DeltaPct >= 0 {
		t.Errorf("delta = %v (defined %v), want a negative number for an improvement", improved.DeltaPct, improved.DeltaDefined)
	}
	if !improved.Tested || improved.TestMethod != "exact" {
		t.Errorf("tested = %v method = %q, want true/exact", improved.Tested, improved.TestMethod)
	}

	tiny := fixtureRun("m003", "fp1", 6, benchmarkSpec{name: "BenchmarkFast", nsOp: 130.2})
	unchanged := metricResult(t, Between(before, tiny, Options{}), "example.com/demo.BenchmarkFast", model.MetricNsOp)
	if unchanged.Classification != Unchanged {
		t.Errorf("0.2%% change = %q, want %q (below the 1%% minimum effect)", unchanged.Classification, Unchanged)
	}
	if unchanged.P == 0 {
		t.Error("the test did not run for a below-threshold change; the verdict should still be based on a p-value")
	}
}

// TestRegressedIsTheMirrorOfImproved keeps the sign convention in one place.
func TestRegressedIsTheMirrorOfImproved(t *testing.T) {
	slow := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 130})
	fast := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	result := metricResult(t, Between(slow, fast, Options{}), "example.com/demo.BenchmarkConcat", model.MetricNsOp)
	if result.Classification != Improved {
		t.Fatalf("classification = %q, want %q", result.Classification, Improved)
	}
	reversed := metricResult(t, Between(fast, slow, Options{}), "example.com/demo.BenchmarkConcat", model.MetricNsOp)
	if reversed.Classification != Regressed {
		t.Errorf("classification = %q, want %q", reversed.Classification, Regressed)
	}
	if reversed.DeltaPct <= 0 {
		t.Errorf("delta = %v, want a positive number for a regression", reversed.DeltaPct)
	}
}

// TestInsufficientBelowFourSamples is the rule that keeps two-sample comparisons
// from producing confident nonsense.
func TestInsufficientBelowFourSamples(t *testing.T) {
	from := fixtureRun("m001", "fp1", 3, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	to := fixtureRun("m002", "fp1", 3, benchmarkSpec{name: "BenchmarkConcat", nsOp: 200})
	result := metricResult(t, Between(from, to, Options{}), "example.com/demo.BenchmarkConcat", model.MetricNsOp)
	if result.Classification != Insufficient {
		t.Errorf("classification = %q, want %q", result.Classification, Insufficient)
	}
	if result.Tested {
		t.Error("a test ran with three samples per side, want it refused")
	}
	if result.P != 0 {
		t.Errorf("p = %v, want 0 for an untested comparison", result.P)
	}
	if result.DeltaPct == 0 {
		t.Error("the descriptive statistics should still be reported (they are just not a verdict)")
	}
}

// TestNewAndRemovedBenchmarksAreClassified keeps a benchmark that only exists on one
// side from being compared against an empty sample set.
func TestNewAndRemovedBenchmarksAreClassified(t *testing.T) {
	from := fixtureRun("m001", "fp1", 6,
		benchmarkSpec{name: "BenchmarkKept", nsOp: 100},
		benchmarkSpec{name: "BenchmarkGone", nsOp: 50})
	to := fixtureRun("m002", "fp1", 6,
		benchmarkSpec{name: "BenchmarkKept", nsOp: 100},
		benchmarkSpec{name: "BenchmarkFresh", nsOp: 70})
	comparison := Between(from, to, Options{})
	if got := metricResult(t, comparison, "example.com/demo.BenchmarkFresh", model.MetricNsOp).Classification; got != New {
		t.Errorf("new benchmark = %q, want %q", got, New)
	}
	if got := metricResult(t, comparison, "example.com/demo.BenchmarkGone", model.MetricNsOp).Classification; got != Removed {
		t.Errorf("removed benchmark = %q, want %q", got, Removed)
	}
	if comparison.Counts[New] != 1 || comparison.Counts[Removed] != 1 {
		t.Errorf("counts = %v, want one new and one removed", comparison.Counts)
	}
	if result := metricResult(t, comparison, "example.com/demo.BenchmarkFresh", model.MetricNsOp); result.TestMethod != "" {
		t.Errorf("a benchmark that cannot be tested reported method %q", result.TestMethod)
	}
}

// TestZeroBaselineReportsAnAbsoluteChange covers the "0 allocs/op" case: the relative
// change is undefined and the comparison must say so instead of dividing by zero.
func TestZeroBaselineReportsAnAbsoluteChange(t *testing.T) {
	zero, three := 0.0, 3.0
	from := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkAlloc", nsOp: 100, allocsOp: &zero})
	to := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkAlloc", nsOp: 100, allocsOp: &three})
	result := metricResult(t, Between(from, to, Options{}), "example.com/demo.BenchmarkAlloc", model.MetricAllocsOp)
	if result.DeltaDefined {
		t.Error("DeltaDefined = true for a zero baseline, want false")
	}
	if result.Classification != Regressed {
		t.Errorf("classification = %q, want %q (allocating where the old code did not is a regression)", result.Classification, Regressed)
	}
	if result.FromMedian != 0 {
		t.Errorf("from median = %v, want 0", result.FromMedian)
	}
	if result.ToMedian < 2.9 || result.ToMedian > 3.1 {
		t.Errorf("to median = %v, want ~3 (populated from the sample summary)", result.ToMedian)
	}
}

// TestGeomeanExcludesUndefinedPairs keeps the headline aggregate honest: benchmarks
// whose baseline is zero cannot contribute a ratio.
func TestGeomeanExcludesUndefinedPairs(t *testing.T) {
	zero, three := 0.0, 3.0
	from := fixtureRun("m001", "fp1", 6,
		benchmarkSpec{name: "BenchmarkA", nsOp: 100, allocsOp: &zero},
		benchmarkSpec{name: "BenchmarkB", nsOp: 100})
	to := fixtureRun("m002", "fp1", 6,
		benchmarkSpec{name: "BenchmarkA", nsOp: 50, allocsOp: &three},
		benchmarkSpec{name: "BenchmarkB", nsOp: 50})
	geomean, ok := Between(from, to, Options{}).Geomean(model.MetricNsOp)
	if !ok {
		t.Fatal("no ns_op geomean")
	}
	if geomean.Count != 2 {
		t.Errorf("count = %d, want 2 benchmarks", geomean.Count)
	}
	if math.Abs(geomean.DeltaPct+50) > 1e-9 {
		t.Errorf("geomean delta = %v, want -50", geomean.DeltaPct)
	}
	if allocs, ok := Between(from, to, Options{}).Geomean(model.MetricAllocsOp); ok && allocs.Count != 0 {
		t.Errorf("allocations geomean counted %d benchmarks with a zero baseline, want 0", allocs.Count)
	}
}

// TestIncomparableHardwareIsRefused covers the provenance rule: runs from different
// machines are not silently compared.
func TestIncomparableHardwareIsRefused(t *testing.T) {
	from := fixtureRun("m001", "laptop", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	to := fixtureRun("m002", "server", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 50})
	comparison := Between(from, to, Options{})
	if !comparison.Incomparable {
		t.Fatal("comparison of two fingerprints is not flagged incomparable")
	}
	if comparison.IncomparableReason == "" {
		t.Error("no reason given for the incomparable verdict")
	}
	if comparison.Forced {
		t.Error("Forced = true without ForceIncomparable")
	}
	if result := metricResult(t, comparison, "example.com/demo.BenchmarkConcat", model.MetricNsOp); result.Classification != Incomparable {
		t.Errorf("classification = %q, want %q", result.Classification, Incomparable)
	}
	for _, geomean := range comparison.Geomeans {
		if geomean.Count != 0 {
			t.Errorf("geomean %q counted %d benchmarks for an incomparable pair, want 0", geomean.Metric, geomean.Count)
		}
	}

	forced := Between(from, to, Options{ForceIncomparable: true})
	if !forced.Forced {
		t.Error("Forced = false with ForceIncomparable, want true")
	}
	result := metricResult(t, forced, "example.com/demo.BenchmarkConcat", model.MetricNsOp)
	if result.Classification != Improved {
		t.Errorf("forced classification = %q, want %q", result.Classification, Improved)
	}
}

// TestForceDoesNotHideTheMismatch keeps the warning attached to a forced comparison.
func TestForceDoesNotHideTheMismatch(t *testing.T) {
	from := fixtureRun("m001", "laptop", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	to := fixtureRun("m002", "server", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 50})
	forced := Between(from, to, Options{ForceIncomparable: true})
	if !forced.Incomparable || forced.IncomparableReason == "" {
		t.Error("a forced comparison lost its incomparable flag or reason")
	}
}

// TestMissingFingerprintIsIncomparable covers runs recorded before a fingerprint was
// available: comparing them would be a guess.
func TestMissingFingerprintIsIncomparable(t *testing.T) {
	from := fixtureRun("m001", "", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	to := fixtureRun("m002", "server", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 50})
	if !Between(from, to, Options{}).Incomparable {
		t.Error("a run without a hardware fingerprint was compared anyway")
	}
}

// TestHigherIsBetterMetricsInvertTheVerdict covers throughput metrics, where a
// larger number is the improvement.
func TestHigherIsBetterMetricsInvertTheVerdict(t *testing.T) {
	from := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkServe", nsOp: 100, extra: map[string]float64{"MB/s": 500}})
	to := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkServe", nsOp: 100, extra: map[string]float64{"MB/s": 700}})
	result := metricResult(t, Between(from, to, Options{}), "example.com/demo.BenchmarkServe", "MB/s")
	if result.Classification != Improved {
		t.Errorf("classification = %q, want %q (more MB/s is better)", result.Classification, Improved)
	}
	if result.DeltaPct <= 0 {
		t.Errorf("delta = %v, want the raw percentage (positive for more throughput)", result.DeltaPct)
	}
}

// TestFindingsAreOrderedByMagnitude gives the report its "top 3" list.
func TestFindingsAreOrderedByMagnitude(t *testing.T) {
	from := fixtureRun("m001", "fp1", 6,
		benchmarkSpec{name: "BenchmarkSmall", nsOp: 100},
		benchmarkSpec{name: "BenchmarkBig", nsOp: 100})
	to := fixtureRun("m002", "fp1", 6,
		benchmarkSpec{name: "BenchmarkSmall", nsOp: 80},
		benchmarkSpec{name: "BenchmarkBig", nsOp: 20})
	findings := Between(from, to, Options{}).Findings(Improved, 2)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}
	if findings[0].ID != "example.com/demo.BenchmarkBig" {
		t.Errorf("first finding = %s, want the larger change (BenchmarkBig)", findings[0].ID)
	}
	if findings[1].ID != "example.com/demo.BenchmarkSmall" {
		t.Errorf("second finding = %s, want BenchmarkSmall", findings[1].ID)
	}
	if limited := Between(from, to, Options{}).Findings(Improved, 1); len(limited) != 1 {
		t.Errorf("limit ignored: got %d findings", len(limited))
	}
}

// TestSameCommitDetectsARerun lets the report say "this is a rerun, differences are
// noise" instead of asking the user to investigate.
func TestSameCommitDetectsARerun(t *testing.T) {
	from := fixtureRun("m001", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	sameCommit := *from
	sameCommit.ID = "m002"
	if !Between(from, &sameCommit, Options{}).SameCommit() {
		t.Error("SameCommit = false for two runs with the same git sha")
	}
	other := fixtureRun("m002", "fp1", 6, benchmarkSpec{name: "BenchmarkConcat", nsOp: 100})
	if Between(from, other, Options{}).SameCommit() {
		t.Error("SameCommit = true for runs from different commits")
	}
}

// TestZeroOptionValuesFallBackToDefaults keeps a caller that only sets Alpha from
// accidentally disabling the minimum effect.
func TestZeroOptionValuesFallBackToDefaults(t *testing.T) {
	resolved := withDefaults(Options{})
	if resolved.Alpha != DefaultAlpha {
		t.Errorf("alpha = %v, want %v", resolved.Alpha, DefaultAlpha)
	}
	if resolved.MinEffectPct != DefaultMinEffectPct {
		t.Errorf("min effect = %v, want %v: zero means unset, never \"report everything\"", resolved.MinEffectPct, DefaultMinEffectPct)
	}
	if resolved.MinSamples != DefaultMinSamples {
		t.Errorf("min samples = %d, want %d", resolved.MinSamples, DefaultMinSamples)
	}
}

// TestWithinRunComparesVariantsAgainstTheBaseline covers the per-workload table.
func TestWithinRunComparesVariantsAgainstTheBaseline(t *testing.T) {
	run := fixtureRun("current", "fp1", 6,
		benchmarkSpec{name: "BenchmarkFindUser/traditional", nsOp: 2000},
		benchmarkSpec{name: "BenchmarkFindUser/result", nsOp: 1800},
	)
	workloads := WithinRun(run, "traditional", Options{})
	if len(workloads) != 1 {
		t.Fatalf("workloads = %d, want 1", len(workloads))
	}
	comparison := workloads[0]
	if comparison.Workload != "FindUser" {
		t.Errorf("workload = %q, want FindUser", comparison.Workload)
	}
	if !comparison.HasBaseline {
		t.Fatal("HasBaseline = false although the baseline variant is present")
	}
	if len(comparison.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(comparison.Rows))
	}
	if comparison.Rows[0].Benchmark.Variant != "traditional" {
		t.Errorf("first row = %q, want the baseline first", comparison.Rows[0].Benchmark.Variant)
	}
	if !comparison.Rows[0].IsBaseline {
		t.Error("the baseline row is not marked")
	}
	if comparison.Rows[0].Comparison != nil {
		t.Error("the baseline row carries a comparison against itself, want none")
	}
	if comparison.Rows[1].Comparison == nil {
		t.Fatal("the non-baseline row has no comparison against the baseline")
	}
	result := comparison.Rows[1].Comparison.Metrics[0]
	if result.Classification != Improved {
		t.Errorf("traditional vs result = %q, want %q", result.Classification, Improved)
	}
}

// TestWithinRunWithoutBaselineOmitsTheColumn keeps a workload that has no baseline
// from being compared against a variant that happens to be first.
func TestWithinRunWithoutBaselineOmitsTheColumn(t *testing.T) {
	run := fixtureRun("current", "fp1", 6,
		benchmarkSpec{name: "BenchmarkOther/one", nsOp: 100},
		benchmarkSpec{name: "BenchmarkOther/two", nsOp: 200},
	)
	workloads := WithinRun(run, "traditional", Options{})
	if len(workloads) != 1 {
		t.Fatalf("workloads = %d, want 1", len(workloads))
	}
	if workloads[0].HasBaseline {
		t.Error("HasBaseline = true although the workload has no baseline variant")
	}
}

// TestWorkloadOfSelectsOneWorkload keeps the benchmark page able to render a single
// workload without rendering the others.
func TestWorkloadOfSelectsOneWorkload(t *testing.T) {
	run := fixtureRun("current", "fp1", 6,
		benchmarkSpec{name: "BenchmarkFindUser/traditional", nsOp: 2000},
		benchmarkSpec{name: "BenchmarkConcat", nsOp: 60},
	)
	workload := WorkloadOf(run, "FindUser", "traditional", Options{})
	if len(workload.Rows) != 1 || workload.Rows[0].Benchmark.Variant != "traditional" {
		t.Errorf("rows = %+v, want only the FindUser variants", workload.Rows)
	}
	empty := WorkloadOf(run, "Missing", "traditional", Options{})
	if len(empty.Rows) != 0 {
		t.Errorf("rows for a missing workload = %+v, want none", empty.Rows)
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //
