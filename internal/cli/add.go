package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/ui"
)

func newAddCmd(opts Options) *cobra.Command {
	var (
		flags      buildFlags
		fromSource bool
		enable     bool
		system     bool
		asset      string
		bin        string
	)

	cmd := &cobra.Command{
		Use:   "add <ref>[@version]...",
		Short: "Install packages from their manifests",
		Long: `Install packages from their manifests, one generation each, in the order
given. A failure stops the command, and the packages before it stay. A ref is
one of:

  ./pkg.toml                          a local file
  https://host/pkg.toml               a URL of a manifest
  https://host/tool-1.2.3.tar.gz      a URL of the download itself
  github:owner/repo                   oku.pkg.toml in a GitHub repo
  github:owner/repo#name              name.toml or packages/name.toml in it
  codeberg:owner/repo                 the same on codeberg.org
  gitea:host/owner/repo               the same on a Gitea or Forgejo server
  gitlab:group/project                the same on gitlab.com
  npm:@scope/name                     a command-line tool in the npm registry
  git+https://host/repo#path/pkg.toml a file in any git repo
  alias/name                          a package in a source, see "oku source"`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 && (asset != "" || bin != "") {
				return errors.New("--asset and --bin describe one download, so add that ref on its own")
			}

			programs := 0

			for _, arg := range args {
				ran, err := runAdd(cmd, opts, arg, &flags, fromSource, enable, system, asset, bin)
				if err != nil {
					return err
				}

				if ran {
					programs++
				}
			}

			if programs == 0 {
				// An app, a font or a file has nothing to run, so PATH does not matter.
				return nil
			}

			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			reportPath(cmd, opts, e, programs)

			return nil
		},
	}

	flags.register(cmd)
	cmd.Flags().
		BoolVar(&fromSource, "from-source", false, "build from source even when a prebuilt download fits")
	cmd.Flags().
		BoolVar(&enable, "service", false, "run the package's services now and at every login")
	cmd.Flags().BoolVar(&system, systemFlag, false, systemUsage)
	cmd.Flags().
		StringVar(&asset, "asset", "", "with no manifest, the release asset for this machine, as a glob")
	cmd.Flags().
		StringVar(&bin, "bin", "", "with no manifest, the file name of the program in the asset")

	return cmd
}

// parseRef reads a ref from the command line and expands a source alias in it.
// oku.toml gets the expanded ref, so a list works on a machine that does not
// define the alias.
func (e env) parseRef(arg string) (ref.Ref, error) {
	sources, err := source.Read(e.configPath())
	if err != nil {
		return ref.Ref{}, err
	}

	expanded, err := sources.Expand(arg)
	if err != nil {
		return ref.Ref{}, err
	}

	return ref.Parse(expanded)
}

func runAdd(
	cmd *cobra.Command,
	opts Options,
	arg string,
	flags *buildFlags,
	fromSource, enable, system bool,
	asset, bin string,
) (bool, error) {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return false, err
	}

	if err := e.recoverPending(cmd, opts); err != nil {
		return false, err
	}

	r, err := e.parseRef(arg)
	if err != nil {
		return false, err
	}

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return false, err
	}

	// The manifest names the package, so the lock entry to reuse is found by ref.
	var previous lock.Package

	for _, pkg := range locked.Packages {
		if pkg.Ref == r.String() {
			previous = pkg
		}
	}

	own, err := list.Read(e.listPath())
	if err != nil {
		return false, err
	}

	platforms, strict := e.lockPlatforms(own, platform.Selector{})

	// A package of a registry runs through, or builds with, the runtime that
	// the list names.
	if e.inferrerOf(r.Kind) != nil {
		all, err := e.mergedList(cmd, opts)
		if err != nil {
			return false, err
		}

		e.runtimes = all.runtimes
	}

	got, err := e.install(cmd.Context(), opts, request{
		ref:             r,
		previous:        previous,
		platforms:       platforms,
		strictPlatforms: strict,
		fromSource:      fromSource,
		asset:           asset,
		bin:             bin,
		service:         enable,
		acceptKey:       flags.acceptKey,
		system:          system,
		verbose:         flags.verbose,
		approve:         e.approver(cmd, opts, flags),
		log:             buildLog(cmd, flags),
	})
	if errors.Is(err, ref.ErrNotFound) && !strings.ContainsAny(arg, ":/\\") {
		return false, fmt.Errorf(
			"there is no file named %s here\n"+
				"a package from a source is written alias/name, and `oku add --help` lists every ref form",
			arg,
		)
	}

	if err != nil {
		return false, err
	}

	locked.Set(got.lock)

	lockData, err := locked.Bytes(e.lockPath())
	if err != nil {
		return false, err
	}

	prof := e.profile()

	staged, err := prof.Add(got.profile, lockData)
	if err != nil {
		return false, err
	}

	err = e.apply(cmd, opts, change{
		to: staged, staged: true, system: system,
		commit: func() error {
			err := list.Set(
				e.listPath(),
				got.lock.Name,
				list.Entry{
					Ref:     ref.InDir(filepath.Dir(e.listPath()), r.String()),
					Version: r.Version,
					Service: enable,
					System:  system,
					When:    got.when,
				},
			)
			if err != nil {
				return err
			}

			return locked.Write(e.lockPath())
		},
	})
	if err != nil {
		return false, err
	}

	reportInferred(cmd.OutOrStdout(), got, flags.verbose)

	if got.when.OS != "" {
		warn(
			cmd.ErrOrStderr(),
			"%s has a release for %s only, so its entry in %s says when = %s",
			got.lock.Name, got.when.OS, e.listPath(), got.when.TOML(),
		)
	}

	e.reportFirstUse(cmd.ErrOrStderr(), got)
	reportUnsandboxed(cmd.ErrOrStderr(), got)
	reportLinks(cmd.ErrOrStderr(), got)
	reportCache(cmd.ErrOrStderr(), got)
	s := ui.For(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), s.Done("added "+s.Bold(got.lock.Name)+" "+got.lock.Version))

	programs, _ := filepath.Glob(filepath.Join(got.profile.StorePath, "bin", "*"))

	return len(programs) > 0, nil
}

// reportPath says how to run the programs of the packages oku added, when
// PATH does not have the profile yet. packages counts the packages that have
// programs, so that the hint says "it" or "them".
func reportPath(cmd *cobra.Command, opts Options, e env, packages int) {
	prof := e.profile()

	them := "it"
	if packages > 1 {
		them = "them"
	}

	switch {
	case slices.Contains(filepath.SplitList(os.Getenv("PATH")), prof.BinDir()):
	case e.project != "":
		fmt.Fprintf(cmd.ErrOrStderr(), "this project's programs are in %s\n", prof.BinDir())
	default:
		// One hook line puts oku and its programs on PATH, see "oku hook --help".
		if hint := setupHint(opts); hint != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "to run %s, %s\n", them, hint)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "add %s to PATH to run %s\n", prof.BinDir(), them)
		}
	}
}
