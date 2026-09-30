// Package cli. milestone - the `benchbook milestone` command, the only way a run
// becomes history. Every safety rule of the specification lives here, and each of
// them explains itself and names the flag that overrides it.
package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the milestone command.
const (
	// flagMessage is the required milestone label.
	flagMessage = "message"
	// flagMessageShorthand is the short form every user types.
	flagMessageShorthand = "m"
	// flagNote adds free text describing what changed.
	flagNote = "note"
	// flagAllowDirty promotes despite a dirty working tree.
	flagAllowDirty = "allow-dirty"
)

// Messages of the milestone rules. They are constants because the integration tests
// and the FAQ both assert on their wording.
const (
	// alreadyPromotedMessage is printed when the content is already promoted.
	alreadyPromotedMessage = "already promoted: %s has exactly this content (nothing to do)"
	// dirtyRefusal explains the dirty-tree rule.
	dirtyRefusal = "the working tree is dirty: commit your changes so the milestone points at a real state, or pass --allow-dirty to record it anyway"
	// gitMissingRefusal explains the missing-git rule.
	gitMissingRefusal = "git could not identify the current commit: initialise a repository, or pass --allow-dirty to record the milestone without a commit"
	// indicativeRefusal explains the single-sample rule.
	indicativeRefusal = "this run is indicative only (one sample per benchmark): re-run with `benchbook run --count %d`, or pass --force to promote it anyway"
	// sparseRefusal explains the per-benchmark sample shortfall.
	sparseRefusal = "%s has only %s, but stats.min_samples is %d: re-run with `benchbook run --count %d`, or pass --force to promote it anyway"
	// dirtyBadgeWarning warns that the recorded state cannot be reproduced.
	dirtyBadgeWarning = "recorded with a dirty working tree (⚠ badge): the numbers cannot be reproduced from the commit alone"
	// forcedBadgeWarning warns that the sample rules were overridden.
	forcedBadgeWarning = "recorded with --force (⚠ badge): the sample-count rules were overridden"
)

// sparseBenchmark is the first benchmark that fails the sample-count rule.
type sparseBenchmark struct {
	// displayName is the benchmark name without the "Benchmark" prefix.
	displayName string
	// samples are the samples that were recorded.
	samples []model.Sample
}

// milestoneOptions holds the parsed flags of `benchbook milestone`.
type milestoneOptions struct {
	// label is the required milestone label.
	label string
	// note is optional free text describing what changed and why.
	note string
	// allowDirty promotes despite a dirty working tree.
	allowDirty bool
	// force overrides the sample-count rules.
	force bool
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newMilestoneCommand builds `benchbook milestone`.
func (a *App) newMilestoneCommand() *cobra.Command {
	options := milestoneOptions{}
	cmd := &cobra.Command{
		Use:     cmdMilestone + ` -m "label"`,
		GroupID: groupHistory,
		Short:   "Promote the working run into the immutable history",
		Long: `Promote benchbook/current.json into the milestone history as the next mNNN, with the
label and note you give it. It is the only command that writes history, and it never
runs the benchmarks itself: the numbers it promotes are the ones you already recorded.

The rules, and how to get past them when you mean it:
  * a milestone points at a commit — commit first, or pass --allow-dirty to record a
    snapshot of a dirty tree (the milestone then keeps a ⚠ badge);
  * git must be able to name the commit — or pass --allow-dirty;
  * every benchmark needs at least stats.min_samples samples — re-run with
    ` + "`benchbook run --count N`" + `, or pass --force (another ⚠ badge).

Promoting content that is already the latest milestone is a no-op, so calling it twice
from a script is safe.`,
		Example: `  benchbook milestone -m "baseline"
  benchbook milestone -m "sync.Pool for Concat" --note "the naive loop was quadratic"
  benchbook milestone -m "snapshot of a work in progress" --allow-dirty`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runMilestone(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVarP(&options.label, flagMessage, flagMessageShorthand, "", "label describing the change (required)")
	cmd.Flags().StringVar(&options.note, flagNote, "", "free text: what changed and why")
	cmd.Flags().BoolVar(&options.allowDirty, flagAllowDirty, false, "promote even though the working tree is dirty")
	cmd.Flags().BoolVar(&options.force, flagForce, false, "override the sample-count rules")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runMilestone promotes the current run into the immutable history.
func (a *App) runMilestone(ctx context.Context, options milestoneOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	if err := runs.EnsureDirs(); err != nil {
		return err
	}
	run, err := runs.LoadCurrent()
	if err != nil {
		if errors.Is(err, store.ErrNoCurrent) {
			return &usageError{err: fmt.Errorf("%w: record one with `benchbook run` before promoting it", err)}
		}
		return err
	}

	milestoneLabel := normalizeLabel(options.label)
	if milestoneLabel == "" {
		return &usageError{err: errors.New("a milestone needs a label: benchbook milestone -m \"sync.Pool for buffers\"")}
	}

	milestones, err := runs.Milestones()
	if err != nil {
		return err
	}
	latest := latestOf(milestones)

	// Promoting identical content again is a no-op, not an error: `milestone` is
	// idempotent, so a script may run it twice without a special case.
	if latest != nil && latest.ContentHash != "" && latest.ContentHash == run.ContentHash {
		a.info(alreadyPromotedMessage, latest.ID)
		return nil
	}
	if err := checkPromotion(run, latest, options.allowDirty); err != nil {
		return err
	}
	sparse := findSparseBenchmark(run, cfg.Stats.MinSamples)
	if err := checkSampleRules(run, sparse, cfg, options.force); err != nil {
		return err
	}
	a.warnOnProvenanceChange(run, latest)

	run.Label = milestoneLabel
	run.Note = normalizeLabel(options.note)
	run.Forced = options.force && (run.IndicativeOnly || sparse != nil)
	sequence, err := runs.NextSeq()
	if err != nil {
		return err
	}
	run.Seq = sequence
	run.ID = milestoneIDFor(sequence)
	path, err := runs.SaveMilestone(run)
	if err != nil {
		return err
	}

	if run.Provenance.GitDirty {
		a.warnf("%s", dirtyBadgeWarning)
	}
	if run.Forced {
		a.warnf("%s", forcedBadgeWarning)
	}
	a.printf("🏁 promoted %s  %q\n", run.ID, run.Label)
	a.info("wrote %s", relativeTo(cfg, path))
	return a.buildSite(cfg, runs)
}

// checkPromotion applies the working-tree rules: a milestone must point at a commit
// unless the user explicitly accepts a snapshot of a dirty tree.
func checkPromotion(run, latest *model.Run, allowDirty bool) error {
	if allowDirty {
		return nil
	}
	if run.Provenance.GitSHA == "" {
		return &refusalError{err: errors.New(gitMissingRefusal)}
	}
	if run.Provenance.GitDirty {
		return &refusalError{err: errors.New(dirtyRefusal)}
	}
	_ = latest
	return nil
}

// checkSampleRules applies the sample-count rules: an indicative-only run or a
// benchmark below the configured minimum needs --force.
func checkSampleRules(run *model.Run, sparse *sparseBenchmark, cfg *config.Config, force bool) error {
	if force {
		return nil
	}
	minimum := cfg.Stats.MinSamples
	if run.IndicativeOnly {
		return &refusalError{err: fmt.Errorf(indicativeRefusal, minimum)}
	}
	if sparse != nil {
		return &refusalError{err: fmt.Errorf(sparseRefusal,
			sparse.displayName, render.Pluralize(len(sparse.samples), "sample"), minimum, minimum)}
	}
	return nil
}

// findSparseBenchmark returns the first benchmark below the sample threshold.
func findSparseBenchmark(run *model.Run, minimum int) *sparseBenchmark {
	for _, benchmark := range run.Benchmarks {
		if len(benchmark.Samples) < minimum {
			return &sparseBenchmark{displayName: model.DisplayName(benchmark.Name), samples: benchmark.Samples}
		}
	}
	return nil
}

// warnOnProvenanceChange warns (but never refuses) when the machine or the toolchain
// changed, because those are legitimate but change what a comparison means.
func (a *App) warnOnProvenanceChange(run, latest *model.Run) {
	if latest == nil {
		return
	}
	if run.Provenance.HardwareFingerprint != latest.Provenance.HardwareFingerprint {
		a.warnf("hardware fingerprint differs from %s (%s): this milestone is marked as not directly comparable",
			latest.ID, render.ShortHash(run.Provenance.HardwareFingerprint, shortSHALength))
	}
	if run.Provenance.GoVersion != latest.Provenance.GoVersion {
		a.warnf("Go version differs from %s (%s → %s): re-measure the baseline before trusting deltas",
			latest.ID, latest.Provenance.GoVersion, run.Provenance.GoVersion)
	}
}

// milestoneIDFor renders a milestone id from its sequence number.
func milestoneIDFor(sequence int) string { return fmt.Sprintf("m%03d", sequence) }
