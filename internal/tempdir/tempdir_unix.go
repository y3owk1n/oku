//go:build !windows

package tempdir

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// running reports whether a process with pid exists. EPERM means it does,
// under another user.
func running(pid int) bool {
	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

func ownedByMe(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)

	return !ok || int(st.Uid) == os.Getuid()
}
