package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/shellhook"
)

// windowsHookLine is the line install.ps1 tells the user to add. The binary's
// name ends in .exe there, which the check has to allow.
var windowsHookLine = shellhook.Line("pwsh", `$HOME\AppData\Local\oku\bin\oku.exe`)

func TestB91DoctorFindsTheHookLineInEveryPowerShellProfile(t *testing.T) {
	for _, name := range []string{
		"Documents/WindowsPowerShell/profile.ps1",
		"Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1",
		"Documents/PowerShell/profile.ps1",
		"Documents/PowerShell/Microsoft.VSCode_profile.ps1",
		".config/powershell/profile.ps1",
		".config/powershell/Microsoft.PowerShell_profile.ps1",
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			file := writeProfile(t, name, "# mine\n"+windowsHookLine+"\n")

			out, _ := m.run(t, "", "doctor")
			if !strings.Contains(out, "the shell hook is loaded from "+file) {
				t.Fatalf("doctor does not find the hook line in %s:\n%s", file, out)
			}
		})
	}
}

func TestB91DoctorIgnoresACommentedHookLineAndAnUnreadProfile(t *testing.T) {
	m := newMachine(t)
	writeProfile(t, "Documents/WindowsPowerShell/profile.ps1", "# "+windowsHookLine+"\n")
	writeProfile(t, "Documents/PowerShell/notes.ps1", windowsHookLine+"\n")

	out, _ := m.run(t, "", "doctor")
	if !strings.Contains(out, "no shell hook line found") {
		t.Fatalf("doctor read a commented line, or a file PowerShell does not read:\n%s", out)
	}
}

// The installer cannot import Go, so it repeats the pattern oku doctor uses.
// This test checks that the two are the same.
func TestB220InstallScriptMatchesTheHookLineTheSameWayAsDoctor(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "install.ps1"))
	must(t, err)

	if !strings.Contains(string(script), "'"+shellhook.HookPattern+"'") {
		t.Fatalf("install.ps1 does not use the pattern %q that oku doctor uses", shellhook.HookPattern)
	}

	for _, profile := range []string{"CurrentUserAllHosts", "CurrentUserCurrentHost"} {
		if !strings.Contains(string(script), "$PROFILE."+profile) {
			t.Fatalf("install.ps1 does not look in $PROFILE.%s for the hook line", profile)
		}
	}
}

// writeProfile puts body in a startup file under the test's home directory.
func writeProfile(t *testing.T, name, body string) string {
	t.Helper()

	file := filepath.Join(os.Getenv("HOME"), filepath.FromSlash(name))
	must(t, os.MkdirAll(filepath.Dir(file), 0o755))
	must(t, os.WriteFile(file, []byte(body), 0o644))

	return file
}
