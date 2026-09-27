//go:build !windows

package dirs

// ProgramData is a folder of Windows only.
func ProgramData() string { return "" }
