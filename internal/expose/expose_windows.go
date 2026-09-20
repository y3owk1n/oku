package expose

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fontsKey names the registry key that lists fonts. Windows shows a font to
// programs only when it is named there. A font for every user is under HKLM,
// which only an administrator may write.
func fontsKey(item Item) string {
	hive := "HKCU"
	if item.System {
		hive = "HKLM"
	}

	return hive + `\Software\Microsoft\Windows NT\CurrentVersion\Fonts`
}

// placeShortcut writes a Start Menu shortcut to target through the shell's COM
// object, which is the interface Windows provides for writing a .lnk file. The
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

func fontValue(item Item) string { return "oku " + filepath.Base(item.Target) }

func registerFont(item Item) error {
	return reg(
		"add",
		fontsKey(item),
		"/v",
		fontValue(item),
		"/t",
		"REG_SZ",
		"/d",
		item.Target,
		"/f",
	)
}

// unregisterFont asks first whether the value exists. reg reports a missing value
// only in the language of the Windows install, so oku cannot match its text.
func unregisterFont(item Item) error {
	if reg("query", fontsKey(item), "/v", fontValue(item)) != nil {
		return nil
	}

	return reg("delete", fontsKey(item), "/v", fontValue(item), "/f")
}

func reg(args ...string) error {
	out, err := exec.Command("reg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}

	return nil
}
