package service

import (
	"strings"
	"testing"
	"time"
)

func TestB427ASystemTaskRunsAsItsUserFromBoot(t *testing.T) {
	d := Definition{Name: "food", User: `HOST\kyle`}

	got := taskXML(d, `C:\oku.exe`, `C:\food.json`, `HOST\kyle`, true, true)
	if !strings.Contains(got, `<UserId>HOST\kyle</UserId>`) || !strings.Contains(got, "<LogonType>S4U</LogonType>") ||
		strings.Contains(got, "S-1-5-18") || !strings.Contains(got, "<BootTrigger>") {
		t.Fatalf("the task does not run as its user from boot:\n%s", got)
	}

	d.User = ""
	if got := taskXML(d, `C:\oku.exe`, `C:\food.json`, `HOST\kyle`, true, true); !strings.Contains(got, "S-1-5-18") {
		t.Fatalf("a task that runs as root is not SYSTEM:\n%s", got)
	}
}

func TestB581AJobTaskHasItsScheduleAsTheTrigger(t *testing.T) {
	d := Definition{Name: "backup", Restart: "never", Schedule: &Schedule{Interval: 15 * time.Minute}}

	got := taskXML(d, `C:\oku.exe`, `C:\backup.json`, `HOST\kyle`, true, false)
	if !strings.Contains(got, "<Repetition><Interval>PT15M</Interval></Repetition>") || strings.Contains(got, "LogonTrigger") ||
		!strings.Contains(got, "<StartWhenAvailable>true</StartWhenAvailable>") {
		t.Fatalf("an interval job should repeat every 15 minutes and catch up a missed run:\n%s", got)
	}

	d.Schedule = &Schedule{Hour: 3, Days: []time.Weekday{time.Monday, time.Friday}}
	if got := taskXML(d, `C:\oku.exe`, `C:\backup.json`, `HOST\kyle`, true, false); !strings.Contains(got, "T03:00:00</StartBoundary>") ||
		!strings.Contains(got, "<DaysOfWeek><Monday/><Friday/></DaysOfWeek>") {
		t.Fatalf("a job at a time of day should run on mon and fri at 03:00:\n%s", got)
	}

	if got := taskXML(d, `C:\oku.exe`, `C:\backup.json`, `HOST\kyle`, false, false); strings.Contains(got, "Trigger>") {
		t.Fatalf("a job that is not enabled has no trigger:\n%s", got)
	}
}
