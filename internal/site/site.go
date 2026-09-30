// Package site. site - the generated documentation site: the embedded shell assets,
// the JSON exported for the in-browser comparer, and the writer that publishes it
// all atomically. Only files benchbook owns are ever written; authored pages under
// content/ are never touched.
package site

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/model/canon"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// shellFS holds the vanilla HTML/CSS/JS shell plus the vendored libraries. It is
// embedded so the binary works with no network, no npm and no build step.
//
//go:embed shell
var shellFS embed.FS

// Paths and file names inside the generated site.
const (
	// shellRoot is the directory of the embedded shell inside this package.
	shellRoot = "shell"
	// ShellAssetDir is where the shell's own assets are published.
	ShellAssetDir = "assets/benchbook"
	// ContentDirName holds Markdown pages, authored and generated.
	ContentDirName = "content"
	// DataDirName holds the JSON exported for the browser.
	DataDirName = "data"
	// DataIndexFile is the run index consumed by the compare page.
	DataIndexFile = "index.json"
	// MilestoneContentPrefix is prepended to a milestone id to form its page path.
	MilestoneContentPrefix = "content/milestones/"
	// MilestonesSection is the sidebar section holding the milestone list.
	MilestonesSection = "Milestones"
	// GeneratedPageBenchmarks is the generated benchmark report path.
	GeneratedPageBenchmarks = "content/benchmarks.md"
	// GeneratedPageProfiling is the generated profile report path.
	GeneratedPageProfiling = "content/profiling.md"
	// GeneratedPageTimeline is the generated timeline path.
	GeneratedPageTimeline = "content/timeline.md"
	// GeneratedPageCompare is the generated comparer page path.
	GeneratedPageCompare = "content/compare.md"
)

// Options configure a build: both the manifest and the rendered Markdown.
type Options struct {
	// SiteDir is the root of the generated site.
	SiteDir string
	// GeneratedBy is the tool signature written into the manifest.
	GeneratedBy string
	// ProjectName, Emoji and RepoURL come from [project].
	ProjectName string
	// Emoji is the project badge.
	Emoji string
	// RepoURL is the project's repository, when known.
	RepoURL string
	// SiteTitle comes from [site].title.
	SiteTitle string
	// DefaultTheme comes from [site].default_theme.
	DefaultTheme string
	// ThemeStorageKey comes from [site].theme_storage_key.
	ThemeStorageKey string
	// HighlightLangs are the extra grammars the shell should try to load.
	HighlightLangs []string
	// LoadingText comes from [site].loading_text.
	LoadingText string
	// NotFoundText comes from [site].not_found_text.
	NotFoundText string
	// PerformanceSection is the nav section generated pages go into.
	PerformanceSection string
	// MilestonesExpanded controls whether the milestone list starts open.
	MilestonesExpanded bool
	// Nav is the authored navigation.
	Nav []NavSection
	// DefaultPath is the page shown for a bare URL.
	DefaultPath string
	// PageExists reports whether an authored page is present on disk; nil disables
	// the navigation warnings.
	PageExists func(string) bool
	// ExtraCSSSource is the optional stylesheet to copy next to the shell assets.
	ExtraCSSSource string
	// ExtraCSS is the published path of that stylesheet, empty when there is none.
	ExtraCSS string
	// CompareOptions are the thresholds exported to the browser.
	CompareOptions CompareOptions
}

// Writer publishes files below a site root.
type Writer struct {
	// root is the absolute site directory.
	root string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// NewWriter returns a Writer for a site root.
func NewWriter(siteDir string) *Writer { return &Writer{root: siteDir} }

// ----------------------------------------- Public API ----------------------------------------- //

// ShellFileCount reports how many files the embedded shell contains, used by tests
// and by the build report.
func ShellFileCount() (int, error) {
	count := 0
	err := fs.WalkDir(shellFS, shellRoot, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && isPublishableShellFile(entry.Name()) {
			count++
		}
		return nil
	})
	return count, err
}

// isPublishableShellFile filters out the shell's own documentation.
func isPublishableShellFile(name string) bool {
	return !strings.HasSuffix(name, ".md")
}

// WriteShell publishes index.html, the shell assets and the vendored libraries.
func (w *Writer) WriteShell() ([]string, error) {
	written := []string{}
	err := fs.WalkDir(shellFS, shellRoot, func(embeddedPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isPublishableShellFile(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(shellRoot, embeddedPath)
		if err != nil {
			return fmt.Errorf("resolve embedded path %s: %w", embeddedPath, err)
		}
		content, err := shellFS.ReadFile(embeddedPath)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", embeddedPath, err)
		}
		target := w.shellTarget(filepath.ToSlash(relative))
		if err := store.WriteFile(target, content); err != nil {
			return err
		}
		written = append(written, target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(written)
	return written, nil
}

// shellTarget maps an embedded shell path onto its published location: index.html
// stays at the root, everything else moves under assets/benchbook/.
func (w *Writer) shellTarget(relative string) string {
	if relative == "index.html" {
		return filepath.Join(w.root, relative)
	}
	return filepath.Join(w.root, filepath.FromSlash(ShellAssetDir), filepath.FromSlash(relative))
}

// WriteManifest publishes manifest.json.
func (w *Writer) WriteManifest(manifest *Manifest) error {
	encoded, err := canon.Encode(manifest, model.CanonicalIndent)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return store.WriteFile(filepath.Join(w.root, ManifestFile), encoded)
}

// WritePage publishes a Markdown page and returns its site-relative path.
func (w *Writer) WritePage(relativePath string, content []byte) (string, error) {
	clean := path.Clean(filepath.ToSlash(relativePath))
	if strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("refuse to write outside the site directory: %s", relativePath)
	}
	target := filepath.Join(w.root, filepath.FromSlash(clean))
	if err := store.WriteFile(target, content); err != nil {
		return "", err
	}
	return clean, nil
}

// WriteDataIndex publishes data/index.json.
func (w *Writer) WriteDataIndex(index *DataIndex) error {
	encoded, err := canon.Encode(index, model.CanonicalIndent)
	if err != nil {
		return fmt.Errorf("encode run index: %w", err)
	}
	return store.WriteFile(filepath.Join(w.root, DataDirName, DataIndexFile), encoded)
}

// PublishRun copies a run document into data/ under the given file name, so the
// browser can compare runs without a server.
func (w *Writer) PublishRun(sourcePath, fileName string) (string, error) {
	target := filepath.Join(w.root, DataDirName, filepath.Base(fileName))
	if err := store.CopyFile(sourcePath, target); err != nil {
		return "", err
	}
	return filepath.ToSlash(path.Join(DataDirName, filepath.Base(fileName))), nil
}

// CopyExtraCSS publishes the optional extra stylesheet next to the shell assets and
// returns its site-relative path.
func (w *Writer) CopyExtraCSS(source string) (string, error) {
	content, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read extra_css %s: %w", source, err)
	}
	relative := path.Join(ShellAssetDir, "extra.css")
	if err := store.WriteFile(filepath.Join(w.root, filepath.FromSlash(relative)), content); err != nil {
		return "", err
	}
	return relative, nil
}

// Root returns the absolute site directory.
func (w *Writer) Root() string { return w.root }

// RunEntries turns a history plus the working run into the index the compare page
// consumes, oldest first, with the headline figures computed between neighbours.
func RunEntries(milestones []*model.Run, current *model.Run, options compare.Options) []RunEntry {
	ordered := append([]*model.Run{}, milestones...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })

	entries := make([]RunEntry, 0, len(ordered)+1)
	for index, milestone := range ordered {
		entry := RunEntry{
			ID:       milestone.ID,
			Title:    milestone.ID + titleSuffix(milestone.Label),
			Label:    milestone.Label,
			Date:     milestone.CreatedAt.UTC().Format(dateLayout),
			Seq:      milestone.Seq,
			File:     path.Join(DataDirName, dataFileName(milestone.ID)),
			SHA:      shortHash(milestone.Provenance.GitSHA),
			Badges:   badges(milestone),
			Headline: map[string]float64{},
		}
		if index > 0 {
			entry.Headline = headlineDeltas(ordered[index-1], milestone, options)
		}
		entries = append(entries, entry)
	}
	if current != nil {
		entry := RunEntry{
			ID:        RefCurrent,
			Title:     RefCurrent,
			Label:     "",
			Date:      current.CreatedAt.UTC().Format(dateLayout),
			Seq:       0,
			File:      path.Join(DataDirName, CurrentDataFile),
			SHA:       shortHash(current.Provenance.GitSHA),
			IsCurrent: true,
			Badges:    badges(current),
			Headline:  map[string]float64{},
		}
		if len(ordered) > 0 {
			entry.Headline = headlineDeltas(ordered[len(ordered)-1], current, options)
		}
		entries = append(entries, entry)
	}
	return entries
}

// Reference keywords the shell and the CLI share.
const (
	// RefCurrent is the id of the working run in the exported data.
	RefCurrent = "current"
	// CurrentDataFile is the published file name of the working run.
	CurrentDataFile = "current.json"
)

// titleSuffix renders " · label" when a milestone has a label.
func titleSuffix(label string) string {
	if strings.TrimSpace(label) == "" {
		return ""
	}
	return " · " + label
}

// dataFileName maps a milestone id onto its published file name.
func dataFileName(id string) string { return id + ".json" }

// shortHash abbreviates a commit hash for the index.
func shortHash(hash string) string {
	if len(hash) <= indexHashLength {
		return hash
	}
	return hash[:indexHashLength]
}

// indexHashLength is how much of a commit hash the index carries.
const indexHashLength = 7

// dateLayout is the date format used in exported data.
const dateLayout = "2006-01-02"

// badges renders the warning badges of a run for the index.
func badges(run *model.Run) []string {
	out := []string{}
	if run.Provenance.GitDirty {
		out = append(out, "dirty")
	}
	if run.Forced {
		out = append(out, "forced")
	}
	if run.IndicativeOnly {
		out = append(out, "indicative-only")
	}
	return out
}

// headlineDeltas computes the geomean change per metric between two runs.
func headlineDeltas(previous, current *model.Run, options compare.Options) map[string]float64 {
	comparison := compare.Between(previous, current, options)
	deltas := map[string]float64{}
	for _, geomean := range comparison.Geomeans {
		if geomean.Count == 0 {
			continue
		}
		deltas[geomean.Metric] = geomean.DeltaPct
	}
	return deltas
}
