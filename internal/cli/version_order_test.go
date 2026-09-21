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

func TestB20APrereleaseWithNoDashIsNeverTheNewest(t *testing.T) {
	m := newMachine(t)

	// Go tags its prereleases go1.9rc2, and a tag list has no prerelease flag.
	server := newReleaseServer(t, "v1.9rc2", "v1.26.8", "v1.27rc1")
	m.opts.GitHubAPI = server.URL + "/api"

	out, err := m.run(t, "", "add", m.discoveredManifest(t, "1.9rc2", "1.26.8", "1.27rc1"))
	must(t, err)

	if got := m.toolOutput(t); got != "1.26.8" || !strings.Contains(out, "1.26.8") {
		t.Fatalf("oku installed %s, want the newest release 1.26.8:\n%s", got, out)
	}
}
