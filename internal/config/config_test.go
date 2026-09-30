// Package config. config_test - tests for the defaults, the sample file, validation
// messages and the path resolution the whole CLI depends on.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ----------------------------------------- Public API ----------------------------------------- //

// TestDefaultsValidateOnceTheProjectIsNamed is the promise that a nearly empty
// benchbook.toml works: the only truly required setting is the project name.
func TestDefaultsValidateOnceTheProjectIsNamed(t *testing.T) {
	config := Defaults()
	if err := config.Validate(); err == nil {
		t.Fatal("Defaults().Validate() = nil; the project name must be required, never guessed")
	}
	config.Project.Name = "demo"
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() with a name = %v, want nil", err)
	}
	// The default sample count must satisfy the promotion rule, otherwise a fresh
	// project could never promote a milestone.
	if config.Bench.Count < config.Stats.MinSamples {
		t.Errorf("bench.count = %d, want at least stats.min_samples = %d", config.Bench.Count, config.Stats.MinSamples)
	}
	if config.Stats.Alpha != 0.05 {
		t.Errorf("stats.alpha = %v, want 0.05", config.Stats.Alpha)
	}
	if config.Stats.MinEffectPct != 1.0 {
		t.Errorf("stats.min_effect_pct = %v, want 1.0", config.Stats.MinEffectPct)
	}
}

// TestSampleRoundTripsThroughDecode is the test that keeps `benchbook init` honest:
// what init writes must load without edits.
func TestSampleRoundTripsThroughDecode(t *testing.T) {
	var builder strings.Builder
	if err := Sample(&builder, "demo-project"); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	config, err := Decode(strings.NewReader(builder.String()), "/tmp/demo")
	if err != nil {
		t.Fatalf("Decode(sample): %v", err)
	}
	if config.Project.Name != "demo-project" {
		t.Errorf("project.name = %q, want demo-project", config.Project.Name)
	}
	if config.Dir != "/tmp/demo" {
		t.Errorf("Dir = %q, want /tmp/demo", config.Dir)
	}
	if _, err := config.BenchTimeout(); err != nil {
		t.Errorf("BenchTimeout of the sample = %v", err)
	}
}

// TestSampleFallsBackToAPlaceholderName keeps `benchbook init` without a project
// name from writing an invalid file.
func TestSampleFallsBackToAPlaceholderName(t *testing.T) {
	var builder strings.Builder
	if err := Sample(&builder, "   "); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if !strings.Contains(builder.String(), `"my-project"`) {
		t.Errorf("sample does not fall back to a placeholder name:\n%s", builder.String())
	}
	if _, err := Decode(strings.NewReader(builder.String()), "/tmp/demo"); err != nil {
		t.Errorf("the fallback sample does not load: %v", err)
	}
}

// TestLoadMissingFileTellsTheUserWhatToDo checks the first error a new user sees.
func TestLoadMissingFileTellsTheUserWhatToDo(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load accepted a directory without benchbook.toml")
	}
	if !strings.Contains(err.Error(), "benchbook init") {
		t.Errorf("error %q does not suggest `benchbook init`", err)
	}
}

// TestLoadReadsARealFile covers the file path end to end.
func TestLoadReadsARealFile(t *testing.T) {
	dir := t.TempDir()
	body := "project.name = \"from-file\"\nbench.count = 9\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	config, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.Project.Name != "from-file" || config.Bench.Count != 9 {
		t.Errorf("loaded %+v, want name from-file and count 9", config.Project)
	}
	// Fields the file does not mention keep their defaults.
	if config.Stats.MinSamples != Defaults().Stats.MinSamples {
		t.Errorf("stats.min_samples = %d, want the default %d", config.Stats.MinSamples, Defaults().Stats.MinSamples)
	}
}

// TestValidationMessagesNameTheFieldAndTheFix walks the validation rules. Each
// message must be actionable: the exact key, and what to set it to.
func TestValidationMessagesNameTheFieldAndTheFix(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantKey string
	}{
		{name: "empty project name", mutate: func(c *Config) { c.Project.Name = "  " }, wantKey: "project.name"},
		// Every other case needs a valid name, otherwise validation stops there.
		{name: "zero count", mutate: func(c *Config) { c.Bench.Count = 0 }, wantKey: "bench.count"},
		{name: "no packages", mutate: func(c *Config) { c.Bench.Packages = nil }, wantKey: "bench.packages"},
		{name: "bad timeout", mutate: func(c *Config) { c.Bench.Timeout = "soon" }, wantKey: "bench.timeout"},
		{name: "zero timeout", mutate: func(c *Config) { c.Bench.Timeout = "0s" }, wantKey: "bench.timeout"},
		{name: "no baseline variant", mutate: func(c *Config) { c.Bench.BaselineVariant = "" }, wantKey: "bench.baseline_variant"},
		{name: "group rule without workload", mutate: func(c *Config) {
			c.Bench.GroupRules = []GroupRule{{Contains: "X"}}
		}, wantKey: "bench.group_rule[0].workload"},
		{name: "group rule without contains", mutate: func(c *Config) {
			c.Bench.GroupRules = []GroupRule{{Workload: "X"}}
		}, wantKey: "bench.group_rule[0].contains"},
		{name: "alpha above one", mutate: func(c *Config) { c.Stats.Alpha = 1.5 }, wantKey: "stats.alpha"},
		{name: "zero alpha", mutate: func(c *Config) { c.Stats.Alpha = 0 }, wantKey: "stats.alpha"},
		{name: "negative minimum effect", mutate: func(c *Config) { c.Stats.MinEffectPct = -1 }, wantKey: "stats.min_effect_pct"},
		{name: "zero minimum effect", mutate: func(c *Config) { c.Stats.MinEffectPct = 0 }, wantKey: "stats.min_effect_pct"},
		{name: "zero min samples", mutate: func(c *Config) { c.Stats.MinSamples = 0 }, wantKey: "stats.min_samples"},
		{name: "unknown theme", mutate: func(c *Config) { c.Site.DefaultTheme = "midnight" }, wantKey: "site.default_theme"},
		{name: "empty category title", mutate: func(c *Config) {
			c.Categories = []CategoryConfig{{Title: " ", Match: []string{"x"}}}
		}, wantKey: "category[0].title"},
		{name: "empty category match", mutate: func(c *Config) {
			c.Categories = []CategoryConfig{{Title: "x"}}
		}, wantKey: "category[0].match"},
		{name: "unknown profile kind", mutate: func(c *Config) { c.Profile.Kinds = []string{"gpu"} }, wantKey: "profile.kinds"},
		{name: "empty profile kinds", mutate: func(c *Config) { c.Profile.Kinds = nil }, wantKey: "profile.kinds"},
		{name: "negative profile top", mutate: func(c *Config) { c.Profile.Top = -1 }, wantKey: "profile.top"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := Defaults()
			config.Project.Name = "demo"
			testCase.mutate(&config)
			err := config.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error mentioning %s", testCase.wantKey)
			}
			if !strings.Contains(err.Error(), testCase.wantKey) {
				t.Errorf("error %q does not name %q", err, testCase.wantKey)
			}
			if !strings.Contains(err.Error(), "=") {
				t.Errorf("error %q does not contain a concrete value to set", err)
			}
		})
	}
}

// TestUnknownTOMLKeyIsRejected keeps a typo from silently disabling a setting.
func TestUnknownTOMLKeyIsRejected(t *testing.T) {
	_, err := Decode(strings.NewReader("project.name = \"demo\"\nbench.countt = 5\n"), "/tmp/demo")
	if err == nil {
		t.Fatal("Decode accepted an unknown key")
	}
	if !strings.Contains(err.Error(), "countt") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

// TestPathAccessorsResolveRelativeToTheConfigDir is what makes the CLI work from any
// working directory.
func TestPathAccessorsResolveRelativeToTheConfigDir(t *testing.T) {
	config, err := Decode(strings.NewReader("project.name = \"demo\"\n"), "/repo")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cases := map[string]string{
		"data dir":    config.DataDir(),
		"site dir":    config.SiteDir(),
		"raw dir":     config.RawDir(),
		"content dir": config.ContentDir(),
	}
	for name, path := range cases {
		if !filepath.IsAbs(path) {
			t.Errorf("%s = %q, want an absolute path", name, path)
		}
		if !strings.HasPrefix(path, "/repo/") {
			t.Errorf("%s = %q, want it to live under /repo", name, path)
		}
	}
	absolute := Defaults()
	absolute.Dir = "/repo"
	absolute.Paths.DataDir = "/elsewhere/data"
	if got := absolute.DataDir(); got != "/elsewhere/data" {
		t.Errorf("absolute data dir = %q, want it kept as-is", got)
	}
}

// TestCategoryForUsesFirstMatch keeps the declaration order meaningful.
func TestCategoryForUsesFirstMatch(t *testing.T) {
	config := Defaults()
	config.Categories = []CategoryConfig{
		{Title: "Lookup", Match: []string{"Find", "Lookup"}},
		{Title: "Basics", Match: []string{"Concat"}},
	}
	config.CategoryDefault = CategoryConfig{Title: "Other"}
	cases := map[string]string{
		"FindUser":         "Lookup",
		"UserLookupByID":   "Lookup",
		"Concat":           "Basics",
		"SomethingElse":    "Other",
		"FindConcat":       "Lookup",
		"concat lowercase": "Other",
	}
	for workload, want := range cases {
		if got := config.CategoryFor(workload); got.Title != want {
			t.Errorf("CategoryFor(%q) = %q, want %q", workload, got.Title, want)
		}
	}
}

// TestBenchTimeoutParsesGoDurations covers the parsing helper every command uses.
func TestBenchTimeoutParsesGoDurations(t *testing.T) {
	config := Defaults()
	config.Bench.Timeout = "2m30s"
	timeout, err := config.BenchTimeout()
	if err != nil {
		t.Fatalf("BenchTimeout: %v", err)
	}
	if timeout != 2*time.Minute+30*time.Second {
		t.Errorf("timeout = %v, want 2m30s", timeout)
	}
}

// TestPackageDefaultedTellsProfileApartFromConfig keeps the "which package do I
// profile?" rule explicit.
func TestPackageDefaultedTellsProfileApartFromConfig(t *testing.T) {
	config := Defaults()
	if !config.PackageDefaulted() {
		t.Errorf("default [profile].package = %q, want it reported as unset", config.Profile.Package)
	}
	config.Profile.Package = "./internal/core"
	if config.PackageDefaulted() {
		t.Error("an explicit [profile].package was reported as unset")
	}
}
