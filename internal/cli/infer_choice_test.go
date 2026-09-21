package cli_test

import (
	"strings"
	"testing"
)

func TestB174InferencePrefersTheCommandLineBuildAndTheChecksumsOfItsOwnOS(t *testing.T) {
	m := newMachine(t)
	file := func(name string) string {
		path, _ := m.archive(t, name, map[string]string{"tool": script})

		return path
	}

	inferServer(t, &m, map[string]string{
		// A desktop app beside the command line build, as sst/opencode ships them.
		"tool-desktop-mac-arm64.app.tar.gz": file("desktop"),
		"tool-darwin-arm64.zip":             file("cli"),
		"tool-linux-arm64.tar.gz":           file("linux"),
		// manifest init wants an asset for the machine it runs on.
		"tool-linux-x86_64.tar.gz": file("linux-amd64"),
		"tool-darwin-x86_64.zip":   file("cli-amd64"),
		// One checksum file for each OS, as stripe/stripe-cli ships them.
		"tool-linux-checksums.txt": file("sums-linux"),
		"tool-mac-checksums.txt":   file("sums-mac"),
	})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	darwin := out[strings.Index(out, `os = "darwin", arch = "arm64"`):]
	darwin = darwin[:strings.Index(darwin, "bin =")]

	if !strings.Contains(darwin, "/cli.tar.gz") || strings.Contains(darwin, "desktop") {
		t.Fatalf("the macOS artifact should be the command line build:\n%s", out)
	}

	if !strings.Contains(darwin, "/sums-mac.tar.gz") {
		t.Fatalf("the macOS artifact should read the checksums of macOS:\n%s", out)
	}

	linux := out[strings.Index(out, `os = "linux", arch = "arm64"`):]
	linux = linux[:strings.Index(linux, "bin =")]

	if !strings.Contains(linux, "/sums-linux.tar.gz") {
		t.Fatalf("the Linux artifact should read the checksums of Linux:\n%s", out)
	}
}
