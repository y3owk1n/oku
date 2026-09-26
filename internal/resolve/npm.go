package resolve

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/oku/internal/npm"
)

// npmVersions returns the published versions of an npm package, newest first.
// It skips a prerelease, which has a "-" in its version. An npm version has no
// tag, so its tag is the version.
func (r *Resolver) npmVersions(ctx context.Context, name string) ([]Release, error) {
	pkg, err := npm.Read(ctx, r.Hosts.HTTP, r.NPM, name)
	if err != nil {
		return nil, fmt.Errorf("list versions of the npm package %s: %w", name, err)
	}

	// Every version is at least as old as the package's last change, so the full
	// answer, which can be tens of MB, is read only when that change is recent.
	times := map[string]time.Time{}

	if r.MinAge > 0 && !pkg.Modified.Before(r.cutoff()) {
		if times, err = npm.Times(ctx, r.Hosts.HTTP, r.NPM, name); err != nil {
			return nil, fmt.Errorf(
				"read when the versions of the npm package %s came out: %w", name, err,
			)
		}
	}

	var releases []Release

	for version, published := range pkg.Versions {
		if strings.Contains(version, "-") {
			continue
		}

		release := Release{
			Version: version, Tag: version, Published: cmp.Or(times[version], pkg.Modified),
		}
		if published.Integrity != "" {
			release.Integrity = map[string]string{published.Tarball: published.Integrity}
		}

		releases = append(releases, release)
	}

	slices.SortFunc(releases, func(a, b Release) int { return Compare(b.Version, a.Version) })

	return releases, nil
}
