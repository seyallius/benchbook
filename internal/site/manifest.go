// Package site. manifest - the manifest that configures the generic shell. The
// shell contains no project-specific text and no hard-coded navigation: everything
// it needs arrives here, which is what makes one binary usable across projects.
package site

import (
	"fmt"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
)

// ---------------------------------------------- Types ----------------------------------------- //

// File name and content type details of the manifest.
const (
	// ManifestFile is the file the shell fetches first.
	ManifestFile = "manifest.json"
	// ManifestSchemaVersion is the manifest format version.
	ManifestSchemaVersion = 1
	// DefaultTheme is used when the configuration is silent.
	DefaultTheme = "light"
	// DefaultPerformanceSection is the nav section generated pages are merged into.
	DefaultPerformanceSection = "Performance"
	// ExpectedManifestVersionField names the version column for error messages.
	ExpectedManifestVersionField = "schema_version"
)

// Manifest is the whole configuration of the documentation shell.
type Manifest struct {
	// SchemaVersion is the manifest format version.
	SchemaVersion int `json:"schema_version"`
	// GeneratedBy names the tool and version that produced the site.
	GeneratedBy string `json:"generated_by"`
	// Project holds the identity values shown in the header.
	Project ProjectInfo `json:"project"`
	// Site holds the shell settings.
	Site SiteSettings `json:"site"`
	// Nav is the merged navigation tree, authored sections first.
	Nav []NavSection `json:"nav"`
	// Data points the shell at the exported comparison data.
	Data DataPaths `json:"data"`
	// Metrics maps a metric id onto its display title, so the comparer can label
	// its selector without hard-coding metric names.
	Metrics map[string]string `json:"metrics"`
	// DefaultPath is the page shown when no hash is present.
	DefaultPath string `json:"default_path"`
	// Favicon is a self-contained data URI built from the project emoji.
	Favicon string `json:"favicon"`
	// Options are the statistical thresholds the in-browser comparer must use, so
	// the page and the CLI never disagree about what counts as a change.
	Options CompareOptions `json:"options"`
}

// ProjectInfo is the project identity block of the manifest.
type ProjectInfo struct {
	// Name is the project name.
	Name string `json:"name"`
	// Emoji is the badge shown next to the name.
	Emoji string `json:"emoji"`
	// RepoURL is linked from the header when set.
	RepoURL string `json:"repo_url"`
}

// SiteSettings mirrors the [site] configuration section.
type SiteSettings struct {
	// Title is the browser title and header text.
	Title string `json:"title"`
	// DefaultTheme is light, dark or neon.
	DefaultTheme string `json:"default_theme"`
	// ThemeStorageKey is the localStorage key of the visitor's choice.
	ThemeStorageKey string `json:"theme_storage_key"`
	// ExtraCSS is the path of an appended stylesheet, when configured.
	ExtraCSS string `json:"extra_css"`
	// HighlightLangs are the extra grammars the shell should try to load.
	HighlightLangs []string `json:"highlight_langs"`
	// LoadingText is shown while a page is fetched.
	LoadingText string `json:"loading_text"`
	// NotFoundText is shown for a missing page.
	NotFoundText string `json:"not_found_text"`
}

// NavSection is one sidebar section.
type NavSection struct {
	// Section is the heading.
	Section string `json:"section"`
	// Collapsible marks a section the shell renders as a <details> element.
	Collapsible bool `json:"collapsible"`
	// Expanded controls whether a collapsible section starts open.
	Expanded bool `json:"expanded"`
	// Pages are the links of the section.
	Pages []NavPage `json:"pages"`
}

// NavPage is one sidebar link.
type NavPage struct {
	// Title is the link label.
	Title string `json:"title"`
	// Path is the page path relative to the site root.
	Path string `json:"path"`
}

// DataPaths tells the shell where the JSON lives.
type DataPaths struct {
	// Index is the run index consumed by the compare page.
	Index string `json:"index"`
	// RunsDir is the directory holding the run copies.
	RunsDir string `json:"runs_dir"`
}

// DataIndex is the content of data/index.json: everything the compare page needs
// before it fetches any individual run.
type DataIndex struct {
	// SchemaVersion is the index format version.
	SchemaVersion int `json:"schema_version"`
	// GeneratedBy names the tool that produced the file.
	GeneratedBy string `json:"generated_by"`
	// Options are the statistical thresholds used in the browser, so the page and
	// the CLI cannot disagree about what "significant" means.
	Options CompareOptions `json:"options"`
	// Metrics maps a metric id onto its display title.
	Metrics map[string]string `json:"metrics"`
	// Runs lists every run available for comparison, oldest first.
	Runs []RunEntry `json:"runs"`
}

// CompareOptions mirrors the comparison thresholds in a JSON-friendly shape.
type CompareOptions struct {
	// Alpha is the significance level.
	Alpha float64 `json:"alpha"`
	// MinEffectPct is the smallest reported effect.
	MinEffectPct float64 `json:"min_effect_pct"`
	// MinSamples is the promotion threshold.
	MinSamples int `json:"min_samples"`
}

// RunEntry describes one run in the index.
type RunEntry struct {
	// ID is the milestone id or "current".
	ID string `json:"id"`
	// Title is the display label used in the comparer's dropdowns.
	Title string `json:"title"`
	// Label is the raw milestone label.
	Label string `json:"label"`
	// Date is the recording date.
	Date string `json:"date"`
	// Seq is the milestone sequence number, 0 for the working run.
	Seq int `json:"seq"`
	// File is the run document, relative to the site root.
	File string `json:"file"`
	// SHA is the abbreviated git commit.
	SHA string `json:"sha"`
	// IsCurrent marks the disposable working run.
	IsCurrent bool `json:"is_current"`
	// Badges are the warning badges shown next to the entry.
	Badges []string `json:"badges"`
	// Headline holds the geomean delta per metric against the previous run.
	Headline map[string]float64 `json:"headline"`
}

// ----------------------------------------- Public API ----------------------------------------- //

// BuildManifest merges the authored navigation with the generated pages and fills
// the site settings. Authored pages that do not resolve are reported as warnings
// rather than errors: a broken link must not stop a build.
func BuildManifest(options Options, milestones []*model.Run, generated []NavPage) (*Manifest, []string) {
	manifest := &Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedBy:   options.GeneratedBy,
		Project: ProjectInfo{
			Name:    options.ProjectName,
			Emoji:   options.Emoji,
			RepoURL: options.RepoURL,
		},
		Site: SiteSettings{
			Title:           options.SiteTitle,
			DefaultTheme:    themeOrDefault(options.DefaultTheme),
			ThemeStorageKey: options.ThemeStorageKey,
			ExtraCSS:        options.ExtraCSS,
			HighlightLangs:  options.HighlightLangs,
			LoadingText:     options.LoadingText,
			NotFoundText:    options.NotFoundText,
		},
		Data: DataPaths{
			Index:   DataDirName + "/" + DataIndexFile,
			RunsDir: DataDirName + "/",
		},
		Metrics:     MetricTitles(),
		DefaultPath: defaultPath(options.DefaultPath),
		Favicon:     FaviconDataURI(options.Emoji),
		Options:     options.CompareOptions,
	}
	if generated == nil {
		generated = []NavPage{}
	}

	warnings := []string{}
	for _, section := range options.Nav {
		merged := NavSection{Section: section.Section, Expanded: true, Pages: []NavPage{}}
		for _, page := range section.Pages {
			merged.Pages = append(merged.Pages, NavPage{Title: page.Title, Path: page.Path})
			if options.PageExists != nil && !options.PageExists(page.Path) {
				warnings = append(warnings, fmt.Sprintf("nav page %q (%s) does not exist yet: write it or fix the path in %s",
					page.Title, page.Path, "benchbook.toml"))
			}
		}
		manifest.Nav = append(manifest.Nav, merged)
	}
	manifest.Nav = append(manifest.Nav, NavSection{
		Section:     options.PerformanceSection,
		Collapsible: false,
		Expanded:    true,
		Pages:       generated,
	})
	manifest.Nav = append(manifest.Nav, milestoneSection(options, milestones))
	return manifest, warnings
}

// defaultPath returns the page a bare URL opens, falling back to the benchmark
// report so a site always has somewhere to land.
func defaultPath(configured string) string {
	if strings.TrimSpace(configured) == "" {
		return GeneratedPageBenchmarks
	}
	return configured
}

// milestoneSection renders the collapsible milestone list, newest first.
func milestoneSection(options Options, milestones []*model.Run) NavSection {
	section := NavSection{
		Section:     MilestonesSection,
		Collapsible: true,
		Expanded:    options.MilestonesExpanded,
		Pages:       []NavPage{},
	}
	ordered := append([]*model.Run{}, milestones...)
	render.SortMilestones(ordered)
	for index := len(ordered) - 1; index >= 0; index-- {
		milestone := ordered[index]
		section.Pages = append(section.Pages, NavPage{
			Title: milestoneTitle(milestone),
			Path:  MilestoneContentPrefix + milestone.ID + ".md",
		})
	}
	return section
}

// milestoneTitle renders "m002 · sync.Pool for buffers" for the sidebar.
func milestoneTitle(milestone *model.Run) string {
	if strings.TrimSpace(milestone.Label) == "" {
		return milestone.ID
	}
	return milestone.ID + " · " + milestone.Label
}

// themeOrDefault validates the configured theme.
func themeOrDefault(theme string) string {
	switch theme {
	case "light", "dark", "neon":
		return theme
	default:
		return DefaultTheme
	}
}

// MetricTitles returns the display title of every metric the shell may have to show.
func MetricTitles() map[string]string {
	titles := map[string]string{
		model.MetricNsOp:     "ns/op",
		model.MetricBytesOp:  "B/op",
		model.MetricAllocsOp: "allocs/op",
	}
	for unit := range higherIsBetterUnits() {
		titles[unit] = unit
	}
	return titles
}

// higherIsBetterUnits returns the throughput units the shell labels as "↑ better".
func higherIsBetterUnits() map[string]bool {
	units := map[string]bool{}
	for _, unit := range []string{"B/s", "kB/s", "MB/s", "GB/s", "MiB/s", "GiB/s", "op/s", "ops/s", "req/s", "reqs/s", "item/s", "items/s"} {
		units[unit] = true
	}
	return units
}

// FaviconDataURI renders the project emoji into a self-contained SVG favicon. An
// inline data URI keeps the favicon working offline and on any subpath, where a
// separate .ico would need a URL that the tool cannot know.
func FaviconDataURI(emoji string) string {
	badge := strings.TrimSpace(emoji)
	if badge == "" {
		badge = "📊"
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><text x="50%%" y="50%%" font-size="52" text-anchor="middle" dominant-baseline="central">%s</text></svg>`,
		xmlEscape(badge))
	return "data:image/svg+xml," + urlEscape(svg)
}

// xmlEscape escapes the characters that would break the favicon SVG.
func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

// urlEscape percent-encodes the characters that are unsafe in a data URI.
func urlEscape(value string) string {
	return strings.NewReplacer(
		" ", "%20",
		"<", "%3C",
		">", "%3E",
		"#", "%23",
		`"`, "%22",
		"&", "%26",
		"%", "%25",
	).Replace(value)
}
