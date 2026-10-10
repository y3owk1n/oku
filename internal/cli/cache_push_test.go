package cli_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestB87PushTakesABuildFromASourceArchiveAndLeavesDownloads(t *testing.T) {
	m := newMachine(t)
	served := filepath.Join(m.fixtures, "served")

	_, err := m.run(t, "", "key", "generate")
	must(t, err)

	// A build records the URL of its source archive, as a download records its
	// own.
	archive, sum := m.archive(t, "src", map[string]string{"README": "source"})
	source := fmt.Sprintf("source = { url = \"file://%s\", sha256 = %q }\n", archive, sum)

	if out, err := m.run(t, "", "add", m.cachedManifest(t, true, source+writeTool), "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "add", m.namedManifest(t, "fetched", "fetched", "fetched")); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	out, err := m.run(t, "", "cache", "push", served, "tool")
	if err != nil || !strings.Contains(out, "pushed tool-1.0.0-") {
		t.Fatalf("push should take the build from a source archive: %v\n%s", err, out)
	}

	out, err = m.run(t, "", "cache", "push", served, "fetched")
	if err != nil || !strings.Contains(out, "nothing to push") {
		t.Fatalf("push should leave a download out: %v\n%s", err, out)
	}
}
