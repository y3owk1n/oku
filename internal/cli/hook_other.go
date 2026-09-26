//go:build !windows

package cli

// knownDocumentsDir is empty off Windows, where Documents is always under the
// home directory.
func knownDocumentsDir() string { return "" }
