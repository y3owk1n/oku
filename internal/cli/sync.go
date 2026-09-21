package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/store"
)

const (
	dryRunFlag  = "dry-run"
	dryRunUsage = "check everything and print what would change, without changing the machine"
)

func newSyncCmd(opts Options) *cobra.Command {
	var flags buildFlags

	cmd := &cobra.Command{
		Use:   "sync [list-ref]",
		Short: "Make the profile match oku.toml at the versions in oku.lock",
		Long: `Make the profile match oku.toml at the versions in oku.lock.

With a list ref, such as github:you/machines, sync first sets this machine up
from that list and the lock beside it. That needs a machine with no global
oku.toml yet.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := recoverFirst(cmd, opts); err != nil {
				return err
			}

			if len(args) == 1 {
				e, err := scopedEnv(cmd, opts)
				if err != nil {
					return err
				}

				if e.project != "" {
					return fmt.Errorf(
						"`oku sync <list-ref>` sets up the global list, and this is the project %s\n"+
							"add the ref to the project's include array, or pass --global",
						e.project,
					)
				}

				// adopt writes the list and the lock, so a sync that then fails has
				// to take them away again.
				before, err := e.readSavedLists()
				if err != nil {
					return err
				}

				if err := adopt(cmd, opts, e, args[0]); err != nil {
					return err
				}

				err = reconcile(cmd, opts, &flags, nil, false, &before)

				// A dry run adopts nothing either.
				if dryRun, _ := cmd.Flags().GetBool(dryRunFlag); err != nil || dryRun {
					return errors.Join(err, e.restoreSavedLists(before))
				}

				return nil
			}

			return reconcile(cmd, opts, &flags, nil, false, nil)
		},
	}

	flags.register(cmd)
	cmd.Flags().Bool(systemFlag, false, systemUsage)
	cmd.Flags().Bool(dryRunFlag, false, dryRunUsage)

	return cmd
}

func newUpdateCmd(opts Options) *cobra.Command {
	var flags buildFlags

	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Re-resolve packages from their refs and rewrite oku.lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := recoverFirst(cmd, opts); err != nil {
				return err
			}

			return reconcile(cmd, opts, &flags, args, true, nil)
		},
	}

	flags.register(cmd)
	cmd.Flags().Bool(systemFlag, false, systemUsage)
	cmd.Flags().Bool(dryRunFlag, false, dryRunUsage)

	return cmd
}

// reconcile installs every package of oku.toml, activates a generation holding
// exactly those, and rewrites oku.lock to match. before is the list and the lock
// to restore when the change fails, or nil for the ones on disk.
//
// Without update, a locked package is read at its locked commit and must still
// have its locked manifest hash. With update, the packages in names, or all of
// them when names is empty, are read fresh and their lock entries replaced.
func reconcile(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
	names []string,
	update bool,
	before *savedLists,
) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	// Updating everything also refreshes includes. Updating named packages keeps
	// them pinned, so the package set stays the same.
	all, err := e.loadList(cmd.Context(), opts, locked, update && len(names) == 0)
	if err != nil {
		return err
	}

	wanted, includes := all.packages, all.includes

	for _, name := range names {
		if _, ok := wanted[name]; !ok {
			return fmt.Errorf("%s is not in %s or its includes", name, e.listPath())
		}
	}

	out := cmd.OutOrStdout()
	host := platform.Host()
	next := &lock.Lock{Includes: includes}

	limit, err := parallel()
	if err != nil {
		return err
	}

	// job is one package to install, and what install gave for it.
	type job struct {
		name          string
		req           request
		fresh         bool
		locksManifest bool
		got           installed
		err           error
	}

	var (
		jobs []*job
		deps = newDepCache()
	)

	for _, name := range slices.Sorted(maps.Keys(wanted)) {
		r := wanted[name].ref
		previous, _ := locked.Find(name)

		// A package for another platform keeps its lock entry and is not installed.
		if !wanted[name].entry.When.Matches(host) {
			if previous.Name != "" {
				next.Set(previous)
			}

			continue
		}

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

		jobs = append(jobs, &job{
			name: name, fresh: fresh, locksManifest: locksManifest,
			req: request{
				ref:          r,
				commit:       commit,
				previous:     previous,
				wantManifest: wantManifest,
				acceptDigest: fresh,
				acceptKey:    flags.acceptKey,
				keepVersion:  !fresh && previous.Ref == r.String(),
				service:      wanted[name].entry.Service,
				system:       wanted[name].entry.System,
				approve:      e.approver(cmd, opts, flags),
				log:          buildLog(cmd, flags),
				root:         name,
				deps:         deps,
			},
		})
	}

	// The first failure stops the packages that are still installing.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	var (
		wg     sync.WaitGroup
		failed atomic.Pointer[job]
		slots  = make(chan struct{}, limit)
	)

	for _, j := range jobs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			if ctx.Err() != nil {
				return
			}

			j.got, j.err = e.install(ctx, opts, j.req)
			if j.err != nil && failed.CompareAndSwap(nil, j) {
				cancel()
			}
		}()
	}

	wg.Wait()

	// An interrupted run leaves packages that never started.
	if err := cmd.Context().Err(); err != nil {
		return err
	}

	// The packages that stopped because of the first failure have nothing to say.
	if j := failed.Load(); j != nil {
		jobs = []*job{j}
	}

	var pkgs []profile.Package

	for _, j := range jobs {
		name, r, previous := j.name, j.req.ref, j.req.previous
		fresh, locksManifest := j.fresh, j.locksManifest
		got, err := j.got, j.err

		switch {
		case errors.Is(err, errManifestChanged):
			return fmt.Errorf("%s: %w\nrun `oku update %s` to accept it", name, err, name)
		case errors.Is(err, store.ErrVendorChanged):
			return fmt.Errorf("%w\nrun `oku update %s` to accept what it downloads now", err, name)
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
		case previous.Platforms[host.String()] != (lock.Platform{}) &&
			previous.Platforms[host.String()].SHA256 != got.lock.Platforms[host.String()].SHA256:
			fmt.Fprintf(out, "%s %s, checksum changed\n", name, got.lock.Version)
		}

		reportInferred(out, got, flags.verbose)
		e.reportFirstUse(cmd.ErrOrStderr(), got)
		reportUnsandboxed(cmd.ErrOrStderr(), got)
		reportCache(cmd.ErrOrStderr(), got)

		next.Set(got.lock)
		pkgs = append(pkgs, got.profile)
	}

	lockData, err := next.Bytes(e.lockPath())
	if err != nil {
		return err
	}

	files, err := e.resolveFiles(all.files, all.vars, all.secrets, pkgs)
	if err != nil {
		return err
	}

	wantedSettings, err := resolveSettings(opts, all.settings, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	staged, err := e.profile().Replace(pkgs, files, wantedSettings, lockData)
	if err != nil {
		return err
	}

	system, _ := cmd.Flags().GetBool(systemFlag)
	dryRun, _ := cmd.Flags().GetBool(dryRunFlag)

	c := change{
		to: staged, staged: true, system: system, before: before, dryRun: dryRun,
		commit: func() error { return next.Write(e.lockPath()) },
	}
	if staged == 0 {
		c.to, c.staged = e.profile().Current(), false
	}

	if err := e.apply(cmd, opts, c); err != nil || dryRun {
		return err
	}

	if staged != 0 {
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

// parallelEnv names the variable that sets how many packages install at once.
const parallelEnv = "OKU_PARALLEL"

// parallel returns how many packages install at once. Most of an install is
// waiting for a server, so the default does not follow the number of cores.
func parallel() (int, error) {
	value := os.Getenv(parallelEnv)
	if value == "" {
		return 8, nil
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s is %q, and it must be a number from 1 up", parallelEnv, value)
	}

	return n, nil
}

func buildLog(cmd *cobra.Command, flags *buildFlags) io.Writer {
	if flags.verbose {
		return cmd.ErrOrStderr()
	}

	return nil
}
