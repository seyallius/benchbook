// Package model. model_test - tests for the canonical JSON encoding, the content
// hash, the decode rules (malformed input, newer schema versions) and the metric
// helpers that decide direction and ordering.
package model

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------- Types ----------------------------------------- //

// fixtureRun builds a run with two benchmarks in a deliberately "wrong" insertion
// order and an extra throughput metric, so the encoder has something to normalise.
func fixtureRun(createdAt time.Time) *Run {
	run := NewCurrent(createdAt, Provenance{
		GitSHA:    "0123456789abcdef0123456789abcdef01234567",
		GitBranch: "main",
		GoVersion: "go1.22.5",
		GOOS:      "linux",
		GOARCH:    "amd64",
		CPUModel:  "Test CPU",
		CPUCores:  8,
		BenchArgs: "-bench=. -count=6",
	})
	bytesOp, allocsOp := 16.0, 1.0
	run.Benchmarks = []Benchmark{
		{
			ID:      "example.com/demo/internal/zeta.BenchmarkZeta",
			Package: "example.com/demo/internal/zeta", Name: "BenchmarkZeta",
			Workload: "Zeta", Variant: DefaultVariant,
			Samples: []Sample{
				{Iterations: 100, NsOp: 100, BytesOp: &bytesOp, AllocsOp: &allocsOp},
				{Iterations: 120, NsOp: 102, BytesOp: &bytesOp, AllocsOp: &allocsOp, Extra: map[string]float64{"MB/s": 512.5}},
			},
		},
		{
			ID:      "example.com/demo/internal/alpha.BenchmarkAlpha",
			Package: "example.com/demo/internal/alpha", Name: "BenchmarkAlpha",
			Workload: "Alpha", Variant: DefaultVariant,
			Samples: []Sample{{Iterations: 10, NsOp: 10}},
		},
	}
	return run
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestCanonicalJSONIsStableUnderInsertionOrder is the promise the whole "JSON is the
// source of truth" design rests on: the same data always produces the same bytes.
func TestCanonicalJSONIsStableUnderInsertionOrder(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	first := fixtureRun(created)
	second := fixtureRun(created)
	second.Benchmarks[0], second.Benchmarks[1] = second.Benchmarks[1], second.Benchmarks[0]

	firstJSON, err := first.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	secondJSON, err := second.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Errorf("canonical JSON depends on insertion order:\n%s\n---\n%s", firstJSON, secondJSON)
	}
	firstHash, err := first.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	secondHash, err := second.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	if firstHash != secondHash {
		t.Errorf("content hash depends on insertion order: %s vs %s", firstHash, secondHash)
	}
	if !strings.HasSuffix(string(firstJSON), "\n") {
		t.Error("canonical JSON does not end with a newline")
	}
	if !json.Valid(firstJSON) {
		t.Error("canonical JSON is not valid JSON")
	}
	if bytes.Contains(firstJSON, []byte("  \t")) {
		t.Error("canonical JSON uses tabs, want the documented two-space indent")
	}
	if !bytes.Contains(firstJSON, []byte("\n  \"benchmarks\"")) {
		t.Errorf("canonical JSON is not indented with two spaces:\n%s", firstJSON)
	}
}

// TestContentHashIgnoresLabelAndNote keeps hand edits safe: editing a label or note
// must not invalidate the measurement hash.
func TestContentHashIgnoresLabelAndNote(t *testing.T) {
	run := fixtureRun(time.Now())
	before, err := run.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	run.Label = "a label a human typed"
	run.Note = "and a note"
	run.Provenance.GitSHA = "ffffffffffffffffffffffffffffffffffffffff"
	after, err := run.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	if before != after {
		t.Errorf("content hash changed after editing label/note/provenance: %s -> %s", before, after)
	}
}

// TestContentHashChangesWithMeasurements is the other half of the promise: a real
// data change must change the hash, otherwise a promotion could hide a regression.
func TestContentHashChangesWithMeasurements(t *testing.T) {
	run := fixtureRun(time.Now())
	before, err := run.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	run.Benchmarks[0].Samples[1].NsOp = 999
	after, err := run.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	if before == after {
		t.Error("content hash did not change when a sample changed")
	}
}

// TestDecodeRunRoundTrip checks that encode then decode returns the same data.
func TestDecodeRunRoundTrip(t *testing.T) {
	run := fixtureRun(time.Now())
	run.Label = "round trip"
	encoded, err := run.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	decoded, err := DecodeRun(bytes.NewReader(encoded), "test")
	if err != nil {
		t.Fatalf("DecodeRun: %v", err)
	}
	if decoded.Label != "round trip" {
		t.Errorf("label = %q, want %q", decoded.Label, "round trip")
	}
	if len(decoded.Benchmarks) != len(run.Benchmarks) {
		t.Fatalf("benchmarks = %d, want %d", len(decoded.Benchmarks), len(run.Benchmarks))
	}
	want, err := run.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	got, err := decoded.ComputeContentHash()
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	if got != want {
		t.Errorf("content hash after round trip = %s, want %s", got, want)
	}
	// DecodeRun normalises the run, so look the benchmark up by id rather than by
	// position: the sorted order is part of the contract, not an accident.
	zeta := FindBenchmark(decoded, "example.com/demo/internal/zeta.BenchmarkZeta")
	if zeta == nil {
		t.Fatalf("BenchmarkZeta missing after decode: %v", decoded.Benchmarks)
	}
	if value, ok := SampleValue(zeta.Samples[0], MetricBytesOp); !ok || value != 16 {
		t.Errorf("bytes_op after round trip = (%v, %v), want (16, true)", value, ok)
	}
	if value, ok := SampleValue(zeta.Samples[1], "MB/s"); !ok || value != 512.5 {
		t.Errorf("extra MB/s after round trip = (%v, %v), want (512.5, true)", value, ok)
	}
}

// TestDecodeRunRefusesNewerSchema is the forward-compatibility rule: a file written
// by a newer benchbook is refused with an upgrade hint, never half-read.
func TestDecodeRunRefusesNewerSchema(t *testing.T) {
	encoded, err := fixtureRun(time.Now()).CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	upgraded := bytes.Replace(encoded, []byte(`"schema_version": 1`), []byte(`"schema_version": 99`), 1)
	if bytes.Equal(upgraded, encoded) {
		t.Fatal("fixture did not contain schema_version 1")
	}
	_, err = DecodeRun(bytes.NewReader(upgraded), "future.json")
	if err == nil {
		t.Fatal("DecodeRun accepted schema_version 99")
	}
	message := err.Error()
	for _, want := range []string{"future.json", "99", "upgrade benchbook", "go install"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
}

// TestDecodeRunRejectsMalformedJSON makes the failure mode explicit instead of
// silently returning an empty run.
func TestDecodeRunRejectsMalformedJSON(t *testing.T) {
	if _, err := DecodeRun(strings.NewReader("{not json"), "broken.json"); err == nil {
		t.Fatal("DecodeRun accepted malformed JSON")
	}
}

// TestMigrateIsANoOpForTheCurrentSchema pins the seam: today every supported input
// passes through untouched, and an impossible version is an error rather than a
// silent acceptance.
func TestMigrateIsANoOpForTheCurrentSchema(t *testing.T) {
	run := fixtureRun(time.Now())
	migrated, err := Migrate(run, SchemaVersion, "current.json")
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if migrated != run {
		t.Error("Migrate copied the run, want the same pointer for the current schema")
	}
	if _, err := Migrate(run, SchemaVersion+1, "future.json"); err == nil {
		t.Error("Migrate accepted a version above SchemaVersion")
	}
	if _, err := Migrate(run, -1, "weird.json"); err == nil {
		t.Error("Migrate accepted a negative version")
	}
}

// TestSlugTurnsLabelsIntoFileNames covers milestone file naming.
func TestSlugTurnsLabelsIntoFileNames(t *testing.T) {
	cases := map[string]string{
		"Faster Concat + fewer allocs": "faster-concat-fewer-allocs",
		"  leading and trailing  ":     "leading-and-trailing",
		"Already-Slugged":              "already-slugged",
		"Ünïcøde and 123 numbers":      "n-c-de-and-123-numbers",
		"a/b\\c:d*e?f\"g<h>i|j":        "a-b-c-d-e-f-g-h-i-j",
		"":                             "",
		"----":                         "",
		"v1.2.3: strings.Join instead of a growing buffer (5x)": "v1-2-3-strings-join-instead-of-a-growing-buffer-5x",
	}
	for input, want := range cases {
		if got := Slug(input); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestNormalizeMetricResolvesAliases checks the spellings the CLI accepts.
func TestNormalizeMetricResolvesAliases(t *testing.T) {
	cases := map[string]MetricID{
		"ns/op":     MetricNsOp,
		"ns_op":     MetricNsOp,
		"B/op":      MetricBytesOp,
		"bytes_op":  MetricBytesOp,
		"allocs/op": MetricAllocsOp,
		"alloc/op":  MetricAllocsOp,
		// Extra metrics are addressed by their own unit, so the mapping is identity.
		"MB/s":           "MB/s",
		"custom-thing/s": "custom-thing/s",
	}
	for input, want := range cases {
		if got := NormalizeMetric(input); got != want {
			t.Errorf("NormalizeMetric(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestIsHigherBetter pins the metric direction rules, including the explicit
// allowlist and the conservative default for anything unknown.
func TestIsHigherBetter(t *testing.T) {
	cases := map[MetricID]bool{
		MetricNsOp:        false,
		MetricBytesOp:     false,
		MetricAllocsOp:    false,
		"MB/s":            true,
		"GB/s":            true,
		"ops/s":           true,
		"reqs/s":          true,
		"requests/s":      false,
		"custom-unit/s":   false,
		"MB":              false,
		"milliseconds/op": false,
	}
	for metric, want := range cases {
		if got := IsHigherBetter(metric); got != want {
			t.Errorf("IsHigherBetter(%q) = %v, want %v", metric, got, want)
		}
	}
}

// TestSampleValueDistinguishesMissingFromZero keeps a run recorded without
// -benchmem from looking like a benchmark that allocates nothing.
func TestSampleValueDistinguishesMissingFromZero(t *testing.T) {
	zero := 0.0
	sample := Sample{NsOp: 12, BytesOp: &zero}
	if value, ok := SampleValue(sample, MetricNsOp); !ok || value != 12 {
		t.Errorf("SampleValue(ns_op) = (%v, %v), want (12, true)", value, ok)
	}
	if value, ok := SampleValue(sample, MetricBytesOp); !ok || value != 0 {
		t.Errorf("SampleValue(bytes_op) = (%v, %v), want (0, true)", value, ok)
	}
	if _, ok := SampleValue(sample, MetricAllocsOp); ok {
		t.Error("SampleValue reported allocs_op for a run recorded without -benchmem")
	}
	if _, ok := SampleValue(sample, "GB/s"); ok {
		t.Error("SampleValue reported a throughput metric the sample does not carry")
	}
}

// TestSampleValuesSkipsMissingMetrics keeps medians computed over the samples that
// actually reported the metric.
func TestSampleValuesSkipsMissingMetrics(t *testing.T) {
	values := SampleValues([]Sample{{NsOp: 1}, {NsOp: 2, Extra: map[string]float64{"MB/s": 100}}}, "MB/s")
	if len(values) != 1 || values[0] != 100 {
		t.Errorf("SampleValues = %v, want [100]", values)
	}
	if values := SampleValues(nil, MetricNsOp); values == nil {
		t.Error("SampleValues returned nil, want an empty slice")
	}
}

// TestMetricsOfOrdersStandardMetricsFirst keeps tables readable and identical across
// runs: ns/op, B/op and allocs/op lead, extras follow alphabetically.
func TestMetricsOfOrdersStandardMetricsFirst(t *testing.T) {
	hitRate := 0.5
	samples := []Sample{{
		NsOp: 1, BytesOp: new(float64), AllocsOp: new(float64), Extra: map[string]float64{"zeta-unit": 1, "MB/s": 512},
	}, {
		NsOp: 1, Extra: map[string]float64{"hit-rate": hitRate},
	}}
	ordered := MetricsOf(samples)
	want := []MetricID{MetricNsOp, MetricBytesOp, MetricAllocsOp, "MB/s", "hit-rate", "zeta-unit"}
	if strings.Join(ordered, ",") != strings.Join(want, ",") {
		t.Errorf("MetricsOf = %v, want %v", ordered, want)
	}
	missing := MetricsOf([]Sample{{NsOp: 5}})
	if len(missing) != 1 || missing[0] != MetricNsOp {
		t.Errorf("MetricsOf(without -benchmem) = %v, want [ns_op]", missing)
	}
}

// TestWorkloadVariantSplitsOnTheLastSlash covers the sub-benchmark convention.
func TestWorkloadVariantSplitsOnTheLastSlash(t *testing.T) {
	cases := []struct {
		name     string
		workload string
		variant  string
	}{
		{name: "BenchmarkErrorHandlingCPU/with_try", workload: "ErrorHandlingCPU", variant: "with_try"},
		{name: "BenchmarkErrorHandlingCPU/with_try/nested", workload: "ErrorHandlingCPU/with_try", variant: "nested"},
		{name: "BenchmarkConcat", workload: "Concat", variant: DefaultVariant},
	}
	for _, testCase := range cases {
		workload, variant := WorkloadVariant(testCase.name)
		if workload != testCase.workload || variant != testCase.variant {
			t.Errorf("WorkloadVariant(%q) = (%q, %q), want (%q, %q)",
				testCase.name, workload, variant, testCase.workload, testCase.variant)
		}
	}
	if got := DisplayName("BenchmarkConcat"); got != "Concat" {
		t.Errorf("DisplayName = %q, want %q", got, "Concat")
	}
}

// TestWorkloadsAreSortedAndDeduplicated feeds the timeline, which must not depend on
// the order benchmarks happened to be parsed in.
func TestWorkloadsAreSortedAndDeduplicated(t *testing.T) {
	benchmarks := []Benchmark{
		{Workload: "Zeta"}, {Workload: "Alpha"}, {Workload: "Zeta"},
	}
	got := Workloads(benchmarks)
	if strings.Join(got, ",") != "Alpha,Zeta" {
		t.Errorf("Workloads = %v, want [Alpha Zeta]", got)
	}
}
