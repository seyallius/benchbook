// Package model. metric - metric selectors, units and the explicit
// "which direction is better" table that every comparison consults.
package model

import "strings"

// ---------------------------------------------- Types ----------------------------------------- //

// MetricID is a stable, storage-level identifier for a comparable metric. It is
// either one of the Metric* constants or the raw unit of an extra metric (e.g.
// "MB/s"), which keeps the mapping reversible without a registry.
type MetricID = string

// MetricAliases maps the human spellings users type on the command line onto the
// canonical MetricID values. Extra metrics are addressed by their own unit, so they
// need no alias.
var MetricAliases = map[string]MetricID{
	"ns_op":     MetricNsOp,
	"ns/op":     MetricNsOp,
	"bytes_op":  MetricBytesOp,
	"B/op":      MetricBytesOp,
	"alloc":     MetricAllocsOp,
	"alloc/op":  MetricAllocsOp,
	"allocs_op": MetricAllocsOp,
	"allocs/op": MetricAllocsOp,
}

// higherIsBetterUnits lists the extra-metric units for which a *larger* value is the
// better outcome. Everything not listed here is treated as "lower is better", which
// is the right default for time, bytes and allocation counts. Keeping this an
// explicit table means adding a throughput-style metric is a one-line, reviewable
// change rather than a heuristic.
var higherIsBetterUnits = map[string]bool{
	"B/s":     true,
	"kB/s":    true,
	"MB/s":    true,
	"GB/s":    true,
	"op/s":    true,
	"ops/s":   true,
	"req/s":   true,
	"reqs/s":  true,
	"item/s":  true,
	"items/s": true,
	"MiB/s":   true,
	"GiB/s":   true,
}

// MetricOrder is the deterministic order in which metrics are reported: the three
// standard ones first, then extras alphabetically. Comparison and rendering both
// consume this so two runs never disagree about table layout.
var MetricOrder = []MetricID{MetricNsOp, MetricBytesOp, MetricAllocsOp}

// ----------------------------------------- Public API ----------------------------------------- //

// NormalizeMetric resolves a user-supplied metric spelling to its canonical
// MetricID. An unknown value is returned trimmed and unchanged, which is how extra
// metrics (addressed by unit) pass through.
func NormalizeMetric(name string) MetricID {
	trimmed := strings.TrimSpace(name)
	if canonical, ok := MetricAliases[trimmed]; ok {
		return canonical
	}
	return trimmed
}

// IsHigherBetter reports whether a larger value of the given metric is the better
// outcome. Standard metrics are always "lower is better".
func IsHigherBetter(metric MetricID) bool {
	return higherIsBetterUnits[metric]
}

// SampleValue extracts the value of a metric from a sample. The second return
// value is false when the sample did not report that metric at all, which is
// different from reporting a zero.
func SampleValue(s Sample, metric MetricID) (float64, bool) {
	switch metric {
	case MetricNsOp:
		return s.NsOp, true
	case MetricBytesOp:
		if s.BytesOp == nil {
			return 0, false
		}
		return *s.BytesOp, true
	case MetricAllocsOp:
		if s.AllocsOp == nil {
			return 0, false
		}
		return *s.AllocsOp, true
	default:
		value, ok := s.Extra[metric]
		return value, ok
	}
}

// SampleValues returns the values of a metric across all samples, skipping samples
// that did not report it. The slice is always non-nil so callers can range freely.
func SampleValues(samples []Sample, metric MetricID) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if value, ok := SampleValue(sample, metric); ok {
			values = append(values, value)
		}
	}
	return values
}

// MetricsOf returns the union of metric ids present in the samples, in
// deterministic order (standard metrics first, then extras alphabetically).
func MetricsOf(samples []Sample) []MetricID {
	seen := make(map[MetricID]bool, len(MetricOrder))
	for _, sample := range samples {
		if _, ok := SampleValue(sample, MetricNsOp); ok {
			seen[MetricNsOp] = true
		}
		if sample.BytesOp != nil {
			seen[MetricBytesOp] = true
		}
		if sample.AllocsOp != nil {
			seen[MetricAllocsOp] = true
		}
		for unit := range sample.Extra {
			seen[unit] = true
		}
	}
	return OrderedMetrics(seen)
}

// OrderedMetrics converts a metric set into a deterministically ordered slice.
func OrderedMetrics(set map[MetricID]bool) []MetricID {
	out := make([]MetricID, 0, len(set))
	for _, metric := range MetricOrder {
		if set[metric] {
			out = append(out, metric)
			delete(set, metric)
		}
	}
	extras := make([]MetricID, 0, len(set))
	for metric := range set {
		extras = append(extras, metric)
	}
	sortMetrics(extras)
	return append(out, extras...)
}
