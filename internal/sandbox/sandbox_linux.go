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
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
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
		// A pid namespace keeps the build from seeing or signalling the user's
		// processes.
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWPID,
		// The build runs as the user, not as root of the namespace, so the exec of
		// its command drops every capability. Only the init process keeps the one
		// that mounting needs, and it clears that before it runs the command, so the
		// build cannot unmount what hides the user's files.
		UidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		AmbientCaps: []uintptr{unix.CAP_SYS_ADMIN},
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

	// The init process runs in a new network namespace, where it cannot list the
	// host's sockets.
	encoded, err := json.Marshal(initSpec{Spec: spec, Sockets: hostSockets()})
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

// initSpec is what the init process reads. Sockets are the paths of the unix
// sockets that listen on the host.
type initSpec struct {
	Spec
	Sockets []string
}

// hostSockets lists the path of every unix socket in /proc/net/unix. The path
// is the eighth column to the end of the line, and an abstract socket's starts
// with @.
func hostSockets() []string {
	data, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return nil
	}

	var sockets []string

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		if path := strings.Join(fields[7:], " "); strings.HasPrefix(path, "/") && !slices.Contains(sockets, path) {
			sockets = append(sockets, path)
		}
	}

	return sockets
}

// Init runs inside the new namespaces. It mounts a /proc of the new pid
// namespace, hides Home and the shared temporary directories behind empty
// tmpfs mounts, puts the readable and writable paths back, hides the sockets of
// the user's session and of the host, makes every mount read-only but the
// writable paths, and replaces itself with the build command.
func Init() error {
	var spec initSpec
	if err := json.Unmarshal([]byte(os.Getenv(specEnv)), &spec); err != nil {
		return fmt.Errorf("read the sandbox spec: %w", err)
	}

	// Mounts made here must not reach the host.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	// The kernel refuses a new /proc in a container that masks parts of its own.
	// The build then keeps the old /proc, where it sees the user's processes and
	// still cannot signal them.
	flags := uintptr(syscall.MS_NOSUID | syscall.MS_NODEV | syscall.MS_NOEXEC)
	if err := syscall.Mount("proc", "/proc", "proc", flags, ""); err != nil && !errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("mount /proc: %w", err)
	}

	keep := append(append([]string{}, spec.Readable...), spec.Writable...)

	if spec.Home != "" {
		if err := hide(spec.Home, keep, "mode=0700"); err != nil {
			return err
		}
	}

	// The user's ssh-agent and X server keep their sockets in the shared
	// temporary directories.
	for _, dir := range tempDirs {
		if err := hide(dir, keep, "mode=1777"); err != nil {
			return err
		}
	}

	// A build that reached the user's D-Bus or systemd could start a program
	// outside the sandbox.
	for _, dir := range sessionSockets {
		if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "mode=0755"); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("hide %s: %w", dir, err)
		}
	}

	// A build that reached Docker, the system D-Bus or another daemon could start
	// a program outside the sandbox. A connection to a socket that /dev/null covers
	// fails. The build cannot reach a socket in a directory the user cannot
	// search, so that socket needs no mount.
	for _, path := range spec.Sockets {
		if within(path, keep) || within(path, resolverSockets) {
			continue
		}

		if err := syscall.Mount("/dev/null", path, "", syscall.MS_BIND, ""); err != nil &&
			!errors.Is(err, fs.ErrNotExist) && !errors.Is(err, unix.EACCES) {
			return fmt.Errorf("hide the socket %s: %w", path, err)
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

	if err := ownTerminals(); err != nil {
		return err
	}

	if err := restrictSockets(spec.Network); err != nil {
		return err
	}

	if err := dropCapabilities(); err != nil {
		return err
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

// hide mounts an empty tmpfs with options over dir and binds the kept paths
// inside it back into place. A dir that does not exist needs no hiding.
func hide(dir string, keep []string, options string) error {
	type kept struct {
		fd   int
		path string
	}

	var inside []kept

	for _, path := range keep {
		if !within(path, []string{dir}) {
			continue
		}

		// The descriptor still reaches the directory after the tmpfs covers its path.
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		defer func() { _ = unix.Close(fd) }()

		inside = append(inside, kept{fd: fd, path: path})
	}

	if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, options); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("hide %s: %w", dir, err)
	}

	for _, k := range inside {
		if err := os.MkdirAll(k.path, 0o755); err != nil {
			return err
		}

		from := "/proc/self/fd/" + strconv.Itoa(k.fd)
		if err := syscall.Mount(from, k.path, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("restore %s: %w", k.path, err)
		}
	}

	return nil
}

// landlockResolveUnix is LANDLOCK_ACCESS_FS_RESOLVE_UNIX, which x/sys does not
// have yet.
const landlockResolveUnix = 1 << 16

// restrictSockets uses Landlock to keep the build from connecting to a socket
// that a process outside it listens on. Since Linux 7.1 it covers every path
// socket outside resolverSockets, also one that appears after the step starts.
// On an older kernel only the mounts over the host's sockets cover path sockets.
// Abstract sockets belong to the network namespace, so only a step with the
// network shares the host's, and Linux 6.12 and later scope those.
func restrictSockets(network bool) error {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return nil
	}

	var attr unix.LandlockRulesetAttr
	if network && abi >= 6 {
		attr.Scoped = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET
	}

	if abi >= 9 {
		attr.Access_fs = landlockResolveUnix
	}

	if attr == (unix.LandlockRulesetAttr{}) {
		return nil
	}

	fd, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0,
	)
	if errno != 0 {
		return fmt.Errorf("restrict sockets: %w", errno)
	}
	defer func() { _ = unix.Close(int(fd)) }()

	if attr.Access_fs != 0 {
		for _, dir := range resolverSockets {
			if err := allowSockets(int(fd), dir); err != nil {
				return err
			}
		}
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("restrict sockets: %w", err)
	}

	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("restrict sockets: %w", errno)
	}

	return nil
}

// allowSockets lets the build connect to the sockets under dir. A dir that
// does not exist needs no rule.
func allowSockets(ruleset int, dir string) error {
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil //nolint:nilerr
	}
	defer func() { _ = unix.Close(fd) }()

	rule := unix.LandlockPathBeneathAttr{Allowed_access: landlockResolveUnix, Parent_fd: int32(fd)}

	if _, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&rule)), 0, 0, 0,
	); errno != 0 {
		return fmt.Errorf("allow the sockets under %s: %w", dir, errno)
	}

	return nil
}

// dropCapabilities clears the ambient capability that the init process kept
// for its mounts, so the build command starts with none, and keeps the command
// from gaining any through a program with file capabilities. A user who runs
// oku as root is root in the namespace too, and root gains the bounding set at
// exec, so that set and the inheritable one empty as well.
func dropCapabilities() error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("drop capabilities: %w", err)
	}

	// The kernel answers EINVAL past its last capability, and EPERM to a process
	// that may not change the set, which is not root and gains nothing at exec.
	for c := uintptr(0); ; c++ {
		err := unix.Prctl(unix.PR_CAPBSET_DROP, c, 0, 0, 0)
		if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EPERM) {
			break
		}

		if err != nil {
			return fmt.Errorf("drop capabilities: %w", err)
		}
	}

	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}

	var none [2]unix.CapUserData
	if err := unix.Capset(&header, &none[0]); err != nil {
		return fmt.Errorf("drop capabilities: %w", err)
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("drop capabilities: %w", err)
	}

	return nil
}

// within reports whether path is one of dirs or inside one.
func within(path string, dirs []string) bool {
	for _, dir := range dirs {
		if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}

	return false
}

// resolverSockets are where nscd and systemd-resolved answer name lookups,
// which a step with the network needs. Neither the mounts over the host's
// sockets nor the Landlock rule covers them.
var resolverSockets = []string{"/run/nscd", "/var/run/nscd", "/run/systemd/resolve"}

// tempDirs are the temporary directories that every program of the host shares.
var tempDirs = []string{"/tmp", "/var/tmp"}

// sessionSockets are the directories that hold the sockets of the user's
// session, the user's D-Bus and systemd under /run/user.
var sessionSockets = []string{"/run/user"}

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

// ownTerminals hides the terminals of the user's session under /dev/pts, which
// belong to the user, so the build can neither read what the user types nor
// write to their screen. A new devpts instance still lets the build open
// terminals of its own. A kernel that refuses one gets an empty tmpfs.
func ownTerminals() error {
	err := syscall.Mount("devpts", "/dev/pts", "devpts", 0, "newinstance,ptmxmode=0666,mode=0620")
	if err == nil {
		err = syscall.Mount("/dev/pts/ptmx", "/dev/ptmx", "", syscall.MS_BIND, "")
	}

	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err := syscall.Mount("tmpfs", "/dev/pts", "tmpfs", 0, "mode=0755"); err != nil {
		return fmt.Errorf("hide /dev/pts: %w", err)
	}

	return nil
}
