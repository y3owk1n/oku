//go:build !windows

package store

import (
	"os"
	"time"
)

// touch sets the time of the file at path to now.
func touch(path string) error {
	now := time.Now()

	return os.Chtimes(path, now, now)
}
