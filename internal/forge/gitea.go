package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// gitea is a Gitea or Forgejo server, such as codeberg.org. Both serve the same
// API.
type gitea struct {
	http  *http.Client
	host  string
	token string
}

type giteaRelease struct {
	Tag        string `json:"tag_name"`
	Commit     string `json:"target_commitish"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r giteaRelease) release() Release {
	out := Release{Tag: r.Tag, Commit: r.Commit, Draft: r.Draft, Prerelease: r.Prerelease}

	for _, asset := range r.Assets {
		out.Assets = append(out.Assets, Asset{Name: asset.Name, URL: asset.URL})
	}

	return out
}

func (g *gitea) Kind() string { return KindGitea }

func (g *gitea) Host() string { return g.host }

func (g *gitea) Auth() Auth {
	if g.token == "" {
		return Auth{}
	}

	return Auth{Host: g.host, Header: "token " + g.token}
}

func (g *gitea) Home(repo string) string {
	return "https://" + g.host + "/" + repo
}

func (g *gitea) Head(ctx context.Context, repo string) (string, error) {
	var found []struct {
		SHA string `json:"sha"`
	}

	// stat, verification and files are off, because each one slows this request
	// on a large repo.
	err := g.json(ctx, repo, "/commits?limit=1&stat=false&verification=false&files=false", &found)
	if err != nil {
		return "", err
	}

	if len(found) == 0 {
		return "", fmt.Errorf("%s has no commits", repo)
	}

	return found[0].SHA, nil
}

func (g *gitea) File(ctx context.Context, repo, commit, path string) ([]byte, error) {
	return g.get(ctx, repo, "/raw/"+path+"?ref="+url.QueryEscape(commit))
}

// Files reads the tree page by page, because the server caps a page.
func (g *gitea) Files(ctx context.Context, repo, commit string) ([]string, error) {
	var paths []string

	seen := 0

	for page := 1; ; page++ {
		var tree struct {
			Total int `json:"total_count"`
			Tree  []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
		}

		err := g.json(
			ctx, repo, "/git/trees/"+commit+"?recursive=true&page="+strconv.Itoa(page), &tree,
		)
		if err != nil {
			return nil, err
		}

		for _, item := range tree.Tree {
			if item.Type == "blob" {
				paths = append(paths, item.Path)
			}
		}

		seen += len(tree.Tree)
		if len(tree.Tree) == 0 || seen >= tree.Total {
			return paths, nil
		}
	}
}

func (g *gitea) Release(ctx context.Context, repo, tag string) (Release, error) {
	which := "latest"
	if tag != "" {
		which = "tags/" + url.PathEscape(tag)
	}

	var found giteaRelease

	err := g.json(ctx, repo, "/releases/"+which, &found)

	return found.release(), err
}

// Releases reads the newest 50 releases, which is the most a server gives in
// one page by default.
func (g *gitea) Releases(ctx context.Context, repo string) ([]Release, error) {
	var found []giteaRelease

	if err := g.json(ctx, repo, "/releases?limit=50", &found); err != nil {
		return nil, err
	}

	releases := make([]Release, len(found))
	for i, release := range found {
		releases[i] = release.release()
	}

	return releases, nil
}

func (g *gitea) TagCommit(ctx context.Context, repo, tag string) (Commit, error) {
	var found struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}

	err := g.json(ctx, repo, "/git/commits/"+url.PathEscape(tag), &found)

	return Commit{SHA: found.SHA, Date: found.Commit.Committer.Date}, err
}

func (g *gitea) json(ctx context.Context, repo, path string, into any) error {
	body, err := g.get(ctx, repo, path)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, into)
}

func (g *gitea) get(ctx context.Context, repo, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://"+g.host+"/api/v1/repos/"+repo+path, nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	if g.token != "" {
		req.Header.Set("Authorization", "token "+g.token)
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s returned %s", g.host, resp.Status)
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
