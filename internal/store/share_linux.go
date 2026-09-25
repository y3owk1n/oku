package store

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile makes dest a clone of source, which shares its blocks until either
// changes. btrfs and XFS clone, and ext4 does not.
func cloneFile(source, dest string) error {
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

	for _, refused := range []error{
		unix.EOPNOTSUPP, unix.EXDEV, unix.EINVAL, unix.ENOTTY, unix.ENOSYS,
	} {
		if errors.Is(err, refused) {
			return errNoClones
		}
	}

	return err
}

// shareable reports whether file has no extended attributes apart from the
// security ones, which the kernel sets by the file's path.
func shareable(file string) bool {
	buf := make([]byte, 4096)

	n, err := unix.Listxattr(file, buf)
	if errors.Is(err, unix.ERANGE) {
		return false
	}

	if err != nil {
		return errors.Is(err, unix.ENOTSUP)
	}

	for name := range bytes.SplitSeq(buf[:n], []byte{0}) {
		if len(name) > 0 && !bytes.HasPrefix(name, []byte("security.")) {
			return false
		}
	}

	return true
}
