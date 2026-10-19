package store

import (
	"time"

	"golang.org/x/sys/windows"
)

// touch sets the time of the file at path to now. os.Chtimes opens the file
// without sharing read access. A package that opens the same download to unpack
// it at that moment then fails, and os.Chtimes fails while a package has the
// file open.
func touch(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	h, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h) //nolint:errcheck

	now := windows.NsecToFiletime(time.Now().UnixNano())

	return windows.SetFileTime(h, nil, &now, &now)
}
