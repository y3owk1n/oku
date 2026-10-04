package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB527AnUnfinishedChangeKeepsListEditsMadeSince(t *testing.T) {
	for _, tc := range []struct {
		name       string
		committing bool
		wantKept   bool
	}{
		{"before oku wrote the lists", false, true},
		{"while oku wrote the lists", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			tool := m.namedManifest(t, "tool", "tool", "tool")
			m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

			_, err := m.run(t, "", "sync")
			must(t, err)

			listPath := filepath.Join(m.config, "oku.toml")
			list, err := os.ReadFile(listPath)
			must(t, err)
			lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
			must(t, err)

			// What a change leaves when oku is killed in the middle of it.
			saved := fmt.Sprintf("had_list = true\nlist = %q\nhad_lock = true\nlock = %q\n", list, lock)
			must(t, os.WriteFile(filepath.Join(m.data, "pending.toml"), []byte(fmt.Sprintf(
				"from = 1\nto = 1\ncommitting = %t\n[before]\n%s[started]\n%s", tc.committing, saved, saved,
			)), 0o644))

			edited := string(list) + "# edited after the crash\n"
			must(t, os.WriteFile(listPath, []byte(edited), 0o644))

			out, err := m.run(t, "", "sync")
			must(t, err)

			now, err := os.ReadFile(listPath)
			must(t, err)

			if kept := string(now) == edited; kept != tc.wantKept {
				t.Fatalf("want the edit kept %v, oku.toml holds:\n%s\noutput:\n%s", tc.wantKept, now, out)
			}

			if tc.wantKept && !strings.Contains(out, "changed since the last change started") {
				t.Fatalf("sync did not say it kept the list:\n%s", out)
			}
		})
	}
}
