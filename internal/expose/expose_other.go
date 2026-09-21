//go:build !windows

package expose

import (
	"errors"
	"os"
)

func placeShortcut(_, _ string) error {
	return errors.New("shortcuts exist on Windows only")
}

// registerFont and unregisterFont do nothing here, because macOS and Linux find a
// font by its directory.
func registerFont(Item) error   { return nil }
func unregisterFont(Item) error { return nil }

// link makes target a symlink to source.
func link(source, target string) error {
	return os.Symlink(source, target)
}
