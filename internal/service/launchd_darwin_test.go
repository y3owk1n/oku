package service

import (
	"strings"
	"testing"
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
