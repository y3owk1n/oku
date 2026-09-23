package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runsThroughItsNode reports whether the program tool of the profile runs, with
// no node on PATH, so that only the node oku picked can run it.
func (m machine) runsThroughItsNode(t *testing.T) string {
	t.Helper()

	cmd := exec.Command(m.profile("bin", "tool"))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	got, err := cmd.Output()
	must(t, err)

	return strings.TrimSpace(string(got))
}

func TestB255TheListNamesTheNodeBeforeConfigToml(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	must(t, os.MkdirAll(filepath.Join(m.config, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "packages", "node.toml"), node, 0o644))

	// config.toml names a node that does not exist, so only the list's works.
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte("[runtimes]\nnode = \"./missing.toml\"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"),
		[]byte("[runtimes]\nnode = \"./packages/node.toml\"\n"), 0o644))

	if out, err := m.run(t, "", "add", "npm:@scope/tool"); err != nil {
		t.Fatalf("add with [runtimes] in the list: %v\n%s", err, out)
	}

	if got := m.runsThroughItsNode(t); got != "1.1.0" {
		t.Fatalf("tool printed %q", got)
	}
}

func TestB255ARepoListNamesTheNodeOfTheSameRepo(t *testing.T) {
	const commit = "9999999999999999999999999999999999999999"

	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	machineRepo(t, &m, commit, map[string]string{
		"oku.toml": "[runtimes]\nnode = \"./packages/node.toml\"\n" +
			"[packages]\ntool = \"npm:@scope/tool\"\n",
		"packages/node.toml": string(node),
	})

	out, err := m.run(t, "", "sync", "github:me/machines")
	if err != nil {
		t.Fatalf("sync of a repo whose list names its node: %v\n%s", err, out)
	}

	if got := m.runsThroughItsNode(t); got != "1.1.0" {
		t.Fatalf("tool printed %q", got)
	}

	lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(lock), "github:me/machines#packages/node.toml") {
		t.Fatalf("the lock does not name the repo's node:\n%s", lock)
	}
}

func TestB255AnAdoptedLockNamesTheNodeOfTheRepo(t *testing.T) {
	const commit = "abababababababababababababababababababab"

	list := "[runtimes]\nnode = \"./packages/node.toml\"\n[packages]\ntool = \"npm:@scope/tool\"\n"

	// The author's machine keeps the repo as its config directory.
	author := newMachine(t)
	npmServer(t, &author, "", "1.1.0")

	node, err := os.ReadFile(author.fakeNode(t))
	must(t, err)

	must(t, os.MkdirAll(filepath.Join(author.config, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(author.config, "packages", "node.toml"), node, 0o644))
	must(t, os.WriteFile(filepath.Join(author.config, "oku.toml"), []byte(list), 0o644))

	_, err = author.run(t, "", "sync")
	must(t, err)

	published, err := os.ReadFile(filepath.Join(author.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(published), `\"./packages/node.toml\"`) {
		t.Fatalf("the author's lock does not name the node relative to the list:\n%s", published)
	}

	// Another machine adopts the repo with the lock beside the list.
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")
	machineRepo(t, &m, commit, map[string]string{
		"oku.toml": list, "oku.lock": string(published), "packages/node.toml": string(node),
	})

	out, err := m.run(t, "", "sync", "github:me/machines")
	if err != nil {
		t.Fatalf("sync of a repo with a published lock of npm packages: %v\n%s", err, out)
	}

	if got := m.runsThroughItsNode(t); got != "1.1.0" {
		t.Fatalf("tool printed %q", got)
	}
}

func TestB183AProjectLockNamesTheNodeRelativeToTheProject(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	project := filepath.Join(m.fixtures, "work", "api")
	must(t, os.MkdirAll(filepath.Join(project, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(project, "packages", "node.toml"), node, 0o644))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"),
		[]byte("[runtimes]\nnode = \"./packages/node.toml\"\n"), 0o644))

	m.opts.WorkDir = project

	if out, err := m.run(t, "", "add", "npm:@scope/tool"); err != nil {
		t.Fatalf("add in a project that names its node: %v\n%s", err, out)
	}

	locked, err := os.ReadFile(filepath.Join(project, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), `\"./packages/node.toml\"`) || strings.Contains(string(locked), project) {
		t.Fatalf("the lock does not name the node relative to the project:\n%s", locked)
	}

	// Another checkout is the same project at another path.
	moved := filepath.Join(m.fixtures, "elsewhere")
	must(t, os.Rename(project, moved))
	must(t, os.RemoveAll(m.data))

	m.opts.WorkDir = moved

	if out, err := m.run(t, "", "sync", "--locked"); err != nil {
		t.Fatalf("sync --locked in the moved project: %v\n%s", err, out)
	}
}
