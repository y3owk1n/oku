package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/ui"
)

// pendingFile exists only while oku applies a change. It holds what a revert
// needs, so that a later command can finish the revert after a crash.
const pendingFile = "pending.toml"

// savedLists are oku.toml and oku.lock as they were before a change. An empty
// text with Had unset is a file that did not exist.
type savedLists struct {
	HadList bool   `toml:"had_list,omitempty"`
	List    string `toml:"list,omitempty"`
	HadLock bool   `toml:"had_lock,omitempty"`
	Lock    string `toml:"lock,omitempty"`
}

type pending struct {
	// Project is the directory of the project list, or empty for the global list.
	Project string `toml:"project,omitempty"`
	From    int    `toml:"from"`
	To      int    `toml:"to"`
	// Staged reports that the change built generation To, so a revert deletes it.
	Staged bool `toml:"staged,omitempty"`
	// Elevated reports that the change wrote system scope, so a revert does too.
	Elevated bool       `toml:"elevated,omitempty"`
	Before   savedLists `toml:"before"`
	// Started holds the lists as the change found them, and Committing reports
	// that the change began to write them. A list that no longer holds what the
	// change found, while the change had not begun to write it, holds edits made
	// since, so a revert keeps it.
	Started    savedLists `toml:"started"`
	Committing bool       `toml:"committing,omitempty"`
	// Done reports that the change finished and only pendingFile was left, so
	// there is nothing to undo.
	Done bool `toml:"done,omitempty"`
}

// change is one switch of generation.
type change struct {
	// to is the generation to activate. It may be the active one, when only the
	// exposures or the lock differ.
	to     int
	staged bool
	system bool
	// yes takes system scope without asking, as --yes does.
	yes bool
	// before replaces the list files read at the start of the apply. "oku sync
	// <list-ref>" sets it, because it writes the list before it reconciles.
	before *savedLists
	// commit edits oku.toml and writes oku.lock.
	commit func() error
	// dryRun stops after the plan and prints what the apply would do.
	dryRun bool
	// changes, when set, records whether the dry run found anything to change.
	changes *bool
}

func (e env) readSavedLists() (savedLists, error) {
	var (
		files savedLists
		err   error
	)

	if files.List, files.HadList, err = readIfExists(e.listPath()); err != nil {
		return files, err
	}

	files.Lock, files.HadLock, err = readIfExists(e.lockPath())

	return files, err
}

func readIfExists(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}

	return string(data), err == nil, err
}

// apply makes the machine match generation c.to. It checks what it can before
// the first change. When a later step fails it puts the machine back as it was
// and returns the error of that step.
func (e env) apply(cmd *cobra.Command, opts Options, c change) error {
	prof := e.profile()

	p, plan, err := e.plan(cmd, opts, c)
	if err != nil || c.dryRun {
		if err == nil {
			err = e.describe(cmd, c, plan)
		}

		if c.staged {
			err = errors.Join(err, prof.Discard(c.to))
		}

		return err
	}

	defer holdInterrupts(cmd.ErrOrStderr())()

	// The generation comes first, because the target of a file with content
	// points through "current".
	err = prof.Activate(c.to)
	if err == nil {
		err = e.placeExposed(cmd, opts, plan)
	}

	if err == nil {
		p.Committing = true
		if err = e.writePending(p); err == nil {
			err = c.commit()
		}
	}

	if err == nil {
		path := filepath.Join(e.data, pendingFile)
		if err := os.Remove(path); err != nil {
			p.Done = true

			return errors.Join(fmt.Errorf("the change finished, but oku could not delete %s: %w", path, err), e.writePending(p))
		}

		return nil
	}

	if _, undoErr := e.revert(cmd.Context(), opts, p); undoErr != nil {
		return fmt.Errorf(
			"%w\noku could not put the machine back: %w\n"+
				"the next oku command tries again, and `oku doctor` shows what is left",
			err, undoErr,
		)
	}

	return err
}

// plan checks a change and writes pendingFile. It changes nothing else.
func (e env) plan(cmd *cobra.Command, opts Options, c change) (pending, exposePlan, error) {
	prof := e.profile()

	pkgs, err := prof.PackagesOf(c.to)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	files, err := prof.FilesWithContent(c.to)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	// A secret that cannot be decrypted has to stop the change here.
	secrets, err := e.unseal(cmd.Context(), c.to, files)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	wantedSettings, err := prof.SettingsOf(c.to)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	plan, err := e.planExposed(cmd, opts, pkgs, files, wantedSettings, c.system, c.yes)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	plan.secrets = secrets

	if plan.rewritten, err = e.rewritten(c.to); err != nil {
		return pending{}, exposePlan{}, err
	}

	p := pending{
		Project: e.project, From: prof.Current(), To: c.to,
		Staged: c.staged, Elevated: plan.elevated,
	}

	if p.Started, err = e.readSavedLists(); err != nil {
		return pending{}, exposePlan{}, err
	}

	p.Before = p.Started
	if c.before != nil {
		p.Before = *c.before
	}

	// A dry run stops after the plan, so nothing is pending.
	if c.dryRun {
		return p, plan, nil
	}

	return p, plan, e.writePending(p)
}

// writePending saves p, so a later command can undo the change.
func (e env) writePending(p pending) error {
	data, err := toml.Marshal(p)
	if err != nil {
		return err
	}

	return list.WriteFile(filepath.Join(e.data, pendingFile), data)
}

// rewritten returns the targets whose content generation to gives other bytes
// than the active generation, apart from a file that holds a secret.
func (e env) rewritten(to int) ([]string, error) {
	prof := e.profile()

	before, err := prof.FilesOf(prof.Current())
	if err != nil {
		return nil, err
	}

	after, err := prof.FilesOf(to)
	if err != nil {
		return nil, err
	}

	var targets []string

	for _, f := range after {
		i := slices.IndexFunc(before, func(b profile.File) bool { return b.Target == f.Target })
		if i >= 0 && before[i].Hash != f.Hash && len(f.Secrets) == 0 && f.Content != "" {
			targets = append(targets, f.Target)
		}
	}

	return targets, nil
}

// describe prints what the apply of c would do, for a dry run. The plan has
// already run, so everything it checks is known to work.
func (e env) describe(cmd *cobra.Command, c change, plan exposePlan) error {
	prof := e.profile()
	all := []wouldChange{}

	have, err := prof.PackagesOf(prof.Current())
	if err != nil {
		return err
	}

	want, err := prof.PackagesOf(c.to)
	if err != nil {
		return err
	}

	for _, pkg := range have {
		if !slices.ContainsFunc(want, func(p profile.Package) bool { return p.Name == pkg.Name }) {
			all = append(all, wouldChange{Would: "remove", Package: pkg.Name, From: pkg.Version})
		}
	}

	for _, pkg := range want {
		i := slices.IndexFunc(have, func(p profile.Package) bool { return p.Name == pkg.Name })

		switch {
		case i < 0:
			all = append(all, wouldChange{Would: "install", Package: pkg.Name, To: pkg.Version})
		case have[i].StorePath != pkg.StorePath:
			all = append(all, wouldChange{Would: "change", Package: pkg.Name, From: have[i].Version, To: pkg.Version})
		}
	}

	for _, target := range plan.rewritten {
		all = append(all, wouldChange{Would: "change", Kind: "file", Target: target})
	}

	if e.project == "" {
		ledger, err := expose.ReadLedger(e.data)
		if err != nil {
			return err
		}

		// An item that oku writes again in place, such as a file with secrets
		// whose text changed, reads as one change.
		replaced := map[[2]string]bool{}
		for _, item := range plan.wanted {
			if !expose.Holds(ledger.Items, item) {
				replaced[[2]string{string(item.Kind), item.Target}] = true
			}
		}

		for _, item := range ledger.Items {
			if !expose.Holds(plan.wanted, item) && !replaced[[2]string{string(item.Kind), item.Target}] {
				all = append(all, wouldChange{Would: "remove", Kind: string(item.Kind), Target: item.Target})
			}
		}

		for _, item := range plan.wanted {
			if expose.Holds(ledger.Items, item) {
				continue
			}

			w := wouldChange{Would: "write", Kind: string(item.Kind), Target: item.Target}

			was := slices.ContainsFunc(ledger.Items, func(had expose.Item) bool {
				return had.Kind == item.Kind && had.Target == item.Target
			})

			switch {
			case was && slices.Contains(plan.rewritten, item.Target):
				// The content line names this file already.
				continue
			case was:
				w.Would = "change"
			}

			all = append(all, w)
		}
	}

	if c.changes != nil {
		*c.changes = len(all) > 0
	}

	if showDiff, _ := cmd.Flags().GetBool(diffFlag); showDiff {
		diffs, err := e.contentDiffs(c.to)
		if err != nil {
			return err
		}

		// A file with secrets is removed and written again, and the diff goes with
		// the write.
		for i, w := range all {
			if w.Would != "remove" && (w.Kind == "file" || w.Kind == "secret") {
				all[i].Diff = diffs[w.Target]
				delete(diffs, w.Target)
			}
		}
	}

	if wantJSON(cmd) {
		return printJSON(cmd, all)
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)

	for _, w := range all {
		fmt.Fprintln(out, s.Would(s.Homes(w.String())))

		for _, line := range w.Diff {
			switch line[0] {
			case '+':
				fmt.Fprintln(out, s.Good("    "+line))
			case '-':
				fmt.Fprintln(out, s.Bad("    "+line))
			default:
				fmt.Fprintln(out, s.Dim("    "+line))
			}
		}
	}

	if len(all) == 0 {
		fmt.Fprintln(out, s.Dim("dry run: already in sync"))

		return nil
	}

	fmt.Fprintln(out, s.Dim("dry run: nothing was changed"))

	return nil
}

// wouldChange is one change a dry run found: a package to install, remove or
// change, the content of a file, or a link, app, file or setting to write or
// remove.
type wouldChange struct {
	Would   string `json:"would"`
	Package string `json:"package,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Target  string `json:"target,omitempty"`
	// Diff is how the text of a file changes, with --diff.
	Diff []string `json:"diff,omitempty"`
}

// String is the line a dry run prints for w.
func (w wouldChange) String() string {
	switch {
	case w.Package != "" && w.Would == "install":
		return fmt.Sprintf("would install %s %s", w.Package, w.To)
	case w.Package != "" && w.Would == "remove":
		return "would remove the package " + w.Package
	case w.Package != "":
		return fmt.Sprintf("would change %s from %s to %s", w.Package, w.From, w.To)
	case w.Kind == "file" && w.Would == "change":
		return "would change the content of " + w.Target
	default:
		return fmt.Sprintf("would %s the %s %s", w.Would, w.Kind, w.Target)
	}
}

// restoreLists puts oku.toml and oku.lock back as they were before p, and
// returns the path of each it kept since it holds edits made since.
func (e env) restoreLists(p pending) ([]string, error) {
	var kept []string

	for _, file := range []struct {
		path, before, started string
		hadBefore, hadStarted bool
	}{
		{e.listPath(), p.Before.List, p.Started.List, p.Before.HadList, p.Started.HadList},
		{e.lockPath(), p.Before.Lock, p.Started.Lock, p.Before.HadLock, p.Started.HadLock},
	} {
		now, has, err := readIfExists(file.path)
		if err != nil {
			return kept, err
		}

		if !p.Committing && (now != file.started || has != file.hadStarted) {
			kept = append(kept, file.path)

			continue
		}

		if file.hadBefore {
			err = list.WriteFile(file.path, []byte(file.before))
		} else if err = os.Remove(file.path); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}

		if err != nil {
			return kept, fmt.Errorf("restore %s: %w", file.path, err)
		}
	}

	return kept, nil
}

// revert puts the machine back to generation p.From. It returns the lists it
// kept, as restoreLists does.
func (e env) revert(ctx context.Context, opts Options, p pending) ([]string, error) {
	defer status.Start(ctx, "undoing the change")()

	e.project = p.Project
	prof := e.profile()

	kept, err := e.restoreLists(p)
	if err != nil {
		return kept, err
	}

	if err := prof.Activate(p.From); err != nil {
		return kept, fmt.Errorf("could not activate generation %d again: %w", p.From, err)
	}

	if e.project == "" {
		pkgs, err := prof.PackagesOf(p.From)
		if err != nil {
			return kept, err
		}

		files, err := prof.FilesWithContent(p.From)
		if err != nil {
			return kept, err
		}

		secrets, err := e.unseal(ctx, p.From, files)
		if err != nil {
			return kept, err
		}

		wantedSettings, err := prof.SettingsOf(p.From)
		if err != nil {
			return kept, err
		}

		wanted, defs, err := e.wantedItems(opts, pkgs, files, wantedSettings)
		if err != nil {
			return kept, err
		}

		ledger, err := expose.ReadLedger(e.data)
		if err != nil {
			return kept, err
		}

		if !p.Elevated {
			wanted = keepSystem(ledger.Items, wanted)
		}

		handlers, err := e.handlers(ctx, opts, defs, secrets)
		if err != nil {
			return kept, err
		}

		before := slices.Clone(ledger.Items)

		err = ledger.Sync(wanted, handlers)

		tellSettings(opts, before, ledger.Items)

		if err != nil {
			return kept, fmt.Errorf(
				"could not restore the apps, fonts and services of generation %d: %w",
				p.From,
				err,
			)
		}
	}

	if p.Staged {
		if err := prof.Discard(p.To); err != nil {
			return kept, err
		}
	}

	return kept, os.Remove(filepath.Join(e.data, pendingFile))
}

// readPending returns the change that did not finish, or nil.
func (e env) readPending() (*pending, error) {
	path := filepath.Join(e.data, pendingFile)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var p pending
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &p, nil
}

// recoverPending reverts a change that an earlier oku process did not finish. Every
// command that changes the machine calls it first.
func (e env) recoverPending(cmd *cobra.Command, opts Options) error {
	p, err := e.readPending()
	if err != nil || p == nil {
		return err
	}

	if p.Done {
		return os.Remove(filepath.Join(e.data, pendingFile))
	}

	defer holdInterrupts(cmd.ErrOrStderr())()

	kept, err := e.revert(cmd.Context(), opts, *p)
	if err != nil {
		return fmt.Errorf(
			"the last change did not finish, and oku could not put the machine back: %w",
			err,
		)
	}

	for _, path := range kept {
		warn(cmd.ErrOrStderr(), "%s changed since the last change started, so oku left it as it is", path)
	}

	if p.From == 0 {
		warn(cmd.ErrOrStderr(), "the last change did not finish, so oku undid it")

		return nil
	}

	warn(cmd.ErrOrStderr(), "the last change did not finish, so oku put generation %d back", p.From)

	return nil
}

// recoverFirst runs recoverPending for the command's list. A dry run changes
// nothing, so it stops at a change that did not finish.
func recoverFirst(cmd *cobra.Command, opts Options) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	if dryRun, _ := cmd.Flags().GetBool(dryRunFlag); dryRun {
		if p, err := e.readPending(); err != nil || p != nil {
			return errors.Join(err, errors.New(
				"the last change did not finish, and a dry run cannot put the machine back\n"+
					"run `oku sync` first",
			))
		}

		return nil
	}

	return e.recoverPending(cmd, opts)
}
