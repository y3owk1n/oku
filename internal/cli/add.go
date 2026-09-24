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
		bins       []string
		plan       bool
		printed    bool
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
  alias/name                          a package in a source, see "oku source"

--plan prints what oku found for a ref and what add would do, and changes
nothing. --manifest prints the manifest add would use, ready to save as a file.`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 && (asset != "" || len(bins) > 0) {
				return errors.New("--asset and --bin describe one download, so add that ref on its own")
			}

			if plan && printed {
				return errors.New("--plan and --manifest both print instead of adding, so pick one")
			}

			if plan || printed {
				return runPlan(cmd, opts, args, planFlags{
					manifest: printed, fromSource: fromSource, asset: asset, bins: bins,
					verbose: flags.verbose, acceptKey: flags.acceptKey,
				})
			}

			programs := 0

			for _, arg := range args {
				ran, err := runAdd(cmd, opts, arg, &flags, fromSource, enable, system, asset, bins)
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
		StringArrayVar(&bins, "bin", nil, "with no manifest, the file name of a program in the asset, once per program")
	cmd.Flags().
		BoolVar(&plan, "plan", false, "print what oku found and what add would do, and change nothing")
	cmd.Flags().
		BoolVar(&printed, "manifest", false, "print the manifest oku would use, inferred for every platform when the ref has none")

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

// addRequest reads the ref of arg and what oku.toml and oku.lock say about it
// into the request that add installs. The env it returns has the runtimes that
// a registry package runs through.
func addRequest(cmd *cobra.Command, opts Options, arg string) (env, request, *lock.Lock, error) {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return e, request{}, nil, err
	}

	r, err := e.parseRef(arg)
	if err != nil {
		return e, request{}, nil, err
	}

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return e, request{}, nil, err
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
		return e, request{}, nil, err
	}

	platforms, strict := e.lockPlatforms(own, nil)

	// A package of a registry runs through, or builds with, the runtime that
	// the list names.
	if e.inferrerOf(r.Kind) != nil {
		all, err := e.mergedList(cmd, opts)
		if err != nil {
			return e, request{}, nil, err
		}

		e.runtimes = all.runtimes
	}

	return e, request{
		ref:             r,
		previous:        previous,
		platforms:       platforms,
		strictPlatforms: strict,
		fit:             fitNarrow,
	}, locked, nil
}

// notFound turns a missing file into a hint when arg looks like a bare name.
func notFound(arg string, err error) error {
	if errors.Is(err, ref.ErrNotFound) && !strings.ContainsAny(arg, ":/\\") {
		return fmt.Errorf(
			"there is no file named %s here\n"+
				"a package from a source is written alias/name, and `oku add --help` lists every ref form",
			arg,
		)
	}

	return err
}

func runAdd(
	cmd *cobra.Command,
	opts Options,
	arg string,
	flags *buildFlags,
	fromSource, enable, system bool,
	asset string,
	bins []string,
) (bool, error) {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return false, err
	}

	if err := e.recoverPending(cmd, opts); err != nil {
		return false, err
	}

	e, req, locked, err := addRequest(cmd, opts, arg)
	if err != nil {
		return false, err
	}

	r := req.ref
	req.fromSource, req.asset, req.bins = fromSource, asset, bins
	req.service, req.acceptKey, req.system = enable, flags.acceptKey, system
	req.verbose, req.approve, req.log = flags.verbose, e.approver(cmd, opts, flags), buildLog(cmd, flags)

	got, err := e.install(cmd.Context(), opts, req)
	if err != nil {
		return false, notFound(arg, err)
	}

	locked.Set(got.lock)

	lockData, err := locked.Bytes(e.lockPath())
	if err != nil {
		return false, err
	}

	prof := e.profile()

	// oku only pins a package that has nothing for this machine, so the current
	// generation stays.
	c := change{to: prof.Current(), system: system}
	if !got.lockOnly {
		if c.to, err = prof.Add(got.profile, lockData); err != nil {
			return false, err
		}

		c.staged = true
	}

	c.commit = func() error {
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
	}

	if err := e.apply(cmd, opts, c); err != nil {
		return false, err
	}

	reportInferred(cmd.OutOrStdout(), got, flags.verbose)
	reportNarrowed(cmd.ErrOrStderr(), got, e.listPath())

	e.reportFirstUse(cmd.ErrOrStderr(), got)
	reportUnsandboxed(cmd.ErrOrStderr(), got)
	reportLinks(cmd.ErrOrStderr(), got)
	reportCache(cmd.ErrOrStderr(), got)
	s := ui.For(cmd.OutOrStdout())

	if got.lockOnly {
		fmt.Fprintln(cmd.OutOrStdout(), s.Done(
			"added "+s.Bold(got.lock.Name)+" "+got.lock.Version+
				", pinned and not installed on "+platform.Host().String(),
		))

		return false, nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), s.Done(
		"added "+s.Bold(got.lock.Name)+" "+got.lock.VersionOn(platform.Host().String()),
	))

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
