//go:build !darwin && !linux

package clone

// File reports that oku clones on macOS and Linux only.
func File(_, _ string) error {
	return ErrUnsupported
}

// cloneTree reports that oku clones on macOS and Linux only.
func cloneTree(_, _ string) error {
	return ErrUnsupported
}
