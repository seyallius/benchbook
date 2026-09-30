// Package render. value - human-readable formatting of stored base units. The
// store keeps nanoseconds, bytes and counts; only this file decides how they look,
// which is why a data-format change never forces a re-run.
package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Unit suffixes and thresholds used when humanising time values.
const (
	// nanoSecondUnit is the shortest time unit.
	nanoSecondUnit = "ns"
	// microSecondUnit is 1000 ns.
	microSecondUnit = "µs"
	// milliSecondUnit is 1000 µs.
	milliSecondUnit = "ms"
	// secondUnit is 1000 ms.
	secondUnit = "s"
	// nsPerMicro is the number of nanoseconds in a microsecond.
	nsPerMicro = 1e3
	// nsPerMilli is the number of nanoseconds in a millisecond.
	nsPerMilli = 1e6
	// nsPerSecond is the number of nanoseconds in a second.
	nsPerSecond = 1e9
)

// Byte unit suffixes, using the binary multiples pprof itself prints.
const (
	// byteUnit is the shortest byte unit.
	byteUnit = " B"
	// kibiByteUnit is 1024 bytes.
	kibiByteUnit = " kB"
	// mebiByteUnit is 1024² bytes.
	mebiByteUnit = " MB"
	// gibiByteUnit is 1024³ bytes.
	gibiByteUnit = " GB"
	// kibi is the number of bytes in a kibibyte.
	kibi = 1024
)

// UnicodeMinusSign is the typographic minus used in every generated view. A plain
// hyphen in a table of numbers is easy to miss and hard to align.
const UnicodeMinusSign = "\u2212"

// PlusSign prefixes positive changes so a column of numbers reads as a column.
const PlusSign = "+"

// DashPlaceholder marks a cell that has no value, e.g. a missing metric.
const DashPlaceholder = "—"

// ----------------------------------------- Public API ----------------------------------------- //

// HumanizeNs renders a duration in nanoseconds using the largest unit that keeps
// the number readable: 15276 -> "15.3 µs", 1240000000 -> "1.24 s".
func HumanizeNs(nanoseconds float64) string {
	absolute := math.Abs(nanoseconds)
	switch {
	case absolute >= nsPerSecond:
		return formatSignificant(nanoseconds/nsPerSecond, 3) + " " + secondUnit
	case absolute >= nsPerMilli:
		return formatSignificant(nanoseconds/nsPerMilli, 3) + " " + milliSecondUnit
	case absolute >= nsPerMicro:
		return formatSignificant(nanoseconds/nsPerMicro, 3) + " " + microSecondUnit
	default:
		return formatSignificant(nanoseconds, 3) + " " + nanoSecondUnit
	}
}

// HumanizeBytes renders a byte count with binary multiples, matching the units
// pprof prints so a reader can compare the report with the raw profile.
func HumanizeBytes(bytes float64) string {
	absolute := math.Abs(bytes)
	switch {
	case absolute >= kibi*kibi*kibi:
		return formatSignificant(bytes/(kibi*kibi*kibi), 3) + gibiByteUnit
	case absolute >= kibi*kibi:
		return formatSignificant(bytes/(kibi*kibi), 3) + mebiByteUnit
	case absolute >= kibi:
		return formatSignificant(bytes/kibi, 3) + kibiByteUnit
	default:
		return formatSignificant(bytes, 3) + byteUnit
	}
}

// HumanizeCount renders an object count as an integer.
func HumanizeCount[Value integerValue](count Value) string {
	return strconv.FormatInt(int64(count), 10)
}

// integerValue is the constraint for values rendered as whole numbers.
type integerValue interface{ ~int | ~int64 | ~float64 }

// allocsSuffix renders the allocation unit, singular for exactly one allocation
// ("1 alloc" reads better than "1 allocs").
func allocsSuffix(value float64) string {
	if math.Round(value) == 1 {
		return " alloc"
	}
	return " allocs"
}

// FormatMetric renders a metric value in the unit the metric is measured in. The
// benchmark tables call it for every cell, which is what keeps ns/op in µs while
// B/op stays in bytes.
func FormatMetric(metric model.MetricID, value float64) string {
	switch metric {
	case model.MetricNsOp:
		return HumanizeNs(value)
	case model.MetricBytesOp:
		return strconv.FormatFloat(value, 'f', 0, 64) + byteUnit
	case model.MetricAllocsOp:
		// "1 allocs" reads badly, so the unit is pluralised like any other noun.
		return strconv.FormatFloat(value, 'f', 0, 64) + allocsSuffix(value)
	default:
		return formatSignificant(value, 4) + " " + string(metric)
	}
}

// FormatSignedPercent renders a signed percentage change with one decimal and the
// typographic minus, e.g. "−18.4%" or "+6.1%".
func FormatSignedPercent(deltaPct float64) string {
	if deltaPct == 0 {
		return "0.0%"
	}
	sign := PlusSign
	if deltaPct < 0 {
		sign = UnicodeMinusSign
	}
	return fmt.Sprintf("%s%.1f%%", sign, math.Abs(deltaPct))
}

// FormatSpread renders the descriptive spread as "±3.1%".
func FormatSpread(spread float64) string {
	return fmt.Sprintf("±%.1f%%", math.Abs(spread)*percentScale)
}

// percentScale converts a fraction into a percentage.
const percentScale = 100

// FormatPValue renders a p-value the way a reader expects: three decimals for
// ordinary values, scientific notation for very small ones.
func FormatPValue(p float64) string {
	switch {
	case p <= 0:
		return "0"
	case p < smallPValue:
		return fmt.Sprintf("%.1e", p)
	default:
		return fmt.Sprintf("%.3f", p)
	}
}

// smallPValue is the threshold below which scientific notation is clearer.
const smallPValue = 0.001

// FormatRatio renders a geomean ratio as a percentage change including the
// direction of the metric, e.g. "−23.4%".
func FormatRatio(deltaPct float64) string { return FormatSignedPercent(deltaPct) }

// ShortHash abbreviates a hash for display, keeping it recognisable.
func ShortHash(hash string, length int) string {
	if length <= 0 || len(hash) <= length {
		return hash
	}
	return hash[:length]
}

// Pluralize appends an "s" when count is not one.
func Pluralize(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

// JoinNonEmpty joins the non-empty parts with the given separator, which keeps
// comma lists free of dangling separators.
func JoinNonEmpty(separator string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, separator)
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// formatSignificant renders a float with the given number of significant digits,
// trimming trailing zeros so "15.30" prints as "15.3" but "15.0" keeps its point.
func formatSignificant(value float64, digits int) string {
	if value == 0 {
		return "0"
	}
	magnitude := math.Floor(math.Log10(math.Abs(value)))
	decimals := digits - 1 - int(magnitude)
	if decimals < 0 {
		decimals = 0
	}
	if decimals > maxDecimals {
		decimals = maxDecimals
	}
	rendered := strconv.FormatFloat(value, 'f', decimals, 64)
	if strings.Contains(rendered, ".") {
		rendered = strings.TrimRight(rendered, "0")
		rendered = strings.TrimSuffix(rendered, ".")
	}
	if rendered == "-0" {
		return "0"
	}
	return rendered
}

// maxDecimals caps the fractional digits so a tiny value cannot produce a wall of
// digits in a table cell.
const maxDecimals = 6
