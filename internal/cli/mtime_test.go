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
	"runtime"
	"strings"
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

// sourceArchive writes a source tarball that holds one script and returns its
// path.
func (m machine) sourceArchive(t *testing.T, name, script string) string {
	t.Helper()

	path, _ := m.archive(t, name, map[string]string{"tool": script})

	return path
}

func TestB172ASourceArchiveWithoutAChecksumIsPinnedOnFirstDownload(t *testing.T) {
	m := newMachine(t)
	m.opts.Interactive = yes()

	archive := m.sourceArchive(t, "src", "#!/bin/sh\necho one\n")

	ref := m.buildManifest(t, false,
		fmt.Sprintf("needs = [\"sh\"]\nsource = { url = \"file://%s\" }", archive), installTool)

	out, err := m.run(t, "y\n", "add", ref)
	must(t, err)

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	data, err := os.ReadFile(archive)
	must(t, err)

	sum := sha256.Sum256(data)

	if !strings.Contains(string(locked), hex.EncodeToString(sum[:])) || !strings.Contains(out, "trusted") {
		t.Fatalf("oku.lock should pin the digest of the source, and add should say so:\n%s\n%s", out, locked)
	}

	// The archive changes under the same version, on a machine with an empty store.
	m.sourceArchive(t, "src", "#!/bin/sh\necho tampered\n")
	must(t, os.RemoveAll(m.data))
	must(t, os.RemoveAll(m.cache))

	_, err = m.run(t, "y\n", "sync")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("sync should refuse a source archive that no longer has the pinned digest, got %v", err)
	}
}

func TestB173AMacOSBuildFindsThePkgConfigFilesOfSystemLibraries(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS ships libraries with headers and no pkg-config file")
	}

	m := newMachine(t)
	m.opts.Interactive = yes()

	ref := m.buildManifest(t, false, `needs = ["sh"]`,
		"[[build.step]]\nshell = \"sh\"\nrun = \"\"\"\n"+
			"dir=$(echo $PKG_CONFIG_PATH | tr ':' '\\\\n' | tail -1)\n"+
			"grep -q '^Version: [0-9]' $dir/zlib.pc\ngrep -q '^Libs: -lz' $dir/zlib.pc\n"+
			"grep -q 'libxml2' $dir/libxml-2.0.pc\n\"\"\"\n"+writeTool+installTool)

	if out, err := m.run(t, "y\n", "add", ref); err != nil {
		t.Fatalf("a build on macOS should find zlib.pc with a version on PKG_CONFIG_PATH: %v\n%s", err, out)
	}
}
