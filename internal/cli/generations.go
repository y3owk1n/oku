package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
)

func newGenerationsCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "generations",
		Short: "List the profile's generations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			gens, err := e.profile().Generations()
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				type pkgRow struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				}

				type row struct {
					Number   int       `json:"number"`
					Current  bool      `json:"current"`
					Created  time.Time `json:"created"`
					Packages []pkgRow  `json:"packages"`
				}

				rows := []row{}

				for _, gen := range gens {
					held := []pkgRow{}
					for _, pkg := range gen.Packages {
						held = append(held, pkgRow{pkg.Name, pkg.Version})
					}

					rows = append(rows, row{gen.Number, gen.Current, gen.Created, held})
				}

				return printJSON(cmd, rows)
			}

			if len(gens) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no generations yet")

				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			for _, gen := range gens {
				marker := " "
				if gen.Current {
					marker = "*"
				}

				fmt.Fprintf(
					w,
					"%s %d\t%s\t%s\n",
					marker,
					gen.Number,
					gen.Created.Local().Format("2006-01-02 15:04"),
					describe(gen.Packages),
				)
			}

			return w.Flush()
		},
	}
}

func describe(pkgs []profile.Package) string {
	if len(pkgs) == 0 {
		return "(empty)"
	}

	names := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		names[i] = pkg.Name + " " + pkg.Version
	}

	return strings.Join(names, ", ")
}

func newRollbackCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback [generation]",
		Short: "Switch the profile and oku.lock back to an earlier generation",
		Long: `Switch the profile and oku.lock back to an earlier generation.

Without a number, rollback goes to the generation before the current one. The
switch is one link change, because every generation's packages are still in
the store. Rollback does not change oku.toml.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(cmd, opts, args)
		},
	}
}

func runRollback(cmd *cobra.Command, opts Options, args []string) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	prof := e.profile()

	gens, err := prof.Generations()
	if err != nil {
		return err
	}

	at := slices.IndexFunc(gens, func(g profile.Generation) bool { return g.Current })
	if at < 0 {
		return errors.New("the profile has no generation to roll back from")
	}

	var target profile.Generation

	switch {
	case len(args) == 1:
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("%q is not a generation number", args[0])
		}

		i := slices.IndexFunc(gens, func(g profile.Generation) bool { return g.Number == n })
		if i < 0 {
			return fmt.Errorf("generation %d does not exist, see `oku generations`", n)
		}

		target = gens[i]
	case at == 0:
		return fmt.Errorf(
			"generation %d is the oldest, there is nothing before it",
			gens[at].Number,
		)
	default:
		target = gens[at-1]
	}

	if target.Current {
		return fmt.Errorf("generation %d is already active", target.Number)
	}

	snapshot, err := prof.Switch(target.Number)
	if err != nil {
		return err
	}

	if err := e.syncExposed(cmd, opts, false); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "generation %d is active: %s\n", target.Number, describe(target.Packages))

	if snapshot == nil {
		fmt.Fprintf(
			cmd.ErrOrStderr(),
			"generation %d has no saved lock, so %s was not changed and `oku sync` may undo this rollback\n",
			target.Number,
			e.lockPath(),
		)

		return nil
	}

	if err := list.WriteFile(e.lockPath(), snapshot); err != nil {
		return fmt.Errorf("restore %s: %w", e.lockPath(), err)
	}

	// The list is the user's file. Rollback only reports where it disagrees.
	own, err := list.Read(e.listPath())
	if err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(own.Packages)) {
		if !slices.ContainsFunc(
			target.Packages,
			func(p profile.Package) bool { return p.Name == name },
		) {
			fmt.Fprintf(
				cmd.ErrOrStderr(),
				"%s still lists %s, so `oku sync` will install it again. Run `oku remove %s` to drop it.\n",
				e.listPath(),
				name,
				name,
			)
		}
	}

	return nil
}
