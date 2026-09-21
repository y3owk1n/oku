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

// maxBody is the most bytes a forge reads from one answer.
const maxBody = 8 << 20

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
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func (r githubRelease) release() Release {
	out := Release{Tag: r.Tag, Commit: r.Commit, Draft: r.Draft, Prerelease: r.Prerelease}

	for _, asset := range r.Assets {
		digest, _ := strings.CutPrefix(asset.Digest, "sha256:")
		out.Assets = append(out.Assets, Asset{Name: asset.Name, URL: asset.URL, Digest: digest})
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

// releasePage and releasePages give the newest 100 releases. One page of 100
// can be larger than maxBody, because every release lists all of its assets.
const (
	releasePage  = 25
	releasePages = 4
)

// Releases reads the newest 100 releases.
func (g *github) Releases(ctx context.Context, repo string) ([]Release, error) {
	var releases []Release

	for page := 1; page <= releasePages; page++ {
		var found []githubRelease

		err := g.json(
			ctx,
			fmt.Sprintf("/repos/%s/releases?per_page=%d&page=%d", repo, releasePage, page),
			&found,
		)
		if err != nil {
			return nil, err
		}

		for _, release := range found {
			releases = append(releases, release.release())
		}

		if len(found) < releasePage {
			break
		}
	}

	return releases, nil
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

func (g *github) json(ctx context.Context, path string, into any) error {
	body, err := g.get(ctx, g.api+path, "application/vnd.github+json")
	if err != nil {
		return err
	}

	return json.Unmarshal(body, into)
}

func (g *github) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	if g.token != "" && strings.HasPrefix(url, g.api) {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, fmt.Errorf("GitHub rate limit reached, set %s to raise it", g.env)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}

	if len(body) > maxBody {
		return nil, fmt.Errorf("response is larger than %d bytes", maxBody)
	}

	return body, nil
}
