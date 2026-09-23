package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/y3owk1n/oku/internal/pypi"
)

// pypiVersions returns the versions of a Python package, newest first. A
// prerelease and a version whose every file was yanked sort behind every
// release, so the newest is never one, and `add pypi:x@y` still finds it. A
// version has no tag, so its tag is the version.
func (r *Resolver) pypiVersions(ctx context.Context, name string) ([]Release, error) {
	pkg, err := pypi.Read(ctx, r.Hosts.HTTP, r.PyPI, name)
	if err != nil {
		return nil, fmt.Errorf("list versions of the Python package %s: %w", name, err)
	}

	var releases []Release

	for version := range pkg.Versions {
		releases = append(releases, Release{Version: version, Tag: version})
	}

	behind := func(r Release) bool {
		return pkg.Versions[r.Version].Yanked || pypi.IsPrerelease(r.Version)
	}

	slices.SortFunc(releases, func(a, b Release) int {
		switch {
		case behind(a) && !behind(b):
			return 1
		case behind(b) && !behind(a):
			return -1
		default:
			return Compare(b.Version, a.Version)
		}
	})

	return releases, nil
}
