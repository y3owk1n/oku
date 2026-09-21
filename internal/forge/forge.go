// Package forge reads repositories and releases from the hosts that serve them.
package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
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
// GH_ENTERPRISE_TOKEN to an Enterprise Server only.
func (h Hosts) GitHub(host string) Forge {
	if host != "" {
		return &github{
			http:  h.HTTP,
			host:  host,
			web:   "https://" + host,
			api:   "https://" + host + "/api/v3",
			token: os.Getenv("GH_ENTERPRISE_TOKEN"),
			env:   "GH_ENTERPRISE_TOKEN",
		}
	}

	g := &github{
		http:  h.HTTP,
		web:   "https://github.com",
		api:   "https://api.github.com",
		raw:   "https://raw.githubusercontent.com",
		token: os.Getenv("GITHUB_TOKEN"),
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

// Open returns the forge that a ref's scheme and location name, and the repo
// on it. The schemes are "github", "gitea" for any Gitea or Forgejo server, and
// "codeberg" for codeberg.org, and "gitlab".
func (h Hosts) Open(scheme, location string) (Forge, string, error) {
	switch scheme {
	case KindGitHub:
		host, repo := Split(location)

		return h.GitHub(host), repo, nil
	case "codeberg":
		return h.gitea("codeberg.org"), location, nil
	case KindGitea:
		// An owner on these servers may have a dot, so the host is always there.
		host, repo, _ := strings.Cut(location, "/")

		return h.gitea(host), repo, nil
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

		return &gitlab{http: h.HTTP, host: host, token: os.Getenv(env)}, repo, nil
	default:
		return nil, "", fmt.Errorf("%q is not a forge oku knows", scheme)
	}
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
