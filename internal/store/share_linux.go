package store

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

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
