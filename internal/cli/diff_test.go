package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB577DiffComparesGenerationsAndLocks(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"one", "two"} {
		if out, err := m.run(t, "", "add", m.namedManifest(t, name, name, name)); err != nil {
			t.Fatalf("add %s: %v\n%s", name, err, out)
		}
	}

	// Without a number, the active generation against the one it replaced.
	out, err := m.run(t, "", "diff")
	if err != nil || !strings.Contains(out, "two") || strings.Contains(out, "one") {
		t.Fatalf("diff should name two alone: %v\n%s", err, out)
	}

	out, err = m.run(t, "", "diff", "1", "2", "--json")
	if err != nil {
		t.Fatalf("diff 1 2 --json: %v\n%s", err, out)
	}

	type row struct{ Kind, Name, Change, Before, After string }

	var rows []row
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 ||
		rows[0] != (row{"package", "two", "added", "", "1.2.3"}) {
		t.Fatalf("diff 1 2 --json printed %+v (%v)\n%s", rows, err, out)
	}

	if out, err := m.run(t, "", "diff", "2", "2"); err != nil || !strings.Contains(out, "no change") {
		t.Fatalf("a generation against itself: %v\n%s", err, out)
	}

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		must(t, os.WriteFile(path, []byte(body), 0o644))

		return path
	}

	before := write("before.lock", `
[[include]]
ref = 'github:you/machines'
commit = 'aaaaaaaaaaaaaaaa'
sha256 = 'x'

[[package]]
name = 'kept'
ref = 'github:o/kept'
manifest_sha256 = 'a'
version = '1.0.0'
[package.platform.darwin-arm64]
strategy = 'artifact'
[package.platform.linux-amd64-glibc]
strategy = 'artifact'
version = '1.0.0'
[[package.dep]]
name = 'lib'
ref = 'github:o/lib'
manifest_sha256 = 'a'
version = '2.0.0'

[[package]]
name = 'gone'
ref = 'github:o/gone'
manifest_sha256 = 'a'
version = '3.0.0'

[[package]]
name = 'bumped'
ref = 'github:o/bumped'
manifest_sha256 = 'a'
version = '1.0.0'

[[package]]
name = 'resigned'
ref = 'github:o/resigned'
manifest_sha256 = 'a'
version = '1.0.0'
`)
	after := write("after.lock", `
[[include]]
ref = 'github:you/machines'
commit = 'bbbbbbbbbbbbbbbb'
sha256 = 'y'

[[package]]
name = 'kept'
ref = 'github:o/kept'
manifest_sha256 = 'a'
version = '1.0.0'
[package.platform.darwin-arm64]
strategy = 'artifact'
[package.platform.linux-amd64-glibc]
strategy = 'artifact'
version = '1.1.0'
[[package.dep]]
name = 'lib'
ref = 'github:o/lib'
manifest_sha256 = 'a'
version = '2.1.0'

[[package]]
name = 'bumped'
ref = 'github:o/bumped'
manifest_sha256 = 'b'
version = '1.2.0'

[[package]]
name = 'resigned'
ref = 'github:o/resigned'
manifest_sha256 = 'b'
version = '1.0.0'

[[package]]
name = 'new'
ref = 'github:o/new'
manifest_sha256 = 'a'
version = '0.1.0'
`)

	out, err = m.run(t, "", "diff", before, after, "--markdown")
	if err != nil {
		t.Fatalf("diff of two locks: %v\n%s", err, out)
	}

	for _, line := range []string{
		"| changed | list | `github:you/machines` | aaaaaaaaaaaa | bbbbbbbbbbbb |",
		"| removed | package | `gone` | 3.0.0 |  |",
		"| changed | package | `kept` | 1.0.0 | 1.1.0, on linux-amd64-glibc |",
		"| changed | dep | `kept github:o/lib` | 2.0.0 | 2.1.0 |",
		"| changed | package | `bumped` | 1.0.0 | 1.2.0 |",
		"| changed | package | `resigned` | 1.0.0 | 1.0.0, repinned |",
		"| added | package | `new` |  | 0.1.0 |",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("the Markdown should hold %q:\n%s", line, out)
		}
	}

	// One lock compares with the list's, and a directory means its oku.lock.
	must(t, os.Rename(after, filepath.Join(dir, "oku.lock")))

	if out, err := m.run(t, "", "diff", before, dir); err != nil || !strings.Contains(out, "bumped") {
		t.Fatalf("diff with a directory: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "diff", before); err != nil || !strings.Contains(out, "one") ||
		!strings.Contains(out, "gone") {
		t.Fatalf("diff with the list's lock should add one and two and remove the rest: %v\n%s", err, out)
	}

	for args, want := range map[[2]string]string{
		{"1", before}:   "not one of each",
		{"9", ""}:       "generation 9 does not exist",
		{"missing", ""}: "neither a generation number nor a lock file",
	} {
		cmd := []string{"diff", args[0]}
		if args[1] != "" {
			cmd = append(cmd, args[1])
		}

		if _, err := m.run(t, "", cmd...); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("diff %v should fail with %q, got %v", args, want, err)
		}
	}
}
