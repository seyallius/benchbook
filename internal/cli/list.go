// Package cli. list - the `benchbook list` command: every milestone with its date,
// label, commit and headline change against the milestone before it.
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/compare"
	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/model"
	"github.com/seyallius/benchbook/internal/render"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the list command.
const (
	// flagLimit caps how many milestones are printed.
	flagLimit = "limit"
)

// defaultListLimit matches the default of [site].milestone_limit.
const defaultListLimit = 500

// listOptions holds the parsed flags of `benchbook list`.
type listOptions struct {
	// limit caps how many milestones are printed.
	limit int
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newListCommand builds `benchbook list`.
func (a *App) newListCommand() *cobra.Command {
	options := listOptions{}
	cmd := &cobra.Command{
		Use:     cmdList,
		GroupID: groupHistory,
		Short:   "List the milestones with their headline change",
		Long: `List every milestone of the history, oldest first, with its date, the commit it
points at, the headline change against the milestone before it, and its label.

A milestone is never rewritten, so this is a stable record: the row for m003 says the
same thing today as it will in a year.`,
		Example: `  benchbook list
  benchbook list --limit 5     # just the recent history`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runList(options)
		},
	}
	cmd.Flags().IntVar(&options.limit, flagLimit, defaultListLimit, "maximum number of milestones to print")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runList prints the milestone history, oldest first.
func (a *App) runList(options listOptions) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	runs := a.storeFor(cfg)
	milestones, err := runs.Milestones()
	if err != nil {
		return err
	}
	if len(milestones) == 0 {
		a.printf("no milestones yet — record the first one with:  benchbook milestone -m \"baseline\"\n")
		return nil
	}
	if options.limit > 0 && len(milestones) > options.limit {
		milestones = milestones[len(milestones)-options.limit:]
	}
	a.printMilestoneTable(cfg, milestones)
	return nil
}

// printMilestoneTable renders the history as an aligned table.
func (a *App) printMilestoneTable(cfg *config.Config, milestones []*model.Run) {
	rows := [][]string{{"ID", "DATE", "SPEC", "HEADLINE", "LABEL"}}
	for index, milestone := range milestones {
		var previous *model.Run
		if index > 0 {
			previous = milestones[index-1]
		}
		rows = append(rows, []string{
			milestone.ID,
			milestone.CreatedAt.UTC().Format(dateLayout),
			shortSpec(milestone, previous),
			headlineText(cfg, previous, milestone),
			milestoneLabelWithBadges(milestone),
		})
	}
	for _, line := range renderColumns(rows) {
		a.printf("%s\n", line)
	}
}

// shortSpec renders the commit with the dirty marker and the warning badges.
func shortSpec(milestone, previous *model.Run) string {
	parts := []string{render.ShortHash(milestone.Provenance.GitSHA, shortSHALength)}
	if milestone.Provenance.GitDirty {
		parts = append(parts, "dirty")
	}
	badges := milestoneBadges(milestone, previous)
	if len(badges) > 0 {
		parts = append(parts, strings.Join(badges, ","))
	}
	return strings.Join(parts, " ")
}

// milestoneLabelWithBadges renders the label, marking forced promotions.
func milestoneLabelWithBadges(milestone *model.Run) string {
	label := milestone.Label
	if strings.TrimSpace(label) == "" {
		label = "(no label)"
	}
	if milestone.Forced {
		label += "  ⚠ forced"
	}
	if milestone.IndicativeOnly {
		label += "  ⚠ indicative"
	}
	return label
}

// headlineText renders the geomean change of a milestone against its predecessor.
func headlineText(cfg *config.Config, previous, milestone *model.Run) string {
	if previous == nil {
		return "baseline"
	}
	comparison := compare.Between(previous, milestone, compareThresholds(cfg))
	parts := []string{}
	for _, geomean := range comparison.Geomeans {
		if geomean.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", render.FormatSignedPercent(geomean.DeltaPct), geomean.Metric))
	}
	if len(parts) == 0 {
		return render.DashPlaceholder
	}
	return strings.Join(parts, ", ")
}

// renderColumns formats a table with per-column widths, keeping the CLI output
// aligned without pulling in a table library.
func renderColumns(rows [][]string) []string {
	widths := make([]int, 0, len(rows[0]))
	for _, row := range rows {
		for index, cell := range row {
			if index >= len(widths) {
				widths = append(widths, 0)
			}
			if length := len([]rune(cell)); length > widths[index] {
				widths[index] = length
			}
		}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, 0, len(row))
		for index, cell := range row {
			cell = padCell(cell, widths[index])
			cells = append(cells, cell)
		}
		lines = append(lines, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
	return lines
}

// padCell pads a cell to a column width.
func padCell(cell string, width int) string {
	padding := width - len([]rune(cell))
	if padding <= 0 {
		return cell
	}
	return cell + strings.Repeat(" ", padding)
}
