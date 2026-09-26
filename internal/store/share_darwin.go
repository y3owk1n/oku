package store

import (
	"golang.org/x/sys/unix"
)

// shareable reports whether file has no extended attributes, which a clone or a
// hard link would take from another file.
func shareable(file string) bool {
	n, err := unix.Listxattr(file, nil)

	return err == nil && n == 0
}
