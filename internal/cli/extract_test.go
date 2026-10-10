package cli_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// linkArchive writes a tar.gz with a program and the symlinks in links, in
// order, each a name and a target, and a manifest for it.
func (m machine) linkArchive(t *testing.T, name string, links [][2]string) string {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	must(t, tw.WriteHeader(&tar.Header{Name: "tool", Mode: 0o755, Size: int64(len(script))}))
	_, err := tw.Write([]byte(script))
	must(t, err)

	for _, link := range links {
		must(t, tw.WriteHeader(&tar.Header{
			Name: link[0], Linkname: link[1], Typeflag: tar.TypeSymlink, Mode: 0o777,
		}))
	}

	must(t, tw.Close())
	must(t, gz.Close())

	archive := filepath.Join(m.fixtures, name+".tar.gz")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	sum := sha256.Sum256(buf.Bytes())

	return m.rawManifest(t, name, fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nbin = [\"tool\"]\n",
		archive, hex.EncodeToString(sum[:]),
	))
}

func TestB247AnArchiveLinkThatLeadsOutsideThePackageIsRefused(t *testing.T) {
	for name, links := range map[string][][2]string{
		// x is the package itself, so x/l is l, and ../outside leaves the package.
		"through-a-linked-dir": {{"x", "."}, {"x/l", "../outside"}},
		// l leads to y inside the package until d becomes the package itself, which
		// makes d/.. the directory above it.
		"redirected-later": {{"l", "d/../y"}, {"d", "."}},
		// Windows resolves a target that starts with \ from the root of the drive.
		"rooted-on-windows": {{"l", `\Windows\System32`}},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)

			_, err := m.run(t, "", "add", m.linkArchive(t, "tool", links))
			if err == nil || !strings.Contains(err.Error(), "outside the package") {
				t.Fatalf("want a refusal of the link, got %v", err)
			}

			if entries := m.storeEntries(t); len(entries) != 0 {
				t.Fatalf("the refused package left %v in the store", entries)
			}
		})
	}

	// A link that stays inside, also through a linked directory, is fine.
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.linkArchive(t, "tool", [][2]string{
		{"lib", "usr/lib"}, {"usr/lib/keep", "../../tool"}, {"alias", "lib/keep"},
	}))
	must(t, err)
}

func TestB410AnXZWhoseHeaderAsksForAHugeDictionaryIsRefused(t *testing.T) {
	m := newMachine(t)

	// A stream header, then a block header whose LZMA2 filter asks for a 1 GiB
	// dictionary, and no data.
	var data []byte

	flags := []byte{0, 1}
	data = append(data, 0xfd, '7', 'z', 'X', 'Z', 0)
	data = append(data, flags...)
	data = binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(flags))

	block := []byte{2, 0, 0x21, 1, 36, 0, 0, 0}
	data = append(data, block...)
	data = binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(block))

	path := filepath.Join(m.fixtures, "huge.tar.xz")
	must(t, os.MkdirAll(m.fixtures, 0o755))
	must(t, os.WriteFile(path, data, 0o644))

	sum := sha256.Sum256(data)
	ref := m.rawManifest(t, "huge", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256 = \"%x\"\nbin = [\"tool\"]\n", path, sum,
	))

	if out, err := m.run(t, "", "add", ref); err == nil || !strings.Contains(err.Error(), "dictionary size exceeds max") {
		t.Fatalf("an xz asking for a 1 GiB dictionary should be refused, got %v:\n%s", err, out)
	}
}

// zipEntry is one entry of zipArtifact. It is a link when link is set, else a
// file.
type zipEntry struct {
	name, body, link string
	mode            os.FileMode
	modified        time.Time
}

// zipArtifact writes a zip of entries, in order, and a manifest that unpacks it
// with strip = 1 and has the program bin/tool.
func (m machine) zipArtifact(t *testing.T, entries []zipEntry) string {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: e.modified}
		header.SetMode(e.mode)

		body := e.body
		if e.link != "" {
			header.SetMode(os.ModeSymlink | 0o777)
			body = e.link
		}

		w, err := zw.CreateHeader(header)
		must(t, err)

		_, err = w.Write([]byte(body))
		must(t, err)
	}

	must(t, zw.Close())

	archive := filepath.Join(m.fixtures, "tool.zip")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	sum := sha256.Sum256(buf.Bytes())

	return m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nstrip = 1\nbin = [\"bin/tool\"]\n",
		archive, hex.EncodeToString(sum[:]),
	))
}

func TestB561AZipUnpacksEveryFileWithItsModeTimeAndLinks(t *testing.T) {
	m := newMachine(t)
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

	entries := []zipEntry{{name: "tool/bin/tool", body: script, mode: 0o755, modified: old}}
	for i := range 20 {
		entries = append(entries, zipEntry{
			name: fmt.Sprintf("tool/share/%d/note.txt", i), body: fmt.Sprint(i), mode: 0o644, modified: old,
		})
	}

	entries = append(entries, zipEntry{name: "tool/share/latest", link: "19/note.txt"})

	_, err := m.run(t, "", "add", m.zipArtifact(t, entries))
	must(t, err)

	program, err := filepath.EvalSymlinks(m.profile("bin", "tool"))
	must(t, err)

	pkg := filepath.Dir(filepath.Dir(program))

	for i := range 20 {
		note := filepath.Join(pkg, "share", fmt.Sprint(i), "note.txt")

		data, err := os.ReadFile(note)
		if err != nil || string(data) != fmt.Sprint(i) {
			t.Fatalf("%s holds %q, %v", note, data, err)
		}

		if info, err := os.Stat(note); err != nil || !info.ModTime().Equal(old) {
			t.Fatalf("%s lost the time of the archive: %v", note, err)
		}
	}

	if data, err := os.ReadFile(filepath.Join(pkg, "share", "latest")); err != nil || string(data) != "19" {
		t.Fatalf("the link reads %q, %v", data, err)
	}

	if got := m.toolOutput(t); got == "" {
		t.Fatal("the program does not run")
	}
}

func TestB561AZipThatNamesAFileTwiceKeepsTheLaterOne(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.zipArtifact(t, []zipEntry{
		{name: "tool/bin/tool", body: "#!/bin/sh\necho first\n", mode: 0o755},
		{name: "tool/share/note.txt", body: "note", mode: 0o644},
		{name: "tool/bin/tool", body: "#!/bin/sh\necho second\n", mode: 0o755},
	}))
	must(t, err)

	if got := m.toolOutput(t); got != "second" {
		t.Fatalf("tool printed %q, want the later entry", got)
	}
}
