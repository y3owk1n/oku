package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
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

			answer := whyReport(pkgs, args[0])

			if wantJSON(cmd) {
				return printJSON(cmd, answer)
			}

			out := cmd.OutOrStdout()

			if answer.InList != "" {
				fmt.Fprintf(
					out,
					"%s %s is in your list, from %s\n",
					answer.Name,
					answer.Version,
					answer.InList,
				)
			}

			for _, user := range answer.NeededBy {
				fmt.Fprintf(
					out,
					"%s %s is needed by %s %s\n",
					answer.Name,
					strings.Join(user.DepVersions, ", "),
					user.Name,
					user.Version,
				)
			}

			found := answer.InList != "" || len(answer.NeededBy) > 0

			if !found {
				return fmt.Errorf("%s is neither in your list nor a dep of anything in it", args[0])
			}

			return nil
		},
	}
}

// whyAnswer is the JSON form of "oku why".
type whyAnswer struct {
	Name string `json:"name"`
	// Version is the listed version, or empty when the package is only a dep.
	Version string `json:"version,omitempty"`
	// InList is the ref that put the package in the list, or empty.
	InList   string    `json:"in_list"`
	NeededBy []whyUser `json:"needed_by"`
}

type whyUser struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	DepVersions []string `json:"dep_versions"`
}

func whyReport(pkgs []profile.Package, name string) whyAnswer {
	answer := whyAnswer{Name: name, NeededBy: []whyUser{}}

	for _, pkg := range pkgs {
		if pkg.Name == name {
			answer.Version, answer.InList = pkg.Version, pkg.Ref
		}

		var versions []string

		for _, path := range pkg.Closure {
			meta, err := store.ReadMeta(path)
			if err == nil && meta.Name == name && !slices.Contains(versions, meta.Version) {
				versions = append(versions, meta.Version)
			}
		}

		if len(versions) > 0 {
			answer.NeededBy = append(answer.NeededBy, whyUser{pkg.Name, pkg.Version, versions})
		}
	}

	return answer
}
