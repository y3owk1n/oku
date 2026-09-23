package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/crates"
)

// crateVersions returns the versions of a crate, newest first. A yanked version
// and a prerelease, which has a "-", sort behind every release, so the newest
// is never one, and `add cargo:x@y` still finds it. Each carries the sha256 that
// crates.io publishes for its .crate file, keyed by its download URL.
func (r *Resolver) crateVersions(ctx context.Context, name string) ([]Release, error) {
	c, err := crates.Read(ctx, r.Hosts.HTTP, r.Crates, name)
	if err != nil {
		return nil, fmt.Errorf("list versions of the crate %s: %w", name, err)
	}

	yanked := map[string]bool{}

	releases := make([]Release, 0, len(c.Versions))
	for _, v := range c.Versions {
		yanked[v.Number] = v.Yanked
		releases = append(releases, Release{
			Version: v.Number, Tag: v.Number,
			Digests: map[string]string{crates.URL(r.CrateDownloads, name, v.Number): v.SHA256},
		})
	}

	behind := func(r Release) bool { return yanked[r.Version] || strings.Contains(r.Version, "-") }

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
