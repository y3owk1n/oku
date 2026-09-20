package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/store"
)

func newSyncCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Make the profile match oku.toml at the versions in oku.lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return reconcile(cmd, opts, nil, false)
		},
	}
}

func newUpdateCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "update [name...]",
		Short: "Re-resolve packages from their refs and rewrite oku.lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			return reconcile(cmd, opts, args, true)
		},
	}
}

// reconcile installs every package of oku.toml, activates a generation holding
// exactly those, and rewrites oku.lock to match.
//
// Without update, a locked package is read at its locked commit and must still
// have its locked manifest hash. With update, the packages in names, or all of
// them when names is empty, are read fresh and their lock entries replaced.
func reconcile(cmd *cobra.Command, opts Options, names []string, update bool) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	listed, err := list.Read(e.listPath())
	if err != nil {
		return err
	}

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	for _, name := range names {
		if _, ok := listed.Packages[name]; !ok {
			return fmt.Errorf("%s is not in %s", name, e.listPath())
		}
	}

	out := cmd.OutOrStdout()
	host := platform.Host().String()
	next := &lock.Lock{}

	var pkgs []profile.Package

	for _, name := range slices.Sorted(maps.Keys(listed.Packages)) {
		entry := listed.Packages[name]

		r, err := ref.Parse(entry.Ref)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		r.Version = entry.Version

		previous, _ := locked.Find(name)
		fresh := update && (len(names) == 0 || slices.Contains(names, name))

		commit := previous.Commit
		if fresh || previous.Ref != r.String() {
			commit = ""
		}

		locksManifest := previous.Ref == r.String() && previous.ManifestSHA256 != ""

		wantManifest := ""
		if locksManifest && !fresh {
			wantManifest = previous.ManifestSHA256
		}

		got, err := e.install(cmd.Context(), opts, request{
			ref:          r,
			commit:       commit,
			previous:     previous,
			wantManifest: wantManifest,
			acceptDigest: fresh,
		})

		switch {
		case errors.Is(err, errManifestChanged):
			return fmt.Errorf("%s: %w\nrun `oku update %s` to accept it", name, err, name)
		case errors.Is(err, store.ErrPinConflict) && !fresh:
			return fmt.Errorf("%w\nrun `oku update %s` to accept the new checksum", err, name)
		case err != nil:
			return fmt.Errorf("%s: %w", name, err)
		case got.lock.Name != name:
			return fmt.Errorf(
				"%s lists %s, but the manifest at %s is named %s",
				e.listPath(), name, r, got.lock.Name,
			)
		}

		switch {
		case !locksManifest:
			fmt.Fprintf(out, "%s %s\n", name, got.lock.Version)
		case previous.Version != got.lock.Version:
			fmt.Fprintf(out, "%s %s -> %s\n", name, previous.Version, got.lock.Version)
		case previous.ManifestSHA256 != got.lock.ManifestSHA256:
			fmt.Fprintf(out, "%s %s, manifest changed\n", name, got.lock.Version)
		case previous.Platforms[host] != (lock.Platform{}) &&
			previous.Platforms[host].SHA256 != got.lock.Platforms[host].SHA256:
			fmt.Fprintf(out, "%s %s, checksum changed\n", name, got.lock.Version)
		}

		e.reportFirstUse(cmd.ErrOrStderr(), got)

		next.Set(got.lock)
		pkgs = append(pkgs, got.profile)
	}

	changed, err := e.globalProfile().Replace(pkgs)
	if err != nil {
		return err
	}

	if err := next.Write(e.lockPath()); err != nil {
		return err
	}

	if changed {
		noun := "packages"
		if len(pkgs) == 1 {
			noun = "package"
		}

		fmt.Fprintf(out, "profile now holds %d %s\n", len(pkgs), noun)
	} else {
		fmt.Fprintln(out, "already in sync")
	}

	return nil
}
