// Package cli. profile - the `benchbook profile` command: capture CPU and heap
// profiles for the whole suite or a single benchmark, parse the `pprof -top` tables
// and merge them into the current run.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/parse/pprof"
	"github.com/seyallius/benchbook/internal/runner"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the profile command.
const (
	// flagBench narrows profiling to one benchmark.
	flagBench = "bench"
	// flagPackage overrides [profile].package.
	flagPackage = "pkg"
)

// File names of the profile artefacts inside the raw directory.
const (
	// cpuProfileFile is the `go test -cpuprofile` output.
	cpuProfileFile = "cpu.pprof"
	// memProfileFile is the `go test -memprofile` output.
	memProfileFile = "mem.pprof"
)

// profileOptions holds the parsed flags of `benchbook profile`.
type profileOptions struct {
	// bench narrows the profile to one benchmark.
	bench string
	// pkg overrides [profile].package.
	pkg string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newProfileCommand builds `benchbook profile`.
func (a *App) newProfileCommand() *cobra.Command {
	options := profileOptions{}
	cmd := &cobra.Command{
		Use:     cmdProfile,
		GroupID: groupMeasure,
		Short:   "Capture cpu and heap profiles and their pprof tables",
		Long: `Run the benchmarks under the profiler, keep the pprof -top tables of every configured
kind, and attach them to the current run.

Run it after ` + "`benchbook run`" + ` and before ` + "`benchbook milestone`" + `: profiles belong to the run
they were captured from, and promoting copies them into the history. A later run
replaces the working run including its profiles, because a profile of yesterday's
binary is not a profile of today's.`,
		Example: `  benchbook profile                          # profile.package, every configured kind
  benchbook profile --bench FindUser         # one benchmark of that package
  benchbook profile --pkg ./internal/core    # override the configured package`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runProfile(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.bench, flagBench, "", "benchmark to profile (default: the whole suite)")
	cmd.Flags().StringVar(&options.pkg, flagPackage, "", "package to profile (default: profile.package)")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runProfile records profiles for the current run and rebuilds the report.
func (a *App) runProfile(ctx context.Context, options profileOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	target, err := profileTarget(cfg, options.pkg)
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	if err := runs.EnsureDirs(); err != nil {
		return err
	}

	profiles, err := a.captureProfiles(ctx, cfg, runs, target, options.bench)
	if err != nil {
		return err
	}
	current, err := runs.LoadCurrent()
	if err != nil {
		return err
	}
	merged := mergeProfiles(current.Profiles, profiles)
	current.Profiles = merged
	hash, err := current.ComputeContentHash()
	if err != nil {
		return err
	}
	current.ContentHash = hash
	if err := runs.SaveCurrent(current); err != nil {
		return err
	}
	a.printf("🔬 recorded %s for %s\n", renderPluralProfiles(len(profiles)), profileLabel(target, options.bench))
	for _, profile := range profiles {
		a.info("%s/%s: %s", profile.Kind, profile.Target, relativeTo(cfg, runs.RawPath(rawProfileName(profile))))
	}
	return a.buildSite(cfg, runs)
}

// profileTarget resolves which package to profile.
func profileTarget(cfg *config.Config, flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue, nil
	}
	if strings.TrimSpace(cfg.Profile.Package) != "" {
		return cfg.Profile.Package, nil
	}
	return "", &usageError{err: fmt.Errorf("no package to profile: set profile.package in %s or pass --pkg ./internal/core", config.FileName)}
}

// captureProfiles runs the profiled benchmark command and parses the results.
func (a *App) captureProfiles(ctx context.Context, cfg *config.Config, runs *store.Store, target, bench string) ([]model.Profile, error) {
	spec := runner.ProfileSpec{
		BenchSpec: runner.BenchSpec{
			Dir:      cfg.Dir,
			Packages: []string{target},
			Args:     cfg.Bench.Args,
			Count:    profileCount,
			Bench:    bench,
		},
		Package: target,
		CPUOut:  runs.RawPath(cpuProfileFile),
		MemOut:  runs.RawPath(memProfileFile),
	}
	if err := a.ensureTimeout(cfg, &spec.BenchSpec); err != nil {
		return nil, err
	}
	command := runner.ProfileTestCommand(spec)
	a.info("running %s", command.String())
	result, err := a.exec.Run(ctx, command)
	output := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if _, writeErr := runs.WriteRaw(rawProfileRunFile, output); writeErr != nil {
		a.warnf("%v", writeErr)
	}
	if err != nil {
		return nil, fmt.Errorf("%w (raw output kept in %s)", err, relativeTo(cfg, runs.RawPath(rawProfileRunFile)))
	}

	targetName := profileTargetName(bench)
	profiles := []model.Profile{}
	for _, kind := range cfg.Profile.Kinds {
		profile, err := a.readProfile(ctx, cfg, runs, kind, targetName, spec)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, *profile)
	}
	// The binary profiles are large and regenerable; the raw text tables above are
	// what a report links to, so only those are kept.
	cleanupProfileArtefacts(profileArtefactPaths(runs)...)
	return profiles, nil
}

// profileCount is the sample count used while profiling: one sample is enough to
// find a hot function and keeps the command fast.
const profileCount = 1

// rawProfileRunFile holds the benchmark output produced while profiling.
const rawProfileRunFile = "last-profile-run.txt"

// readProfile runs `go tool pprof -top` for one kind and parses it.
func (a *App) readProfile(ctx context.Context, cfg *config.Config, runs *store.Store, kind model.ProfileKind, target string, spec runner.ProfileSpec) (*model.Profile, error) {
	source := spec.CPUOut
	if kind != model.ProfileKindCPU {
		source = spec.MemOut
	}
	command := runner.ProfileTopCommand(cfg.Dir, source, runner.SampleIndexFor(kind), cfg.Profile.Top)
	result, err := a.exec.Run(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("read %s profile: %w", kind, err)
	}
	if err := writeRaw(runs, rawProfileNameFor(kind, target), result.Stdout); err != nil {
		return nil, err
	}
	parser := pprof.New(pprof.Options{
		Source: relativeTo(cfg, source),
		Kind:   kind,
		Target: target,
	})
	profile, err := parser.Parse(strings.NewReader(string(result.Stdout)))
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// writeRaw stores raw text under the raw directory.
func writeRaw(runs *store.Store, name string, content []byte) error {
	if _, err := runs.WriteRaw(name, content); err != nil {
		return err
	}
	return nil
}

// rawProfileName renders the raw file name of a merged profile.
func rawProfileName(profile model.Profile) string {
	return rawProfileNameFor(profile.Kind, profile.Target)
}

// rawProfileNameFor renders the raw file name of one profile kind and target.
func rawProfileNameFor(kind model.ProfileKind, target string) string {
	return fmt.Sprintf("%s-%s-%s.txt", rawProfilePrefix, kind, sanitizeTarget(target))
}

// sanitizeTarget makes a benchmark name safe to use as a file name.
func sanitizeTarget(target string) string {
	replaced := strings.NewReplacer("/", "_", " ", "_", "\\", "_").Replace(target)
	if strings.TrimSpace(replaced) == "" {
		return model.ProfileTargetFull
	}
	return replaced
}

// profileTargetName renders the profile target of a run: "full" or the benchmark.
func profileTargetName(bench string) string {
	trimmed := strings.TrimSpace(bench)
	if trimmed == "" || trimmed == "." {
		return model.ProfileTargetFull
	}
	return trimmed
}

// profileLabel renders the human description of a profile target.
func profileLabel(target, bench string) string {
	if profileTargetName(bench) == model.ProfileTargetFull {
		return target
	}
	return profileTargetName(bench)
}

// renderPluralProfiles renders "3 profiles".
func renderPluralProfiles(count int) string {
	if count == 1 {
		return "1 profile"
	}
	return fmt.Sprintf("%d profiles", count)
}

// mergeProfiles replaces the profiles of the same (kind, target) and keeps the rest.
func mergeProfiles(existing, incoming []model.Profile) []model.Profile {
	merged := make([]model.Profile, 0, len(existing)+len(incoming))
	for _, profile := range existing {
		if !hasProfile(incoming, profile.Kind, profile.Target) {
			merged = append(merged, profile)
		}
	}
	merged = append(merged, incoming...)
	model.SortProfiles(merged)
	return merged
}

// hasProfile reports whether a profile with the given key exists in the list.
func hasProfile(profiles []model.Profile, kind model.ProfileKind, target string) bool {
	for _, profile := range profiles {
		if profile.Kind == kind && profile.Target == target {
			return true
		}
	}
	return false
}

// cleanupProfileArtefacts removes the binary profile files after they were parsed,
// because they are large and trivially regenerable.
func cleanupProfileArtefacts(paths ...string) {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			continue
		}
	}
}

// profileArtefactPaths lists the binary profiles written by a profiling run.
func profileArtefactPaths(runs *store.Store) []string {
	return []string{
		filepath.Join(runs.RawDir(), cpuProfileFile),
		filepath.Join(runs.RawDir(), memProfileFile),
	}
}
