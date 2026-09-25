package cli_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestB359OneCommandListsTheReleasesOfASourceOnce(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.1.0", "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0", "1.1.0")+"@1.0.0")
	must(t, err)

	// outdated looks up the newest version that 1.0.0 allows and the latest
	// release, from the one list of releases.
	server.hits = 0

	out, err := m.run(t, "", "outdated")
	if err != nil || !strings.Contains(out, "1.1.0") {
		t.Fatalf("outdated: %v\n%s", err, out)
	}

	if server.hits != 1 {
		t.Fatalf("outdated asked for the releases %d times, want 1", server.hits)
	}
}

func TestB359OneCommandReadsARegistryDocumentOnce(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.0.0")

	upstream, err := url.Parse(m.opts.NPMRegistry)
	must(t, err)

	var hits atomic.Int32

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		httputil.NewSingleHostReverseProxy(upstream).ServeHTTP(w, r)
	}))
	t.Cleanup(registry.Close)

	m.opts.NPMRegistry = registry.URL

	// Inference and the version lookup both read the package's document.
	out, err := m.run(t, "", "add", "npm:@scope/tool", "--plan")
	if err != nil {
		t.Fatalf("add --plan: %v\n%s", err, out)
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("add --plan read the registry %d times, want 1\n%s", got, out)
	}
}
