package store

import (
	"os"
	"os/exec"
	"path/filepath"
)

// hostEnv returns the system directories that come last in a build's PATH, and the
// variables that point a build at its own home and temporary directory.
//
// Windows programs do not start without SystemRoot, and cmd and PowerShell need
// ComSpec and PATHEXT. Everything that names a user directory points into the
// build's scratch home, so a build neither reads nor writes the real profile.
func hostEnv(home, tmp string) (systemDirs, vars []string) {
	root := os.Getenv("SystemRoot")

	systemDirs = []string{
		filepath.Join(root, "System32"),
		root,
		filepath.Join(root, "System32", "WindowsPowerShell", "v1.0"),
	}

	// PowerShell 7 is not in a system directory.
	if pwsh, err := exec.LookPath("pwsh"); err == nil {
		systemDirs = append(systemDirs, filepath.Dir(pwsh))
	}

	vars = []string{
		"SystemRoot=" + root,
		"SystemDrive=" + filepath.VolumeName(root),
		"ComSpec=" + filepath.Join(root, "System32", "cmd.exe"),
		"PATHEXT=" + os.Getenv("PATHEXT"),
		"HOME=" + home, "USERPROFILE=" + home,
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"TEMP=" + tmp, "TMP=" + tmp,
	}

	return systemDirs, vars
}
