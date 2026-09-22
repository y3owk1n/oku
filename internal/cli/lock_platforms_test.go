package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/platform"
)

// otherPlatform returns a platform of another OS than the host's. It has no
// libc, so one artifact matches it alone.
func otherPlatform() platform.Platform {
	for _, p := range platform.All() {
		if p.OS != platform.Host().OS && p.Libc == "" {
			return p
		}
	}

	panic("unreachable")
}

// twoPlatformManifest writes a manifest with an artifact for the host and one
// for other. The other download is no archive and the manifest gives no
// checksum for it.
func (m machine) twoPlatformManifest(t *testing.T, other platform.Platform) (string, string) {
	t.Helper()

	archive, sum := m.archive(t, "tool", map[string]string{"tool": script})
	foreign := filepath.Join(m.fixtures, "tool-foreign.bin")
	must(t, os.WriteFile(foreign, []byte("not an archive"), 0o644))

	foreignSum := sha256.Sum256([]byte("not an archive"))
	host := platform.Host()

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nmatch = { os = %q, arch = %q, libc = %q }\nurl = \"file://%s\"\nsha256 = %q\n"+
			"bin = [\"tool\"]\n"+
			"[[artifact]]\nmatch = { os = %q, arch = %q }\nurl = \"file://%s\"\nbin = [\"tool\"]\n",
		host.OS,
		host.Arch,
		host.Libc,
		archive,
		sum,
		other.OS,
		other.Arch,
		foreign,
	))

	return ref, hex.EncodeToString(foreignSum[:])
}

func TestB179LockPlatformsPinsAnotherPlatformWithoutInstallingIt(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref, foreignSum := m.twoPlatformManifest(t, other)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref,
	)), 0o644))

	out, err := m.run(t, "", "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !strings.Contains(out, "publishes no checksum for "+other.String()) {
		t.Fatalf("sync did not report the download it trusted:\n%s", out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	_, entry, _ := strings.Cut(string(locked), "platform."+other.String()+"]")
	if !strings.Contains(entry, foreignSum) {
		t.Fatalf("oku.lock does not pin %s at %s:\n%s", other, foreignSum, locked)
	}

	if len(m.storeEntries(t)) != 1 {
		t.Fatalf("the store holds more than the host's package: %v", m.storeEntries(t))
	}

	// oku cannot pin a platform that has no artifact and no build.
	none := platform.Platform{OS: other.OS, Arch: "arm64"}

	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", none.String(), ref,
	)), 0o644))

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "tool has no artifact for "+none.String()) {
		t.Fatalf("want an error that names %s, got %v", none, err)
	}
}

func TestB180AProjectPinsEveryPlatformItCanAndTheGlobalListTheHost(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref, _ := m.twoPlatformManifest(t, other)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if strings.Contains(string(locked), "platform."+other.String()+"]") {
		t.Fatalf("the global lock pins %s:\n%s", other, locked)
	}

	project := filepath.Join(m.fixtures, "work")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))

	m.opts.WorkDir = project

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	locked, err = os.ReadFile(filepath.Join(project, "oku.lock"))
	must(t, err)

	// The manifest has an artifact for two platforms, and oku skips the others.
	if got := strings.Count(string(locked), "[package.platform."); got != 2 ||
		!strings.Contains(string(locked), "platform."+other.String()+"]") {
		t.Fatalf("the project lock does not pin the host and %s alone:\n%s", other, locked)
	}
}

func TestB181LockedSyncFailsWhenTheLockWouldChange(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if out, err := m.run(t, "", "sync", "--locked"); err != nil {
		t.Fatalf("a locked sync of a complete lock failed: %v\n%s", err, out)
	}

	lockPath := filepath.Join(m.config, "oku.lock")
	locked, err := os.ReadFile(lockPath)
	must(t, err)

	// The lock now looks as if another machine wrote it, on an empty store.
	foreign := strings.ReplaceAll(string(locked), platform.Host().String(), "plan9-mips")
	must(t, os.WriteFile(lockPath, []byte(foreign), 0o644))
	must(t, os.RemoveAll(m.data))

	_, err = m.run(t, "", "sync", "--locked")
	if err == nil ||
		!strings.Contains(err.Error(), "does not pin tool for "+platform.Host().String()) {
		t.Fatalf("want an error that names tool and the host, got %v", err)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(after) != foreign || len(m.storeEntries(t)) != 0 {
		t.Fatalf("a locked sync changed the lock or the store: %v\n%s", m.storeEntries(t), after)
	}

	// A lock that holds a package the list dropped is out of date too.
	must(t, os.WriteFile(lockPath, locked, 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), nil, 0o644))

	_, err = m.run(t, "", "sync", "--locked")
	if err == nil || !strings.Contains(err.Error(), "is out of date") {
		t.Fatalf("want an out of date error, got %v", err)
	}
}

func TestB182SyncPinsAPackageWhoseWhenLeavesOutTheHost(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()

	// Both downloads are for the other platform alone, and neither is an archive.
	download := filepath.Join(m.fixtures, "foreign.bin")
	must(t, os.WriteFile(download, []byte("not an archive"), 0o644))

	artifact := fmt.Sprintf(
		"[[artifact]]\nmatch = { os = %q }\nurl = \"file://%s\"\nbin = [\"x\"]\n",
		other.OS,
		download,
	)
	m.rawManifest(t, "interp", artifact)
	tool := m.rawManifest(t, "tool", "[runtime]\ndeps = [\"./interp.toml\"]\n"+artifact)

	list := fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = { ref = %q, when = { os = %q } }\n",
		other.String(), tool, other.OS,
	)
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(list), 0o644))

	out, err := m.run(t, "", "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !strings.Contains(out, "tool 1.2.3, pinned and not installed on "+platform.Host().String()) {
		t.Fatalf("sync did not say that it pinned tool:\n%s", out)
	}

	lockPath := filepath.Join(m.config, "oku.lock")
	locked, err := os.ReadFile(lockPath)
	must(t, err)

	for _, want := range []string{
		"name = 'tool'", "name = 'interp'",
		"[package.platform." + other.String() + "]", "[package.dep.platform." + other.String() + "]",
	} {
		if !strings.Contains(string(locked), want) {
			t.Fatalf("oku.lock lacks %s:\n%s", want, locked)
		}
	}

	if strings.Contains(string(locked), platform.Host().String()) || len(m.storeEntries(t)) != 0 ||
		exists(m.profile("bin", "x")) {
		t.Fatalf("sync installed tool on a host that its when leaves out:\n%s", locked)
	}

	// A lock that pins the package needs no second look at its manifest.
	must(t, os.Remove(tool))

	if out, err := m.run(t, "", "sync", "--locked"); err != nil {
		t.Fatalf("sync of a complete lock read the manifest again: %v\n%s", err, out)
	}
}

func TestB184AServiceWithWhenIsInstalledOnMatchingPlatformsOnly(t *testing.T) {
	m := newMachine(t)

	// Both services have one name, and only the second is for this platform.
	ref := m.manifest(t, "food", map[string]string{"food": script}, fmt.Sprintf(
		"bin = [\"food\"]\n"+
			"[[service]]\nname = \"food\"\ncommand = \"Food.app/food\"\nwhen = { os = %q }\n"+
			"[[service]]\nname = \"food\"\ncommand = \"bin/food\"\nwhen = { os = %q }\n",
		otherPlatform().OS, platform.Host().OS,
	))

	out, err := m.run(t, "", "add", ref, "--service")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got, ok := m.services.state["food"]
	if !ok || !got.running || !strings.HasSuffix(got.def.Program, "/bin/food") {
		t.Fatalf("the service of this platform is not the one that runs: %+v", got)
	}

	if len(m.services.state) != 1 {
		t.Fatalf("oku installed the service of another platform: %v", m.services.state)
	}
}

// platformEntry returns the text of name's entry in a lock.
func platformEntry(locked, name string) string {
	_, after, _ := strings.Cut(locked, "platform."+name+"]\n")
	entry, _, _ := strings.Cut(after, "\n[")

	return entry
}

func TestB187ABuildIsPinnedForAnotherPlatformWithItsSourceArchive(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	archive, sum := m.archive(t, "src", map[string]string{"tool": script})

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[build]\nsource = { url = \"file://%s\" }\n"+
			"[[build.step]]\nrun = \"true\"\nshell = \"sh\"\nnetwork = true\n"+
			"[[build.step]]\ninstall = { bin = [\"tool\"] }\n", archive,
	))

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref,
	)), 0o644))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	entry := platformEntry(string(locked), other.String())
	for _, want := range []string{"strategy = 'build'", sum, "impure = true"} {
		if !strings.Contains(entry, want) {
			t.Fatalf("the entry of %s lacks %s:\n%s", other, want, locked)
		}
	}

	if entry != platformEntry(string(locked), platform.Host().String()) {
		t.Fatalf("the entry of %s differs from the one this machine built:\n%s", other, locked)
	}
}

// goVendorManifest writes a manifest that builds a Go program with a vendor
// step. The vendored module is a local directory, so it needs no network.
func (m machine) goVendorManifest(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs go")
	}

	tmp := filepath.Join(m.fixtures, "tmp")
	must(t, os.Mkdir(tmp, 0o755))
	t.Setenv("TMPDIR", tmp)

	lib := filepath.Join(m.fixtures, "greet")
	must(t, os.MkdirAll(lib, 0o755))
	must(t, os.WriteFile(filepath.Join(lib, "go.mod"),
		[]byte("module example.com/greet\n\ngo 1.21\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(lib, "greet.go"),
		[]byte("package greet\n\nfunc Text() string { return \"hi\" }\n"), 0o644))

	ref := filepath.Join(m.fixtures, "gotool.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(`[package]
name = "gotool"
[version]
value = "1.0.0"
[build]
needs = ["go"]
[[build.step]]
run = """
printf 'module example.com/gotool\\n\\ngo 1.21\\n\\nrequire example.com/greet v0.0.0\\n\\nreplace example.com/greet => %s\\n' > go.mod
printf 'package main\\n\\nimport "example.com/greet"\\n\\nfunc main() { println(greet.Text()) }\\n' > main.go
"""
shell = "sh"
[[build.step]]
vendor = "go"
[[build.step]]
run = "go build -mod=vendor -o gotool ."
shell = "sh"
env = { GOTOOLCHAIN = "local", GOFLAGS = "-buildvcs=false" }
[[build.step]]
install = { bin = ["gotool"] }
`, lib)), 0o644))

	return ref
}

func TestB187AGoVendorDigestIsPinnedForEveryPlatform(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref := m.goVendorManifest(t)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ngotool = %q\n", other.String(), ref,
	)), 0o644))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	entry := platformEntry(string(locked), other.String())
	if !strings.Contains(entry, "vendor_sha256 = '") ||
		entry != platformEntry(string(locked), platform.Host().String()) {
		t.Fatalf("the entry of %s lacks the vendor digest of this machine:\n%s", other, locked)
	}
}

func TestB188UpdateKeepsThePinsOfABuildThatTheStoreHolds(t *testing.T) {
	m := newMachine(t)
	ref := m.goVendorManifest(t)

	_, err := m.run(t, "", "add", ref, "--yes")
	must(t, err)

	lockPath := filepath.Join(m.config, "oku.lock")
	before, err := os.ReadFile(lockPath)
	must(t, err)

	if !strings.Contains(string(before), "vendor_sha256 = '") {
		t.Fatalf("oku.lock does not pin the vendor output:\n%s", before)
	}

	// The store holds the build, so update builds nothing.
	if out, err := m.run(t, "", "update", "--yes"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(after) != string(before) {
		t.Fatalf("update changed the lock of a build that did not change:\n%s", after)
	}
}

func TestB190SyncCompletesTheBuildPinsThatAnOlderOkuDidNotWrite(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	archive, _ := m.archive(t, "src", map[string]string{"tool": script})

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[build]\nsource = { url = \"file://%s\" }\n"+
			"[[build.step]]\ninstall = { bin = [\"tool\"] }\n", archive,
	))

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref,
	)), 0o644))

	_, err := m.run(t, "", "sync", "--yes")
	must(t, err)

	lockPath := filepath.Join(m.config, "oku.lock")
	full, err := os.ReadFile(lockPath)
	must(t, err)

	// An older oku wrote no source pin, neither into the lock nor beside the build.
	strip := func(path string) {
		data, err := os.ReadFile(path)
		must(t, err)

		var kept []string

		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "url = ") && !strings.HasPrefix(line, "sha256 = ") {
				kept = append(kept, line)
			}
		}

		must(t, os.Chmod(path, 0o644))
		must(t, os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644))
	}

	strip(lockPath)

	metas, err := filepath.Glob(filepath.Join(m.data, "store", "tool-*", "oku-meta.toml"))
	must(t, err)

	for _, meta := range metas {
		strip(meta)
	}

	before := m.storeEntries(t)

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(after) != string(full) {
		t.Fatalf("sync did not complete the pins of both platforms:\n%s", after)
	}

	if got := m.storeEntries(t); len(got) != len(before) {
		t.Fatalf("sync built again to pin: %v", got)
	}
}
