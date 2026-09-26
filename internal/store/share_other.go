//go:build !darwin && !linux && !windows

package store

// shareable reports that every file can be shared.
func shareable(string) bool {
	return true
}
