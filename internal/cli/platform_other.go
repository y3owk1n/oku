//go:build !windows

package cli

import "github.com/spf13/cobra"

// platformCommands returns the hidden commands that only one OS needs.
func platformCommands() []*cobra.Command { return nil }
