// Package cli. version - the `benchbook version` command, which needs neither a
// configuration file nor a repository.
package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------- Constructor(s) -------------------------------------- //

// newVersionCommand builds `benchbook version`.
func (a *App) newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:     cmdVersion,
		GroupID: groupSetUp,
		Short:   "Print the version, the schema version and the Go toolchain",
		Long: `Print the version of benchbook, the schema version it writes into every JSON
document, and the Go toolchain the binary was built with.

The schema version is the one to compare when a repository is shared between
machines: a document written by a newer benchbook is refused by an older one rather
than half-read.`,
		Example: "  benchbook version",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runVersion()
		},
	}
}

// ----------------------------------------- Public API ----------------------------------------- //

// runVersion prints the tool version, the schema version it writes and the toolchain
// it was built with.
func (a *App) runVersion() error {
	a.printf("benchbook %s\n", Version)
	a.printf("schema_version %d\n", model.SchemaVersion)
	a.printf("built with %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

// versionLine renders the one-line version used in generated pages, so a reader
// knows which tool produced a report.
func versionLine() string {
	return fmt.Sprintf("benchbook %s (schema %d)", Version, model.SchemaVersion)
}
