package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
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
		whens      []string
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
			when, err := parseWhenFlags(whens)
			if err != nil {
				return err
			}

			if len(args) > 1 && (asset != "" || len(bins) > 0) {
				return errors.New("--asset and --bin describe one download, so add that ref on its own")
			}

			if plan && printed {
				return errors.New("--plan and --manifest both print instead of adding, so pick one")
			}

			if plan || printed {
				return runPlan(cmd, opts, args, planFlags{
					manifest: printed, fromSource: fromSource, asset: asset, bins: bins,
					verbose: flags.verbose, acceptKey: flags.acceptKey, when: when,
				})
			}

			programs := 0

			for _, arg := range args {
				ran, err := runAdd(cmd, opts, arg, &flags, fromSource, enable, system, asset, bins, when)
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
	cmd.Flags().StringArrayVar(&whens, "when", nil,
		"the platforms the package is for, as os=darwin,arch=arm64, once per alternative")

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
// into the request that add installs with asset. The env it returns has the
// runtimes that a registry package runs through.
func addRequest(
	cmd *cobra.Command, opts Options, arg string, when platform.When, asset string,
) (env, request, *lock.Lock, error) {
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

	previous := previousOf(locked, r, asset)

	own, err := list.Read(e.listPath())
	if err != nil {
		return e, request{}, nil, err
	}

	platforms, strict := e.lockPlatforms(own, when)

	age, err := releaseAge(cmd, own, own.Packages[previous.Name])
	if err != nil {
		return e, request{}, nil, err
	}

	// A when that leaves out this machine pins the package for the lock
	// platforms it matches, as sync does.
	lockOnly := !when.Matches(platform.Host())
	if lockOnly && len(platforms) == 0 {
		return e, request{}, nil, fmt.Errorf(
			"--when leaves out this machine, %s, and matches no platform of [lock], so there is nothing to pin",
			platform.Host(),
		)
	}

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
		name:            previous.Name,
		asset:           asset,
		platforms:       platforms,
		strictPlatforms: strict,
		fit:             fitNarrow,
		when:            when,
		lockOnly:        lockOnly,
		releaseAge:      age,
	}, locked, nil
}

// previousOf returns the lock entry that add of r with asset replaces. The
// manifest names the package, so add finds the entry by ref. One release may
// hold several programs, each a package with its own asset. With asset, the
// entry is the one of that asset. Without one, it is the package named after
// the repo, or the one added without an asset.
func previousOf(locked *lock.Lock, r ref.Ref, asset string) lock.Package {
	repo := strings.ToLower(path.Base(r.Location))

	for _, pkg := range locked.Packages {
		if pkg.Ref == r.String() && (asset != "" && pkg.Asset == asset ||
			asset == "" && (pkg.Name == repo || pkg.Asset == "")) {
			return pkg
		}
	}

	return lock.Package{}
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
	when platform.When,
) (bool, error) {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return false, err
	}

	if err := e.recoverPending(cmd, opts); err != nil {
		return false, err
	}

	e, req, locked, err := addRequest(cmd, opts, arg, when, asset)
	if err != nil {
		return false, err
	}

	r := req.ref
	req.fromSource, req.bins = fromSource, bins
	req.service, req.acceptKey, req.system = enable, flags.acceptKey, system
	req.verbose, req.approve, req.log = flags.verbose, e.approver(cmd, opts, flags), buildLog(cmd, flags)
	req.checkAge = e.ageChecker(cmd, opts, flags)

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

	// install narrows a when to the platforms the manifest has something for.
	entryWhen := got.when
	if entryWhen == nil {
		entryWhen = when
	}

	c.commit = func() error {
		// No flag of add writes the entry's minimum release age, so it stays.
		own, err := list.Read(e.listPath())
		if err != nil {
			return err
		}

		err = list.Set(
			e.listPath(),
			got.lock.Name,
			list.Entry{
				Ref:           ref.InDir(filepath.Dir(e.listPath()), r.String()),
				Version:       r.Version,
				Service:       enable,
				System:        system,
				When:          entryWhen,
				Asset:         got.lock.Asset,
				Bins:          got.lock.Bins,
				MinReleaseAge: own.Packages[got.lock.Name].MinReleaseAge,
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
	reportAge(cmd.ErrOrStderr(), got)
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

// parseWhenFlags reads the values of --when, each os=,arch= and libc= pairs
// joined by commas, as the tables of a when. It fails on a when that matches
// no platform oku runs on.
func parseWhenFlags(values []string) (platform.When, error) {
	if len(values) == 0 {
		return nil, nil
	}

	tables := make([]any, 0, len(values))

	for _, value := range values {
		table := map[string]any{}

		for _, pair := range strings.Split(value, ",") {
			key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || val == "" {
				return nil, fmt.Errorf("--when %s: want key=value pairs, such as os=darwin,arch=arm64", value)
			}

			table[key] = val
		}

		tables = append(tables, table)
	}

	when, err := platform.ParseWhen(tables)
	if err != nil {
		return nil, fmt.Errorf("--when: %w", err)
	}

	if len(when.Of()) == 0 {
		return nil, fmt.Errorf("--when %s matches no platform oku runs on", strings.Join(values, " "))
	}

	return when, nil
}
