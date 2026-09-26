package store

import (
	"bytes"
	"io"
	"os"
)

// shareable reports whether file is not a program or library. Windows refuses to
// delete any hard link to a program that runs, so a shared program would keep
// gc from deleting an old store path.
func shareable(file string) bool {
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false
	}

	return !bytes.Equal(magic, []byte("MZ"))
}
