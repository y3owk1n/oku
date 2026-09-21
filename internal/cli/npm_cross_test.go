package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/platform"
)

func TestB189TheNPMPackagesOfAnotherPlatformArePinnedFromThisOne(t *testing.T) {
	m := newMachine(t)
	other := otherPlatform()
	npmServerWith(t, &m, "", true, "1.1.0")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte(fmt.Sprintf("[runtimes]\nnode = %q\n", m.fakeNode(t))), 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = \"npm:@scope/tool\"\n", other.String(),
	)), 0o644))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	here := platformEntry(string(locked), platform.Host().String())
	there := platformEntry(string(locked), other.String())

	// The fake npm writes the platform it installs for into the tree, as the real
	// one installs other packages, so the two digests differ.
	if !strings.Contains(there, "vendor_sha256 = '") || there == here {
		t.Fatalf("oku.lock does not pin what npm installs on %s:\n%s", other, locked)
	}

	// Only the node and the tool of this machine are in the store.
	if got := m.storeEntries(t); len(got) != 2 {
		t.Fatalf("the packages of %s entered the store: %v", other, got)
	}
}
