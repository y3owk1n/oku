package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestB361ARegistryDocumentOver32MBIsReadWhole(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.0.0")

	upstream, err := url.Parse(m.opts.NPMRegistry)
	must(t, err)

	// The registry pads the package's document to 40 MB with a readme and sends
	// an ETag, as npm does for a package with thousands of versions.
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/-/") {
			httputil.NewSingleHostReverseProxy(upstream).ServeHTTP(w, r)

			return
		}

		resp, err := http.Get(upstream.String() + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)

			return
		}
		defer resp.Body.Close()

		var doc map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)

			return
		}

		doc["readme"] = strings.Repeat("x", 40<<20)

		w.Header().Set("ETag", `"big"`)
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(registry.Close)

	m.opts.NPMRegistry = registry.URL

	out, err := m.run(t, "", "add", "npm:@scope/tool", "--plan")
	if err != nil {
		t.Fatalf("add --plan of a 40 MB document: %v\n%s", err, out)
	}

	if !strings.Contains(out, "1.0.0") {
		t.Fatalf("add --plan did not find version 1.0.0:\n%s", out)
	}
}

