package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// RunCommand is the hidden oku subcommand that a scheduled task starts. A task
// can neither set environment variables nor send output to a file, so oku does
// both and then runs the service's program.
const RunCommand = "service-run"

// SystemLogDir is unused on Windows, where system scope is not built yet.
const SystemLogDir = ""

// taskScheduler runs a service as a scheduled task of the current user. An
// enabled service has a logon trigger. One that is only installed has no
// trigger, so it runs when "oku service start" asks for it.
type taskScheduler struct {
	// dir holds one JSON file per service, which "oku service-run" reads.
	dir string
}

// New returns the service manager for this OS. dataDir is oku's data directory.
func New(_, dataDir string) Manager {
	return &taskScheduler{dir: filepath.Join(dataDir, "services")}
}

// NewSystem returns the manager for services that run for the whole machine.
func NewSystem() Manager { return unsupported{} }

// Stored is what "oku service-run" needs to start a service.
type Stored struct {
	Definition Definition
	Enabled    bool
}

func (t *taskScheduler) task(d Definition) string { return "oku-" + d.Name }

func (t *taskScheduler) File(d Definition) string {
	return filepath.Join(t.dir, d.Name+".json")
}

// ReadStored loads the definition that Install saved at path.
func ReadStored(path string) (Stored, error) {
	var stored Stored

	data, err := os.ReadFile(path)
	if err != nil {
		return stored, err
	}

	return stored, json.Unmarshal(data, &stored)
}

func (t *taskScheduler) Install(ctx context.Context, d Definition, enabled bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	owner, err := user.Current()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(d.LogFile), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(Stored{Definition: d, Enabled: enabled}, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(t.File(d), data, 0o644); err != nil {
		return err
	}

	definition := filepath.Join(t.dir, d.Name+".xml")
	if err := os.WriteFile(
		definition,
		utf16File(taskXML(d, self, t.File(d), owner.Username, enabled)),
		0o644,
	); err != nil {
		return err
	}
	defer os.Remove(definition)

	_ = t.schtasks(ctx, "/End", "/TN", t.task(d))

	if err := t.schtasks(ctx, "/Create", "/TN", t.task(d), "/XML", definition, "/F"); err != nil {
		return err
	}

	if !enabled {
		return nil
	}

	return t.Start(ctx, d)
}

func (t *taskScheduler) Remove(ctx context.Context, d Definition) error {
	if t.exists(ctx, d) {
		_ = t.schtasks(ctx, "/End", "/TN", t.task(d))

		if err := t.schtasks(ctx, "/Delete", "/TN", t.task(d), "/F"); err != nil {
			return err
		}
	}

	if err := os.Remove(t.File(d)); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (t *taskScheduler) Start(ctx context.Context, d Definition) error {
	return t.schtasks(ctx, "/Run", "/TN", t.task(d))
}

func (t *taskScheduler) Stop(ctx context.Context, d Definition) error {
	return t.schtasks(ctx, "/End", "/TN", t.task(d))
}

func (t *taskScheduler) exists(ctx context.Context, d Definition) bool {
	return exec.CommandContext(ctx, "schtasks", "/Query", "/TN", t.task(d)).Run() == nil
}

// Status asks PowerShell for the task's state, because its names are the same
// in every language and the text that schtasks prints is not.
func (t *taskScheduler) Status(ctx context.Context, d Definition) (Status, error) {
	var status Status

	stored, err := ReadStored(t.File(d))
	if err != nil || !t.exists(ctx, d) {
		return status, nil //nolint:nilerr
	}

	status.Installed, status.Enabled = true, stored.Enabled

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-ScheduledTask -TaskName $env:OKU_TASK).State")
	cmd.Env = append(os.Environ(), "OKU_TASK="+t.task(d))

	out, err := cmd.Output()
	if err != nil {
		return status, fmt.Errorf("read the state of task %s: %w", t.task(d), err)
	}

	status.Running = strings.TrimSpace(string(out)) == "Running"

	return status, nil
}

func (t *taskScheduler) Logs(_ context.Context, d Definition, lines int) (string, error) {
	data, err := os.ReadFile(d.LogFile)
	if os.IsNotExist(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	all := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")

	return strings.Join(all[max(0, len(all)-lines):], "\n"), nil
}

func (t *taskScheduler) schtasks(ctx context.Context, args ...string) error {
	if out, err := exec.CommandContext(ctx, "schtasks", args...).CombinedOutput(); err != nil {
		return commandError("schtasks", args, out, err)
	}

	return nil
}

// utf16File encodes text as UTF-16 with a byte order mark, which is the only
// encoding schtasks reads a task definition in.
func utf16File(text string) []byte {
	out := []byte{0xff, 0xfe}
	for _, unit := range utf16.Encode([]rune(text)) {
		out = append(out, byte(unit), byte(unit>>8))
	}

	return out
}

// taskXML renders the Task Scheduler definition. The task runs with the user's
// own rights and only while that user is logged on. Task Scheduler restarts a task only after a failure, so "always"
// and "on-failure" are the same here.
func taskXML(d Definition, self, stored, account string, enabled bool) string {
	esc := func(s string) string {
		var b strings.Builder

		_ = xml.EscapeText(&b, []byte(s))

		return b.String()
	}

	trigger := ""
	if enabled {
		trigger = "<LogonTrigger><Enabled>true</Enabled><UserId>" + esc(account) +
			"</UserId></LogonTrigger>"
	}

	restart := ""
	if d.Restart == "always" || d.Restart == "on-failure" {
		restart = "<RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>"
	}

	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>` + esc(d.Name) + `, installed by oku</Description></RegistrationInfo>
  <Triggers>` + trigger + `</Triggers>
  <Principals><Principal id="Author">
    <UserId>` + esc(account) + `</UserId>
    <LogonType>InteractiveToken</LogonType>
    <RunLevel>LeastPrivilege</RunLevel>
  </Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    ` + restart + `
  </Settings>
  <Actions Context="Author"><Exec>
    <Command>` + esc(self) + `</Command>
    <Arguments>` + RunCommand + ` "` + esc(stored) + `"</Arguments>
  </Exec></Actions>
</Task>
`
}
