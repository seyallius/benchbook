// Package stats. stats - pure statistics with no I/O and no knowledge of
// benchmarks: descriptive summaries, the Mann-Whitney U test and the geometric
// mean. Every percentage benchbook reports comes from here, so this is the package
// that keeps the history honest.
package stats

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Summary is the descriptive statistics of one sample set.
type Summary struct {
	// N is the number of samples.
	N int `json:"n"`
	// Median is the middle value (mean of the two middle values for even N).
	Median float64 `json:"median"`
	// Min is the smallest value.
	Min float64 `json:"min"`
	// Max is the largest value.
	Max float64 `json:"max"`
	// Spread is the half-range divided by the median, i.e. the "±x%" a reader sees.
	Spread float64 `json:"spread"`
}

// UTESTResult is the outcome of a two-sample Mann-Whitney U test.
type UTESTResult struct {
	// U is the smaller of the two rank-sum statistics.
	U float64 `json:"u"`
	// P is the two-sided p-value.
	P float64 `json:"p"`
	// Significant reports whether P is below the caller's alpha. The test itself
	// does not know alpha, so this field is filled by the caller-facing helper.
	Significant bool `json:"significant"`
	// Method names the computation path ("exact" or "normal") so the result can be
	// reproduced and explained in a report.
	Method string `json:"method"`
}

// Methods reported in UTESTResult.Method.
const (
	// MethodExact is the exact rank-counting computation.
	MethodExact = "exact"
	// MethodNormal is the tie- and continuity-corrected normal approximation.
	MethodNormal = "normal"
)

// InsufficientSamplesError is returned when a comparison is asked for with sample
// counts so small that no p-value below alpha could ever be produced. Returning an
// error (instead of a p-value of 1) makes it impossible to accidentally claim
// significance from two samples.
type InsufficientSamplesError struct {
	// SamplesA is the sample count on the first side.
	SamplesA int
	// SamplesB is the sample count on the second side.
	SamplesB int
	// MinRequired is the smallest sample count for which significance is reachable.
	MinRequired int
}

// Error describes the shortfall and what to do about it.
func (e *InsufficientSamplesError) Error() string {
	return fmt.Sprintf("insufficient samples for a significance test (%d vs %d, need at least %d per side): re-run with --count %d",
		e.SamplesA, e.SamplesB, e.MinRequired, e.MinRequired)
}

// ErrEmptyInput is returned by the descriptive helpers when there is nothing to
// summarise. Callers should treat it as flow control, not as a crash.
var ErrEmptyInput = errors.New("no samples to summarise")

// ----------------------------------------- Public API ----------------------------------------- //

// Summarize computes the descriptive statistics of a sample set. The input slice is
// copied, never reordered in place, so callers keep their observation order.
func Summarize(values []float64) (Summary, error) {
	if len(values) == 0 {
		return Summary{}, ErrEmptyInput
	}
	sorted := sortedCopy(values)
	median := Median(sorted)
	summary := Summary{
		N:      len(sorted),
		Median: median,
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
	summary.Spread = HalfRangeOverMedian(summary.Min, summary.Max, median)
	return summary, nil
}

// Median returns the median of an already sorted slice. Even-length sets average
// the two middle values, matching statistics.Median in the standard library.
func Median(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// HalfRangeOverMedian returns (max-min)/2 divided by the median, the "±x%" figure
// used throughout the reports. A zero median has no meaningful relative spread, so
// the result is 0 rather than an infinity that would poison a table.
func HalfRangeOverMedian(min, max, median float64) float64 {
	if median == 0 {
		return 0
	}
	return ((max - min) / 2) / median
}

// Sorted returns a sorted copy of the input.
func Sorted(values []float64) []float64 { return sortedCopy(values) }

// sortedCopy returns a new ascending slice, leaving the input untouched.
func sortedCopy(values []float64) []float64 {
	out := make([]float64, len(values))
	copy(out, values)
	sort.Float64s(out)
	return out
}

// MinSamplesForSignificance is the smallest per-side sample count for which a
// two-sided p-value below 0.05 is reachable. With 4 samples the smallest possible
// exact two-sided p-value is 2/C(8,4) = 0.0286; with 3 it is 2/C(6,3) = 0.1. Any
// comparison below this must be reported as "insufficient", never as a result.
const MinSamplesForSignificance = 4

// ExactTestMaxSamples is the largest per-side sample count for which the exact
// distribution is computed instead of the normal approximation.
const ExactTestMaxSamples = 50

// MannWhitneyU runs a two-sided Mann-Whitney U test with a normal approximation
// path (tie corrected, continuity corrected). It returns an error when either side
// has fewer than MinSamplesForSignificance samples, because no p-value below alpha
// is reachable there and any number would be misleading.
func MannWhitneyU(a, b []float64) (UTESTResult, error) {
	if len(a) < MinSamplesForSignificance || len(b) < MinSamplesForSignificance {
		return UTESTResult{}, &InsufficientSamplesError{
			SamplesA:    len(a),
			SamplesB:    len(b),
			MinRequired: MinSamplesForSignificance,
		}
	}
	return mannWhitneyU(a, b)
}

// mannWhitneyU dispatches between the exact and the normal path. It assumes the
// sample-count precondition was already checked.
func mannWhitneyU(a, b []float64) (UTESTResult, error) {
	nA, nB := len(a), len(b)
	ranks, tieTerm := rankWithTies(a, b)
	sumA := 0.0
	for i := 0; i < nA; i++ {
		sumA += ranks[i]
	}
	uA := sumA - float64(nA*(nA+1))/2
	uB := float64(nA*nB) - uA
	u := math.Min(uA, uB)
	if nA <= ExactTestMaxSamples && nB <= ExactTestMaxSamples && tieTerm == 0 {
		p := exactTwoSidedP(nA, nB, u)
		return UTESTResult{U: u, P: clampProbability(p), Method: MethodExact}, nil
	}
	p := normalTwoSidedP(nA, nB, u, tieTerm)
	return UTESTResult{U: u, P: clampProbability(p), Method: MethodNormal}, nil
}

// rankWithTies ranks the pooled values ascending (ties receive the average rank)
// and returns the ranks belonging to the first group in original order, plus the
// tie-correction term sum(t^3 - t). A zero tie term means the pooled sample has no
// ties at all, which is the precondition for the exact distribution.
func rankWithTies(a, b []float64) ([]float64, float64) {
	type pooledItem struct {
		value  float64
		group  int
		origin int
	}
	pooled := make([]pooledItem, 0, len(a)+len(b))
	for i, value := range a {
		pooled = append(pooled, pooledItem{value: value, group: 0, origin: i})
	}
	for i, value := range b {
		pooled = append(pooled, pooledItem{value: value, group: 1, origin: i})
	}
	// Tie-breaking on the origin keeps the ranking a pure function of the input,
	// which the golden vectors and reproducibility tests rely on.
	sort.Slice(pooled, func(i, j int) bool {
		if pooled[i].value != pooled[j].value {
			return pooled[i].value < pooled[j].value
		}
		if pooled[i].group != pooled[j].group {
			return pooled[i].group < pooled[j].group
		}
		return pooled[i].origin < pooled[j].origin
	})

	ranksA := make([]float64, len(a))
	tieTerm := 0.0
	for start := 0; start < len(pooled); {
		end := start + 1
		for end < len(pooled) && pooled[end].value == pooled[start].value {
			end++
		}
		averageRank := float64(start+1+end) / 2
		if size := float64(end - start); size > 1 {
			tieTerm += size*size*size - size
		}
		for position := start; position < end; position++ {
			if pooled[position].group == 0 {
				ranksA[pooled[position].origin] = averageRank
			}
		}
		start = end
	}
	return ranksA, tieTerm
}

// exactTwoSidedP counts the rank configurations whose U is at most the observed U
// and returns their share of all C(nA+nB, nA) configurations, doubled for the
// two-sided test. It is the exact distribution and only valid without ties.
func exactTwoSidedP(nA, nB int, u float64) float64 {
	counts := uDistribution(nA, nB)
	total := 0.0
	limit := int(math.Round(u))
	for observed, ways := range counts {
		if observed <= limit {
			total += ways
		}
	}
	return 2 * total / combinations(nA+nB, nA)
}

// uDistribution returns the number of rank configurations for every U statistic
// from 0 to nA*nB, computed by dynamic programming over the recurrence
// P(nA,nB,u) = P(nA-1,nB,u-nB) + P(nA,nB-1,u).
func uDistribution(nA, nB int) []float64 {
	dp := make([][][]float64, nA+1)
	for i := range dp {
		dp[i] = make([][]float64, nB+1)
		for j := range dp[i] {
			dp[i][j] = make([]float64, nA*nB+1)
		}
	}
	for i := 0; i <= nA; i++ {
		dp[i][0][0] = 1
	}
	for j := 0; j <= nB; j++ {
		dp[0][j][0] = 1
	}
	for i := 1; i <= nA; i++ {
		for j := 1; j <= nB; j++ {
			for u := 0; u <= i*j; u++ {
				// The recurrence is P(i,j,u) = P(i-1,j,u-j) + P(i,j-1,u): adding the
				// i-th member of the first group shifts every existing configuration
				// by j (the size of the second group).
				count := dp[i][j-1][u]
				if u >= j {
					count += dp[i-1][j][u-j]
				}
				dp[i][j][u] = count
			}
		}
	}
	return dp[nA][nB]
}

// normalTwoSidedP applies the tie- and continuity-corrected normal approximation.
func normalTwoSidedP(nA, nB int, u float64, tieTerm float64) float64 {
	n := float64(nA + nB)
	mean := float64(nA*nB) / 2
	variance := (float64(nA*nB) / 12) * ((n + 1) - tieTerm/(n*(n-1)))
	if variance <= 0 {
		return 1
	}
	z := (math.Abs(u-mean) - 0.5) / math.Sqrt(variance)
	if z < 0 {
		z = 0
	}
	return 2 * standardNormalSurvival(z)
}

// standardNormalSurvival returns P(Z > z) for the standard normal distribution.
func standardNormalSurvival(z float64) float64 {
	if z < 0 {
		return 1 - standardNormalSurvival(-z)
	}
	return 0.5 * math.Erfc(z/math.Sqrt2)
}

// combinations returns the binomial coefficient C(n, k) as a float64.
func combinations(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	result := 1.0
	for i := 1; i <= k; i++ {
		result = result * float64(n-k+i) / float64(i)
	}
	return result
}

// clampProbability keeps a p-value inside [0, 1] against floating point drift.
func clampProbability(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// Geomean returns the geometric mean of the values, ignoring not-a-number and
// non-positive entries (a geometric mean of zero or of a negative ratio is
// undefined). It also returns how many values it actually used, so a report can
// print "over N benchmarks" with the honest count.
func Geomean(values []float64) (float64, int) {
	product := 0.0
	used := 0
	for _, value := range values {
		if math.IsNaN(value) || value <= 0 {
			continue
		}
		product += math.Log(value)
		used++
	}
	if used == 0 {
		return 0, 0
	}
	return math.Exp(product / float64(used)), used
}

// RelativeChange returns (to-from)/from*100, or 0 when the base is zero. It is the
// convenience form used by renderers that only need a number for a table cell;
// callers that must distinguish "no change" from "undefined" use PercentChange.
func RelativeChange(from, to float64) float64 {
	deltaPct, defined := PercentChange(from, to)
	if !defined {
		return 0
	}
	return deltaPct
}

// PercentChange returns (to-from)/from*100. It reports false when the change is
// undefined (zero base), which is the case for the common "0 B/op" benchmarks.
func PercentChange(from, to float64) (float64, bool) {
	if from == 0 {
		return 0, false
	}
	return (to - from) / from * 100, true
}
