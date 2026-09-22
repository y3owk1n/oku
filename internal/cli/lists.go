package cli

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
)

// maxIncludeDepth is the deepest nesting of includes that merge follows.
const maxIncludeDepth = 8

// listed is one package of the merged list.
type listed struct {
	entry list.Entry
	ref   ref.Ref
	// from is the ref of the included list that declares the package, or empty
	// for the user's own list.
	from string
}

// merger resolves includes into one package set.
// fileKey names one [files] entry across the merged lists.
type fileKey struct {
	target string
	when   platform.Selector
}

type merger struct {
	ctx     context.Context
	fetcher *ref.Fetcher
	locked  *lock.Lock
	// With refresh, merge reads includes at their newest commit and accepts
	// changed content.
	refresh bool
	// project is set for a project list, which may not place files.
	project  string
	packages map[string]listed
	// files is keyed by the target as the list has it.
	// files is keyed by target and when, so a later list overrides an entry
	// only when both agree, and two lists may write one target on other
	// platforms.
	files map[fileKey]listedFile
	vars  map[string]string
	// secrets is keyed by the name in [secrets].
	secrets map[string]listedSecret
	// settings is keyed by backend, domain and key.
	settings map[[3]string]list.Setting
	includes []lock.Include
	seen     map[string]bool
}

// merged is a list with its includes merged under it.
type merged struct {
	packages map[string]listed
	// files is sorted by target.
	files   []listedFile
	vars    map[string]string
	secrets map[string]listedSecret
	// settings is sorted by backend, domain and key.
	settings []list.Setting
	includes []lock.Include
	// own is the user's own list, without its includes.
	own *list.List
}

// lockPlatforms returns the platforms besides the host that oku.lock pins a
// package of own for, limited to those when matches, and whether each one must
// resolve. [lock] platforms names them, and they must. Without it a project
// pins every platform it can, because a project is shared between machines,
// and the global list pins the host alone.
func (e env) lockPlatforms(
	own *list.List,
	when platform.Selector,
) ([]platform.Platform, bool) {
	all, strict := own.LockPlatforms, len(own.LockPlatforms) > 0
	if !strict && e.project != "" {
		all = platform.All()
	}

	var platforms []platform.Platform

	for _, p := range all {
		if p != platform.Host() && when.Matches(p) {
			platforms = append(platforms, p)
		}
	}

	return platforms, strict
}

// loadList reads the global list and merges its includes under it. An included
// list is read at the commit oku.lock pinned and must still have the pinned
// hash, unless refresh is set.
func (e env) loadList(
	ctx context.Context,
	opts Options,
	locked *lock.Lock,
	refresh bool,
) (merged, error) {
	own, err := list.Read(e.listPath())
	if err != nil {
		return merged{}, err
	}

	m := &merger{
		ctx:      ctx,
		fetcher:  e.fetcher(opts),
		locked:   locked,
		refresh:  refresh,
		project:  e.project,
		packages: map[string]listed{},
		files:    map[fileKey]listedFile{},
		vars:     map[string]string{},
		secrets:  map[string]listedSecret{},
		settings: map[[3]string]list.Setting{},
		seen:     map[string]bool{},
	}

	if err := m.merge(own, e.listPath(), filepath.Dir(e.listPath()), "", 0); err != nil {
		return merged{}, err
	}

	files := make([]listedFile, 0, len(m.files))
	for _, key := range slices.SortedFunc(maps.Keys(m.files), func(a, b fileKey) int {
		return cmp.Or(
			cmp.Compare(a.target, b.target),
			cmp.Compare(a.when.OS, b.when.OS),
			cmp.Compare(a.when.Arch, b.when.Arch),
			cmp.Compare(a.when.Libc, b.when.Libc),
		)
	}) {
		files = append(files, m.files[key])
	}

	keys := slices.SortedFunc(maps.Keys(m.settings), func(a, b [3]string) int {
		return slices.Compare(a[:], b[:])
	})

	settings := make([]list.Setting, 0, len(keys))
	for _, key := range keys {
		settings = append(settings, m.settings[key])
	}

	return merged{
		packages: m.packages, files: files, vars: m.vars, secrets: m.secrets,
		settings: settings, includes: m.includes, own: own,
	}, nil
}

// merge adds l's includes and then l's own packages, so a package in l overrides
// the same name from anything l includes. dir is where l's relative paths start,
// and it is empty for a list that came from a URL or a repo.
func (m *merger) merge(l *list.List, origin, dir, from string, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s: includes are nested more than %d deep", origin, maxIncludeDepth)
	}

	parse := func(s string) (ref.Ref, error) {
		r, err := ref.ParseIn(dir, s)
		if err != nil {
			return r, err
		}

		if dir == "" && r.Kind == ref.File {
			return r, fmt.Errorf("a remote list cannot point at the local path %s", s)
		}

		return r, nil
	}

	for _, include := range l.Include {
		r, err := parse(include)
		if err != nil {
			return fmt.Errorf("%s: include: %w", origin, err)
		}

		if m.seen[r.String()] {
			return fmt.Errorf("%s: %s is included twice or includes itself", origin, r)
		}

		m.seen[r.String()] = true

		pin, _ := m.locked.FindInclude(r.String())
		if m.refresh {
			pin = lock.Include{}
		}

		fetched, err := m.fetcher.Fetch(m.ctx, r, pin.Commit, ref.List)
		if err != nil {
			return fmt.Errorf("include %s: %w", r, err)
		}

		sum := sha256.Sum256(fetched.Data)
		digest := hex.EncodeToString(sum[:])

		// A list on this machine is the user's own file, like oku.toml itself, so
		// oku reads it as it is. The pin protects against a list that changes
		// somewhere else.
		local := r.Kind == ref.File

		if !local && pin.SHA256 != "" && pin.SHA256 != digest {
			return fmt.Errorf(
				"include %s: the included list changed since oku.lock was written\n"+
					"run `oku update` to accept it", r,
			)
		}

		if local {
			digest = ""
		}

		m.includes = append(m.includes, lock.Include{
			Ref:    r.String(),
			Commit: fetched.Commit,
			SHA256: digest,
		})

		sub, err := list.Parse(fetched.Data, r.String())
		if err != nil {
			return err
		}

		subDir := ""
		if r.Kind == ref.File {
			subDir = filepath.Dir(r.Location)
		}

		if err := m.merge(sub, r.String(), subDir, r.String(), depth+1); err != nil {
			return err
		}
	}

	for name, entry := range l.Packages {
		r, err := parse(entry.Ref)
		if err != nil {
			return fmt.Errorf("%s: packages.%s: %w", origin, name, err)
		}

		r.Version = entry.Version
		m.packages[name] = listed{entry: entry, ref: r, from: from}
	}

	if dir == "" && len(l.Files) > 0 {
		return fmt.Errorf("%s: [files] only works in a list on this machine for now", origin)
	}

	// A cloned repo must not write into the home directory.
	if m.project != "" && len(l.Files) > 0 {
		return fmt.Errorf("%s has [files], and only the global list may place files", origin)
	}

	if m.project != "" && len(l.Vars) > 0 {
		return fmt.Errorf("%s has [vars], which only the global list uses", origin)
	}

	switch {
	case len(l.Secrets) == 0:
	case m.project != "":
		return fmt.Errorf("%s has [secrets], and only the global list may hold secrets", origin)
	case dir == "":
		return fmt.Errorf("%s: [secrets] only works in a list on this machine for now", origin)
	}

	for name, s := range l.Secrets {
		m.secrets[name] = listedSecret{secret: s, dir: dir}
	}

	if m.project != "" && len(l.Settings) > 0 {
		return fmt.Errorf(
			"%s has [%s], and only the global list may change settings",
			origin,
			l.Settings[0].Backend,
		)
	}

	// A later list overrides a variable or a setting of an earlier one, like a
	// package.
	maps.Copy(m.vars, l.Vars)

	for _, s := range l.Settings {
		m.settings[[3]string{s.Backend, s.Domain, s.Key}] = s
	}

	for _, file := range l.Files {
		m.files[fileKey{file.Target, file.When}] = listedFile{file: file, dir: dir}
	}

	return nil
}
