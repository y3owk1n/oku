package service

import (
	"strings"
	"testing"
	"time"
)

func TestB427ALaunchDaemonNamesTheUserItRunsAs(t *testing.T) {
	d := Definition{Name: "food", Program: "/store/food/bin/food"}

	if got := string(plist(d)); strings.Contains(got, "UserName") {
		t.Fatalf("a root daemon names a user:\n%s", got)
	}

	d.User = "kyle"
	if got := string(plist(d)); !strings.Contains(got, "<key>UserName</key>\n\t<string>kyle</string>") {
		t.Fatalf("the daemon does not run as its user:\n%s", got)
	}
}

func TestB581AJobPlistRunsOnItsScheduleAndNotAtLoad(t *testing.T) {
	d := Definition{Name: "backup", Program: "/p", Schedule: &Schedule{Interval: 15 * time.Minute}}

	got := string(plist(d))
	if strings.Contains(got, "RunAtLoad") || !strings.Contains(got, "<key>StartInterval</key>\n\t<integer>900</integer>") {
		t.Fatalf("an interval job should start every 900 seconds and not at load:\n%s", got)
	}

	d.Schedule = &Schedule{Hour: 3, Minute: 5, Days: []time.Weekday{time.Monday, time.Friday}}
	if got := string(plist(d)); !strings.Contains(got, "<key>Weekday</key><integer>1</integer><key>Hour</key><integer>3</integer><key>Minute</key><integer>5</integer>") ||
		!strings.Contains(got, "<key>Weekday</key><integer>5</integer>") {
		t.Fatalf("a job at a time of day should name each weekday:\n%s", got)
	}
}
