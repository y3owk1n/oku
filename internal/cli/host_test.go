package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/host"
)

// otherOS names an OS that is not this machine's.
func otherOS() string {
	if runtime.GOOS == "windows" {
		return "linux"
	}

	return "windows"
}

func TestB445SyncWarnsAboutWhatTheHostLacksAndInstallsTheRest(t *testing.T) {
	m := newMachine(t)
	m.opts.Host = &host.System{
		Manager:   "apt",
		Installed: func(pkg string) (bool, error) { return pkg == "curl", nil },
	}

	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)
	present := filepath.Join(m.fixtures, "present")
	must(t, os.WriteFile(present, nil, 0o644))

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), fmt.Appendf(nil, `[packages]
tool = { ref = %q }

[host]
has-go = { command = "go" }
has-file = { path = %q }
compiler = { command = "oku-no-such-program", install = "install a C compiler" }
sdk = { path = "/oku/no/such/file", install = "xcode-select --install" }
libgl = { apt = "libgl1", dnf = "mesa-libGL" }
has-curl = { apt = "curl" }
gcc = { dnf = "gcc" }
elsewhere = { command = "oku-no-such-program", when = { os = %q } }
`, ref, present, otherOS()), 0o644))

	out, err := m.run(t, "", "sync")
	if err != nil {
		t.Fatalf("sync stopped on a missing requirement: %v\n%s", err, out)
	}

	if m.toolOutput(t) != "hello from tool" {
		t.Fatalf("sync did not install the package:\n%s", out)
	}

	want := []string{
		"compiler is missing\ninstall a C compiler",
		"sdk is missing\nxcode-select --install",
		"libgl is missing\nget it with `sudo apt-get install libgl1`",
		"gcc names a package only for dnf, so oku cannot check it on this machine",
	}

	for _, line := range want {
		if !strings.Contains(out, line) {
			t.Fatalf("sync does not say %q:\n%s", line, out)
		}
	}

	for _, name := range []string{"has-go", "has-file", "has-curl", "elsewhere"} {
		if strings.Contains(out, name) {
			t.Fatalf("sync warns about %s, which the machine has or does not need:\n%s", name, out)
		}
	}

	// doctor reads the requirements from the active generation.
	out, err = m.run(t, "", "doctor")
	if err == nil || !strings.Contains(out, want[2]) || !strings.Contains(out, want[0]) {
		t.Fatalf("doctor does not report what the host lacks: %v\n%s", err, out)
	}

	// A requirement oku cannot check here is a note, not a problem.
	if !strings.Contains(out, "note     "+want[3]) {
		t.Fatalf("doctor does not note what it cannot check:\n%s", out)
	}
}

func TestB446AHostEntryOkuCannotReadIsAnError(t *testing.T) {
	m := newMachine(t)
	must(t, os.MkdirAll(m.config, 0o755))

	for entry, want := range map[string]string{
		`bad = { brew = "curl" }`:       "host.bad.brew is not a key of [host]",
		`bad = { install = "by hand" }`: "host.bad names nothing to check",
		`bad = { command = 1 }`:         "host.bad.command wants a string",
	} {
		must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte("[host]\n"+entry+"\n"), 0o644))

		if out, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want an error with %q, got %v\n%s", entry, want, err, out)
		}
	}
}
