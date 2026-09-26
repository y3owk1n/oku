package clone

import (
	"errors"

	"golang.org/x/sys/unix"
)

// File makes dest a clone of source, which shares its blocks until either
// changes. APFS clones, and HFS+ and network volumes do not.
func File(source, dest string) error {
	return refuse(unix.Clonefile(source, dest, unix.CLONE_NOFOLLOW))
}

// cloneTree clones a whole directory in one call, which APFS does.
func cloneTree(source, dest string) error {
	return File(source, dest)
}

// refuse turns the errors of a filesystem that cannot clone into
// ErrUnsupported.
func refuse(err error) error {
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EXDEV) {
		return ErrUnsupported
	}

	return err
}
