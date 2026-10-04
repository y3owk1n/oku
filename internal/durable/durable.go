// Package durable writes the files that mark a store path or a generation as
// finished so that they last a power loss.
package durable

import (
	"io/fs"
	"os"
)

// WriteFile writes data to path and flushes it to disk before it returns.
func WriteFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}

	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	return err
}

// SyncDir flushes the entries of dir to disk, so that a file created or
// renamed in it lasts a power loss. Windows cannot open a directory for this,
// so there it does nothing.
func SyncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}
