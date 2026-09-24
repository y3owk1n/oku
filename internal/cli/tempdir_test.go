package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/tempdir"
)

// endedPid returns the pid of a process that has exited.
func endedPid(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("true")
	must(t, cmd.Run())

	return cmd.Process.Pid
}

func TestB286GCRemovesTemporaryFilesThatEndedOkuProcessesLeft(t *testing.T) {
	m := newMachine(t)
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	ended := filepath.Join(dir, fmt.Sprintf("oku-build-%d-1", endedPid(t)))
	legacy := filepath.Join(dir, "oku-build-4242")
	live := filepath.Join(dir, fmt.Sprintf("oku-build-%d-2", os.Getpid()))
	other := filepath.Join(dir, "not-oku")

	for _, path := range []string{ended, legacy, live, other} {
		must(t, os.MkdirAll(filepath.Join(path, "mod"), 0o755))
		must(t, os.WriteFile(filepath.Join(path, "mod", "go.mod"), []byte("module x\n"), 0o444))
	}

	// Go's module cache is read-only.
	must(t, os.Chmod(filepath.Join(ended, "mod"), 0o555))

	out, err := m.run(t, "", "gc", "--dry-run")
	if err != nil || !strings.Contains(out, "would remove "+ended) || !exists(ended) {
		t.Fatalf("gc --dry-run did not only name %s: %v\n%s", ended, err, out)
	}

	out, err = m.run(t, "", "gc")
	if err != nil {
		t.Fatalf("gc: %v\n%s", err, out)
	}

	if exists(ended) || exists(legacy) {
		t.Fatalf("gc left what an ended oku process left:\n%s", out)
	}

	if !exists(live) || !exists(other) {
		t.Fatalf("gc removed what a running process or another program uses:\n%s", out)
	}

	if !strings.Contains(out, "left by an oku process that ended") {
		t.Fatalf("gc did not say why it removed them:\n%s", out)
	}
}

func TestB287AnImageThatAKilledRunLeftMountedIsDetached(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("oku mounts disk images on macOS only")
	}

	m := newMachine(t)
	t.Setenv("TMPDIR", t.TempDir())

	payload := filepath.Join(m.fixtures, "payload")
	must(t, os.MkdirAll(filepath.Join(payload, "Tool.app", "Contents", "MacOS"), 0o755))
	must(t, os.WriteFile(
		filepath.Join(payload, "Tool.app", "Contents", "MacOS", "tool"), []byte("#!/bin/sh\necho from image\n"), 0o755,
	))

	dmg := filepath.Join(m.fixtures, "tool.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-volname", "Tool", "-srcfolder", payload, "-format", "UDZO", dmg).
		CombinedOutput(); err != nil {
		t.Skipf("cannot create a disk image here: %v\n%s", err, out)
	}

	image, err := os.ReadFile(dmg)
	must(t, err)

	ref := m.fileManifest(t, "image.dmg", image, "bin = [\"Tool.app/Contents/MacOS/tool\"]")

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	sum := sha256.Sum256(image)
	cached := filepath.Join(m.cache, "downloads", hex.EncodeToString(sum[:]))

	// leave mounts the cached image the way an oku process that was killed
	// while it copied leaves it.
	leave := func() string {
		t.Helper()

		point := filepath.Join(os.TempDir(), fmt.Sprintf("oku-dmg-%d-1", endedPid(t)))
		must(t, os.MkdirAll(point, 0o755))

		if out, err := exec.Command("/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noverify",
			"-mountpoint", point, cached).CombinedOutput(); err != nil {
			t.Fatalf("attach: %v\n%s", err, out)
		}

		t.Cleanup(func() { _ = exec.Command("/usr/bin/hdiutil", "detach", "-force", point).Run() })

		return point
	}

	mounted := func() []string {
		t.Helper()

		points, err := tempdir.Mounted(cached)
		must(t, err)

		return points
	}

	leave()
	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync did not unpack an image that a killed run left mounted: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "from image" {
		t.Fatalf("tool printed %q", got)
	}

	if points := mounted(); len(points) > 0 {
		t.Fatalf("the image is still mounted at %v", points)
	}

	point := leave()

	if out, err := m.run(t, "", "gc"); err != nil || exists(point) || len(mounted()) > 0 {
		t.Fatalf("gc did not detach and remove %s: %v\n%s", point, err, out)
	}
}
