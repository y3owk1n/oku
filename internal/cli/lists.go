package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
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
type merger struct {
	ctx     context.Context
	fetcher *ref.Fetcher
	locked  *lock.Lock
	// With refresh, merge reads includes at their newest commit and accepts
	// changed content.
	refresh  bool
	packages map[string]listed
	includes []lock.Include
	seen     map[string]bool
}

// loadList reads the global list and merges its includes under it. An included
// list is read at the commit oku.lock pinned and must still have the pinned
// hash, unless refresh is set.
func (e env) loadList(
	ctx context.Context,
	opts Options,
	locked *lock.Lock,
	refresh bool,
) (map[string]listed, []lock.Include, error) {
	own, err := list.Read(e.listPath())
	if err != nil {
		return nil, nil, err
	}

	m := &merger{
		ctx:      ctx,
		fetcher:  e.fetcher(opts),
		locked:   locked,
		refresh:  refresh,
		packages: map[string]listed{},
		seen:     map[string]bool{},
	}

	if err := m.merge(own, e.listPath(), filepath.Dir(e.listPath()), "", 0); err != nil {
		return nil, nil, err
	}

	return m.packages, m.includes, nil
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

		if pin.SHA256 != "" && pin.SHA256 != digest {
			return fmt.Errorf(
				"include %s: the included list changed since oku.lock was written\n"+
					"run `oku update` to accept it", r,
			)
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

	return nil
}
