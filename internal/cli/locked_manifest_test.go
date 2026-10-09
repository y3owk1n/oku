package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestB555UpdateOfOnePackageReadsTheOthersLockedManifestsWithoutAskingGitHub(t *testing.T) {
	const (
		first  = "0123456789abcdef0123456789abcdef01234567"
		second = "89abcdef0123456789abcdef0123456789abcdef"
	)

	m := newMachine(t)

	v1, err := os.ReadFile(m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`))
	must(t, err)

	v2 := bytes.ReplaceAll(v1, []byte("1.2.3"), []byte("2.0.0"))

	var (
		head  atomic.Value
		reads atomic.Int32
	)

	head.Store(first)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/repo/commits/HEAD":
			_, _ = w.Write([]byte(head.Load().(string)))
		case "/raw/owner/repo/" + first + "/oku.pkg.toml":
			reads.Add(1)
			_, _ = w.Write(v1)
		case "/raw/owner/repo/" + second + "/oku.pkg.toml":
			reads.Add(1)
			_, _ = w.Write(v2)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	for _, ref := range []string{
		"github:owner/repo",
		m.manifest(t, "other", map[string]string{"other": script}, `bin = ["other"]`),
	} {
		_, err := m.run(t, "", "add", ref)
		must(t, err)
	}

	// The file at a commit never changes, so the later commands reuse the answer
	// that add got.
	for _, args := range [][]string{{"update", "other"}, {"sync"}, {"update"}} {
		out, err := m.run(t, "", args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	if got := reads.Load(); got != 1 {
		t.Fatalf("GitHub served the manifest at %s %d times, want once", first[:7], got)
	}

	// When HEAD moves to another commit, update reads the manifest there.
	head.Store(second)

	_, err = m.run(t, "", "update")
	must(t, err)

	out, err := m.run(t, "", "list")
	must(t, err)

	if got := reads.Load(); got != 2 || !strings.Contains(out, "2.0.0") {
		t.Fatalf("update after HEAD moved read %d manifests and listed:\n%s", got, out)
	}
}

func TestB555UpdateOfOnePackageReadsTheOthersLockedManifestsFromTheClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	m := newMachine(t)
	repo := filepath.Join(m.fixtures, "repo")

	m.opts.Interactive = yes()

	for _, ref := range []string{
		m.commitManifest(t, repo, "1.0.0"),
		m.manifest(t, "other", map[string]string{"other": script}, `bin = ["other"]`),
	} {
		_, err := m.run(t, "y\n", "add", ref)
		must(t, err)
	}

	// The clone in the cache holds the locked commit, so oku does not ask the
	// repo.
	must(t, os.Rename(repo, repo+"-gone"))

	out, err := m.run(t, "", "update", "other")
	if err != nil {
		t.Fatalf("update other asked the repo of tool: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "built 1.0.0" {
		t.Fatalf("tool printed %q", got)
	}
}
