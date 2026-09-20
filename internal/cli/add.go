package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/source"
)

func newAddCmd(opts Options) *cobra.Command {
	var (
		flags      buildFlags
		fromSource bool
	)

	cmd := &cobra.Command{
		Use:   "add <ref>[@version]",
		Short: "Install a package from a manifest",
		Long: `Install a package from a manifest. A ref is one of:

  ./pkg.toml                          a local file
  https://host/pkg.toml               a URL
  github:owner/repo                   oku.pkg.toml in a GitHub repo
  github:owner/repo#name              name.toml or packages/name.toml in it
  git+https://host/repo#path/pkg.toml a file in any git repo
  alias/name                          a package in a source, see "oku source"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, opts, args[0], &flags, fromSource)
		},
	}

	flags.register(cmd)
	cmd.Flags().
		BoolVar(&fromSource, "from-source", false, "build from source even when a prebuilt download fits")

	return cmd
}

func runAdd(
	cmd *cobra.Command,
	opts Options,
	arg string,
	flags *buildFlags,
	fromSource bool,
) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	sources, err := source.Read(e.configPath())
	if err != nil {
		return err
	}

	// oku.toml gets the expanded ref, so a list works on a machine that does not
	// define the alias.
	expanded, err := sources.Expand(arg)
	if err != nil {
		return err
	}

	r, err := ref.Parse(expanded)
	if err != nil {
		return err
	}

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	// The manifest names the package, so the lock entry to reuse is found by ref.
	var previous lock.Package

	for _, pkg := range locked.Packages {
		if pkg.Ref == r.String() {
			previous = pkg
		}
	}

	got, err := e.install(cmd.Context(), opts, request{
		ref:        r,
		previous:   previous,
		fromSource: fromSource,
		approve:    e.approver(cmd, opts, flags),
		log:        buildLog(cmd, flags),
	})
	if err != nil {
		return err
	}

	locked.Set(got.lock)

	lockData, err := locked.Bytes()
	if err != nil {
		return err
	}

	prof := e.profile()
	if err := prof.Add(got.profile, lockData); err != nil {
		return err
	}

	if err := e.syncExposed(cmd.ErrOrStderr()); err != nil {
		return err
	}

	err = list.Set(e.listPath(), got.lock.Name, list.Entry{Ref: r.String(), Version: r.Version})
	if err != nil {
		return err
	}

	if err := locked.Write(e.lockPath()); err != nil {
		return err
	}

	reportInferred(cmd.OutOrStdout(), got)
	e.reportFirstUse(cmd.ErrOrStderr(), got)
	reportUnsandboxed(cmd.ErrOrStderr(), got)
	fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", got.lock.Name, got.lock.Version)

	switch {
	case slices.Contains(filepath.SplitList(os.Getenv("PATH")), prof.BinDir()):
	case e.project != "":
		fmt.Fprintf(cmd.ErrOrStderr(), "this project's programs are in %s\n", prof.BinDir())
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "add %s to PATH to run it\n", prof.BinDir())
	}

	return nil
}
