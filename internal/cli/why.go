package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/store"
)

func newWhyCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "why <name>",
		Short: "Say why a package is in the store",
		Long: `Say why a package is in the store.

A dep is not linked into your profile, so "oku list" does not show it. "why"
names the installed packages that depend on it, directly or through another
dep.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			pkgs, err := e.profile().Packages()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			found := false

			for _, pkg := range pkgs {
				if pkg.Name == args[0] {
					fmt.Fprintf(
						out,
						"%s %s is in your list, from %s\n",
						pkg.Name,
						pkg.Version,
						pkg.Ref,
					)

					found = true
				}

				var versions []string

				for _, path := range pkg.Closure {
					meta, err := store.ReadMeta(path)
					if err == nil && meta.Name == args[0] &&
						!slices.Contains(versions, meta.Version) {
						versions = append(versions, meta.Version)
					}
				}

				if len(versions) > 0 {
					fmt.Fprintf(
						out,
						"%s %s is needed by %s %s\n",
						args[0],
						strings.Join(versions, ", "),
						pkg.Name,
						pkg.Version,
					)

					found = true
				}
			}

			if !found {
				return fmt.Errorf("%s is neither in your list nor a dep of anything in it", args[0])
			}

			return nil
		},
	}
}
