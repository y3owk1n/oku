package settings

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Dconf is the settings database of GNOME and related desktops, through the
// dconf tool. Writing needs the dconf service and a D-Bus session.
type Dconf struct{}

func dconf(args ...string) (string, error) {
	out, err := exec.Command("dconf", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dconf %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}

	return strings.TrimSpace(string(out)), nil
}

// Unavailable reports a machine without the dconf tool, such as a server.
func (Dconf) Unavailable() string {
	if _, err := exec.LookPath("dconf"); err != nil {
		return "the dconf tool is not on PATH"
	}

	return ""
}

func (Dconf) Encode(value any) (string, error) { return EncodeDconf(value) }

func (Dconf) Read(dir, key string) (string, bool, error) {
	// dconf prints nothing for a key that is not set.
	value, err := dconf("read", DconfPath(dir, key))

	return value, value != "", err
}

func (Dconf) Write(dir, key, fragment string) error {
	_, err := dconf("write", DconfPath(dir, key), fragment)

	return err
}

func (Dconf) Delete(dir, key string) error {
	_, err := dconf("reset", DconfPath(dir, key))

	return err
}

// Applied does nothing. dconf tells the programs that watch a key.
func (Dconf) Applied([]string) {}

// OS returns the settings mechanism of this OS.
func OS() (Store, string) { return Dconf{}, "dconf" }
