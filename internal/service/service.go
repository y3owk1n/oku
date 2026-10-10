// Package service runs a package's long-running programs and scheduled jobs
// under the OS's own service manager: launchd on macOS, systemd on Linux and
// the Task Scheduler on Windows.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StartWait is how long "oku service start" and "restart" wait before they
// check that the program still runs, so a program that exits right after start
// is reported as exited and not as running.
const StartWait = time.Second

// Definition is one service of an installed package.
type Definition struct {
	// Name is the service's name, unique among the user's packages.
	Name string
	// Program is the absolute path of the executable in the store.
	Program string
	Args    []string
	Env     map[string]string
	// Restart is "never", "on-failure" or "always".
	Restart string
	// LogFile is where the service's output goes on systems without a journal.
	LogFile string
	// User is the account a system service runs as. Empty runs it as root, or
	// as SYSTEM on Windows. A user service runs as its user either way.
	User string
	// Schedule makes the service a job that runs at set times and ends on its
	// own. It is nil for a program that runs until it is stopped.
	Schedule *Schedule `json:",omitempty"`
}

// Schedule is when a job runs: every Interval, or at Hour and Minute local time
// each day, or on Days only.
type Schedule struct {
	Interval     time.Duration
	Hour, Minute int
	Days         []time.Weekday
}

// String says when s runs, such as "every 15m" or "at 03:00 on mon, fri".
func (s Schedule) String() string {
	if s.Interval > 0 {
		if s.Interval%time.Hour == 0 {
			return fmt.Sprintf("every %dh", s.Interval/time.Hour)
		}

		return fmt.Sprintf("every %dm", s.Interval/time.Minute)
	}

	at := fmt.Sprintf("at %02d:%02d", s.Hour, s.Minute)
	if len(s.Days) == 0 {
		return "daily " + at
	}

	days := make([]string, len(s.Days))
	for i, day := range s.Days {
		days[i] = strings.ToLower(day.String()[:3])
	}

	return at + " on " + strings.Join(days, ", ")
}

// Label is the name the OS knows the service by.
func (d Definition) Label() string {
	return "dev.oku." + d.Name
}

// Status is what a manager knows about a service.
type Status struct {
	// Installed reports that the OS has the service's definition.
	Installed bool
	// Enabled reports that it starts at login.
	Enabled bool
	Running bool
	// Detail is the manager's own one-line description, such as a pid.
	Detail string
}

// Manager installs and controls services for the current user. Tests replace it
// with a fake, because a real one changes the user's login session.
type Manager interface {
	// Unavailable says why this machine cannot run services, or is empty. oku
	// then installs no service and says so once.
	Unavailable() string
	// Install writes the definition where the OS reads it. With enabled it also
	// starts the service now and at every login.
	Install(ctx context.Context, d Definition, enabled bool) error
	// Remove stops the service and deletes its definition.
	Remove(ctx context.Context, d Definition) error
	Start(ctx context.Context, d Definition) error
	Stop(ctx context.Context, d Definition) error
	Status(ctx context.Context, d Definition) (Status, error)
	// Logs returns the last lines of the service's output.
	Logs(ctx context.Context, d Definition, lines int) (string, error)
	// File returns the definition file the manager writes for d.
	File(d Definition) string
	// LogHint says where to look when d exits right after start, such as its
	// log file or a journalctl command. It is empty when the OS keeps no log.
	LogHint(d Definition) string
}

func commandError(name string, args []string, out []byte, err error) error {
	return fmt.Errorf(
		"%s %s: %w: %s",
		name,
		strings.Join(args, " "),
		err,
		strings.TrimSpace(string(out)),
	)
}
