//go:build !windows

package cli

import (
	"io/fs"
	"syscall"
)

// fileID names a file apart from its paths.
type fileID struct{ dev, ino uint64 }

// idOf returns the id of the file that info describes, and whether it has more
// hard links than one.
func idOf(_ string, info fs.FileInfo) (fileID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return fileID{}, false
	}

	return fileID{uint64(st.Dev), st.Ino}, true
}
