package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/y3owk1n/oku/internal/tempdir"
)

// specEnv carries the Spec to the init process.
const specEnv = "OKU_SANDBOX_SPEC"

var (
	probeOnce sync.Once
	probeWhy  string
)

// unavailable tries to start a process in new user, mount and network
// namespaces. Kernels and container runtimes can forbid that.
// unavailable runs the real sandbox setup once with a command that does nothing.
// A host that lets oku create the namespaces can still refuse the setup. Ubuntu
// 24.04 lets an unprivileged process create a user namespace and then denies it
// every mount inside it.
func unavailable() string {
	probeOnce.Do(func() {
		self, err := os.Executable()
		if err != nil {
			probeWhy = "oku cannot find its own binary: " + err.Error()

			return
		}

		encoded, err := json.Marshal(Spec{Argv: []string{"/bin/true"}, Dir: "/"})
		if err != nil {
			probeWhy = err.Error()

			return
		}

		cmd := exec.Command(self, InitCommand)
		cmd.SysProcAttr = namespaces()
		cmd.Env = []string{specEnv + "=" + string(encoded)}

		if out, err := cmd.CombinedOutput(); err != nil {
			probeWhy = "this host does not let an unprivileged user set up namespaces (" +
				strings.TrimSpace(err.Error()+" "+string(out)) + ")"
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
// puts the readable and writable paths back, hides the sockets of the user's
// session, makes every mount read-only but the writable paths, and replaces
// itself with the build command.
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

	// A build that reached the user's D-Bus or X server could start a program
	// outside the sandbox. Abstract sockets belong to the network namespace, so
	// the build cannot reach those either.
	for _, dir := range sessionSockets {
		if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "mode=0755"); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("hide %s: %w", dir, err)
		}
	}

	if err := readOnly(spec.Writable); err != nil {
		return err
	}

	// Python and others need shared memory, and a build must not leave files in
	// the one the host uses.
	if err := syscall.Mount("tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777"); err != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("give the build its own /dev/shm: %w", err)
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
	stash, err := tempdir.Dir("sandbox")
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

// sessionSockets are the directories that hold the sockets of the user's
// session: the user's D-Bus and systemd under /run/user, and the X server.
var sessionSockets = []string{"/run/user", "/tmp/.X11-unix"}

// readOnly makes every mount read-only, then binds each writable path onto
// itself and makes that bind writable again.
func readOnly(writable []string) error {
	if err := setReadOnly("/", true, true); err != nil {
		return err
	}

	for _, path := range writable {
		if err := syscall.Mount(path, path, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("keep %s writable: %w", path, err)
		}

		if err := setReadOnly(path, false, false); err != nil {
			return err
		}
	}

	return nil
}

// setReadOnly sets or clears the read-only flag of the mount at path, and with
// recursive of every mount below it. Linux 5.12 added mount_setattr. An older
// kernel remounts each mount, which has to repeat the flags it keeps.
func setReadOnly(path string, on, recursive bool) error {
	attr := unix.MountAttr{}
	if on {
		attr.Attr_set = unix.MOUNT_ATTR_RDONLY
	} else {
		attr.Attr_clr = unix.MOUNT_ATTR_RDONLY
	}

	flags := uint(0)
	if recursive {
		flags = unix.AT_RECURSIVE
	}

	err := unix.MountSetattr(unix.AT_FDCWD, path, flags, &attr)
	if !errors.Is(err, unix.ENOSYS) {
		if err != nil {
			return fmt.Errorf("change the read-only flag of %s: %w", path, err)
		}

		return nil
	}

	mounts := []string{path}
	if recursive {
		var listErr error
		if mounts, listErr = mountsUnder(path); listErr != nil {
			return listErr
		}
	}

	for _, mount := range mounts {
		if err := remount(mount, on); err != nil {
			return fmt.Errorf("change the read-only flag of %s: %w", mount, err)
		}
	}

	return nil
}

// remount changes the read-only flag of one mount. The kernel refuses a remount
// in a user namespace that drops a flag the mount had, so it keeps them.
func remount(mount string, readOnly bool) error {
	var st unix.Statfs_t
	if err := unix.Statfs(mount, &st); err != nil {
		// A mount under another mount, such as one below the hidden home, cannot
		// be reached from the build, so it needs no change.
		return nil //nolint:nilerr
	}

	// The ST_ flags of statfs have the values of the MS_ flags of mount.
	kept := uintptr(st.Flags) & (unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC |
		unix.MS_NOATIME | unix.MS_NODIRATIME | unix.MS_RELATIME)

	flags := syscall.MS_REMOUNT | syscall.MS_BIND | kept
	if readOnly {
		flags |= syscall.MS_RDONLY
	}

	return syscall.Mount("", mount, "", flags, "")
}

// mountsUnder lists the mount points at or below path, parents first.
func mountsUnder(path string) ([]string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var mounts []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}

		// mountinfo escapes a space in a path as \040.
		mount := strings.ReplaceAll(fields[4], `\040`, " ")
		if rel, err := filepath.Rel(path, mount); err == nil && !strings.HasPrefix(rel, "..") {
			mounts = append(mounts, mount)
		}
	}

	return mounts, scanner.Err()
}
