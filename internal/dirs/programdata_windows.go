package dirs

import "golang.org/x/sys/windows"

// ProgramData returns the folder for data of every user, such as
// C:\ProgramData. It asks Windows, since the ProgramData variable comes from
// the environment of whoever starts oku, and an elevated oku writes there.
func ProgramData() string {
	path, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return `C:\ProgramData`
	}

	return path
}
