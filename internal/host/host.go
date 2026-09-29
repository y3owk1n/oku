// Package host checks what a machine must have that oku does not install, such
// as the Xcode command line tools or a package of the Linux distribution.
package host

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/platform"
)

// Managers are the Linux package managers a requirement can name a package
// for, each with the command that installs a package.
var Managers = map[string]string{
	"apt":    "sudo apt-get install",
	"dnf":    "sudo dnf install",
	"pacman": "sudo pacman -S",
	"apk":    "sudo apk add",
	"zypper": "sudo zypper install",
}

// Requirement is one entry of [host]: something the machine must have.
type Requirement struct {
	Name string `toml:"name"`
	// Package names the package whose manifest has the requirement, or is empty
	// for one of the list.
	Package string `toml:"package,omitempty"`
	// Command is a program that must be on PATH.
	Command string `toml:"command,omitempty"`
	// Path is a file that must exist.
	Path string `toml:"path,omitempty"`
	// Packages maps a package manager of Managers to the name of the package
	// that must be installed, when the machine uses that manager.
	Packages map[string]string `toml:"packages,omitempty"`
	// Install tells the user how to get it.
	Install string `toml:"install,omitempty"`
	// When limits the requirement to matching platforms. The empty When matches
	// all.
	When platform.When `toml:"-"`
}

// Parse reads the entry name of [host].
func Parse(name string, value any) (Requirement, error) {
	table, ok := value.(map[string]any)
	if !ok {
		return Requirement{}, fmt.Errorf("host.%s wants a table", name)
	}

	r := Requirement{Name: name}

	for key, v := range table {
		if key == "when" {
			w, err := platform.ParseWhen(v)
			if err != nil {
				return Requirement{}, fmt.Errorf("host.%s: %w", name, err)
			}

			r.When = w

			continue
		}

		text, ok := v.(string)
		if !ok || text == "" {
			return Requirement{}, fmt.Errorf("host.%s.%s wants a string", name, key)
		}

		switch key {
		case "command":
			r.Command = text
		case "path":
			r.Path = text
		case "install":
			r.Install = text
		default:
			if _, known := Managers[key]; !known {
				return Requirement{}, fmt.Errorf(
					"host.%s.%s is not a key of [host], use command, path, apt, dnf, pacman, apk, zypper, install or when",
					name, key,
				)
			}

			if r.Packages == nil {
				r.Packages = map[string]string{}
			}

			r.Packages[key] = text
		}
	}

	if r.Command == "" && r.Path == "" && len(r.Packages) == 0 {
		return Requirement{}, fmt.Errorf(
			"host.%s names nothing to check, give it a command, a path or a package", name,
		)
	}

	return r, nil
}

// System is the package manager of a machine and how to query it.
type System struct {
	// Manager is the package manager of the Linux distribution, a key of
	// Managers, or empty.
	Manager string
	// Installed reports whether the package manager has the package installed.
	Installed func(pkg string) (bool, error)
}

// Detect finds the package manager of this machine from /etc/os-release.
func Detect() System {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return System{}
	}

	var ids []string

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), "=")
		if key == "ID" || key == "ID_LIKE" {
			ids = append(ids, strings.Fields(strings.Trim(value, `"'`))...)
		}
	}

	for _, id := range ids {
		switch {
		case slices.Contains([]string{"debian", "ubuntu"}, id):
			return System{Manager: "apt", Installed: query("dpkg-query", "-W", "-f=${Status}")}
		case slices.Contains([]string{"fedora", "rhel", "centos"}, id):
			return System{Manager: "dnf", Installed: query("rpm", "-q")}
		case id == "arch":
			return System{Manager: "pacman", Installed: query("pacman", "-Q")}
		case id == "alpine":
			return System{Manager: "apk", Installed: query("apk", "info", "-e")}
		case strings.HasPrefix(id, "opensuse") || id == "suse":
			return System{Manager: "zypper", Installed: query("rpm", "-q")}
		}
	}

	return System{}
}

// query runs a program of the package manager with the package's name last. A
// package is installed when it exits with 0, and for dpkg-query when its status
// also says so.
func query(program string, args ...string) func(string) (bool, error) {
	return func(pkg string) (bool, error) {
		out, err := exec.Command(program, append(args, pkg)...).Output()

		if _, exited := errors.AsType[*exec.ExitError](err); exited {
			return false, nil
		}

		if err != nil {
			return false, fmt.Errorf("check the package %s with %s: %w", pkg, program, err)
		}

		if program == "dpkg-query" {
			return strings.HasSuffix(strings.TrimSpace(string(out)), " installed"), nil
		}

		return true, nil
	}
}

// Missing is a requirement the machine does not meet, with how to meet it.
type Missing struct {
	Requirement
	// Fix is the command or the text that gets it, or empty.
	Fix string
	// Command is set when Fix is the package manager's command.
	Command bool
	// Unchecked is set when the requirement names a package only for other
	// package managers, so oku could not check it here.
	Unchecked bool
}

// Check returns the requirements that the machine does not meet.
func (s System) Check(reqs []Requirement) ([]Missing, error) {
	var missing []Missing

	for _, r := range reqs {
		met := true

		if r.Command != "" {
			if _, err := exec.LookPath(r.Command); err != nil {
				met = false
			}
		}

		if r.Path != "" {
			if _, err := os.Stat(r.Path); err != nil {
				met = false
			}
		}

		pkg, named := r.Packages[s.Manager]
		unchecked := len(r.Packages) > 0 && !named && r.Command == "" && r.Path == ""

		if named && s.Installed != nil {
			ok, err := s.Installed(pkg)
			if err != nil {
				return nil, err
			}

			met = met && ok
		}

		if met && !unchecked {
			continue
		}

		fix := r.Install
		if named {
			fix = Managers[s.Manager] + " " + pkg
		}

		missing = append(missing, Missing{Requirement: r, Fix: fix, Command: named, Unchecked: unchecked})
	}

	return missing, nil
}
