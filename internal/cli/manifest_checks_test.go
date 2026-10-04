package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB462APlanExpandsThePlatformInABuildsSource(t *testing.T) {
	m := newMachine(t)
	want := "tool-1.0.0-" + runtime.GOOS + "-" + runtime.GOARCH
	_, sum := m.archive(t, want, map[string]string{"tool": script})

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, fmt.Appendf(nil, `[package]
name = "tool"
[version]
value = "1.0.0"
[build]
source = { url = "file://%s/tool-{{version}}-{{os}}-{{arch}}.tar.gz", sha256 = %q }
[[build.step]]
install = { bin = ["tool"] }
`, m.fixtures, sum), 0o644))

	out, err := m.run(t, "", "add", path, "--plan")

	if err != nil || !strings.Contains(out, want) {
		t.Fatalf("the plan does not show the source for this machine, %s: %v\n%s", want, err, out)
	}
}

func TestB463AMatchOrWhenThatNamesNoPlatformIsAnError(t *testing.T) {
	m := newMachine(t)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"),
		[]byte("[packages]\ntool = { ref = \"./tool.toml\", when = { os = \"macos\" } }\n"), 0o644))

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), `use "darwin"`) {
		t.Fatalf("a list entry with os = macos should fail and name darwin, got %v", err)
	}

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(`[package]
name = "tool"
[version]
value = "1.0.0"
[[artifact]]
match = { os = "linux", arch = "x86_64" }
url = "https://example.com/tool.tar.gz"
bin = ["tool"]
`), 0o644))

	out, err := m.run(t, "", "manifest", "lint", path)
	if err == nil || !strings.Contains(out, `arch = "x86_64" matches no platform, use "amd64"`) {
		t.Fatalf("lint should refuse arch = x86_64 and name amd64: %v\n%s", err, out)
	}
}

func TestB464LintFollowsLatestAndTheBuildsWhen(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")

	must(t, os.WriteFile(path, []byte(`[package]
name = "tool"
[[artifact]]
version = { from = "redirect", repo = "https://example.com/latest", regex = '/(\d+)/', latest = true }
url = "https://example.com/{{version}}/tool.tar.gz"
bin = ["tool"]
`), 0o644))

	out, err := m.run(t, "", "manifest", "lint", path)
	if err == nil || !strings.Contains(out, "artifact[0].version.latest needs from") {
		t.Fatalf("lint should refuse latest with a redirect: %v\n%s", err, out)
	}

	// A build for macOS alone never runs on Windows, so its run step needs no shell.
	must(t, os.WriteFile(path, []byte(`[package]
name = "tool"
description = "a tool"
[version]
value = "1.0.0"
[build]
when = { os = "darwin" }
source = { url = "https://example.com/tool.tar.gz", sha256 = "0000000000000000000000000000000000000000000000000000000000000000" }
[[build.step]]
run = "make"
[[build.step]]
install = { bin = ["tool"] }
`), 0o644))

	if out, err := m.run(t, "", "manifest", "lint", path); err != nil || strings.Contains(out, "can run on Windows") {
		t.Fatalf("lint asks a macOS build for a shell: %v\n%s", err, out)
	}
}

func TestB28LintChecksAnArtifactsVersionAndAVendorStepsScripts(t *testing.T) {
	m := newMachine(t)

	for body, want := range map[string]string{
		// json reads a page, and regex reads a redirect or a page.
		"[package]\nname = \"tool\"\n[[artifact]]\nurl = \"https://example.com/{{tag}}/tool\"\nbin = [\"tool\"]\n" +
			"version = { from = \"github-releases\", repo = \"o/tool\", json = [\"version\"] }\n": `artifact[0].version.json needs artifact[0].version.from = "page"`,
		"[package]\nname = \"tool\"\n[[artifact]]\nurl = \"https://example.com/{{tag}}/tool\"\nbin = [\"tool\"]\n" +
			"version = { from = \"github-releases\", repo = \"o/tool\", regex = '([0-9.]+)' }\n": `artifact[0].version.regex needs artifact[0].version.from = "redirect" or "page"`,
		// Only npm runs a package's install scripts.
		"[package]\nname = \"tool\"\n[version]\nvalue = \"1.0.0\"\n[build]\nneeds = [\"cargo\"]\n" +
			"[[build.step]]\nvendor = \"cargo\"\npackage = \"ripgrep\"\nscripts = [\"x\"]\n" +
			"[[build.step]]\ninstall = { bin = [\"tool\"] }\n": `scripts needs vendor = "npm" with package`,
	} {
		path := filepath.Join(m.fixtures, "tool.toml")
		must(t, os.MkdirAll(m.fixtures, 0o755))
		must(t, os.WriteFile(path, []byte(body), 0o644))

		if out, err := m.run(t, "", "manifest", "lint", path); err == nil || !strings.Contains(out, want) {
			t.Errorf("lint should say %q, got %v:\n%s", want, err, out)
		}
	}
}

func TestB28AnInstallSaysWhichCompletionsKeyItLeftOut(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script, "tool.fish": "complete -c tool"},
		"bin = [\"tool\"]\ncompletions = { fish = \"tool.fish\", fsh = \"tool.fish\" }")

	if out, err := m.run(t, "", "add", ref); err != nil || !strings.Contains(out, "unknown key artifact.completions.fsh") {
		t.Fatalf("add should install and name the key it left out: %v\n%s", err, out)
	}
}
