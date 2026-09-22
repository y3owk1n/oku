//go:build !windows

package store

import "os"

// linkNeed makes dest a symlink to the tool at target.
func linkNeed(dest, target string) error {
	return os.Symlink(target, dest)
}
