package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestB528WhatAKilledOkuLeftHalfDoneIsNotUsed(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	// A generation an older oku left without its state file.
	half := filepath.Join(m.data, "profiles", "global", "gen-7")
	must(t, os.MkdirAll(half, 0o755))

	if out, err := m.run(t, "", "generations"); err != nil {
		t.Fatalf("generations failed on a generation without its state file: %v\n%s", err, out)
	}

	_, err = m.run(t, "", "gc")
	must(t, err)

	if exists(half) {
		t.Fatal("gc kept a generation without its state file")
	}

	// A store path that a delete left half done: its meta file and download gone.
	var path string

	for _, entry := range m.storeEntries(t) {
		if strings.HasPrefix(entry, "tool-") {
			path = filepath.Join(m.data, "store", entry)
		}
	}

	must(t, writable(path))
	must(t, os.Remove(filepath.Join(path, "oku-meta.toml")))
	must(t, os.RemoveAll(filepath.Join(path, "pkg")))

	_, err = m.run(t, "", "remove", "tool")
	must(t, err)

	if out, err := m.run(t, "", "add", tool); err != nil {
		t.Fatalf("add reused a half deleted store path: %v\n%s", err, out)
	}

	if _, err := exec.Command(m.profile("bin", "tool")).Output(); err != nil {
		t.Fatalf("tool does not run: %v", err)
	}
}
