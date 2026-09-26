package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// gitHostServer answers the forge API calls that a git-tags or a git-branch
// source makes, and counts them.
type gitHostServer struct {
	*httptest.Server
	tags    []string
	sha     string
	date    string
	refuse  atomic.Bool
	asked   atomic.Int64
	refused atomic.Int64
}

// newTagServer lists tags, the way a git-tags source reads them.
func newTagServer(t *testing.T, tags ...string) *gitHostServer {
	t.Helper()

	return newGitHostServer(t, &gitHostServer{tags: tags})
}

// newBranchServer names the newest commit of a branch, the way a git-branch
// source reads it.
func newBranchServer(t *testing.T, sha, date string) *gitHostServer {
	t.Helper()

	return newGitHostServer(t, &gitHostServer{sha: sha, date: date})
}

func newGitHostServer(t *testing.T, host *gitHostServer) *gitHostServer {
	t.Helper()

	host.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/tags":
			if host.refuse.Load() {
				host.refused.Add(1)
				http.Error(w, `{"message": "API rate limit exceeded"}`, http.StatusForbidden)

				return
			}

			host.asked.Add(1)

			names := make([]string, len(host.tags))
			for i, tag := range host.tags {
				names[i] = fmt.Sprintf(`{"name": %q}`, tag)
			}

			fmt.Fprintf(w, "[%s]", strings.Join(names, ","))
		case "/api/repos/owner/tool/commits/main":
			host.asked.Add(1)

			fmt.Fprintf(
				w,
				`{"sha": %q, "commit": {"committer": {"date": %q}}}`,
				host.sha, host.date,
			)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(host.Close)

	return host
}

func TestB381AGitTagsVersionComesFromTheForgeAPI(t *testing.T) {
	m := newMachine(t)
	host := newTagServer(t, "v1.0.0", "v1.1.0")
	m.opts.GitHubAPI = host.URL + "/api"

	for _, version := range []string{"1.0.0", "1.1.0"} {
		m.archive(t, "tool-v"+version, map[string]string{"tool": "#!/bin/sh\necho " + version + "\n"})
	}

	// github.com/owner/tool does not exist, so a run that asked git would fail.
	ref := filepath.Join(m.fixtures, "tags.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"git-tags\"\nrepo = \"https://github.com/owner/tool\"\n"+
			"strip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{tag}}.tar.gz\"\nbin = [\"tool\"]\n",
		m.fixtures,
	)), 0o644))

	out, err := m.run(t, "", "add", ref, "--accept-unknown-age")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("add installed %s, want 1.1.0 from the tags the API listed", got)
	}

	if host.asked.Load() == 0 {
		t.Fatal("add did not ask the API for the tags")
	}
}

func TestB382AGitBranchVersionComesFromTheForgeAPI(t *testing.T) {
	m := newMachine(t)
	host := newBranchServer(t, "abcdef1234567890abcdef1234567890abcdef12", "2026-09-20T10:00:00Z")
	m.opts.GitHubAPI = host.URL + "/api"

	version := "2026.09.20-abcdef1"
	m.archive(t, "tool-"+version, map[string]string{"tool": "#!/bin/sh\necho " + version + "\n"})

	ref := filepath.Join(m.fixtures, "branch-api.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"git-branch\"\nrepo = \"https://github.com/owner/tool\"\n"+
			"branch = \"main\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{version}}.tar.gz\"\nbin = [\"tool\"]\n",
		m.fixtures,
	)), 0o644))

	out, err := m.run(t, "", "add", ref)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != version {
		t.Fatalf("add installed %s, want %s from the commit the API named", got, version)
	}

	if host.asked.Load() == 0 {
		t.Fatal("add did not ask the API for the newest commit of the branch")
	}
}

func TestB383AGitTagsVersionFallsBackToGitWhenTheHostRefuses(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}

	m := newMachine(t)
	host := newTagServer(t)
	host.refuse.Store(true)
	m.opts.GitHubAPI = host.URL + "/api"

	// GitHubWeb is where the tests serve github.com from, so a repository under
	// it is on a host oku knows and git can read.
	repo := filepath.Join(m.fixtures, "owner", "tool")
	m.opts.GitHubWeb = "file://" + filepath.ToSlash(m.fixtures)

	must(t, os.MkdirAll(repo, 0o755))
	gitIn(t, repo, "init", "--quiet", "--initial-branch", "main")
	must(t, os.WriteFile(filepath.Join(repo, "readme"), []byte("tool"), 0o644))
	gitIn(t, repo, "add", "readme")
	gitIn(t, repo, "commit", "--quiet", "-m", "first")
	gitIn(t, repo, "tag", "v1.2.0")

	m.archive(t, "tool-v1.2.0", map[string]string{"tool": "#!/bin/sh\necho 1.2.0\n"})

	ref := filepath.Join(m.fixtures, "fallback.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"git-tags\"\nrepo = \"file://%s/owner/tool\"\n"+
			"strip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{tag}}.tar.gz\"\nbin = [\"tool\"]\n",
		filepath.ToSlash(m.fixtures), m.fixtures,
	)), 0o644))

	out, err := m.run(t, "", "add", ref, "--accept-unknown-age")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.2.0" {
		t.Fatalf("add installed %s, want 1.2.0 from the tags git listed", got)
	}

	if host.refused.Load() == 0 {
		t.Fatal("add did not ask the API before it asked git")
	}
}

// gitIn runs git in dir with an author and no user configuration.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
