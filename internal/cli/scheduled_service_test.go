package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestB581AServiceWithAScheduleRunsAsAJob(t *testing.T) {
	m := newMachine(t)

	job := func(schedule string) string {
		return m.manifest(t, "backup", map[string]string{"backup": script},
			"bin = [\"backup\"]\n[[service]]\nname = \"backup\"\ncommand = \"bin/backup\"\nschedule = "+schedule+"\n")
	}

	ref := job(`{ every = "15m" }`)
	if out, err := m.run(t, "", "add", ref, "--service"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got := m.services.state["backup"]
	if got == nil || !got.enabled || got.running || got.def.Schedule == nil || got.def.Schedule.Interval != 15*time.Minute {
		t.Fatalf("an enabled job should wait for its schedule of 15m, got %+v", got)
	}

	if out, err := m.run(t, "", "service", "status", "backup"); err != nil || !strings.Contains(out, "backup: idle, runs every 15m") {
		t.Fatalf("service status: %v\n%s", err, out)
	}

	out, err := m.run(t, "", "service", "list", "--json")
	must(t, err)

	var rows []struct{ Name, Schedule string }
	must(t, json.Unmarshal([]byte(out), &rows))

	if len(rows) != 1 || rows[0].Schedule != "every 15m" {
		t.Fatalf("service list --json should give the schedule, got %+v", rows)
	}

	// A job ends on its own, so start reports no exit.
	m.services.exitAfterStart = map[string]bool{"backup": true}

	if out, err := m.run(t, "", "service", "start", "backup"); err != nil {
		t.Fatalf("starting a job should run it once and not fail: %v\n%s", err, out)
	}

	// A new schedule reaches the service manager with the next update.
	job(`{ at = "03:00", weekdays = ["fri", "mon"] }`)

	if out, err := m.run(t, "", "update", "backup"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if s := m.services.state["backup"].def.Schedule; s == nil || s.Hour != 3 || len(s.Days) != 2 ||
		s.Days[0] != time.Monday || s.Days[1] != time.Friday {
		t.Fatalf("the new schedule did not reach the service manager: %+v", s)
	}

	if out, err := m.run(t, "", "service", "status", "backup"); err != nil || !strings.Contains(out, "runs at 03:00 on mon, fri") {
		t.Fatalf("service status: %v\n%s", err, out)
	}

	for schedule, want := range map[string]string{
		`{ every = "15m", at = "03:00" }`:          "every or at, not both",
		`{ every = "30s" }`:                        "minutes or hours from 1m to 24h",
		`{ at = "3:00" }`:                          "as HH:MM",
		`{ every = "1h", weekdays = ["mon"] }`:     "weekdays goes with at",
		`{ at = "03:00", weekdays = ["monday"] }`:  `"monday" is not a day`,
		"{ every = \"1h\" }\nrestart = \"always\"": "takes no restart",
	} {
		path := filepath.Join(m.fixtures, "bad.toml")
		must(t, os.WriteFile(path, []byte("[package]\nname = \"bad\"\n[version]\nvalue = \"1.0.0\"\n"+
			"[[service]]\nname = \"bad\"\ncommand = \"bin/bad\"\nschedule = "+schedule+"\n"), 0o644))

		if out, err := m.run(t, "", "manifest", "lint", path); err == nil || !strings.Contains(out+err.Error(), want) {
			t.Fatalf("lint of schedule %s should say %q: %v\n%s", schedule, want, err, out)
		}
	}
}
