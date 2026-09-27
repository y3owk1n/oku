package sandbox

import "os/exec"

func detach(*exec.Cmd) {}

// Run runs cmd from Command and waits for it.
func Run(cmd *exec.Cmd) error {
	return cmd.Run()
}
