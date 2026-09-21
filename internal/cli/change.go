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
}

// change is one switch of generation.
type change struct {
	// to is the generation to activate. It may be the active one, when only the
	// exposures or the lock differ.
	to     int
	staged bool
	system bool
	// before replaces the list files read at the start of the apply. "oku sync
	// <list-ref>" sets it, because it writes the list before it reconciles.
	before *savedLists
	// commit edits oku.toml and writes oku.lock.
	commit func() error
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

func (e env) restoreSavedLists(files savedLists) error {
	for _, file := range []struct {
		path, text string
		had        bool
	}{
		{e.listPath(), files.List, files.HadList},
		{e.lockPath(), files.Lock, files.HadLock},
	} {
		var err error
		if file.had {
			err = list.WriteFile(file.path, []byte(file.text))
		} else if err = os.Remove(file.path); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}

		if err != nil {
			return fmt.Errorf("restore %s: %w", file.path, err)
		}
	}

	return nil
}

// apply makes the machine match generation c.to. It checks what it can before
// the first change. When a later step fails it puts the machine back as it was
// and returns the error of that step.
func (e env) apply(cmd *cobra.Command, opts Options, c change) error {
	prof := e.profile()

	p, plan, err := e.plan(cmd, opts, c)
	if err != nil {
		if c.staged {
			err = errors.Join(err, prof.Discard(c.to))
		}

		return err
	}

	// The generation comes first, because the target of a file with content
	// points through "current".
	err = prof.Activate(c.to)
	if err == nil {
		err = e.placeExposed(cmd, opts, plan)
	}

	if err == nil {
		err = c.commit()
	}

	if err == nil {
		return os.Remove(filepath.Join(e.data, pendingFile))
	}

	if undoErr := e.revert(cmd.Context(), opts, p); undoErr != nil {
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

	files, err := prof.FilesOf(c.to)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	wantedSettings, err := prof.SettingsOf(c.to)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	plan, err := e.planExposed(cmd, opts, pkgs, files, wantedSettings, c.system)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	p := pending{
		Project: e.project, From: prof.Current(), To: c.to,
		Staged: c.staged, Elevated: plan.elevated,
	}

	if c.before != nil {
		p.Before = *c.before
	} else if p.Before, err = e.readSavedLists(); err != nil {
		return pending{}, exposePlan{}, err
	}

	data, err := toml.Marshal(p)
	if err != nil {
		return pending{}, exposePlan{}, err
	}

	return p, plan, list.WriteFile(filepath.Join(e.data, pendingFile), data)
}

// revert puts the machine back to generation p.From.
func (e env) revert(ctx context.Context, opts Options, p pending) error {
	e.project = p.Project
	prof := e.profile()

	if err := e.restoreSavedLists(p.Before); err != nil {
		return err
	}

	if err := prof.Activate(p.From); err != nil {
		return fmt.Errorf("could not activate generation %d again: %w", p.From, err)
	}

	if e.project == "" {
		pkgs, err := prof.PackagesOf(p.From)
		if err != nil {
			return err
		}

		files, err := prof.FilesOf(p.From)
		if err != nil {
			return err
		}

		wantedSettings, err := prof.SettingsOf(p.From)
		if err != nil {
			return err
		}

		wanted, defs, err := e.wantedItems(opts, pkgs, files, wantedSettings)
		if err != nil {
			return err
		}

		ledger, err := expose.ReadLedger(e.data)
		if err != nil {
			return err
		}

		if !p.Elevated {
			wanted = keepSystem(ledger.Items, wanted)
		}

		handlers, err := e.handlers(ctx, opts, defs)
		if err != nil {
			return err
		}

		before := slices.Clone(ledger.Items)

		err = ledger.Sync(wanted, handlers)

		tellSettings(opts, before, ledger.Items)

		if err != nil {
			return fmt.Errorf(
				"could not restore the apps, fonts and services of generation %d: %w",
				p.From,
				err,
			)
		}
	}

	if p.Staged {
		if err := prof.Discard(p.To); err != nil {
			return err
		}
	}

	return os.Remove(filepath.Join(e.data, pendingFile))
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

	if err := e.revert(cmd.Context(), opts, *p); err != nil {
		return fmt.Errorf(
			"the last change did not finish, and oku could not put the machine back: %w",
			err,
		)
	}

	if p.From == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "the last change did not finish, so oku undid it")

		return nil
	}

	fmt.Fprintf(
		cmd.ErrOrStderr(),
		"the last change did not finish, so oku put generation %d back\n", p.From,
	)

	return nil
}

// recoverFirst runs recoverPending for the command's list.
func recoverFirst(cmd *cobra.Command, opts Options) error {
	e, err := scopedEnv(cmd, opts)
	if err != nil {
		return err
	}

	return e.recoverPending(cmd, opts)
}
