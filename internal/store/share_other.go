//go:build !darwin && !linux && !windows

package store

// cloneFile reports that oku clones on macOS and Linux only.
func cloneFile(_, _ string) error {
	return errNoClones
}

// shareable reports that every file can be shared.
func shareable(string) bool {
	return true
}
