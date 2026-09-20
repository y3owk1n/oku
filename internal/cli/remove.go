package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a package from the profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			if err := e.globalProfile().Remove(args[0]); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])

			return nil
		},
	}
}
