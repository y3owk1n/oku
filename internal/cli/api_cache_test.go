package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// apiAnswers returns the files of the API cache.
func apiAnswers(t *testing.T, m machine) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(m.cache, "api"))
	must(t, err)

	var files []string

	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(m.cache, "api", entry.Name()))
		}
	}

	return files
}

func TestB384GCCacheDeletesAnAnswerNoCommandHasReadForAMonth(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0"))
	must(t, err)

	kept := apiAnswers(t, m)
	if len(kept) != 1 {
		t.Fatalf("the cache holds %d answers, want 1", len(kept))
	}

	// An answer read today stays.
	out, err := m.run(t, "", "gc", "--cache")
	must(t, err)

	if len(apiAnswers(t, m)) != 1 {
		t.Fatalf("gc deleted an answer oku just read:\n%s", out)
	}

	old := time.Now().Add(-31 * 24 * time.Hour)
	must(t, os.Chtimes(kept[0], old, old))

	out, err = m.run(t, "", "gc", "--cache")
	must(t, err)

	if len(apiAnswers(t, m)) != 0 {
		t.Fatalf("gc kept an answer no command read for a month:\n%s", out)
	}

	if !strings.Contains(out, "1 answer from the API cache") {
		t.Fatalf("gc did not say what it deleted from the API cache:\n%s", out)
	}
}

func TestB385AnAnswerFromAnOlderOkuIsAskedForAgain(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	ref := m.discoveredManifest(t, "1.0.0", "1.1.0")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	kept := apiAnswers(t, m)
	if len(kept) != 1 {
		t.Fatalf("the cache holds %d answers, want 1", len(kept))
	}

	// An older oku kept the whole answer as one JSON document, with the body
	// inside it as base64.
	must(t, os.WriteFile(kept[0], []byte(
		`{"etag": "\"old\"", "type": "application/json", "body": "W10="}`,
	), 0o644))

	server.tags = []string{"v1.1.0", "v1.0.0"}
	server.unchanged = 0

	_, err = m.run(t, "", "update")
	must(t, err)

	if server.unchanged != 0 {
		t.Fatal("oku sent If-None-Match with an ETag it read from the older format")
	}

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("update installed %s, want 1.1.0 from the answer oku asked for again", got)
	}
}
