package settings

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Registry is the registry of the current user, through reg.exe.
type Registry struct{}

func reg(args ...string) (string, error) {
	out, err := exec.Command("reg", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("reg %s: %w: %s", args[0], err, bytes.TrimSpace(out))
	}

	return string(out), nil
}

func (Registry) Unavailable() string { return "" }

func (Registry) Encode(value any) (string, error) { return EncodeRegistry(value) }

func (Registry) Read(key, name string) (string, bool, error) {
	// reg.exe reports a missing value only in the language of the Windows install,
	// so oku cannot tell it from another failure by its text.
	out, err := reg("query", key, "/v", name)
	if err != nil {
		return "", false, nil
	}

	fragment, found := FindRegistry(out, name)

	return fragment, found, nil
}

func (Registry) Write(key, name, fragment string) error {
	kind, data, _ := strings.Cut(fragment, ":")

	_, err := reg("add", key, "/v", name, "/t", kind, "/d", data, "/f")

	return err
}

func (r Registry) Delete(key, name string) error {
	if _, set, _ := r.Read(key, name); !set {
		return nil
	}

	_, err := reg("delete", key, "/v", name, "/f")

	return err
}

// Applied does nothing. A program reads the registry when it needs a value.
func (Registry) Applied([]string) {}

// OS returns the settings mechanism of this OS.
func OS() (Store, string) { return Registry{}, "registry" }
