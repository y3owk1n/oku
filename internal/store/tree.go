package store

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// treeFile records every file of a store path when oku made it, so that
// Check can tell later which files changed.
const treeFile = "oku-tree.txt"

// ErrNoTree reports a store path that an oku made before it recorded files.
var ErrNoTree = errors.New("oku did not record its files")

// writeTree records the files under dir in dir's treeFile. The record and the
// meta file stay out of it.
func writeTree(dir string) error {
	tree, err := readTree(dir)
	if err != nil {
		return fmt.Errorf("record the files of %s: %w", filepath.Base(dir), err)
	}

	var b strings.Builder

	for _, rel := range slices.Sorted(maps.Keys(tree)) {
		fmt.Fprintf(&b, "%s\t%s\n", tree[rel], rel)
	}

	return os.WriteFile(filepath.Join(dir, treeFile), []byte(b.String()), 0o444)
}

// readTree returns, for each file and link under dir by its slash path, what
// it is: the sha256 of a file with "x" for one that runs, or "link" and its
// target. A directory is in it through what it holds, and a __pycache__
// directory is not in it.
func readTree(dir string) (map[string]string, error) {
	tree := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == treeFile || rel == metaFile {
			return err
		}

		// Python writes its bytecode beside the code it runs, into the store
		// path, so oku leaves those caches out.
		if d.IsDir() && d.Name() == "__pycache__" {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		rel = filepath.ToSlash(rel)

		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			tree[rel] = "link:" + filepath.ToSlash(target)

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}

		// Windows has no mode that runs a file.
		if info.Mode()&0o111 != 0 && runtime.GOOS != "windows" {
			sum += "x"
		}

		tree[rel] = sum

		return nil
	})

	return tree, err
}

// Change is a file of a store path that differs from what oku recorded.
type Change struct {
	// Path is the file's path in the store path, with slashes.
	Path string `json:"path"`
	// Kind is "changed", "added" or "removed".
	Kind string `json:"kind"`
}

// Check compares the files of the store path at path with what oku recorded
// when it made it, and returns the files that differ, in order of their paths.
// A store path from before oku recorded files returns ErrNoTree.
func Check(path string) ([]Change, error) {
	f, err := os.Open(filepath.Join(path, treeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoTree
	}

	if err != nil {
		return nil, err
	}
	defer f.Close()

	recorded := map[string]string{}

	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 64<<10), 1<<20)

	for lines.Scan() {
		what, rel, ok := strings.Cut(lines.Text(), "\t")
		if !ok {
			return nil, fmt.Errorf("%s: a line of %s has no path", filepath.Base(path), treeFile)
		}

		recorded[rel] = what
	}

	if err := lines.Err(); err != nil {
		return nil, err
	}

	now, err := readTree(path)
	if err != nil {
		return nil, err
	}

	var changes []Change

	for _, rel := range slices.Sorted(maps.Keys(recorded)) {
		switch what, ok := now[rel]; {
		case !ok:
			changes = append(changes, Change{rel, "removed"})
		case what != recorded[rel]:
			changes = append(changes, Change{rel, "changed"})
		}
	}

	for _, rel := range slices.Sorted(maps.Keys(now)) {
		if _, ok := recorded[rel]; !ok {
			changes = append(changes, Change{rel, "added"})
		}
	}

	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })

	return changes, nil
}
