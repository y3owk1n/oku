//go:build !darwin && !windows && !linux

package settings

// OS returns the settings mechanism of this OS. oku knows one for macOS,
// Windows and Linux.
func OS() (Store, string) { return nil, "" }
