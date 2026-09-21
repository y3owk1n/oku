package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readingManifest writes a manifest whose build installs a program that prints
// the text of a file outside the build, as it was at build time. A file named
// fail beside it makes the build fail. A build may read outside its directory
// and may not write there.
func (m machine) readingManifest(t *testing.T) (string, string) {
	t.Helper()

	dir := filepath.Join(m.fixtures, "input")
	must(t, os.MkdirAll(dir, 0o755))

	return m.rawManifest(t, "tool", fmt.Sprintf(`[build]
[[build.step]]
run = """
test ! -e %[1]s/fail
printf '#!/bin/sh\necho %%s\n' "$(cat %[1]s/text)" > tool
chmod +x tool
"""
shell = "sh"
[[build.step]]
install = { bin = ["tool"] }
`, dir)), dir
}

func TestB191RebuildBuildsAPackageAgainAndKeepsTheOldBuildWhenItFails(t *testing.T) {
	m := newMachine(t)
	ref, dir := m.readingManifest(t)
	text := filepath.Join(dir, "text")

	must(t, os.WriteFile(text, []byte("first"), 0o644))

	_, err := m.run(t, "", "add", ref, "--yes")
	must(t, err)

	// The store holds the build, so a plain sync builds nothing.
	must(t, os.WriteFile(text, []byte("second"), 0o644))

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.output(t, "tool"); got != "first" {
		t.Fatalf("a plain sync built again, tool printed %q", got)
	}

	out, err := m.run(t, "", "sync", "--yes", "--rebuild", "tool")
	if err != nil || !strings.Contains(out, "tool 1.2.3, built again") {
		t.Fatalf("sync --rebuild: %v\n%s", err, out)
	}

	if got := m.output(t, "tool"); got != "second" {
		t.Fatalf("sync --rebuild did not build again, tool printed %q", got)
	}

	// A rebuild that fails leaves the program that was there.
	must(t, os.WriteFile(filepath.Join(dir, "fail"), nil, 0o644))

	if _, err := m.run(t, "", "sync", "--yes", "--rebuild", "tool"); err == nil {
		t.Fatal("a rebuild whose build fails reported no error")
	}

	if got := m.output(t, "tool"); got != "second" {
		t.Fatalf("a failed rebuild lost the old build, tool printed %q", got)
	}

	for _, entry := range m.storeEntries(t) {
		if strings.HasSuffix(entry, ".old") {
			t.Fatalf("a failed rebuild left %s in the store", entry)
		}
	}
}

func TestB191RebuildRefusesADownload(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	_, err = m.run(t, "", "sync", "--rebuild", "tool")
	if err == nil || !strings.Contains(err.Error(), "--rebuild is for a package that oku builds") {
		t.Fatalf("want an error that says tool is a download, got %v", err)
	}
}
