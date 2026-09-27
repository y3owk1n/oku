package settings

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Dconf is the settings database of GNOME and related desktops, through the
// dconf tool. Writing needs the dconf service and a D-Bus session.
type Dconf struct{}

// dconfPath is the dconf of the system. A program called dconf on the user's
// PATH, which a package could ship, would get every setting oku writes.
func dconfPath() string {
	for _, path := range []string{"/usr/bin/dconf", "/bin/dconf"} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func dconf(args ...string) (string, error) {
	out, err := exec.Command(dconfPath(), args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dconf %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}

	return strings.TrimSpace(string(out)), nil
}

// Unavailable reports a machine without the dconf tool, such as a server.
func (Dconf) Unavailable() string {
	if dconfPath() == "" {
		return "the dconf tool is not in /usr/bin or /bin"
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
