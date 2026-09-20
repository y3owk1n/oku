package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"

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

	// Task Scheduler gives a console program a window. oku detaches from it, and
	// the program below starts without one.
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

	if err := program.Start(); err != nil {
		return err
	}

	if err := killWithParent(program.Process.Pid); err != nil {
		_ = program.Process.Kill()

		return err
	}

	var exit *exec.ExitError
	if err := program.Wait(); errors.As(err, &exit) {
		return ExitError{Code: exit.ExitCode()}
	} else if err != nil {
		return err
	}

	return nil
}

// killWithParent puts the process in a job object that Windows closes when oku
// exits. "schtasks /End" kills only the task's own process, which is oku, and
// without the job the service's program would keep running.
func killWithParent(pid int) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
	); err != nil {
		return err
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)

	// The job handle stays open while oku runs, because closing it ends the program.
	return windows.AssignProcessToJobObject(job, process)
}
