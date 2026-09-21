//go:build !darwin

package settings

// OS returns the settings mechanism of this OS. Only macOS has one so far.
func OS() (Store, string) { return nil, "" }
