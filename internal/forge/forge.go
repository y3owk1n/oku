// Package forge reads repositories and releases from the hosts that serve them.
package forge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrNotFound reports that the host has no such repository, file, tag or
// release.
var ErrNotFound = errors.New("not found")

// The kinds of forge. A manifest's version.from is a kind plus "-releases".
const (
	KindGitHub = "github"
	KindGitea  = "gitea"
	KindGitLab = "gitlab"
)

// Forge is one host. A repo is "owner/name", without the host.
type Forge interface {
	// Kind is the API the host serves.
	Kind() string
	// Host is the host's name, or "" for github.com.
	Host() string
	// Auth returns the login for downloads from this host.
	Auth() Auth
	// Home returns the web page of repo.
	Home(repo string) string
	// Head returns the newest commit of the default branch.
	Head(ctx context.Context, repo string) (string, error)
	// File returns the file at path in commit.
	File(ctx context.Context, repo, commit, path string) ([]byte, error)
	// Files lists the paths of the files in commit.
	Files(ctx context.Context, repo, commit string) ([]string, error)
	// Release returns the release of tag. An empty tag means the newest release
	// that is no draft and no prerelease.
	Release(ctx context.Context, repo, tag string) (Release, error)
	// Releases lists the newest releases.
	Releases(ctx context.Context, repo string) ([]Release, error)
	// TagCommit returns the commit that tag points at.
	TagCommit(ctx context.Context, repo, tag string) (Commit, error)
	// Archive returns the files of commit as a tar.gz with one top directory.
	Archive(ctx context.Context, repo, commit string) ([]byte, error)
}

// Release is a release with its downloads.
type Release struct {
	Tag string
	// Commit is the commit the release was made from, when upstream made it from
	// a commit and not from a branch.
	Commit     string
	Draft      bool
	Prerelease bool
	Assets     []Asset
}

// Asset is one download of a release.
type Asset struct {
	Name string
	URL  string
	// Digest is the sha256 the host reports, or "".
	Digest string
	// Size is the bytes of the download, or 0 when the host does not say.
	Size int64
}

// maxReleases is the most releases a forge lists. A repo with two release
// streams, such as a stable and a nightly one, needs more than one page for the
// older stream to show.
const maxReleases = 1000

// Auth is the Authorization header for the downloads of one host. The zero
// value sends nothing.
type Auth struct {
	Host   string
	Header string
}

// For returns the header for a download at rawURL, or "". It is for https URLs
// on Host only, so a token never goes to another server or over plain http.
func (a Auth) For(rawURL string) string {
	at, err := url.Parse(rawURL)
	if err != nil || a.Header == "" || at.Scheme != "https" || at.Host != a.Host {
		return ""
	}

	return a.Header
}

// Commit is a commit and the time it was made.
type Commit struct {
	SHA  string
	Date time.Time
}

// Hosts opens forges. GitHubAPI and GitHubRaw replace the github.com URLs when
// set, which tests do.
type Hosts struct {
	HTTP      *http.Client
	GitHubAPI string
	GitHubRaw string
}

// GitHub returns github.com for an empty host, else the GitHub Enterprise
// Server at host. oku sends GITHUB_TOKEN to github.com only, and
// GH_ENTERPRISE_TOKEN to an Enterprise Server only. When the variable is not
// set, oku asks the gh CLI for its login to that host.
func (h Hosts) GitHub(host string) Forge {
	return checked{Forge: h.github(host), name: "GitHub"}
}

func (h Hosts) github(host string) *github {
	if host != "" {
		return &github{
			http:  h.HTTP,
			host:  host,
			web:   "https://" + host,
			api:   "https://" + host + "/api/v3",
			token: tokenFor("GH_ENTERPRISE_TOKEN", host),
			env:   "GH_ENTERPRISE_TOKEN",
		}
	}

	g := &github{
		http:  h.HTTP,
		web:   "https://github.com",
		api:   "https://api.github.com",
		raw:   "https://raw.githubusercontent.com",
		token: tokenFor("GITHUB_TOKEN", "github.com"),
		env:   "GITHUB_TOKEN",
	}

	if h.GitHubAPI != "" {
		g.api = h.GitHubAPI
	}

	if h.GitHubRaw != "" {
		g.raw = h.GitHubRaw
	}

	return g
}

// ghTokens holds what "gh auth token" printed for each host and PATH, so oku
// runs gh once per host.
var ghTokens sync.Map

// tokenFor returns the variable env, or else the token that the gh CLI holds
// for host, or "". Many users log in with gh and set no variable, and GitHub
// counts every request without a token against a limit of 60 an hour.
func tokenFor(env, host string) string {
	if token := os.Getenv(env); token != "" {
		return token
	}

	key := host + "\x00" + os.Getenv("PATH")
	if token, done := ghTokens.Load(key); done {
		return token.(string)
	}

	token := ""

	// gh picks its default host without --hostname, which may be another one.
	if gh, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(gh, "auth", "token", "--hostname", host).Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
		}
	}

	ghTokens.Store(key, token)

	return token
}

// Open returns the forge that a ref's scheme and location name, and the repo
// on it. The schemes are "github", "gitea" for any Gitea or Forgejo server, and
// "codeberg" for codeberg.org, and "gitlab".
func (h Hosts) Open(scheme, location string) (Forge, string, error) {
	switch scheme {
	case KindGitHub:
		host, repo := Split(location)

		return h.GitHub(host), repo, nil
	case "codeberg":
		return checked{Forge: h.gitea("codeberg.org"), name: "Codeberg"}, location, nil
	case KindGitea:
		// An owner on these servers may have a dot, so the host is always there.
		host, repo, _ := strings.Cut(location, "/")

		return checked{Forge: h.gitea(host), name: host}, repo, nil
	case KindGitLab:
		host, repo := Split(location)
		if host == "gitlab.com" {
			host = ""
		}

		// GITLAB_TOKEN is for gitlab.com and GITLAB_SERVER_TOKEN for every other
		// host.
		env := "GITLAB_TOKEN"
		if host != "" {
			env = "GITLAB_SERVER_TOKEN"
		}

		return checked{
			Forge: &gitlab{http: h.HTTP, host: host, token: os.Getenv(env)}, name: cmp.Or(host, "GitLab"),
		}, repo, nil
	default:
		return nil, "", fmt.Errorf("%q is not a forge oku knows", scheme)
	}
}

// AuthFor returns the login for a package's downloads. from and repo are the
// version.from and version.repo of its manifest. A manifest that does not
// follow a forge's releases gets nothing.
func (h Hosts) AuthFor(from, repo string) Auth {
	kind, ok := strings.CutSuffix(from, "-releases")
	if !ok {
		return Auth{}
	}

	server, _, err := h.Open(kind, repo)
	if err != nil {
		return Auth{}
	}

	return server.Auth()
}

// gitea returns the Gitea or Forgejo server at host. CODEBERG_TOKEN is for
// codeberg.org and GITEA_TOKEN for every other host.
func (h Hosts) gitea(host string) Forge {
	env := "GITEA_TOKEN"
	if host == "codeberg.org" {
		env = "CODEBERG_TOKEN"
	}

	return &gitea{http: h.HTTP, host: host, token: os.Getenv(env)}
}

// Split cuts the host off a location such as "git.example.com/owner/repo". A
// first part with a dot is a host, because no owner name has one.
func Split(location string) (string, string) {
	first, rest, ok := strings.Cut(location, "/")
	if ok && strings.Contains(first, ".") {
		return first, rest
	}

	return "", location
}
