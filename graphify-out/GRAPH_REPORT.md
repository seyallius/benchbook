# Graph Report - benchbook  (2026-09-30)

## Corpus Check
- 100 files · ~94,562 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 7 file(s) not represented in the graph (top: (none) 3, .css 3, .toml 1)

## Summary
- 1295 nodes · 3794 edges · 96 communities (50 shown, 46 thin omitted)
- Extraction: 93% EXTRACTED · 7% INFERRED · 0% AMBIGUOUS · INFERRED: 265 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Core implementation
- benchmark comparison
- vendored frontend assets
- data models
- Core implementation
- Core implementation
- Core implementation
- benchmark comparison
- benchmark rendering
- Core implementation
- benchmark comparison
- benchmark rendering
- configuration
- Core implementation
- parsing
- chart generation
- data models
- benchmark comparison
- benchmark comparison
- command line interface
- environment collection
- environment collection
- benchmark comparison
- configuration
- benchmark comparison
- configuration
- command line interface
- configuration
- benchmark comparison
- command line interface
- command line interface
- benchmark comparison
- environment collection
- parsing
- data models
- command line interface
- environment collection
- vendored frontend assets
- command line interface
- benchmark rendering
- Core implementation
- Core implementation
- command line interface
- benchmark comparison
- benchmark comparison
- benchmark rendering
- command line interface
- vendored frontend assets
- command line interface
- command line interface
- documentation site
- Core implementation
- command line interface
- Core implementation
- environment collection
- command line interface
- benchmark rendering
- data models
- benchmark comparison
- Core implementation
- Core implementation
- documentation site
- Core implementation
- Core implementation
- Core implementation
- Core implementation
- Core implementation
- Core implementation
- Core implementation
- environment collection
- profiling
- profiling
- benchmark rendering
- benchmark rendering
- benchmark comparison
- benchmark rendering
- benchmark rendering
- Core implementation
- benchmark rendering
- benchmark rendering
- Core implementation
- Core implementation
- command line interface
- Core implementation
- Core implementation
- documentation site
- benchmark comparison
- vendored frontend assets
- data models
- data models

## God Nodes (most connected - your core abstractions)
1. `Run` - 87 edges
2. `Config` - 52 edges
3. `Store` - 33 edges
4. `Between()` - 31 edges
5. `se` - 28 edges
6. `Comparison` - 26 edges
7. `Context` - 26 edges
8. `Profile` - 25 edges
9. `ie` - 25 edges
10. `newProject()` - 22 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `New()`  [EXTRACTED]
  cmd/benchbook/main.go → internal/cli/cli.go
- `Current Versus M003 Regression Summary` --semantically_similar_to--> `Benchmark Comparison JSON`  [INFERRED] [semantically similar]
  internal/render/testdata/golden/summary-current-regression.md → internal/render/testdata/golden/summary-json.md
- `Benchmark Comparison Markdown` --semantically_similar_to--> `Benchmark Comparison JSON`  [INFERRED] [semantically similar]
  internal/render/testdata/golden/summary-markdown.md → internal/render/testdata/golden/summary-json.md
- `Benchmark Comparison Text` --semantically_similar_to--> `Benchmark Comparison JSON`  [INFERRED] [semantically similar]
  internal/render/testdata/golden/summary-text.md → internal/render/testdata/golden/summary-json.md
- `relativeTo()` --calls--> `relativePath()`  [INFERRED]
  internal/cli/run.go → internal/cli/session.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Benchmark comparison represented in JSON, Markdown, and text formats** — internal_render_testdata_golden_summary_json_benchmark_comparison_json, internal_render_testdata_golden_summary_markdown_benchmark_comparison_markdown, internal_render_testdata_golden_summary_text_benchmark_comparison_text [INFERRED 0.95]
- **Benchmark progression across baseline, optimization, and instrumentation milestones** — internal_render_testdata_golden_timeline_m001_baseline, internal_render_testdata_golden_timeline_m002_sync_pool_optimization, internal_render_testdata_golden_timeline_m003_instrumentation_build [EXTRACTED 1.00]
- **Offline documentation shell and embedded vendor assets** — internal_site_shell_index_documentation_shell, internal_site_shell_vendor_vendored_assets, internal_site_shell_vendor_offline_embedded_assets [INFERRED 0.85]

## Communities (96 total, 46 thin omitted)

### Community 0 - "Core implementation"
Cohesion: 0.11
Nodes (19): Ordered, usageError, incomparable(), short(), marshalString(), marshalValue(), SortedKeys(), Options (+11 more)

### Community 1 - "benchmark comparison"
Cohesion: 0.07
Nodes (78): benchmarkSpec, Between(), fixtureRun(), jitter(), metricResult(), TestFindingsAreOrderedByMagnitude(), TestForceDoesNotHideTheMismatch(), TestGeomeanExcludesUndefinedPairs() (+70 more)

### Community 2 - "vendored frontend assets"
Cohesion: 0.06
Nodes (36): a(), b(), be(), d(), e(), f(), b(), c() (+28 more)

### Community 3 - "data models"
Cohesion: 0.06
Nodes (41): hasProfile(), mergeProfiles(), rawProfileName(), rawProfileNameFor(), sanitizeTarget(), DecodeRun(), Run, Migrate() (+33 more)

### Community 4 - "Core implementation"
Cohesion: 0.07
Nodes (38): generatedNav(), Encode(), Sort(), BuildManifest(), defaultPath(), FaviconDataURI(), NavPage, higherIsBetterUnits() (+30 more)

### Community 5 - "Core implementation"
Cohesion: 0.10
Nodes (46): addCopyButtons(), addHeadingAnchors(), appendCell(), applyFilter(), applySiteSettings(), applyTheme(), bindGlobalHandlers(), boot() (+38 more)

### Community 6 - "Core implementation"
Cohesion: 0.16
Nodes (29): project, main(), isCharDevice(), New(), benchmarkText(), formatFloat(), formatIterations(), itoa() (+21 more)

### Community 7 - "benchmark comparison"
Cohesion: 0.09
Nodes (28): milestoneOptions, sparseBenchmark, comparePageMarkdown(), latestOf(), previousOf(), referenceFor(), normalizeLabel(), shortSpec() (+20 more)

### Community 8 - "benchmark rendering"
Cohesion: 0.20
Nodes (28): MetricGeomean, DisplayName(), Table(), buildChart(), capNames(), chartSection(), cumulativeFactor(), findByName() (+20 more)

### Community 9 - "Core implementation"
Cohesion: 0.12
Nodes (29): NewCurrent(), alternatingSign(), fixtureBaseline(), fixtureContext(), fixtureCurrent(), fixtureImproved(), fixtureOtherMachine(), fixtureProfiles() (+21 more)

### Community 10 - "benchmark comparison"
Cohesion: 0.14
Nodes (29): clampProbability(), combinations(), compareRuns(), direction(), displayName(), erfc(), erfcContinuedFraction(), erfSeries() (+21 more)

### Community 11 - "benchmark rendering"
Cohesion: 0.10
Nodes (28): profileDelta(), SummaryCell(), FormatSpread(), clampProbability(), combinations(), exactTwoSidedP(), Summary, HalfRangeOverMedian() (+20 more)

### Community 12 - "configuration"
Cohesion: 0.15
Nodes (23): Options, parsedSample, Parser, SortBenchmarks(), applyMetric(), containsWordSequence(), floatPtr(), GroupRule (+15 more)

### Community 13 - "Core implementation"
Cohesion: 0.08
Nodes (3): ne(), se, te()

### Community 14 - "parsing"
Cohesion: 0.13
Nodes (8): App, Store, milestoneFileName(), milestoneID(), New(), ParseMilestoneID(), seqOf(), writeFileAtomic()

### Community 15 - "chart generation"
Cohesion: 0.23
Nodes (22): chartAxis(), chartBreaks(), chartGrid(), chartLegend(), chartSeriesMarkup(), escapeAttribute(), escapeTextXML(), formatFloat() (+14 more)

### Community 16 - "data models"
Cohesion: 0.11
Nodes (3): ie, W(), Y()

### Community 17 - "benchmark comparison"
Cohesion: 0.08
Nodes (16): compare, erfCases, faster, fixtures, here, improvement, options, regression (+8 more)

### Community 18 - "benchmark comparison"
Cohesion: 0.13
Nodes (21): Presence, metricOf(), direction(), geomeans(), BenchmarkComparison, Finding, MetricResult, headline() (+13 more)

### Community 19 - "command line interface"
Cohesion: 0.12
Nodes (15): profileOptions, serveOptions, cleanupProfileArtefacts(), App, profileArtefactPaths(), profileLabel(), profileTarget(), profileTargetName() (+7 more)

### Community 20 - "environment collection"
Cohesion: 0.13
Nodes (5): OSEnvironment, StaticEnvironment, OSEnvironment, OSEnvironment, OSEnvironment

### Community 21 - "environment collection"
Cohesion: 0.21
Nodes (20): LocalOS(), New(), statusPath(), collectWithStatus(), staticEnvironment(), TestCleanWhenOnlyGeneratedFilesChanged(), TestCollectIncludesHostnameOnlyWhenAsked(), TestCollectReadsGitState() (+12 more)

### Community 22 - "benchmark comparison"
Cohesion: 0.22
Nodes (18): Options, banner(), benchmarkSections(), BenchmarksPage(), comparisonIndex(), Reference, metaLine(), notesBlock() (+10 more)

### Community 23 - "configuration"
Cohesion: 0.14
Nodes (8): CategoryConfig, navFromConfig(), performanceSection(), headlineText(), compareThresholds(), versionLine(), Config, NavSection

### Community 24 - "benchmark comparison"
Cohesion: 0.16
Nodes (19): deltaCell(), pCell(), padCell(), renderColumns(), runLabel(), Badge(), benchmarkPhrase(), geomeanSnippet() (+11 more)

### Community 25 - "configuration"
Cohesion: 0.13
Nodes (13): BenchConfig, GroupRule, NavPage, NavSection, PathsConfig, ProfileConfig, ProjectConfig, SiteConfig (+5 more)

### Community 26 - "command line interface"
Cohesion: 0.16
Nodes (6): ProfileTopCommand(), Fake, Command, CommandError, FakeAnswer, Result

### Community 27 - "configuration"
Cohesion: 0.12
Nodes (17): findConfigDir(), Decode(), Defaults(), joinKeys(), Load(), Sample(), TestBenchTimeoutParsesGoDurations(), TestCategoryForUsesFirstMatch() (+9 more)

### Community 28 - "benchmark comparison"
Cohesion: 0.24
Nodes (4): classificationColor(), geomeanCount(), App, Comparison

### Community 29 - "command line interface"
Cohesion: 0.14
Nodes (13): rawRequest, runOptions, benchArgsString(), defaultFilter(), App, groupRules(), pluralSuffix(), rawSource() (+5 more)

### Community 30 - "command line interface"
Cohesion: 0.24
Nodes (13): BenchCommand(), BenchSpec, ProfileSpec, SampleIndexFor(), argsAfter(), benchFixture(), contains(), profileFixture() (+5 more)

### Community 31 - "benchmark comparison"
Cohesion: 0.26
Nodes (14): compareBenchmarks(), evaluate(), evaluateMissing(), evaluateWithoutNumbers(), metricsOfBoth(), MetricsOf(), OrderedMetrics(), SampleValue() (+6 more)

### Community 32 - "environment collection"
Cohesion: 0.14
Nodes (8): Options, fallbackGoVersion(), OSEnvironment, NewEnvironmentWithRunner(), NewLocalEnvironment(), Runner, NewExec(), Exec

### Community 35 - "command line interface"
Cohesion: 0.27
Nodes (7): exactArgs(), flagError(), App, noArgs(), pluralizeCount(), quoteList(), unknownCommandError()

### Community 36 - "environment collection"
Cohesion: 0.23
Nodes (4): Options, Environment, Marshal(), writeCanonical()

### Community 37 - "vendored frontend assets"
Cohesion: 0.21
Nodes (3): ee(), re, t()

### Community 38 - "command line interface"
Cohesion: 0.21
Nodes (7): buildOptions, App, isMissingCurrent(), milestoneFileNameFor(), renderContext(), Slug(), TestSlugTurnsLabelsIntoFileNames()

### Community 40 - "Core implementation"
Cohesion: 0.22
Nodes (3): exact_vector(), main(), normal_vector()

### Community 41 - "Core implementation"
Cohesion: 0.20
Nodes (10): code(), countOrUnknown(), dirtyText(), EscapeCell(), HardwareBlock(), memoryOrUnknown(), TestTableKeepsInlineCodeFences(), TestTablesEscapeCellContent() (+2 more)

### Community 42 - "command line interface"
Cohesion: 0.20
Nodes (8): benchbook profile Command, Profiling M001 Baseline, Profiling M002, M001 Baseline, M002 sync.Pool Optimization, M003 Instrumentation Build, Single Milestone Timeline, Benchmark Timeline

### Community 43 - "benchmark comparison"
Cohesion: 0.22
Nodes (9): Current Versus M003 Regression Summary, Benchmark Comparison JSON, Concat Benchmark, FindUser Result Benchmark, FindUser Traditional Benchmark, Geometric Mean of Median Ratios, ParseJSON Benchmark, Benchmark Comparison Markdown (+1 more)

### Community 44 - "benchmark comparison"
Cohesion: 0.29
Nodes (5): compareOptions, App, resolveRun(), NormalizeMetric(), TestNormalizeMetricResolvesAliases()

### Community 45 - "benchmark rendering"
Cohesion: 0.32
Nodes (6): summaryOptions, App, runID(), Summary(), summaryJSON(), Format

### Community 46 - "command line interface"
Cohesion: 0.39
Nodes (4): Collector, isIgnored(), GitCommand(), TestGitCommandKeepsArgumentsVerbatim()

### Community 47 - "vendored frontend assets"
Cohesion: 0.57
Nodes (6): checksum_of(), download_all(), main(), vendor.sh script, verify_all(), verify_entry()

### Community 48 - "command line interface"
Cohesion: 0.47
Nodes (3): initOptions, containsLine(), App

### Community 49 - "command line interface"
Cohesion: 0.47
Nodes (3): listOptions, App, milestoneLabelWithBadges()

### Community 50 - "documentation site"
Cohesion: 0.50
Nodes (4): Documentation Shell, highlight.js Vendor Bundle, marked Vendor Bundle, Vendored Assets

### Community 51 - "Core implementation"
Cohesion: 0.50
Nodes (3): npx, graphify, @dreamtree-org/graphify

### Community 54 - "environment collection"
Cohesion: 0.67
Nodes (3): CPU Profile, Heap Space Profile, Profiling Current Run

## Knowledge Gaps
- **74 isolated node(s):** `npx`, `@dreamtree-org/graphify`, `github.com/seyallius/benchbook`, `App`, `benchmarkSpec` (+69 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 207 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **46 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Run` connect `benchmark comparison` to `Core implementation`, `benchmark comparison`, `data models`, `Core implementation`, `command line interface`, `benchmark rendering`, `Core implementation`, `Core implementation`, `benchmark comparison`, `benchmark rendering`, `parsing`, `command line interface`, `benchmark comparison`, `benchmark comparison`, `configuration`, `benchmark comparison`, `benchmark comparison`, `command line interface`?**
  _High betweenness centrality (0.065) - this node is a cross-community bridge._
- **Why does `Config` connect `configuration` to `command line interface`, `benchmark rendering`, `benchmark comparison`, `benchmark comparison`, `parsing`, `command line interface`, `command line interface`, `configuration`, `configuration`, `benchmark comparison`, `command line interface`?**
  _High betweenness centrality (0.021) - this node is a cross-community bridge._
- **Why does `Profile` connect `data models` to `Core implementation`, `benchmark comparison`, `Core implementation`, `command line interface`, `benchmark comparison`?**
  _High betweenness centrality (0.020) - this node is a cross-community bridge._
- **What connects `npx`, `@dreamtree-org/graphify`, `github.com/seyallius/benchbook` to the rest of the system?**
  _74 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Core implementation` be split into smaller, more focused modules?**
  _Cohesion score 0.10513296227581942 - nodes in this community are weakly interconnected._
- **Should `benchmark comparison` be split into smaller, more focused modules?**
  _Cohesion score 0.06610644257703081 - nodes in this community are weakly interconnected._
- **Should `vendored frontend assets` be split into smaller, more focused modules?**
  _Cohesion score 0.06151062867480778 - nodes in this community are weakly interconnected._