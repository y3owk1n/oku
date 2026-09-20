// Package resolve discovers the versions a manifest can install and picks one.
package resolve

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/y3owk1n/oku/internal/manifest"
)

// Resolver lists versions. The GitHub URL is a field so tests can point it at a
// local server.
type Resolver struct {
	HTTP      *http.Client
	GitHubAPI string
	// The resolver sends Token to the GitHub API when set, which raises the rate
	// limit.
	Token string
}

// Release is one installable version and the upstream tag it came from.
type Release struct {
	Version string
	Tag     string
	// Commit is the commit a moving tag points at. It is empty for every other
	// release.
	Commit string
	// Digests maps a download URL of a moving tag's release to the sha256 that
	// GitHub reports for it.
	Digests map[string]string
}

// Pick returns the release of v to install. want selects an exact version, and
// an empty want selects the newest. A manifest with a fixed version has one
// release, whose tag equals its version.
func (r *Resolver) Pick(ctx context.Context, v manifest.Version, want string) (Release, error) {
	if v.From == "" {
		if want != "" && want != v.Value {
			return Release{}, fmt.Errorf("the manifest provides version %s, not %s", v.Value, want)
		}

		return Release{Version: v.Value, Tag: v.Value}, nil
	}

	releases, err := r.List(ctx, v)
	if err != nil {
		return Release{}, err
	}

	if len(releases) == 0 {
		return Release{}, fmt.Errorf("%s %s has no versions", v.From, v.Repo)
	}

	if want == "" {
		return releases[0], nil
	}

	for _, release := range releases {
		if release.Version == want {
			return release, nil
		}
	}

	newest := releases[:min(5, len(releases))]
	names := make([]string, len(newest))

	for i, release := range newest {
		names[i] = release.Version
	}

	return Release{}, fmt.Errorf(
		"%s %s has no version %s, the newest are %s",
		v.From, v.Repo, want, strings.Join(names, ", "),
	)
}

// PickWithin returns the newest release of v that satisfies constraint, such as
// ">=3" or ">=1.2, <2". An empty constraint accepts every version.
func (r *Resolver) PickWithin(
	ctx context.Context,
	v manifest.Version,
	constraint string,
) (Release, error) {
	releases := []Release{{Version: v.Value, Tag: v.Value}}

	if v.From != "" {
		var err error
		if releases, err = r.List(ctx, v); err != nil {
			return Release{}, err
		}
	}

	var seen []string

	for _, release := range releases {
		ok, err := Satisfies(release.Version, constraint)
		if err != nil {
			return Release{}, err
		}

		if ok {
			return release, nil
		}

		if len(seen) < 5 {
			seen = append(seen, release.Version)
		}
	}

	return Release{}, fmt.Errorf(
		"no version satisfies %q, the versions found are %s", constraint, strings.Join(seen, ", "),
	)
}

// Satisfies reports whether version meets every comma-separated part of
// constraint. A part is an operator (>=, >, <=, <, =) and a version, and a bare
// version means "=".
func Satisfies(version, constraint string) (bool, error) {
	for _, part := range strings.Split(constraint, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		op := "="

		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if rest, ok := strings.CutPrefix(part, candidate); ok {
				op, part = candidate, strings.TrimSpace(rest)

				break
			}
		}

		if part == "" || !unicode.IsDigit(rune(part[0])) {
			return false, fmt.Errorf(
				"version constraint %q: want an operator and a version, such as >=1.2",
				constraint,
			)
		}

		order := Compare(version, part)

		met := map[string]bool{
			">=": order >= 0, ">": order > 0, "<=": order <= 0, "<": order < 0, "=": order == 0,
		}[op]
		if !met {
			return false, nil
		}
	}

	return true, nil
}

// List returns the releases of v, newest first.
func (r *Resolver) List(ctx context.Context, v manifest.Version) ([]Release, error) {
	if v.Tag != "" {
		release, err := r.movingTag(ctx, v.Repo, v.Tag)
		if err != nil {
			return nil, err
		}

		return []Release{release}, nil
	}

	var (
		tags []string
		err  error
	)

	switch v.From {
	case manifest.FromGitHubReleases:
		tags, err = r.githubReleases(ctx, v.Repo)
	case manifest.FromGitTags:
		tags, err = gitTags(ctx, v.Repo)
	default:
		return nil, fmt.Errorf("version.from %q is not supported", v.From)
	}

	if err != nil {
		return nil, err
	}

	var releases []Release

	for _, tag := range tags {
		version, ok := strings.CutPrefix(tag, v.StripPrefix)
		if !ok || version == "" || !unicode.IsDigit(rune(version[0])) {
			continue
		}

		releases = append(releases, Release{Version: version, Tag: tag})
	}

	slices.SortFunc(releases, func(a, b Release) int { return Compare(b.Version, a.Version) })

	return releases, nil
}

// movingTag returns the one release of a tag that upstream moves, such as
// "nightly". Its version is the day of the commit the tag points at and that
// commit, such as 2026.09.20-a73243f, so one commit always has one version. A
// prerelease counts, a draft does not.
func (r *Resolver) movingTag(ctx context.Context, repo, tag string) (Release, error) {
	what := "read the release " + tag + " of " + repo

	var found struct {
		Draft  bool `json:"draft"`
		Assets []struct {
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}

	err := r.github(
		ctx,
		"/repos/"+repo+"/releases/tags/"+tag,
		what,
		"the repository has no such release",
		&found,
	)
	if err != nil {
		return Release{}, err
	}

	if found.Draft {
		return Release{}, fmt.Errorf("%s: the release is a draft", what)
	}

	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}

	err = r.github(
		ctx, "/repos/"+repo+"/commits/"+tag, what, "the repository has no such tag", &commit,
	)
	if err != nil {
		return Release{}, err
	}

	if len(commit.SHA) < 7 {
		return Release{}, fmt.Errorf("%s: GitHub named no commit for the tag", what)
	}

	digests := map[string]string{}

	for _, asset := range found.Assets {
		if sum, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok {
			digests[asset.URL] = sum
		}
	}

	return Release{
		Version: commit.Commit.Committer.Date.UTC().Format("2006.01.02") + "-" + commit.SHA[:7],
		Tag:     tag,
		Commit:  commit.SHA,
		Digests: digests,
	}, nil
}

// githubReleases returns the tags of published releases. It reads the newest
// 100 and skips drafts and prereleases.
func (r *Resolver) githubReleases(ctx context.Context, repo string) ([]string, error) {
	var found []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}

	err := r.github(
		ctx, "/repos/"+repo+"/releases?per_page=100", "list releases of "+repo,
		"the repository was not found", &found,
	)
	if err != nil {
		return nil, err
	}

	var tags []string

	for _, release := range found {
		if !release.Draft && !release.Prerelease {
			tags = append(tags, release.Tag)
		}
	}

	return tags, nil
}

// github decodes the GitHub API's answer for path into into. what names the
// request in errors, and missing says what a 404 means for it.
func (r *Resolver) github(ctx context.Context, path, what, missing string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.GitHubAPI+path, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "oku")
	req.Header.Set("Accept", "application/vnd.github+json")

	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s: %s", what, missing)
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return errors.New("GitHub rate limit reached, set GITHUB_TOKEN to raise it")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: server returned %s", what, resp.Status)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	return nil
}

func gitTags(ctx context.Context, url string) ([]string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New(`version.from = "git-tags" needs git on PATH`)
	}

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--refs", "--", url)
	// A credential prompt would hang a non-interactive install.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tags of %s: %w", url, err)
	}

	var tags []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if _, name, ok := strings.Cut(line, "refs/tags/"); ok {
			tags = append(tags, name)
		}
	}

	return tags, nil
}

// Compare orders two versions by their dot-separated numbers, so 1.10.0 is newer
// than 1.9.0. A version with a "-" suffix, such as 2.0.0-rc1, is older than the
// same version without one. It returns -1, 0 or 1.
func Compare(a, b string) int {
	coreA, preA, _ := strings.Cut(a, "-")
	coreB, preB, _ := strings.Cut(b, "-")

	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")

	for i := range max(len(partsA), len(partsB)) {
		var x, y string
		if i < len(partsA) {
			x = partsA[i]
		}

		if i < len(partsB) {
			y = partsB[i]
		}

		nx, errX := strconv.Atoi(x)
		ny, errY := strconv.Atoi(y)

		switch {
		case errX == nil && errY == nil && nx != ny:
			return cmp.Compare(nx, ny)
		case (errX != nil || errY != nil) && x != y:
			return strings.Compare(x, y)
		}
	}

	switch {
	case preA == "" && preB != "":
		return 1
	case preA != "" && preB == "":
		return -1
	default:
		return strings.Compare(preA, preB)
	}
}
