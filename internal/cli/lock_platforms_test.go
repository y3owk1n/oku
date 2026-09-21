package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/platform"
)

// otherPlatform returns a platform of another OS than the host's. It has no
// libc, so one artifact matches it alone.
func otherPlatform() platform.Platform {
	for _, p := range platform.All() {
		if p.OS != platform.Host().OS && p.Libc == "" {
			return p
		}
	}

	panic("unreachable")
}

// twoPlatformManifest writes a manifest with an artifact for the host and one
// for other. The other download is no archive and the manifest gives no
// checksum for it.
func (m machine) twoPlatformManifest(t *testing.T, other platform.Platform) (string, string) {
	t.Helper()

	archive, sum := m.archive(t, "tool", map[string]string{"tool": script})
	foreign := filepath.Join(m.fixtures, "tool-foreign.bin")
	must(t, os.WriteFile(foreign, []byte("not an archive"), 0o644))

	foreignSum := sha256.Sum256([]byte("not an archive"))
	host := platform.Host()

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nmatch = { os = %q, arch = %q, libc = %q }\nurl = \"file://%s\"\nsha256 = %q\n"+
			"bin = [\"tool\"]\n"+
			"[[artifact]]\nmatch = { os = %q, arch = %q }\nurl = \"file://%s\"\nbin = [\"tool\"]\n",
		host.OS, host.Arch, host.Libc, archive, sum, other.OS, other.Arch, foreign,
	))

	return ref, hex.EncodeToString(foreignSum[:])
}

func TestB179LockPlatformsPinsAnotherPlatformWithoutInstallingIt(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref, foreignSum := m.twoPlatformManifest(t, other)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref,
	)), 0o644))

	out, err := m.run(t, "", "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !strings.Contains(out, "publishes no checksum for "+other.String()) {
		t.Fatalf("sync did not report the download it trusted:\n%s", out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	_, entry, _ := strings.Cut(string(locked), "platform."+other.String()+"]")
	if !strings.Contains(entry, foreignSum) {
		t.Fatalf("oku.lock does not pin %s at %s:\n%s", other, foreignSum, locked)
	}

	if len(m.storeEntries(t)) != 1 {
		t.Fatalf("the store holds more than the host's package: %v", m.storeEntries(t))
	}

	// oku cannot pin a platform that has no artifact and no build.
	none := platform.Platform{OS: other.OS, Arch: "arm64"}

	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", none.String(), ref,
	)), 0o644))

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "tool has no artifact for "+none.String()) {
		t.Fatalf("want an error that names %s, got %v", none, err)
	}
}

func TestB180AProjectPinsEveryPlatformItCanAndTheGlobalListTheHost(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	ref, _ := m.twoPlatformManifest(t, other)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if strings.Contains(string(locked), "platform."+other.String()+"]") {
		t.Fatalf("the global lock pins %s:\n%s", other, locked)
	}

	project := filepath.Join(m.fixtures, "work")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))

	m.opts.WorkDir = project

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	locked, err = os.ReadFile(filepath.Join(project, "oku.lock"))
	must(t, err)

	// The manifest has an artifact for two platforms, and oku skips the others.
	if got := strings.Count(string(locked), "[package.platform."); got != 2 ||
		!strings.Contains(string(locked), "platform."+other.String()+"]") {
		t.Fatalf("the project lock does not pin the host and %s alone:\n%s", other, locked)
	}
}

func TestB181LockedSyncFailsWhenTheLockWouldChange(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if out, err := m.run(t, "", "sync", "--locked"); err != nil {
		t.Fatalf("a locked sync of a complete lock failed: %v\n%s", err, out)
	}

	lockPath := filepath.Join(m.config, "oku.lock")
	locked, err := os.ReadFile(lockPath)
	must(t, err)

	// The lock now looks as if another machine wrote it, on an empty store.
	foreign := strings.ReplaceAll(string(locked), platform.Host().String(), "plan9-mips")
	must(t, os.WriteFile(lockPath, []byte(foreign), 0o644))
	must(t, os.RemoveAll(m.data))

	_, err = m.run(t, "", "sync", "--locked")
	if err == nil || !strings.Contains(err.Error(), "does not pin tool for "+platform.Host().String()) {
		t.Fatalf("want an error that names tool and the host, got %v", err)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(after) != foreign || len(m.storeEntries(t)) != 0 {
		t.Fatalf("a locked sync changed the lock or the store: %v\n%s", m.storeEntries(t), after)
	}

	// A lock that holds a package the list dropped is out of date too.
	must(t, os.WriteFile(lockPath, locked, 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), nil, 0o644))

	_, err = m.run(t, "", "sync", "--locked")
	if err == nil || !strings.Contains(err.Error(), "is out of date") {
		t.Fatalf("want an out of date error, got %v", err)
	}
}
