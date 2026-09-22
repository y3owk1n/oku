//go:build !windows

package store

import (
	"path/filepath"
	"runtime"
)

// hostEnv returns the system directories that come last in a build's PATH, and the
// variables that point a build at its own home and temporary directory.
//
// /usr/sbin and /sbin hold sysctl, which configure scripts run to count CPUs.
//
// On macOS every developer tool in /usr/bin is a shim that asks xcrun where the
// real one is, and xcrun keeps that answer in a cache file under the user's
// temporary directory, which the sandbox does not let it write. xcrun reads the
// file's path from xcrun_db, so the cache goes into the build's own tmp.
func hostEnv(home, tmp string) (systemDirs, vars []string) {
	vars = []string{"HOME=" + home, "TMPDIR=" + tmp}

	if runtime.GOOS == "darwin" {
		vars = append(vars, "xcrun_db="+filepath.Join(tmp, "xcrun_db"))
	}

	return []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}, vars
}
