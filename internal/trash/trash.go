// Package trash deletes directories that may hold a program that runs.
package trash

import (
	"os"
	"path/filepath"
)

// Remove deletes path. On Windows a file that cannot be deleted, such as a
// program that runs, moves into dir, which must be on the same volume, and a
// later Remove with the same dir deletes it. Elsewhere it is os.RemoveAll.
func Remove(path, dir string) error {
	empty(dir)

	return remove(path, dir)
}

// empty deletes what dir holds, and leaves what is still in use.
func empty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}
