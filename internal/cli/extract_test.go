package cli_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)

			_, err := m.run(t, "", "add", m.linkArchive(t, "tool", links))
			if err == nil || !strings.Contains(err.Error(), "leads outside the package") {
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
