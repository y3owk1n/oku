package cli

import "golang.org/x/sys/windows"

// knownDocumentsDir returns the Documents directory as Windows records it. It
// is not always under the home directory. OneDrive's Known Folder Move redirects
// it to OneDrive\Documents, and a user can move it anywhere.
func knownDocumentsDir() string {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Documents, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return ""
	}

	return dir
}
