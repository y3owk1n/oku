//go:build !windows

package secret

import "os"

// Restrict makes path readable by the user alone. mode is 0700 for a directory
// and the mode of the entry for a file.
func Restrict(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
