package service

import (
	"strings"
	"testing"
	"time"
)

func TestB417AUnitFileHoldsEachValueOnItsOwnLine(t *testing.T) {
	unit := string((&systemd{scope: "--user"}).unitFile(Definition{
		Name:    "food",
		Program: "/store/food/bin/food",
		Args:    []string{"--flag\nExecStartPre=/bin/evil", "$HOME"},
		Env:     map[string]string{"MODE": "fast\nExecStartPost=/bin/evil"},
	}))

	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "ExecStartPre") || strings.HasPrefix(line, "ExecStartPost") {
			t.Fatalf("a value started a directive of its own:\n%s", unit)
		}
	}

	if !strings.Contains(unit, `"--flag\x0aExecStartPre=/bin/evil"`) || !strings.Contains(unit, `"$$HOME"`) {
		t.Fatalf("the unit does not escape the newline and the dollar:\n%s", unit)
	}
}

func TestB427ASystemUnitNamesTheUserItRunsAs(t *testing.T) {
	unit := string((&systemd{scope: "--system"}).unitFile(Definition{Name: "food", Program: "/p", User: "kyle"}))
	if !strings.Contains(unit, "\nUser=kyle\n") {
		t.Fatalf("the unit does not run as its user:\n%s", unit)
	}

	if unit := string((&systemd{scope: "--system"}).unitFile(Definition{Name: "food", Program: "/p"})); strings.Contains(unit, "User=") {
		t.Fatalf("a root unit names a user:\n%s", unit)
	}
}

func TestB581AJobIsAOneshotUnitThatItsTimerStarts(t *testing.T) {
	s := &systemd{scope: "--user"}
	d := Definition{Name: "backup", Program: "/p", Schedule: &Schedule{Hour: 3, Days: []time.Weekday{time.Monday, time.Friday}}}

	unit := string(s.unitFile(d))
	if !strings.Contains(unit, "\nType=oneshot\n") || strings.Contains(unit, "[Install]") {
		t.Fatalf("a job should be a oneshot unit that only its timer starts:\n%s", unit)
	}

	if timer := string(s.timerFile(d)); !strings.Contains(timer, "\nOnCalendar=Mon,Fri *-*-* 03:00:00\nPersistent=true\n") ||
		!strings.Contains(timer, "\nUnit=oku-backup.service\n") || !strings.Contains(timer, "\nWantedBy=timers.target\n") {
		t.Fatalf("the timer does not start the job on mon and fri at 03:00:\n%s", timer)
	}

	d.Schedule = &Schedule{Interval: 6 * time.Hour}
	if timer := string(s.timerFile(d)); !strings.Contains(timer, "\nOnActiveSec=21600\nOnUnitActiveSec=21600\n") {
		t.Fatalf("the timer does not repeat every 6h:\n%s", timer)
	}
}
