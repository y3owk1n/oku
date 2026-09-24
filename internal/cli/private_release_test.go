package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestB335APrivateGitHubReleaseDownloadsThroughTheAPIWithTheToken(t *testing.T) {
	m := newMachine(t)
	archive, sum := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho private\n"})
	body, err := os.ReadFile(archive)
	must(t, err)

	// Signed storage on another host, which must never see the token.
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the token went to the storage host")
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(storage.Close)

	var github *httptest.Server

	github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized := r.Header.Get("Authorization") == "Bearer secret"

		switch {
		case r.URL.Path == "/owner/tool/releases/download/v1.0.0/tool.tar.gz":
			// A private repo's download page answers 404, with or without a token.
			http.NotFound(w, r)
		case r.URL.Path == "/api/repos/owner/tool/releases/tags/v1.0.0" && authorized:
			fmt.Fprintf(w, `{"tag_name": "v1.0.0", "assets": [{"name": "tool.tar.gz", `+
				`"browser_download_url": %q, "url": %q}]}`,
				github.URL+"/owner/tool/releases/download/v1.0.0/tool.tar.gz",
				github.URL+"/api/repos/owner/tool/releases/assets/7")
		case r.URL.Path == "/api/repos/owner/tool/releases/assets/7" && authorized &&
			r.Header.Get("Accept") == "application/octet-stream":
			http.Redirect(w, r, storage.URL+"/signed/tool.tar.gz", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(github.Close)

	m.opts.GitHubAPI, m.opts.GitHubWeb = github.URL+"/api", github.URL

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = %q\nsha256 = %q\nbin = [\"tool\"]\n",
		github.URL+"/owner/tool/releases/download/v1.0.0/tool.tar.gz", sum,
	))

	t.Setenv("GITHUB_TOKEN", "")

	if _, err := m.run(t, "", "add", ref); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want the download refused without a token, got %v", err)
	}

	t.Setenv("GITHUB_TOKEN", "secret")

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add with a token: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "private" {
		t.Fatalf("tool printed %q", got)
	}
}
