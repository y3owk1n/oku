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
	"github.com/y3owk1n/oku/internal/ui"
)

func newInfoCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show what oku knows about an installed package",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			pkgs, err := e.profile().Packages()
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

			if wantJSON(cmd) {
				return printJSON(cmd, struct {
					Name         string `json:"name"`
					Version      string `json:"version"`
					Ref          string `json:"ref"`
					Commit       string `json:"commit,omitempty"`
					Installed    string `json:"installed"`
					StorePath    string `json:"store_path"`
					Inferred     bool   `json:"inferred"`
					Impure       bool   `json:"impure"`
					VendorSHA256 string `json:"vendor_sha256,omitempty"`
					SigningKey   string `json:"signing_key,omitempty"`
				}{
					pkg.Name, pkg.Version, pkg.Ref, entry.Commit, at.Strategy, pkg.StorePath,
					entry.Inferred, at.Impure, at.VendorSHA256, entry.SigningKey,
				})
			}

			s := ui.For(out)

			// A terminal gets the start of the commit and the strategy in words.
			commit, strategy := entry.Commit, at.Strategy
			if s.On() {
				if len(commit) > 12 {
					commit = commit[:12]
				}

				switch strategy {
				case strategyArtifact:
					strategy = "from a release download"
				case strategyBuild:
					strategy = "built from source"
				}
			}

			pairs := [][2]string{
				{"name", s.Bold(pkg.Name)},
				{"version", pkg.Version},
				{"ref", s.Home(pkg.Ref)},
				{"commit", commit},
				{"installed", strategy},
				{"store", s.Home(pkg.StorePath)},
			}

			if entry.Inferred {
				pairs = append(
					pairs,
					[2]string{"manifest", "inferred by oku from the repo's releases"},
				)
			}

			if at.Impure {
				pairs = append(pairs, [2]string{
					"impure",
					s.Warn("a build step used the network, so this build is not reproducible"),
				})
			}

			if at.VendorSHA256 != "" {
				pairs = append(pairs, [2]string{"vendored", "sha256 " + at.VendorSHA256})
			}

			var deps []string

			for _, path := range pkg.Closure {
				if meta, err := store.ReadMeta(path); err == nil {
					deps = append(deps, meta.Name+" "+meta.Version)
				}
			}

			pairs = append(pairs, [2]string{"deps", strings.Join(deps, ", ")})

			bins, _ := filepath.Glob(filepath.Join(pkg.StorePath, "bin", "*"))
			for i, bin := range bins {
				bins[i] = filepath.Base(bin)
			}

			pairs = append(pairs, [2]string{"programs", strings.Join(bins, ", ")})

			return s.KV(out, pairs...)
		},
	}
}
