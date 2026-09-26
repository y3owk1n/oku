//go:build !windows

package shim

import "os"

// tie does nothing, since only Windows runs shims.
func tie(*os.Process) {}
