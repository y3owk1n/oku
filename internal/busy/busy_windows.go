package busy

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is past any pid, so a waiting process can still read the pid.
// Windows refuses a read of a locked range.
const lockOffset = 1 << 30

func tryLock(f *os.File) error {
	overlapped := windows.Overlapped{Offset: lockOffset}

	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errHeld
	}

	return err
}
