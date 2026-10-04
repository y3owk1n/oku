package cli

import (
	"encoding/json"
	"fmt"
	"slices"

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

// noArgs is cobra.NoArgs with the usage line under the error, for a command
// without subcommands.
func noArgs(cmd *cobra.Command, args []string) error {
	return maxArgs(0)(cmd, args)
}

// maxArgs is cobra.MaximumNArgs with the usage line under the error.
func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return fmt.Errorf("too many arguments\nusage: %s", cmd.UseLine())
		}

		return nil
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

// jsonCommands are the commands that print JSON with --json. Any other command
// refuses the flag, so that a script never reads text as JSON.
var jsonCommands = []string{
	"oku add", "oku cache list", "oku doctor", "oku du", "oku env", "oku generations",
	"oku info", "oku key list", "oku list", "oku manifest lint", "oku outdated",
	"oku search", "oku service list", "oku service restart", "oku service start",
	"oku service status", "oku service stop", "oku source list", "oku verify", "oku which", "oku why",
}

// checkJSON refuses --json on a command that has no JSON output. "oku add"
// prints JSON only for --plan.
func checkJSON(cmd *cobra.Command) error {
	if !wantJSON(cmd) {
		return nil
	}

	if !slices.Contains(jsonCommands, cmd.CommandPath()) {
		return fmt.Errorf("%s has no --json output", cmd.CommandPath())
	}

	if plan, err := cmd.Flags().GetBool("plan"); err == nil && !plan {
		return fmt.Errorf("%s prints JSON only with --plan", cmd.CommandPath())
	}

	return nil
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
