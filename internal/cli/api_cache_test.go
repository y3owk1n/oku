package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	old := time.Now().Add(-31 * 24 * time.Hour)
	must(t, os.Chtimes(kept[0], old, old))

	// The host answers 304 to update, so oku reads the kept answer, and that
	// read makes it new again.
	server.unchanged = 0

	_, err = m.run(t, "", "update")
	must(t, err)

	if server.unchanged != 1 || len(apiAnswers(t, m)) != 1 {
		t.Fatalf("update got %d answers of 304 and left %d answers, want 1 and 1",
			server.unchanged, len(apiAnswers(t, m)))
	}

	out, err := m.run(t, "", "gc", "--cache")
	must(t, err)

	if len(apiAnswers(t, m)) != 1 {
		t.Fatalf("gc deleted an answer that update read since:\n%s", out)
	}

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

func TestB401GCOlderThanSetsHowLongAnAnswerStays(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0"))
	must(t, err)

	kept := apiAnswers(t, m)
	if len(kept) != 1 {
		t.Fatalf("the cache holds %d answers, want 1", len(kept))
	}

	week := time.Now().Add(-8 * 24 * time.Hour)
	must(t, os.Chtimes(kept[0], week, week))

	// By default an answer stays for a month, so this one stays.
	out, err := m.run(t, "", "gc", "--cache")
	must(t, err)

	if len(apiAnswers(t, m)) != 1 {
		t.Fatalf("gc deleted an answer read a week ago:\n%s", out)
	}

	// --older-than says how long it stays instead.
	out, err = m.run(t, "", "gc", "--cache", "--older-than", "2d")
	must(t, err)

	if len(apiAnswers(t, m)) != 0 {
		t.Fatalf("--older-than 2d kept an answer read a week ago:\n%s", out)
	}

	if !strings.Contains(out, "1 answer from the API cache") {
		t.Fatalf("gc did not say what it deleted from the API cache:\n%s", out)
	}
}

func TestB402GCCacheOlderThanClearsTheCacheAndKeepsEveryGeneration(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0"))
	must(t, err)

	for _, name := range []string{"one", "two"} {
		_, err := m.run(t, "", "add", m.namedManifest(t, name, name, name))
		must(t, err)
	}

	// Generations 1 and 2 are older than the age, so a gc that read it as an
	// age for generations would delete one.
	for n, age := range map[int]time.Duration{1: 10, 2: 8} {
		path := filepath.Join(m.genDir(n), "oku-gen.toml")
		data, err := os.ReadFile(path)
		must(t, err)

		created := time.Now().Add(-age * 24 * time.Hour).UTC().Format(time.RFC3339)
		data = regexp.MustCompile(`(?m)^created = .*$`).ReplaceAll(data, []byte("created = "+created))
		must(t, os.WriteFile(path, data, 0o644))
	}

	before, err := m.run(t, "", "generations")
	must(t, err)

	kept := apiAnswers(t, m)
	if len(kept) != 1 {
		t.Fatalf("the cache holds %d answers, want 1", len(kept))
	}

	week := time.Now().Add(-8 * 24 * time.Hour)
	must(t, os.Chtimes(kept[0], week, week))

	// --cache-older-than turns --cache on by itself.
	out, err := m.run(t, "", "gc", "--cache-older-than", "2d")
	must(t, err)

	if len(apiAnswers(t, m)) != 0 {
		t.Fatalf("--cache-older-than 2d kept an answer read a week ago:\n%s", out)
	}

	// It names an age for the cache only, so every generation stays.
	after, err := m.run(t, "", "generations")
	must(t, err)

	if after != before {
		t.Fatalf("--cache-older-than changed the generations:\n%s\nwant:\n%s", after, before)
	}

	// The error for an age oku cannot read names the flag it came from.
	_, err = m.run(t, "", "gc", "--cache-older-than", "soon")
	if err == nil || !strings.Contains(err.Error(), "--cache-older-than takes") {
		t.Fatalf("--cache-older-than with an age it cannot read: %v", err)
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

	server.tags = []string{"v1.1.0", "v1.0.0"}

	// The old answer carries the ETag the host gives now, so the host would
	// answer 304 if oku sent it.
	resp, err := server.Client().Get(server.URL + "/api/repos/owner/tool/releases")
	must(t, err)
	resp.Body.Close()

	// An older oku kept the whole answer as one JSON document, with the body
	// inside it as base64.
	must(t, os.WriteFile(kept[0], []byte(
		`{"etag": `+strconv.Quote(resp.Header.Get("ETag"))+`, "type": "application/json", "body": "W10="}`,
	), 0o644))

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
