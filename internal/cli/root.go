// Package cli holds the cobra commands.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the oku command tree.
func NewRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:           "oku",
		Short:         "A cross-platform package manager with no central registry",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
