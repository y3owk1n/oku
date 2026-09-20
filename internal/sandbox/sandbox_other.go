//go:build !darwin && !linux

package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
)

func unavailable() string {
	return "oku has no sandbox for " + runtime.GOOS
}

func command(context.Context, Spec) (*exec.Cmd, string) {
	return nil, unavailable()
}

// Init only exists for the Linux sandbox.
func Init() error {
	return errors.New("sandbox-init is only used on Linux")
}
