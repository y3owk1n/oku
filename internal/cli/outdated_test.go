package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestB178OutdatedLooksUpThePackagesAtOnce(t *testing.T) {
	m := newMachine(t)

	// Each package reads its version from a page of its own. Once counting is
	// on, the server waits with each page until another one is in flight, for
	// up to half a second, and counts the most in flight at once.
	var (
		mu             sync.Mutex
		counting       bool
		inFlight, most int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		watch := counting
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()

		for deadline := time.Now().Add(500 * time.Millisecond); watch && time.Now().Before(deadline); {
			mu.Lock()
			both := most > 1
			mu.Unlock()

			if both {
				break
			}

			time.Sleep(5 * time.Millisecond)
		}

		mu.Lock()
		inFlight--
		mu.Unlock()

		_, _ = w.Write([]byte("1.0.0"))
	}))
	t.Cleanup(server.Close)

	list := "[packages]\n"

	for _, name := range []string{"one", "two"} {
		archive, sum := m.archive(t, name, map[string]string{name: script})
		list += fmt.Sprintf("%s = %q\n", name, m.rawManifest(t, name, fmt.Sprintf(
			"[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nbin = [%q]\n", archive, sum, name,
		)))

		// rawManifest pins the version. A page source takes its place.
		path := filepath.Join(m.fixtures, name+".toml")
		data, err := os.ReadFile(path)
		must(t, err)
		must(t, os.WriteFile(path, []byte(strings.Replace(string(data), "value = \"1.2.3\"\n",
			fmt.Sprintf("from = \"page\"\nrepo = \"%s/%s\"\nregex = '([0-9.]+)'\n", server.URL, name), 1)), 0o644))
	}

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(list), 0o644))

	_, err := m.run(t, "", "sync")
	must(t, err)

	outdated := func() int {
		mu.Lock()
		counting, most = true, 0
		mu.Unlock()

		out, err := m.run(t, "", "outdated")
		if err != nil {
			t.Fatalf("outdated: %v\n%s", err, out)
		}

		mu.Lock()
		defer mu.Unlock()

		return most
	}

	if n := outdated(); n != 2 {
		t.Fatalf("without OKU_PARALLEL outdated looked up %d packages at once, want 2", n)
	}

	t.Setenv("OKU_PARALLEL", "1")

	if n := outdated(); n != 1 {
		t.Fatalf("with OKU_PARALLEL=1 outdated looked up %d packages at once, want 1", n)
	}
}
