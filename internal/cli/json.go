package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

const jsonFlag = "json"

// exactArgs is cobra.ExactArgs with a message that shows the command's form,
// so the user sees what was missing instead of a count.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		what := ""

		switch {
		case len(args) < n:
			what = "missing argument"
		case len(args) > n:
			what = "too many arguments"
		default:
			return nil
		}

		return fmt.Errorf("%s\nusage: %s", what, cmd.UseLine())
	}
}

// minArgs is cobra.MinimumNArgs with the usage line under the error.
func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return fmt.Errorf("missing argument\nusage: %s", cmd.UseLine())
		}

		return nil
	}
}

// wantJSON reports whether the user asked for JSON in place of text.
func wantJSON(cmd *cobra.Command) bool {
	asked, _ := cmd.Flags().GetBool(jsonFlag)

	return asked
}

// printJSON writes value as indented JSON. An empty list prints as [] and never
// as null, so a script can always iterate over it.
func printJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)

	return encoder.Encode(value)
}
