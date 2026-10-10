package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

const (
	dryRunFlag  = "dry-run"
	dryRunUsage = "check everything and print what would change, and change only the store and the cache"
	lockedFlag  = "locked"
	rebuildFlag = "rebuild"
)

const lockedHint = "run `oku sync` without --locked, and commit oku.lock"

// exitCodeFlag makes a check exit with exitChanges when there is something to
// do, for a script or CI.
const (
	exitCodeFlag  = "exit-code"
	exitCodeUsage = "with --dry-run, exit with 2 when something would change"
	exitChanges   = 2
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
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := recoverFirst(cmd, opts); err != nil {
				return err
			}

			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			var a *adoption

			if len(args) == 1 {
				if e.project != "" {
					return fmt.Errorf(
						"`oku sync <list-ref>` sets up the global list, and this is the project %s\n"+
							"add the ref to the project's include array, or pass --global",
						e.project,
					)
				}

				// adopt writes the list and the lock, so a sync that fails before
				// its change commits has to take them away again.
				before, err := e.readSavedLists()
				if err != nil {
					return err
				}

				if err := adopt(cmd, opts, e, args[0]); err != nil {
					return err
				}

				a = &adoption{before: before}
			} else if a, err = e.mergeConflict(cmd); err != nil {
				return err
			}

			err = reconcile(cmd, opts, &flags, nil, false, a)

			// A dry run adopts nothing and merges nothing either.
			if dryRun, _ := cmd.Flags().GetBool(dryRunFlag); a != nil && (dryRun || err != nil && !a.committed) {
				_, restoreErr := e.restoreLists(pending{Before: a.before, Committing: true})

				return errors.Join(err, restoreErr)
			}

			return err
		},
	}

	flags.register(cmd)
	cmd.Flags().Bool(systemFlag, false, systemUsage)
	cmd.Flags().Bool(dryRunFlag, false, dryRunUsage)
	cmd.Flags().Bool(diffFlag, false, diffUsage)
	cmd.Flags().Bool(exitCodeFlag, false, exitCodeUsage)
	cmd.Flags().Bool(lockedFlag, false, "fail when oku.lock would change, for use in CI")
	cmd.Flags().StringSlice(
		rebuildFlag, nil, "build these packages again, even though the store holds their builds",
	)
	_ = cmd.RegisterFlagCompletionFunc(rebuildFlag, completePackages(opts))

	return cmd
}

func newUpdateCmd(opts Options) *cobra.Command {
	var flags buildFlags

	cmd := &cobra.Command{
		Use:               "update [name...]",
		Aliases:           []string{"upgrade"},
		ValidArgsFunction: completePackages(opts),
		Short:             "Re-resolve packages from their refs, install them and rewrite oku.lock",
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
	cmd.Flags().Bool(diffFlag, false, diffUsage)
	cmd.Flags().Bool(exitCodeFlag, false, exitCodeUsage)

	return cmd
}

// adoption holds the list and the lock as they were before "oku sync <list-ref>"
// wrote them, or before sync merged the conflicts in oku.lock, and whether the
// change that followed committed.
type adoption struct {
	before    savedLists
	committed bool
}

// unfitError says that sync left out a package on the platforms it has no
// artifact or build for, and gives the line of oku.toml that says so.
func (e env) unfitError(l listed, name string, got installed) error {
	where := "sync left it out there"
	if slices.Contains(got.unsupported, platform.Host()) {
		where = "sync did not install it"
	}

	if len(got.when) == 0 {
		return fmt.Errorf(
			"%s has no artifact or build for %s, so %s\n"+
				"it has none for any platform its when matches, so remove it from the list",
			name, platformNames(got.unsupported), where,
		)
	}

	entry := l.entry
	entry.When = got.when

	if l.from != "" {
		return fmt.Errorf(
			"%s has no artifact or build for %s, so %s\n"+
				"the list %s declares it, and its entry there needs when = %s",
			name, platformNames(got.unsupported), where, l.from, got.when.TOML(),
		)
	}

	return fmt.Errorf(
		"%s has no artifact or build for %s, so %s\nchange its line in %s to\n  %s",
		name, platformNames(got.unsupported), where, e.listPath(), list.Line(name, entry),
	)
}

// reportGained names the host and [lock] platforms that the package's when
// leaves out and that its new version has an artifact or a build for. oku never
// widens a when, because the user may have narrowed it on purpose.
func reportGained(
	w io.Writer,
	own *list.List,
	name string,
	when platform.When,
	got installed,
) {
	var gained []platform.Platform

	for _, p := range append([]platform.Platform{platform.Host()}, own.LockPlatforms...) {
		if !when.Matches(p) && slices.Contains(got.support, p) && !slices.Contains(gained, p) {
			gained = append(gained, p)
		}
	}

	if len(gained) > 0 {
		warn(
			w, "%s %s has an artifact or a build for %s, which its when leaves out",
			name, got.lock.VersionOn(platform.Host().String()), platformNames(gained),
		)
	}
}

// elapsed rounds a duration for the closing line: to the second, or under a
// second to 10ms, so that a quick sync does not read "0s".
func elapsed(d time.Duration) time.Duration {
	if d < time.Second {
		return max(d.Round(10*time.Millisecond), 10*time.Millisecond)
	}

	return d.Round(time.Second)
}

// needsLock reports whether sync must pin a package that it does not install.
// previous is its lock entry, and platforms are the ones to pin it for.
// With fromSource, each of them needs a build pin.
func needsLock(
	previous lock.Package,
	ref string,
	platforms []platform.Platform,
	strict, fresh, fromSource bool,
) bool {
	if len(platforms) == 0 {
		return false
	}

	if fresh || previous.Ref != ref {
		return true
	}

	if fromSource && slices.ContainsFunc(platforms, func(p platform.Platform) bool {
		return previous.Platforms[p.String()].Strategy != strategyBuild
	}) {
		return true
	}

	return strict && slices.ContainsFunc(platforms, func(p platform.Platform) bool {
		return previous.Platforms[p.String()] == (lock.Platform{})
	})
}

// parallelEnv names the variable that sets how many packages install, or look
// up their versions, at once.
const parallelEnv = "OKU_PARALLEL"

// parallel returns how many packages install, or look up their versions, at
// once. Most of that time goes to waiting for a server, and builds run one at a
// time, so the default does not follow the number of cores.
func parallel() (int, error) {
	value := os.Getenv(parallelEnv)
	if value == "" {
		return 16, nil
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

// drift says what `oku update` would accept when the package no longer
// matches oku.lock, and returns the job's error. It returns a nil error for
// any other result.
func (j *job) drift() (accepts string, err error) {
	switch {
	case errors.Is(j.err, errManifestChanged):
		return "it", fmt.Errorf("%s: %w", j.name, j.err)
	case errors.Is(j.err, store.ErrVendorChanged):
		return "what it downloads now", j.err
	case errors.Is(j.err, store.ErrPinConflict) && !j.fresh:
		return "the new checksum", j.err
	}

	return "", nil
}

// driftError reports every package that drifted from oku.lock, with one
// `oku update` that accepts them all. The command also names the packages the
// user asked to update. An update that fails writes nothing, so an update of
// the drifted names alone would find the first ones drifted again.
func driftError(jobs []*job, updating []string) error {
	var (
		errs    []error
		drifted []string
		accepts string
	)

	for _, j := range jobs {
		if what, err := j.drift(); err != nil {
			errs = append(errs, err)
			drifted = append(drifted, j.name)
			accepts = what
		}
	}

	if len(errs) == 0 {
		return nil
	}

	update := slices.Compact(slices.Sorted(slices.Values(slices.Concat(updating, drifted))))

	if len(errs) > 1 || len(update) > 1 {
		accepts = "these changes"
	}

	return fmt.Errorf(
		"%w\nrun `oku update %s` to accept %s",
		errors.Join(errs...), strings.Join(update, " "), accepts,
	)
}

// row says what sync did with the package: the kind of change, the version
// and a note. The kind is "+" for a fresh resolve, "^" for a version change,
// "~" for a rebuild, "·" for a pin on another platform, and "" when nothing
// changed.
func (j *job) row(s ui.Style, host platform.Platform) (kind, version, note string) {
	previous, got := j.req.previous, j.got
	version = got.lock.VersionOn(host.String())

	switch {
	case j.req.rebuild:
		return "~", version, "built again"
	case j.got.lockOnly && got.lock.Name == "":
		// A package that fits no platform has nothing pinned to show.
		return "", "", ""
	case j.got.lockOnly:
		return "·", version, "pinned and not installed on " + host.String()
	case !j.locksManifest:
		return "+", version, ""
	case previous.VersionOn(host.String()) != version:
		return "^", previous.VersionOn(host.String()) + " " + s.Arrow() + " " + version, ""
	case previous.ManifestSHA256 != got.lock.ManifestSHA256:
		return "~", version, "manifest changed"
	case previous.Platforms[host.String()] != (lock.Platform{}) &&
		previous.Platforms[host.String()].SHA256 != got.lock.Platforms[host.String()].SHA256:
		return "~", version, "checksum changed"
	}

	// A platform with a version of its own may move while the host stays.
	for _, key := range slices.Sorted(maps.Keys(got.lock.Platforms)) {
		was, now := previous.Platforms[key].Version, got.lock.Platforms[key].Version
		if was != "" && now != was {
			return "·", version, key + " " + was + " " + s.Arrow() + " " + now
		}
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

// liveRow is the line a terminal gets when a package is ready, before the
// change commits. Its mark says what the change does to the package, and only
// the closing line has a check. The marks are a green plus for a new package,
// a cyan arrow for a new version, a cyan tilde for the same version changed, a
// dim dot for one pinned for another platform, a red minus for one that left,
// and a yellow tilde for one a dry run would change. The name is padded to the
// longest one, so the rows align.
func liveRow(s ui.Style, nameWidth int, kind, name, version, note string) string {
	var glyph string

	switch kind {
	case "+":
		glyph = s.Good("+")
	case "^":
		glyph = s.Accent(s.Pick("↑", "^"))
	case "~":
		glyph = s.Accent("~")
	case "·":
		glyph = s.Dim("·")
	case "-":
		glyph = s.Bad("-")
	case "?":
		glyph = s.Warn("~")
	}

	line := glyph + " " + s.Bold(name) + strings.Repeat(" ", max(nameWidth-len(name), 0)) + "  " + version
	if note != "" {
		line += "  " + s.Dim(note)
	}

	return line
}
