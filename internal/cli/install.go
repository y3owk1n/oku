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
	"github.com/y3owk1n/oku/internal/resolve"
)

var errManifestChanged = errors.New("the manifest changed since oku.lock was written")

// installed is one package after oku fetched its manifest and realized it.
type installed struct {
	profile  profile.Package
	lock     lock.Package
	firstUse bool
	// inferred is the manifest text when oku inferred it during this install.
	inferred string
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
	// keepVersion installs the version in previous without listing versions
	// again. "oku sync" sets it.
	keepVersion bool
}

// install fetches the manifest, realizes the host's artifact and returns the
// profile and lock entries for it. It changes the store only.
func (e env) install(ctx context.Context, opts Options, req request) (installed, error) {
	r, previous, wantManifest := req.ref, req.previous, req.wantManifest

	fetched, inferred, err := e.manifestData(ctx, opts, req)
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

	// A pin in the list decides the version. Without one, sync stays on the locked
	// version, and add and update take the newest.
	release := resolve.Release{Version: previous.Version, Tag: previous.Tag}

	keep := req.keepVersion && previous.Version != "" &&
		(r.Version == "" || r.Version == previous.Version)
	if !keep {
		release, err = e.resolver(opts).Pick(ctx, m.Version, r.Version)
		if err != nil {
			return installed{}, fmt.Errorf("%s: %w", r, err)
		}
	}

	if release.Tag == "" {
		release.Tag = release.Version
	}

	m.Version.Value, m.Tag = release.Version, release.Tag

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

	if previous.ManifestSHA256 == m.SHA256 && previous.Ref == r.String() &&
		previous.Version == m.Version.Value {
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
			Tag:            tagFor(m),
			Inferred:       inferred != "" || req.previous.Inferred && req.keepVersion,
			Manifest:       inferredText(inferred, req),
			Platforms:      platforms,
		},
		firstUse: realized.FirstUse,
		inferred: inferred,
	}, nil
}

// manifestData returns the manifest for req. A GitHub repo without a manifest
// gets an inferred one, which is returned a second time as text. Sync reuses the
// inferred text in the lock, so it installs from what the user saw.
func (e env) manifestData(
	ctx context.Context,
	opts Options,
	req request,
) (ref.Fetched, string, error) {
	if req.keepVersion && req.previous.Inferred {
		return ref.Fetched{
			Data:   []byte(req.previous.Manifest),
			Commit: req.previous.Commit,
		}, "", nil
	}

	fetched, err := e.fetcher(opts).Fetch(ctx, req.ref, req.commit, ref.Manifest)
	if err == nil || !errors.Is(err, ref.ErrNotFound) ||
		req.ref.Kind != ref.GitHub || req.ref.Fragment != "" {
		return fetched, "", err
	}

	text, err := e.inferrer(opts).Manifest(ctx, req.ref.Location, platform.Host())
	if err != nil {
		return ref.Fetched{}, "", err
	}

	return ref.Fetched{Data: []byte(text), Commit: fetched.Commit}, text, nil
}

func inferredText(inferred string, req request) string {
	if inferred == "" && req.previous.Inferred && req.keepVersion {
		return req.previous.Manifest
	}

	return inferred
}

// reportInferred prints a manifest that oku just inferred.
func reportInferred(w io.Writer, got installed) {
	if got.inferred == "" {
		return
	}

	fmt.Fprintf(
		w, "%s has no manifest, so oku inferred this one from its newest release:\n\n%s\n",
		got.lock.Ref, got.inferred,
	)
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

// tagFor returns the tag to lock. A tag equal to the version is left out.
func tagFor(m *manifest.Manifest) string {
	if m.Tag == m.Version.Value {
		return ""
	}

	return m.Tag
}
