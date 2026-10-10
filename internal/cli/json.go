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
	"oku add", "oku cache list", "oku doctor", "oku du", "oku env", "oku gc", "oku generations",
	"oku info", "oku key list", "oku list", "oku manifest lint", "oku outdated", "oku remove",
	"oku rollback", "oku search", "oku self update", "oku service list", "oku service restart",
	"oku service start", "oku service status", "oku service stop", "oku source list", "oku sync",
	"oku update", "oku verify", "oku which", "oku why",
}

// jsonOnlyWith names the flags that make a command which changes the machine or
// the oku binary print JSON. Without one of them it prints none.
var jsonOnlyWith = map[string][]string{
	"oku add":         {dryRunFlag},
	"oku gc":          {dryRunFlag},
	"oku remove":      {dryRunFlag},
	"oku rollback":    {dryRunFlag},
	"oku self update": {"check"},
	"oku sync":        {dryRunFlag},
	"oku update":      {dryRunFlag},
}

// checkJSON refuses --json on a command that has no JSON output, or that
// prints it only with a flag the command line lacks.
func checkJSON(cmd *cobra.Command) error {
	if !wantJSON(cmd) {
		return nil
	}

	if !slices.Contains(jsonCommands, cmd.CommandPath()) {
		return fmt.Errorf("%s has no --json output", cmd.CommandPath())
	}

	flags, ok := jsonOnlyWith[cmd.CommandPath()]
	if ok && !slices.ContainsFunc(flags, func(flag string) bool {
		on, _ := cmd.Flags().GetBool(flag)

		return on
	}) {
		return fmt.Errorf("%s prints JSON only with --%s", cmd.CommandPath(), flags[0])
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
