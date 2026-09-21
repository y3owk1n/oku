package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serviceNamed writes a package that ships one service, both called name.
func (m machine) serviceNamed(t *testing.T, name string) string {
	t.Helper()

	return m.manifest(t, name, map[string]string{name: script}, fmt.Sprintf(
		"bin = [%q]\n[[service]]\nname = %q\ncommand = \"bin/%s\"\n", name, name, name,
	))
}

// writeList replaces the global oku.toml with these packages.
func (m machine) writeList(t *testing.T, refs map[string]string) {
	t.Helper()

	body := "[packages]\n"
	for name, ref := range refs {
		body += fmt.Sprintf("%s = %q\n", name, ref)
	}

	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(body), 0o644))
}

func (m machine) generation(t *testing.T) string {
	t.Helper()

	gen, err := os.Readlink(filepath.Join(m.data, "profiles", "global", "current"))
	must(t, err)

	return filepath.Base(gen)
}

func (m machine) pending() string {
	return filepath.Join(m.data, "pending.toml")
}

func TestB130AFailedAddLeavesTheMachineAsItWas(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	list, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)
	lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	m.services.failInstall = map[string]bool{"food": true}

	if _, err := m.run(t, "", "add", m.serviceManifest(t)); err == nil {
		t.Fatal("add succeeded although the service could not be installed")
	}

	if got := m.generation(t); got != "gen-1" {
		t.Fatalf("the active generation is %s, want gen-1", got)
	}

	if exists(filepath.Join(m.data, "profiles", "global", "gen-2")) {
		t.Fatal("the generation of the failed add is still there")
	}

	if exists(m.profile("bin", "food")) {
		t.Fatal("the failed add left its program in the profile")
	}

	if got, _ := os.ReadFile(filepath.Join(m.config, "oku.toml")); string(got) != string(list) {
		t.Fatalf("oku.toml changed:\n%s", got)
	}

	if got, _ := os.ReadFile(filepath.Join(m.config, "oku.lock")); string(got) != string(lock) {
		t.Fatalf("oku.lock changed:\n%s", got)
	}

	if exists(m.pending()) {
		t.Fatal("pending.toml is left behind")
	}
}

func TestB131ATargetOfSomeoneElseStopsTheChangeBeforeItStarts(t *testing.T) {
	m := newMachine(t)
	_, font := m.exposedPaths()

	_, err := m.run(t, "", "add", m.serviceManifest(t))
	must(t, err)

	must(t, os.MkdirAll(filepath.Dir(font), 0o755))
	must(t, os.WriteFile(font, []byte("mine"), 0o644))

	// This sync has to remove the service and then expose the font.
	m.writeList(t, map[string]string{"foo": m.desktopManifest(t)})

	out, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), font) {
		t.Fatalf("sync should refuse and name %s, got %v\n%s", font, err, out)
	}

	if _, still := m.services.state["food"]; !still {
		t.Fatal("sync removed the service before it found the font it cannot write")
	}

	if body, _ := os.ReadFile(font); string(body) != "mine" {
		t.Fatalf("the user's file now holds %q", body)
	}
}

func TestB132AFailedStepUndoesTheStepsBeforeIt(t *testing.T) {
	m := newMachine(t)
	_, font := m.exposedPaths()

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	// This sync removes the font and then fails to install the service.
	m.writeList(t, map[string]string{"bard": m.serviceNamed(t, "bard")})
	m.services.failInstall = map[string]bool{"bard": true}

	out, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "the service manager refused bard") {
		t.Fatalf("sync should report the failed step, got %v\n%s", err, out)
	}

	if body, _ := os.ReadFile(font); string(body) != "not really a font" {
		t.Fatal("the font that sync removed is not back")
	}

	if got := m.generation(t); got != "gen-1" {
		t.Fatalf("the active generation is %s, want gen-1", got)
	}
}

func TestB133TheNextCommandPutsBackAChangeThatWasKilled(t *testing.T) {
	m := newMachine(t)
	_, font := m.exposedPaths()

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	// This sync removes the font, and dies while it installs the service.
	m.writeList(t, map[string]string{"bard": m.serviceNamed(t, "bard")})

	m.services.dieInInstall = true

	func() {
		defer func() { _ = recover() }()

		_, _ = m.run(t, "", "sync")
	}()

	if !exists(m.pending()) || exists(font) {
		t.Fatal("the killed sync should leave pending.toml and a machine without the font")
	}

	m.services.dieInInstall = false

	// remove changes the machine, so it puts generation 1 back first. bard is then
	// in no generation, and remove only has to drop it from the list.
	out, err := m.run(t, "", "remove", "bard")
	must(t, err)

	if !strings.Contains(out, "did not finish") {
		t.Fatalf("oku did not say that it put the machine back:\n%s", out)
	}

	if !exists(font) || exists(m.pending()) {
		t.Fatalf("the machine is not back at generation 1:\n%s", out)
	}

	if _, installed := m.services.state["bard"]; installed {
		t.Fatal("the service of the killed sync is installed")
	}

	if exists(filepath.Join(m.data, "profiles", "global", "gen-2")) {
		t.Fatal("the generation of the killed sync is still there")
	}
}

func TestB134DoctorReportsAChangeThatOkuCouldNotUndo(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.serviceManifest(t))
	must(t, err)

	// The sync fails at bard, and the undo fails when it installs food again.
	m.writeList(t, map[string]string{"bard": m.serviceNamed(t, "bard")})
	m.services.failInstall = map[string]bool{"bard": true, "food": true}

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "could not put the machine back") ||
		!strings.Contains(err.Error(), "refused food") {
		t.Fatalf("sync should name what it could not undo, got %v", err)
	}

	out, _ := m.run(t, "", "doctor")
	if !strings.Contains(out, "did not finish") {
		t.Fatalf("doctor does not report the unfinished change:\n%s", out)
	}

	m.services.failInstall = nil

	_, err = m.run(t, "", "sync")
	must(t, err)

	if out, _ := m.run(t, "", "doctor"); strings.Contains(out, "did not finish") {
		t.Fatalf("doctor still reports the change after oku resolved it:\n%s", out)
	}

	if _, installed := m.services.state["bard"]; !installed {
		t.Fatal("the sync after the repair did not install the service")
	}
}
