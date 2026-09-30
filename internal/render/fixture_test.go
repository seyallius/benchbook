// Package render. fixture_test - the run fixtures every renderer test shares: a
// baseline milestone, an improved milestone, a run recorded on other hardware and a
// current run with a regression, all with realistic jitter and a profile set.
package render

import (
	"strconv"
	"time"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// benchmarkShape describes one benchmark of a fixture run.
type benchmarkShape struct {
	// name is the raw benchmark name, including the sub-benchmark path.
	name string
	// nsOp is the nominal nanoseconds-per-op of the samples.
	nsOp float64
	// bytesOp and allocsOp are the -benchmem columns, applied to every sample.
	bytesOp  float64
	allocsOp float64
	// iterations is the reported loop count of the first sample.
	iterations int64
}

// fixtureRunOptions collects the knobs of fixtureRun.
type fixtureRunOptions struct {
	// id is empty for the current run and "mNNN" for a milestone.
	id string
	// label is the human label of the run.
	label string
	// created is the UTC timestamp of the run.
	created time.Time
	// sha is the git commit the run belongs to.
	sha string
	// fingerprint identifies the machine the run was recorded on.
	fingerprint string
	// goVersion is the toolchain used.
	goVersion string
	// cpuModel and cpuCores describe the machine.
	cpuModel string
	cpuCores int
	// benchmarks are the measured benchmarks.
	benchmarks []benchmarkShape
	// samples is the number of repetitions per benchmark.
	samples int
	// dirty and indicative mark the run's provenance flags.
	dirty      bool
	indicative bool
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// fixtureBaseline is the first milestone: four benchmarks, nothing to compare against.
func fixtureBaseline() *model.Run {
	return fixtureRun(fixtureRunOptions{
		id: "m001", label: "baseline", created: fixtureTime(1), sha: "1a2b3c4d5e6f",
		fingerprint: "aaaa1111bbbb2222", goVersion: "go1.22.5", cpuModel: "AMD Ryzen 9 5950X", cpuCores: 16,
		samples: 6,
		benchmarks: []benchmarkShape{
			{name: "BenchmarkConcat", nsOp: 120, bytesOp: 96, allocsOp: 3, iterations: 9_800_000},
			{name: "BenchmarkFindUser/traditional", nsOp: 2400, bytesOp: 0, allocsOp: 0, iterations: 520_000},
			{name: "BenchmarkFindUser/result", nsOp: 2150, bytesOp: 0, allocsOp: 0, iterations: 560_000},
			{name: "BenchmarkParseJSON", nsOp: 8400, bytesOp: 4096, allocsOp: 42, iterations: 140_000},
		},
	})
}

// fixtureImproved is the second milestone: much faster, but ParseJSON allocates twice
// as often, so the reports have both kinds of finding to show.
func fixtureImproved() *model.Run {
	run := fixtureRun(fixtureRunOptions{
		id: "m002", label: "sync.Pool for Concat + fewer copies", created: fixtureTime(2), sha: "2b3c4d5e6f7a",
		fingerprint: "aaaa1111bbbb2222", goVersion: "go1.22.5", cpuModel: "AMD Ryzen 9 5950X", cpuCores: 16,
		samples: 6,
		benchmarks: []benchmarkShape{
			{name: "BenchmarkConcat", nsOp: 68, bytesOp: 32, allocsOp: 1, iterations: 17_500_000},
			{name: "BenchmarkFindUser/traditional", nsOp: 2380, bytesOp: 0, allocsOp: 0, iterations: 530_000},
			{name: "BenchmarkFindUser/result", nsOp: 1180, bytesOp: 0, allocsOp: 0, iterations: 1_020_000},
			{name: "BenchmarkParseJSON", nsOp: 8420, bytesOp: 4160, allocsOp: 51, iterations: 139_000},
		},
	})
	run.Profiles = fixtureProfiles()
	return run
}

// fixtureOtherMachine is a milestone recorded on different hardware, which breaks the
// comparison chain and must be visible as such.
func fixtureOtherMachine() *model.Run {
	run := fixtureRun(fixtureRunOptions{
		id: "m003", label: "instrumentation build", created: fixtureTime(3), sha: "3c4d5e6f7a8b",
		fingerprint: "cccc3333dddd4444", goVersion: "go1.22.5", cpuModel: "Apple M2 Pro", cpuCores: 10,
		samples: 6,
		benchmarks: []benchmarkShape{
			{name: "BenchmarkConcat", nsOp: 41, bytesOp: 32, allocsOp: 1, iterations: 28_000_000},
			{name: "BenchmarkFindUser/traditional", nsOp: 1490, bytesOp: 0, allocsOp: 0, iterations: 800_000},
			{name: "BenchmarkFindUser/result", nsOp: 760, bytesOp: 0, allocsOp: 0, iterations: 1_600_000},
			{name: "BenchmarkParseJSON", nsOp: 5100, bytesOp: 4160, allocsOp: 51, iterations: 230_000},
		},
	})
	run.Profiles = fixtureProfilesOnOtherMachine()
	return run
}

// fixtureCurrent is the working run: a regression, an improvement, an unchanged
// benchmark, a new one and a dirty tree.
func fixtureCurrent() *model.Run {
	run := fixtureRun(fixtureRunOptions{
		id: "", label: "", created: fixtureTime(4), sha: "4d5e6f7a8b9c",
		fingerprint: "cccc3333dddd4444", goVersion: "go1.22.5", cpuModel: "Apple M2 Pro", cpuCores: 10,
		samples: 6, dirty: true,
		benchmarks: []benchmarkShape{
			{name: "BenchmarkConcat", nsOp: 40, bytesOp: 32, allocsOp: 1, iterations: 29_000_000},
			{name: "BenchmarkFindUser/traditional", nsOp: 1610, bytesOp: 0, allocsOp: 0, iterations: 740_000},
			{name: "BenchmarkFindUser/result", nsOp: 740, bytesOp: 0, allocsOp: 0, iterations: 1_650_000},
			{name: "BenchmarkParseJSON", nsOp: 5050, bytesOp: 4096, allocsOp: 48, iterations: 235_000},
			{name: "BenchmarkCacheMiss", nsOp: 92_000, bytesOp: 1024, allocsOp: 12, iterations: 13_000},
		},
	})
	run.Profiles = fixtureProfilesAfterImprovement()
	return run
}

// fixtureProfiles is a CPU profile plus a heap profile, the shape `profile` records.
func fixtureProfiles() []model.Profile {
	return []model.Profile{
		{
			Kind: model.ProfileKindCPU, Target: model.ProfileTargetFull,
			Meta: []string{
				"File: core.test",
				"Type: cpu",
				"Time: 2026-03-02 12:05:00 UTC",
				"Duration: 5.20s, Total samples = 5.10s (98.08%)",
				"Dropped 42 nodes (cum <= 25.50ms)",
			},
			Rows: []model.ProfileRow{
				{Flat: 1.20e9, FlatPct: 23.53, SumPct: 23.53, Cum: 2.40e9, CumPct: 47.06, Name: "example.com/demo/internal/core.FindUser"},
				{Flat: 9.30e8, FlatPct: 18.24, SumPct: 41.77, Cum: 9.30e8, CumPct: 18.24, Name: "memeqbody"},
				{Flat: 5.10e8, FlatPct: 10.00, SumPct: 51.77, Cum: 1.10e9, CumPct: 21.57, Name: "runtime.memequal"},
				{Flat: 3.30e8, FlatPct: 6.47, SumPct: 58.24, Cum: 4.30e8, CumPct: 8.43, Name: "strings.Join"},
			},
		},
		{
			Kind: model.ProfileKindMemSpace, Target: model.ProfileTargetFull,
			Meta: []string{
				"File: core.test",
				"Type: alloc_space",
				"Time: 2026-03-02 12:05:00 UTC",
				"Showing nodes accounting for 236.83kB, 95.85% of 247.09kB total",
				"Dropped 93 nodes (cum <= 1.24kB)",
			},
			Rows: []model.ProfileRow{
				{Flat: 184.32e3, FlatPct: 72.85, SumPct: 72.85, Cum: 184.32e3, CumPct: 72.85, Name: "regexp.(*bitState).reset"},
				{Flat: 17.18e3, FlatPct: 6.79, SumPct: 79.64, Cum: 31.58e3, CumPct: 12.48, Name: "example.com/demo/internal/core.users"},
				{Flat: 14.40e3, FlatPct: 5.69, SumPct: 85.33, Cum: 14.40e3, CumPct: 5.69, Name: "internal/strconv.FormatInt"},
			},
		},
	}
}

// fixtureProfilesOnOtherMachine changes the numbers, so profile deltas are visible.
func fixtureProfilesOnOtherMachine() []model.Profile {
	profiles := fixtureProfiles()
	profiles[0].Rows[0].Flat = 1.40e9
	profiles[0].Rows[0].FlatPct = 26.42
	profiles[0].Rows[1].Flat = 9.10e8
	profiles[0].Rows[3].Flat = 2.10e8
	profiles[1].Rows[1].Flat = 22.40e3
	return profiles
}

// fixtureProfilesAfterImprovement is a profile set where most rows got cheaper and one
// row is new.
func fixtureProfilesAfterImprovement() []model.Profile {
	profiles := fixtureProfiles()
	profiles[0].Rows[0].Flat = 1.05e9
	profiles[0].Rows[1].Flat = 8.90e8
	profiles[0].Rows[3].Flat = 2.60e8
	profiles[0].Rows = append(profiles[0].Rows, model.ProfileRow{
		Flat: 1.20e8, FlatPct: 2.35, SumPct: 60.59, Cum: 1.20e8, CumPct: 2.35,
		Name: "runtime.newobject",
	})
	return profiles
}

// fixtureTime returns a fixed timestamp, so golden files never contain the clock.
func fixtureTime(day int) time.Time {
	return time.Date(2026, 3, day, 12, 0, 0, 0, time.UTC)
}

// fixtureContext is the render context matching the fixture configuration.
func fixtureContext() Context {
	return Context{
		ProjectName:     "demo",
		BaselineVariant: "traditional",
		Categories: []Category{
			{Title: "⚡ Basics", Match: []string{"Concat", "Parse"}},
			{Title: "🔎 Lookup", Match: []string{"Find"}},
		},
		DefaultCategory: Category{Title: "🧪 Other"},
		Tracked:         []string{"Concat", "FindUser/result", "ParseJSON"},
		MaxTracked:      4,
		ProfileTop:      10,
		NoiseFloorNote:  true,
		Options:         compare.Options{Alpha: 0.05, MinEffectPct: 1.0, MinSamples: 6},
	}
}

// fixtureRun builds a run from the options, jittering every sample by a fixed
// alternating pattern so that no two samples are equal.
func fixtureRun(options fixtureRunOptions) *model.Run {
	run := model.NewCurrent(options.created, model.Provenance{
		GitSHA: options.sha, GitBranch: "main", GitDirty: options.dirty,
		GoVersion: options.goVersion, GOOS: "linux", GOARCH: "amd64",
		CPUModel: options.cpuModel, CPUCores: options.cpuCores, MemoryBytes: 64 << 30,
		HardwareFingerprint: options.fingerprint,
		BenchArgs:           "-run ^$ -bench . -benchmem -count " + strconv.Itoa(options.samples),
	})
	run.ID = options.id
	run.Label = options.label
	run.IndicativeOnly = options.indicative
	for _, shape := range options.benchmarks {
		benchmark := model.Benchmark{
			ID:      "example.com/demo/internal/core." + shape.name,
			Package: "example.com/demo/internal/core",
			Name:    shape.name,
		}
		benchmark.Workload, benchmark.Variant = model.WorkloadVariant(shape.name)
		for index := 0; index < options.samples; index++ {
			factor := 1 + 0.004*float64(index+1)*alternatingSign(index)
			bytesOp := shape.bytesOp
			allocsOp := shape.allocsOp
			benchmark.Samples = append(benchmark.Samples, model.Sample{
				Iterations: shape.iterations + int64(index),
				NsOp:       shape.nsOp * factor,
				BytesOp:    &bytesOp,
				AllocsOp:   &allocsOp,
				Extra:      map[string]float64{},
			})
		}
		run.Benchmarks = append(run.Benchmarks, benchmark)
	}
	return run.Normalized()
}

// alternatingSign returns +1 for even indices and -1 for odd ones.
func alternatingSign(index int) float64 {
	if index%2 == 1 {
		return -1
	}
	return 1
}

// itoa renders a small non-negative integer, so golden failure messages need no
// string formatting import.
func itoa(value int) string { return strconv.Itoa(value) }
