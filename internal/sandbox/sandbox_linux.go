package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// specEnv carries the Spec to the init process.
const specEnv = "OKU_SANDBOX_SPEC"

var (
	probeOnce sync.Once
	probeWhy  string
)

// unavailable tries to start a process in new user, mount and network
// namespaces. Kernels and container runtimes can forbid that.
func unavailable() string {
	probeOnce.Do(func() {
		cmd := exec.Command("/bin/true")
		cmd.SysProcAttr = namespaces()

		if err := cmd.Run(); err != nil {
			probeWhy = "this host does not allow unprivileged user namespaces (" + err.Error() + ")"
		}
	})

	return probeWhy
}

func namespaces() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
		// Mounting needs root inside the namespace. That root maps to the real user,
		// so files are still created as that user.
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}
}

func command(ctx context.Context, spec Spec) (*exec.Cmd, string) {
	if why := unavailable(); why != "" {
		return nil, why
	}

	self, err := os.Executable()
	if err != nil {
		return nil, "oku cannot find its own binary: " + err.Error()
	}

	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, err.Error()
	}

	cmd := exec.CommandContext(ctx, self, InitCommand)
	cmd.SysProcAttr = namespaces()

	if spec.Network {
		cmd.SysProcAttr.Cloneflags &^= syscall.CLONE_NEWNET
	}

	// The init process reads the spec from its environment and drops the variable
	// before it runs the build command.
	cmd.Env = append(append([]string{}, spec.Env...), specEnv+"="+string(encoded))

	return cmd, ""
}

// Init runs inside the new namespaces. It hides Home behind an empty tmpfs,
// puts the readable and writable paths back, and replaces itself with the build
// command.
func Init() error {
	var spec Spec
	if err := json.Unmarshal([]byte(os.Getenv(specEnv)), &spec); err != nil {
		return fmt.Errorf("read the sandbox spec: %w", err)
	}

	// Mounts made here must not reach the host.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	if spec.Home != "" {
		if err := hide(
			spec.Home,
			append(append([]string{}, spec.Readable...), spec.Writable...),
		); err != nil {
			return err
		}
	}

	env := make([]string, 0, len(spec.Env))
	for _, kv := range spec.Env {
		if !strings.HasPrefix(kv, specEnv+"=") {
			env = append(env, kv)
		}
	}

	program, err := exec.LookPath(spec.Argv[0])
	if err != nil {
		return err
	}

	if err := os.Chdir(spec.Dir); err != nil {
		return err
	}

	return syscall.Exec(program, spec.Argv, env)
}

// hide mounts an empty tmpfs over home and binds the kept paths inside it back
// into place.
func hide(home string, keep []string) error {
	stash, err := os.MkdirTemp("", "oku-sandbox-")
	if err != nil {
		return err
	}

	type kept struct{ at, path string }

	var inside []kept

	for i, path := range keep {
		rel, err := filepath.Rel(home, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}

		at := filepath.Join(stash, fmt.Sprint(i))
		if err := os.Mkdir(at, 0o700); err != nil {
			return err
		}

		if err := syscall.Mount(path, at, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("keep %s: %w", path, err)
		}

		inside = append(inside, kept{at: at, path: path})
	}

	if err := syscall.Mount("tmpfs", home, "tmpfs", 0, "mode=0700"); err != nil {
		return fmt.Errorf("hide %s: %w", home, err)
	}

	for _, k := range inside {
		if err := os.MkdirAll(k.path, 0o755); err != nil {
			return err
		}

		if err := syscall.Mount(k.at, k.path, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("restore %s: %w", k.path, err)
		}
	}

	return nil
}
