package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/ui"
)

// syncRun is one run of reconcile: the command and its flags, the list and the
// lock it read, and the lock it writes.
type syncRun struct {
	e     env
	cmd   *cobra.Command
	opts  Options
	flags *buildFlags
	// names are the packages that update takes, or all of them when it is
	// empty. rebuild are the ones --rebuild builds again.
	names, rebuild []string
	update         bool
	dryRun, frozen bool

	all    merged
	locked *lock.Lock
	next   *lock.Lock

	style ui.Style
	// live gives a terminal each package's row as it finishes, with the names
	// nameWidth wide. A pipe gets the table at the end.
	live      bool
	nameWidth int
	// rowsShown records that a terminal got a row for a finished package.
	rowsShown bool
}

// reconcile installs every package of oku.toml, activates a generation holding
// exactly those, and rewrites oku.lock to match. adopted holds the list and the
// lock to restore when the change fails, or is nil for the ones on disk.
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
	adopted *adoption,
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

	if err := e.trustProject(cmd, opts, flags.yes); err != nil {
		return err
	}

	// Updating everything also refreshes includes. Updating named packages keeps
	// them pinned, so the package set stays the same.
	var refresh listReview
	if update && len(names) == 0 {
		refresh = e.listReviewer(cmd, opts, flags.yes)
	}

	all, err := e.loadList(cmd.Context(), opts, locked, refresh)
	if err != nil {
		return err
	}

	e.runtimes = all.runtimes

	rebuild, _ := cmd.Flags().GetStringSlice(rebuildFlag)
	dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
	frozen, _ := cmd.Flags().GetBool(lockedFlag)

	r := &syncRun{
		e: e, cmd: cmd, opts: opts, flags: flags, names: names, rebuild: rebuild, update: update,
		dryRun: dryRun, frozen: frozen, all: all, locked: locked,
		next: &lock.Lock{Includes: all.includes}, style: ui.For(cmd.OutOrStdout()),
	}
	r.live = r.style.On()

	if err := r.check(); err != nil {
		return err
	}

	jobs, err := r.plan()
	if err != nil {
		return err
	}

	if jobs, err = r.install(jobs); err != nil {
		return err
	}

	pkgs, narrowed, unfit, err := r.collect(jobs)
	if err != nil {
		return err
	}

	lockData, err := r.next.Bytes(e.lockPath())
	if err != nil {
		return err
	}

	if frozen {
		// A missing lock reads as empty, which differs too. git on Windows checks
		// the lock out with CRLF line endings, which change no pin.
		onDisk, _ := os.ReadFile(e.lockPath())
		if !bytes.Equal(bytes.ReplaceAll(onDisk, []byte("\r\n"), []byte("\n")), lockData) {
			return fmt.Errorf(
				"%s is out of date, and --locked does not change it\n%s", e.lockPath(), lockedHint,
			)
		}
	}

	if err := e.placeTrees(cmd.Context(), opts, all.files, all.secrets); err != nil {
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

	reqs := append(hostHere(all.host), jobsHost(jobs)...)
	out := cmd.OutOrStdout()

	// A list with nothing in it, not even a pin for another platform, writes
	// no first generation.
	if len(jobs) == 0 && len(files) == 0 && len(wantedSettings) == 0 && len(reqs) == 0 &&
		e.profile().Current() == 0 {
		if dryRun && wantJSON(cmd) {
			return printJSON(cmd, []wouldChange{})
		}

		fmt.Fprintln(out, r.style.Done("nothing to sync, "+e.listPath()+" lists no packages, files or settings"))

		return nil
	}

	staged, err := e.profile().Replace(pkgs, files, wantedSettings, reqs, lockData)
	if err != nil {
		return err
	}

	changes, err := r.apply(staged, narrowed, adopted)
	if err != nil {
		return err
	}

	for _, j := range narrowed {
		reportNarrowed(cmd.ErrOrStderr(), j.got, e.listPath())
	}

	if err := tellMissing(cmd.ErrOrStderr(), opts, reqs); err != nil {
		return err
	}

	if dryRun {
		// A failure says more than the exit code, so it wins.
		if exitCode, _ := cmd.Flags().GetBool(exitCodeFlag); exitCode && changes && len(unfit) == 0 {
			return ExitError{Code: exitChanges}
		}

		return errors.Join(unfit...)
	}

	held := holds(profile.Generation{Packages: pkgs, Files: files, Settings: wantedSettings})
	took := elapsed(time.Since(started))

	switch {
	case staged != 0 && r.style.On():
		fmt.Fprintln(out, r.style.Done(fmt.Sprintf(
			"done in %s, profile holds %s, generation %d", took, held, staged,
		)))
	case staged != 0:
		fmt.Fprintf(out, "profile now holds %s, generation %d, %s\n", held, staged, took)
	case len(unfit) == 0:
		fmt.Fprintln(out, r.style.Done("already in sync"))
	}

	return errors.Join(unfit...)
}

// check refuses the names and flags that do not fit the list.
func (r *syncRun) check() error {
	wanted := r.all.packages

	for _, name := range slices.Concat(r.names, r.rebuild) {
		if _, ok := wanted[name]; !ok {
			return fmt.Errorf("%s is not in %s or its includes", name, r.e.listPath())
		}
	}

	if exitCode, _ := r.cmd.Flags().GetBool(exitCodeFlag); exitCode && !r.dryRun {
		return fmt.Errorf("--%s goes with --%s", exitCodeFlag, dryRunFlag)
	}

	// A rebuild replaces a build in the store, which a dry run must not do.
	if r.dryRun && len(r.rebuild) > 0 {
		return errors.New("--rebuild builds a package again, so it does not go with --dry-run")
	}

	for _, name := range r.rebuild {
		if !wanted[name].entry.When.Here() {
			return fmt.Errorf(
				"%s is not installed on %s, so there is no build of it to replace",
				name, platform.Host(),
			)
		}
	}

	return nil
}

// plan returns a job for each package to install or pin. It puts the lock
// entry of a package for another platform that needs no change in r.next.
func (r *syncRun) plan() ([]*job, error) {
	e, cmd, flags, host := r.e, r.cmd, r.flags, platform.Host()

	var (
		jobs []*job
		deps = newDepCache()
		// unpinned holds the packages that oku.lock does not pin for the host, and
		// outside those it pins at a version the list leaves out.
		unpinned, outside []string
	)

	for _, name := range slices.Sorted(maps.Keys(r.all.packages)) {
		listed := r.all.packages[name]
		ref, entry := listed.ref, listed.entry
		previous, _ := r.locked.Find(name)

		// When the entry names another asset or other programs than the lock,
		// sync infers again, as it does for a changed ref. For an entry that
		// names none, it infers the way the lock recorded.
		if entry.Asset != "" && entry.Asset != previous.Asset ||
			len(entry.Bins) > 0 && !slices.Equal(entry.Bins, previous.Bins) {
			previous.Ref = ""
		}

		platforms, strict := e.lockPlatforms(r.all.own, entry.When)
		fresh := r.update && (len(r.names) == 0 || slices.Contains(r.names, name))

		age, err := releaseAge(cmd, r.all.own, entry)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		// oku does not install a package for another platform. It keeps the lock
		// entry, or pins the package again when the entry has to change.
		lockOnly := !entry.When.Here()
		if lockOnly && !needsLock(previous, ref.String(), platforms, strict, fresh, entry.FromSource) {
			if previous.Name != "" {
				r.next.Set(previous)
			}

			continue
		}

		// A lock entry pins the package only at a version the list allows, since
		// install picks another version from the source otherwise.
		allowed, _ := resolve.Matches(previous.Version, ref.Version)
		pinned := previous.Ref == ref.String() && previous.Platforms[host.String()] != (lock.Platform{})

		switch {
		case lockOnly:
		case !pinned:
			unpinned = append(unpinned, name)
		case !allowed:
			outside = append(outside, fmt.Sprintf("%s at %s, which version %q leaves out", name, previous.Version, ref.Version))
		}

		commit := previous.Commit
		if fresh || previous.Ref != ref.String() {
			commit = listed.commit
		}

		locksManifest := previous.Ref == ref.String() && previous.ManifestSHA256 != ""

		// A list version that leaves out the locked one takes the manifest of
		// the version oku picks, which differs for an inferred package.
		wantManifest := ""
		if locksManifest && !fresh && allowed {
			wantManifest = previous.ManifestSHA256
		}

		// update writes a narrowed when to the user's own list. sync, and update
		// of an included package, report it.
		fit := fitReport
		if fresh && listed.from == "" {
			fit = fitNarrow
		}

		jobs = append(jobs, &job{
			name: name, fresh: fresh, locksManifest: locksManifest,
			req: request{
				ref:             ref,
				name:            name,
				asset:           entry.Asset,
				bins:            entry.Bins,
				fromSource:      entry.FromSource,
				when:            entry.When,
				fit:             fit,
				platforms:       platforms,
				strictPlatforms: strict,
				lockOnly:        lockOnly,
				rebuild:         slices.Contains(r.rebuild, name),
				commit:          commit,
				previous:        previous,
				wantManifest:    wantManifest,
				acceptDigest:    fresh,
				acceptKey:       flags.acceptKey,
				acceptWeaker:    flags.acceptWeaker,
				releaseAge:      age,
				keepVersion:     !fresh && previous.Ref == ref.String(),
				service:         entry.Service,
				system:          entry.System,
				runAs:           entry.RunAs,
				signingKey:      entry.SigningKey,
				signerWorkflow:  entry.SignerWorkflow,
				verbose:         flags.verbose,
				approve:         e.approver(cmd, r.opts, flags),
				checkAge:        e.ageChecker(cmd, r.opts, flags),
				checkTrust:      e.trustChecker(cmd, r.opts, flags),
				log:             buildLog(cmd, flags),
				root:            name,
				deps:            deps,
			},
		})
	}

	// A locked sync downloads nothing that oku.lock does not pin.
	switch {
	case !r.frozen:
	case len(unpinned) > 0:
		return nil, fmt.Errorf(
			"%s does not pin %s for %s\n%s",
			e.lockPath(), strings.Join(unpinned, ", "), host, lockedHint,
		)
	case len(outside) > 0:
		return nil, fmt.Errorf("%s pins %s\n%s", e.lockPath(), strings.Join(outside, ", "), lockedHint)
	}

	return jobs, nil
}

// install runs jobs in parallel. The first failure stops the ones still
// running, and install then returns the failed job alone, since the others
// stopped because of it.
func (r *syncRun) install(jobs []*job) ([]*job, error) {
	limit, err := parallel()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(r.cmd.Context())
	defer cancel()

	var (
		wg      sync.WaitGroup
		failed  atomic.Pointer[job]
		slots   = make(chan struct{}, limit)
		liveOut = status.Writer(ctx, r.cmd.OutOrStdout())
		liveMu  sync.Mutex
		host    = platform.Host()
	)

	for _, j := range jobs {
		r.nameWidth = max(r.nameWidth, len(j.name))
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

			// When a package drifted from oku.lock, the others still finish, so
			// the error names every package to update.
			j.got, j.err = r.e.install(status.Scope(ctx, j.name), r.opts, j.req)
			if _, drift := j.drift(); j.err != nil && drift == nil && failed.CompareAndSwap(nil, j) {
				cancel()
			}

			if j.err != nil || !r.live {
				return
			}

			if kind, version, note := j.row(r.style, host); kind != "" {
				// A dry run installs nothing, so its rows say what would change.
				if r.dryRun {
					kind = "?"
				}

				liveMu.Lock()
				fmt.Fprintln(liveOut, liveRow(r.style, r.nameWidth, kind, j.name, version, note))
				r.rowsShown = true
				liveMu.Unlock()
			}
		}()
	}

	wg.Wait()

	// An interrupted run leaves packages that never started.
	if err := r.cmd.Context().Err(); err != nil {
		return nil, err
	}

	if j := failed.Load(); j != nil {
		jobs = []*job{j}
	}

	if err := driftError(jobs, r.names); err != nil {
		return nil, r.unchanged(err)
	}

	return jobs, nil
}

// unchanged returns err for a failure, which leaves the profile as it was. A
// terminal already got rows for the packages that were ready, so the error
// says that none was installed.
func (r *syncRun) unchanged(err error) error {
	if !r.rowsShown {
		return err
	}

	return fmt.Errorf("%w\nnothing was installed, and the next run reuses the downloads above", err)
}

// collect puts the result of each job in r.next and reports it. It returns the
// packages of the new generation, the jobs whose when update narrows, and an
// error for each package that sync left out on a platform it has no build for.
// It prints the table of what changed.
func (r *syncRun) collect(jobs []*job) ([]profile.Package, []*job, []error, error) {
	e, stderr, out, host := r.e, r.cmd.ErrOrStderr(), r.cmd.OutOrStdout(), platform.Host()

	have, err := e.profile().Packages()
	if err != nil {
		return nil, nil, nil, err
	}

	var (
		pkgs     []profile.Package
		summary  = r.style.Table("", "package", "version", "")
		narrowed []*job
		unfit    []error
		// A terminal gets one note for all the manifests oku inferred.
		inferred []installed
	)

	for _, j := range jobs {
		name, ref := j.name, j.req.ref
		got, err := j.got, j.err

		switch {
		case err != nil:
			return nil, nil, nil, r.unchanged(fmt.Errorf("%s: %w", name, err))
		case got.lock.Name == "" && len(got.unsupported) > 0:
			// A package that fits no platform and that the lock does not pin yet
			// has no entry to keep.
		case got.lock.Name != name:
			return nil, nil, nil, r.unchanged(fmt.Errorf(
				"%s lists %s, but the manifest at %s is named %s",
				e.listPath(), name, ref, got.lock.Name,
			))
		}

		// In a pipe a dry run says what would change in its "would" lines alone.
		if kind, version, note := j.row(r.style, host); kind != "" && !r.live && !r.dryRun {
			syncRow(r.style, summary, kind, name, version, note)
		}

		if r.style.On() && !r.flags.verbose {
			inferred = append(inferred, got)
		} else {
			reportInferred(stderr, got, r.flags.verbose)
		}

		e.reportFirstUse(stderr, got)
		reportAge(stderr, got)
		reportUnsandboxed(stderr, got)
		reportNotes(stderr, got)
		reportCache(stderr, got)

		if got.lock.Name != "" {
			r.next.Set(got.lock)
		}

		if !got.lockOnly {
			pkgs = append(pkgs, got.profile)
		}

		switch {
		case len(got.unsupported) > 0 && j.req.fit == fitNarrow:
			narrowed = append(narrowed, j)
		case len(got.unsupported) > 0:
			unfit = append(unfit, e.unfitError(r.all.packages[name], name, got))
		}

		if j.fresh && j.req.previous.Version != "" && j.req.previous.Version != got.lock.Version {
			reportGained(stderr, r.all.own, name, j.req.when, got)
		}
	}

	reportInferredTogether(stderr, inferred)

	// A dry run says "would remove" further down, so it needs no row here.
	if !r.dryRun {
		for _, pkg := range have {
			if _, ok := r.all.packages[pkg.Name]; ok {
				continue
			}

			if r.live {
				fmt.Fprintln(out, liveRow(r.style, r.nameWidth, "-", pkg.Name, pkg.Version, "removed"))
			} else {
				syncRow(r.style, summary, "-", pkg.Name, pkg.Version, "removed")
			}
		}
	}

	if err := summary.Write(out); err != nil {
		return nil, nil, nil, err
	}

	return pkgs, narrowed, unfit, nil
}

// apply makes the machine match the staged generation, or the active one when
// nothing was staged. Its commit writes the narrowed whens to oku.toml and
// r.next to oku.lock. It reports whether a dry run found anything to change.
func (r *syncRun) apply(staged int, narrowed []*job, adopted *adoption) (bool, error) {
	e := r.e
	system, _ := r.cmd.Flags().GetBool(systemFlag)

	var changes bool

	c := change{
		to: staged, staged: true, system: system, yes: r.flags.yes, dryRun: r.dryRun, changes: &changes,
		commit: func() error {
			for _, j := range narrowed {
				entry := r.all.own.Packages[j.name]
				entry.When = j.got.when

				if err := list.Set(e.listPath(), j.name, entry); err != nil {
					return err
				}
			}

			if err := r.next.Write(e.lockPath()); err != nil {
				return err
			}

			if adopted != nil {
				adopted.committed = true
			}

			return nil
		},
	}
	if adopted != nil {
		c.before = &adopted.before
	}

	if staged == 0 {
		c.to, c.staged = e.profile().Current(), false
	}

	return changes, e.apply(r.cmd, r.opts, c)
}
