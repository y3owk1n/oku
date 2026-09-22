package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/ui"
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
				fmt.Fprintln(
					cmd.OutOrStdout(),
					"no packages installed, `oku add <ref>` installs one",
				)

				return nil
			}

			out := cmd.OutOrStdout()
			s := ui.For(out)
			tab := s.Table("name", "version", "ref", "")

			for _, pkg := range pkgs {
				cells := []string{pkg.Name, pkg.Version, s.Home(pkg.Ref)}

				// A terminal gets a column that says what else the package does.
				if s.On() {
					var marks []string
					if pkg.Service {
						marks = append(marks, "service")
					}

					if pkg.System {
						marks = append(marks, "system")
					}

					cells = append(cells, strings.Join(marks, " "))
				}

				tab.Styled(cells, s.Bold, nil, s.Dim, s.Accent)
			}

			if err := tab.Write(out); err != nil {
				return err
			}

			// A terminal gets a footer with the count and which list this is.
			if s.On() {
				where := "the global list"
				if e.project != "" {
					where = "the project " + s.Home(e.project)
				}

				fmt.Fprintln(out, s.Dim(count(len(pkgs), "package")+" in "+where))
			}

			return nil
		},
	}
}
