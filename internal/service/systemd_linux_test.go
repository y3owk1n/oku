package service

import (
	"strings"
	"testing"
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
	if !strings.Contains(unit, "User=\"kyle\"\n") {
		t.Fatalf("the unit does not run as its user:\n%s", unit)
	}

	if unit := string((&systemd{scope: "--system"}).unitFile(Definition{Name: "food", Program: "/p"})); strings.Contains(unit, "User=") {
		t.Fatalf("a root unit names a user:\n%s", unit)
	}
}
