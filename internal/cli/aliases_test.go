package cli_test

import (
	"strings"
	"testing"
)

func TestB492CommonAliasesRunTheirCommands(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "install", m.namedManifest(t, "tool", "tool", "tool"))
	must(t, err)

	if !exists(m.profile("bin", "tool")) {
		t.Fatal("install did not add tool")
	}

	for _, args := range [][]string{{"ls"}, {"show", "tool"}, {"ls", "--json"}} {
		out, err := m.run(t, "", args...)
		must(t, err)

		if !strings.Contains(out, "tool") {
			t.Fatalf("%v should print tool:\n%s", args, out)
		}
	}

	_, err = m.run(t, "", "upgrade")
	must(t, err)

	_, err = m.run(t, "", "source", "add", "core", "github:someone/recipes")
	must(t, err)

	out, err := m.run(t, "", "source", "ls")
	must(t, err)

	if !strings.Contains(out, "core") {
		t.Fatalf("source ls:\n%s", out)
	}

	_, err = m.run(t, "", "source", "rm", "core")
	must(t, err)

	for _, args := range [][]string{{"cache", "ls"}, {"key", "ls"}, {"service", "ls"}} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	_, err = m.run(t, "", "uninstall", "tool")
	must(t, err)

	if exists(m.profile("bin", "tool")) {
		t.Fatal("uninstall left tool in the profile")
	}
}
