package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

const jsonFlag = "json"

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
