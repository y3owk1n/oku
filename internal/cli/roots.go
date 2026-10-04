package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/tempdir"
)

// sessionsDir holds a file for each running oku shell and oku run, named after
// the oku process, with the store paths it uses, one per line.
const sessionsDir = "sessions"

// holdSession records paths for gc, which keeps them while this process runs.
// The returned func deletes the record.
func (e env) holdSession(paths []string) (func(), error) {
	dir := filepath.Join(e.data, sessionsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(path, []byte(strings.Join(paths, "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}

	return func() { _ = os.Remove(path) }, nil
}

// sessionRoots returns the store paths of the oku shell and run sessions that
// still run, and deletes the record of each one that ended.
func (e env) sessionRoots() ([]string, error) {
	dir := filepath.Join(e.data, sessionsDir)

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var roots []string

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !tempdir.Running(pid) {
			_ = os.Remove(path)

			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		roots = append(roots, strings.Fields(string(data))...)
	}

	return roots, nil
}

// liveRoots returns the store paths in use outside the generations: those of
// the ledger and of the running oku shell and run sessions.
func (e env) liveRoots(profiles []*profile.Profile) ([]string, error) {
	exposed, err := e.exposedRoots(profiles)
	if err != nil {
		return nil, err
	}

	sessions, err := e.sessionRoots()

	return append(exposed, sessions...), err
}

// exposedRoots returns the store paths that the ledger's apps, fonts, services
// and launchers lead into, each with its closure from a generation of
// profiles. The ledger can keep an item after its generation is gone, when the
// user declined the system change that would remove it.
func (e env) exposedRoots(profiles []*profile.Profile) ([]string, error) {
	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return nil, err
	}

	closures := map[string][]string{}

	for _, prof := range profiles {
		gens, err := prof.Generations()
		if err != nil {
			return nil, err
		}

		for _, gen := range gens {
			for _, pkg := range gen.Packages {
				closures[pkg.StorePath] = pkg.Closure
			}
		}
	}

	var roots []string

	for _, item := range ledger.Items {
		for _, st := range e.stores() {
			if at, in := st.Holding(item.Source); in {
				roots = append(append(roots, at), closures[at]...)
			}
		}
	}

	return roots, nil
}
