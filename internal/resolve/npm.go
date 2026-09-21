package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// npmRegistry is where npm packages are published.
const npmRegistry = "https://registry.npmjs.org"

// maxNPMBody is the most bytes read from the registry. A package with
// thousands of versions has a list of several MiB.
const maxNPMBody = 64 << 20

// npmVersions returns the published versions of an npm package, newest first.
// It skips a prerelease, which has a "-" in its version. An npm version has no
// tag, so its tag is the version.
func (r *Resolver) npmVersions(ctx context.Context, name string) ([]Release, error) {
	registry := npmRegistry
	if r.NPM != "" {
		registry = r.NPM
	}

	what := "list versions of the npm package " + name

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/"+name, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")
	// This form leaves out each version's readme and scripts.
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")

	resp, err := r.Hosts.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%s: the registry has no such package", what)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: the registry returned %s", what, resp.Status)
	}

	var found struct {
		Versions map[string]struct {
			Dist struct {
				Tarball   string `json:"tarball"`
				Integrity string `json:"integrity"`
			} `json:"dist"`
		} `json:"versions"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxNPMBody)).Decode(&found); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}

	var releases []Release

	for version, published := range found.Versions {
		if strings.Contains(version, "-") {
			continue
		}

		release := Release{Version: version, Tag: version}
		if published.Dist.Integrity != "" {
			release.Integrity = map[string]string{published.Dist.Tarball: published.Dist.Integrity}
		}

		releases = append(releases, release)
	}

	slices.SortFunc(releases, func(a, b Release) int { return Compare(b.Version, a.Version) })

	return releases, nil
}
