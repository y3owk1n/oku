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
	"unicode"
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

// Unavailable reports a machine that systemd does not run, such as a container
// or a distro with another init. systemd creates /run/systemd/system when it is
// the init, and a systemctl on PATH alone does not say that.
func (s *systemd) Unavailable() string {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return "systemd does not run this machine"
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return "the systemctl tool is not on PATH"
	}

	return ""
}

func (s *systemd) unit(d Definition) string { return "oku-" + d.Name + ".service" }

// timer is the unit that starts a job on its schedule.
func (s *systemd) timer(d Definition) string { return "oku-" + d.Name + ".timer" }

// enabler is the unit that enable and disable act on: the timer of a job, and
// the service itself otherwise.
func (s *systemd) enabler(d Definition) string {
	if d.Schedule != nil {
		return s.timer(d)
	}

	return s.unit(d)
}

func (s *systemd) File(d Definition) string { return filepath.Join(s.units, s.unit(d)) }

func (s *systemd) Install(ctx context.Context, d Definition, enabled bool) error {
	if err := os.MkdirAll(s.units, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(s.File(d), s.unitFile(d), 0o644); err != nil {
		return err
	}

	if d.Schedule != nil {
		if err := os.WriteFile(filepath.Join(s.units, s.timer(d)), s.timerFile(d), 0o644); err != nil {
			return err
		}
	}

	if err := s.ctl(ctx, "daemon-reload"); err != nil {
		return err
	}

	if !enabled {
		return s.ctl(ctx, "disable", "--now", s.enabler(d))
	}

	return s.ctl(ctx, "enable", "--now", s.enabler(d))
}

// Remove takes away the service and, for a job, its timer. Remove gets only
// the name, so it looks for a timer either way.
func (s *systemd) Remove(ctx context.Context, d Definition) error {
	timer := filepath.Join(s.units, s.timer(d))
	if _, err := os.Stat(timer); err == nil {
		_ = s.ctl(ctx, "disable", "--now", s.timer(d))
	}

	_ = s.ctl(ctx, "disable", "--now", s.unit(d))

	for _, path := range []string{s.File(d), timer} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return s.ctl(ctx, "daemon-reload")
}

// Start runs the program. A job runs once now, and oku does not wait for it to
// end.
func (s *systemd) Start(ctx context.Context, d Definition) error {
	if d.Schedule != nil {
		return s.ctl(ctx, "start", "--no-block", s.unit(d))
	}

	return s.ctl(ctx, "start", s.unit(d))
}

func (s *systemd) Stop(ctx context.Context, d Definition) error {
	return s.ctl(ctx, "stop", s.unit(d))
}

func (s *systemd) Status(ctx context.Context, d Definition) (Status, error) {
	var status Status

	_, err := os.Stat(s.File(d))
	status.Installed = err == nil

	enabled, _ := exec.CommandContext(ctx, "systemctl", s.scope, "is-enabled", s.enabler(d)).Output()
	status.Enabled = strings.TrimSpace(string(enabled)) == "enabled"

	pid, _ := exec.CommandContext(ctx, "systemctl", s.scope, "show", "--property=MainPID", "--value", s.unit(d)).
		Output()
	if n, _ := strconv.Atoi(strings.TrimSpace(string(pid))); n > 0 {
		status.Running = true
		status.Detail = "pid " + strconv.Itoa(n)
	}

	return status, nil
}

func (s *systemd) LogHint(d Definition) string {
	return "run \"journalctl " + s.scope + " -u " + s.unit(d) + "\""
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

	// systemd expands $NAME in ExecStart, and $$ is a plain dollar there.
	command := []string{strings.ReplaceAll(quoteUnit(d.Program), "$", "$$")}
	for _, arg := range d.Args {
		command = append(command, strings.ReplaceAll(quoteUnit(arg), "$", "$$"))
	}

	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(command, " "))

	restart := map[string]string{"always": "always", "on-failure": "on-failure"}[d.Restart]
	if restart == "" {
		restart = "no"
	}

	// A job runs once each time its timer starts it.
	if d.Schedule != nil {
		b.WriteString("Type=oneshot\n")
	}

	fmt.Fprintf(&b, "Restart=%s\n", restart)

	// A system unit runs as root unless it names its user. systemd unquotes
	// ExecStart and Environment, not User, which takes the name as it stands.
	// The name is the account oku runs as, which the system's user database
	// gave it.
	if d.User != "" {
		fmt.Fprintf(&b, "User=%s\n", d.User)
	}

	names := make([]string, 0, len(d.Env))
	for name := range d.Env {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		fmt.Fprintf(&b, "Environment=%s\n", quoteUnit(name+"="+d.Env[name]))
	}

	// A job has no [Install], since its timer is what starts it. A user manager
	// has no multi-user.target, and the system manager does not start
	// default.target's user units.
	if d.Schedule != nil {
		return []byte(b.String())
	}

	target := "default.target"
	if s.scope == "--system" {
		target = "multi-user.target"
	}

	fmt.Fprintf(&b, "\n[Install]\nWantedBy=%s\n", target)

	return []byte(b.String())
}

// timerFile renders the timer that starts the job d. With Persistent, systemd
// starts a run that the machine slept through once it wakes.
func (s *systemd) timerFile(d Definition) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "[Unit]\nDescription=%s on its schedule, installed by oku\n\n[Timer]\n", d.Name)

	if sch := d.Schedule; sch.Interval > 0 {
		seconds := int(sch.Interval.Seconds())
		fmt.Fprintf(&b, "OnActiveSec=%d\nOnUnitActiveSec=%d\n", seconds, seconds)
	} else {
		days := make([]string, len(sch.Days))
		for i, day := range sch.Days {
			days[i] = day.String()[:3]
		}

		calendar := strings.TrimSpace(strings.Join(days, ",") + fmt.Sprintf(" *-*-* %02d:%02d:00", sch.Hour, sch.Minute))
		fmt.Fprintf(&b, "OnCalendar=%s\nPersistent=true\n", calendar)
	}

	fmt.Fprintf(&b, "Unit=%s\n\n[Install]\nWantedBy=timers.target\n", s.unit(d))

	return []byte(b.String())
}

// quoteUnit quotes one word of a unit file. A control character becomes a
// \xNN escape, since a newline would end the line and start a directive of
// the manifest's choosing.
func quoteUnit(s string) string {
	var b strings.Builder

	for _, r := range strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) {
		if unicode.IsControl(r) && r < 0x80 {
			fmt.Fprintf(&b, `\x%02x`, r)
		} else {
			b.WriteRune(r)
		}
	}

	return `"` + b.String() + `"`
}
