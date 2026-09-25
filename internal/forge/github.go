package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// githubVersion is the version of GitHub's REST API that oku asks github.com
// for. GitHub keeps a version for 24 months after the next one ships, and then
// answers it with 410 Gone.
const githubVersion = "2026-03-10"

// maxBody is the most bytes a forge reads from one answer. 100 releases of
// astral-sh/uv, each with all its assets, were 8 MB in September 2026.
const maxBody = 32 << 20

// github is github.com or a GitHub Enterprise Server.
type github struct {
	http *http.Client
	// host is "" for github.com.
	host string
	web  string
	api  string
	// raw serves files without the API's rate limit. It is empty for an
	// Enterprise Server, whose files come from the API.
	raw   string
	token string
	// env names the variable that holds token.
	env string
}

type githubRelease struct {
	Tag        string `json:"tag_name"`
	Commit     string `json:"target_commitish"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		// API is the asset's address in the API, which serves the file of a
		// private repo too.
		API    string `json:"url"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

func (r githubRelease) release() Release {
	out := Release{Tag: r.Tag, Commit: r.Commit, Draft: r.Draft, Prerelease: r.Prerelease}

	for _, asset := range r.Assets {
		digest, _ := strings.CutPrefix(asset.Digest, "sha256:")
		out.Assets = append(out.Assets, Asset{
			Name: asset.Name, URL: asset.URL, Digest: digest, Size: asset.Size,
		})
	}

	return out
}

func (g *github) Kind() string { return KindGitHub }

func (g *github) Host() string { return g.host }

// Auth sends nothing. GitHub serves the file of a private release from its API
// only, and oku downloads the URL a release lists.
func (g *github) Auth() Auth { return Auth{} }

func (g *github) Home(repo string) string {
	return g.web + "/" + repo
}

func (g *github) Head(ctx context.Context, repo string) (string, error) {
	sha, err := g.get(ctx, g.api+"/repos/"+repo+"/commits/HEAD", "application/vnd.github.sha")

	return strings.TrimSpace(string(sha)), err
}

func (g *github) File(ctx context.Context, repo, commit, path string) ([]byte, error) {
	if g.raw != "" {
		return g.get(ctx, g.raw+"/"+repo+"/"+commit+"/"+path, "")
	}

	return g.get(
		ctx, g.api+"/repos/"+repo+"/contents/"+path+"?ref="+commit, "application/vnd.github.raw",
	)
}

func (g *github) Files(ctx context.Context, repo, commit string) ([]string, error) {
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}

	err := g.json(ctx, "/repos/"+repo+"/git/trees/"+commit+"?recursive=1", &tree)
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, item := range tree.Tree {
		if item.Type == "blob" {
			paths = append(paths, item.Path)
		}
	}

	return paths, nil
}

func (g *github) Release(ctx context.Context, repo, tag string) (Release, error) {
	which := "latest"
	if tag != "" {
		which = "tags/" + tag
	}

	var found githubRelease

	err := g.json(ctx, "/repos/"+repo+"/releases/"+which, &found)

	return found.release(), err
}

// releasePage is the releases oku asks for in one answer, the most GitHub
// gives. Each page is one request of the rate limit.
const releasePage = 100

// Releases reads the newest maxReleases releases.
func (g *github) Releases(ctx context.Context, repo string) ([]Release, error) {
	return readReleases(ctx, releasePage, func(ctx context.Context, page int) ([]Release, int, error) {
		at := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", g.api, repo, releasePage)
		if page > 1 {
			at += fmt.Sprintf("&page=%d", page)
		}

		body, link, err := g.page(ctx, at, "application/vnd.github+json")
		if err != nil {
			return nil, 0, err
		}

		var found []githubRelease
		if err := json.Unmarshal(body, &found); err != nil {
			return nil, 0, err
		}

		releases := make([]Release, len(found))
		for i, release := range found {
			releases[i] = release.release()
		}

		return releases, lastPage(link), nil
	})
}

func (g *github) TagCommit(ctx context.Context, repo, tag string) (Commit, error) {
	var found struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}

	err := g.json(ctx, "/repos/"+repo+"/commits/"+tag, &found)

	return Commit{SHA: found.SHA, Date: found.Commit.Committer.Date}, err
}

func (g *github) Archive(ctx context.Context, repo, commit string) ([]byte, error) {
	return g.get(ctx, g.api+"/repos/"+repo+"/tarball/"+commit, "")
}

func (g *github) json(ctx context.Context, path string, into any) error {
	body, err := g.get(ctx, g.api+path, "application/vnd.github+json")
	if err != nil {
		return err
	}

	return json.Unmarshal(body, into)
}

func (g *github) get(ctx context.Context, url, accept string) ([]byte, error) {
	body, _, err := g.page(ctx, url, accept)

	return body, err
}

// page reads url and returns the body and the Link header, which names the
// other pages of a list.
func (g *github) page(ctx context.Context, url, accept string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("User-Agent", "oku")

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	if g.token != "" && strings.HasPrefix(url, g.api) {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	// An Enterprise Server may not know this version, so oku sends it none.
	if g.host == "" && strings.HasPrefix(url, g.api) {
		req.Header.Set("X-GitHub-Api-Version", githubVersion)
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, "", fmt.Errorf("GitHub rate limit reached, set %s to raise it", g.env)
	case resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("Retry-After") != "":
		// GitHub's secondary limit, for too many requests in a short time.
		return nil, "", fmt.Errorf(
			"GitHub asked oku to slow down, try again in %s seconds", resp.Header.Get("Retry-After"),
		)
	case resp.StatusCode == http.StatusGone && g.host == "":
		return nil, "", fmt.Errorf(
			"GitHub no longer serves version %s of its API, which this oku asks for. Update oku", githubVersion,
		)
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("server returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, "", err
	}

	if len(body) > maxBody {
		return nil, "", fmt.Errorf("response is larger than %d bytes", maxBody)
	}

	return body, resp.Header.Get("Link"), nil
}

// GitHubDir lists the names in the folder dir of a github.com repo at commit.
// A repo too large for Files, such as winget-pkgs, is read one folder at a time.
func (h Hosts) GitHubDir(ctx context.Context, repo, commit, dir string) ([]string, error) {
	g := h.github("")

	var entries []struct {
		Name string `json:"name"`
	}

	if err := g.json(ctx, "/repos/"+repo+"/contents/"+dir+"?ref="+commit, &entries); err != nil {
		return nil, err
	}

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}

	return names, nil
}

// GitHubAsset returns the API address of the release asset whose download page
// is rawURL, and the header that authorizes it, for a private repo on
// github.com. GitHub serves such a file only from the API, to a request that
// asks for application/octet-stream. It returns "" when rawURL is no release
// download on github.com or no token is set.
func (h Hosts) GitHubAsset(ctx context.Context, rawURL string) (string, string, error) {
	g := h.github("")
	if g.http == nil {
		g.http = http.DefaultClient
	}

	rest, ok := strings.CutPrefix(rawURL, g.web+"/")
	if !ok || g.token == "" {
		return "", "", nil
	}

	// owner/repo/releases/download/tag/name, where the tag may hold a slash.
	parts := strings.Split(rest, "/")
	if len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", nil
	}

	repo, name := parts[0]+"/"+parts[1], parts[len(parts)-1]
	tag := strings.Join(parts[4:len(parts)-1], "/")

	var found githubRelease
	if err := g.json(ctx, "/repos/"+repo+"/releases/tags/"+tag, &found); err != nil {
		return "", "", fmt.Errorf("read release %s of %s: %w", tag, repo, err)
	}

	for _, asset := range found.Assets {
		if asset.Name == name && strings.HasPrefix(asset.API, g.api) {
			return asset.API, "Bearer " + g.token, nil
		}
	}

	return "", "", fmt.Errorf("release %s of %s has no asset %s", tag, repo, name)
}
