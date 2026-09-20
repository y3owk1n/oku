package cli

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/source"
)

func (e env) configPath() string {
	return filepath.Join(e.config, source.FileName)
}

func newSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Name the manifest collections you install from",
		Long: `Name the manifest collections you install from.

A collection is a repo or a directory of manifests named <name>.toml, at its
root or under packages/. After "oku source add core github:someone/recipes",
"oku add core/ripgrep" means "oku add github:someone/recipes#ripgrep". oku ships
with no sources.`,
	}

	add := &cobra.Command{
		Use:   "add <alias> <ref>",
		Short: "Give a collection an alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			config, err := source.Read(e.configPath())
			if err != nil {
				return err
			}

			if err := config.Add(args[0], args[1]); err != nil {
				return err
			}

			if err := config.Write(e.configPath()); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s is %s\n", args[0], config.Sources[args[0]])

			return nil
		},
	}

	remove := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Forget an alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			config, err := source.Read(e.configPath())
			if err != nil {
				return err
			}

			if _, ok := config.Sources[args[0]]; !ok {
				return fmt.Errorf("%s is not a source, see `oku source list`", args[0])
			}

			delete(config.Sources, args[0])

			if err := config.Write(e.configPath()); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])

			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List your aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			config, err := source.Read(e.configPath())
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				type row struct {
					Alias string `json:"alias"`
					Ref   string `json:"ref"`
				}

				rows := []row{}
				for _, alias := range slices.Sorted(maps.Keys(config.Sources)) {
					rows = append(rows, row{alias, config.Sources[alias]})
				}

				return printJSON(cmd, rows)
			}

			if len(config.Sources) == 0 {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					"no sources, add one with `oku source add <alias> <ref>`",
				)

				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, alias := range slices.Sorted(maps.Keys(config.Sources)) {
				fmt.Fprintf(w, "%s\t%s\n", alias, config.Sources[alias])
			}

			return w.Flush()
		},
	}

	cmd.AddCommand(add, remove, list)

	return cmd
}

func newSearchCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Search the names and descriptions of packages in your sources",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			config, err := source.Read(e.configPath())
			if err != nil {
				return err
			}

			if len(config.Sources) == 0 {
				return fmt.Errorf(
					"you have no sources to search, add one with `oku source add <alias> <ref>`",
				)
			}

			term := strings.ToLower(args[0])
			fetcher := e.fetcher(opts)
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			hits := 0

			type hit struct {
				Ref         string `json:"ref"`
				Description string `json:"description"`
			}

			found := []hit{}

			for _, alias := range slices.Sorted(maps.Keys(config.Sources)) {
				r, err := ref.Parse(config.Sources[alias])
				if err != nil {
					return fmt.Errorf("source %s: %w", alias, err)
				}

				files, err := fetcher.ListManifests(cmd.Context(), r)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "skipped source %s: %v\n", alias, err)

					continue
				}

				for _, name := range slices.Sorted(maps.Keys(files)) {
					// A collection may hold TOML files that are not manifests.
					m, err := manifest.Parse(files[name], name)
					if err != nil {
						continue
					}

					if strings.Contains(strings.ToLower(m.Package.Name), term) ||
						strings.Contains(strings.ToLower(m.Package.Description), term) {
						fmt.Fprintf(w, "%s/%s\t%s\n", alias, name, m.Package.Description)

						found = append(found, hit{alias + "/" + name, m.Package.Description})
						hits++
					}
				}
			}

			if wantJSON(cmd) {
				return printJSON(cmd, found)
			}

			if hits == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "nothing in your sources matches %q\n", args[0])

				return nil
			}

			return w.Flush()
		},
	}
}
