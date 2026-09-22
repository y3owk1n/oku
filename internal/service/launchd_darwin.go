package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// launchd manages per-user agents. An enabled service's plist is in
// ~/Library/LaunchAgents, which launchd loads at login. A service that is only
// installed keeps its plist in oku's data directory, which launchd does not
// read, so it stays stopped until "oku service start" loads it for this session.
type launchd struct {
	agents  string
	holding string
	// domain is the launchd domain, "gui/<uid>" for a user and "system" for the
	// whole machine.
	domain string
}

// New returns the service manager for this OS. home is the user's home
// directory and dataDir is oku's data directory.
func New(home, dataDir string) Manager {
	return &launchd{
		agents:  filepath.Join(home, "Library", "LaunchAgents"),
		holding: filepath.Join(dataDir, "services"),
		domain:  "gui/" + strconv.Itoa(os.Getuid()),
	}
}

// SystemLogDir is where a system service's output goes.
func SystemLogDir() string { return "/Library/Logs/oku" }

// NewSystem returns the manager for services that run as root for the whole
// machine. Everything but Status and Logs needs root.
func NewSystem() Manager {
	return &launchd{
		agents:  "/Library/LaunchDaemons",
		holding: "/Library/Application Support/oku/services",
		domain:  "system",
	}
}

func (l *launchd) File(d Definition) string {
	return filepath.Join(l.agents, d.Label()+".plist")
}

func (l *launchd) held(d Definition) string {
	return filepath.Join(l.holding, d.Label()+".plist")
}

// Unavailable is empty, because every Mac has launchd.
func (l *launchd) Unavailable() string { return "" }

func (l *launchd) Install(ctx context.Context, d Definition, enabled bool) error {
	_ = l.bootout(ctx, d)

	os.Remove(l.File(d))
	os.Remove(l.held(d))

	path := l.held(d)
	if enabled {
		path = l.File(d)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(d.LogFile), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(path, plist(d), 0o644); err != nil {
		return err
	}

	if !enabled {
		return nil
	}

	return l.bootstrap(ctx, path)
}

func (l *launchd) Remove(ctx context.Context, d Definition) error {
	_ = l.bootout(ctx, d)

	for _, path := range []string{l.File(d), l.held(d)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if l.domain != "system" {
		return nil
	}

	// A user service's log and holding directory are inside oku's data directory.
	// In system scope they are not, so oku deletes them here. Remove fails on a
	// directory that another service still uses, which is what oku wants.
	os.Remove(filepath.Join(SystemLogDir(), d.Name+".log"))
	os.Remove(SystemLogDir())
	os.Remove(l.holding)
	os.Remove(filepath.Dir(l.holding))

	return nil
}

func (l *launchd) Start(ctx context.Context, d Definition) error {
	if status, _ := l.Status(ctx, d); status.Running {
		return nil
	}

	_ = l.bootout(ctx, d)

	for _, path := range []string{l.File(d), l.held(d)} {
		if _, err := os.Stat(path); err == nil {
			return l.bootstrap(ctx, path)
		}
	}

	return fmt.Errorf("%s is not installed", d.Name)
}

func (l *launchd) Stop(ctx context.Context, d Definition) error {
	return l.bootout(ctx, d)
}

func (l *launchd) Status(ctx context.Context, d Definition) (Status, error) {
	var status Status

	_, enabledErr := os.Stat(l.File(d))
	_, heldErr := os.Stat(l.held(d))
	status.Enabled = enabledErr == nil
	status.Installed = status.Enabled || heldErr == nil

	out, err := exec.CommandContext(ctx, "/bin/launchctl", "print", l.domain+"/"+d.Label()).
		Output()
	if err != nil {
		return status, nil
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)

		if pid, ok := strings.CutPrefix(line, "pid = "); ok {
			status.Running = true
			status.Detail = "pid " + pid
		}
	}

	return status, nil
}

func (l *launchd) LogHint(d Definition) string { return "look at " + d.LogFile }

func (l *launchd) Logs(_ context.Context, d Definition, lines int) (string, error) {
	data, err := os.ReadFile(d.LogFile)
	if os.IsNotExist(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	return strings.Join(all[max(0, len(all)-lines):], "\n"), nil
}

func (l *launchd) bootstrap(ctx context.Context, path string) error {
	args := []string{"bootstrap", l.domain, path}

	if out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).
		CombinedOutput(); err != nil {
		return commandError("launchctl", args, out, err)
	}

	return nil
}

// bootout unloads the service. launchd reports an error for a service that is
// not loaded, which is not a failure here.
//
// launchctl returns while the program still runs. Until the program has exited,
// a bootstrap of the same label fails with "Input/output error". bootout
// therefore waits until launchd has unloaded the label.
func (l *launchd) bootout(ctx context.Context, d Definition) error {
	target := l.domain + "/" + d.Label()
	args := []string{"bootout", target}

	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if err != nil && !bytes.Contains(out, []byte("No such process")) &&
		!bytes.Contains(out, []byte("Could not find service")) {
		return commandError("launchctl", args, out, err)
	}

	deadline := time.Now().Add(bootoutWait)

	for exec.CommandContext(ctx, "/bin/launchctl", "print", target).Run() == nil {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is still loaded %s after launchctl bootout", target, bootoutWait)
		}

		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// bootoutWait is how long a service gets to exit. launchd kills a program that
// ignores SIGTERM after 20 seconds by default.
const bootoutWait = 30 * time.Second

// plist renders the launchd property list for d.
func plist(d Definition) []byte {
	var b bytes.Buffer

	esc := func(s string) string {
		var out bytes.Buffer

		_ = xml.EscapeText(&out, []byte(s))

		return out.String()
	}

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", esc(d.Label()))
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")

	for _, arg := range append([]string{d.Program}, d.Args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", esc(arg))
	}

	b.WriteString("\t</array>\n\t<key>RunAtLoad</key>\n\t<true/>\n")

	switch d.Restart {
	case "always":
		b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	case "on-failure":
		b.WriteString(
			"\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n",
		)
	}

	if len(d.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")

		names := make([]string, 0, len(d.Env))
		for name := range d.Env {
			names = append(names, name)
		}

		slices.Sort(names)

		for _, name := range names {
			fmt.Fprintf(
				&b,
				"\t\t<key>%s</key>\n\t\t<string>%s</string>\n",
				esc(name),
				esc(d.Env[name]),
			)
		}

		b.WriteString("\t</dict>\n")
	}

	fmt.Fprintf(&b, "\t<key>StandardOutPath</key>\n\t<string>%s</string>\n", esc(d.LogFile))
	fmt.Fprintf(&b, "\t<key>StandardErrorPath</key>\n\t<string>%s</string>\n", esc(d.LogFile))
	b.WriteString("</dict>\n</plist>\n")

	return b.Bytes()
}
