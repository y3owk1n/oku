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

	// Someone who can write beside oku drops a spec that names another program.
	if err := os.WriteFile(filepath.Join(dir, "oku.shim"), []byte("path = /bin/echo\narg = hijacked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || strings.Contains(string(out), "hijacked") || !strings.Contains(string(out), "oku version") {
		t.Fatalf("oku --version ran the spec beside it: %v\n%s", err, out)
	}
}
