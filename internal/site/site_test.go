// Package site. site_test - tests for the static site writer: the shell assets, the
// manifest merge (authored navigation plus generated pages plus milestones), the
// exported data index and the determinism of a rebuild.
package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// writerOptions is the option set the tests use: a demo project with one authored
// page and the standard generated pages.
func writerOptions(t *testing.T, siteDir string) Options {
	t.Helper()
	return Options{
		SiteDir:            siteDir,
		GeneratedBy:        "benchbook 0.1.0 (schema 1)",
		ProjectName:        "demo",
		Emoji:              "🧪",
		RepoURL:            "https://github.com/example/demo",
		SiteTitle:          "demo benchmarks",
		DefaultTheme:       "light",
		ThemeStorageKey:    "benchbook-theme",
		HighlightLangs:     []string{"go", "bash"},
		LoadingText:        "Loading...",
		NotFoundText:       "Not found.",
		PerformanceSection: "Performance",
		MilestonesExpanded: false,
		Nav: []NavSection{{
			Section: "Getting Started",
			Pages:   []NavPage{{Title: "Introduction", Path: "content/home.md"}},
		}},
		DefaultPath: "content/benchmarks.md",
		PageExists:  func(string) bool { return true },
		CompareOptions: CompareOptions{
			Alpha: 0.05, MinEffectPct: 1.0, MinSamples: 6,
		},
	}
}

// milestone builds a milestone run with one benchmark and a label.
func milestone(sequence int, label string, nsOp float64) *model.Run {
	run := model.NewCurrent(time.Date(2026, 3, sequence, 12, 0, 0, 0, time.UTC), model.Provenance{
		GitSHA:              strings.Repeat("a", sequence) + "bcdef",
		GitBranch:           "main",
		GoVersion:           "go1.22.5",
		GOOS:                "linux",
		GOARCH:              "amd64",
		CPUModel:            "Test CPU",
		CPUCores:            8,
		HardwareFingerprint: "fingerprint",
	})
	run.ID = "m00" + string(rune('0'+sequence))
	run.Seq = sequence
	run.Label = label
	run.Benchmarks = []model.Benchmark{{
		ID: "example.com/demo.BenchmarkConcat", Package: "example.com/demo",
		Name: "BenchmarkConcat", Workload: "Concat", Variant: model.DefaultVariant,
		Samples: []model.Sample{{Iterations: 100, NsOp: nsOp}},
	}}
	hash, err := run.ComputeContentHash()
	if err != nil {
		panic(err)
	}
	run.ContentHash = hash
	return run
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestShellIsEmbeddedAndComplete covers the go:embed contract: the shell files the
// browser needs must ship inside the binary.
func TestShellIsEmbeddedAndComplete(t *testing.T) {
	count, err := ShellFileCount()
	if err != nil {
		t.Fatalf("ShellFileCount: %v", err)
	}
	if count < 5 {
		t.Fatalf("embedded shell has %d files, want at least 5 (html, css, app.js, compare.js, vendored libs)", count)
	}
	writer := NewWriter(t.TempDir())
	written, err := writer.WriteShell()
	if err != nil {
		t.Fatalf("WriteShell: %v", err)
	}
	if len(written) != count {
		t.Errorf("WriteShell wrote %d files, want %d", len(written), count)
	}
	// The entry point is at the root; everything else lives under the asset prefix so
	// a project can keep its own index.html if it ever wants to.
	index := filepath.Join(writer.Root(), "index.html")
	if _, err := os.Stat(index); err != nil {
		t.Errorf("index.html was not published: %v", err)
	}
	for _, name := range []string{"app.js", "compare.js", "style.css", "manifest.json"} {
		if name == "manifest.json" {
			continue
		}
		path := filepath.Join(writer.Root(), ShellAssetDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not published: %v", path, err)
		}
	}
	// The vendored libraries must be present, never fetched from a CDN at runtime.
	vendored, err := os.ReadDir(filepath.Join(writer.Root(), ShellAssetDir, "vendor"))
	if err != nil {
		t.Fatalf("vendor directory missing: %v", err)
	}
	if len(vendored) == 0 {
		t.Error("the vendor directory is empty")
	}
	indexContent, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if strings.Contains(string(indexContent), "cdn.jsdelivr.net") || strings.Contains(string(indexContent), "unpkg.com") {
		t.Error("index.html references a CDN, want the vendored copies")
	}
}

// TestManifestMergesNavigationWithoutDuplicates is the "manifest merge" rule: the
// authored navigation is preserved, generated pages are appended to the performance
// section exactly once, and milestones are listed newest first in a collapsible
// section.
func TestManifestMergesNavigationWithoutDuplicates(t *testing.T) {
	options := writerOptions(t, t.TempDir())
	generated := []NavPage{
		{Title: "Benchmarks", Path: GeneratedPageBenchmarks},
		{Title: "Profiling", Path: GeneratedPageProfiling},
		{Title: "Timeline", Path: GeneratedPageTimeline},
		{Title: "Compare", Path: GeneratedPageCompare},
	}
	milestones := []*model.Run{
		milestone(1, "baseline", 100),
		milestone(2, "faster concat", 80),
		milestone(3, "another step", 70),
	}
	manifest, warnings := BuildManifest(options, milestones, generated)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	sections := map[string][]string{}
	for _, section := range manifest.Nav {
		for _, page := range section.Pages {
			sections[section.Section] = append(sections[section.Section], page.Path)
		}
	}
	if got := sections["Getting Started"]; len(got) != 1 || got[0] != "content/home.md" {
		t.Errorf("authored section = %v, want the authored page", got)
	}
	performance := sections[options.PerformanceSection]
	for _, want := range []string{GeneratedPageBenchmarks, GeneratedPageProfiling, GeneratedPageTimeline, GeneratedPageCompare} {
		if !contains(performance, want) {
			t.Errorf("performance section %v is missing %s", performance, want)
		}
	}
	milestonePaths := sections[MilestonesSection]
	if len(milestonePaths) != 3 {
		t.Fatalf("milestone pages = %v, want 3", milestonePaths)
	}
	if milestonePaths[0] != "content/milestones/m003.md" {
		t.Errorf("milestone order = %v, want newest first", milestonePaths)
	}
	if manifest.DefaultPath != GeneratedPageBenchmarks {
		t.Errorf("default path = %q, want %q", manifest.DefaultPath, GeneratedPageBenchmarks)
	}
}

// TestManifestWarnsAboutMissingAuthoredPages keeps a broken link from shipping
// silently.
func TestManifestWarnsAboutMissingAuthoredPages(t *testing.T) {
	options := writerOptions(t, t.TempDir())
	options.PageExists = func(string) bool { return false }
	_, warnings := BuildManifest(options, nil, nil)
	if len(warnings) == 0 {
		t.Fatal("no warning for a missing authored page")
	}
	if !strings.Contains(strings.Join(warnings, " "), "content/home.md") {
		t.Errorf("warnings = %v, want the missing path named", warnings)
	}
}

// TestManifestFallsBackToBenchmarksWithoutARun keeps a fresh project's site usable
// before the first run.
func TestManifestFallsBackToBenchmarksWithoutARun(t *testing.T) {
	options := writerOptions(t, t.TempDir())
	options.DefaultPath = ""
	manifest, _ := BuildManifest(options, nil, nil)
	if manifest.DefaultPath != GeneratedPageBenchmarks {
		t.Errorf("default path = %q, want %q", manifest.DefaultPath, GeneratedPageBenchmarks)
	}
	if len(manifest.Nav) == 0 {
		t.Error("manifest has no navigation")
	}
}

// TestManifestCarriesThresholdsAndTheme keeps the browser comparer honest: it must
// use the same alpha, minimum effect and minimum samples as the Go side.
func TestManifestCarriesThresholdsAndTheme(t *testing.T) {
	manifest, _ := BuildManifest(writerOptions(t, t.TempDir()), nil, nil)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	for _, want := range []string{`"alpha":0.05`, `"min_effect_pct":1`, `"min_samples":6`, `"light"`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("manifest does not contain %s:\n%s", want, encoded)
		}
	}
	if !strings.Contains(string(encoded), "favicon") || !strings.Contains(string(encoded), "data:image/svg+xml") {
		t.Error("manifest has no inline favicon")
	}
}

// TestRunEntriesExportsComparableRuns covers the data the compare page loads: one
// entry per milestone plus the current run, oldest first, each with its own file.
func TestRunEntriesExportsComparableRuns(t *testing.T) {
	milestones := []*model.Run{milestone(1, "baseline", 100), milestone(2, "faster", 80)}
	current := milestone(3, "working", 70)
	current.ID, current.Seq, current.Label = "", 0, ""
	options := compare.Options{Alpha: 0.05, MinEffectPct: 1.0, MinSamples: 6}

	entries := RunEntries(milestones, current, options)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].ID != "m001" || entries[1].ID != "m002" {
		t.Errorf("entries are not oldest-first: %s, %s", entries[0].ID, entries[1].ID)
	}
	last := entries[2]
	if last.ID != RefCurrent || !last.IsCurrent {
		t.Errorf("last entry = %q (is_current %v), want the current run", last.ID, last.IsCurrent)
	}
	if last.File != DataDirName+"/"+CurrentDataFile {
		t.Errorf("current file = %q, want %q", last.File, DataDirName+"/"+CurrentDataFile)
	}
	if entries[1].Headline["ns_op"] == 0 {
		t.Errorf("entry for m002 has no headline delta: %+v", entries[1].Headline)
	}
	// The very first milestone has nothing to compare against, so it carries no
	// headline instead of a fabricated 0%.
	if len(entries[0].Headline) != 0 {
		t.Errorf("first entry headline = %v, want none", entries[0].Headline)
	}
}

// TestWriterPublishesPagesAndData covers the file layout the shell expects.
func TestWriterPublishesPagesAndData(t *testing.T) {
	options := writerOptions(t, t.TempDir())
	writer := NewWriter(options.SiteDir)
	if _, err := writer.WriteShell(); err != nil {
		t.Fatalf("WriteShell: %v", err)
	}
	manifest, _ := BuildManifest(options, nil, nil)
	if err := writer.WriteManifest(manifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// WritePage returns a site-relative path (the form the manifest uses), so the
	// file itself is checked on disk.
	pagePath, err := writer.WritePage(GeneratedPageBenchmarks, []byte("# Benchmarks\n"))
	if err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if pagePath != GeneratedPageBenchmarks {
		t.Errorf("page path = %q, want %q", pagePath, GeneratedPageBenchmarks)
	}
	if _, err := os.Stat(filepath.Join(options.SiteDir, filepath.FromSlash(pagePath))); err != nil {
		t.Errorf("page was not written: %v", err)
	}
	index := &DataIndex{
		SchemaVersion: 1, GeneratedBy: "benchbook 0.1.0 (schema 1)",
		Options: CompareOptions{Alpha: 0.05, MinEffectPct: 1.0, MinSamples: 6},
		Metrics: MetricTitles(),
		Runs:    RunEntries([]*model.Run{milestone(1, "baseline", 100)}, nil, compare.Options{}),
	}
	if err := writer.WriteDataIndex(index); err != nil {
		t.Fatalf("WriteDataIndex: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(options.SiteDir, DataDirName, DataIndexFile))
	if err != nil {
		t.Fatalf("read data index: %v", err)
	}
	if !strings.HasSuffix(string(content), "\n") {
		t.Error("the data index does not end with a newline")
	}
	if !strings.Contains(string(content), `"runs"`) {
		t.Errorf("data index has no runs: %s", content)
	}
	// Publishing a run copies the JSON verbatim, so the compare page sees exactly
	// what the CLI wrote.
	source := filepath.Join(t.TempDir(), "current.json")
	runJSON, err := milestone(1, "baseline", 100).CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	if err := os.WriteFile(source, runJSON, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	published, err := writer.PublishRun(source, CurrentDataFile)
	if err != nil {
		t.Fatalf("PublishRun: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(options.SiteDir, filepath.FromSlash(published)))
	if err != nil {
		t.Fatalf("read published run: %v", err)
	}
	if string(copied) != string(runJSON) {
		t.Error("PublishRun changed the document")
	}
}

// TestWriterIsIdempotent is the "byte-identical for identical inputs" rule: writing
// the same site twice must not change a single byte.
func TestWriterIsIdempotent(t *testing.T) {
	options := writerOptions(t, t.TempDir())
	milestones := []*model.Run{milestone(1, "baseline", 100), milestone(2, "faster", 80)}
	build := func() map[string]string {
		writer := NewWriter(options.SiteDir)
		if _, err := writer.WriteShell(); err != nil {
			t.Fatalf("WriteShell: %v", err)
		}
		manifest, _ := BuildManifest(options, milestones, []NavPage{{Title: "Benchmarks", Path: GeneratedPageBenchmarks}})
		if err := writer.WriteManifest(manifest); err != nil {
			t.Fatalf("WriteManifest: %v", err)
		}
		if _, err := writer.WritePage(GeneratedPageTimeline, []byte("# Timeline\n")); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
		index := &DataIndex{SchemaVersion: 1, Options: CompareOptions{Alpha: 0.05}, Metrics: MetricTitles(), Runs: RunEntries(milestones, nil, compare.Options{})}
		if err := writer.WriteDataIndex(index); err != nil {
			t.Fatalf("WriteDataIndex: %v", err)
		}
		return snapshotTree(t, options.SiteDir)
	}

	first := build()
	second := build()
	if len(first) != len(second) {
		t.Fatalf("second build has %d files, want %d", len(second), len(first))
	}
	for path, content := range first {
		if second[path] != content {
			t.Errorf("%s differs between two identical builds", path)
		}
	}
}

// TestFaviconIsAnInlineSVGDataURI keeps the site self-contained: no separate icon
// request, no external image.
func TestFaviconIsAnInlineSVGDataURI(t *testing.T) {
	uri := FaviconDataURI("🧪")
	if !strings.HasPrefix(uri, "data:image/svg+xml,") {
		t.Errorf("favicon = %q, want an inline SVG data URI", uri)
	}
	if strings.Contains(uri, "<svg") {
		t.Error("favicon contains raw markup, want it percent-encoded")
	}
	if !strings.Contains(uri, "%3Csvg") {
		t.Errorf("favicon is not encoded: %q", uri)
	}
	fallback := FaviconDataURI("")
	if fallback == uri || !strings.Contains(fallback, "%3Csvg") {
		t.Errorf("empty emoji did not fall back to a default icon: %q", fallback)
	}
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// snapshotTree reads every file under root into a map, for byte comparisons.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("%s is empty", root)
	}
	return files
}

// contains reports whether a slice of strings contains a value.
func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
