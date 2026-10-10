package cli_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
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

func TestB359InferenceReadsTheReleaseItPicksOnce(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho inferred\n",
	})
	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	upstream, err := url.Parse(m.opts.GitHubAPI)
	must(t, err)

	var (
		mu    sync.Mutex
		asked []string
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()

		r.URL.Path = upstream.Path + r.URL.Path
		httputil.NewSingleHostReverseProxy(&url.URL{Scheme: upstream.Scheme, Host: upstream.Host}).ServeHTTP(w, r)
	}))
	t.Cleanup(api.Close)

	m.opts.GitHubAPI = api.URL

	// With a release age, oku infers from the newest release and then picks from
	// the versions. The pick is that same release.
	out, err := m.run(t, "", "add", "github:owner/tool", "--min-release-age", "1d")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, path := range asked {
		if strings.Contains(path, "/releases/tags/") {
			t.Fatalf("add inferred again from %s, the release it had read:\n%s", path, strings.Join(asked, "\n"))
		}
	}
}

func TestB359OneCommandReusesItsConnectionsToAHost(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho inferred\n",
	})
	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	upstream, err := url.Parse(m.opts.GitHubAPI)
	must(t, err)

	var conns, requests atomic.Int32

	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		r.URL.Path = upstream.Path + r.URL.Path
		httputil.NewSingleHostReverseProxy(&url.URL{Scheme: upstream.Scheme, Host: upstream.Host}).ServeHTTP(w, r)
	}))
	api.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	api.Start()
	t.Cleanup(api.Close)

	m.opts.GitHubAPI = api.URL

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// add asks the API one request after another, so one connection carries them.
	if requests.Load() < 2 || conns.Load() != 1 {
		t.Fatalf("add made %d requests over %d connections, want one", requests.Load(), conns.Load())
	}
}
