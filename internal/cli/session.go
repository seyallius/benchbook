// Package cli. session - shared plumbing for the subcommands: opening the store,
// resolving run references and describing a run in one line. Keeping this here
// means no subcommand has to know how the data directory is laid out.
package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Run reference keywords accepted by `compare`, `summary` and friends.
const (
	// refCurrent refers to the working run.
	refCurrent = "current"
	// refLatest refers to the highest milestone.
	refLatest = "latest"
)

// shortSHALength is how many characters of a git SHA are shown in one-liners.
const shortSHALength = 7

// ----------------------------------------- Public API ----------------------------------------- //

// storeFor opens the store described by the configuration.
func (a *App) storeFor(cfg *config.Config) *store.Store {
	return store.New(cfg.DataDir(), cfg.RawDir())
}

// resolveRun turns a user-supplied reference into a run. Accepted forms are
// "current", "latest", "m002", "m002-sync-pool.json" and a path to any JSON run
// document, which makes ad-hoc comparisons possible without promoting anything.
func resolveRun(cfg *config.Config, runs *store.Store, ref string, loadPath func(string) (*model.Run, error)) (*model.Run, error) {
	trimmed := strings.TrimSpace(ref)
	switch {
	case trimmed == "" || trimmed == refCurrent:
		run, err := runs.LoadCurrent()
		if err != nil {
			if errors.Is(err, store.ErrNoCurrent) {
				return nil, &usageError{err: fmt.Errorf("%w: record one with `benchbook run`, or pass a milestone id such as %q", err, "m001")}
			}
			return nil, err
		}
		return run, nil
	case trimmed == refLatest:
		run, err := runs.LatestMilestone()
		if err != nil {
			if errors.Is(err, store.ErrNoMilestones) {
				return nil, &usageError{err: fmt.Errorf("%w: promote the current run with `benchbook milestone -m \"baseline\"`", err)}
			}
			return nil, err
		}
		return run, nil
	case strings.Contains(trimmed, stringPathSeparator) || strings.HasSuffix(trimmed, jsonSuffix):
		if loadPath == nil {
			return nil, &usageError{err: fmt.Errorf("reading a run from a path is not supported here: %q", ref)}
		}
		return loadPath(trimmed)
	default:
		run, err := runs.LoadMilestone(trimmed)
		if err != nil {
			if errors.Is(err, store.ErrMilestoneNotFound) || errors.Is(err, store.ErrInvalidMilestone) {
				return nil, &usageError{err: fmt.Errorf("%w (see the available ids with `benchbook list`)", err)}
			}
			return nil, err
		}
		return run, nil
	}
}

// Path constants used when sniffing a run reference.
const (
	// stringPathSeparator distinguishes a file reference from a milestone id.
	stringPathSeparator = "/"
	// jsonSuffix marks a file reference on every platform.
	jsonSuffix = ".json"
)

// runLabel renders "m002 \"sync.Pool for buffers\"" or "current run".
func runLabel(run *model.Run) string {
	if run == nil {
		return "unknown"
	}
	if run.IsCurrent() {
		return "current run"
	}
	if strings.TrimSpace(run.Label) == "" {
		return run.ID
	}
	return fmt.Sprintf("%s %q", run.ID, run.Label)
}

// provenanceLine renders the "(abc1234 clean, go1.22.5, 12× CPU, 10 samples)"
// fragment of the status header.
func provenanceLine(run *model.Run) string {
	if run == nil {
		return ""
	}
	parts := []string{}
	if run.Provenance.GitSHA != "" {
		state := "clean"
		if run.Provenance.GitDirty {
			state = "dirty"
		}
		parts = append(parts, fmt.Sprintf("%s %s", render.ShortHash(run.Provenance.GitSHA, shortSHALength), state))
	} else {
		parts = append(parts, "no git")
	}
	if run.Provenance.GoVersion != "" {
		parts = append(parts, run.Provenance.GoVersion)
	}
	if cpu := cpuLine(run); cpu != "" {
		parts = append(parts, cpu)
	}
	parts = append(parts, sampleSummary(run))
	return strings.Join(parts, ", ")
}

// cpuLine renders "12× i5-11400H", omitting what is unknown.
func cpuLine(run *model.Run) string {
	cores := run.Provenance.CPUCores
	cpuModel := run.Provenance.CPUModel
	switch {
	case cores > 0 && isKnown(cpuModel):
		return fmt.Sprintf("%d× %s", cores, cpuModel)
	case cores > 0:
		return fmt.Sprintf("%d cores", cores)
	case isKnown(cpuModel):
		return cpuModel
	default:
		return ""
	}
}

// sampleSummary renders "10 samples", or "6–10 samples" when the benchmark
// counts disagree (which happens when a package is filtered out mid-run).
func sampleSummary(run *model.Run) string {
	if len(run.Benchmarks) == 0 {
		return "no benchmarks"
	}
	counts := map[int]bool{}
	for _, benchmark := range run.Benchmarks {
		counts[len(benchmark.Samples)] = true
	}
	distinct := make([]int, 0, len(counts))
	for count := range counts {
		distinct = append(distinct, count)
	}
	sort.Ints(distinct)
	if len(distinct) == 1 {
		return render.Pluralize(distinct[0], "sample")
	}
	if distinct[0] == 0 {
		return "no samples"
	}
	return fmt.Sprintf("%d–%d samples", distinct[0], distinct[len(distinct)-1])
}

// relativePath renders target relative to base when that is shorter and cleaner
// than the absolute path, which keeps messages readable in logs and tests.
func relativePath(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

// isKnown reports whether a hardware probe produced a usable value.
func isKnown(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed != "unknown"
}

// milestoneBadges lists the warning badges of a milestone: a dirty tree, a forced
// promotion, a Go version change or a hardware change compared with the previous
// milestone. They are rendered in the site and in `list`.
func milestoneBadges(milestone, previous *model.Run) []string {
	badges := []string{}
	if milestone.Provenance.GitDirty {
		badges = append(badges, "dirty")
	}
	if milestone.Forced {
		badges = append(badges, "forced")
	}
	if previous != nil {
		if milestone.Provenance.GoVersion != previous.Provenance.GoVersion {
			badges = append(badges, "go-version-change")
		}
		if milestone.Provenance.HardwareFingerprint != previous.Provenance.HardwareFingerprint {
			badges = append(badges, "hardware-change")
		}
	}
	return badges
}
