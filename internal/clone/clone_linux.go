package clone

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// File makes dest a clone of source, which shares its blocks until either
// changes. btrfs and XFS clone, and ext4 does not.
func File(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	err = unix.IoctlFileClone(int(out.Fd()), int(in.Fd()))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		os.Remove(dest)
	}

	return refuse(err)
}

// cloneTree reports that Linux has no call that clones a whole directory, so
// oku walks the tree and clones each file on its own.
func cloneTree(_, _ string) error {
	return ErrUnsupported
}

// refuse turns the errors of a filesystem that cannot clone into
// ErrUnsupported.
func refuse(err error) error {
	for _, refused := range []error{
		unix.EOPNOTSUPP, unix.EXDEV, unix.EINVAL, unix.ENOTTY, unix.ENOSYS,
	} {
		if errors.Is(err, refused) {
			return ErrUnsupported
		}
	}

	return err
}
