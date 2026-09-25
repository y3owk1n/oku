package store

import (
	"errors"

	"golang.org/x/sys/unix"
)

// cloneFile makes dest a clone of source, which shares its blocks until either
// changes. APFS clones, and HFS+ and network volumes do not.
func cloneFile(source, dest string) error {
	err := unix.Clonefile(source, dest, unix.CLONE_NOFOLLOW)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EXDEV) {
		return errNoClones
	}

	return err
}

// shareable reports whether file has no extended attributes, which a clone or a
// hard link would take from another file.
func shareable(file string) bool {
	n, err := unix.Listxattr(file, nil)

	return err == nil && n == 0
}
