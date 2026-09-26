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
	"regexp"
	"strconv"
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
	// Releases lists the newest releases. With all false it reads the first page
	// only and reports whether the host has more pages.
	Releases(ctx context.Context, repo string, all bool) (found []Release, more bool, err error)
	// Tags lists the repo's tags, newest first where the host says so.
	Tags(ctx context.Context, repo string) ([]string, error)
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
	// Published is when the release was published, or zero when the host does
	// not say. The creation date of a release is its commit's, which its author
	// sets.
	Published time.Time
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

// maxTags is the most tags a forge lists. A repository tags at least every
// release, and many tag more often, so this cap is higher than the one for
// releases.
const maxTags = 2000

// pagesAtOnce is the most pages of one list that oku asks for at the same time.
const pagesAtOnce = 4

var (
	nextPageRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)
	lastPageRe = regexp.MustCompile(`<([^>]+)>;\s*rel="last"`)
)

// nextPage returns the URL of the next page that link names, or "". A next
// page on another host would get the token, so oku does not follow it.
func nextPage(link, prefix string) string {
	if m := nextPageRe.FindStringSubmatch(link); m != nil && strings.HasPrefix(m[1], prefix) {
		return m[1]
	}

	return ""
}

// lastPage reads the Link header of one page of a list. It returns the number
// of the last page, -1 when there is a next page but the header names no last
// one, and 0 when this page is the last.
func lastPage(link string) int {
	if m := lastPageRe.FindStringSubmatch(link); m != nil {
		if at, err := url.Parse(m[1]); err == nil {
			if n, err := strconv.Atoi(at.Query().Get("page")); err == nil {
				return n
			}
		}
	}

	if nextPageRe.MatchString(link) {
		return -1
	}

	return 0
}

// readReleases reads a list of releases with size releases to a page, up to
// maxReleases. With all false it reads the first page only.
func readReleases(
	ctx context.Context,
	size int,
	all bool,
	read func(ctx context.Context, page int) ([]Release, int, error),
) ([]Release, bool, error) {
	return readPages(ctx, size, maxReleases, all, read)
}

// readTags reads a list of tags with size tags to a page, up to maxTags.
func readTags(
	ctx context.Context,
	size int,
	read func(ctx context.Context, page int) ([]string, int, error),
) ([]string, error) {
	tags, _, err := readPages(ctx, size, maxTags, true, read)

	return tags, err
}

// readPages reads a list with size items to a page, up to most items.
// read returns one page and the last page, as lastPage gives it. When the first
// page names the last one, oku asks for the others at once, a few at a time.
// Otherwise it reads one page after another. With all false it stops after the
// first page and reports whether the host has more.
func readPages[T any](
	ctx context.Context,
	size, most int,
	all bool,
	read func(ctx context.Context, page int) ([]T, int, error),
) ([]T, bool, error) {
	items, last, err := read(ctx, 1)
	if err != nil {
		return nil, false, err
	}

	if !all {
		return items, last != 0, nil
	}

	pageLimit := most / size

	if last > 1 {
		pages := make([][]T, min(last, pageLimit)+1)
		errs := make([]error, len(pages))

		var wg sync.WaitGroup

		limit := make(chan struct{}, pagesAtOnce)

		for page := 2; page < len(pages); page++ {
			wg.Go(func() {
				limit <- struct{}{}
				defer func() { <-limit }()

				pages[page], _, errs[page] = read(ctx, page)
			})
		}

		wg.Wait()

		if err := cmp.Or(errs...); err != nil {
			return nil, false, err
		}

		for _, found := range pages[2:] {
			items = append(items, found...)
		}

		return items, false, nil
	}

	for page := 2; last < 0 && page <= pageLimit; page++ {
		var found []T
		if found, last, err = read(ctx, page); err != nil {
			return nil, false, err
		}

		items = append(items, found...)
	}

	return items, false, nil
}

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
	// GitHubWeb replaces https://github.com, for tests.
	GitHubWeb string
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

	if h.GitHubWeb != "" {
		g.web = h.GitHubWeb
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

// OpenURL returns the forge that serves the git repository at rawURL, the repo
// on it, and whether oku knows that host. A host oku does not know, and a URL
// that names no repository, give false, and the caller reads the repository
// with git instead.
func (h Hosts) OpenURL(rawURL string) (Forge, string, bool) {
	kind, path := "", ""

	// A test serves github.com from its own address.
	if h.GitHubWeb != "" && strings.HasPrefix(rawURL, h.GitHubWeb+"/") {
		kind, path = KindGitHub, strings.TrimPrefix(rawURL, h.GitHubWeb+"/")
	} else {
		at, err := url.Parse(rawURL)
		if err != nil || at.Scheme != "https" && at.Scheme != "http" {
			return nil, "", false
		}

		path = strings.Trim(at.Path, "/")

		switch at.Host {
		case "github.com":
			kind = KindGitHub
		case "gitlab.com":
			kind = KindGitLab
		case "codeberg.org":
			kind = "codeberg"
		default:
			return nil, "", false
		}
	}

	repo := strings.TrimSuffix(path, ".git")
	if strings.Count(repo, "/") == 0 {
		return nil, "", false
	}

	// Open reads a host off the front of a location when it holds a dot, which a
	// GitLab group may. The host is known here, so it goes in front.
	location := repo
	if kind == KindGitLab {
		location = "gitlab.com/" + repo
	}

	server, on, err := h.Open(kind, location)
	if err != nil {
		return nil, "", false
	}

	return server, on, true
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
