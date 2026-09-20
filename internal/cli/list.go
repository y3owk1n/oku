package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			pkgs, err := e.globalProfile().Packages()
			if err != nil {
				return err
			}

			if len(pkgs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no packages installed")

				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, pkg := range pkgs {
				fmt.Fprintf(w, "%s\t%s\t%s\n", pkg.Name, pkg.Version, pkg.Ref)
			}

			return w.Flush()
		},
	}
}
