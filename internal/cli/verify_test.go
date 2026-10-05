package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolPath returns the store path of the package tool.
func (m machine) toolPath(t *testing.T) string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(m.data, "store", "tool-*"))
	must(t, err)

	if len(paths) != 1 {
		t.Fatalf("want one store path of tool, got %v", paths)
	}

	return paths[0]
}

func TestB511VerifyNamesTheFilesThatChangedAndRepairRemovesThem(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script, "doc/readme": "hi"}, `bin = ["tool"]`)
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

	_, err := m.run(t, "", "sync")
	must(t, err)

	out, err := m.run(t, "", "verify")
	if err != nil || !strings.Contains(out, "tool 1.2.3") {
		t.Fatalf("want an unchanged package to pass, got %v\n%s", err, out)
	}

	// The store is read-only, so the test makes it writable to change it.
	pkg := filepath.Join(m.toolPath(t), "pkg")
	must(t, writable(pkg))
	readme := filepath.Join(pkg, "doc", "readme")
	must(t, os.Chmod(readme, 0o644))
	must(t, os.WriteFile(readme, []byte("changed"), 0o644))
	must(t, os.WriteFile(filepath.Join(pkg, "extra"), []byte("x"), 0o644))

	// Python writes bytecode beside the code it runs, which is no change.
	must(t, os.MkdirAll(filepath.Join(pkg, "__pycache__"), 0o755))
	must(t, os.WriteFile(filepath.Join(pkg, "__pycache__", "tool.pyc"), []byte("x"), 0o644))

	out, err = m.run(t, "", "verify", "--json")
	if err == nil || !strings.Contains(err.Error(), "--repair") {
		t.Fatalf("want verify to fail and name --repair, got %v", err)
	}

	var results []struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Changes []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
	}
	must(t, json.Unmarshal([]byte(out[strings.Index(out, "["):]), &results))

	if len(results) != 1 || results[0].Status != "changed" || len(results[0].Changes) != 2 ||
		results[0].Changes[0] != (struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		}{"pkg/doc/readme", "changed"}) || results[0].Changes[1].Path != "pkg/extra" ||
		results[0].Changes[1].Kind != "added" {
		t.Fatalf("want the changed and the added file named, and no __pycache__, got %+v", results)
	}

	_, err = m.run(t, "", "verify", "--repair")
	must(t, err)

	if left, _ := filepath.Glob(filepath.Join(m.data, "store", "tool-*")); len(left) != 0 {
		t.Fatalf("--repair left the changed package in the store: %v", left)
	}

	_, err = m.run(t, "", "sync")
	must(t, err)

	if out, err := m.run(t, "", "verify"); err != nil {
		t.Fatalf("want the package installed again to pass, got %v\n%s", err, out)
	}
}

func TestB512VerifyNotesAPackageWithoutARecord(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

	_, err := m.run(t, "", "sync")
	must(t, err)

	// The store is read-only, which the test undoes to delete the record.
	record := filepath.Join(m.toolPath(t), "oku-tree.txt")
	must(t, writable(m.toolPath(t)))
	must(t, os.Chmod(record, 0o644))
	must(t, os.Remove(record))

	out, err := m.run(t, "", "verify")
	if err != nil || !strings.Contains(out, "cannot check it") {
		t.Fatalf("want a note for a package without a record, got %v\n%s", err, out)
	}
}

func TestB515VerifyRecordRecordsAPackageWithoutARecord(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

	_, err := m.run(t, "", "sync")
	must(t, err)

	record := filepath.Join(m.toolPath(t), "oku-tree.txt")
	must(t, writable(m.toolPath(t)))
	must(t, os.Chmod(record, 0o644))
	must(t, os.Remove(record))

	out, err := m.run(t, "", "verify", "--record")
	if err != nil || !strings.Contains(out, "recorded its files") {
		t.Fatalf("want --record to record the package, got %v\n%s", err, out)
	}

	// The next run checks it, and a change shows.
	must(t, writable(m.toolPath(t)))
	must(t, os.WriteFile(filepath.Join(m.toolPath(t), "pkg", "extra"), []byte("x"), 0o644))

	if _, err := m.run(t, "", "verify", "--record"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("want --record to leave a recorded package checked, got %v", err)
	}
}

func TestB517APackageIsReadOnlyOnceInstalled(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

	_, err := m.run(t, "", "sync")
	must(t, err)

	pkg := filepath.Join(m.toolPath(t), "pkg")
	if err := os.WriteFile(filepath.Join(pkg, "cache"), []byte("x"), 0o644); err == nil {
		t.Fatal("a program could write into its package")
	}

	// oku still removes it.
	m.writeFilesList(t, "[packages]\n")

	_, err = m.run(t, "", "sync")
	must(t, err)

	_, err = m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	if left, _ := filepath.Glob(filepath.Join(m.data, "store", "tool-*")); len(left) != 0 {
		t.Fatalf("gc left the read-only package: %v", left)
	}
}
