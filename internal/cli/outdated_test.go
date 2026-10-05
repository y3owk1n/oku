package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/cli"
)

// stdout runs oku and returns what it printed on stdout alone, where the waits
// that stderr shows are not.
func (m machine) stdout(t *testing.T, args ...string) string {
	t.Helper()

	var out bytes.Buffer

	cmd := cli.NewRootCmd(m.opts)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	must(t, cmd.Execute())

	return out.String()
}

func TestB262OutdatedListsWhatHasANewerVersionAndChangesNothing(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	if out, err := m.run(t, "", "outdated"); err != nil || !strings.Contains(out, "oku.lock holds no packages") {
		t.Fatalf("outdated with an empty lock: %v\n%s", err, out)
	}

	ref := m.discoveredManifest(t, "1.0.0", "1.1.0")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	out, err := m.run(t, "", "outdated")
	if err != nil || !strings.Contains(out, "all 1 packages are at their newest version") {
		t.Fatalf("outdated with nothing newer: %v\n%s", err, out)
	}

	// oku claims nothing about a package whose lookup fails.
	server.tags = nil

	out, err = m.run(t, "", "outdated")
	if err == nil || !strings.Contains(err.Error(), "tool: github-releases owner/tool has no versions") ||
		strings.Contains(out, "newest version") {
		t.Fatalf("outdated whose lookup failed: %v\n%s", err, out)
	}

	server.tags = []string{"v1.1.0", "v1.0.0"}

	lockBefore, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	out = m.stdout(t, "outdated", "--json")

	var rows []struct{ Name, Version, Newest, Latest, Ref string }
	must(t, json.Unmarshal([]byte(out), &rows))

	if len(rows) != 1 || rows[0].Name != "tool" || rows[0].Version != "1.0.0" ||
		rows[0].Newest != "1.1.0" || rows[0].Latest != "1.1.0" || rows[0].Ref != ref {
		t.Fatalf("outdated --json gave %+v", rows)
	}

	lockAfter, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if string(lockAfter) != string(lockBefore) || m.toolOutput(t) != "1.0.0" {
		t.Fatal("outdated changed the lock or the profile")
	}

	// An exact version in the list holds update back, and the latest release
	// still shows.
	_, err = m.run(t, "", "add", ref+"@1.0.0")
	must(t, err)

	out, err = m.run(t, "", "outdated")
	if err != nil || !strings.Contains(out, "1.0.0") || !strings.Contains(out, "1.1.0") || !strings.Contains(out, "latest") {
		t.Fatalf("outdated with tool pinned to 1.0.0 and 1.1.0 released:\n%s", out)
	}
}
