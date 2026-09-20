// Package sandbox runs a build command with no network and without the user's
// home directory. macOS uses sandbox-exec and Linux uses namespaces. Other
// systems, and hosts where the mechanism is off, run the command unsandboxed.
package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
)

// InitCommand is the hidden oku subcommand that finishes the Linux sandbox from
// inside the new namespaces and then runs the build command.
const InitCommand = "sandbox-init"

// Spec describes one command to run.
type Spec struct {
	// Argv is the program and its arguments.
	Argv []string
	Dir  string
	Env  []string
	// Home is the user's real home directory, which the command must not read.
	Home string
	// Readable are paths the command may read even when they are inside Home, such
	// as the store and the directories of the needs tools.
	Readable []string
	// Writable are the only paths the command may write to.
	Writable []string
	// Network lets the command use the network. A step sets it with network = true.
	Network bool
}

// Command returns the command for spec. why is empty when the command is
// sandboxed. Otherwise it says in one sentence why this host cannot sandbox, and
// the command runs unsandboxed.
func Command(ctx context.Context, spec Spec) (cmd *exec.Cmd, why string) {
	spec.Home = resolve(spec.Home)
	spec.Dir = resolve(spec.Dir)

	for _, paths := range [][]string{spec.Readable, spec.Writable} {
		for i, path := range paths {
			paths[i] = resolve(path)
		}
	}

	cmd, why = command(ctx, spec)
	if cmd == nil {
		cmd = exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	}

	cmd.Dir = spec.Dir
	if cmd.Env == nil {
		cmd.Env = spec.Env
	}

	return cmd, why
}

// Available reports whether this host can sandbox, and why not when it cannot.
func Available() (bool, string) {
	why := unavailable()

	return why == "", why
}

// resolve follows symlinks, because both mechanisms match real paths. On macOS
// /tmp and /var are symlinks.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}

	return path
}
