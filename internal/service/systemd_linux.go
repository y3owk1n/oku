package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// systemd manages user units. A unit file is always in the user's unit
// directory. "enable" decides whether it starts at login.
type systemd struct {
	units string
	// scope is "--user", or "--system" for units that run as root.
	scope string
}

// New returns the service manager for this OS. home is the user's home
// directory and dataDir is oku's data directory.
func New(home, _ string) Manager {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}

	return &systemd{units: filepath.Join(config, "systemd", "user"), scope: "--user"}
}

// SystemLogDir is empty because the journal holds a system service's output.
func SystemLogDir() string { return "" }

// NewSystem returns the manager for services that run as root for the whole
// machine. Everything but Status and Logs needs root.
func NewSystem() Manager {
	return &systemd{units: "/etc/systemd/system", scope: "--system"}
}

func (s *systemd) unit(d Definition) string { return "oku-" + d.Name + ".service" }

func (s *systemd) File(d Definition) string { return filepath.Join(s.units, s.unit(d)) }

func (s *systemd) Install(ctx context.Context, d Definition, enabled bool) error {
	if err := os.MkdirAll(s.units, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(s.File(d), s.unitFile(d), 0o644); err != nil {
		return err
	}

	if err := s.ctl(ctx, "daemon-reload"); err != nil {
		return err
	}

	if !enabled {
		return s.ctl(ctx, "disable", "--now", s.unit(d))
	}

	return s.ctl(ctx, "enable", "--now", s.unit(d))
}

func (s *systemd) Remove(ctx context.Context, d Definition) error {
	_ = s.ctl(ctx, "disable", "--now", s.unit(d))

	if err := os.Remove(s.File(d)); err != nil && !os.IsNotExist(err) {
		return err
	}

	return s.ctl(ctx, "daemon-reload")
}

func (s *systemd) Start(ctx context.Context, d Definition) error {
	return s.ctl(ctx, "start", s.unit(d))
}

func (s *systemd) Stop(ctx context.Context, d Definition) error {
	return s.ctl(ctx, "stop", s.unit(d))
}

func (s *systemd) Status(ctx context.Context, d Definition) (Status, error) {
	var status Status

	_, err := os.Stat(s.File(d))
	status.Installed = err == nil

	enabled, _ := exec.CommandContext(ctx, "systemctl", s.scope, "is-enabled", s.unit(d)).Output()
	status.Enabled = strings.TrimSpace(string(enabled)) == "enabled"

	pid, _ := exec.CommandContext(ctx, "systemctl", s.scope, "show", "--property=MainPID", "--value", s.unit(d)).
		Output()
	if n, _ := strconv.Atoi(strings.TrimSpace(string(pid))); n > 0 {
		status.Running = true
		status.Detail = "pid " + strconv.Itoa(n)
	}

	return status, nil
}

func (s *systemd) Logs(ctx context.Context, d Definition, lines int) (string, error) {
	args := []string{
		s.scope,
		"--no-pager",
		"-o",
		"cat",
		"-n",
		strconv.Itoa(lines),
		"-u",
		s.unit(d),
	}

	out, err := exec.CommandContext(ctx, "journalctl", args...).CombinedOutput()
	if err != nil {
		return "", commandError("journalctl", args, out, err)
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func (s *systemd) ctl(ctx context.Context, args ...string) error {
	args = append([]string{s.scope}, args...)

	if out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput(); err != nil {
		return commandError("systemctl", args, out, err)
	}

	return nil
}

// unitFile renders the systemd unit for d.
func (s *systemd) unitFile(d Definition) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "[Unit]\nDescription=%s, installed by oku\n\n[Service]\n", d.Name)

	command := []string{quoteUnit(d.Program)}
	for _, arg := range d.Args {
		command = append(command, quoteUnit(arg))
	}

	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(command, " "))

	restart := map[string]string{"always": "always", "on-failure": "on-failure"}[d.Restart]
	if restart == "" {
		restart = "no"
	}

	fmt.Fprintf(&b, "Restart=%s\n", restart)

	names := make([]string, 0, len(d.Env))
	for name := range d.Env {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		fmt.Fprintf(&b, "Environment=%s\n", quoteUnit(name+"="+d.Env[name]))
	}

	// A user manager has no multi-user.target, and the system manager does not
	// start default.target's user units.
	target := "default.target"
	if s.scope == "--system" {
		target = "multi-user.target"
	}

	fmt.Fprintf(&b, "\n[Install]\nWantedBy=%s\n", target)

	return []byte(b.String())
}

// quoteUnit quotes one word of a unit file.
func quoteUnit(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
}
