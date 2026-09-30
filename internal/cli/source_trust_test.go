package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// untrustedProject is a checkout of a project whose oku.toml names an npm
// package of a scope this machine has not trusted, with a node of its own.
func untrustedProject(t *testing.T) machine {
	t.Helper()

	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	project := filepath.Join(m.fixtures, "project")
	must(t, os.MkdirAll(filepath.Join(project, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(project, "packages", "node.toml"), node, 0o644))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte(
		"[runtimes]\nnode = \"./packages/node.toml\"\n[packages]\ntool = \"npm:@scope/tool\"\n",
	), 0o644))

	m.opts.WorkDir = project

	return m
}

func TestB471AProjectAsksBeforeItInstallsFromAnUntrustedSource(t *testing.T) {
	m := untrustedProject(t)

	out, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "npm:@scope for tool") ||
		!strings.Contains(err.Error(), "oku allow") {
		t.Fatalf("want sync without a terminal refused, naming the source, got %v:\n%s", err, out)
	}

	m.opts.Interactive = yes()

	if out, err := m.run(t, "n\n", "sync"); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("want a no to install nothing, got %v:\n%s", err, out)
	}

	if out, err := m.run(t, "y\n", "sync"); err != nil {
		t.Fatalf("a yes should install: %v\n%s", err, out)
	}

	// The answer is kept, so the next run does not ask.
	no := false
	m.opts.Interactive = &no

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("a trusted source should not ask again: %v\n%s", err, out)
	}
}

func TestB472AllowConfigAndYesTrustAProjectsSources(t *testing.T) {
	m := untrustedProject(t)
	if out, err := m.run(t, "", "allow"); err != nil || !strings.Contains(out, "trusted npm:@scope") {
		t.Fatalf("allow should trust the project's sources: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync after allow: %v\n%s", err, out)
	}

	m = untrustedProject(t)
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte("[trust]\nsources = [\"npm:@scope\"]\n"), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync with the scope in config.toml: %v\n%s", err, out)
	}

	m = untrustedProject(t)
	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync --yes: %v\n%s", err, out)
	}
}

func TestB473TheGlobalListNeedsNoTrust(t *testing.T) {
	m := untrustedProject(t)
	project := m.opts.WorkDir

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.Rename(filepath.Join(project, "packages"), filepath.Join(m.config, "packages")))
	must(t, os.Rename(filepath.Join(project, "oku.toml"), filepath.Join(m.config, "oku.toml")))

	m.opts.WorkDir = m.fixtures

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("the global list is the user's own, so it should sync: %v\n%s", err, out)
	}
}

func TestB474ReadingAnUntrustedProjectRefuses(t *testing.T) {
	m := untrustedProject(t)

	if out, err := m.run(t, "", "outdated"); err == nil || !strings.Contains(err.Error(), "oku allow") {
		t.Fatalf("want outdated refused in an untrusted project, got %v:\n%s", err, out)
	}
}
