// Package pprof. pprof_test - tests for the `go tool pprof -top` parser: header
// capture, base-unit conversion for every unit family, and the parser error that
// names what a valid table looks like.
package pprof

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// cpuFixture is a realistic `pprof -top` CPU table.
const cpuFixture = `File: core.test
Build ID: dc925f80e0d3babca16318119e89212047ac9ef4
Type: cpu
Time: 2026-09-30 05:46:26 UTC
Duration: 9.39ms, Total samples = 10ms (106.45%)
Showing nodes accounting for 10ms, 100% of 10ms total
Dropped 42 nodes (cum <= 26.80ms)
      flat  flat%   sum%        cum   cum%
      10ms   100%   100%       10ms   100%  internal/runtime/atomic.(*Uint64).Add (inline)
   930ms 17.35% 17.35%      930ms 17.35%  memeqbody
      1.20s 22.10% 39.45%       2.40s 40.00%  example.com/demo/internal/core.FindUserResult
         0     0%   100%       10ms   100%  runtime.mallocgc
`

// spaceFixture is a heap profile in bytes, where pprof prints binary byte units.
const spaceFixture = `File: core.test
Type: alloc_space
Time: 2026-09-30 05:46:21 UTC
Showing nodes accounting for 236.83kB, 95.85% of 247.09kB total
Dropped 93 nodes (cum <= 1.24kB)
Showing top 8 nodes out of 50
      flat  flat%   sum%        cum   cum%
     180kB 72.85% 72.85%      180kB 72.85%  regexp.(*bitState).reset
   16.78kB  6.79% 79.64%    30.84kB 12.48%  example.com/demo/internal/core.users
`

// objectsFixture is a heap profile in object counts: a unit-less "flat" column must
// be read as a count, not as nanoseconds or bytes.
const objectsFixture = `File: core.test
Type: alloc_objects
Duration: 1.2s, Total samples = 500
      flat  flat%   sum%        cum   cum%
    1.20k 40.00% 40.00%     1.50k 50.00%  example.com/demo/internal/core.FindUser
       16  0.53% 40.53%        16  0.53%  internal/strconv.FormatInt
`

// ----------------------------------------- Public API ----------------------------------------- //

// TestParseCPUProfileConvertsToNanoseconds is the unit-contract test: milliseconds
// and seconds in the source become nanoseconds in the document.
func TestParseCPUProfileConvertsToNanoseconds(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindCPU, Target: model.ProfileTargetFull}, cpuFixture)
	if profile.Kind != model.ProfileKindCPU {
		t.Errorf("kind = %q, want %q", profile.Kind, model.ProfileKindCPU)
	}
	byName := indexRows(profile)
	row := byName["example.com/demo/internal/core.FindUserResult"]
	if row == nil {
		t.Fatalf("row missing, got %v", rowNames(profile))
	}
	if row.Flat != 1.2e9 {
		t.Errorf("flat = %v ns, want 1.2e9", row.Flat)
	}
	if row.Cum != 2.4e9 {
		t.Errorf("cum = %v ns, want 2.4e9", row.Cum)
	}
	if row.FlatPct != 22.10 || row.CumPct != 40.00 {
		t.Errorf("percentages = %v/%v, want 22.1/40", row.FlatPct, row.CumPct)
	}
	if memeq := byName["memeqbody"]; memeq == nil || memeq.Flat != 9.3e8 {
		t.Errorf("memeqbody flat = %v, want 9.3e8", memeq.Flat)
	}
}

// TestParseHeapProfileConvertsToBytes covers the byte family, including the binary
// multiples pprof uses (kB = 1024).
func TestParseHeapProfileConvertsToBytes(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindMemSpace}, spaceFixture)
	row := indexRows(profile)["example.com/demo/internal/core.users"]
	if row == nil {
		t.Fatalf("row missing, got %v", rowNames(profile))
	}
	if row.Flat != 16.78*1024 {
		t.Errorf("flat = %v bytes, want %v", row.Flat, 16.78*1024)
	}
	if row.Cum != 30.84*1024 {
		t.Errorf("cum = %v bytes, want %v", row.Cum, 30.84*1024)
	}
}

// TestParseObjectProfileTreatsBareNumbersAsCounts makes the kind decide the base
// unit, because pprof prints unit-less numbers for object counts.
func TestParseObjectProfileTreatsBareNumbersAsCounts(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindMemObjects}, objectsFixture)
	byName := indexRows(profile)
	if row := byName["internal/strconv.FormatInt"]; row == nil || row.Flat != 16 {
		t.Errorf("bare count row = %v, want flat 16", row)
	}
	if row := byName["example.com/demo/internal/core.FindUser"]; row == nil || row.Flat != 1200 {
		t.Errorf("1200 = %v, want 1200 (1.2k in counts is SI, not binary)", row)
	}
}

// TestParseKeepsMetaVerbatim lets a report show exactly what the profiler said,
// including the "Dropped ... (cum <= ...)" line that only makes sense untouched.
func TestParseKeepsMetaVerbatim(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindCPU}, cpuFixture)
	if len(profile.Meta) == 0 {
		t.Fatal("no metadata captured")
	}
	joined := strings.Join(profile.Meta, "\n")
	for _, want := range []string{"File: core.test", "Duration:", "Showing nodes accounting for", "Dropped 42 nodes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("metadata %q does not contain %q", joined, want)
		}
	}
	if strings.Contains(joined, "flat  flat%") {
		t.Errorf("metadata contains the column header row:\n%s", joined)
	}
}

// TestParseDefaultsTargetToTheFullSuite keeps a profile with no target readable: it
// belongs to the whole run.
func TestParseDefaultsTargetToTheFullSuite(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindCPU}, cpuFixture)
	if profile.Target != model.ProfileTargetFull {
		t.Errorf("target = %q, want %q", profile.Target, model.ProfileTargetFull)
	}
	scoped := parseFixture(t, Options{Kind: model.ProfileKindCPU, Target: "BenchmarkFindUser"}, cpuFixture)
	if scoped.Target != "BenchmarkFindUser" {
		t.Errorf("target = %q, want BenchmarkFindUser", scoped.Target)
	}
}

// TestParseSkipsNoiseRowsWithoutFailing covers the "Showing top N nodes out of M"
// line and blank lines, which must not be read as data rows.
func TestParseSkipsNoiseRowsWithoutFailing(t *testing.T) {
	profile := parseFixture(t, Options{Kind: model.ProfileKindCPU}, cpuFixture)
	if len(profile.Rows) != 4 {
		t.Errorf("rows = %d, want 4 (%v)", len(profile.Rows), rowNames(profile))
	}
}

// TestParseEmptyInputIsAnActionableError names the source and the expected shape.
func TestParseEmptyInputIsAnActionableError(t *testing.T) {
	_, err := New(Options{Source: "benchbook/raw/last-profile-cpu-full.txt", Kind: model.ProfileKindCPU}).
		Parse(strings.NewReader("File: core.test\nno table here\n"))
	if err == nil {
		t.Fatal("Parse accepted input without a table")
	}
	message := err.Error()
	for _, want := range []string{"benchbook/raw/last-profile-cpu-full.txt", "pprof -top"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
}

// TestParseRowRejectsUnknownUnits keeps a unit the schema cannot store from being
// written as a silent zero.
func TestParseRowRejectsUnknownUnits(t *testing.T) {
	parser := New(Options{Kind: model.ProfileKindCPU})
	if _, ok := parser.parseRow("12 parsecs 1% 2% 3 parsecs 4% some.Func"); ok {
		t.Error("parseRow accepted an unknown unit")
	}
	if _, ok := parser.parseRow("1.0s 1% 2% 3.0s 4%"); ok {
		t.Error("parseRow accepted a row without a symbol name")
	}
}

// TestParseRealCapturesFromTestdata parses the captures checked into testdata, so the
// parser is also exercised against output produced by the real toolchain.
func TestParseRealCapturesFromTestdata(t *testing.T) {
	cases := []struct {
		file string
		kind model.ProfileKind
	}{
		{file: "cpu-top.txt", kind: model.ProfileKindCPU},
		{file: "mem-space-top.txt", kind: model.ProfileKindMemSpace},
		{file: "mem-objects-top.txt", kind: model.ProfileKindMemObjects},
	}
	for _, testCase := range cases {
		t.Run(testCase.file, func(t *testing.T) {
			path := filepath.Join("testdata", testCase.file)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			profile, err := New(Options{Source: path, Kind: testCase.kind}).Parse(strings.NewReader(string(content)))
			if err != nil {
				t.Fatalf("Parse(%s): %v", path, err)
			}
			if len(profile.Rows) == 0 {
				t.Fatalf("Parse(%s) returned no rows", path)
			}
			for _, row := range profile.Rows {
				if row.Name == "" {
					t.Errorf("%s: a row has no symbol name", path)
				}
				if row.Flat < 0 || row.Cum < 0 {
					t.Errorf("%s: negative value in row %q", path, row.Name)
				}
			}
		})
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// parseFixture runs the parser over one fixture body.
func parseFixture(t *testing.T, options Options, body string) *model.Profile {
	t.Helper()
	parser := New(options)
	profile, err := parser.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return profile
}

// indexRows keys rows by symbol name.
func indexRows(profile *model.Profile) map[string]*model.ProfileRow {
	byName := make(map[string]*model.ProfileRow, len(profile.Rows))
	for index := range profile.Rows {
		byName[profile.Rows[index].Name] = &profile.Rows[index]
	}
	return byName
}

// rowNames lists the parsed symbol names, for failure messages.
func rowNames(profile *model.Profile) []string {
	out := make([]string, 0, len(profile.Rows))
	for _, row := range profile.Rows {
		out = append(out, row.Name)
	}
	return out
}
