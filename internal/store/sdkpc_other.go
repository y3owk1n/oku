//go:build !darwin

package store

// writeSystemPkgConfig does nothing here. Linux distributions ship a pkg-config
// file with the headers of a library.
func writeSystemPkgConfig(string) string { return "" }
