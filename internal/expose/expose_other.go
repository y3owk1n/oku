//go:build !windows

package expose

import "errors"

func placeShortcut(_, _ string) error {
	return errors.New("shortcuts exist on Windows only")
}

// registerFont and unregisterFont do nothing here, because macOS and Linux find a
// font by its directory.
func registerFont(string) error   { return nil }
func unregisterFont(string) error { return nil }
