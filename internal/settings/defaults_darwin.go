package settings

import (
	"fmt"
	"os/exec"
	"strings"
)

// Defaults is the preference domains of macOS, through /usr/bin/defaults. That
// tool goes through the preferences daemon, which caches a domain, so oku never
// edits the plist files themselves.
type Defaults struct{}

func run(args ...string) ([]byte, error) {
	out, err := exec.Command("/usr/bin/defaults", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("defaults %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}

	return out, nil
}

func (Defaults) Read(domain, key string) (string, bool, error) {
	// A domain that does not exist exports as an empty dict.
	exported, err := run("export", domain, "-")
	if err != nil {
		return "", false, err
	}

	return Find(exported, key)
}

func (Defaults) Write(domain, key, fragment string) error {
	_, err := run("write", domain, key, fragment)

	return err
}

func (d Defaults) Delete(domain, key string) error {
	// "defaults delete" fails for a key that is not set.
	if _, set, err := d.Read(domain, key); err != nil || !set {
		return err
	}

	_, err := run("delete", domain, key)

	return err
}

// Applied makes the system read the settings again. Apps such as the Dock still
// read theirs only when they start.
func (Defaults) Applied() {
	_ = exec.Command(
		"/System/Library/PrivateFrameworks/SystemAdministration.framework/Resources/activateSettings",
		"-u",
	).Run()
}

// OS returns the settings mechanism of this OS.
func OS() (Store, string) { return Defaults{}, "defaults" }
