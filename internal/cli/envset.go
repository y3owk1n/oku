package cli

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/shellhook"
)

// shellEnv is what the hook and oku exec apply over the environment, from each
// source in the order of precedence: the global profile's packages, the global
// list's [env], the project's programs and packages, and the project's [env].
type shellEnv struct {
	set   map[string]string
	unset map[string]bool
	// prepend holds, for each list variable, the entries to put in front of it,
	// in order.
	prepend map[string][]string
	// missing holds each required variable that nothing sets, with its hint.
	missing map[string]string
}

// hookState is what the hook applied last time, kept in the environment so the
// next run can undo exactly that.
type hookState struct {
	// saved holds the value each variable had before the hook set or unset it,
	// nil for one that was not set.
	saved map[string]*string
	// added holds the entries the hook put in front of each list variable.
	added map[string][]string
}

// readHookState reads the hook's state from the environment. A shell that an
// older oku set up kept the bin directory in StatePath and the names it set in
// StateKeys, which had no value before.
func readHookState() hookState {
	state := hookState{saved: map[string]*string{}, added: map[string][]string{}}

	_ = json.Unmarshal([]byte(os.Getenv(shellhook.StateSaved)), &state.saved)
	_ = json.Unmarshal([]byte(os.Getenv(shellhook.StateAdded)), &state.added)

	if old := os.Getenv(shellhook.StatePath); old != "" && state.added["PATH"] == nil {
		state.added["PATH"] = []string{old}
	}

	for _, name := range strings.FieldsFunc(os.Getenv(shellhook.StateKeys), func(r rune) bool { return r == ':' }) {
		if _, ok := state.saved[name]; !ok {
			state.saved[name] = nil
		}
	}

	return state
}

// base returns name as it is without what the hook applied, which is the value
// the hook saved, or the current value without the entries the hook put in
// front.
func (s hookState) base(name string) (string, bool) {
	if old, ok := s.saved[name]; ok {
		if old == nil {
			return "", false
		}

		return *old, true
	}

	value, ok := os.LookupEnv(name)
	if !ok || s.added[name] == nil {
		return value, ok
	}

	// Only the first of each entry is the hook's, since the user may list the
	// same directory later on.
	kept := filepath.SplitList(value)
	for _, entry := range s.added[name] {
		if i := slices.Index(kept, entry); i >= 0 {
			kept = slices.Delete(kept, i, i+1)
		}
	}

	return strings.Join(kept, string(os.PathListSeparator)), true
}

// wantedEnv works out the environment for project, empty outside one, over
// base. active says whether the project's programs and variables apply. It also
// returns a hint for the user, such as a list that does not parse.
func (e env) wantedEnv(project string, active bool, base func(string) (string, bool)) (shellEnv, []string) {
	want := shellEnv{
		set: map[string]string{}, unset: map[string]bool{},
		prepend: map[string][]string{}, missing: map[string]string{},
	}

	var hints []string

	want.packages(e.globalProfile())
	hints = append(hints, want.list(filepath.Join(e.config, list.FileName), base)...)

	if project != "" && active {
		e.project = project
		want.prepend["PATH"] = append([]string{e.profile().BinDir()}, want.prepend["PATH"]...)
		want.packages(e.profile())
		hints = append(hints, want.list(e.listPath(), base)...)
	}

	for _, name := range slices.Sorted(maps.Keys(want.missing)) {
		hints = append(hints, name+" is not set, "+want.missing[name])
	}

	return want, hints
}

// packages sets the [env] of the packages of prof.
func (w *shellEnv) packages(prof *profile.Profile) {
	pkgs, _ := prof.Packages()
	for _, pkg := range pkgs {
		for name, value := range pkg.Env {
			w.set[name] = value
			delete(w.unset, name)
		}
	}
}

// list applies the [env] of the list at path. A list that does not parse is
// left out, and the hint says why.
func (w *shellEnv) list(path string, base func(string) (string, bool)) []string {
	if _, err := os.Stat(path); err != nil {
		return nil
	}

	l, err := list.Read(path)
	if err != nil {
		return []string{err.Error()}
	}

	change, err := list.ResolveEnv(l.Env, filepath.Dir(path), func(name string) (string, bool) {
		if w.unset[name] {
			return "", false
		}

		if value, ok := w.set[name]; ok {
			return value, true
		}

		return base(name)
	})
	if err != nil {
		return []string{path + ": " + err.Error()}
	}

	for name, value := range change.Set {
		w.set[name] = value
		delete(w.unset, name)
	}

	for _, name := range change.Unset {
		w.unset[name] = true
		delete(w.set, name)
	}

	for name, entries := range change.Prepend {
		w.prepend[name] = append(slices.Clone(entries), w.prepend[name]...)
	}

	maps.Copy(w.missing, change.Missing)

	return nil
}

// finalValues returns the value each variable of want ends up with over base,
// nil for one that is unset. A list variable that want only prepends to is in
// prepended too, whose entries the hook undoes by removing them.
func (w shellEnv) finalValues(base func(string) (string, bool)) (map[string]*string, map[string][]string) {
	final := map[string]*string{}
	prepended := map[string][]string{}

	for name, value := range w.set {
		final[name] = &value
	}

	for name := range w.unset {
		final[name] = nil
	}

	for name, entries := range w.prepend {
		rest, ok := base(name)
		if value, set := w.set[name]; set {
			rest, ok = value, true
		} else if w.unset[name] {
			rest, ok = "", false
		} else {
			prepended[name] = entries
		}

		joined := strings.Join(entries, string(os.PathListSeparator))
		if ok && rest != "" {
			joined += string(os.PathListSeparator) + rest
		}

		final[name] = &joined
	}

	return final, prepended
}

// execEnviron returns the environment oku exec runs a command with, and its
// PATH. It is
// what the hook would apply in e.project with the project allowed. It fails on
// a list that does not parse and on a variable a list requires that is not set.
func (e env) execEnviron() ([]string, string, error) {
	state := readHookState()

	// The hook puts the global programs on PATH once, when the shell starts.
	base := func(name string) (string, bool) {
		value, ok := state.base(name)
		if name != "PATH" {
			return value, ok
		}

		return strings.Join([]string{e.globalProfile().BinDir(), value}, string(os.PathListSeparator)), true
	}

	want, problems := e.wantedEnv(e.project, e.project != "", base)
	if len(problems) > 0 {
		return nil, "", errors.New(strings.Join(problems, "\n"))
	}

	final, _ := want.finalValues(base)
	if _, ok := final["PATH"]; !ok {
		path, _ := base("PATH")
		final["PATH"] = &path
	}

	environ := make([]string, 0, len(final))

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, ours := final[name]; !ours && !isHookState(name) {
			environ = append(environ, entry)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(final)) {
		if value := final[name]; value != nil {
			environ = append(environ, name+"="+*value)
		}
	}

	return environ, *final["PATH"], nil
}

// isHookState reports whether name is a variable the hook keeps its state in.
func isHookState(name string) bool {
	return slices.Contains([]string{
		shellhook.StateSaved, shellhook.StateAdded, shellhook.StatePath, shellhook.StateKeys, shellhook.StateHint,
	}, name)
}
