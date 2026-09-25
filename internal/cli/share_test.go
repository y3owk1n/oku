package cli_test

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/y3owk1n/oku/internal/status"
)

// blob is a file big enough for the store to share.
var blob = strings.Repeat("\x00", 20000)

// blobManifest is a package whose download holds blob beside its program.
func (m machine) blobManifest(t *testing.T, name string) string {
	t.Helper()

	return m.manifest(t, name, map[string]string{name: script, "blob.dat": blob}, `bin = ["`+name+`"]`)
}

// blobOf returns the path of blob in the store path of the package name.
func (m machine) blobOf(t *testing.T, name string) string {
	t.Helper()

	found, err := filepath.Glob(filepath.Join(m.data, "store", name+"-*", "pkg", "blob.dat"))
	must(t, err)

	if len(found) != 1 {
		t.Fatalf("the store holds %d blobs of %s", len(found), name)
	}

	return found[0]
}

// storeBytes adds up the files of the named store paths as if none shared.
func (m machine) storeBytes(t *testing.T, names ...string) int64 {
	t.Helper()

	var size int64

	for _, name := range names {
		dirs, err := filepath.Glob(filepath.Join(m.data, "store", name+"-*"))
		must(t, err)

		for _, dir := range dirs {
			must(t, filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}

				info, err := entry.Info()
				size += info.Size()

				return err
			}))
		}
	}

	return size
}

// duStore returns the bytes oku du gives for the store.
func (m machine) duStore(t *testing.T) int64 {
	t.Helper()

	var got struct {
		Areas []struct {
			Area  string `json:"area"`
			Bytes int64  `json:"bytes"`
		} `json:"areas"`
	}
	must(t, json.Unmarshal([]byte(m.stdout(t, "du", "--json")), &got))

	for _, a := range got.Areas {
		if a.Area == "store" {
			return a.Bytes
		}
	}

	t.Fatal("du has no store area")

	return 0
}

func TestB362StorePathsKeepOneCopyOfAnIdenticalFile(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"one", "two"} {
		_, err := m.run(t, "", "add", m.blobManifest(t, name))
		must(t, err)
	}

	one, two := m.blobOf(t, "one"), m.blobOf(t, "two")

	for _, path := range []string{one, two} {
		if data, err := os.ReadFile(path); err != nil || string(data) != blob {
			t.Fatalf("%s no longer holds its bytes: %v", path, err)
		}
	}

	infoOne, err := os.Stat(one)
	must(t, err)
	infoTwo, err := os.Stat(two)
	must(t, err)

	// A hard link must not let one store path change the other. A clone keeps the
	// mode the file had.
	if os.SameFile(infoOne, infoTwo) {
		if infoOne.Mode().Perm()&0o222 != 0 {
			t.Fatalf("a shared hard link is writable: %v", infoOne.Mode())
		}
	} else if infoOne.Mode().Perm() != 0o755 || infoTwo.Mode().Perm() != 0o755 {
		t.Fatalf("a clone changed mode: %v and %v", infoOne.Mode(), infoTwo.Mode())
	}

	store := m.duStore(t)
	if want := m.storeBytes(t, "one", "two") - int64(len(blob)); store != want {
		t.Fatalf("du gives the store %d bytes, want %d with the blob once", store, want)
	}

	for _, name := range []string{"one", "two"} {
		if _, err := exec.Command(m.profile("bin", name)).Output(); err != nil {
			t.Fatalf("%s no longer runs: %v", name, err)
		}
	}
}

func TestB363GCFreesOnlyWhatDeletingAStorePathFrees(t *testing.T) {
	m := newMachine(t)

	for _, args := range [][]string{
		{"add", m.blobManifest(t, "one")},
		{"add", m.blobManifest(t, "two")},
		{"remove", "two"},
	} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	// one keeps the blob, so deleting two frees the rest of it.
	want := status.Size(m.storeBytes(t, "two") - int64(len(blob)))

	dry, err := m.run(t, "", "gc", "--keep", "1", "--dry-run")
	must(t, err)

	out, err := m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	if !strings.Contains(dry, "would free "+want) || !strings.Contains(out, "freed "+want) {
		t.Fatalf("gc does not free %s:\n%s\n%s", want, dry, out)
	}

	if data, err := os.ReadFile(m.blobOf(t, "one")); err != nil || string(data) != blob {
		t.Fatalf("gc took the blob from one: %v", err)
	}

	// With the last store path that holds it gone, the index lets it go too.
	for _, args := range [][]string{{"remove", "one"}, {"gc", "--keep", "1"}} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	links := filepath.Join(m.data, "store", ".links")
	must(t, filepath.WalkDir(links, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		if err == nil && !entry.IsDir() {
			t.Errorf("%s is left in the index", path)
		}

		return err
	}))
}

func TestB364GCSharesTheFilesOfOlderStorePaths(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"one", "two"} {
		_, err := m.run(t, "", "add", m.blobManifest(t, name))
		must(t, err)
	}

	// A store from before oku shared files holds a copy per store path.
	must(t, os.RemoveAll(filepath.Join(m.data, "store", ".links")))

	two := m.blobOf(t, "two")
	must(t, os.Remove(two))
	must(t, os.WriteFile(two, []byte(blob), 0o755))

	out, err := m.run(t, "", "gc", "--dry-run")
	must(t, err)

	if !strings.Contains(out, "would share the identical files of 2 store paths") {
		t.Fatalf("the dry run does not say what gc shares:\n%s", out)
	}

	out, err = m.run(t, "", "gc")
	must(t, err)

	saved := status.Size(int64(len(blob)))
	if !strings.Contains(out, "shared the identical files of 2 store paths ("+saved+")") ||
		!strings.Contains(out, "freed "+saved+" from identical files") {
		t.Fatalf("gc does not say what sharing saved:\n%s", out)
	}

	if data, err := os.ReadFile(two); err != nil || string(data) != blob {
		t.Fatalf("sharing changed the blob of two: %v", err)
	}

	out, err = m.run(t, "", "gc")
	must(t, err)

	if !strings.Contains(out, "nothing to delete") {
		t.Fatalf("a second gc shares again:\n%s", out)
	}
}

func TestB365CachePushWritesASharedFileWithItsOwnMode(t *testing.T) {
	m := newMachine(t)
	dir := filepath.Join(filepath.Dir(m.fixtures), "served")

	_, err := m.run(t, "", "key", "generate")
	must(t, err)

	_, err = m.run(t, "", "add", m.blobManifest(t, "one"))
	must(t, err)

	built := filepath.Join(m.fixtures, "built.toml")
	must(t, os.WriteFile(built, []byte(
		"[package]\nname = \"built\"\nrelocatable = true\n[version]\nvalue = \"1.0.0\"\n[build]\n"+
			"[[build.step]]\nshell = \"sh\"\n"+
			"run = \"head -c 20000 /dev/zero > {{prefix}}/blob.dat && chmod 755 {{prefix}}/blob.dat\"\n",
	), 0o644))

	_, err = m.run(t, "", "add", built, "--yes")
	must(t, err)

	_, err = m.run(t, "", "cache", "push", dir)
	must(t, err)

	entries, err := filepath.Glob(filepath.Join(dir, "built-*.tar.zst"))
	must(t, err)

	if len(entries) != 1 {
		t.Fatalf("push wrote %d entries of built", len(entries))
	}

	f, err := os.Open(entries[0])
	must(t, err)

	defer f.Close()

	zr, err := zstd.NewReader(f)
	must(t, err)

	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("the entry holds no blob.dat")
		}

		must(t, err)

		if strings.HasSuffix(header.Name, "/blob.dat") {
			if mode := header.Mode & 0o777; mode != 0o755 {
				t.Fatalf("the entry gives blob.dat mode %o, want 755", mode)
			}

			return
		}
	}
}
