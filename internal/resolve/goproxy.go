package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/goproxy"
	"github.com/y3owk1n/oku/internal/manifest"
)

// goVersions returns the versions of a Go module, newest first. A version with
// a "-", which is a prerelease or the pseudo-version of a commit, sorts behind
// every release, so the newest is never one unless the module has no tags, and
// `add go:x@y` still finds it. A version has no tag, so its tag is the version.
func (r *Resolver) goVersions(ctx context.Context, module string) ([]Release, error) {
	versions, err := goproxy.Versions(ctx, r.Hosts.HTTP, r.GoProxy, module)
	if err != nil {
		return nil, fmt.Errorf("list versions of the Go module %s: %w", module, err)
	}

	releases := make([]Release, 0, len(versions))
	for _, version := range versions {
		releases = append(releases, Release{Version: version, Tag: version})
	}

	slices.SortFunc(releases, func(a, b Release) int {
		preA, preB := strings.Contains(a.Version, "-"), strings.Contains(b.Version, "-")

		switch {
		case preA && !preB:
			return 1
		case preB && !preA:
			return -1
		default:
			return Compare(b.Version, a.Version)
		}
	})

	return releases, nil
}

// goTimes returns a copy of releases with the publish times that MinAge needs
// to pick one for want. The proxy gives one time per request. oku asks from the
// newest version down and stops at the first that fits want and is old enough,
// which is the one Pick takes.
func (r *Resolver) goTimes(ctx context.Context, v manifest.Version, releases []Release, want string) ([]Release, error) {
	// A version named exactly skips the age.
	exact := slices.ContainsFunc(releases, func(release Release) bool { return release.Version == want })
	if r.MinAge == 0 || exact {
		return releases, nil
	}

	releases = slices.Clone(releases)

	for i := range releases {
		if ok, _ := fits(releases[i], want); !ok {
			continue
		}

		at, err := goproxy.Published(ctx, r.Hosts.HTTP, r.GoProxy, v.Repo, releases[i].Version)

		// A proxy that keeps no times leaves them unknown.
		if errors.Is(err, goproxy.ErrNotFound) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read when %s %s came out: %w", v.Repo, releases[i].Version, err)
		}

		releases[i].Published = at
		if at.Before(r.cutoff()) {
			break
		}
	}

	return releases, nil
}
