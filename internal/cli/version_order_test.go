package cli_test

import (
	"strings"
	"testing"
)

func TestB20ANumberInAVersionSuffixComparesAsANumber(t *testing.T) {
	m := newMachine(t)

	// ImageMagick counts its releases this way, and 31 is newer than 9.
	server := newReleaseServer(t, "v7.1.2-9", "v7.1.2-31", "v7.1.2-10")
	m.opts.GitHubAPI = server.URL + "/api"

	out, err := m.run(t, "", "add", m.discoveredManifest(t, "7.1.2-9", "7.1.2-10", "7.1.2-31"))
	must(t, err)

	if got := m.toolOutput(t); got != "7.1.2-31" || !strings.Contains(out, "7.1.2-31") {
		t.Fatalf("oku installed %s, want the newest release 7.1.2-31:\n%s", got, out)
	}
}
