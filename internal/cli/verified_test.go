package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// digestReleases fakes GitHub for owner/tool. Each tag of releases serves the
// same archive, and GitHub reports its digest when the tag maps to true. It
// returns a manifest that installs the file as an artifact, and the function
// that replaces the releases.
func digestReleases(t *testing.T, m *machine, releases map[string]bool) (string, func(map[string]bool)) {
	t.Helper()

	archive, sum := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho tool\n"})

	var (
		mu     sync.Mutex
		server *httptest.Server
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.URL.Path == "/api/repos/owner/tool/releases" {
			var listed []string

			for tag, digest := range releases {
				asset := fmt.Sprintf(`{"name": "tool.tar.gz", "browser_download_url": %q`,
					server.URL+"/owner/tool/releases/download/"+tag+"/tool.tar.gz")
				if digest {
					asset += fmt.Sprintf(`, "digest": "sha256:%s"`, sum)
				}

				listed = append(listed, fmt.Sprintf(`{"tag_name": %q, "assets": [%s}]}`, tag, asset))
			}

			fmt.Fprintf(w, "[%s]", strings.Join(listed, ","))

			return
		}

		if tag, ok := strings.CutPrefix(r.URL.Path, "/owner/tool/releases/download/"); ok {
			if _, listed := releases[strings.TrimSuffix(tag, "/tool.tar.gz")]; listed {
				http.ServeFile(w, r, archive)

				return
			}
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"%s/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
		server.URL,
	)), 0o644))

	return path, func(next map[string]bool) {
		mu.Lock()
		defer mu.Unlock()

		releases = next
	}
}

func TestB496TheLockRecordsWhatTheDownloadWasCheckedAgainst(t *testing.T) {
	m := newMachine(t)
	tool, _ := digestReleases(t, &m, map[string]bool{"v1.0.0": true})
	lockPath := filepath.Join(m.config, "oku.lock")

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	added, err := os.ReadFile(lockPath)
	must(t, err)

	if !strings.Contains(string(added), "verified = 'published'") {
		t.Fatalf("want the lock to record the digest GitHub reports:\n%s", added)
	}

	// A sync of the locked download writes the same lock, so --locked passes.
	_, err = m.run(t, "", "sync", "--locked")
	must(t, err)

	synced, err := os.ReadFile(lockPath)
	must(t, err)

	if string(synced) != string(added) {
		t.Fatalf("sync changed the lock:\n%s\nwant\n%s", synced, added)
	}

	// A release file that nothing states a digest for is trusted on first use.
	first := newMachine(t)
	plain, _ := digestReleases(t, &first, map[string]bool{"v1.0.0": false})

	_, err = first.run(t, "", "add", plain)
	must(t, err)

	text, err := os.ReadFile(filepath.Join(first.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(text), "verified = 'first-use'") {
		t.Fatalf("want the lock to record a first use:\n%s", text)
	}
}

func TestB497AWeakerCheckStopsUpdateUntilAccepted(t *testing.T) {
	m := newMachine(t)
	tool, publish := digestReleases(t, &m, map[string]bool{"v1.0.0": true})
	lockPath := filepath.Join(m.config, "oku.lock")

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	before, err := os.ReadFile(lockPath)
	must(t, err)

	// The next release has no digest, so oku would trust its first download.
	publish(map[string]bool{"v1.0.0": true, "v1.1.0": false})

	_, err = m.run(t, "", "update")
	if err == nil || !strings.Contains(err.Error(), "--accept-weaker-check") ||
		!strings.Contains(err.Error(), "the digest that its source publishes") {
		t.Fatalf("want update stopped for a weaker check, got %v", err)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(after) != string(before) {
		t.Fatalf("a refused update changed the lock:\n%s", after)
	}

	if _, err := m.run(t, "", "add", tool, "--plan"); err == nil ||
		!strings.Contains(err.Error(), "--accept-weaker-check") {
		t.Fatalf("want the plan to stop like add, got %v", err)
	}

	_, err = m.run(t, "", "update", "--accept-weaker-check")
	must(t, err)

	after, err = os.ReadFile(lockPath)
	must(t, err)

	if !strings.Contains(string(after), "version = '1.1.0'") ||
		!strings.Contains(string(after), "verified = 'first-use'") {
		t.Fatalf("want the accepted version and its check in the lock:\n%s", after)
	}
}

func TestB498PlanAndInfoShowTheCheck(t *testing.T) {
	m := newMachine(t)
	tool, _ := digestReleases(t, &m, map[string]bool{"v1.0.0": true})

	out, err := m.run(t, "", "add", tool, "--plan", "--json")
	must(t, err)

	var plans []map[string]any
	must(t, json.Unmarshal([]byte(out[strings.Index(out, "["):]), &plans))

	if len(plans) != 1 || plans[0]["verified"] != "published" ||
		plans[0]["verify"] != "sha256 the release publishes" {
		t.Fatalf("want the plan to name the published digest:\n%s", out)
	}

	_, err = m.run(t, "", "add", tool)
	must(t, err)

	out, err = m.run(t, "", "info", "tool")
	must(t, err)

	if !strings.Contains(out, "the digest that its source publishes") {
		t.Fatalf("want info to show the check:\n%s", out)
	}
}
