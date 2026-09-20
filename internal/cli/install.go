package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
)

var errManifestChanged = errors.New("the manifest changed since oku.lock was written")

// installed is one package after oku fetched its manifest and realized it.
type installed struct {
	profile  profile.Package
	lock     lock.Package
	firstUse bool
}

// request says what install should fetch and what it must match.
type request struct {
	ref ref.Ref
	// commit pins where the manifest is read from. Empty means the newest.
	commit string
	// previous is the package's current lock entry, or the zero value.
	previous lock.Package
	// wantManifest is the manifest hash to insist on. A different one returns
	// errManifestChanged before anything is downloaded. Empty accepts any.
	wantManifest string
	// acceptDigest lets a digest stated by the manifest replace the one pinned in
	// previous. "oku update" sets it.
	acceptDigest bool
}

// install fetches the manifest, realizes the host's artifact and returns the
// profile and lock entries for it. It changes the store only.
func (e env) install(ctx context.Context, opts Options, req request) (installed, error) {
	r, commit, previous, wantManifest := req.ref, req.commit, req.previous, req.wantManifest

	fetched, err := e.fetcher(opts).Fetch(ctx, r, commit)
	if err != nil {
		return installed{}, err
	}

	m, err := manifest.Parse(fetched.Data, r.String())
	if err != nil {
		return installed{}, err
	}

	if wantManifest != "" && wantManifest != m.SHA256 {
		return installed{}, errManifestChanged
	}

	if r.Version != "" && r.Version != m.Version.Value {
		return installed{}, fmt.Errorf(
			"%s provides version %s, not %s", r, m.Version.Value, r.Version,
		)
	}

	host := platform.Host()

	artifact, ok, err := m.Select(host)
	if err != nil {
		return installed{}, err
	}

	if !ok && m.HasBuild() {
		return installed{}, fmt.Errorf(
			"%s has no artifact for %s, and building from source is not supported so far",
			m.Package.Name, host,
		)
	}

	if !ok {
		return installed{}, fmt.Errorf("%s has no artifact for %s", m.Package.Name, host)
	}

	// A digest that oku.lock pinned for this version and URL still applies, even
	// when the manifest gives none.
	pinned := ""
	if at := previous.Platforms[host.String()]; previous.Version == m.Version.Value &&
		at.URL == artifact.URL {
		pinned = at.SHA256
	}

	if req.acceptDigest && (artifact.SHA256 != "" || artifact.SHA256URL != "") {
		pinned = ""
	}

	realized, err := e.store().Realize(ctx, m, artifact, host, pinned)
	if err != nil {
		return installed{}, err
	}

	// Entries for other platforms stay while they describe the same manifest.
	platforms := map[string]lock.Platform{}

	if previous.ManifestSHA256 == m.SHA256 && previous.Ref == r.String() {
		for name, at := range previous.Platforms {
			platforms[name] = at
		}
	}

	platforms[host.String()] = lock.Platform{
		Strategy: "artifact",
		URL:      artifact.URL,
		SHA256:   realized.SHA256,
	}

	return installed{
		profile: profile.Package{
			Name:      m.Package.Name,
			Version:   m.Version.Value,
			Ref:       r.String(),
			StorePath: realized.Path,
		},
		lock: lock.Package{
			Name:           m.Package.Name,
			Ref:            r.String(),
			Commit:         fetched.Commit,
			ManifestSHA256: m.SHA256,
			Version:        m.Version.Value,
			Platforms:      platforms,
		},
		firstUse: realized.FirstUse,
	}, nil
}

// reportFirstUse tells the user that oku trusted a download unverified.
func (e env) reportFirstUse(w io.Writer, got installed) {
	if !got.firstUse {
		return
	}

	fmt.Fprintf(
		w,
		"%s publishes no checksum, so oku trusted this download and pinned sha256 %s in %s\n",
		got.lock.Name, got.lock.Platforms[platform.Host().String()].SHA256, e.lockPath(),
	)
}
