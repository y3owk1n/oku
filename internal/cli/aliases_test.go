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

	// Each alias below proves it ran the command by what the command prints.
	_, err = m.run(t, "", "source", "add", "core", "github:someone/recipes")
	must(t, err)

	cache := t.TempDir()

	_, err = m.run(t, "", "cache", "add", cache)
	must(t, err)

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"source", "ls"}, "core  github:someone/recipes"},
		{[]string{"source", "rm", "core"}, "removed core"},
		{[]string{"cache", "rm", cache}, "removed cache " + cache},
		{[]string{"cache", "ls"}, "no caches, add one"},
		{[]string{"key", "ls"}, "no keys yet"},
		{[]string{"service", "ls"}, "no installed package ships a service"},
	} {
		out, err := m.run(t, "", c.args...)
		must(t, err)

		if !strings.Contains(out, c.want) {
			t.Fatalf("%v should print %q:\n%s", c.args, c.want, out)
		}
	}

	// Both flags fail before oku looks for a release, so no binary changes.
	_, err = m.run(t, "", "self", "upgrade", "--nightly", "--release")
	if err == nil || !strings.Contains(err.Error(), "exclude each other") {
		t.Fatalf("self upgrade should run self update and refuse both flags, got %v", err)
	}

	_, err = m.run(t, "", "rm", "tool")
	must(t, err)

	if exists(m.profile("bin", "tool")) {
		t.Fatal("rm left tool in the profile")
	}

	_, err = m.run(t, "", "install", m.namedManifest(t, "tool", "tool", "tool"))
	must(t, err)

	_, err = m.run(t, "", "uninstall", "tool")
	must(t, err)

	if exists(m.profile("bin", "tool")) {
		t.Fatal("uninstall left tool in the profile")
	}
}
