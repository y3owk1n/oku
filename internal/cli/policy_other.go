//go:build !windows

package cli

// blockingScriptPolicy is "" outside Windows, where no Group Policy applies.
func blockingScriptPolicy() string { return "" }
