// Package cli. root - the cobra command tree. Every subcommand, its flags and its
// help text are declared here or in the file that implements it, so `benchbook
// --help` is generated from the same source that parses the command line instead of
// being maintained by hand next to it.
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Command groups. They order the help screen by workflow — measure, history, publish
// — rather than alphabetically, which is how a reader actually starts using the tool.
const (
	// groupSetUp holds the commands run once per repository.
	groupSetUp = "setup"
	// groupMeasure holds the commands that touch the toolchain.
	groupMeasure = "measure"
	// groupHistory holds the commands that read or extend the milestone history.
	groupHistory = "history"
	// groupPublish holds the commands that produce or serve the documentation site.
	groupPublish = "publish"
	// groupOther holds the commands that fit nowhere else.
	groupOther = "other"
)

// suggestionsMinimumDistance is the Levenshtein distance within which a mistyped
// command still gets a suggestion.
const suggestionsMinimumDistance = 2

// Group titles as they appear in the help screen.
const (
	// groupSetUpTitle is the title of the setup group.
	groupSetUpTitle = "Set up (once per repository):"
	// groupMeasureTitle is the title of the measurement group.
	groupMeasureTitle = "Measure:"
	// groupHistoryTitle is the title of the history group.
	groupHistoryTitle = "History and comparison:"
	// groupPublishTitle is the title of the publishing group.
	groupPublishTitle = "Publish:"
	// groupOtherTitle is the title of the remaining group.
	groupOtherTitle = "Other:"
)

// Help text of the root command. The model paragraph is repeated in the README and
// in `benchbook help`: it is the one thing a new user must understand, because every
// refusal the tool makes follows from it.
const (
	// rootShort is the one-line description shown in command lists.
	rootShort = "🦀 benchmark and profile documentation with deliberate milestones"
	// rootLong is the paragraph shown by `benchbook --help`.
	rootLong = `benchbook records go test -bench results and pprof profiles, keeps an immutable
history of deliberate milestones, compares runs with statistics instead of
percentages, and generates a static documentation site you can publish on GitHub
Pages — with one binary and one benchbook.toml per repository.

The model, in two lines:
  a run is the working tree: benchbook/current.json, overwritten by every ` + "`benchbook run`" + `;
  a milestone is a commit: history/mNNN-slug.json, immutable, kept forever, and
  written only by ` + "`benchbook milestone`" + `.

Nothing is promoted automatically, percentages are only printed when the samples
support them, and generated pages are rebuilt byte-identically from the JSON that is
the source of truth.`
	// rootNeedsCommand is printed when benchbook is invoked with no arguments.
	rootNeedsCommand = "no command given: pick one from the list above, or run `benchbook help <command>` for the details of one command"
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// rootArgs rejects a stray argument on the bare command (`benchbook runn`). Cobra
// would otherwise report an unknown command with its own error, which would leave
// this package's exit-code contract (2 = usage) up to the library.
func (a *App) rootArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return &usageError{err: unknownCommandError(cmd, args[0])}
}

// unknownCommandError renders a typo with a suggestion, because "unknown command" on
// its own makes the reader scroll back to the help screen.
func unknownCommandError(cmd *cobra.Command, typed string) error {
	message := fmt.Sprintf("unknown command %q for %q", typed, cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(typed); len(suggestions) > 0 {
		return fmt.Errorf("%s: did you mean %s?", message, quoteList(suggestions))
	}
	return fmt.Errorf("%s: run `%s --help` to see the commands", message, cmd.CommandPath())
}

// quoteList renders names as a comma-separated list of quoted names.
func quoteList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return strings.Join(quoted, ", ")
}

// flagError turns a flag-parsing failure into a usage error. Without it the error
// would be reported as a runtime failure and exit with the wrong code.
func flagError(cmd *cobra.Command, err error) error {
	return &usageError{err: fmt.Errorf("%w (run `%s --help` for the accepted flags)", err, cmd.CommandPath())}
}

// noArgs rejects positional arguments, pointing at the flags the command does take.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return &usageError{err: fmt.Errorf("%s takes no arguments, but got %q (run `%s --help` to see the flags)",
		cmd.CommandPath(), args[0], cmd.CommandPath())}
}

// newHelpCommand builds `benchbook help [command]`. It is written here rather than
// taken from cobra's default so that an unknown topic is a usage error with exit code
// 2, and so that no code path calls os.Exit behind the caller's back.
func (a *App) newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:     cmdHelp + " [command]",
		GroupID: groupOther,
		Short:   "Show help for benchbook or for one command",
		Long: `Show the full help screen, or the help of one command.

Every command also accepts -h and --help, so ` + "`benchbook run --help`" + ` and
` + "`benchbook help run`" + ` are the same thing.`,
		Example: `  benchbook help
  benchbook help milestone`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _, err := root.Find(args)
			if err != nil {
				return &usageError{err: err}
			}
			if target == root && len(args) > 0 {
				return &usageError{err: unknownCommandError(root, args[0])}
			}
			// Cobra adds -h/--help lazily, on the path where a command executes.
			// Initialising it here keeps `benchbook help run` and
			// `benchbook run --help` byte-identical, which is what the tests assert.
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
}

// exactArgs requires exactly count positional arguments and explains what they are.
func exactArgs(count int, what string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == count {
			return nil
		}
		return &usageError{err: fmt.Errorf("%s needs %s (got %s)", cmd.CommandPath(),
			what, pluralizeCount(len(args), "argument"))}
	}
}

// pluralizeCount renders "1 argument" or "3 arguments".
func pluralizeCount(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newRootCommand builds the whole command tree for one invocation. It is built fresh
// every time so that a long-lived process — and the tests — can run the CLI more than
// once without state leaking between invocations.
func (a *App) newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           cmdBenchbook,
		Short:         rootShort,
		Long:          rootLong,
		Args:          a.rootArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Colour is resolved once, after the flags are parsed: it depends on
		// --no-color, on NO_COLOR and on whether stdout is a terminal.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.resolveColour()
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A bare invocation is a usage error, but the reader still gets the
			// command list rather than a one-line complaint.
			if err := cmd.Help(); err != nil {
				return err
			}
			return &usageError{err: errors.New(rootNeedsCommand)}
		},
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetFlagErrorFunc(flagError)
	// Cobra applies its own default only on the code path this package replaces with
	// rootArgs, so the threshold is set here: a typo should suggest the real command.
	root.SuggestionsMinimumDistance = suggestionsMinimumDistance
	root.SetHelpCommand(a.newHelpCommand(root))
	// The shell-completion command is part of cobra but not part of this tool's
	// interface, so it is not offered.
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddGroup(
		&cobra.Group{ID: groupSetUp, Title: groupSetUpTitle},
		&cobra.Group{ID: groupMeasure, Title: groupMeasureTitle},
		&cobra.Group{ID: groupHistory, Title: groupHistoryTitle},
		&cobra.Group{ID: groupPublish, Title: groupPublishTitle},
		&cobra.Group{ID: groupOther, Title: groupOtherTitle},
	)
	root.PersistentFlags().BoolVar(&a.noColor, noColorFlag, false, "disable ANSI colours (NO_COLOR is honoured as well)")
	root.AddCommand(
		a.newInitCommand(),
		a.newVersionCommand(),
		a.newRunCommand(),
		a.newProfileCommand(),
		a.newStatusCommand(),
		a.newMilestoneCommand(),
		a.newListCommand(),
		a.newCompareCommand(),
		a.newSummaryCommand(),
		a.newBuildCommand(),
		a.newServeCommand(),
	)
	return root
}
