package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

func newVerifyCmd(opts Options) *cobra.Command {
	var repair, record bool

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check that the installed packages hold the files oku put there",
		Long: `Check that the installed packages hold the files oku put there.

verify hashes every file of the packages of the list in use and of their
deps, and compares them with what oku recorded when it installed them. It
names each file that changed, appeared or went away, and exits with code 1
when one did. --repair removes those packages from the store, so that the
next oku sync downloads them again and checks them against oku.lock.

A package that an older oku installed has no record. --record writes one from
its files as they are now, so that later runs check it.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVerify(cmd, opts, repair, record)
		},
	}

	cmd.Flags().BoolVar(&repair, "repair", false,
		"remove the packages that changed, so the next oku sync installs them again")
	cmd.Flags().BoolVar(&record, "record", false,
		"record the files of the packages that have no record, as they are now")

	return cmd
}

// verified is the result of verify for one store path.
type verified struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	// Status is "ok", "changed", "unrecorded", or "recorded" with --record.
	Status  string         `json:"status"`
	Changes []store.Change `json:"changes"`
}

func runVerify(cmd *cobra.Command, opts Options, repair, record bool) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	pkgs, err := e.profile().Packages()
	if err != nil {
		return err
	}

	var paths []string

	for _, pkg := range pkgs {
		for _, path := range append([]string{pkg.StorePath}, pkg.Closure...) {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}

	var (
		results = []verified{}
		changed []verified
	)

	for _, path := range paths {
		meta, err := store.ReadMeta(path)
		if err != nil {
			return err
		}

		v := verified{Name: meta.Name, Version: meta.Version, Path: path, Status: "ok", Changes: []store.Change{}}

		changes, err := store.Check(path)

		switch {
		case errors.Is(err, store.ErrNoTree) && record:
			if err := store.Record(path); err != nil {
				return fmt.Errorf("%s %s: %w", meta.Name, meta.Version, err)
			}

			v.Status = "recorded"
		case errors.Is(err, store.ErrNoTree):
			v.Status = "unrecorded"
		case err != nil:
			return fmt.Errorf("%s %s: %w", meta.Name, meta.Version, err)
		case len(changes) > 0:
			v.Status, v.Changes = "changed", changes
			changed = append(changed, v)
		}

		results = append(results, v)
	}

	if wantJSON(cmd) {
		if err := printJSON(cmd, results); err != nil {
			return err
		}
	} else {
		printVerified(cmd, results)
	}

	if len(changed) == 0 {
		return nil
	}

	if !repair {
		return fmt.Errorf("the files of %s changed since oku installed them\n"+
			"run `oku verify --repair`, then `oku sync`, which downloads them again and checks them against oku.lock",
			count(len(changed), "package"))
	}

	for _, v := range changed {
		if err := e.store().Remove(v.Path); err != nil {
			return fmt.Errorf("remove %s: %w", filepath.Base(v.Path), err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "removed %s from the store, run `oku sync` to install what is missing\n",
		count(len(changed), "package"))

	return nil
}

// printVerified writes a line for each package, and the files that changed
// under it.
func printVerified(cmd *cobra.Command, results []verified) {
	out := cmd.OutOrStdout()
	s := ui.For(out)

	for _, v := range results {
		name := ui.Clean(v.Name + " " + v.Version)

		switch v.Status {
		case "ok":
			fmt.Fprintf(out, "%s %s\n", s.Check(), name)
		case "unrecorded":
			fmt.Fprintf(out, "%s %s: oku installed it before it recorded files, so it cannot check it, see --record\n",
				s.Note(), name)
		case "recorded":
			fmt.Fprintf(out, "%s %s: recorded its files as they are now\n", s.Note(), name)
		case "changed":
			fmt.Fprintf(out, "%s %s\n", s.Bad(s.Pick("✗", "changed")), name)

			for _, c := range v.Changes {
				fmt.Fprintf(out, "    %s %s\n", c.Kind, ui.Clean(c.Path))
			}
		}
	}
}
