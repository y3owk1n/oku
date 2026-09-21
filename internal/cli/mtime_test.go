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
	"testing"
	"time"
)

func TestB169AnUnpackedSourceKeepsTheFileTimesOfTheArchive(t *testing.T) {
	m := newMachine(t)

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	// The generated file comes first in the archive and is the newer one, as in a
	// release tarball. Unpacked with the time of the unpacking it would be the
	// older one, and make would run autotools to make it again.
	for _, file := range []struct {
		name string
		year int
	}{{"aclocal.m4", 2002}, {"configure.ac", 2001}} {
		must(t, tw.WriteHeader(&tar.Header{
			Name: file.name, Mode: 0o644, Size: 1,
			ModTime: time.Date(file.year, 1, 1, 0, 0, 0, 0, time.UTC),
		}))

		_, err := tw.Write([]byte("x"))
		must(t, err)
	}

	must(t, tw.Close())
	must(t, gz.Close())

	archive := filepath.Join(m.fixtures, "source.tar.gz")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	sum := sha256.Sum256(buf.Bytes())

	ref := m.buildManifest(t, false, fmt.Sprintf(
		"needs = [\"sh\"]\nsource = { url = \"file://%s\", sha256 = %q }",
		archive, hex.EncodeToString(sum[:]),
	), "[[build.step]]\nrun = \"test aclocal.m4 -nt configure.ac\"\nshell = \"sh\"\n"+writeTool+installTool)

	m.opts.Interactive = yes()

	if out, err := m.run(t, "y\n", "add", ref); err != nil {
		t.Fatalf("the generated file should still be newer than its input after unpacking: %v\n%s", err, out)
	}
}
