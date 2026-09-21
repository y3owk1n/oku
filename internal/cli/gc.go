package cli

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/status"
)

func newGCCmd() *cobra.Command {
	var (
		keep   int
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Delete store paths that no generation uses",
		Long: `Delete store paths that no generation of any profile uses.

Old generations keep their packages in the store so rollback needs no download.
--keep deletes old generations first, which frees the packages only they use.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("keep") && keep < 1 {
				return fmt.Errorf("--keep must be at least 1, got %d", keep)
			}

			return runGC(cmd, keep, dryRun)
		},
	}

	cmd.Flags().
		IntVar(&keep, "keep", 0, "first delete all but the newest N generations of each profile")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be deleted and delete nothing")

	return cmd
}

func runGC(cmd *cobra.Command, keep int, dryRun bool) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	// A revert needs the generation and the store paths it goes back to.
	if p, err := e.readPending(); err != nil || p != nil {
		return errors.Join(err, errors.New(
			"the last change did not finish. Run `oku sync` first, which puts the machine back",
		))
	}

	profiles, err := profile.All(e.data)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	verb := "removed"

	if dryRun {
		verb = "would remove"
	}

	pruned := map[*profile.Profile][]int{}

	if keep > 0 {
		for _, prof := range profiles {
			if pruned[prof], err = prof.Prune(keep, dryRun); err != nil {
				return err
			}

			for _, n := range pruned[prof] {
				fmt.Fprintf(out, "%s generation %d\n", verb, n)
			}
		}
	}

	used := map[string]bool{}

	for _, prof := range profiles {
		gens, err := prof.Generations()
		if err != nil {
			return err
		}

		for _, gen := range gens {
			// A dry run deleted nothing, so skip the generations it would have pruned.
			if slices.Contains(pruned[prof], gen.Number) {
				continue
			}

			for _, pkg := range gen.Packages {
				used[pkg.StorePath] = true

				for _, dep := range pkg.Closure {
					used[dep] = true
				}
			}
		}
	}

	unused := map[string]int64{}

	var freed int64

	for _, st := range e.stores() {
		done := status.Start(cmd.Context(), "scanning the store for unused packages")
		found, err := st.Unreferenced(used)

		done()

		if err != nil {
			return err
		}

		for _, path := range slices.Sorted(maps.Keys(found)) {
			if !dryRun {
				if err := st.Remove(path); err != nil {
					return fmt.Errorf("delete %s: %w", path, err)
				}
			}

			unused[path] = found[path]
			freed += found[path]
			fmt.Fprintf(out, "%s %s (%s)\n", verb, filepath.Base(path), status.Size(found[path]))
		}
	}

	if len(unused) == 0 {
		fmt.Fprintln(out, "nothing to delete, every store path is used by a generation")

		return nil
	}

	noun := "store paths"
	if len(unused) == 1 {
		noun = "store path"
	}

	summary := "freed"
	if dryRun {
		summary = "would free"
	}

	fmt.Fprintf(out, "%s %s from %d %s\n", summary, status.Size(freed), len(unused), noun)

	return nil
}
