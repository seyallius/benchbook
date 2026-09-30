// Package gotest. gotest_test - tests for the `go test -bench` parser: samples,
// optional -benchmem columns, sub-benchmark variants, GOMAXPROCS suffixes and the
// legacy group-rule fallback.
package gotest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// fixture is the realistic captured `go test -bench` output the tests parse.
const fixture = `goos: linux
goarch: amd64
pkg: example.com/demo/internal/core
cpu: Test CPU
BenchmarkFindUser/traditional-8         697983              1884 ns/op               0 B/op          0 allocs/op
BenchmarkFindUser/result-8              623494              1524 ns/op               0 B/op          0 allocs/op
BenchmarkFindUser/traditional-8         701234              1870 ns/op               0 B/op          0 allocs/op
BenchmarkFindUser/result-8              640123              1501 ns/op               0 B/op          0 allocs/op
BenchmarkConcat-8                     21602530                66.3 ns/op            16 B/op          1 allocs/op
BenchmarkConcat-8                     21998887                65.1 ns/op            16 B/op          1 allocs/op
BenchmarkThroughput-8                    50000             42000 ns/op         512.30 MB/s
PASS
pkg: example.com/demo/internal/other
BenchmarkCacheMiss-8                     20000             95000 ns/op            1024 B/op         12 allocs/op
ok  	example.com/demo/internal/core	3.412s
`

// ----------------------------------------- Public API ----------------------------------------- //

// TestParseFixtureGroupsSubBenchmarks is the core happy path: two samples of the
// same variant collapse into one benchmark, and the variant is split on the slash.
func TestParseFixtureGroupsSubBenchmarks(t *testing.T) {
	benchmarks := parseFixture(t, Options{Source: "run.txt"})
	byID := indexBenchmarks(benchmarks)

	traditional := byID["example.com/demo/internal/core.BenchmarkFindUser/traditional"]
	if traditional == nil {
		t.Fatalf("BenchmarkFindUser/traditional missing, got %v", ids(benchmarks))
	}
	if len(traditional.Samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(traditional.Samples))
	}
	if traditional.Workload != "FindUser" || traditional.Variant != "traditional" {
		t.Errorf("workload/variant = %q/%q, want FindUser/traditional", traditional.Workload, traditional.Variant)
	}
	if traditional.Samples[0].NsOp != 1884 || traditional.Samples[1].NsOp != 1870 {
		t.Errorf("ns/op samples = %v, want [1884 1870]", []float64{traditional.Samples[0].NsOp, traditional.Samples[1].NsOp})
	}
	if traditional.Samples[0].Iterations != 697983 {
		t.Errorf("iterations = %d, want 697983", traditional.Samples[0].Iterations)
	}
}

// TestParseFixtureKeepsPackageContext checks the "pkg:" line is honoured, because
// two packages may define benchmarks with the same name.
func TestParseFixtureKeepsPackageContext(t *testing.T) {
	benchmarks := parseFixture(t, Options{Source: "run.txt"})
	byID := indexBenchmarks(benchmarks)
	cacheMiss := byID["example.com/demo/internal/other.BenchmarkCacheMiss"]
	if cacheMiss == nil {
		t.Fatalf("BenchmarkCacheMiss missing, got %v", ids(benchmarks))
	}
	if cacheMiss.Package != "example.com/demo/internal/other" {
		t.Errorf("package = %q, want example.com/demo/internal/other", cacheMiss.Package)
	}
	if cacheMiss.Samples[0].BytesOp == nil || *cacheMiss.Samples[0].BytesOp != 1024 {
		t.Errorf("bytes_op = %v, want 1024", cacheMiss.Samples[0].BytesOp)
	}
}

// TestParseFixtureReadsExtraMetrics covers throughput-style columns after the
// standard three.
func TestParseFixtureReadsExtraMetrics(t *testing.T) {
	benchmarks := parseFixture(t, Options{Source: "run.txt"})
	byID := indexBenchmarks(benchmarks)
	throughput := byID["example.com/demo/internal/core.BenchmarkThroughput"]
	if throughput == nil {
		t.Fatalf("BenchmarkThroughput missing, got %v", ids(benchmarks))
	}
	value, ok := throughput.Samples[0].Extra["MB/s"]
	if !ok || value != 512.30 {
		t.Errorf("extra MB/s = (%v, %v), want (512.3, true)", value, ok)
	}
	if throughput.Samples[0].BytesOp != nil {
		t.Error("bytes_op was filled although the line has no B/op column")
	}
}

// TestParseWithoutBenchmemLeavesAllocationMetricsNil keeps a run recorded without
// -benchmem from claiming zero allocations.
func TestParseWithoutBenchmemLeavesAllocationMetricsNil(t *testing.T) {
	input := "pkg: example.com/demo\nBenchmarkPlain-8  1000  250 ns/op\n"
	benchmarks := parseFixture(t, Options{Source: "run.txt"}, input)
	if len(benchmarks) != 1 {
		t.Fatalf("benchmarks = %d, want 1", len(benchmarks))
	}
	sample := benchmarks[0].Samples[0]
	if sample.BytesOp != nil || sample.AllocsOp != nil {
		t.Errorf("bytes/allocs = %v/%v, want nil/nil", sample.BytesOp, sample.AllocsOp)
	}
	if metrics := model.MetricsOf(benchmarks[0].Samples); len(metrics) != 1 || metrics[0] != model.MetricNsOp {
		t.Errorf("metrics = %v, want [ns_op]", metrics)
	}
}

// TestParseStripsGOMAXPROCSSuffix is the cross-machine rule: the "-8" suffix must not
// become part of the benchmark identity.
func TestParseStripsGOMAXPROCSSuffix(t *testing.T) {
	input := "pkg: example.com/demo\nBenchmarkConcat-16  1000  10 ns/op\nBenchmarkConcat-4  1000  11 ns/op\n"
	benchmarks := parseFixture(t, Options{Source: "run.txt"}, input)
	if len(benchmarks) != 1 {
		t.Fatalf("benchmarks = %d, want 1 (both suffixes belong to BenchmarkConcat)", len(benchmarks))
	}
	if len(benchmarks[0].Samples) != 2 {
		t.Errorf("samples = %d, want 2", len(benchmarks[0].Samples))
	}
}

// TestParseUsesOnePackageWhenTheInputHasNoPackageLines covers captured output that
// was filtered with grep, where "pkg:" never made it into the file.
func TestParseUsesOnePackageWhenTheInputHasNoPackageLines(t *testing.T) {
	input := "BenchmarkSolo-8  10  5 ns/op\n"
	benchmarks := parseFixture(t, Options{Source: "run.txt", DefaultPackage: "example.com/demo"}, input)
	if len(benchmarks) != 1 {
		t.Fatalf("benchmarks = %d, want 1", len(benchmarks))
	}
	if benchmarks[0].Package != "example.com/demo" {
		t.Errorf("package = %q, want example.com/demo", benchmarks[0].Package)
	}
	if benchmarks[0].ID != "example.com/demo.BenchmarkSolo" {
		t.Errorf("id = %q, want example.com/demo.BenchmarkSolo", benchmarks[0].ID)
	}
}

// TestParseIgnoresNoise keeps non-benchmark lines (warnings, build output, blank
// lines) from producing phantom benchmarks.
func TestParseIgnoresNoise(t *testing.T) {
	input := `# example.com/demo
warning: something happened
BenchmarkReal-8  10  5 ns/op

--- FAIL: TestSomething (0.00s)
    core_test.go:12: boom
FAIL
exit status 1
`
	benchmarks := parseFixture(t, Options{Source: "run.txt"}, input)
	if len(benchmarks) != 1 {
		t.Fatalf("benchmarks = %d, want 1 (%v)", len(benchmarks), ids(benchmarks))
	}
	if benchmarks[0].Name != "BenchmarkReal" {
		t.Errorf("name = %q, want BenchmarkReal", benchmarks[0].Name)
	}
}

// TestParseEmptyInputIsAnActionableError checks the message names the source and what
// was expected, instead of returning an empty run.
func TestParseEmptyInputIsAnActionableError(t *testing.T) {
	_, err := New(Options{Source: "benchbook/raw/last-run.txt"}).Parse(strings.NewReader("goos: linux\nPASS\n"))
	if err == nil {
		t.Fatal("Parse accepted input without benchmark lines")
	}
	message := err.Error()
	for _, want := range []string{"benchbook/raw/last-run.txt", "-bench"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
}

// TestGroupRulesAssignLegacyNamesToWorkloads covers the fallback for repositories
// that cannot use sub-benchmarks.
func TestGroupRulesAssignLegacyNamesToWorkloads(t *testing.T) {
	input := `pkg: example.com/demo
BenchmarkErrorHandlingCPUWithTry-8     1    10 ns/op
BenchmarkErrorHandlingCPUWithTry-8     1    12 ns/op
BenchmarkErrorHandlingCPUWithoutTry-8  1    30 ns/op
BenchmarkErrorHandlingCPUDB-8          1    40 ns/op
BenchmarkUnrelatedThing-8              1     1 ns/op
`
	options := Options{
		Source: "run.txt",
		GroupRules: []GroupRule{
			{Contains: "ErrorHandlingCPU", Exclude: "DB", Workload: "ErrorHandlingCPU"},
		},
	}
	benchmarks := parseFixture(t, options, input)
	byID := indexBenchmarks(benchmarks)

	withTry := byID["example.com/demo.BenchmarkErrorHandlingCPUWithTry"]
	if withTry == nil {
		t.Fatalf("grouped benchmark missing, got %v", ids(benchmarks))
	}
	if withTry.Workload != "ErrorHandlingCPU" {
		t.Errorf("workload = %q, want ErrorHandlingCPU", withTry.Workload)
	}
	if withTry.Variant != "WithTry" {
		t.Errorf("variant = %q, want WithTry", withTry.Variant)
	}
	if withTry.Name != "BenchmarkErrorHandlingCPUWithTry" {
		t.Errorf("name = %q, want the original name (the rule groups, it does not rename)", withTry.Name)
	}
	if len(withTry.Samples) != 2 {
		t.Errorf("samples = %d, want 2", len(withTry.Samples))
	}
	if excluded := byID["example.com/demo.BenchmarkErrorHandlingCPUDB"]; excluded == nil {
		t.Fatal("excluded benchmark disappeared")
	} else if excluded.Workload == "ErrorHandlingCPU" {
		t.Error("the exclude rule did not fire: BenchmarkErrorHandlingCPUDB was grouped")
	}
	if unrelated := byID["example.com/demo.BenchmarkUnrelatedThing"]; unrelated.Workload != "UnrelatedThing" {
		t.Errorf("unrelated workload = %q, want UnrelatedThing", unrelated.Workload)
	}
}

// TestGroupRuleMatchingIsWordBoundaryAware protects the bug the rules exist to
// avoid: "WithTry" must not match inside "WithoutTry".
func TestGroupRuleMatchingIsWordBoundaryAware(t *testing.T) {
	rule := GroupRule{Contains: "WithTry", Workload: "ErrorHandlingCPU"}
	if !ruleMatches(rule, "ErrorHandlingCPUWithTry") {
		t.Error("WithTry did not match ErrorHandlingCPUWithTry")
	}
	if ruleMatches(rule, "ErrorHandlingCPUWithoutTry") {
		t.Error("WithTry matched inside WithoutTry, want a word-boundary match")
	}
	if ruleMatches(rule, "Withdrawal") {
		t.Error("WithTry matched a name that only shares a prefix")
	}
	if got := variantFromRule(rule, "ErrorHandlingCPUWithTry"); got != "WithTry" {
		t.Errorf("variant = %q, want WithTry", got)
	}
}

// TestSplitWordsHandlesAcronymsAndDigits pins the word splitter used by the rules.
func TestSplitWordsHandlesAcronymsAndDigits(t *testing.T) {
	cases := []struct {
		name  string
		words string
	}{
		{name: "ErrorHandlingCPUWithTry", words: "error|handling|cpu|with|try"},
		{name: "DBQuery", words: "db|query"},
		{name: "JSONXMLParser", words: "jsonxml|parser"},
		{name: "Parse2Items", words: "parse2|items"},
		{name: "snake_case_name", words: "snake|case|name"},
		{name: "WithTry", words: "with|try"},
	}
	for _, testCase := range cases {
		if got := strings.Join(splitWords(testCase.name), "|"); got != testCase.words {
			t.Errorf("splitWords(%q) = %q, want %q", testCase.name, got, testCase.words)
		}
	}
}

// TestParseBenchmarkRequiresAMetricPair keeps a truncated or non-benchmark line from
// being read as a zero-cost benchmark.
func TestParseBenchmarkRequiresAMetricPair(t *testing.T) {
	cases := map[string]bool{
		"BenchmarkX-8  1000  250 ns/op":        true,
		"BenchmarkX-8  1000":                   false,
		"BenchmarkX  1000  250 ns/op  16 B/op": true,
		"=== RUN   TestX":                      false,
		"ok  example.com/demo  0.123s":         false,
		"BenchmarkX-8  1000  notanumber ns/op": false,
	}
	for line, want := range cases {
		if _, ok := parseBenchmarkLine(line); ok != want {
			t.Errorf("parseBenchmarkLine(%q) ok = %v, want %v", line, ok, want)
		}
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// parseFixture runs the parser over the shared fixture, or over an override input.
func parseFixture(t *testing.T, options Options, overrides ...string) []model.Benchmark {
	t.Helper()
	input := fixture
	if len(overrides) > 0 {
		input = overrides[0]
	}
	parser := New(options)
	if parser == nil {
		t.Fatal("New returned nil")
	}
	benchmarks, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return benchmarks
}

// indexBenchmarks keys benchmarks by id for readable assertions.
func indexBenchmarks(benchmarks []model.Benchmark) map[string]*model.Benchmark {
	byID := make(map[string]*model.Benchmark, len(benchmarks))
	for index := range benchmarks {
		byID[benchmarks[index].ID] = &benchmarks[index]
	}
	return byID
}

// ids lists the parsed benchmark ids, for failure messages.
func ids(benchmarks []model.Benchmark) []string {
	out := make([]string, 0, len(benchmarks))
	for _, benchmark := range benchmarks {
		out = append(out, benchmark.ID)
	}
	return out
}

// TestParseFixtureFromTestdataFileParsesRealCapture is a smoke test over the checked-in
// capture, so the fixture file and the inline fixture cannot drift apart silently.
func TestParseFixtureFromTestdataFileParsesRealCapture(t *testing.T) {
	path := filepath.Join("testdata", "bench.txt")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no captured fixture at %s: %v", path, err)
	}
	benchmarks, err := New(Options{Source: path}).Parse(strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	if len(benchmarks) == 0 {
		t.Fatalf("Parse(%s) returned no benchmarks", path)
	}
}
