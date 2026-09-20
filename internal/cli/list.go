package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			pkgs, err := e.profile().Packages()
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				type row struct {
					Name      string `json:"name"`
					Version   string `json:"version"`
					Ref       string `json:"ref"`
					StorePath string `json:"store_path"`
					Service   bool   `json:"service"`
					System    bool   `json:"system"`
				}

				rows := []row{}
				for _, pkg := range pkgs {
					rows = append(rows, row{
						pkg.Name, pkg.Version, pkg.Ref, pkg.StorePath, pkg.Service, pkg.System,
					})
				}

				return printJSON(cmd, rows)
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
