//go:build !windows

package cli

import "os"

// removeBinary deletes the oku binary. A unix program may delete its own file
// while it runs.
func removeBinary(path string) error {
	return os.Remove(path)
}
