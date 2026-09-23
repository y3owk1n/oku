package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/lock"
)

// lockedVersion returns the version that the global lock pins for name.
func (m machine) lockedVersion(t *testing.T, name string) string {
	t.Helper()

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	pkg, ok := locked.Find(name)
	if !ok {
		t.Fatalf("the lock has no %s", name)
	}

	return pkg.Version
}

func TestB265AListVersionIsARangeOrAPrefixThatSyncAndUpdateFollow(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.4.0", "v1.4.9", "v1.5.0", "v2.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	data := m.dataDep(t)

	out, err := m.run(t, "", "add", data+"@^1.4", "--yes")
	if err != nil {
		t.Fatalf("add @^1.4: %v\n%s", err, out)
	}

	if got := m.lockedVersion(t, "data"); got != "1.5.0" {
		t.Fatalf("add @^1.4 took %s, want 1.5.0, the newest below 2", got)
	}

	// sync stays on the locked version while the range allows it.
	server.tags = append(server.tags, "v1.6.0")

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "data"); got != "1.5.0" {
		t.Fatalf("sync moved to %s, want the locked 1.5.0", got)
	}

	_, err = m.run(t, "", "update", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "data"); got != "1.6.0" {
		t.Fatalf("update took %s, want 1.6.0, the newest below 2", got)
	}

	// 2.0.0 is newer, but the range does not allow it.
	if out := m.stdout(t, "outdated", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("outdated with ^1.4 at 1.6.0 lists a package:\n%s", out)
	}

	// A range that no longer allows the locked version makes sync pick again.
	setVersion := func(version string) {
		t.Helper()

		must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
			"[packages]\ndata = { ref = %q, version = %q }\n", data, version,
		)), 0o644))
	}

	setVersion("~1.4")

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "data"); got != "1.4.9" {
		t.Fatalf("sync with ~1.4 took %s, want 1.4.9", got)
	}

	// A version that is no release is a prefix.
	server.tags = append(server.tags, "v2.1.0")
	setVersion("2")

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "data"); got != "2.1.0" {
		t.Fatalf("sync with 2 took %s, want 2.1.0, the newest 2.x", got)
	}

	server.tags = append(server.tags, "v2.2.0")

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "data"); got != "2.1.0" {
		t.Fatalf("sync moved to %s, want the locked 2.1.0, which 2 allows", got)
	}
}

func TestB265AnInferredPackageTakesTheNewestVersionInTheRange(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0", "1.2.0", "2.0.0")

	out, err := m.run(t, "", "add", "npm:@scope/tool@^1.1", "--yes")
	if err != nil {
		t.Fatalf("add @^1.1: %v\n%s", err, out)
	}

	if got := m.lockedVersion(t, "tool"); got != "1.2.0" {
		t.Fatalf("add @^1.1 took %s, want 1.2.0, the newest below 2", got)
	}

	_, err = m.run(t, "", "add", "npm:@scope/tool@1", "--yes")
	must(t, err)

	if got := m.lockedVersion(t, "tool"); got != "1.2.0" {
		t.Fatalf("add @1 took %s, want 1.2.0, the newest 1.x", got)
	}
}

func TestB266ATagWithOrWithoutAVIsAVersion(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.4.0", "1.3.0", "1.2.0", "v1.2.0")
	m.opts.GitHubAPI = server.URL + "/api"

	data := m.dataDep(t)

	out, err := m.run(t, "", "add", data+"@1.3.0", "--yes")
	if err != nil {
		t.Fatalf("add a version whose tag has no v: %v\n%s", err, out)
	}

	if got := m.lockedVersion(t, "data"); got != "1.3.0" {
		t.Fatalf("add @1.3.0 took %s", got)
	}

	_, err = m.run(t, "", "add", data+"@1.2.0", "--yes")
	must(t, err)

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if pkg, _ := locked.Find("data"); pkg.Tag != "v1.2.0" {
		t.Fatalf("1.2.0 came from the tag %q, want v1.2.0, the form strip_prefix names", pkg.Tag)
	}
}

func TestB265ADepReadsABareVersionAsAPrefix(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0", "v1.5.0", "v2.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	m.dataDep(t)

	// A [runtimes] entry with a version becomes a dep with this constraint too.
	out, err := m.run(t, "", "add", m.dataUser(t, "user", "1"), "--yes")
	if err != nil {
		t.Fatalf("add a package whose dep is data 1: %v\n%s", err, out)
	}

	if got := m.output(t, "user"); got != "1.5.0" {
		t.Fatalf("the dep data 1 was %s, want 1.5.0, the newest 1.x", got)
	}
}
