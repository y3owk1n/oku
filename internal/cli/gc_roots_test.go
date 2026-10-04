package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestB530GCKeepsWhatTheLedgerAndRunningSessionsUse(t *testing.T) {
	m := newMachine(t)

	// Three packages that no generation holds once each is removed.
	paths := map[string]string{}

	for _, name := range []string{"exposed", "session", "ended"} {
		_, err := m.run(t, "", "add", m.namedManifest(t, name, name, name))
		must(t, err)

		for _, entry := range m.storeEntries(t) {
			if strings.HasPrefix(entry, name+"-") {
				paths[name] = filepath.Join(m.data, "store", entry)
			}
		}

		_, err = m.run(t, "", "remove", name)
		must(t, err)
	}

	// A service that a declined system change left in the ledger runs the first.
	must(t, os.WriteFile(filepath.Join(m.data, "exposed.toml"), []byte(fmt.Sprintf(
		"[[item]]\nkind = \"service\"\npackage = \"exposed\"\nsource = %q\ntarget = \"/etc/systemd/system/oku-exposed.service\"\nsystem = true\n",
		filepath.Join(paths["exposed"], "bin", "exposed"),
	)), 0o644))

	// An oku shell that still runs uses the second, and one that ended the third.
	sessions := filepath.Join(m.data, "sessions")
	must(t, os.MkdirAll(sessions, 0o755))
	must(t, os.WriteFile(filepath.Join(sessions, strconv.Itoa(os.Getpid())), []byte(paths["session"]+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(sessions, "999999"), []byte(paths["ended"]+"\n"), 0o644))

	out, err := m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	for name, want := range map[string]bool{"exposed": true, "session": true, "ended": false} {
		if exists(paths[name]) != want {
			t.Fatalf("want the store path of %s kept %v:\n%s", name, want, out)
		}
	}

	if exists(filepath.Join(sessions, "999999")) {
		t.Fatal("gc kept the record of a session that ended")
	}
}
