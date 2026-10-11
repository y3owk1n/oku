package cli_test

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"
)

// manSection is man1 in share/man beside the bin directory of m's binary.
func (m machine) manSection() string {
	return filepath.Join(filepath.Dir(filepath.Dir(m.exe)), "share", "man", "man1")
}

// writePage writes a page named name to dir, whose .TH line names manual.
func writePage(t *testing.T, dir, name, manual string) string {
	t.Helper()

	must(t, os.MkdirAll(dir, 0o755))

	path := filepath.Join(dir, name)
	must(t, os.WriteFile(path, []byte(`.TH "PAGE" 1 "2026-01-01" "x" "`+manual+"\"\n"), 0o644))

	return path
}

func TestB584SelfManWritesAPageForEachCommandBesideTheBinary(t *testing.T) {
	m := newMachine(t)
	section := m.manSection()

	// A page of a command that an older oku had, and a page of another program.
	stale := writePage(t, section, "oku-gone.1", "oku Manual")
	other := writePage(t, section, "oku-other.1", "other Manual")

	out, err := m.run(t, "", "self", "man")
	must(t, err)

	if !strings.Contains(out, "man pages to "+section) {
		t.Fatalf("self man does not say where the pages went:\n%s", out)
	}

	page, err := os.ReadFile(filepath.Join(section, "oku-self-update.1"))
	must(t, err)

	for _, want := range []string{`.TH "OKU\-SELF\-UPDATE" 1`, `"oku test" "oku Manual"`, `\fB\-\-check\fR`} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("oku-self-update.1 lacks %q:\n%s", want, page)
		}
	}

	// The synopsis keeps what --help prints, angle brackets too.
	page, err = os.ReadFile(filepath.Join(section, "oku-add.1"))
	must(t, err)

	for _, want := range []string{`\fBoku add <ref>[@version]... [flags]\fR`, `\fB\-h\fR, \fB\-\-help\fR`} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("oku-add.1 lacks %q of its --help:\n%s", want, page)
		}
	}

	if !exists(filepath.Join(section, "oku.1")) {
		t.Fatal("self man wrote no oku.1")
	}

	if exists(stale) || !exists(other) {
		t.Fatal("self man should remove oku's old pages and leave another program's")
	}

	dir := filepath.Join(t.TempDir(), "man")

	_, err = m.run(t, "", "self", "man", "--dir", dir)
	must(t, err)

	if !exists(filepath.Join(dir, "man1", "oku.1")) {
		t.Fatal("--dir did not get the pages")
	}
}

func TestB585SelfUpdateHasTheNewBinaryWriteItsManPages(t *testing.T) {
	m := newMachine(t)
	section := m.manSection()
	stale := writePage(t, section, "oku-gone.1", "oku Manual")

	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	m.opts.ReleaseKey = public.String()

	args := filepath.Join(m.fixtures, "args")
	m.releaseWith(t, "#!/bin/sh\necho \"$@\" > '"+args+"'\n", "v1.4.0", secret)

	_, err = m.run(t, "", "self", "update")
	must(t, err)

	if got, _ := os.ReadFile(args); strings.TrimSpace(string(got)) != "self man" {
		t.Fatalf("self update should run the new binary with self man, it ran it with %q", got)
	}

	if exists(stale) {
		t.Fatal("self update left the old binary's pages")
	}

	// A new binary that cannot write its pages still replaces the old one.
	m = newMachine(t)
	m.opts.ReleaseKey = public.String()
	m.releaseWith(t, "#!/bin/sh\necho 'no pages here' >&2\nexit 1\n", "v1.4.0", secret)

	out, err := m.run(t, "", "self", "update")
	must(t, err)

	if !strings.Contains(out, "to 1.4.0") || !strings.Contains(out, "could not write the man pages") ||
		!strings.Contains(out, "no pages here") {
		t.Fatalf("self update should replace the binary and say why the pages are missing:\n%s", out)
	}
}

func TestB586UninstallRemovesOkusManPages(t *testing.T) {
	m := newMachine(t)
	section := m.manSection()

	_, err := m.run(t, "", "self", "man")
	must(t, err)

	other := writePage(t, section, "oku-other.1", "other Manual")

	out, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if !strings.Contains(out, "oku's pages in "+section) {
		t.Fatalf("uninstall does not list the man pages:\n%s", out)
	}

	if exists(filepath.Join(section, "oku.1")) || !exists(other) {
		t.Fatal("uninstall should remove oku's pages and leave another program's")
	}

	// With only oku's pages there, the directories go too.
	must(t, os.Remove(other))

	_, err = m.run(t, "", "self", "man")
	must(t, err)

	_, err = m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if exists(filepath.Dir(section)) {
		t.Fatalf("uninstall left %s", filepath.Dir(section))
	}
}
