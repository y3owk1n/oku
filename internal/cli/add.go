package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
)

func newAddCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "add <ref>[@version]",
		Short: "Install a package from a manifest",
		Long: `Install a package from a manifest. A ref is one of:

  ./pkg.toml                          a local file
  https://host/pkg.toml               a URL
  github:owner/repo                   oku.pkg.toml in a GitHub repo
  github:owner/repo#name              name.toml or packages/name.toml in it
  git+https://host/repo#path/pkg.toml a file in any git repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, opts, args[0])
		},
	}
}

func runAdd(cmd *cobra.Command, opts Options, arg string) error {
	r, err := ref.Parse(arg)
	if err != nil {
		return err
	}

	e, err := loadEnv()
	if err != nil {
		return err
	}

	fetched, err := e.fetcher(opts).Fetch(cmd.Context(), r, "")
	if err != nil {
		return err
	}

	m, err := manifest.Parse(fetched.Data, r.String())
	if err != nil {
		return err
	}

	if r.Version != "" && r.Version != m.Version.Value {
		return fmt.Errorf(
			"%s provides version %s, not %s",
			r, m.Version.Value, r.Version,
		)
	}

	host := platform.Host()

	artifact, ok, err := m.Select(host)
	if err != nil {
		return err
	}

	if !ok && m.HasBuild() {
		return fmt.Errorf(
			"%s has no artifact for %s, and building from source is not supported so far",
			m.Package.Name, host,
		)
	}

	if !ok {
		return fmt.Errorf("%s has no artifact for %s", m.Package.Name, host)
	}

	storePath, err := e.store().Realize(cmd.Context(), m, artifact, host)
	if err != nil {
		return err
	}

	prof := e.globalProfile()

	err = prof.Add(profile.Package{
		Name:      m.Package.Name,
		Version:   m.Version.Value,
		Ref:       r.String(),
		StorePath: storePath,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", m.Package.Name, m.Version.Value)

	if !slices.Contains(filepath.SplitList(os.Getenv("PATH")), prof.BinDir()) {
		fmt.Fprintf(cmd.ErrOrStderr(), "add %s to PATH to run it\n", prof.BinDir())
	}

	return nil
}
