// Package shim is the Windows stand-in for a symlink in a profile's bin. A shim
// is a copy of oku.exe under the program's name, beside a "<name>.shim" file that
// says what to run. Windows users cannot create symlinks without extra rights.
package shim

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

// Ext is the ending of the file that describes a shim.
const Ext = ".shim"

// Spec is what a shim runs.
type Spec struct {
	// Target is the program in the store.
	Target string
	// Dirs go to the front of PATH, so that Windows finds the DLLs of the
	// package's deps from any working directory.
	Dirs []string
}

// sidecar returns the spec file of the shim at executable.
func sidecar(executable string) string {
	return strings.TrimSuffix(executable, filepath.Ext(executable)) + Ext
}

// Write saves spec for the shim at executable.
func Write(executable string, spec Spec) error {
	lines := []string{"path = " + spec.Target}
	for _, dir := range spec.Dirs {
		lines = append(lines, "dir = "+dir)
	}

	return os.WriteFile(sidecar(executable), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func read(executable string) (Spec, error) {
	var spec Spec

	data, err := os.ReadFile(sidecar(executable))
	if err != nil {
		return spec, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if !ok {
			continue
		}

		switch key {
		case "path":
			spec.Target = value
		case "dir":
			spec.Dirs = append(spec.Dirs, value)
		}
	}

	if spec.Target == "" {
		return spec, fmt.Errorf("%s names no path", sidecar(executable))
	}

	return spec, nil
}

// Run acts as the shim at executable when a spec file is beside it. It passes
// the arguments, stdio and the exit code through. handled is false when
// executable is not a shim, and then the caller carries on as oku.
func Run(executable string, args []string) (code int, handled bool) {
	spec, err := read(executable)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "oku shim:", err)

		return 1, true
	}

	cmd := exec.Command(spec.Target, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if len(spec.Dirs) > 0 {
		path := strings.Join(append(spec.Dirs, os.Getenv("PATH")), string(os.PathListSeparator))
		cmd.Env = append(os.Environ(), "PATH="+path)
	}

	// Ctrl-C reaches the program too. The shim ignores it and waits for the
	// program to exit.
	signal.Ignore(os.Interrupt)

	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) {
		return exit.ExitCode(), true
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "oku shim: run %s: %v\n", spec.Target, err)

		return 1, true
	}

	return 0, true
}
