package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/gitcmd"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/ui"
)

// mergeConflict replaces an oku.lock that holds git merge conflicts with the
// merge of its two sides. It returns what the lists held before, so a dry run
// or a failed sync puts the conflicts back, and nil when the lock has none.
func (e env) mergeConflict(cmd *cobra.Command) (*adoption, error) {
	conflict, err := readConflict(cmd.Context(), e.lockPath())
	if conflict == nil || err != nil {
		return nil, err
	}

	if locked, _ := cmd.Flags().GetBool(lockedFlag); locked {
		return nil, fmt.Errorf("%s has git merge conflicts, and --locked keeps oku from merging them", e.lockPath())
	}

	before, err := e.readSavedLists()
	if err != nil {
		return nil, err
	}

	merged, notes := mergeLocks(conflict)
	if err := merged.Write(e.lockPath()); err != nil {
		return nil, err
	}

	w := cmd.ErrOrStderr()
	s := ui.For(w)

	fmt.Fprintf(w, "%s has git merge conflicts, so oku merged %s and %s, and the sync checks the result\n",
		s.Home(e.lockPath()), conflict.oursName, conflict.theirsName)

	for _, note := range notes {
		fmt.Fprintln(w, "  "+note)
	}

	return &adoption{before: before}, nil
}

// conflict is the two sides of an oku.lock with git merge conflicts. ours is
// the side git names first, such as HEAD, and theirs the other.
type conflict struct {
	ours, theirs         *lock.Lock
	oursName, theirsName string
}

// readConflict reads the lock at path when it holds git merge conflicts, and
// returns nil when it holds none. It reads each side whole from git's index,
// since the lines outside the conflict markers are a line merge of both, which
// can pair the version of one side with the downloads of the other.
func readConflict(ctx context.Context, path string) (*conflict, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	names, ok := lock.Conflicted(data)
	if !ok {
		return nil, nil
	}

	c := &conflict{oursName: names[0], theirsName: names[1]}

	for i, into := range []**lock.Lock{&c.ours, &c.theirs} {
		// Stage 2 is ours and stage 3 theirs, while git still has the conflict.
		cmd := gitcmd.Command(ctx, "-C", filepath.Dir(path), "show", fmt.Sprintf(":%d:./%s", i+2, filepath.Base(path)))

		side, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf(
				"%s has git merge conflicts, and git holds no copy of %s's side to merge\n"+
					"resolve the conflicts by hand",
				path, names[i],
			)
		}

		if *into, err = lock.ParseFile(side, path); err != nil {
			return nil, fmt.Errorf("%s as %s has it: %w", path, names[i], err)
		}
	}

	return c, nil
}

// mergeLocks merges the two sides of a conflict. A package on one side only is
// kept. A package on both takes the side with the newer version, and at the
// same version the platforms and deps of both. An included list on both keeps
// the commit of ours, since two commits have no order. It returns a line for
// each choice it made between the sides.
func mergeLocks(c *conflict) (*lock.Lock, []string) {
	merged := &lock.Lock{Includes: slices.Clone(c.ours.Includes), Packages: slices.Clone(c.ours.Packages)}

	var notes []string

	for _, inc := range c.theirs.Includes {
		ours, ok := c.ours.FindInclude(inc.Ref)

		switch {
		case !ok:
			merged.Includes = append(merged.Includes, inc)
		case ours.Commit != inc.Commit || ours.SHA256 != inc.SHA256:
			notes = append(notes, fmt.Sprintf(
				"%s: kept the list as %s pins it, `oku update` reads the newest", inc.Ref, c.oursName,
			))
		}
	}

	for _, theirs := range c.theirs.Packages {
		ours, ok := c.ours.Find(theirs.Name)

		switch order := resolve.Compare(ours.Version, theirs.Version); {
		case !ok:
			merged.Set(theirs)
		case order < 0:
			merged.Set(theirs)
			notes = append(notes, fmt.Sprintf("%s: took %s from %s over %s", theirs.Name, theirs.Version, c.theirsName, ours.Version))
		case order > 0:
			notes = append(notes, fmt.Sprintf("%s: took %s from %s over %s", ours.Name, ours.Version, c.oursName, theirs.Version))
		default:
			merged.Set(samePin(ours, theirs))
		}
	}

	return merged, notes
}

// samePin merges two pins of one version: the platforms and deps of ours, and
// those that only theirs pins.
func samePin(ours, theirs lock.Package) lock.Package {
	merged := ours
	merged.Platforms = map[string]lock.Platform{}
	maps.Copy(merged.Platforms, theirs.Platforms)
	maps.Copy(merged.Platforms, ours.Platforms)

	merged.Deps = slices.Clone(ours.Deps)

	for _, dep := range theirs.Deps {
		if !slices.ContainsFunc(merged.Deps, func(d lock.Package) bool {
			return d.Ref == dep.Ref && d.Version == dep.Version
		}) {
			merged.Deps = append(merged.Deps, dep)
		}
	}

	return merged
}
