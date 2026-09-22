package settings

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// Defaults is the preference domains of macOS, through /usr/bin/defaults. That
// tool goes through the preferences daemon, which caches a domain, so oku never
// edits the plist files themselves.
type Defaults struct{}

// run calls defaults with verb on domain. A domain that starts with CurrentHost
// is one of this Mac only.
func run(verb, domain string, rest ...string) ([]byte, error) {
	args := []string{verb, domain}

	if plain, thisMac := strings.CutPrefix(domain, CurrentHost); thisMac {
		args = []string{"-currentHost", verb, plain}
	}

	out, err := exec.Command("/usr/bin/defaults", append(args, rest...)...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"defaults %s %s: %w: %s",
			verb,
			domain,
			err,
			strings.TrimSpace(string(out)),
		)
	}

	return out, nil
}

func (Defaults) Unavailable() string { return "" }

func (Defaults) Encode(value any) (string, error) { return EncodePlist(value) }

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

// dockDomain holds the settings that the Dock reads only when it starts.
const dockDomain = "com.apple.dock"

// Applied makes the system read the settings again, so that they show without a
// logout. The Dock reads its own only when it starts, so oku stops it after a
// change of them, and launchd starts it again at once. nix-darwin does the same.
func (Defaults) Applied(domains []string) {
	_ = exec.Command(
		"/System/Library/PrivateFrameworks/SystemAdministration.framework/Resources/activateSettings",
		"-u",
	).Run()

	if slices.Contains(domains, dockDomain) {
		_ = exec.Command("/usr/bin/killall", "-q", "Dock").Run()
	}
}

// OS returns the settings mechanism of this OS.
func OS() (Store, string) { return Defaults{}, "defaults" }
