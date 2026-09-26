package cli_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestB387GCCacheDeletesADownloadNoInstallHasUsedForTwoDays(t *testing.T) {
	m := newMachine(t)
	_, sum := m.archive(t, "keep", map[string]string{"keep": script})

	_, err := m.run(t, "", "add", m.namedManifest(t, "keep", "keep", "keep"))
	must(t, err)

	download := filepath.Join(m.cache, "downloads", sum)
	if !exists(download) {
		t.Fatalf("the cache holds no download at %s", download)
	}

	// A day and a half after the install, the store path still holds its download.
	setAge(t, download, 36*time.Hour)

	out, err := m.run(t, "", "gc", "--cache")
	must(t, err)

	if !exists(download) {
		t.Fatalf("gc --cache deleted a download from yesterday:\n%s", out)
	}

	setAge(t, download, 50*time.Hour)

	out, err = m.run(t, "", "gc", "--cache")
	must(t, err)

	if exists(download) {
		t.Fatalf("gc --cache kept a download that no install used for two days:\n%s", out)
	}

	// The package still runs, because its store path holds the unpacked content.
	m.output(t, "keep")
}

func TestB387OlderThanSetsHowLongADownloadStays(t *testing.T) {
	m := newMachine(t)
	_, sum := m.archive(t, "keep", map[string]string{"keep": script})

	_, err := m.run(t, "", "add", m.namedManifest(t, "keep", "keep", "keep"))
	must(t, err)

	download := filepath.Join(m.cache, "downloads", sum)
	setAge(t, download, 5*24*time.Hour)

	out, err := m.run(t, "", "gc", "--cache", "--older-than", "2w")
	must(t, err)

	if !exists(download) {
		t.Fatalf("gc --cache --older-than 2w deleted a download of five days:\n%s", out)
	}

	out, err = m.run(t, "", "gc", "--cache", "--older-than", "3d")
	must(t, err)

	if exists(download) {
		t.Fatalf("gc --cache --older-than 3d kept a download of five days:\n%s", out)
	}
}

func TestB387AnInstallThatUsesADownloadKeepsItLonger(t *testing.T) {
	m := newMachine(t)
	_, sum := m.archive(t, "keep", map[string]string{"keep": script})

	ref := m.namedManifest(t, "keep", "keep", "keep")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	download := filepath.Join(m.cache, "downloads", sum)
	setAge(t, download, 50*time.Hour)

	// The store path is gone, as it would be on another machine, so the install
	// unpacks the download again and that use sets its time.
	for _, name := range m.storeEntries(t) {
		must(t, os.RemoveAll(filepath.Join(m.data, "store", name)))
	}

	_, err = m.run(t, "", "sync")
	must(t, err)

	out, err := m.run(t, "", "gc", "--cache")
	must(t, err)

	if !exists(download) {
		t.Fatalf("gc --cache deleted a download that an install just used:\n%s", out)
	}
}

// setAge sets the time of the file at path to d ago.
func setAge(t *testing.T, path string, d time.Duration) {
	t.Helper()

	when := time.Now().Add(-d)
	must(t, os.Chtimes(path, when, when))
}
