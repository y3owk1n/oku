//go:build !windows

package cli

import "os"

// removeBinary deletes the oku binary. A unix program may delete its own file
// while it runs.
func removeBinary(path string) error {
	return os.Remove(path)
}

// deleteLater deletes dir. Unix deletes the files of a program that runs, so
// uninstall moves nothing aside.
func deleteLater(dir string) error {
	return os.RemoveAll(dir)
}
