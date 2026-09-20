package expose

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fontsKey lists the fonts of the current user. Windows shows a per-user font to
// programs only when it is named here.
const fontsKey = `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`

// placeShortcut writes a Start Menu shortcut to target through the shell's COM
// object, which is the one interface Windows gives for the .lnk format. The
// paths go through the environment, so no quoting rule applies to them.
func placeShortcut(program, shortcut string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:OKU_SHORTCUT); "+
			"$s.TargetPath = $env:OKU_PROGRAM; "+
			"$s.WorkingDirectory = Split-Path $env:OKU_PROGRAM; $s.Save()")
	cmd.Env = append(os.Environ(), "OKU_SHORTCUT="+shortcut, "OKU_PROGRAM="+program)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create shortcut: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func fontValue(path string) string { return "oku " + filepath.Base(path) }

func registerFont(path string) error {
	return reg("add", fontsKey, "/v", fontValue(path), "/t", "REG_SZ", "/d", path, "/f")
}

func unregisterFont(path string) error {
	err := reg("delete", fontsKey, "/v", fontValue(path), "/f")
	if err != nil && strings.Contains(err.Error(), "unable to find") {
		return nil
	}

	return err
}

func reg(args ...string) error {
	out, err := exec.Command("reg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}

	return nil
}
