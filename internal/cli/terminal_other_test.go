//go:build !darwin

package cli_test

import "testing"

// openTerminal returns nothing here. Only the macOS case of the test calls it,
// and Linux opens its terminal in the test.
func openTerminal(t *testing.T) string {
	t.Helper()

	return ""
}
