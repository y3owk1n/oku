package cli_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/oku/internal/clone"
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

func TestB380DuSaysAClonedAppSharesTheStoresBlocks(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	app, _ := m.exposedPaths()
	note := "shares its blocks with the store"
	out := m.stdout(t, "du")

	if clone.Possible(filepath.Join(m.data, "store"), filepath.Dir(app)) {
		if !strings.Contains(out, note) {
			t.Fatalf("du said nothing about the clone:\n%s", out)
		}

		return
	}

	if strings.Contains(out, note) {
		t.Fatalf("du claims a clone on a filesystem that cannot clone:\n%s", out)
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

func TestB348GCCacheDeletesDownloadsNoKeptStorePathWasMadeFrom(t *testing.T) {
	m := newMachine(t)
	_, keepSum := m.archive(t, "keep", map[string]string{"keep": script})
	_, goneSum := m.archive(t, "gone", map[string]string{"gone": script})

	for _, args := range [][]string{
		{"add", m.namedManifest(t, "keep", "keep", "keep")},
		{"add", m.namedManifest(t, "gone", "gone", "gone")},
		{"remove", "gone"},
	} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	// A download less than a day old may belong to a run that has not written
	// its lock yet, so the cache is made two days old, but for one new file.
	downloads := filepath.Join(m.cache, "downloads")
	old := time.Now().Add(-48 * time.Hour)

	must(t, filepath.WalkDir(downloads, func(path string, _ fs.DirEntry, err error) error {
		must(t, err)

		return os.Chtimes(path, old, old)
	}))

	fresh := filepath.Join(downloads, strings.Repeat("f", 64))
	must(t, os.WriteFile(fresh, []byte("new"), 0o644))

	// With every generation kept, gone's generation still holds its download.
	var before struct {
		GCCacheFrees int64 `json:"gc_cache_frees"`
	}
	must(t, json.Unmarshal([]byte(m.stdout(t, "du", "--json")), &before))

	if before.GCCacheFrees == 0 {
		t.Fatal("du says gc --cache frees nothing, but the index by url is old")
	}

	out, err := m.run(t, "", "gc", "--keep", "1", "--cache")
	must(t, err)

	switch {
	case !exists(filepath.Join(downloads, keepSum)):
		t.Fatalf("gc --cache deleted the download of keep, which generation 3 uses:\n%s", out)
	case exists(filepath.Join(downloads, goneSum)):
		t.Fatalf("gc --cache kept the download of gone, which no kept generation uses:\n%s", out)
	case exists(filepath.Join(downloads, "by-url")) && len(m.entries(t, filepath.Join(downloads, "by-url"))) > 0:
		t.Fatalf("gc --cache kept the old index by url:\n%s", out)
	case !exists(fresh):
		t.Fatalf("gc --cache deleted a download less than a day old:\n%s", out)
	}

	// Without --cache, gc leaves the cache alone.
	stray := filepath.Join(downloads, strings.Repeat("a", 64))
	must(t, os.WriteFile(stray, []byte("old"), 0o644))
	must(t, os.Chtimes(stray, old, old))

	_, err = m.run(t, "", "gc")
	must(t, err)

	if !exists(stray) {
		t.Fatal("gc without --cache deleted a download")
	}
}

func (m machine) entries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(dir)
	must(t, err)

	return entries
}
