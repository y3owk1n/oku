package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// removeBinary deletes the oku binary. Windows refuses to delete a program that
// is running, but it lets one be renamed. So oku moves itself aside, and a
// detached cmd deletes that file a few seconds after oku has exited.
func removeBinary(path string) error {
	aside := path + ".uninstalled"
	os.Remove(aside)

	if err := os.Rename(path, aside); err != nil {
		return err
	}

	const detachedProcess = 0x00000008

	// cmd has no sleep command and every Windows has ping, so ping is the wait.
	// CmdLine is given whole, because
	// cmd does not parse the quoting that Go applies to arguments.
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(
			`cmd /c "ping -n 4 127.0.0.1 >nul & del /f /q "%s""`, aside,
		),
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	return cmd.Start()
}

// deleteLater deletes dir once no program holds its files. A detached cmd tries
// every ten seconds for a day.
func deleteLater(dir string) error {
	const detachedProcess = 0x00000008

	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(
			`cmd /c "for /l %%i in (1,1,8640) do @(rd /s /q "%s" 2>nul & `+
				`if not exist "%s" exit & ping -n 11 127.0.0.1 >nul)"`,
			dir, dir,
		),
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	return cmd.Start()
}
