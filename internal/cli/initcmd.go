// Package cli. initcmd - the `benchbook init` command: it writes a commented
// benchbook.toml, the .gitignore entries the data layout needs, and a justfile
// snippet suggestion. It never overwrites an existing configuration.
package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/config"
	"github.com/seyallius/benchbook/internal/store"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Flag names of the init command.
const (
	// flagName sets the project name written into the config.
	flagName = "name"
	// flagForce allows overwriting an existing benchbook.toml.
	flagForce = "force"
)

// gitignoreEntries is what a project must ignore: raw tool output is regenerable
// and huge, while the JSON history is committed on purpose.
var gitignoreEntries = []string{
	"/benchbook/raw/",
}

// justfileSnippet is printed rather than written: benchbook does not own the
// project's justfile and silently editing it would be rude.
const justfileSnippet = `# suggested justfile additions
bench       *ARGS:   # record a run and rebuild the docs
    benchbook run {{ARGS}}
promote *ARGS:       # promote the current run into the history
    benchbook milestone {{ARGS}}
docs:
    benchbook build`

// configFilePerm is the permission used for generated configuration files.
const configFilePerm fs.FileMode = 0o644

// initOptions holds the parsed flags of `benchbook init`.
type initOptions struct {
	// name is written into [project].name.
	name string
	// force allows overwriting an existing benchbook.toml.
	force bool
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newInitCommand builds `benchbook init`.
func (a *App) newInitCommand() *cobra.Command {
	options := initOptions{}
	cmd := &cobra.Command{
		Use:     cmdInit,
		GroupID: groupSetUp,
		Short:   "Write benchbook.toml, the .gitignore entries and a justfile snippet",
		Long: `Write benchbook.toml in the current directory, append the .gitignore entries the
data layout needs, and print the justfile recipes worth having.

The generated file is a commented example rather than a dump of every default: a
setting that is not in your file is a setting you can inherit from a future version.
An existing benchbook.toml is never touched unless you pass --force.`,
		Example: `  benchbook init                       # project name defaults to the directory name
  benchbook init --name myproject      # explicit name for the header and the site`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runInit(options)
		},
	}
	cmd.Flags().StringVar(&options.name, flagName, filepath.Base(a.dir), "project name written into [project].name")
	cmd.Flags().BoolVar(&options.force, flagForce, false, "overwrite an existing benchbook.toml")
	return cmd
}

// ----------------------------------------- Public API ----------------------------------------- //

// runInit creates benchbook.toml in the working directory and appends the .gitignore
// entries that the data layout requires.
func (a *App) runInit(options initOptions) error {
	a.printf("🦀 Welcome to benchbook!\n\n")
	target := filepath.Join(a.dir, config.FileName)
	if _, err := os.Stat(target); err == nil && !options.force {
		return &usageError{err: fmt.Errorf(
			"%s already exists: edit it directly, or pass --force to replace it with the commented sample", target)}
	}
	var builder strings.Builder
	if err := config.Sample(&builder, options.name); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(builder.String()), configFilePerm); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	a.info("wrote %s", config.FileName)

	updated, err := a.updateGitignore()
	if err != nil {
		return err
	}
	if len(updated) > 0 {
		a.info("added %s to .gitignore", strings.Join(updated, ", "))
	} else {
		a.info(".gitignore already ignores the raw output directory")
	}

	a.printf("\nNext steps:\n")
	a.printf("  1. benchbook run              # record the first run into %s\n", store.CurrentFile)
	a.printf("  2. benchbook milestone -m \"baseline\"\n")
	a.printf("  3. benchbook build && benchbook serve\n")
	a.printf("\n%s\n", justfileSnippet)
	return nil
}

// updateGitignore appends the entries that are missing and returns the ones it
// added, so the command can report exactly what it changed.
func (a *App) updateGitignore() ([]string, error) {
	path := filepath.Join(a.dir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(existing)
	added := []string{}
	for _, entry := range gitignoreEntries {
		if containsLine(content, entry) {
			continue
		}
		added = append(added, entry)
	}
	if len(added) == 0 {
		return nil, nil
	}
	suffix := ""
	if content != "" && !strings.HasSuffix(content, "\n") {
		suffix = "\n"
	}
	block := suffix + "\n# benchbook raw tool output (regenerable, large)\n" + strings.Join(added, "\n") + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, configFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open %s for append: %w", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(block); err != nil {
		return nil, fmt.Errorf("append to %s: %w", path, err)
	}
	return added, nil
}

// containsLine reports whether a file already contains the given line.
func containsLine(content, line string) bool {
	for _, existing := range strings.Split(content, "\n") {
		if strings.TrimSpace(existing) == line {
			return true
		}
	}
	return false
}
