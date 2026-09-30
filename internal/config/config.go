// Package config. config - the `benchbook.toml` schema: defaults, decoding,
// validation with actionable messages, and the commented sample written by
// `benchbook init`.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// FileName is the name of the per-project configuration file.
const FileName = "benchbook.toml"

// defaultTemplate is the commented configuration written by `benchbook init` and
// shipped as examples/benchbook.toml. Keeping it a format string with the project
// name injected means the example and the generated file can never drift apart.
const defaultTemplate = `# benchbook.toml - configuration for the benchbook benchmark documentation tool.
# Everything here is optional except [project].name; defaults are shown commented out.

[project]
name     = %q
# emoji     = "🦀🐹"                       # header + favicon badge
# repo_url  = "https://github.com/me/repo"

[paths]
# data_dir = "benchbook"                   # current.json + history/ (commit this)
# site_dir = "docs"                        # generated documentation site
# raw_dir  = "benchbook/raw"               # raw go test / pprof output (add to .gitignore)

[bench]
# packages         = ["./..."]
# args             = ["-benchmem"]         # "go test -run '^$' -bench=." is always added
# count            = 10                    # samples per benchmark
# timeout          = "30m"
# baseline_variant = "traditional"         # variant others are compared against

[stats]
# alpha            = 0.05
# min_effect_pct   = 1.0
# min_samples      = 6
# noise_floor_note = true

[[category]]
title = "🧠 CPU Bound"
match = ["Parse", "Encode"]

[category_default]
title = "⚡ Basic Operations"

[tracked]
# benchmarks = ["FindUser/result", "Success/result"]

[profile]
# package = "./internal/core"
# top     = 20
# kinds   = ["cpu", "mem_space", "mem_objects"]

[site]
# title             = "Documentation"
# default_theme     = "light"
# theme_storage_key = "benchbook-theme"
# highlight_langs   = ["go", "bash", "toml"]
# loading_text      = "Loading documentation... (๑•̀ㅂ•́)و✧"
# not_found_text    = "Looks like this page wandered off... (╥_╥)"

# Authored navigation. Commented out on purpose: an entry pointing at a page that does
# not exist yet makes every build warn, so add the page and the entry together.
# [[nav]]
# section = "Getting Started"
# pages = [ { title = "Introduction", path = "content/home.md" } ]
# Generated pages are merged into a "Performance" section automatically.
`

// ---------------------------------------------- Types ----------------------------------------- //

// Config is the whole contents of benchbook.toml.
type Config struct {
	// Project holds identity values shown in the site header and reports.
	Project ProjectConfig `toml:"project"`
	// Paths holds the directories benchbook reads and writes.
	Paths PathsConfig `toml:"paths"`
	// Bench holds how benchmarks are executed and interpreted.
	Bench BenchConfig `toml:"bench"`
	// Stats holds the thresholds of every statistical claim.
	Stats StatsConfig `toml:"stats"`
	// Categories are ordered, first match wins for a workload name.
	Categories []CategoryConfig `toml:"category"`
	// CategoryDefault is the fallback bucket for unmatched workloads.
	CategoryDefault CategoryConfig `toml:"category_default"`
	// Tracked selects the benchmarks shown in the timeline chain.
	Tracked TrackedConfig `toml:"tracked"`
	// Profile configures `benchbook profile`.
	Profile ProfileConfig `toml:"profile"`
	// Site configures the generated documentation shell.
	Site SiteConfig `toml:"site"`
	// Nav is the authored navigation tree; generated pages are merged in.
	Nav []NavSection `toml:"nav"`

	// Dir is the directory the configuration was loaded from. It is not part of
	// the file: it anchors every relative path so the CLI works from anywhere.
	Dir string `toml:"-"`
}

// ProjectConfig is the human identity of the project.
type ProjectConfig struct {
	// Name is the project name; required.
	Name string `toml:"name"`
	// Emoji is a short decorative badge used in the header and favicon.
	Emoji string `toml:"emoji"`
	// RepoURL is linked from the site header when set.
	RepoURL string `toml:"repo_url"`
}

// PathsConfig holds the directories, all relative to Config.Dir.
type PathsConfig struct {
	// DataDir holds current.json and history/.
	DataDir string `toml:"data_dir"`
	// SiteDir is the root of the generated static site.
	SiteDir string `toml:"site_dir"`
	// RawDir holds untouched go test and pprof output.
	RawDir string `toml:"raw_dir"`
}

// BenchConfig controls benchmark execution and interpretation.
type BenchConfig struct {
	// Packages are the patterns passed to `go test`.
	Packages []string `toml:"packages"`
	// Args are extra `go test` flags; -bench and -count are managed by benchbook.
	Args []string `toml:"args"`
	// Count is the number of samples taken per benchmark.
	Count int `toml:"count"`
	// Timeout is the `go test -timeout` value.
	Timeout string `toml:"timeout"`
	// BaselineVariant names the variant every other variant is compared against.
	BaselineVariant string `toml:"baseline_variant"`
	// GroupRules map flat legacy benchmark names onto workloads.
	GroupRules []GroupRule `toml:"group_rule"`
}

// GroupRule is the legacy fallback for repositories that cannot use Go
// sub-benchmarks: it assigns variants of a flat benchmark name to one workload.
type GroupRule struct {
	// Contains must appear in the benchmark name for the rule to apply.
	Contains string `toml:"contains"`
	// Exclude disqualifies a name that would otherwise match.
	Exclude string `toml:"exclude"`
	// Workload is the workload name assigned to matching benchmarks.
	Workload string `toml:"workload"`
}

// StatsConfig holds every threshold between raw samples and a claim.
type StatsConfig struct {
	// Alpha is the significance level of the two-sided Mann-Whitney U test.
	Alpha float64 `toml:"alpha"`
	// MinEffectPct is the smallest percentage change worth reporting.
	MinEffectPct float64 `toml:"min_effect_pct"`
	// MinSamples is the minimum number of samples per benchmark to promote.
	MinSamples int `toml:"min_samples"`
	// NoiseFloorNote appends the explanation of the noise floor to generated pages.
	NoiseFloorNote bool `toml:"noise_floor_note"`
}

// CategoryConfig is a named group of workloads used for page layout.
type CategoryConfig struct {
	// Title is the section heading, emoji welcome.
	Title string `toml:"title"`
	// Match is a list of case-sensitive substrings tested against workload names.
	Match []string `toml:"match"`
}

// TrackedConfig selects which benchmarks headline the timeline.
type TrackedConfig struct {
	// Benchmarks are benchmark names or "workload/variant" suffixes to track.
	Benchmarks []string `toml:"benchmarks"`
}

// ProfileConfig configures the `benchbook profile` command.
type ProfileConfig struct {
	// Package is the single package to profile.
	Package string `toml:"package"`
	// Top is the number of rows kept in generated profile tables.
	Top int `toml:"top"`
	// Kinds lists the profiles to capture.
	Kinds []string `toml:"kinds"`
}

// SiteConfig configures the generated documentation shell.
type SiteConfig struct {
	// Title is the page title and header text.
	Title string `toml:"title"`
	// DefaultTheme is one of the Theme* constants.
	DefaultTheme string `toml:"default_theme"`
	// ThemeStorageKey is the localStorage key holding the visitor's theme choice.
	ThemeStorageKey string `toml:"theme_storage_key"`
	// ExtraCSS is an optional stylesheet path, appended after the shell CSS.
	ExtraCSS string `toml:"extra_css"`
	// HighlightLangs are extra highlight.js grammars to load besides Go.
	HighlightLangs []string `toml:"highlight_langs"`
	// LoadingText is shown while a page is being fetched.
	LoadingText string `toml:"loading_text"`
	// NotFoundText is shown for a missing page.
	NotFoundText string `toml:"not_found_text"`
	// PerformanceSection is the nav section generated pages are merged into.
	PerformanceSection string `toml:"performance_section"`
	// MilestoneLimit caps how many milestones appear in history listings.
	MilestoneLimit int `toml:"milestone_limit"`
}

// NavSection is one authored sidebar section.
type NavSection struct {
	// Section is the group heading.
	Section string `toml:"section"`
	// Pages are the links inside the section, in order.
	Pages []NavPage `toml:"pages"`
}

// NavPage is one authored sidebar link.
type NavPage struct {
	// Title is the link label.
	Title string `toml:"title"`
	// Path is the page path relative to the site directory.
	Path string `toml:"path"`
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// Defaults returns the configuration every field falls back to. Having defaults in
// code (rather than only in the sample file) means a nearly empty benchbook.toml
// is a valid configuration.
func Defaults() Config {
	return Config{
		Paths: PathsConfig{
			DataDir: "benchbook",
			SiteDir: "docs",
			RawDir:  filepath.Join("benchbook", "raw"),
		},
		Bench: BenchConfig{
			Packages:        []string{"./..."},
			Args:            []string{"-benchmem"},
			Count:           10,
			Timeout:         "30m",
			BaselineVariant: "traditional",
			GroupRules:      []GroupRule{},
		},
		Stats: StatsConfig{
			Alpha:          0.05,
			MinEffectPct:   1.0,
			MinSamples:     6,
			NoiseFloorNote: true,
		},
		CategoryDefault: CategoryConfig{Title: "⚡ Basic Operations"},
		Tracked:         TrackedConfig{Benchmarks: []string{}},
		Profile: ProfileConfig{
			Package: "",
			Top:     20,
			Kinds:   []string{kindCPU, kindMemSpace, kindMemObjects},
		},
		Site: SiteConfig{
			Title:              "Documentation",
			DefaultTheme:       ThemeLight,
			ThemeStorageKey:    "benchbook-theme",
			ExtraCSS:           "",
			HighlightLangs:     []string{"go", "bash", "toml", "json", "diff"},
			LoadingText:        "Loading documentation... (๑•̀ㅂ•́)و✧",
			NotFoundText:       "Looks like this page wandered off... (╥_╥)",
			PerformanceSection: "Performance",
			MilestoneLimit:     500,
		},
		Nav:        []NavSection{},
		Categories: []CategoryConfig{},
	}
}

// ----------------------------------------- Public API ----------------------------------------- //

// Supported profile kinds, mirrored here to keep config free of an import cycle
// with the model package.
const (
	// kindCPU is the CPU profile kind.
	kindCPU = "cpu"
	// kindMemSpace is the heap profile measured in bytes.
	kindMemSpace = "mem_space"
	// kindMemObjects is the heap profile measured in object counts.
	kindMemObjects = "mem_objects"
)

// Supported themes for the documentation shell.
const (
	// ThemeLight is the light colour scheme.
	ThemeLight = "light"
	// ThemeDark is the dark colour scheme.
	ThemeDark = "dark"
	// ThemeNeon is the high-contrast neon colour scheme.
	ThemeNeon = "neon"
)

// SupportedThemes lists the themes accepted by [site].default_theme.
var SupportedThemes = []string{ThemeLight, ThemeDark, ThemeNeon}

// SupportedProfileKinds lists the kinds accepted by [profile].kinds.
var SupportedProfileKinds = []string{kindCPU, kindMemSpace, kindMemObjects}

// Load reads benchbook.toml from dir, applies defaults and validates the result.
// Missing file is an error: benchbook never guesses a project's identity.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w (run `benchbook init` to create one)", path, err)
	}
	defer file.Close()
	config, err := Decode(file, dir)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// Decode parses configuration from a reader, applies defaults and validates it.
// The dir argument anchors all relative paths and is stored on the result.
func Decode(reader io.Reader, dir string) (*Config, error) {
	config := Defaults()
	metadata, err := toml.NewDecoder(reader).Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	// A misspelled key would otherwise be ignored silently, which turns a typo into
	// a mysteriously ignored setting.
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown setting(s) in %s: %s: check the spelling, or see `benchbook init` for the full example",
			FileName, joinKeys(undecoded))
	}
	config.Dir = dir
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// Validate checks every field against its allowed range and returns a message
// naming the field path and the fix, instead of a bare "invalid config".
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Project.Name) == "" {
		return fmt.Errorf("project.name is empty: set it in %s, e.g. name = \"My Project\"", FileName)
	}
	if c.Bench.Count < 1 {
		return fmt.Errorf("bench.count must be >= 1, got %d: set bench.count = 10 (samples per benchmark)", c.Bench.Count)
	}
	if len(c.Bench.Packages) == 0 {
		return fmt.Errorf("bench.packages is empty: set bench.packages = [\"./...\"]")
	}
	if _, err := c.BenchTimeout(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Bench.BaselineVariant) == "" {
		return fmt.Errorf("bench.baseline_variant must not be empty: set it to the variant name used as baseline, e.g. baseline_variant = \"traditional\"")
	}
	for i, rule := range c.Bench.GroupRules {
		if strings.TrimSpace(rule.Contains) == "" {
			return fmt.Errorf("bench.group_rule[%d].contains is empty: set contains = \"WithTry\" (the substring that identifies the legacy benchmark name)", i)
		}
		if strings.TrimSpace(rule.Workload) == "" {
			return fmt.Errorf("bench.group_rule[%d].workload is empty: name the workload the matches belong to, e.g. workload = \"ErrorHandlingCPU\"", i)
		}
	}
	if err := validateStats(c.Stats); err != nil {
		return err
	}
	for i, category := range c.Categories {
		if strings.TrimSpace(category.Title) == "" {
			return fmt.Errorf("category[%d].title is empty: give the section a title, e.g. title = \"🗄️ Database Operations\"", i)
		}
		if len(category.Match) == 0 {
			return fmt.Errorf("category[%d].match is empty: list the workload substrings it covers, e.g. match = [\"DB\"]", i)
		}
	}
	if strings.TrimSpace(c.CategoryDefault.Title) == "" {
		return fmt.Errorf("category_default.title is empty: set the fallback section title, e.g. title = \"⚡ Basic Operations\"")
	}
	if c.Profile.Top < 1 {
		return fmt.Errorf("profile.top must be >= 1, got %d: use profile.top = 20", c.Profile.Top)
	}
	if len(c.Profile.Kinds) == 0 {
		return fmt.Errorf("profile.kinds is empty: use profile.kinds = [\"cpu\", \"mem_space\", \"mem_objects\"]")
	}
	for i, kind := range c.Profile.Kinds {
		if !containsString(SupportedProfileKinds, kind) {
			return fmt.Errorf("profile.kinds[%d] = %q is not a known profile kind: use one of %s",
				i, kind, strings.Join(SupportedProfileKinds, ", "))
		}
	}
	if err := validateSite(c.Site); err != nil {
		return err
	}
	for i, section := range c.Nav {
		if strings.TrimSpace(section.Section) == "" {
			return fmt.Errorf("nav[%d].section is empty: give the section a heading, e.g. section = \"Getting Started\"", i)
		}
		for j, page := range section.Pages {
			if strings.TrimSpace(page.Title) == "" {
				return fmt.Errorf("nav[%d].pages[%d].title is empty: give the link a label", i, j)
			}
			if strings.TrimSpace(page.Path) == "" {
				return fmt.Errorf("nav[%d].pages[%d].path is empty: point it at a page relative to site_dir, e.g. path = \"content/home.md\"", i, j)
			}
		}
	}
	return nil
}

// joinKeys renders undecoded TOML keys as a comma-separated list for error messages.
func joinKeys(keys []toml.Key) string {
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, key.String())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// validateStats checks the statistical thresholds.
func validateStats(stats StatsConfig) error {
	if stats.Alpha <= 0 || stats.Alpha >= 1 {
		return fmt.Errorf("stats.alpha must be strictly between 0 and 1, got %v: use stats.alpha = 0.05", stats.Alpha)
	}
	if stats.MinEffectPct <= 0 {
		return fmt.Errorf("stats.min_effect_pct must be > 0, got %v: use stats.min_effect_pct = 1.0 (a change below it is reported as unchanged)", stats.MinEffectPct)
	}
	if stats.MinSamples < 1 {
		return fmt.Errorf("stats.min_samples must be >= 1, got %d: use stats.min_samples = 6", stats.MinSamples)
	}
	return nil
}

// validateSite checks the site-facing settings.
func validateSite(site SiteConfig) error {
	if strings.TrimSpace(site.Title) == "" {
		return fmt.Errorf("site.title is empty: set the documentation title, e.g. title = \"My Docs\"")
	}
	if !containsString(SupportedThemes, site.DefaultTheme) {
		return fmt.Errorf("site.default_theme = %q is not a known theme: use one of %s",
			site.DefaultTheme, strings.Join(SupportedThemes, ", "))
	}
	if strings.TrimSpace(site.ThemeStorageKey) == "" {
		return fmt.Errorf("site.theme_storage_key is empty: set a localStorage key, e.g. theme_storage_key = \"benchbook-theme\"")
	}
	if site.MilestoneLimit < 1 {
		return fmt.Errorf("site.milestone_limit must be >= 1, got %d: use site.milestone_limit = 500", site.MilestoneLimit)
	}
	return nil
}

// containsString reports membership of a string slice.
func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// BenchTimeout parses [bench].timeout into a duration.
func (c *Config) BenchTimeout() (time.Duration, error) {
	timeout, err := time.ParseDuration(c.Bench.Timeout)
	if err != nil {
		return 0, fmt.Errorf("bench.timeout = %q is not a duration: use a Go duration string such as \"30m\" or \"1h\": %w", c.Bench.Timeout, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("bench.timeout must be positive, got %q: set bench.timeout = \"30m\"", c.Bench.Timeout)
	}
	return timeout, nil
}

// PackageDefaulted reports whether [profile].package was left empty, in which
// case `benchbook profile` requires an explicit --pkg flag.
func (c *Config) PackageDefaulted() bool { return strings.TrimSpace(c.Profile.Package) == "" }

// DataDir returns the absolute data directory.
func (c *Config) DataDir() string { return c.abs(c.Paths.DataDir) }

// SiteDir returns the absolute site directory.
func (c *Config) SiteDir() string { return c.abs(c.Paths.SiteDir) }

// RawDir returns the absolute raw-output directory.
func (c *Config) RawDir() string { return c.abs(c.Paths.RawDir) }

// ContentDir returns the directory holding Markdown pages.
func (c *Config) ContentDir() string { return filepath.Join(c.SiteDir(), contentDirName) }

// DataExportDir returns the directory holding JSON copied for client-side compare.
func (c *Config) DataExportDir() string { return filepath.Join(c.SiteDir(), dataDirName) }

// abs resolves a relative-to-config path into an absolute one.
func (c *Config) abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.Dir, filepath.Clean(path))
}

// Directory names inside the site directory.
const (
	// contentDirName holds authored and generated Markdown.
	contentDirName = "content"
	// dataDirName holds the JSON exported for the in-browser comparer.
	dataDirName = "data"
)

// CategoryFor returns the category a workload belongs to, honouring declaration
// order (first match wins) and falling back to [category_default].
func (c *Config) CategoryFor(workload string) CategoryConfig {
	for _, category := range c.Categories {
		for _, needle := range category.Match {
			if strings.Contains(workload, needle) {
				return category
			}
		}
	}
	return c.CategoryDefault
}

// Sample writes the commented example configuration with the given project name.
func Sample(writer io.Writer, projectName string) error {
	if strings.TrimSpace(projectName) == "" {
		projectName = "my-project"
	}
	if _, err := fmt.Fprintf(writer, defaultTemplate, projectName); err != nil {
		return fmt.Errorf("write sample config: %w", err)
	}
	return nil
}
