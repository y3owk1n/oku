package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// projectWith makes a project at dir that installs a package called name, from
// the global list's directory so that oku uses the project.
func (m *machine) projectWith(t *testing.T, dir, name string) {
	t.Helper()

	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "oku.toml"), nil, 0o644))

	m.opts.WorkDir = dir

	_, err := m.run(t, "", "add", m.namedManifest(t, name, name, name))
	must(t, err)

	m.opts.WorkDir = m.fixtures
}

func (m machine) profiles(t *testing.T) []string {
	t.Helper()

	found, err := filepath.Glob(filepath.Join(m.data, "profiles", "project-*"))
	must(t, err)

	return found
}

func TestB378GCRemovesAProjectThatIsGoneAndFreesItsPackages(t *testing.T) {
	m := newMachine(t)
	kept := filepath.Join(m.fixtures, "kept")
	deleted := filepath.Join(m.fixtures, "deleted")
	unlisted := filepath.Join(m.fixtures, "unlisted")

	m.projectWith(t, kept, "keep")
	m.projectWith(t, deleted, "gone")
	m.projectWith(t, unlisted, "left")

	if len(m.profiles(t)) != 3 {
		t.Fatalf("want 3 project profiles, got %v", m.profiles(t))
	}

	// The shell hook may apply the project, which gc forgets with it.
	m.opts.WorkDir = deleted
	_, err := m.run(t, "", "allow")
	must(t, err)
	m.opts.WorkDir = m.fixtures

	must(t, os.RemoveAll(deleted))
	must(t, os.Remove(filepath.Join(unlisted, "oku.toml")))

	out, err := m.run(t, "", "gc", "--dry-run")
	must(t, err)

	if !strings.Contains(out, "would remove project") || len(m.profiles(t)) != 3 {
		t.Fatalf("the dry run removed a project or did not name it:\n%s", out)
	}

	out, err = m.run(t, "", "gc")
	must(t, err)

	for _, want := range []string{"its folder is gone", "its oku.toml is gone", "removed gone-"} {
		if !strings.Contains(out, want) {
			t.Fatalf("gc does not say %q:\n%s", want, out)
		}
	}

	allowed, _ := os.ReadFile(filepath.Join(m.data, "trust", "allow.toml"))
	if strings.Contains(string(allowed), deleted) {
		t.Fatalf("gc kept the allow of a project that is gone:\n%s", allowed)
	}

	if left := m.profiles(t); len(left) != 1 {
		t.Fatalf("gc left %v, want the kept project's profile only", left)
	}

	if entries := m.storeEntries(t); !strings.Contains(strings.Join(entries, " "), "keep-") {
		t.Fatalf("gc took the store path of a project that is there: %v", entries)
	}
}

func TestB378GCKeepsAProjectOnADriveThatIsNotMountedAndOneItCannotPlace(t *testing.T) {
	m := newMachine(t)

	away := "/Volumes/oku-test-not-mounted/project"
	if runtime.GOOS != "darwin" {
		away = "/mnt/oku-test-not-mounted/project"
	}

	placed := map[string]string{"project-aaaaaaaaaaaa": away, "project-bbbbbbbbbbbb": ""}

	for name, dir := range placed {
		profile := filepath.Join(m.data, "profiles", name)
		must(t, os.MkdirAll(filepath.Join(profile, "gen-1"), 0o755))
		state := []byte("created = 2026-09-01T00:00:00Z\n")
		must(t, os.WriteFile(filepath.Join(profile, "gen-1", "oku-gen.toml"), state, 0o644))

		if dir != "" {
			must(t, os.WriteFile(filepath.Join(profile, "project"), []byte(dir), 0o644))
		}
	}

	out, err := m.run(t, "", "gc")
	must(t, err)

	if !strings.Contains(out, "its volume is not mounted") ||
		!strings.Contains(out, "kept 1 project profile whose folder oku does not know") ||
		len(m.profiles(t)) != 2 {
		t.Fatalf("gc removed a project it cannot place:\n%s", out)
	}
}
