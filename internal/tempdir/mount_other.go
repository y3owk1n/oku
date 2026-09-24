//go:build !darwin

package tempdir

// oku mounts disk images on macOS only.

// Mounted returns no mount points.
func Mounted(string) ([]string, error) { return nil, nil }

func unmount(string) error { return nil }
