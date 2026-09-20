package cli

import (
	"context"
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
	return []string{
		"cmd", "/c", "mkdir", dir, "&&", "icacls", dir, "/grant", owner.Username + ":(OI)(CI)F",
	}
}

func removeDirArgv(dir string) []string { return []string{"cmd", "/c", "rmdir", dir} }
