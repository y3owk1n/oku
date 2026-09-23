package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/platform"
)

// onlyFor writes a manifest named name with one artifact, for p alone.
func (m machine) onlyFor(t *testing.T, name string, p platform.Platform) string {
	t.Helper()

	archive, sum := m.archive(t, name, map[string]string{name: script})

	return m.rawManifest(t, name, fmt.Sprintf(
		"[[artifact]]\nmatch = { os = %q, arch = %q }\nurl = \"file://%s\"\nsha256 = %q\nbin = [%q]\n",
		p.OS, p.Arch, archive, sum, name,
	))
}

func (m machine) writeOwnList(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(m.config, "oku.toml")
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func TestB270WhenTakesAnArrayOfTablesAndMatchesWhenAnyDoes(t *testing.T) {
	m := newMachine(t)
	here := m.namedManifest(t, "here", "here", "here")
	elsewhere := m.namedManifest(t, "elsewhere", "elsewhere", "elsewhere")

	m.writeOwnList(t, fmt.Sprintf(
		"[packages]\nhere = { ref = %q, when = [{ os = \"plan9\" }, { os = %q }] }\n"+
			"elsewhere = { ref = %q, when = [{ os = \"plan9\" }, { os = \"aix\" }] }\n",
		here, platform.Host().OS, elsewhere,
	))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "here")) || exists(m.profile("bin", "elsewhere")) {
		t.Fatal("profile does not hold exactly the package that one table matches")
	}
}

func TestB271AddLeavesOutThePlatformsAManifestHasNothingFor(t *testing.T) {
	m := newMachine(t)
	host, other := platform.Host(), otherPlatform()
	list := m.writeOwnList(t, fmt.Sprintf("[lock]\nplatforms = [%q]\n\n[packages]\n", other))

	// A tool for the host alone is installed, and its entry leaves out other.
	out, err := m.run(t, "", "add", m.onlyFor(t, "here", host))
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "here has no artifact or build for "+other.String()) {
		t.Fatalf("add does not say what it left out:\n%s", out)
	}

	// A tool for other alone is pinned for it and not installed here.
	out, err = m.run(t, "", "add", m.onlyFor(t, "there", other))
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "pinned and not installed on "+host.String()) ||
		exists(m.profile("bin", "there")) {
		t.Fatalf("add installed a package that has no artifact for the host:\n%s", out)
	}

	own, err := os.ReadFile(list)
	must(t, err)

	for _, want := range []string{
		fmt.Sprintf(`when = { os = %q, arch = %q }`, host.OS, host.Arch),
		fmt.Sprintf(`when = { os = %q, arch = %q }`, other.OS, other.Arch),
	} {
		if !strings.Contains(string(own), want) {
			t.Fatalf("oku.toml lacks %s:\n%s", want, own)
		}
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	_, there, _ := strings.Cut(string(locked), "name = 'there'")
	if !strings.Contains(there, "platform."+other.String()+"]") {
		t.Fatalf("oku.lock does not pin there for %s:\n%s", other, locked)
	}

	if out, err := m.run(t, "", "sync", "--locked"); err != nil {
		t.Fatalf("a locked sync after the adds should pass: %v\n%s", err, out)
	}

	// With no platform left, add fails and names the ones the manifest has.
	m.writeOwnList(t, "[packages]\n")

	_, err = m.run(t, "", "add", m.onlyFor(t, "none", other))
	if err == nil || !strings.Contains(err.Error(), host.String()+", only for "+other.String()) {
		t.Fatalf("want an error that names %s and %s, got %v", host, other, err)
	}
}

func TestB272UpdateNarrowsWhenAndNeverWidensIt(t *testing.T) {
	m := newMachine(t)
	host, other := platform.Host(), otherPlatform()
	ref, _ := m.twoPlatformManifest(t, other)
	list := m.writeOwnList(t, fmt.Sprintf("[lock]\nplatforms = [%q]\n\n[packages]\n", other))

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	both, err := os.ReadFile(ref)
	must(t, err)

	// The new version drops other.
	hostOnly, _, _ := strings.Cut(string(both), "[[artifact]]\nmatch = { os = \""+other.OS)
	must(t, os.WriteFile(ref, []byte(strings.Replace(hostOnly, "1.2.3", "1.2.4", 1)), 0o644))

	out, err := m.run(t, "", "update")
	if err != nil {
		t.Fatalf("update should narrow the entry instead of failing: %v\n%s", err, out)
	}

	if !strings.Contains(out, "tool has no artifact or build for "+other.String()) {
		t.Fatalf("update does not say it narrowed the entry:\n%s", out)
	}

	own, err := os.ReadFile(list)
	must(t, err)

	narrowed := fmt.Sprintf("when = { os = %q, arch = %q, libc = %q }", host.OS, host.Arch, host.Libc)
	if host.Libc == "" {
		narrowed = fmt.Sprintf("when = { os = %q, arch = %q }", host.OS, host.Arch)
	}

	if !strings.Contains(string(own), narrowed) {
		t.Fatalf("oku.toml lacks %s:\n%s", narrowed, own)
	}

	// The version after it has other again, and update leaves the when alone.
	must(t, os.WriteFile(ref, []byte(strings.Replace(string(both), "1.2.3", "1.2.5", 1)), 0o644))

	out, err = m.run(t, "", "update")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if !strings.Contains(out, "tool 1.2.5 has an artifact or a build for "+other.String()) {
		t.Fatalf("update does not say the new version has other:\n%s", out)
	}

	now, err := os.ReadFile(list)
	must(t, err)

	if string(now) != string(own) {
		t.Fatalf("update widened the when:\n%s", now)
	}
}

func TestB273SyncLeavesOutAPackageWithNothingForTheHostAndSaysWhatToWrite(t *testing.T) {
	m := newMachine(t)
	host, other := platform.Host(), otherPlatform()
	here := m.onlyFor(t, "here", host)
	there := m.onlyFor(t, "there", other)
	body := fmt.Sprintf("[packages]\nhere = %q\nthere = %q\n", here, there)
	list := m.writeOwnList(t, body)

	_, err := m.run(t, "", "sync")

	line := fmt.Sprintf("there = { ref = %q, when = { os = %q, arch = %q } }", there, other.OS, other.Arch)
	if err == nil || !strings.Contains(err.Error(), "there has no artifact or build for "+host.String()) ||
		!strings.Contains(err.Error(), line) {
		t.Fatalf("want an error that names %s and gives the line\n%s\ngot %v", host, line, err)
	}

	if !exists(m.profile("bin", "here")) {
		t.Fatal("sync did not install the rest of the list")
	}

	if own, _ := os.ReadFile(list); string(own) != body {
		t.Fatalf("sync changed oku.toml:\n%s", own)
	}

	m.writeOwnList(t, fmt.Sprintf("[packages]\nhere = %q\n%s\n", here, line))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("a sync with the line should pass: %v\n%s", err, out)
	}
}

func TestB274ABuildAppliesOnlyToThePlatformsOfItsWhen(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[build]\nwhen = { os = %q }\n[[build.step]]\nrun = \"true\"\n", other.OS,
	))

	_, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "tool has no artifact or build for "+platform.Host().String()) {
		t.Fatalf("want an error that the build leaves out the host, got %v", err)
	}
}

func TestB230InferenceWithNoAssetForTheHostInfersForALockPlatform(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	archive, _ := m.archive(t, "release", map[string]string{"tool": script, "tool.exe": script})
	asset := fmt.Sprintf("tool-v1.4.0-%s-%s.tar.gz", other.OS, other.Arch)

	inferServer(t, &m, map[string]string{asset: archive})
	list := m.writeOwnList(t, fmt.Sprintf("[lock]\nplatforms = [%q]\n\n[packages]\n", other))

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add should pin the lock platform instead of failing: %v\n%s", err, out)
	}

	if !strings.Contains(out, "pinned and not installed on "+platform.Host().String()) ||
		exists(m.profile("bin", "tool")) {
		t.Fatalf("add installed a package with no asset for the host:\n%s", out)
	}

	own, err := os.ReadFile(list)
	must(t, err)

	if !strings.Contains(string(own), fmt.Sprintf(`when = { os = %q`, other.OS)) {
		t.Fatalf("oku.toml should limit the package to %s:\n%s", other, own)
	}
}

func TestB222AnAppImageFitsLinuxAlone(t *testing.T) {
	m := newMachine(t)
	image := filepath.Join(m.fixtures, "tool.AppImage")
	must(t, os.WriteFile(image, []byte(script), 0o755))

	inferServer(t, &m, map[string]string{
		"tool-x86_64.AppImage":  image,
		"tool-aarch64.AppImage": image,
	})

	linux := platform.Platform{OS: "linux", Arch: "arm64", Libc: platform.LibcGlibc}
	if platform.Host().OS == "linux" {
		linux = platform.Platform{OS: "darwin", Arch: "arm64"}
	}

	m.writeOwnList(t, fmt.Sprintf("[lock]\nplatforms = [%q]\n\n[packages]\n", linux))

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	own, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(own), `when = { os = "linux" }`) {
		t.Fatalf("an AppImage should limit the package to Linux:\n%s", own)
	}
}

func TestB276AServiceAndABuildStepTakeAnArrayOfWhenTables(t *testing.T) {
	m := newMachine(t)
	host := platform.Host().OS

	ref := m.manifest(t, "food", map[string]string{"food": script}, fmt.Sprintf(
		"bin = [\"food\"]\n"+
			"[[service]]\nname = \"food\"\ncommand = \"Food.app/food\"\nwhen = [{ os = \"plan9\" }, { os = %q }]\n"+
			"[[service]]\nname = \"food\"\ncommand = \"bin/food\"\nwhen = [{ os = \"plan9\" }, { os = %q }]\n",
		otherPlatform().OS, host,
	))

	if out, err := m.run(t, "", "add", ref, "--service"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.services.state["food"]; !strings.HasSuffix(got.def.Program, "/bin/food") ||
		len(m.services.state) != 1 {
		t.Fatalf("the service whose when matches this platform is not the one that runs: %v", m.services.state)
	}

	skipped := m.buildManifest(
		t, false, "",
		"[[build.step]]\nrun = \"exit 7\"\nshell = \"sh\"\nwhen = [{ os = \"plan9\" }, { os = \"aix\" }]\n"+
			writeTool+installTool,
	)

	if out, err := m.run(t, "", "add", skipped, "--yes"); err != nil {
		t.Fatalf("a step for other platforms ran: %v\n%s", err, out)
	}

	ran := m.buildManifest(t, false, "", fmt.Sprintf(
		"[[build.step]]\nrun = \"exit 7\"\nshell = \"sh\"\nwhen = [{ os = \"plan9\" }, { os = %q }]\n",
		host,
	)+writeTool+installTool)

	if _, err := m.run(t, "", "add", ran, "--yes"); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("want the step whose when matches this platform to run and fail, got %v", err)
	}
}
