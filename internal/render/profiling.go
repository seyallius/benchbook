// Package render. profiling - the profile report: one collapsed block per
// (kind, target) with its metadata and top-N table, plus flat-value deltas for the
// functions that appear in both this run and the previous milestone.
package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/stats"
)

// ---------------------------------------------- Types ----------------------------------------- //

// metaFence opens and closes the metadata block. Four backticks are used because a
// symbol name in pprof output could contain three.
const metaFence = "````"

// sanitizeFence makes a line incapable of closing the metadata fence early.
func sanitizeFence(line string) string {
	return strings.ReplaceAll(line, metaFence, "\u2019\u2019\u2019")
}

// Grouping rules of the profiling page.
const (
	// fullSuiteHeading labels the group holding whole-suite profiles.
	fullSuiteHeading = "Full Suite"
	// perBenchmarkHeading labels the group holding per-benchmark profiles.
	perBenchmarkHeading = "Per-Benchmark"
)

// ProfileKindTitles maps a kind onto its display title.
var ProfileKindTitles = map[model.ProfileKind]string{
	model.ProfileKindCPU:        "CPU",
	model.ProfileKindMemSpace:   "Heap — space",
	model.ProfileKindMemObjects: "Heap — objects",
}

// Direction of a profile value is always "lower is better": a hot function that got
// cooler is an improvement, in nanoseconds, bytes or objects alike.
const (
	// profileDeltaImproved marks a function that spends less than before.
	profileDeltaImproved = "⚡"
	// profileDeltaRegressed marks a function that spends more than before.
	profileDeltaRegressed = "🐢"
	// profileDeltaUnchanged marks a function whose change is below the noise floor.
	profileDeltaUnchanged = "~"
	// profileDeltaNew marks a function that did not appear in the reference profile.
	profileDeltaNew = "new"
	// profileDeltaFresh marks a similarity threshold under which a change is ignored.
	profileSimilarityFloor = 0.005
)

// ----------------------------------------- Public API ----------------------------------------- //

// ProfilingPage renders every profile of a run. The reference run supplies the
// flat-value deltas; pass a nil run to render without them.
func ProfilingPage(ctx Context, run, reference *model.Run) string {
	var builder strings.Builder
	builder.WriteString(GeneratedHeader + "\n\n")
	if run == nil {
		builder.WriteString("# Profiling\n\nNo run recorded yet. Record one with `benchbook run`.\n")
		return builder.String()
	}
	builder.WriteString(fmt.Sprintf("# Profiling — %s\n\n", EscapeText(runLabelText(run))))
	if len(run.Profiles) == 0 {
		builder.WriteString("No profiles recorded for this run. Capture them with `benchbook profile`.\n\n")
		builder.WriteString(HardwareBlock(run))
		return builder.String()
	}
	builder.WriteString("Values are printed in the unit of the profile: nanoseconds for CPU, bytes for heap space, object counts for heap objects.\n\n")

	full := []model.Profile{}
	perBenchmark := []model.Profile{}
	for _, profile := range run.Profiles {
		if profile.Target == model.ProfileTargetFull {
			full = append(full, profile)
		} else {
			perBenchmark = append(perBenchmark, profile)
		}
	}
	builder.WriteString(fmt.Sprintf("## %s\n\n", fullSuiteHeading))
	builder.WriteString(profileGroup(ctx, run, reference, full))
	builder.WriteString(fmt.Sprintf("\n## %s\n\n", perBenchmarkHeading))
	builder.WriteString(profileGroup(ctx, run, reference, perBenchmark))
	builder.WriteString(HardwareBlock(run))
	return builder.String()
}

// profileGroup renders every profile of one group.
func profileGroup(ctx Context, run, reference *model.Run, profiles []model.Profile) string {
	if len(profiles) == 0 {
		return "_none recorded_\n"
	}
	var builder strings.Builder
	for _, profile := range profiles {
		builder.WriteString(profileBlock(ctx, run, reference, profile))
		builder.WriteString("\n")
	}
	return builder.String()
}

// profileBlock renders one collapsed profile table.
func profileBlock(ctx Context, run, reference *model.Run, profile model.Profile) string {
	var builder strings.Builder
	title := fmt.Sprintf("%s — %s", profileKindTitle(profile.Kind), profile.Target)
	builder.WriteString(fmt.Sprintf("<details>\n<summary><strong>%s</strong> (%s)</summary>\n\n",
		EscapeText(title), EscapeText(Pluralize(len(profile.Rows), "row"))))
	if len(profile.Meta) > 0 {
		// A fenced block is literal, so the metadata goes in verbatim — including the
		// "<=" of pprof's "Dropped ... (cum <= 26.80ms)" line, which must not be
		// HTML-escaped. A four-backtick fence keeps a stray fence inside the data
		// from ending the block early.
		builder.WriteString(metaFence + "\n")
		for _, line := range profile.Meta {
			builder.WriteString(sanitizeFence(line) + "\n")
		}
		builder.WriteString(metaFence + "\n\n")
	}
	referenceProfile := model.FindProfile(reference, profile.Kind, profile.Target)
	rows := profileRows(ctx, profile, referenceProfile)
	builder.WriteString(Table(tableIndentNone,
		[]string{"Function", "flat", "flat%", "cum", "cum%", "vs previous"},
		rows))
	builder.WriteString("\n</details>\n")
	return builder.String()
}

// profileKindTitle renders the human title of a profile kind.
func profileKindTitle(kind model.ProfileKind) string {
	if title, ok := ProfileKindTitles[kind]; ok {
		return title
	}
	return kind
}

// profileRows renders the top-N rows of a profile along with the delta against the
// reference profile's flat value for the same function.
func profileRows(ctx Context, profile model.Profile, reference *model.Profile) [][]string {
	limit := ctx.ProfileTopOrDefault()
	referenceFlat := flatByName(reference)
	rows := make([][]string, 0, limit)
	for position, row := range profile.Rows {
		if position >= limit {
			break
		}
		rows = append(rows, []string{
			row.Name,
			formatProfileValue(profile.Kind, row.Flat),
			fmt.Sprintf("%.2f%%", row.FlatPct),
			formatProfileValue(profile.Kind, row.Cum),
			fmt.Sprintf("%.2f%%", row.CumPct),
			profileDelta(row.Flat, referenceFlat, row.Name),
		})
	}
	return rows
}

// flatByName indexes the flat value of a reference profile by function name.
func flatByName(reference *model.Profile) map[string]float64 {
	flats := map[string]float64{}
	if reference == nil {
		return flats
	}
	for _, row := range reference.Rows {
		flats[row.Name] = row.Flat
	}
	return flats
}

// profileDelta renders the change of a function's flat value against the reference
// profile. A function missing from the reference is "new"; a change below the
// similarity floor reads "~"; with no reference profile at all the cell is a dash.
func profileDelta(value float64, referenceFlat map[string]float64, name string) string {
	previous, present := referenceFlat[name]
	if !present {
		if len(referenceFlat) == 0 {
			return DashPlaceholder
		}
		return profileDeltaNew
	}
	deltaPct, defined := stats.PercentChange(previous, value)
	if !defined {
		return profileDeltaNew
	}
	if math.Abs(deltaPct) < profileSimilarityFloor*percentScale {
		return profileDeltaUnchanged
	}
	if deltaPct < 0 {
		return fmt.Sprintf("%s %.1f%%", profileDeltaImproved, math.Abs(deltaPct))
	}
	return fmt.Sprintf("%s +%.1f%%", profileDeltaRegressed, deltaPct)
}

// ProfileSummaryLine renders "cpu/full: 20 rows, 4.31 s flat" for compact digests.
func ProfileSummaryLine(profile model.Profile) string {
	total := 0.0
	for _, row := range profile.Rows {
		total += row.Flat
	}
	return fmt.Sprintf("%s/%s: %s, %s flat",
		profile.Kind, profile.Target, Pluralize(len(profile.Rows), "row"), formatProfileValue(profile.Kind, total))
}

// formatProfileValue humanises a base-unit value for the profile kind.
func formatProfileValue(kind model.ProfileKind, value float64) string {
	switch kind {
	case model.ProfileKindMemSpace:
		return HumanizeBytes(value)
	case model.ProfileKindMemObjects:
		return HumanizeCount(value)
	default:
		return HumanizeNs(value)
	}
}
