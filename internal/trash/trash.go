// Package trash deletes directories that may hold a program that runs.
package trash

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Remove deletes path. On Windows a file that cannot be deleted, such as a
// program that runs, moves into dir, which must be on the same volume, and a
// later Remove with the same dir deletes it. Elsewhere it is os.RemoveAll.
// Remove first lets the owner write to each directory under path, since the
// store's packages are read-only.
func Remove(path, dir string) error {
	empty(dir)

	if err := writable(path); err != nil {
		return err
	}

	return remove(path, dir)
}

// writable lets the owner write to every directory under path, which deleting
// what it holds needs.
func writable(path string) error {
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if mode := info.Mode().Perm(); mode&0o700 != 0o700 {
			return os.Chmod(p, mode|0o700)
		}

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return err
}

// empty deletes what dir holds, and leaves what is still in use.
func empty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		_ = writable(filepath.Join(dir, entry.Name()))
		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}
