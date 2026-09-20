package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/service"
)

// platformCommands returns the hidden commands that only one OS needs.
func platformCommands() []*cobra.Command {
	return []*cobra.Command{{
		Use:    service.RunCommand + " <definition>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, args[0])
		},
	}}
}

// runService is what a service's scheduled task starts. It gives the program its
// environment and its log file, which a task cannot, and exits with its code.
func runService(cmd *cobra.Command, path string) error {
	stored, err := service.ReadStored(path)
	if err != nil {
		return err
	}

	d := stored.Definition

	// Task Scheduler gives a console program a window. oku lets go of it, and the
	// program below starts without one.
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole").Call()

	log, err := os.OpenFile(d.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	program := exec.CommandContext(cmd.Context(), d.Program, d.Args...)
	program.Stdout, program.Stderr = log, log
	program.Env = os.Environ()

	for name, value := range d.Env {
		program.Env = append(program.Env, name+"="+value)
	}

	const createNoWindow = 0x08000000

	program.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}

	var exit *exec.ExitError
	if err := program.Run(); errors.As(err, &exit) {
		return ExitError{Code: exit.ExitCode()}
	} else if err != nil {
		return err
	}

	return nil
}
