package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB580PackagesDeliverPowerShellCompletionsThatTheHookLoads(t *testing.T) {
	m := newMachine(t)
	pwsh := func(name string) string { return m.profile("share", "completions", "pwsh", name) }

	// A table may name the file as pwsh or powershell, and a directory finds
	// _<name>.ps1.
	for name, completions := range map[string]string{
		"table": "{ powershell = \"c/_table.ps1\" }",
		"dir":   "\"c/\"",
	} {
		ref := m.manifest(
			t, name, map[string]string{name: script, "c/_" + name + ".ps1": "Register-ArgumentCompleter"},
			"bin = [\""+name+"\"]\ncompletions = "+completions,
		)

		if out, err := m.run(t, "", "add", ref); err != nil || !exists(pwsh("_"+name+".ps1")) {
			t.Fatalf("add %s: %v\n%s", name, err, out)
		}
	}

	// generate runs with powershell for PowerShell. A program that cannot print
	// them still installs, with the other shells.
	for name, body := range map[string]string{
		"knows":   completer,
		"refuses": "#!/bin/sh\n[ \"$2\" = powershell ] && exit 1\necho \"complete $2\"\n",
	} {
		ref := m.manifest(
			t, name, map[string]string{name: body},
			"bin = [\""+name+"\"]\ncompletions = { generate = \""+name+" completions {{shell}}\" }",
		)

		if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
			t.Fatalf("add %s: %v\n%s", name, err, out)
		}
	}

	if body, _ := os.ReadFile(pwsh("_knows.ps1")); string(body) != "complete powershell\n" {
		t.Fatalf("generate should print the PowerShell completions, got %q", body)
	}

	if exists(pwsh("_refuses.ps1")) || !exists(m.profile("share", "completions", "fish", "refuses.fish")) {
		t.Fatal("a program without PowerShell completions should install the others alone")
	}

	out, err := m.run(t, "", "hook", "pwsh")
	if err != nil {
		t.Fatalf("hook pwsh: %v\n%s", err, out)
	}

	if dir := filepath.Join(m.data, "profiles", "global", "current", "share", "completions") + "/pwsh"; !strings.Contains(out, dir) ||
		!strings.Contains(out, "-Filter *.ps1) { . $okuFile.FullName }") {
		t.Fatalf("the PowerShell hook should dot-source each script in %s:\n%s", dir, out)
	}
}
