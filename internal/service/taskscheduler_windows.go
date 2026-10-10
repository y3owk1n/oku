package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/y3owk1n/oku/internal/dirs"
	"golang.org/x/sys/windows"
)

// RunCommand is the hidden oku subcommand that a scheduled task starts. A task
// can neither set environment variables nor send output to a file, so oku does
// both and then runs the service's program.
const RunCommand = "service-run"

// systemDir is where oku keeps the definitions and logs of system services.
func systemDir() string { return filepath.Join(dirs.ProgramData(), "oku") }

// SystemLogDir is where a system service's output goes.
func SystemLogDir() string { return filepath.Join(systemDir(), "logs") }

// taskScheduler runs a service as a scheduled task of the current user. An
// enabled service has a logon trigger. One that is only installed has no
// trigger, so it runs when "oku service start" asks for it.
type taskScheduler struct {
	// dir holds one JSON file per service, which "oku service-run" reads.
	dir string
	// system makes the task run from boot, with no user logged on, as the user
	// of the definition or as SYSTEM for one that runs as root. Registering such
	// a task needs administrator rights.
	system bool
}

// New returns the service manager for this OS. dataDir is oku's data directory.
func New(_, dataDir string) Manager {
	return &taskScheduler{dir: filepath.Join(dataDir, "services")}
}

// NewSystem returns the manager for services that run for the whole machine.
func NewSystem() Manager {
	return &taskScheduler{dir: filepath.Join(systemDir(), "services"), system: true}
}

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

// Unavailable is empty, because every Windows machine has the Task Scheduler.
func (t *taskScheduler) Unavailable() string { return "" }

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

	// A system task reads its definition, and a root one writes its log, in
	// ProgramData, where any user can make files.
	if t.system {
		dirs := []string{systemDir(), t.dir}
		if d.User == "" {
			dirs = append(dirs, filepath.Dir(d.LogFile))
		}

		for _, dir := range dirs {
			if err := protect(dir); err != nil {
				return err
			}
		}
	}

	data, err := json.MarshalIndent(Stored{Definition: d, Enabled: enabled}, "", "  ")
	if err != nil {
		return err
	}

	// A file that someone else made keeps them as its owner, so it goes first.
	if err := os.Remove(t.File(d)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.WriteFile(t.File(d), data, 0o644); err != nil {
		return err
	}

	definition := filepath.Join(t.dir, d.Name+".xml")
	if err := os.WriteFile(
		definition,
		utf16File(taskXML(d, self, t.File(d), owner.Username, enabled, t.system)),
		0o644,
	); err != nil {
		return err
	}
	defer os.Remove(definition)

	_ = t.schtasks(ctx, "/End", "/TN", t.task(d))

	if err := t.schtasks(ctx, "/Create", "/TN", t.task(d), "/XML", definition, "/F"); err != nil {
		return err
	}

	// A job runs on its schedule, not when oku installs it.
	if !enabled || d.Schedule != nil {
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

// LogHint names the file that the task's output goes to.
func (t *taskScheduler) LogHint(d Definition) string { return "look at " + d.LogFile }

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
func taskXML(d Definition, self, stored, account string, enabled, system bool) string {
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

	principal := "<UserId>" + esc(account) + "</UserId>\n    " +
		"<LogonType>InteractiveToken</LogonType>\n    <RunLevel>LeastPrivilege</RunLevel>"

	if system {
		// S4U runs the task as the user from boot without their password. S-1-5-18
		// is the SYSTEM account, for a service the list runs as root.
		principal = "<UserId>" + esc(d.User) + "</UserId>\n    " +
			"<LogonType>S4U</LogonType>\n    <RunLevel>LeastPrivilege</RunLevel>"
		if d.User == "" {
			principal = "<UserId>S-1-5-18</UserId>\n    <RunLevel>HighestAvailable</RunLevel>"
		}

		if enabled {
			trigger = "<BootTrigger><Enabled>true</Enabled></BootTrigger>"
		}
	}

	restart := ""
	if d.Restart == "always" || d.Restart == "on-failure" {
		restart = "<RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>"
	}

	// A job has its schedule as the trigger, and runs a run it missed once the
	// machine is on again.
	if d.Schedule != nil {
		trigger = ""
		if enabled {
			trigger = scheduleTrigger(*d.Schedule)
		}

		restart = "<StartWhenAvailable>true</StartWhenAvailable>"
	}

	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>` + esc(d.Name) + `, installed by oku</Description></RegistrationInfo>
  <Triggers>` + trigger + `</Triggers>
  <Principals><Principal id="Author">
    ` + principal + `
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

// scheduleTrigger renders the trigger of a job. The start boundary is a past
// date, so the schedule holds from now on, at local time.
func scheduleTrigger(s Schedule) string {
	if s.Interval > 0 {
		return fmt.Sprintf(
			"<TimeTrigger><Repetition><Interval>PT%dM</Interval></Repetition>"+
				"<StartBoundary>2000-01-01T00:00:00</StartBoundary><Enabled>true</Enabled></TimeTrigger>",
			int(s.Interval.Minutes()),
		)
	}

	start := fmt.Sprintf("<StartBoundary>2000-01-01T%02d:%02d:00</StartBoundary><Enabled>true</Enabled>", s.Hour, s.Minute)

	if len(s.Days) == 0 {
		return "<CalendarTrigger>" + start + "<ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger>"
	}

	days := ""
	for _, day := range s.Days {
		days += "<" + day.String() + "/>"
	}

	return "<CalendarTrigger>" + start + "<ScheduleByWeek><WeeksInterval>1</WeeksInterval><DaysOfWeek>" +
		days + "</DaysOfWeek></ScheduleByWeek></CalendarTrigger>"
}

// protect lets only SYSTEM, Administrators and the user who installs system
// services write in dir. It refuses a dir that another user owns, since that
// user could have put a definition in it that a task would run.
func protect(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the owner of %s: %w", dir, err)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}

	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}

	mine := me.User.Sid.String()

	switch owner.String() {
	case "S-1-5-18", "S-1-5-32-544", mine:
	default:
		return fmt.Errorf("%s belongs to another user, remove it first", dir)
	}

	// D:P takes nothing from ProgramData. SY is SYSTEM and BA Administrators,
	// and OICI passes each entry on to what dir holds.
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + mine + ")",
	)
	if err != nil {
		return err
	}

	list, _, err := descriptor.DACL()
	if err != nil {
		return err
	}

	return windows.SetNamedSecurityInfo(
		dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, list, nil,
	)
}
