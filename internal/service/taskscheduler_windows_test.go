package service

import (
	"strings"
	"testing"
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
