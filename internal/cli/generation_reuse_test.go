package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// genDir returns generation n of the global profile.
func (m machine) genDir(n int) string {
	return filepath.Join(m.data, "profiles", "global", fmt.Sprintf("gen-%d", n))
}

func TestB369AGenerationReusesWhatItsPackagesAndLockDidNotChange(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	for _, text := range []string{"one", "two"} {
		m.writeFilesList(t, fmt.Sprintf(
			"[packages]\ntool = %q\n[files]\n\"{{home}}/.kept\" = { text = \"same\" }\n"+
				"\"{{home}}/.changed\" = { text = %q }\n",
			ref, text,
		))

		_, err := m.run(t, "", "sync")
		must(t, err)
	}

	binOne, err := filepath.EvalSymlinks(filepath.Join(m.genDir(1), "bin"))
	must(t, err)
	binTwo, err := filepath.EvalSymlinks(filepath.Join(m.genDir(2), "bin"))
	must(t, err)

	if binOne != binTwo {
		t.Fatalf("generation 2 built its own links: %s and %s", binOne, binTwo)
	}

	same := func(rel string) bool {
		one, errOne := os.Stat(filepath.Join(m.genDir(1), rel))
		two, errTwo := os.Stat(filepath.Join(m.genDir(2), rel))

		return errOne == nil && errTwo == nil && os.SameFile(one, two)
	}

	if !same("oku.lock") {
		t.Fatal("generation 2 holds its own copy of an unchanged lock")
	}

	entries, err := os.ReadDir(filepath.Join(m.genDir(2), "files"))
	must(t, err)

	shared := 0

	for _, entry := range entries {
		rel := filepath.Join("files", entry.Name())

		one, _ := os.ReadFile(filepath.Join(m.genDir(1), rel))
		two, _ := os.ReadFile(filepath.Join(m.genDir(2), rel))

		switch {
		case bytes.Equal(one, two) && !same(rel):
			t.Errorf("generation 2 copied the unchanged %s", rel)
		case !bytes.Equal(one, two) && same(rel):
			t.Errorf("a change to %s reached generation 1", rel)
		case same(rel):
			shared++
		}
	}

	if shared != 1 {
		t.Fatalf("generation 2 shares %d files with generation 1, want 1", shared)
	}

	if body, _ := os.ReadFile(home(".changed")); string(body) != "two" {
		t.Fatalf(".changed holds %q", body)
	}

	if _, err := exec.Command(m.profile("bin", "tool")).Output(); err != nil {
		t.Fatalf("tool no longer runs: %v", err)
	}

	_, err = m.run(t, "", "rollback", "1")
	must(t, err)

	if body, _ := os.ReadFile(home(".changed")); string(body) != "one" {
		t.Fatalf("rollback left .changed with %q", body)
	}
}

func TestB370GCDeletesTheLinksNoGenerationUses(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"one", "two"} {
		_, err := m.run(t, "", "add", m.namedManifest(t, name, name, name))
		must(t, err)
	}

	trees := filepath.Join(m.data, "profiles", "global", "trees")

	if entries, _ := os.ReadDir(trees); len(entries) != 2 {
		t.Fatalf("two sets of packages have %d trees of links", len(entries))
	}

	_, err := m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	if entries, _ := os.ReadDir(trees); len(entries) != 1 {
		t.Fatalf("gc --keep 1 left %d trees of links", len(entries))
	}

	for _, name := range []string{"one", "two"} {
		if _, err := exec.Command(m.profile("bin", name)).Output(); err != nil {
			t.Fatalf("%s no longer runs: %v", name, err)
		}
	}
}

func TestB371GCOlderThanKeepsWhatWasActiveThen(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"one", "two", "three", "four"} {
		_, err := m.run(t, "", "add", m.namedManifest(t, name, name, name))
		must(t, err)
	}

	// Generations 1 and 2 are older than 30 days, and 2 was active 30 days ago.
	for n, age := range map[int]time.Duration{1: 60, 2: 40, 3: 10} {
		path := filepath.Join(m.genDir(n), "oku-gen.toml")
		data, err := os.ReadFile(path)
		must(t, err)

		created := time.Now().Add(-age * 24 * time.Hour).UTC().Format(time.RFC3339)
		data = regexp.MustCompile(`(?m)^created = .*$`).ReplaceAll(data, []byte("created = "+created))
		must(t, os.WriteFile(path, data, 0o644))
	}

	if _, err := m.run(t, "", "gc", "--older-than", "30"); err == nil {
		t.Fatal("--older-than took a number without a unit")
	}

	out, err := m.run(t, "", "gc", "--older-than", "30d")
	must(t, err)

	for n, kept := range map[int]bool{1: false, 2: true, 3: true, 4: true} {
		if _, err := os.Stat(m.genDir(n)); (err == nil) != kept {
			t.Fatalf(
				"after gc --older-than 30d, generation %d exists: %v, want %v\n%s",
				n, err == nil, kept, out,
			)
		}
	}

	_, err = m.run(t, "", "rollback", "2")
	must(t, err)

	if _, err := exec.Command(m.profile("bin", "one")).Output(); err != nil {
		t.Fatalf("rollback to what was active 30 days ago does not run one: %v", err)
	}
}
