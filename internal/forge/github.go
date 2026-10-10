package forge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/snappy"

	"github.com/y3owk1n/oku/internal/limit"
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
	Tag        string    `json:"tag_name"`
	Commit     string    `json:"target_commitish"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
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
	out := Release{
		Tag: r.Tag, Commit: r.Commit, Draft: r.Draft, Prerelease: r.Prerelease,
		Published: r.Published,
	}

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
	ctx = fixed(ctx, commit)

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

// firstReleases is the size of the page a lookup reads first. GitHub builds the
// whole page before it answers, also for a 304, so a smaller page answers
// sooner. The version to install is almost always among the newest 30, see D104.
const firstReleases = 30

// Releases reads the newest maxReleases releases, or with all false the newest
// firstReleases.
func (g *github) Releases(ctx context.Context, repo string, all bool) ([]Release, bool, error) {
	size := releasePage
	if !all {
		size = firstReleases
	}

	return readReleases(ctx, size, all, func(ctx context.Context, page int) ([]Release, int, error) {
		at := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", g.api, repo, size)
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

// Tags reads the newest maxTags tags. GitHub lists them newest first.
func (g *github) Tags(ctx context.Context, repo string) ([]string, error) {
	return readTags(ctx, releasePage, func(ctx context.Context, page int) ([]string, int, error) {
		at := fmt.Sprintf("%s/repos/%s/tags?per_page=%d", g.api, repo, releasePage)
		if page > 1 {
			at += fmt.Sprintf("&page=%d", page)
		}

		body, link, err := g.page(ctx, at, "application/vnd.github+json")
		if err != nil {
			return nil, 0, err
		}

		var found []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &found); err != nil {
			return nil, 0, err
		}

		tags := make([]string, len(found))
		for i, tag := range found {
			tags[i] = tag.Name
		}

		return tags, lastPage(link), nil
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

	body, err := limit.Read(resp.Body, maxBody)
	if err != nil {
		return nil, "", err
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
		g.http = h.Net.Client(CheckRedirect)
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

type freshKey struct{}

// Fresh marks ctx so that GitHubAttestations asks GitHub rather than give the
// attestations it kept, which may predate the one a caller needs.
func Fresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshKey{}, true)
}

// IsFresh reports whether Fresh marked ctx.
func IsFresh(ctx context.Context) bool {
	return ctx.Value(freshKey{}) != nil
}

// GitHubAttestations returns the Sigstore bundles of the artifact attestations
// that repo, on github.com, holds for the file whose sha256 is digest. GitHub
// may keep a bundle apart at its bundle_url, compressed with snappy.
func (h Hosts) GitHubAttestations(ctx context.Context, repo, digest string) ([][]byte, error) {
	g := h.github("")
	if g.http == nil {
		g.http = h.Net.Client(CheckRedirect)
	}

	// The caller verifies each bundle it uses, so oku keeps the attestations it
	// found for a digest. It asks again for a file that had none, and under
	// Fresh.
	var path string

	if h.Answers != "" {
		sum := sha256.Sum256([]byte("attestations\n" + g.api + "/" + repo + "\n" + digest))
		path = filepath.Join(h.Answers, hex.EncodeToString(sum[:]))

		var bundles [][]byte
		if old, ok := read(path); ok && !IsFresh(ctx) && json.Unmarshal(old.Body, &bundles) == nil {
			used(path)

			return bundles, nil
		}
	}

	var found struct {
		Attestations []struct {
			Bundle    json.RawMessage `json:"bundle"`
			BundleURL string          `json:"bundle_url"`
		} `json:"attestations"`
	}

	err := g.json(ctx, "/repos/"+repo+"/attestations/sha256:"+digest+"?per_page=30", &found)
	if errors.Is(err, ErrNotFound) || err == nil && len(found.Attestations) == 0 {
		return nil, errors.New("it has none for this file")
	}

	if err != nil {
		return nil, err
	}

	bundles := make([][]byte, 0, len(found.Attestations))

	for _, a := range found.Attestations {
		if a.BundleURL == "" {
			bundles = append(bundles, a.Bundle)

			continue
		}

		// The address is a storage service's, which takes no GitHub token.
		packed, err := g.get(ctx, a.BundleURL, "")
		if err != nil {
			return nil, fmt.Errorf("read the attestation at %s: %w", a.BundleURL, err)
		}

		data, err := snappy.Decode(nil, packed)
		if err != nil {
			return nil, fmt.Errorf("read the attestation at %s: %w", a.BundleURL, err)
		}

		bundles = append(bundles, data)
	}

	if body, err := json.Marshal(bundles); path != "" && err == nil {
		keep(path, kept{Type: "application/json", Bytes: len(body), Body: body})
	}

	return bundles, nil
}
