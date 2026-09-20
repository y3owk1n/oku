package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/store"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show what oku knows about an installed package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			pkgs, err := e.globalProfile().Packages()
			if err != nil {
				return err
			}

			i := slices.IndexFunc(pkgs, func(p profile.Package) bool { return p.Name == args[0] })
			if i < 0 {
				return fmt.Errorf(
					"%s is not installed, see `oku list` and `oku why %s`",
					args[0],
					args[0],
				)
			}

			locked, err := lock.Read(e.lockPath())
			if err != nil {
				return err
			}

			pkg := pkgs[i]
			entry, _ := locked.Find(pkg.Name)
			at := entry.Platforms[platform.Host().String()]
			out := cmd.OutOrStdout()

			row := func(label, value string) {
				if value != "" {
					fmt.Fprintf(out, "%-10s %s\n", label, value)
				}
			}

			row("name", pkg.Name)
			row("version", pkg.Version)
			row("ref", pkg.Ref)
			row("commit", entry.Commit)
			row("installed", at.Strategy)
			row("store", pkg.StorePath)

			if entry.Inferred {
				row("manifest", "inferred by oku from the repo's releases")
			}

			if at.Impure {
				row("impure", "a build step used the network, so this build is not reproducible")
			}

			if at.VendorSHA256 != "" {
				row("vendored", "sha256 "+at.VendorSHA256)
			}

			var deps []string

			for _, path := range pkg.Closure {
				if meta, err := store.ReadMeta(path); err == nil {
					deps = append(deps, meta.Name+" "+meta.Version)
				}
			}

			row("deps", strings.Join(deps, ", "))

			bins, _ := filepath.Glob(filepath.Join(pkg.StorePath, "bin", "*"))
			for i, bin := range bins {
				bins[i] = filepath.Base(bin)
			}

			row("programs", strings.Join(bins, ", "))

			return nil
		},
	}
}
