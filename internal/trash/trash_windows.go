package trash

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// remove moves each file that Windows refuses to delete into dir. Windows keeps
// every hard link of a program that runs, and every shim is a hard link of one
// file, but it lets such a file move. os.RemoveAll deletes a read-only file and
// leaves the attribute of its other links alone, where os.Remove would clear it.
func remove(path, dir string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	}

	_ = filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || os.RemoveAll(file) == nil {
			return nil //nolint:nilerr
		}

		// A file that cannot move either makes the last RemoveAll fail with its error.
		if os.MkdirAll(dir, 0o755) == nil {
			aside := fmt.Sprintf("%x-%s", time.Now().UnixNano(), entry.Name())
			_ = os.Rename(file, filepath.Join(dir, aside))
		}

		return nil
	})

	return os.RemoveAll(path)
}
