package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/status"
)

func TestB344DuSizesEachAreaAndWhatGCFrees(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.namedManifest(t, "keep", "keep", "keep"))
	must(t, err)

	// A store path no generation uses, and a hard link to a store file.
	leftover := filepath.Join(m.data, "store", "left-1.0.0-0123456789abcdef")
	must(t, os.MkdirAll(leftover, 0o755))
	must(t, os.WriteFile(filepath.Join(leftover, "blob"), make([]byte, 5000), 0o644))
	must(t, os.MkdirAll(filepath.Join(m.data, "extra"), 0o755))
	must(t, os.Link(filepath.Join(leftover, "blob"), filepath.Join(m.data, "extra", "blob")))

	out := m.stdout(t, "du", "--json")

	var got struct {
		Areas []struct {
			Area  string   `json:"area"`
			Paths []string `json:"paths"`
			Bytes int64    `json:"bytes"`
		} `json:"areas"`
		Total   int64 `json:"total"`
		GCFrees int64 `json:"gc_frees"`
	}
	must(t, json.Unmarshal([]byte(out), &got))

	bytes := map[string]int64{}

	var sum int64

	for _, a := range got.Areas {
		bytes[a.Area] = a.Bytes
		sum += a.Bytes
	}

	if got.GCFrees != 5000 || bytes["store"] <= 5000 || got.Total != sum {
		t.Fatalf("du --json = %s", out)
	}

	// The hard link in the data directory is the store's file, counted once.
	if bytes["other"] >= 5000 {
		t.Fatalf("other counts the hard linked file again: %s", out)
	}

	freed, err := m.run(t, "", "gc")
	must(t, err)

	if !strings.Contains(freed, "freed "+status.Size(got.GCFrees)) {
		t.Fatalf("gc freed other than du said, %s:\n%s", status.Size(got.GCFrees), freed)
	}
}

func TestB345DuPackagesSaysWhatKeepsEachStorePath(t *testing.T) {
	m := newMachine(t)

	for _, args := range [][]string{
		{"add", m.namedManifest(t, "keep", "keep", "keep")},
		{"add", m.namedManifest(t, "gone", "gone", "gone")},
		{"remove", "gone"},
	} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	must(t, os.MkdirAll(filepath.Join(m.data, "store", "left-1.0.0-0123456789abcdef"), 0o755))

	out := m.stdout(t, "du", "--packages", "--json")

	var rows []struct {
		Name     string   `json:"name"`
		Version  string   `json:"version"`
		Profiles []string `json:"profiles"`
		Old      bool     `json:"old"`
		Unused   bool     `json:"unused"`
	}
	must(t, json.Unmarshal([]byte(out), &rows))

	state := map[string]string{}

	for _, r := range rows {
		switch {
		case r.Unused:
			state[r.Name] = "unused"
		case r.Old:
			state[r.Name] = "old " + strings.Join(r.Profiles, ",")
		default:
			state[r.Name] = "active " + strings.Join(r.Profiles, ",") + " " + r.Version
		}
	}

	want := map[string]string{
		"keep":                        "active global 1.2.3",
		"gone":                        "old global",
		"left-1.0.0-0123456789abcdef": "unused",
	}
	if len(state) != len(want) {
		t.Fatalf("du --packages = %v, want %v", state, want)
	}

	for name, s := range want {
		if state[name] != s {
			t.Fatalf("du --packages = %v, want %v", state, want)
		}
	}
}
