// Package cli. summarycmd - the `benchbook summary` command: a post-ready snippet
// for a blog post, a pull request or a commit message, in text, Markdown or JSON.
package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/render"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the summary command.
const (
	// flagFrom selects the older run, defaulting to the last milestone.
	flagFrom = "from"
	// flagTo selects the newer run, defaulting to the working run.
	flagTo = "to"
	// flagFormat selects the output format.
	flagFormat = "format"
	// flagFindings caps how many changes the snippet lists.
	flagFindings = "findings"
)

// defaultSummaryFindings matches render's default snippet length.
const defaultSummaryFindings = 3

// summaryOptions holds the parsed flags of `benchbook summary`.
type summaryOptions struct {
	// fromRef is the older side of the comparison.
	fromRef string
	// toRef is the newer side of the comparison.
	toRef string
	// format selects text, md or json.
	format string
	// findings caps how many changes the text snippet lists.
	findings int
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newSummaryCommand builds `benchbook summary`.
func (a *App) newSummaryCommand() *cobra.Command {
	options := summaryOptions{}
	cmd := &cobra.Command{
		Use:     cmdSummary,
		GroupID: groupHistory,
		Short:   "Print a ready-to-paste snippet for a commit or a pull request",
		Long: `Print the headline of a comparison plus the most notable findings, in the format that
fits where it is going: plain text for a terminal or a commit message, Markdown for a
pull request, JSON for a script.

Every figure comes from a significance test and a minimum effect, which is why the
snippet is safe to paste: it never quotes a difference that is indistinguishable from
noise.`,
		Example: `  benchbook summary                                   # last milestone → working run
  benchbook summary --from m001 --to m004 --format md
  benchbook summary --from latest --to current --format json --findings 5`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runSummary(options)
		},
	}
	cmd.Flags().StringVar(&options.fromRef, flagFrom, refLatest, "older run: milestone id, \"current\" or \"latest\"")
	cmd.Flags().StringVar(&options.toRef, flagTo, refCurrent, "newer run: milestone id, \"current\" or \"latest\"")
	cmd.Flags().StringVar(&options.format, flagFormat, string(render.FormatText), "output format: "+strings.Join(render.SupportedFormats, ", "))
	cmd.Flags().IntVar(&options.findings, flagFindings, defaultSummaryFindings, "how many changes the text snippet lists")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runSummary prints a compact digest of a comparison.
func (a *App) runSummary(options summaryOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	from, err := resolveRun(cfg, runs, options.fromRef, a.loadRunFile)
	if err != nil {
		return err
	}
	to, err := resolveRun(cfg, runs, options.toRef, a.loadRunFile)
	if err != nil {
		return err
	}
	comparison := compare.Between(from, to, compareThresholds(cfg))
	snippet, err := render.Summary(comparison, render.Format(options.format), options.findings)
	if err != nil {
		return &usageError{err: err}
	}
	a.printf("%s", snippet)
	return nil
}
