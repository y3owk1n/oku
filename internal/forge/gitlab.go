package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// gitlab is gitlab.com or a GitLab server of one's own.
type gitlab struct {
	http *http.Client
	// host is "" for gitlab.com.
	host  string
	token string
}

type gitlabRelease struct {
	Tag string `json:"tag_name"`
	// Upcoming is true for a release dated in the future, which GitLab does not
	// count as published.
	Upcoming bool `json:"upcoming_release"`
	Commit   struct {
		ID string `json:"id"`
	} `json:"commit"`
	Assets struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
			// Direct is the download URL with the tag in plain text. URL may
			// percent-encode the dots of a version.
			Direct string `json:"direct_asset_url"`
		} `json:"links"`
	} `json:"assets"`
}

func (r gitlabRelease) release() Release {
	out := Release{Tag: r.Tag, Commit: r.Commit.ID, Draft: r.Upcoming}

	for _, link := range r.Assets.Links {
		at := link.Direct
		if at == "" {
			at = link.URL
		}

		out.Assets = append(out.Assets, Asset{Name: link.Name, URL: at})
	}

	return out
}

func (g *gitlab) Kind() string { return KindGitLab }

func (g *gitlab) Host() string { return g.host }

func (g *gitlab) Auth() Auth {
	if g.token == "" {
		return Auth{}
	}

	return Auth{Host: strings.TrimPrefix(g.web(), "https://"), Header: "Bearer " + g.token}
}

func (g *gitlab) web() string {
	if g.host == "" {
		return "https://gitlab.com"
	}

	return "https://" + g.host
}

// project returns the API URL of repo. GitLab takes a project's path with its
// slashes escaped.
func (g *gitlab) project(repo string) string {
	return g.web() + "/api/v4/projects/" + url.PathEscape(repo)
}

func (g *gitlab) Home(repo string) string {
	return g.web() + "/" + repo
}

func (g *gitlab) Head(ctx context.Context, repo string) (string, error) {
	var found []struct {
		ID string `json:"id"`
	}

	if _, err := g.json(ctx, g.project(repo)+"/repository/commits?per_page=1", &found); err != nil {
		return "", err
	}

	if len(found) == 0 {
		return "", fmt.Errorf("%s has no commits", repo)
	}

	return found[0].ID, nil
}

func (g *gitlab) File(ctx context.Context, repo, commit, path string) ([]byte, error) {
	body, _, err := g.get(
		ctx,
		g.project(
			repo,
		)+"/repository/files/"+url.PathEscape(
			path,
		)+"/raw?ref="+url.QueryEscape(
			commit,
		),
	)

	return body, err
}

// Files follows the "next" links of the tree, because GitLab gives at most 100
// entries in one answer.
func (g *gitlab) Files(ctx context.Context, repo, commit string) ([]string, error) {
	var paths []string

	next := g.project(
		repo,
	) + "/repository/tree?recursive=true&per_page=100&pagination=keyset&ref=" +
		url.QueryEscape(
			commit,
		)

	for next != "" {
		var tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}

		link, err := g.json(ctx, next, &tree)
		if err != nil {
			return nil, err
		}

		next = nextPage(link, g.web()+"/")

		for _, item := range tree {
			if item.Type == "blob" {
				paths = append(paths, item.Path)
			}
		}
	}

	return paths, nil
}

func (g *gitlab) Release(ctx context.Context, repo, tag string) (Release, error) {
	which := "permalink/latest"
	if tag != "" {
		which = url.PathEscape(tag)
	}

	var found gitlabRelease

	_, err := g.json(ctx, g.project(repo)+"/releases/"+which, &found)

	return found.release(), err
}

// gitlabPage is the releases oku asks for in one answer, the most GitLab gives.
const gitlabPage = 100

// Releases reads the newest maxReleases releases.
func (g *gitlab) Releases(ctx context.Context, repo string) ([]Release, error) {
	return readReleases(ctx, gitlabPage, func(ctx context.Context, page int) ([]Release, int, error) {
		at := fmt.Sprintf("%s/releases?per_page=%d", g.project(repo), gitlabPage)
		if page > 1 {
			at += fmt.Sprintf("&page=%d", page)
		}

		var found []gitlabRelease

		link, err := g.json(ctx, at, &found)
		if err != nil {
			return nil, 0, err
		}

		releases := make([]Release, len(found))
		for i, release := range found {
			releases[i] = release.release()
		}

		return releases, lastPage(link), nil
	})
}

func (g *gitlab) TagCommit(ctx context.Context, repo, tag string) (Commit, error) {
	var found struct {
		ID   string    `json:"id"`
		Date time.Time `json:"committed_date"`
	}

	_, err := g.json(ctx, g.project(repo)+"/repository/commits/"+url.PathEscape(tag), &found)

	return Commit{SHA: found.ID, Date: found.Date}, err
}

func (g *gitlab) Archive(ctx context.Context, repo, commit string) ([]byte, error) {
	body, _, err := g.get(ctx, g.project(repo)+"/repository/archive.tar.gz?sha="+url.QueryEscape(commit))

	return body, err
}

// json decodes the answer for at into into and returns the Link header, which
// names the other pages of a list.
func (g *gitlab) json(ctx context.Context, at string, into any) (string, error) {
	body, link, err := g.get(ctx, at)
	if err != nil {
		return "", err
	}

	return link, json.Unmarshal(body, into)
}

func (g *gitlab) get(ctx context.Context, at string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, at, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("User-Agent", "oku")

	// Go drops the Authorization header when a redirect leaves the host. It would
	// keep GitLab's own PRIVATE-TOKEN header, so oku does not use that one.
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("%s returned %s", g.web(), resp.Status)
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
