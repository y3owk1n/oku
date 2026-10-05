package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB429AShimFileBesideOkuRunsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows profile's shims are copies of oku, which live-windows.ps1 covers")
	}

	dir := t.TempDir()
	binary := filepath.Join(dir, "oku")

	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build oku: %v\n%s", err, out)
	}

	// Outside Windows a copy under another name runs no shim either, which the
	// copy named tool checks. live-windows.ps1 checks oku under its own name.
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tool"), data, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"oku", "tool"} {
		// Someone who can write beside the binary drops a spec that names
		// another program.
		spec := []byte("path = /bin/echo\narg = hijacked\n")
		if err := os.WriteFile(filepath.Join(dir, name+".shim"), spec, 0o644); err != nil {
			t.Fatal(err)
		}

		out, err := exec.Command(filepath.Join(dir, name), "--version").CombinedOutput()
		if err != nil || strings.Contains(string(out), "hijacked") || !strings.Contains(string(out), "oku version") {
			t.Fatalf("%s --version ran the spec beside it: %v\n%s", name, err, out)
		}
	}
}
