package cli

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// fileID names a file apart from its paths.
type fileID struct{ volume, high, low uint32 }

// idOf returns the id of the file at path, and whether it has more hard links
// than one. Windows reports neither in info, so it opens the file.
func idOf(path string, _ fs.FileInfo) (fileID, bool) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileID{}, false
	}

	handle, err := windows.CreateFile(
		name,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fileID{}, false
	}
	defer windows.CloseHandle(handle) //nolint:errcheck

	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(
		handle,
		&data,
	); err != nil ||
		data.NumberOfLinks < 2 {
		return fileID{}, false
	}

	return fileID{data.VolumeSerialNumber, data.FileIndexHigh, data.FileIndexLow}, true
}
