//go:build !darwin && !windows

package settings

// OS returns the settings mechanism of this OS. Linux has none so far.
func OS() (Store, string) { return nil, "" }
