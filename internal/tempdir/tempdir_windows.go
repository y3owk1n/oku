package tempdir

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/windows"
)

// running reports whether a process with pid has not exited.
func running(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// An elevated process of the same user refuses the query, and it runs.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h) //nolint:errcheck

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}

	return code == 259 // STILL_ACTIVE
}

// ownedByMe is true, because the temporary directory of a Windows user is
// that user's own.
func ownedByMe(fs.FileInfo) bool { return true }
