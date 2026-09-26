package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
