//go:build unix

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// detach starts cmd in a session of its own. A session has no controlling
// terminal, so the command cannot open /dev/tty, read what the user types, or
// push input into the user's shell. Cancelling the context kills the whole
// session, since a build leaves children such as the jobs of make.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Setsid = true
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// Run runs cmd from Command and waits for it. The terminal's interrupt no
// longer reaches a command in its own session, so on SIGINT, SIGTERM or SIGHUP
// Run kills the session and fails, and oku stops as after any failed build.
// When the command ends, Run kills what it left running in its session, so no
// process of a build step outlives it.
func Run(cmd *exec.Cmd) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

		return err
	case sig := <-signals:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done

		return errors.New("stopped by " + sig.String())
	}
}
