package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/goproxy"
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
