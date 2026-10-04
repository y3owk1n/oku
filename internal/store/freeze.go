package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// freeze takes the write bits off every file and directory under path, so that
// a program cannot write into a package after oku made it, such as to save a
// cache beside itself. Windows keeps its files as
// they are, since it gives a directory no such bit.
func freeze(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// A link has no mode of its own, and Chmod would change its target.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if mode := info.Mode().Perm(); mode&0o222 != 0 {
			return os.Chmod(p, mode&^0o222)
		}

		return nil
	})
}

// thaw lets the owner write to every directory under path again, so that oku
// can change what a frozen store path holds or delete it. Files keep their
// mode, since deleting or replacing one needs only its directory.
func thaw(path string) error {
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
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

// removeFrozen deletes the frozen tree at path, or does nothing when there is
// none.
func removeFrozen(path string) error {
	if err := thaw(path); err != nil {
		return err
	}

	return os.RemoveAll(path)
}
