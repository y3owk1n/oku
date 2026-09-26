// Package clone copies a file or a directory as cheaply as the filesystem
// allows. A clone shares its blocks with the file it came from until either
// side changes, so the copy costs no disk and no reading of the content. APFS,
// btrfs and XFS clone, and ext4, HFS+ and NTFS do not.
package clone

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrUnsupported reports a filesystem that cannot clone a file, or two paths on
// different filesystems.
var ErrUnsupported = errors.New("the filesystem cannot clone files")

// Tree makes dest a copy of source, which is a file or a directory, keeping
// modes and the symlinks inside it. It clones what the filesystem can clone and
// copies the rest, so the caller gets a copy either way. dest must not exist.
func Tree(source, dest string) error {
	err := cloneTree(source, dest)
	if err == nil {
		// A clone keeps the mode of the file it came from, and a store path may
		// hold read-only files and directories.
		return ownerWritable(dest)
	}

	if !errors.Is(err, ErrUnsupported) {
		return err
	}

	return copyTree(source, dest)
}

// Possible reports whether a file in the directory from can become a clone in
// the directory to, which needs both on one filesystem and that filesystem able
// to clone. It tries one, and leaves nothing behind.
func Possible(from, to string) bool {
	source, err := os.CreateTemp(from, ".tmp-clone-")
	if err != nil {
		return false
	}

	defer os.Remove(source.Name())

	_, err = source.WriteString("oku")
	if closeErr := source.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return false
	}

	dest := filepath.Join(to, filepath.Base(source.Name())+".clone")
	if err := File(source.Name(), dest); err != nil {
		return false
	}

	return os.Remove(dest) == nil
}

// copyTree walks source and clones each file it can, and copies the others.
func copyTree(source, dest string) error {
	// A filesystem that refuses one clone refuses every clone, so the walk stops
	// asking.
	copies := false

	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dest, rel)

		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}

			return os.Symlink(link, target)
		}

		if !copies {
			err := File(path, target)
			if err == nil {
				return os.Chmod(target, info.Mode().Perm()|0o600)
			}

			if !errors.Is(err, ErrUnsupported) {
				return err
			}

			copies = true
		}

		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(source, dest string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode|0o600)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	return err
}

// ownerWritable lets the owner write every file and directory of the tree at
// path, which a copy of it would also give them. Removing the tree again needs
// it, and so does an app that writes inside its own bundle.
func ownerWritable(path string) error {
	return filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		// Chmod follows a symlink, and the file it names is not part of the tree.
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil
		}

		want := info.Mode().Perm() | 0o600
		if entry.IsDir() {
			want = info.Mode().Perm() | 0o700
		}

		if want != info.Mode().Perm() {
			return os.Chmod(file, want)
		}

		return nil
	})
}
