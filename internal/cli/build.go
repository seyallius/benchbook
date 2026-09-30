// Package cli. build - the `benchbook build` command: render every derived page and
// publish the documentation shell, the manifest and the browser data. It is the one
// place that turns JSON into files, so `run` and `milestone` end by calling it.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
	"github.com/seyallius/benchbook/internal/site"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the build command.
const (
	// flagQuiet only reports the summary line, for pre-commit hooks and CI.
	flagQuiet = "quiet"
)

// buildOptions holds the parsed flags of `benchbook build`.
type buildOptions struct {
	// quiet only reports the summary line.
	quiet bool
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newBuildCommand builds `benchbook build`.
func (a *App) newBuildCommand() *cobra.Command {
	options := buildOptions{}
	cmd := &cobra.Command{
		Use:     cmdBuild,
		GroupID: groupPublish,
		Short:   "Regenerate every derived page and the documentation shell",
		Long: `Regenerate the whole documentation site from the JSON that is the source of truth:
the benchmark report, the profiling tables, the timeline with its charts, the compare
page, one page per milestone and the exported data the compare page reads.

The build is deterministic: the same history always produces the same bytes, so a
rebuild that changes nothing produces no diff. Authored pages under the site's
content directory are never touched.`,
		Example: `  benchbook build              # report what was written
  benchbook build --quiet      # one line, for a pre-commit hook`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runBuild(options)
		},
	}
	cmd.Flags().BoolVar(&options.quiet, flagQuiet, false, "only print the summary line")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runBuild regenerates the whole site.
func (a *App) runBuild(options buildOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	return a.buildSiteQuiet(cfg, runs, options.quiet)
}

// buildSite renders and publishes the site, reporting what it wrote.
func (a *App) buildSite(cfg *config.Config, runs *store.Store) error {
	return a.buildSiteQuiet(cfg, runs, false)
}

// buildSiteQuiet is the shared implementation behind `build`, `run` and `milestone`.
// It tolerates a missing current run (a fresh clone with only milestones) and never
// deletes anything: milestones are immutable, so their pages can never go stale.
func (a *App) buildSiteQuiet(cfg *config.Config, runs *store.Store, quiet bool) error {
	current, err := runs.LoadCurrent()
	if err != nil {
		if !isMissingCurrent(err) {
			return err
		}
		current = nil
	}
	milestones, err := runs.Milestones()
	if err != nil {
		return err
	}
	if current == nil && len(milestones) == 0 {
		return &usageError{err: fmt.Errorf("nothing to document: record a run with `benchbook run` first")}
	}

	ctx := renderContext(cfg)
	writer := site.NewWriter(cfg.SiteDir())
	written := []string{}

	shellFiles, err := writer.WriteShell()
	if err != nil {
		return err
	}
	written = append(written, shellFiles...)

	dataExport, err := a.publishData(cfg, runs, milestones, current)
	if err != nil {
		return err
	}
	written = append(written, dataExport...)

	pages, err := a.publishPages(ctx, cfg, writer, milestones, current)
	if err != nil {
		return err
	}
	written = append(written, pages...)

	manifest, warnings := site.BuildManifest(a.manifestOptions(cfg, current), milestones, generatedNav())
	if err := writer.WriteManifest(manifest); err != nil {
		return err
	}
	written = append(written, site.ManifestFile)
	for _, warning := range warnings {
		a.warnf("%s", warning)
	}

	if quiet {
		a.printf("built %s (%d files)\n", relativeTo(cfg, cfg.SiteDir()), len(written))
		return nil
	}
	a.printf("📖 built %s — %s, %s\n",
		relativeTo(cfg, cfg.SiteDir()),
		render.Pluralize(len(milestones), "milestone"),
		render.Pluralize(len(written), "file"))
	a.info("serve it with: benchbook serve")
	return nil
}

// isMissingCurrent reports whether a load failure is simply "no run recorded".
func isMissingCurrent(err error) bool { return errors.Is(err, store.ErrNoCurrent) }

// manifestOptions assembles the site options from the configuration.
func (a *App) manifestOptions(cfg *config.Config, current *model.Run) site.Options {
	extraCSS := ""
	if strings.TrimSpace(cfg.Site.ExtraCSS) != "" {
		extraCSS = site.ShellAssetDir + "/extra.css"
	}
	options := site.Options{
		SiteDir:            cfg.SiteDir(),
		GeneratedBy:        versionLine(),
		ProjectName:        cfg.Project.Name,
		Emoji:              cfg.Project.Emoji,
		RepoURL:            cfg.Project.RepoURL,
		SiteTitle:          cfg.Site.Title,
		DefaultTheme:       cfg.Site.DefaultTheme,
		ThemeStorageKey:    cfg.Site.ThemeStorageKey,
		HighlightLangs:     cfg.Site.HighlightLangs,
		LoadingText:        cfg.Site.LoadingText,
		NotFoundText:       cfg.Site.NotFoundText,
		PerformanceSection: performanceSection(cfg),
		MilestonesExpanded: false,
		Nav:                navFromConfig(cfg),
		DefaultPath:        site.GeneratedPageBenchmarks,
		ExtraCSS:           extraCSS,
		CompareOptions: site.CompareOptions{
			Alpha:        cfg.Stats.Alpha,
			MinEffectPct: cfg.Stats.MinEffectPct,
			MinSamples:   cfg.Stats.MinSamples,
		},
		PageExists: func(relative string) bool {
			info, err := os.Stat(filepath.Join(cfg.SiteDir(), filepath.FromSlash(relative)))
			return err == nil && !info.IsDir()
		},
	}
	if strings.TrimSpace(cfg.Site.ExtraCSS) != "" {
		options.ExtraCSSSource = a.projectPath(cfg, cfg.Site.ExtraCSS)
	}
	return options
}

// performanceSection returns the configured nav section of the generated pages.
func performanceSection(cfg *config.Config) string {
	if strings.TrimSpace(cfg.Site.PerformanceSection) != "" {
		return cfg.Site.PerformanceSection
	}
	return site.DefaultPerformanceSection
}

// navFromConfig converts the authored navigation into manifest sections.
func navFromConfig(cfg *config.Config) []site.NavSection {
	sections := make([]site.NavSection, 0, len(cfg.Nav))
	for _, section := range cfg.Nav {
		converted := site.NavSection{Section: section.Section, Expanded: true, Pages: []site.NavPage{}}
		for _, page := range section.Pages {
			converted.Pages = append(converted.Pages, site.NavPage{Title: page.Title, Path: page.Path})
		}
		sections = append(sections, converted)
	}
	return sections
}

// generatedNav lists the pages the tool generates, in the order they appear.
func generatedNav() []site.NavPage {
	return []site.NavPage{
		{Title: "Benchmarks", Path: site.GeneratedPageBenchmarks},
		{Title: "Profiling", Path: site.GeneratedPageProfiling},
		{Title: "Timeline", Path: site.GeneratedPageTimeline},
		{Title: "Compare", Path: site.GeneratedPageCompare},
	}
}

// renderContext maps the configuration onto the render package's own shape, so
// renderers never import the config package.
func renderContext(cfg *config.Config) render.Context {
	categories := make([]render.Category, 0, len(cfg.Categories))
	for _, category := range cfg.Categories {
		categories = append(categories, render.Category{Title: category.Title, Match: category.Match})
	}
	return render.Context{
		ProjectName:     cfg.Project.Name,
		BaselineVariant: cfg.Bench.BaselineVariant,
		Categories:      categories,
		DefaultCategory: render.Category{Title: cfg.CategoryDefault.Title, Match: cfg.CategoryDefault.Match},
		Tracked:         cfg.Tracked.Benchmarks,
		ProfileTop:      cfg.Profile.Top,
		NoiseFloorNote:  cfg.Stats.NoiseFloorNote,
		Options: compare.Options{
			Alpha:        cfg.Stats.Alpha,
			MinEffectPct: cfg.Stats.MinEffectPct,
			MinSamples:   cfg.Stats.MinSamples,
		},
	}
}

// publishPages renders every owned Markdown page.
func (a *App) publishPages(ctx render.Context, cfg *config.Config, writer *site.Writer, milestones []*model.Run, current *model.Run) ([]string, error) {
	written := []string{}
	latest := latestOf(milestones)
	options := ctx.Options

	if current != nil {
		reference := render.Reference{Label: "vs last milestone", Run: latest}
		if latest == nil {
			reference = render.Reference{}
		}
		page := render.BenchmarksPage(ctx, current, reference, options)
		path, err := writer.WritePage(site.GeneratedPageBenchmarks, []byte(page))
		if err != nil {
			return nil, err
		}
		written = append(written, path)
	} else if latest != nil {
		page := render.BenchmarksPage(ctx, latest, referenceFor(milestones, latest), options)
		path, err := writer.WritePage(site.GeneratedPageBenchmarks, []byte(page))
		if err != nil {
			return nil, err
		}
		written = append(written, path)
	}

	profileRun := current
	profileReference := latest
	if profileRun == nil {
		profileRun = latest
		profileReference = previousOf(milestones, latest)
	}
	profilePage := render.ProfilingPage(ctx, profileRun, profileReference)
	path, err := writer.WritePage(site.GeneratedPageProfiling, []byte(profilePage))
	if err != nil {
		return nil, err
	}
	written = append(written, path)

	timelinePage := render.TimelinePage(ctx, milestones, current)
	path, err = writer.WritePage(site.GeneratedPageTimeline, []byte(timelinePage))
	if err != nil {
		return nil, err
	}
	written = append(written, path)

	path, err = writer.WritePage(site.GeneratedPageCompare, []byte(comparePageMarkdown()))
	if err != nil {
		return nil, err
	}
	written = append(written, path)

	for _, milestone := range milestones {
		page := render.BenchmarksPage(ctx, milestone, referenceFor(milestones, milestone), options)
		page += render.ProfilingPage(ctx, milestone, previousOf(milestones, milestone))
		pagePath, err := writer.WritePage(site.MilestoneContentPrefix+milestone.ID+".md", []byte(page))
		if err != nil {
			return nil, err
		}
		written = append(written, pagePath)
	}
	return written, nil
}

// referenceFor builds the reference of a milestone: the one before it, or none for
// the first.
func referenceFor(milestones []*model.Run, milestone *model.Run) render.Reference {
	previous := previousOf(milestones, milestone)
	if previous == nil {
		return render.Reference{}
	}
	return render.Reference{Label: "vs " + previous.ID, Run: previous}
}

// previousOf returns the milestone recorded before the given one.
func previousOf(milestones []*model.Run, milestone *model.Run) *model.Run {
	if milestone == nil {
		return nil
	}
	var previous *model.Run
	for _, candidate := range milestones {
		if candidate.Seq >= milestone.Seq {
			break
		}
		previous = candidate
	}
	return previous
}

// latestOf returns the highest-sequence milestone.
func latestOf(milestones []*model.Run) *model.Run {
	if len(milestones) == 0 {
		return nil
	}
	return milestones[len(milestones)-1]
}

// comparePageMarkdown renders the static page that hosts the interactive comparer.
// The mount point is a plain div: the shell fills it from the exported JSON.
func comparePageMarkdown() string {
	var builder strings.Builder
	builder.WriteString(render.GeneratedHeader + "\n\n")
	builder.WriteString("# Compare runs\n\n")
	builder.WriteString("Pick two runs and a metric. The classification uses the same procedure as the\n")
	builder.WriteString("command line: a two-sided Mann-Whitney U test with a minimum effect, so a\n")
	builder.WriteString("percentage is only labelled when it is both significant and large enough to matter.\n\n")
	builder.WriteString("The selection is part of the URL, so a comparison can be linked or bookmarked.\n\n")
	builder.WriteString(`<div id="bb-compare">Loading comparison data…</div>` + "\n")
	return builder.String()
}

// publishData exports the run index and the run documents the browser needs.
func (a *App) publishData(cfg *config.Config, runs *store.Store, milestones []*model.Run, current *model.Run) ([]string, error) {
	writer := site.NewWriter(cfg.SiteDir())
	index := &site.DataIndex{
		SchemaVersion: site.ManifestSchemaVersion,
		GeneratedBy:   versionLine(),
		Options: site.CompareOptions{
			Alpha:        cfg.Stats.Alpha,
			MinEffectPct: cfg.Stats.MinEffectPct,
			MinSamples:   cfg.Stats.MinSamples,
		},
		Metrics: site.MetricTitles(),
		Runs:    site.RunEntries(milestones, current, compareThresholds(cfg)),
	}
	written := []string{}
	for _, entry := range index.Runs {
		var source string
		switch {
		case entry.IsCurrent:
			source = runs.CurrentPath()
		default:
			source = filepath.Join(runs.HistoryPath(), entry.ID+".json")
			if _, err := os.Stat(source); err != nil {
				// The file name carries the label slug; find the real one.
				source = a.milestonePath(runs, entry.Seq)
			}
		}
		published, err := writer.PublishRun(source, filepath.Base(entry.File))
		if err != nil {
			return nil, err
		}
		written = append(written, published)
	}
	if err := writer.WriteDataIndex(index); err != nil {
		return nil, err
	}
	written = append(written, filepath.ToSlash(filepath.Join(site.DataDirName, site.DataIndexFile)))

	if strings.TrimSpace(cfg.Site.ExtraCSS) != "" {
		relative, err := writer.CopyExtraCSS(a.projectPath(cfg, cfg.Site.ExtraCSS))
		if err != nil {
			return nil, err
		}
		written = append(written, relative)
	}
	return written, nil
}

// milestonePath resolves the on-disk path of a milestone by sequence number, which
// tolerates a hand-edited label (and therefore a missing slug).
func (a *App) milestonePath(runs *store.Store, sequence int) string {
	milestones, err := runs.Milestones()
	if err != nil {
		return filepath.Join(runs.HistoryPath(), fmt.Sprintf("m%03d.json", sequence))
	}
	for _, milestone := range milestones {
		if milestone.Seq == sequence {
			return filepath.Join(runs.HistoryPath(), milestoneFileNameFor(milestone))
		}
	}
	return filepath.Join(runs.HistoryPath(), fmt.Sprintf("m%03d.json", sequence))
}

// milestoneFileNameFor reconstructs the file name of a milestone from its data.
func milestoneFileNameFor(milestone *model.Run) string {
	slug := model.Slug(milestone.Label)
	if slug == "" {
		return milestone.ID + ".json"
	}
	return milestone.ID + "-" + slug + ".json"
}
