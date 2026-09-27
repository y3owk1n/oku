package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"golang.org/x/sys/windows"
)

// elevatedCommand returns the command that runs argv with administrator rights.
// A process that is already elevated runs argv directly. Otherwise PowerShell
// starts it with the "RunAs" verb, which shows the Windows consent prompt, and
// waits for it. The arguments go through the environment, so that no quoting
// rule of PowerShell applies to them.
func elevatedCommand(ctx context.Context, argv []string) *exec.Cmd {
	if windows.GetCurrentProcessToken().IsElevated() {
		return exec.CommandContext(ctx, argv[0], argv[1:]...)
	}

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$a = $env:OKU_ELEVATE_ARGS -split [char]0x1f; "+
			"$p = Start-Process -FilePath $env:OKU_ELEVATE_FILE -ArgumentList $a "+
			"-Verb RunAs -Wait -PassThru -WindowStyle Hidden; exit $p.ExitCode")
	cmd.Env = append(
		os.Environ(),
		"OKU_ELEVATE_FILE="+argv[0],
		"OKU_ELEVATE_ARGS="+strings.Join(quoteArgs(argv[1:]), "\x1f"),
	)

	return cmd
}

// quoteArgs wraps an argument that holds a space in quotes, because
// Start-Process joins its arguments with spaces and quotes nothing.
func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))

	for i, arg := range args {
		quoted[i] = arg
		if strings.ContainsAny(arg, " \t") {
			quoted[i] = `"` + arg + `"`
		}
	}

	return quoted
}

// createRootArgv creates dir and gives owner full control of it and of what is
// made in it later.
func createRootArgv(dir string, owner *user.User) []string {
	// The directory can exist already, because system services keep their
	// definitions and logs in it, and mkdir fails on a directory that exists.
	// Inheritance from ProgramData would let every user add files. The root
	// keeps SYSTEM, Administrators and owner only, and Administrators owns it.
	return []string{
		"cmd", "/c", "(if", "not", "exist", dir, "mkdir", dir + ")", "&&",
		"icacls", dir, "/inheritance:r", "/grant:r",
		"*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", owner.Username + ":(OI)(CI)F", "&&",
		"icacls", dir, "/setowner", "*S-1-5-32-544",
	}
}

// checkRootOwner fails when dir exists and belongs to someone other than
// SYSTEM, Administrators or the user. Any user can make a folder in
// ProgramData, and one made before setup would stay theirs.
func checkRootOwner(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}

	if err != nil {
		return err
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}

	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}

	switch owner.String() {
	case "S-1-5-18", "S-1-5-32-544", me.User.Sid.String():
		return nil
	}

	return fmt.Errorf("%s exists and belongs to another user, remove it first", dir)
}

func removeDirArgv(dir string) []string { return []string{"cmd", "/c", "rmdir", dir} }
