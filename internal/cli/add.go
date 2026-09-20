package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
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

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	// A digest that oku.lock pinned for this version and URL still applies, even
	// when the manifest gives none.
	previous, _ := locked.Find(m.Package.Name)

	pinned := ""
	if at := previous.Platforms[host.String()]; previous.Version == m.Version.Value &&
		at.URL == artifact.URL {
		pinned = at.SHA256
	}

	realized, err := e.store().Realize(cmd.Context(), m, artifact, host, pinned)
	if err != nil {
		return err
	}

	prof := e.globalProfile()

	err = prof.Add(profile.Package{
		Name:      m.Package.Name,
		Version:   m.Version.Value,
		Ref:       r.String(),
		StorePath: realized.Path,
	})
	if err != nil {
		return err
	}

	// Entries for other platforms stay while they describe the same manifest.
	platforms := map[string]lock.Platform{}
	if previous.ManifestSHA256 == m.SHA256 && previous.Ref == r.String() {
		platforms = previous.Platforms
	}

	platforms[host.String()] = lock.Platform{
		Strategy: "artifact",
		URL:      artifact.URL,
		SHA256:   realized.SHA256,
	}

	locked.Set(lock.Package{
		Name:           m.Package.Name,
		Ref:            r.String(),
		Commit:         fetched.Commit,
		ManifestSHA256: m.SHA256,
		Version:        m.Version.Value,
		Platforms:      platforms,
	})

	err = list.Set(e.listPath(), m.Package.Name, list.Entry{Ref: r.String(), Version: r.Version})
	if err != nil {
		return err
	}

	if err := locked.Write(e.lockPath()); err != nil {
		return err
	}

	if realized.FirstUse {
		fmt.Fprintf(
			cmd.ErrOrStderr(),
			"%s publishes no checksum, so oku trusted this download and pinned sha256 %s in %s\n",
			m.Package.Name, realized.SHA256, e.lockPath(),
		)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", m.Package.Name, m.Version.Value)

	if !slices.Contains(filepath.SplitList(os.Getenv("PATH")), prof.BinDir()) {
		fmt.Fprintf(cmd.ErrOrStderr(), "add %s to PATH to run it\n", prof.BinDir())
	}

	return nil
}
