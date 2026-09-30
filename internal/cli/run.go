// Package cli. run - the `benchbook run` command: gather provenance, obtain raw
// benchmark output (by executing `go test` or by ingesting a file), parse it,
// overwrite current.json, print the status summary and rebuild the site.
package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/collect"
	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/parse/gotest"
	"github.com/seyallius/benchbook/internal/runner"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flags of the run command.
const (
	// flagCount overrides the configured sample count.
	flagCount = "count"
	// flagFilter narrows the run to benchmarks matching a regex.
	flagFilter = "filter"
	// flagQuick records a single-sample, indicative-only run.
	flagQuick = "quick"
	// flagFromFile ingests raw output instead of executing the toolchain.
	flagFromFile = "from-file"
	// flagNoHW records provenance without probing the hardware.
	flagNoHW = "no-hw"
	// flagNoBuild skips the site rebuild that normally follows a run.
	flagNoBuild = "no-build"
)

// Raw output file names. They are stable on purpose: raw output is disposable and
// regenerable, exactly like current.json, so a new run replaces the old text
// instead of littering the directory.
const (
	// rawRunFile holds the last `go test -bench` output.
	rawRunFile = "last-run.txt"
	// rawProfilePrefix is prepended to a profile's (kind, target) to form its file name.
	rawProfilePrefix = "last-profile"
)

// quickCount is the sample count of an indicative-only run.
const quickCount = 1

// runOptions holds the parsed flags of `benchbook run`.
type runOptions struct {
	// count is the number of samples per benchmark; 0 means "use the config".
	count int
	// filter narrows the benchmarks, as passed to go test -bench.
	filter string
	// quick records a single sample per benchmark.
	quick bool
	// fromFile ingests raw output instead of running the toolchain.
	fromFile string
	// noHW skips the hardware probes.
	noHW bool
	// noBuild skips regenerating the site afterwards.
	noBuild bool
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newRunCommand builds `benchbook run`.
func (a *App) newRunCommand() *cobra.Command {
	options := runOptions{}
	cmd := &cobra.Command{
		Use:     cmdRun,
		GroupID: groupMeasure,
		Short:   "Run the benchmarks and record the working run",
		Long: `Run ` + "`go test -run '^$' -bench=. -benchmem -count=N`" + ` over bench.packages, record the
result as benchbook/current.json together with its provenance (git state, machine,
Go version, exact flags), then rebuild the documentation site.

The working run is overwritten by every run: it is a measurement, not a record. Use
` + "`benchbook milestone`" + ` to promote it into the immutable history.

--from-file ingests a capture produced elsewhere (a container, a benchmark machine)
and keeps the text verbatim, so a report can always be traced back to what the
toolchain actually printed. Run it on the machine you are documenting; a shared CI
runner is not one.`,
		Example: `  benchbook run                                   # config defaults, then rebuild the site
  benchbook run --count 10 --filter FindUser      # more samples, one benchmark
  benchbook run --quick --no-build                # one sample while iterating
  benchbook run --from-file bench.txt             # ingest a capture from another machine`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runRun(cmd.Context(), options)
		},
	}
	cmd.Flags().IntVar(&options.count, flagCount, 0, "samples per benchmark (default: bench.count from the config)")
	cmd.Flags().StringVar(&options.filter, flagFilter, "", "regex of benchmarks to run, passed to go test -bench")
	cmd.Flags().BoolVar(&options.quick, flagQuick, false, "single sample, indicative only (cannot be promoted)")
	cmd.Flags().StringVar(&options.fromFile, flagFromFile, "", "read raw \"go test -bench\" output from a file instead of running the toolchain")
	cmd.Flags().BoolVar(&options.noHW, flagNoHW, false, "skip the hardware probes and record unknown hardware")
	cmd.Flags().BoolVar(&options.noBuild, flagNoBuild, false, "do not rebuild the documentation site afterwards")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runRun records a new working run.
func (a *App) runRun(ctx context.Context, options runOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	if err := runs.EnsureDirs(); err != nil {
		return err
	}

	samples, err := resolveSampleCount(cfg, options.count, options.quick)
	if err != nil {
		return err
	}
	if options.quick && options.count > 0 && options.count != quickCount {
		a.warnf("--quick forces a single sample; ignoring --count=%d", options.count)
	}

	raw, benchArgs, err := a.obtainRawOutput(ctx, cfg, runs, rawRequest{
		samples:  samples,
		filter:   options.filter,
		fromFile: options.fromFile,
	})
	if err != nil {
		return err
	}

	provenance, err := a.collectProvenance(ctx, cfg, benchArgs, options.noHW)
	if err != nil {
		return err
	}
	parsed, err := gotest.New(gotest.Options{
		Source:     rawSource(options.fromFile),
		GroupRules: groupRules(cfg),
	}).Parse(bytes.NewReader(raw))
	if err != nil {
		return err
	}

	run := model.NewCurrent(a.now(), provenance)
	run.Benchmarks = parsed
	run.IndicativeOnly = options.quick || samples <= quickCount
	hash, err := run.ComputeContentHash()
	if err != nil {
		return err
	}
	run.ContentHash = hash
	if err := runs.SaveCurrent(run); err != nil {
		return err
	}

	a.printf("🦀 recorded %s (%s) in %s\n",
		renderPluralBenchmarks(len(run.Benchmarks)), sampleSummary(run), relativeTo(cfg, runs.CurrentPath()))
	if run.IndicativeOnly {
		a.warnf("indicative only: %s — this run cannot be promoted, re-run without --quick", sampleSummary(run))
	}
	if err := a.printStatus(cfg, runs); err != nil {
		return err
	}
	if options.noBuild {
		return nil
	}
	return a.buildSite(cfg, runs)
}

// rawRequest bundles what is needed to obtain raw benchmark output.
type rawRequest struct {
	// samples is the number of repetitions.
	samples int
	// filter narrows the benchmark regex.
	filter string
	// fromFile reads raw text instead of executing go test; empty means execute.
	fromFile string
}

// resolveSampleCount applies precedence: --quick wins, then --count, then config.
func resolveSampleCount(cfg *config.Config, count int, quick bool) (int, error) {
	switch {
	case quick:
		return quickCount, nil
	case count > 0:
		return count, nil
	case cfg.Bench.Count > 0:
		return cfg.Bench.Count, nil
	default:
		return 0, &usageError{err: fmt.Errorf("no sample count: set bench.count in %s or pass --count N", config.FileName)}
	}
}

// obtainRawOutput returns the raw benchmark text and the effective argument string
// that will be recorded as provenance. Whichever way the text was obtained, it is
// stored under the raw directory so a report can always be traced back to it.
func (a *App) obtainRawOutput(ctx context.Context, cfg *config.Config, runs *store.Store, request rawRequest) ([]byte, string, error) {
	if request.fromFile != "" {
		path := a.projectPath(cfg, request.fromFile)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, "", &usageError{err: fmt.Errorf("read --from-file %s: %w", path, err)}
		}
		if _, err := runs.WriteRaw(rawRunFile, content); err != nil {
			return nil, "", err
		}
		return content, fmt.Sprintf("ingested from %s (%s)", relativeTo(cfg, path), benchArgsString(cfg, request.samples, request.filter)), nil
	}

	spec := runner.BenchSpec{
		Dir:      cfg.Dir,
		Packages: cfg.Bench.Packages,
		Args:     cfg.Bench.Args,
		Count:    request.samples,
		Bench:    request.filter,
	}
	if err := a.ensureTimeout(cfg, &spec); err != nil {
		return nil, "", err
	}
	command := runner.BenchCommand(spec)
	a.info("running %s", command.String())
	result, err := a.exec.Run(ctx, command)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if err != nil {
		if _, writeErr := runs.WriteRaw(rawRunFile, combined); writeErr != nil {
			a.warnf("%v", writeErr)
		}
		return nil, "", fmt.Errorf("%w (raw output kept in %s)", err, relativeTo(cfg, runs.RawPath(rawRunFile)))
	}
	if _, err := runs.WriteRaw(rawRunFile, combined); err != nil {
		return nil, "", err
	}
	return combined, benchArgsString(cfg, request.samples, request.filter), nil
}

// ensureTimeout copies the configured timeout into the spec.
func (a *App) ensureTimeout(cfg *config.Config, spec *runner.BenchSpec) error {
	timeout, err := cfg.BenchTimeout()
	if err != nil {
		return &usageError{err: err}
	}
	spec.Timeout = timeout
	return nil
}

// benchArgsString renders the effective flags for the provenance record, so a
// milestone always says how it was produced.
func benchArgsString(cfg *config.Config, samples int, filter string) string {
	parts := []string{"-run '^$'", "-bench=" + defaultFilter(filter)}
	parts = append(parts, cfg.Bench.Args...)
	parts = append(parts, fmt.Sprintf("-count=%d", samples))
	return strings.Join(parts, " ")
}

// defaultFilter renders the benchmark regex, defaulting to everything.
func defaultFilter(filter string) string {
	if strings.TrimSpace(filter) == "" {
		return "."
	}
	return filter
}

// rawSource names the raw input for parser error messages.
func rawSource(fromFile string) string {
	if strings.TrimSpace(fromFile) == "" {
		return "go test output"
	}
	return fromFile
}

// groupRules converts the config rules into the parser's own type, keeping the
// parser package free of a config dependency.
func groupRules(cfg *config.Config) []gotest.GroupRule {
	if len(cfg.Bench.GroupRules) == 0 {
		return nil
	}
	rules := make([]gotest.GroupRule, 0, len(cfg.Bench.GroupRules))
	for _, rule := range cfg.Bench.GroupRules {
		rules = append(rules, gotest.GroupRule{
			Contains: rule.Contains,
			Exclude:  rule.Exclude,
			Workload: rule.Workload,
		})
	}
	return rules
}

// collectProvenance gathers the environment record for a run.
func (a *App) collectProvenance(ctx context.Context, cfg *config.Config, benchArgs string, noHardware bool) (model.Provenance, error) {
	collector := collect.New(collect.Options{
		Exec:            a.exec,
		Dir:             cfg.Dir,
		Environment:     a.environment,
		IncludeHostname: false,
		BenchArgs:       benchArgs,
		SkipHardware:    noHardware,
		// The tool's own output directories do not make the measured code "dirty".
		IgnorePaths: []string{cfg.Paths.DataDir, cfg.Paths.SiteDir, cfg.Paths.RawDir},
	})
	ctx, cancel := context.WithTimeout(ctx, provenanceTimeout)
	defer cancel()
	provenance, err := collector.Collect(ctx)
	if err != nil {
		return model.Provenance{}, fmt.Errorf("collect provenance: %w", err)
	}
	if noHardware {
		a.info("hardware probes skipped (--no-hw): this run is not comparable with probed ones")
	}
	return provenance, nil
}

// provenanceTimeout bounds git and hardware probes so a hung tool cannot hang a run.
const provenanceTimeout = 30 * time.Second

// renderPluralBenchmarks renders "41 benchmarks" for the run confirmation line.
func renderPluralBenchmarks(count int) string {
	return fmt.Sprintf("%d benchmark%s", count, pluralSuffix(count))
}

// pluralSuffix returns "s" unless the count is one.
func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// relativeTo renders a path relative to the config directory when possible, so
// messages stay short and reproducible across machines.
func relativeTo(cfg *config.Config, path string) string {
	if cfg == nil || path == "" {
		return path
	}
	relative, err := relativePath(cfg.Dir, path)
	if err != nil {
		return path
	}
	return relative
}
