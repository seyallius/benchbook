// Package render. render_test - golden tests for every generated page: the benchmark
// report, the profile report, the timeline (including its inline SVG chart) and the
// summary in all three formats. Set UPDATE_GOLDEN=1 to re-record the goldens.
package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// goldenCase is one page and the fixture state it renders.
type goldenCase struct {
	// name is the golden file name (without the .md suffix).
	name string
	// render produces the page.
	render func() string
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// assertGolden compares the rendered page with testdata/golden/<name>.md and, with
// UPDATE_GOLDEN=1, rewrites it instead.
func assertGolden(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".md")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `UPDATE_GOLDEN=1 go test ./internal/render/` to record it)", path, err)
	}
	if string(want) != content {
		t.Errorf("%s differs from the golden file.\n%s", path, firstDifference(string(want), content))
	}
}

// firstDifference describes the first differing line, which is far easier to read
// than two full pages side by side.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for index := 0; index < len(wantLines) || index < len(gotLines); index++ {
		var wantLine, gotLine string
		if index < len(wantLines) {
			wantLine = wantLines[index]
		}
		if index < len(gotLines) {
			gotLine = gotLines[index]
		}
		if wantLine != gotLine {
			return "first difference at line " + itoa(index+1) + ":\n  want: " + wantLine + "\n  got:  " + gotLine
		}
	}
	return "the files differ only in trailing content"
}

// goldenCases returns every golden case, ordered so a failure list is readable.
func goldenCases() []goldenCase {
	baseline := fixtureBaseline()
	improved := fixtureImproved()
	otherMachine := fixtureOtherMachine()
	current := fixtureCurrent()
	ctx := fixtureContext()
	options := ctx.Options

	return []goldenCase{
		{
			name: "benchmarks-milestone",
			render: func() string {
				return BenchmarksPage(ctx, improved, Reference{Label: "vs m001", Run: baseline}, options)
			},
		},
		{
			name: "benchmarks-current",
			render: func() string {
				return BenchmarksPage(ctx, current, Reference{Label: "vs last milestone", Run: otherMachine}, options)
			},
		},
		{
			name: "benchmarks-first-run",
			render: func() string {
				return BenchmarksPage(ctx, baseline, Reference{Label: "", Run: nil}, options)
			},
		},
		{
			name: "benchmarks-different-hardware",
			render: func() string {
				return BenchmarksPage(ctx, otherMachine, Reference{Label: "vs m002", Run: improved}, options)
			},
		},
		{
			name: "benchmarks-no-run",
			render: func() string {
				return BenchmarksPage(ctx, nil, Reference{}, options)
			},
		},
		{
			name: "profiling-milestone",
			render: func() string {
				return ProfilingPage(ctx, improved, baseline)
			},
		},
		{
			name: "profiling-current",
			render: func() string {
				return ProfilingPage(ctx, current, otherMachine)
			},
		},
		{
			name: "profiling-empty",
			render: func() string {
				return ProfilingPage(ctx, baseline, nil)
			},
		},
		{
			name: "timeline",
			render: func() string {
				return TimelinePage(ctx, []*model.Run{baseline, improved, otherMachine}, current)
			},
		},
		{
			name: "timeline-single-milestone",
			render: func() string {
				return TimelinePage(ctx, []*model.Run{baseline}, nil)
			},
		},
		{
			name: "timeline-empty",
			render: func() string {
				return TimelinePage(ctx, nil, nil)
			},
		},
	}
}

// summaryCases returns the summary goldens: the same comparison in three formats.
func summaryCases() []goldenCase {
	baseline := fixtureBaseline()
	improved := fixtureImproved()
	otherMachine := fixtureOtherMachine()
	current := fixtureCurrent()
	options := fixtureContext().Options

	renderSummary := func(comparison *compare.Comparison, format Format) string {
		content, err := Summary(comparison, format, 3)
		if err != nil {
			panic(err)
		}
		return content
	}
	return []goldenCase{
		{
			name:   "summary-text",
			render: func() string { return renderSummary(compare.Between(baseline, improved, options), FormatText) },
		},
		{
			name:   "summary-markdown",
			render: func() string { return renderSummary(compare.Between(baseline, improved, options), FormatMarkdown) },
		},
		{
			name:   "summary-json",
			render: func() string { return renderSummary(compare.Between(baseline, improved, options), FormatJSON) },
		},
		{
			name: "summary-current-regression",
			render: func() string {
				return renderSummary(compare.Between(otherMachine, current, options), FormatMarkdown)
			},
		},
		{
			name:   "summary-incomparable-text",
			render: func() string { return renderSummary(compare.Between(improved, otherMachine, options), FormatText) },
		},
	}
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestPagesMatchTheirGoldens is the regression net for the report wording and layout.
func TestPagesMatchTheirGoldens(t *testing.T) {
	for _, testCase := range goldenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			assertGolden(t, testCase.name, testCase.render())
		})
	}
}

// TestSummariesMatchTheirGoldens covers text, Markdown and JSON output.
func TestSummariesMatchTheirGoldens(t *testing.T) {
	for _, testCase := range summaryCases() {
		t.Run(testCase.name, func(t *testing.T) {
			assertGolden(t, testCase.name, testCase.render())
		})
	}
}

// TestEveryPageStartsWithTheGeneratedHeader is the contract that keeps a human from
// editing a file the next build will overwrite. It applies to pages and to the
// Markdown summary; the text and JSON summaries are console output, not files.
func TestEveryPageStartsWithTheGeneratedHeader(t *testing.T) {
	cases := goldenCases()
	cases = append(cases, summaryCases()...)
	for _, testCase := range cases {
		if strings.HasSuffix(testCase.name, "-text") || testCase.name == "summary-json" {
			if strings.HasPrefix(testCase.render(), GeneratedHeader) {
				t.Errorf("%s is console output, want no DO-NOT-EDIT header", testCase.name)
			}
			continue
		}
		content := testCase.render()
		if !strings.HasPrefix(content, GeneratedHeader) {
			t.Errorf("%s does not start with %q; it starts with %q", testCase.name, GeneratedHeader, firstLine(content))
		}
	}
}

// TestRenderingIsDeterministic renders every page twice: identical inputs must give
// byte-identical output, which is what makes the double-build test meaningful.
func TestRenderingIsDeterministic(t *testing.T) {
	cases := append(goldenCases(), summaryCases()...)
	for _, testCase := range cases {
		first := testCase.render()
		second := testCase.render()
		if first != second {
			t.Errorf("%s is not deterministic", testCase.name)
		}
	}
}

// TestChartsEscapesHostileNames covers benchmark names that contain markup: the SVG
// is inline in Markdown, so an unescaped name would inject HTML into the page.
func TestChartsEscapesHostileNames(t *testing.T) {
	hostile := `Benchmark<script>alert("x")</script>&"quote"'`
	chart := LineChart(Chart{
		Title:   `index of ` + hostile,
		XLabels: []string{"m001", `m002"`, "current"},
		Series: []ChartSeries{
			{Title: hostile, Points: []ChartPoint{{X: 0, Value: 100}, {X: 1, Value: 80}}},
			{Title: "safe", Points: []ChartPoint{{X: 0, Value: 100}, {X: 1, Value: 100}, {X: 2, Value: 95, Current: true}}},
		},
		YLabel: "index, m001 = 100",
		Breaks: []int{1},
	})
	if strings.Contains(chart, "<script") {
		t.Errorf("chart contains an unescaped <script tag:\n%s", chart)
	}
	if !strings.Contains(chart, "&lt;script&gt;") {
		t.Error("chart does not escape the angle brackets of the benchmark name")
	}
	if !strings.Contains(chart, "&amp;") {
		t.Error("chart does not escape the ampersand")
	}
	// The title also ends up in an attribute, where a raw quote would end the value.
	aria := attributeValue(chart, "aria-label")
	if aria == "" {
		t.Fatal("chart has no aria-label attribute")
	}
	if strings.Contains(aria, `"`) {
		t.Errorf("aria-label contains a raw quote: %q", aria)
	}
	if !strings.Contains(aria, "&quot;") {
		t.Errorf("aria-label does not escape the quote of the benchmark name: %q", aria)
	}
	if strings.Count(chart, "<svg") != 1 || !strings.Contains(chart, "</svg>") {
		t.Error("chart is not exactly one SVG element")
	}
}

// TestTablesEscapeCellContent keeps pipes and newlines in labels from breaking a
// Markdown table.
func TestTablesEscapeCellContent(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"a | b":            `a \| b`,
		"line\nbreak":      "line<br>break",
		"<b>bold</b>":      "&lt;b&gt;bold&lt;/b&gt;",
		"m002 &#124; pipe": "m002 &amp;#124; pipe",
	}
	// A backtick survives escaping: the fences around a code span are added by code()
	// and must reach the page intact.
	cases["`code`"] = "`code`"
	for input, want := range cases {
		if got := EscapeCell(input); got != want {
			t.Errorf("EscapeCell(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestTableKeepsInlineCodeFences is the regression test for a double-escaping bug: the
// table escapes every cell, so the backticks code() added around a value must survive
// that pass. Before the fix, a milestone page showed "\\`10e8a3cc\\`".
func TestTableKeepsInlineCodeFences(t *testing.T) {
	table := Table(tableIndentNone,
		[]string{"Field", "Value"},
		[][]string{{"Git commit", code("10e8a3cc")}})
	if !strings.Contains(table, "| Git commit | `10e8a3cc` |") {
		t.Errorf("the code span was damaged by cell escaping:\n%s", table)
	}
	if strings.Contains(table, "\\`") {
		t.Errorf("the table contains an escaped backtick:\n%s", table)
	}
}

// TestProfilingPageKeepsPPROFMetaVerbatim guards a real bug: the metadata block is a
// fenced code block, so escaping it would turn "cum <= 26.80ms" into "cum &lt;=".
func TestProfilingPageKeepsPPROFMetaVerbatim(t *testing.T) {
	run := fixtureImproved()
	run.Profiles[0].Meta = []string{
		"File: core.test",
		"Dropped 42 nodes (cum <= 26.80ms)",
		"Path with `backticks` and <angles>",
	}
	page := ProfilingPage(fixtureContext(), run, fixtureBaseline())
	if !strings.Contains(page, "(cum <= 26.80ms)") {
		t.Errorf("profile metadata was HTML-escaped:\n%s", section(page, "Dropped"))
	}
	if !strings.Contains(page, "````") {
		t.Error("profile metadata is not wrapped in a four-backtick fence")
	}
}

// TestTimelineMarksHardwareBreaks keeps the chart honest about the machine change.
func TestTimelineMarksHardwareBreaks(t *testing.T) {
	page := TimelinePage(fixtureContext(), []*model.Run{fixtureBaseline(), fixtureImproved(), fixtureOtherMachine()}, nil)
	if !strings.Contains(page, "hardware") {
		t.Error("the timeline does not mention the hardware change")
	}
	if !strings.Contains(page, "bb-chart-break") {
		t.Error("the chart has no break marker for the hardware change")
	}
}

// TestTimelineDotsTheCurrentRun keeps the "unpromoted run" visual cue.
func TestTimelineDotsTheCurrentRun(t *testing.T) {
	withCurrent := TimelinePage(fixtureContext(), []*model.Run{fixtureBaseline(), fixtureOtherMachine()}, fixtureCurrent())
	if !strings.Contains(withCurrent, ">current<") {
		t.Error("the chart has no slot labelled \"current\"")
	}
	if !strings.Contains(withCurrent, "bb-chart-current") {
		t.Error("the current run is not drawn as a dotted line")
	}
	withoutCurrent := TimelinePage(fixtureContext(), []*model.Run{fixtureBaseline(), fixtureOtherMachine()}, nil)
	if strings.Contains(withoutCurrent, ">current<") {
		t.Error("the chart shows a current slot although there is no current run")
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// firstLine returns the first line of a document, for failure messages.
func firstLine(content string) string {
	if index := strings.Index(content, "\n"); index >= 0 {
		return content[:index]
	}
	return content
}

// attributeValue returns the value of one attribute, for failure messages.
func attributeValue(markup, name string) string {
	needle := name + `="`
	index := strings.Index(markup, needle)
	if index < 0 {
		return ""
	}
	rest := markup[index+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// section returns the lines around the first occurrence of needle, for failure
// messages.
func section(content, needle string) string {
	index := strings.Index(content, needle)
	if index < 0 {
		return "(not found)"
	}
	start := index - 120
	if start < 0 {
		start = 0
	}
	end := index + 120
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}
