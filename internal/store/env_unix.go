//go:build !windows

package store

// hostEnv returns the system directories that end a build's PATH, and the
// variables that point a build at its own home and temporary directory.
func hostEnv(home, tmp string) (systemDirs, vars []string) {
	return []string{"/usr/bin", "/bin"}, []string{"HOME=" + home, "TMPDIR=" + tmp}
}
