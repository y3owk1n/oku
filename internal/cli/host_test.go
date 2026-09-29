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

func TestB449APackageNamesWhatTheHostMustHaveForItAndItsDeps(t *testing.T) {
	m := newMachine(t)
	m.opts.Host = &host.System{}

	archive, sum := m.archive(t, "lib", map[string]string{"lib.txt": "lib"})
	must(t, os.WriteFile(filepath.Join(m.fixtures, "lib.toml"), fmt.Appendf(nil, `[package]
name = "lib"
[version]
value = "1.0.0"
[host]
sdk = { path = "/oku/no/such/sdk", install = "install the SDK" }
[[artifact]]
url = "file://%s"
sha256 = %q
data = true
`, archive, sum), 0o644))

	archive, sum = m.archive(t, "tool", map[string]string{"tool": script})
	ref := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(ref, fmt.Appendf(nil, `[package]
name = "tool"
[version]
value = "1.0.0"
[host]
docker = { command = "oku-no-such-docker", install = "install docker" }
[runtime]
deps = ["./lib.toml"]
[[artifact]]
url = "file://%s"
sha256 = %q
bin = ["tool"]
`, archive, sum), 0o644))

	out, err := m.run(t, "", "add", ref)
	if err != nil || m.toolOutput(t) != "hello from tool" {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// A dep's requirement is the package's, since the package needs the dep.
	for _, line := range []string{
		"docker, which tool needs, is missing\ninstall docker",
		"sdk, which tool needs, is missing\ninstall the SDK",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("add does not say %q:\n%s", line, out)
		}
	}

	out, err = m.run(t, "", "sync")
	if err != nil || !strings.Contains(out, "docker, which tool needs, is missing") {
		t.Fatalf("sync does not warn about the package's requirement: %v\n%s", err, out)
	}

	if out, err = m.run(t, "", "doctor"); err == nil || !strings.Contains(out, "sdk, which tool needs, is missing") {
		t.Fatalf("doctor does not report the package's requirement: %v\n%s", err, out)
	}

	// Removing the package removes what it needs.
	_, err = m.run(t, "", "remove", "tool")
	must(t, err)

	if out, _ = m.run(t, "", "doctor"); strings.Contains(out, "is missing") {
		t.Fatalf("doctor still reports the requirement of a removed package:\n%s", out)
	}
}

func TestB450AFailedBuildNamesWhatTheHostLacks(t *testing.T) {
	m := newMachine(t)
	m.opts.Host = &host.System{}

	ref := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(ref, []byte(`[package]
name = "tool"
[version]
value = "1.0.0"
[host]
compiler = { command = "oku-no-such-cc", install = "install a C compiler" }
[build]
needs = ["sh"]
[[build.step]]
run = "oku-no-such-cc"
shell = "sh"
`), 0o644))

	out, err := m.run(t, "", "add", ref, "--yes")
	if err == nil || !strings.Contains(err.Error(), "compiler is missing\ninstall a C compiler") {
		t.Fatalf("the failed build does not name the missing compiler: %v\n%s", err, out)
	}
}

func TestB451LintReportsAHostEntryOkuCannotRead(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(`[package]
name = "tool"
[version]
value = "1.0.0"
[host]
compiler = { brew = "gcc" }
[[artifact]]
url = "https://example.com/tool.tar.gz"
bin = ["tool"]
`), 0o644))

	out, err := m.run(t, "", "manifest", "lint", path)
	if err == nil || !strings.Contains(out, "host.compiler.brew is not a key of [host]") {
		t.Fatalf("lint does not report the [host] entry: %v\n%s", err, out)
	}
}

func TestB452ARequirementOfManyPackagesNamesTheFirstAndCountsTheRest(t *testing.T) {
	m := newMachine(t)
	m.opts.Host = &host.System{}

	var entries strings.Builder

	for _, name := range []string{"pa", "pb", "pc", "pd", "pe"} {
		archive, sum := m.archive(t, name, map[string]string{name: script})
		must(t, os.WriteFile(filepath.Join(m.fixtures, name+".toml"), fmt.Appendf(nil, `[package]
name = %q
[version]
value = "1.0.0"
[host]
sdk = { path = "/oku/no/such/sdk" }
[[artifact]]
url = "file://%s"
sha256 = %q
bin = [%q]
`, name, archive, sum, name), 0o644))
		fmt.Fprintf(&entries, "%s = %q\n", name, filepath.Join(m.fixtures, name+".toml"))
	}

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte("[packages]\n"+entries.String()), 0o644))

	out, err := m.run(t, "", "sync")
	if err != nil || !strings.Contains(out, "sdk, which pa, pb, pc and 2 other packages need, is missing") {
		t.Fatalf("sync does not name the first packages and count the rest: %v\n%s", err, out)
	}
}
