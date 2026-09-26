package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/tempdir"
	"github.com/y3owk1n/oku/internal/ui"
)

func newGCCmd() *cobra.Command {
	var (
		keep      int
		olderThan string
		dryRun    bool
		cache     bool
	)

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Delete store paths that no generation uses",
		Long: `Delete store paths that no generation of any profile uses.

Old generations keep their packages in the store so rollback needs no download.
--keep deletes old generations first, which frees the packages only they use.
--older-than deletes the generations older than a number of days or weeks, and
keeps the one that was active then, so you can still roll back to that date.
With both, a generation stays when either flag keeps it.
--cache also deletes the downloads that no kept store path was made from.

gc also shares the identical files of store paths that an older oku installed,
so the disk keeps each of them once.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("keep") && keep < 1 {
				return fmt.Errorf("--keep must be at least 1, got %d", keep)
			}

			r := profile.Retention{Keep: keep}

			if olderThan != "" {
				age, err := parseAge(olderThan)
				if err != nil {
					return err
				}

				r.Since = time.Now().Add(-age)
			}

			return runGC(cmd, r, dryRun, cache)
		},
	}

	cmd.Flags().
		IntVar(&keep, "keep", 0, "first delete all but the newest N generations of each profile")
	cmd.Flags().StringVar(
		&olderThan, "older-than", "",
		"first delete the generations older than this, such as 30d or 2w",
	)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be deleted and delete nothing")
	cmd.Flags().BoolVar(&cache, "cache", false, "also delete the downloads that no kept store path was made from")

	return cmd
}

// parseAge reads a number of days or weeks, such as 30d or 2w.
func parseAge(text string) (time.Duration, error) {
	units := map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour}

	n, err := strconv.Atoi(text[:max(len(text)-1, 0)])
	unit, ok := units[text[max(len(text)-1, 0):]]

	if err != nil || !ok || n < 1 {
		return 0, fmt.Errorf(
			"--older-than takes a number of days or weeks, such as 30d or 2w, got %q", text,
		)
	}

	return time.Duration(n) * unit, nil
}

func runGC(cmd *cobra.Command, r profile.Retention, dryRun, cache bool) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	// A revert needs the generation and the store paths it goes back to.
	if p, err := e.readPending(); err != nil || p != nil {
		return errors.Join(err, errors.New(
			"the last change did not finish. Run `oku sync` first, which puts the machine back",
		))
	}

	profiles, err := profile.All(e.data)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)
	verb := "removed"

	// A dry run removes nothing, so its lines carry no check.
	mark := s.Done
	if dryRun {
		verb, mark = "would remove", func(text string) string { return text }
	}

	pruned := map[*profile.Profile][]int{}

	if r.Keep > 0 || !r.Since.IsZero() {
		for _, prof := range profiles {
			if pruned[prof], err = prof.Prune(r, dryRun); err != nil {
				return err
			}

			for _, n := range pruned[prof] {
				fmt.Fprintln(out, mark(fmt.Sprintf("%s generation %d", verb, n)))
			}
		}
	}

	// A dry run deleted nothing, so skip the generations it would have pruned.
	holders, err := e.storeHolders(profiles, pruned)
	if err != nil {
		return err
	}

	used := map[string]bool{}
	for path := range holders {
		used[path] = true
	}

	unused := map[string]int64{}

	var freed int64

	for _, st := range e.stores() {
		done := status.Start(cmd.Context(), "scanning the store for unused packages")
		found, err := st.Unreferenced(used)

		done()

		if err != nil {
			return err
		}

		for _, path := range slices.Sorted(maps.Keys(found)) {
			if !dryRun {
				if err := st.Remove(path); err != nil {
					return fmt.Errorf("delete %s: %w", path, err)
				}
			}

			unused[path] = found[path]
			freed += found[path]
			fmt.Fprintln(out, mark(fmt.Sprintf(
				"%s %s (%s)", verb, filepath.Base(path), status.Size(found[path]),
			)))
		}

		if !dryRun {
			if err := st.DropLinks(); err != nil {
				return err
			}
		}
	}

	// gc shares the identical files of store paths from before oku shared files.
	var (
		sharedPaths int
		saved       int64
	)

	for _, st := range e.stores() {
		paths, err := st.Unshared()
		if err != nil {
			return err
		}

		// A dry run deleted nothing, so skip the store paths it would have deleted.
		paths = slices.DeleteFunc(paths, func(path string) bool {
			_, gone := unused[path]

			return gone
		})

		sharedPaths += len(paths)

		if dryRun || len(paths) == 0 {
			continue
		}

		done := status.Start(
			cmd.Context(), "sharing the identical files of %s", count(len(paths), "store path"),
		)

		for _, path := range paths {
			n, err := st.Share(path)
			saved += n

			if err != nil {
				done()

				return err
			}
		}

		done()
	}

	switch {
	case dryRun && sharedPaths > 0:
		fmt.Fprintf(out, "would share the identical files of %s\n", count(sharedPaths, "store path"))
	case saved > 0:
		freed += saved
		fmt.Fprintln(out, mark(fmt.Sprintf(
			"shared the identical files of %s (%s)", count(sharedPaths, "store path"), status.Size(saved),
		)))
	}

	// gc holds the lock of the busy package, so an oku process that changes the
	// machine does not use these.
	leftovers, err := tempdir.Stale()
	if err != nil {
		return err
	}

	for _, path := range leftovers {
		size := tempdir.Size(path)

		if !dryRun {
			if err := tempdir.Remove(path); err != nil {
				return fmt.Errorf("delete %s: %w", path, err)
			}
		}

		freed += size
		fmt.Fprintln(out, mark(fmt.Sprintf(
			"%s %s, left by an oku process that ended (%s)", verb, path, status.Size(size),
		)))
	}

	var downloads map[string]int64

	if cache {
		if downloads, err = e.staleDownloads(used); err != nil {
			return err
		}

		var size int64

		for path, n := range downloads {
			if !dryRun {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("delete %s: %w", path, err)
				}
			}

			size += n
		}

		// A cache holds hundreds of files, named after their digests, so one line
		// counts them.
		if len(downloads) > 0 {
			freed += size
			fmt.Fprintln(out, mark(fmt.Sprintf(
				"%s %s from the download cache (%s)", verb, count(len(downloads), "file"), status.Size(size),
			)))
		}
	}

	if len(unused) == 0 && len(leftovers) == 0 && len(downloads) == 0 && saved == 0 {
		// After deleted generations, "nothing to delete" would contradict the
		// lines above it.
		text := "nothing to delete, every store path is used by a generation"
		for _, gone := range pruned {
			if len(gone) > 0 {
				text = "every store path is still used by a generation"
			}
		}

		if cache {
			text += ", and every cached download belongs to one"
		}

		fmt.Fprintln(out, mark(text))

		return nil
	}

	var counts []string

	for _, c := range []struct {
		n            int
		one, several string
	}{
		{len(unused), "store path", "store paths"},
		{len(leftovers), "temporary file", "temporary files"},
		{len(downloads), "cached file", "cached files"},
	} {
		switch {
		case c.n == 1:
			counts = append(counts, "1 "+c.one)
		case c.n > 1:
			counts = append(counts, fmt.Sprintf("%d %s", c.n, c.several))
		}
	}

	if saved > 0 {
		counts = append(counts, "identical files")
	}

	noun := strings.Join(counts, ", ")
	if i := strings.LastIndex(noun, ", "); i >= 0 {
		noun = noun[:i] + " and " + noun[i+2:]
	}

	summary := "freed"
	if dryRun {
		summary = "would free"
	}

	fmt.Fprintln(out, mark(fmt.Sprintf("%s %s from %s", summary, status.Size(freed), noun)))

	return nil
}

// staleDownloads returns the files of the download cache that no store path in
// used was made from, with their sizes. A store path's meta holds the digest
// of its download, or of a build's source archive.
func (e env) staleDownloads(used map[string]bool) (map[string]int64, error) {
	keep := map[string]bool{}

	for path := range used {
		if meta, err := store.ReadMeta(path); err == nil && meta.SHA256 != "" {
			keep[meta.SHA256] = true
		}
	}

	return e.store().StaleDownloads(keep)
}
