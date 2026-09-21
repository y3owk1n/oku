// Package service runs a package's long-running programs under the OS's own
// service manager: launchd on macOS and systemd on Linux.
package service

import (
	"context"
	"fmt"
	"strings"
)

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
