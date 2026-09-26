//go:build !windows

package trash

import "os"

func remove(path, _ string) error {
	return os.RemoveAll(path)
}
