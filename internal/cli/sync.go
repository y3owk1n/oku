package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

const (
	dryRunFlag  = "dry-run"
	dryRunUsage = "check everything and print what would change, without changing the machine"
	lockedFlag  = "locked"
	rebuildFlag = "rebuild"
)

const lockedHint = "run `oku sync` without --locked, and commit oku.lock"

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
	cmd.Flags().Bool(lockedFlag, false, "fail when oku.lock would change, for use in CI")
	cmd.Flags().StringSlice(
		rebuildFlag, nil, "build these packages again, even though the store holds their builds",
	)

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
	started := time.Now()

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

	rebuild, _ := cmd.Flags().GetStringSlice(rebuildFlag)

	for _, name := range slices.Concat(names, rebuild) {
		if _, ok := wanted[name]; !ok {
			return fmt.Errorf("%s is not in %s or its includes", name, e.listPath())
		}
	}

	// A rebuild replaces a build in the store, which a dry run must not do.
	if dryRun, _ := cmd.Flags().GetBool(dryRunFlag); dryRun && len(rebuild) > 0 {
		return errors.New("--rebuild builds a package again, so it does not go with --dry-run")
	}

	for _, name := range rebuild {
		if !wanted[name].entry.When.Matches(platform.Host()) {
			return fmt.Errorf(
				"%s is not installed on %s, so there is no build of it to replace",
				name, platform.Host(),
			)
		}
	}

	out := cmd.OutOrStdout()
	host := platform.Host()
	next := &lock.Lock{Includes: includes}

	limit, err := parallel()
	if err != nil {
		return err
	}

	var (
		jobs []*job
		deps = newDepCache()
		// unpinned holds the packages that oku.lock does not pin for the host.
		unpinned []string
	)

	frozen, _ := cmd.Flags().GetBool(lockedFlag)

	for _, name := range slices.Sorted(maps.Keys(wanted)) {
		r := wanted[name].ref
		previous, _ := locked.Find(name)
		platforms, strict := e.lockPlatforms(all.own, wanted[name].entry.When)
		fresh := update && (len(names) == 0 || slices.Contains(names, name))

		// oku does not install a package for another platform. It keeps the lock
		// entry, or pins the package again when the entry has to change.
		lockOnly := !wanted[name].entry.When.Matches(host)
		if lockOnly && !needsLock(previous, r.String(), platforms, strict, fresh) {
			if previous.Name != "" {
				next.Set(previous)
			}

			continue
		}

		pinned := previous.Ref == r.String() &&
			previous.Platforms[host.String()] != (lock.Platform{})
		if !lockOnly && !pinned {
			unpinned = append(unpinned, name)
		}

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
				ref:             r,
				platforms:       platforms,
				strictPlatforms: strict,
				lockOnly:        lockOnly,
				rebuild:         slices.Contains(rebuild, name),
				commit:          commit,
				previous:        previous,
				wantManifest:    wantManifest,
				acceptDigest:    fresh,
				acceptKey:       flags.acceptKey,
				keepVersion:     !fresh && previous.Ref == r.String(),
				service:         wanted[name].entry.Service,
				system:          wanted[name].entry.System,
				verbose:         flags.verbose,
				approve:         e.approver(cmd, opts, flags),
				log:             buildLog(cmd, flags),
				root:            name,
				deps:            deps,
			},
		})
	}

	// A locked sync downloads nothing that oku.lock does not pin.
	if frozen && len(unpinned) > 0 {
		return fmt.Errorf(
			"%s does not pin %s for %s\n%s",
			e.lockPath(), strings.Join(unpinned, ", "), host, lockedHint,
		)
	}

	// The first failure stops the packages that are still installing.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	var (
		wg     sync.WaitGroup
		failed atomic.Pointer[job]
		slots  = make(chan struct{}, limit)
		style  = ui.For(out)
		// A terminal gets each package's row as it finishes, above the waits
		// that are still running, with the names aligned. A pipe gets the
		// table at the end, as before.
		live      = style.On()
		liveOut   = status.Writer(ctx, out)
		liveMu    sync.Mutex
		nameWidth = 0
	)

	for _, j := range jobs {
		nameWidth = max(nameWidth, len(j.name))
	}

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

			if j.err != nil || !live {
				return
			}

			if kind, version, note := j.row(style, host); kind != "" {
				liveMu.Lock()
				fmt.Fprintln(liveOut, liveRow(style, nameWidth, kind, j.name, version, note))
				liveMu.Unlock()
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

	have, err := e.profile().Packages()
	if err != nil {
		return err
	}

	var (
		pkgs    []profile.Package
		summary = style.Table("", "package", "version", "")
	)

	for _, j := range jobs {
		name, r := j.name, j.req.ref
		fresh := j.fresh
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

		if kind, version, note := j.row(style, host); kind != "" && !live {
			syncRow(style, summary, kind, name, version, note)
		}

		reportInferred(out, got, flags.verbose)
		e.reportFirstUse(cmd.ErrOrStderr(), got)
		reportUnsandboxed(cmd.ErrOrStderr(), got)
		reportLinks(cmd.ErrOrStderr(), got)
		reportCache(cmd.ErrOrStderr(), got)

		next.Set(got.lock)

		if !j.req.lockOnly {
			pkgs = append(pkgs, got.profile)
		}
	}

	// A dry run says "would remove" further down, so it needs no row here.
	if dryRun, _ := cmd.Flags().GetBool(dryRunFlag); !dryRun {
		for _, pkg := range have {
			if _, ok := wanted[pkg.Name]; ok {
				continue
			}

			if live {
				fmt.Fprintln(out, liveRow(style, nameWidth, "-", pkg.Name, pkg.Version, "removed"))
			} else {
				syncRow(style, summary, "-", pkg.Name, pkg.Version, "removed")
			}
		}
	}

	if err := summary.Write(out); err != nil {
		return err
	}

	lockData, err := next.Bytes(e.lockPath())
	if err != nil {
		return err
	}

	if frozen {
		// A missing lock reads as empty, which differs too.
		onDisk, _ := os.ReadFile(e.lockPath())
		if !bytes.Equal(onDisk, lockData) {
			return fmt.Errorf(
				"%s is out of date, and --locked does not change it\n%s", e.lockPath(), lockedHint,
			)
		}
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

	held := holds(profile.Generation{Packages: pkgs, Files: files, Settings: wantedSettings})
	took := time.Since(started).Round(time.Second)

	switch {
	case staged != 0 && style.On():
		fmt.Fprintln(out, style.Done(fmt.Sprintf(
			"done in %s, profile holds %s, generation %d", took, held, staged,
		)))
	case staged != 0:
		fmt.Fprintf(out, "profile now holds %s, generation %d, %s\n", held, staged, took)
	default:
		fmt.Fprintln(out, style.Done("already in sync"))
	}

	return nil
}

// needsLock reports whether sync must pin a package that it does not install.
// previous is its lock entry, and platforms are the ones to pin it for.
func needsLock(
	previous lock.Package,
	ref string,
	platforms []platform.Platform,
	strict, fresh bool,
) bool {
	if len(platforms) == 0 {
		return false
	}

	if fresh || previous.Ref != ref {
		return true
	}

	return strict && slices.ContainsFunc(platforms, func(p platform.Platform) bool {
		return previous.Platforms[p.String()] == (lock.Platform{})
	})
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

// job is one package to install, and what install gave for it.
type job struct {
	name          string
	req           request
	fresh         bool
	locksManifest bool
	got           installed
	err           error
}

// row says what sync did with the package: the kind of change, the version
// and a note. The kind is "+" for a fresh resolve, "^" for a version change,
// "~" for a rebuild, "·" for a pin on another platform, and "" when nothing
// changed.
func (j *job) row(s ui.Style, host platform.Platform) (kind, version, note string) {
	previous, got := j.req.previous, j.got
	version = got.lock.Version

	switch {
	case j.req.rebuild:
		return "~", version, "built again"
	case j.req.lockOnly:
		return "·", version, "pinned and not installed on " + host.String()
	case !j.locksManifest:
		return "+", version, ""
	case previous.Version != version:
		return "^", previous.Version + " " + s.Arrow() + " " + version, ""
	case previous.ManifestSHA256 != got.lock.ManifestSHA256:
		return "~", version, "manifest changed"
	case previous.Platforms[host.String()] != (lock.Platform{}) &&
		previous.Platforms[host.String()].SHA256 != got.lock.Platforms[host.String()].SHA256:
		return "~", version, "checksum changed"
	}

	return "", "", ""
}

// syncRow adds one changed package to the summary that a pipe gets at the end:
// "name version, note", the one line sync always printed.
func syncRow(_ ui.Style, tab *ui.Table, _, name, version, note string) {
	line := name + " " + version
	if note != "" {
		line += ", " + note
	}

	tab.Row(line)
}

// liveRow is the line a terminal gets when a package finishes, in the style
// of a package manager's install log: a green check for a package that is
// installed, a dim dot for one pinned for another platform, a red minus for
// one that left. The name is padded to the longest one, so the rows align.
func liveRow(s ui.Style, nameWidth int, kind, name, version, note string) string {
	glyph := s.Good(s.Pick("✓", "ok"))

	switch kind {
	case "·":
		glyph = s.Dim("·")
	case "-":
		glyph = s.Bad("-")
	}

	line := glyph + " " + s.Bold(name) + strings.Repeat(" ", max(nameWidth-len(name), 0)) + "  " + version
	if note != "" {
		line += "  " + s.Dim(note)
	}

	return line
}
