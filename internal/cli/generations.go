package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ui"
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

				type fileRow struct {
					Target string `json:"target"`
					Link   string `json:"link,omitempty"`
				}

				type settingRow struct {
					Domain string `json:"domain"`
					Key    string `json:"key"`
					Value  string `json:"value"`
				}

				type row struct {
					Number   int          `json:"number"`
					From     int          `json:"from,omitempty"`
					Current  bool         `json:"current"`
					Created  time.Time    `json:"created"`
					Packages []pkgRow     `json:"packages"`
					Files    []fileRow    `json:"files"`
					Settings []settingRow `json:"settings"`
				}

				rows := []row{}

				for _, gen := range gens {
					held := []pkgRow{}
					for _, pkg := range gen.Packages {
						held = append(held, pkgRow{pkg.Name, pkg.Version})
					}

					files := []fileRow{}
					for _, f := range gen.Files {
						files = append(files, fileRow{f.Target, f.Link})
					}

					settings := []settingRow{}
					for _, s := range gen.Settings {
						settings = append(settings, settingRow{s.Domain, s.Key, s.Value})
					}

					rows = append(rows, row{
						gen.Number, gen.From, gen.Current, gen.Created, held, files, settings,
					})
				}

				return printJSON(cmd, rows)
			}

			if len(gens) == 0 {
				hint(cmd.OutOrStdout(), "no generations yet, the first `oku add` or `oku sync` makes one")

				return nil
			}

			out := cmd.OutOrStdout()
			s := ui.For(out)
			tab := s.Table("", "created", "holds", "changes")

			var previous profile.Generation

			for _, gen := range gens {
				// A generation says what changed from the one it replaced, which after
				// a rollback is not the one numbered before it.
				base, prefix := previous, ""
				if i := slices.IndexFunc(gens, func(g profile.Generation) bool {
					return g.Number == gen.From
				}); i >= 0 && gen.From != previous.Number {
					base, prefix = gens[i], s.Dim("from "+strconv.Itoa(gen.From)+",")+" "
				}

				marker, mark := " ", s.Dim
				if gen.Current {
					marker, mark = s.Pick("●", "*"), s.Bold
				}

				cells := []string{
					marker + " " + strconv.Itoa(gen.Number),
					gen.Created.Local().Format("2006-01-02 15:04"),
					holds(gen),
					prefix + changes(s, base, gen),
				}

				if gen.Current {
					tab.Styled(cells, mark, nil, nil, nil)
				} else {
					tab.Styled(cells, nil, s.Dim, s.Dim, nil)
				}

				previous = gen
			}

			return tab.Write(out)
		},
	}
}

// count returns "3 packages", or "1 package".
func count(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}

	return strconv.Itoa(n) + " " + noun
}

// holds counts what a generation holds: "3 packages, 2 files, 1 setting". Files
// and settings appear when there are any.
func holds(gen profile.Generation) string {
	parts := []string{count(len(gen.Packages), "package")}

	if len(gen.Files) > 0 {
		parts = append(parts, count(len(gen.Files), "file"))
	}

	if len(gen.Settings) > 0 {
		parts = append(parts, count(len(gen.Settings), "setting"))
	}

	return strings.Join(parts, ", ")
}

// changes says what differs between two generations: which packages came,
// went, or changed version, and the same for files and settings. A long list
// ends in "and N more", because the reader wants the shape of a change, not
// every name.
func changes(s ui.Style, from, to profile.Generation) string {
	const show = 6

	find := func(pkgs []profile.Package, name string) int {
		return slices.IndexFunc(pkgs, func(p profile.Package) bool { return p.Name == name })
	}

	var parts []string

	for _, pkg := range from.Packages {
		if find(to.Packages, pkg.Name) < 0 {
			parts = append(parts, s.Bad("-")+" "+pkg.Name)
		}
	}

	for _, pkg := range to.Packages {
		i := find(from.Packages, pkg.Name)

		switch {
		case i < 0:
			parts = append(parts, s.Good("+")+" "+pkg.Name+" "+pkg.Version)
		case from.Packages[i].Version != pkg.Version:
			parts = append(
				parts, pkg.Name+" "+from.Packages[i].Version+" "+s.Arrow()+" "+pkg.Version,
			)
		case from.Packages[i].StorePath != pkg.StorePath:
			parts = append(parts, pkg.Name+" rebuilt")
		}
	}

	parts = append(parts, fileChanges(s, from.Files, to.Files)...)
	parts = append(parts, settingChanges(s, from.Settings, to.Settings)...)

	switch {
	case len(parts) == 0 && from.Number == 0:
		return s.Dim("empty")
	case len(parts) == 0:
		return s.Dim("no change")
	}

	if len(parts) > show {
		parts = append(parts[:show], fmt.Sprintf("and %d more", len(parts)-show))
	}

	return strings.Join(parts, ", ")
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

	if err := e.recoverPending(cmd, opts); err != nil {
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

	snapshot, err := prof.LockSnapshotOf(target.Number)
	if err != nil {
		return err
	}

	err = e.apply(cmd, opts, change{
		to: target.Number,
		commit: func() error {
			if snapshot == nil {
				return nil
			}

			if err := list.WriteFile(e.lockPath(), snapshot); err != nil {
				return fmt.Errorf("restore %s: %w", e.lockPath(), err)
			}

			return nil
		},
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)
	fmt.Fprintln(out, s.Done(fmt.Sprintf(
		"generation %d is active, %s: %s",
		target.Number, holds(target), changes(s, gens[at], target),
	)))

	if snapshot == nil {
		fmt.Fprintf(
			cmd.ErrOrStderr(),
			"generation %d has no saved lock, so %s was not changed and `oku sync` may undo this rollback\n",
			target.Number,
			e.lockPath(),
		)

		return nil
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
			warn(
				cmd.ErrOrStderr(),
				"%s still lists %s, so `oku sync` will install it again. Run `oku remove %s` to drop it.",
				e.listPath(),
				name,
				name,
			)
		}
	}

	// An included list may name the package, and oku does not read it here.
	if len(own.Include) > 0 {
		return nil
	}

	for _, pkg := range target.Packages {
		if _, ok := own.Packages[pkg.Name]; !ok {
			warn(
				cmd.ErrOrStderr(),
				"%s does not list %s, so `oku sync` will remove it again. Run `oku add %s` to keep it.",
				e.listPath(),
				pkg.Name,
				pkg.Ref,
			)
		}
	}

	return nil
}
