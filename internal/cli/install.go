package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/store"
)

const (
	strategyArtifact = "artifact"
	strategyBuild    = "build"
)

var errManifestChanged = errors.New("the manifest changed since oku.lock was written")

// installed is one package after oku fetched its manifest and realized it.
type installed struct {
	profile profile.Package
	lock    lock.Package
	// closure holds the store paths of every dep, direct and indirect.
	closure  []string
	firstUse bool
	// inferred is the manifest text when oku inferred it during this install.
	inferred string
	// unsandboxed says why the build ran without the sandbox, or is empty.
	unsandboxed string
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
	// fromSource builds even when a prebuilt artifact fits the host.
	fromSource bool
	// approve decides whether a manifest may run its build commands.
	approve func(m *manifest.Manifest, host platform.Platform) error
	// log receives the output of build commands, or is nil.
	log io.Writer
	// constraint limits the version of a dep, such as ">=3". A version pin in ref
	// overrides it.
	constraint string
	// progress reports each build step of this package, and may be nil. Deps do
	// not inherit it.
	progress func(step, total int, kind string, err error)
	// stack holds the refs being installed above this one. A ref that is already
	// in it is a dependency cycle.
	stack []string
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
	switch {
	case keep:
	case r.Version == "" && req.constraint != "":
		release, err = e.resolver(opts).PickWithin(ctx, m.Version, req.constraint)
	default:
		release, err = e.resolver(opts).Pick(ctx, m.Version, r.Version)
	}

	if err != nil {
		return installed{}, fmt.Errorf("%s: %w", r, err)
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

	// A platform whose lock entry says "build" is built from source again.
	build := req.fromSource || previous.Platforms[host.String()].Strategy == strategyBuild &&
		previous.Version == m.Version.Value

	switch {
	case (build || !ok) && m.HasBuild():
		build = true
	case build:
		return installed{}, fmt.Errorf(
			"%s has no [build], so it cannot be built from source",
			m.Package.Name,
		)
	case !ok:
		return installed{}, fmt.Errorf("%s has no artifact for %s", m.Package.Name, host)
	}

	// oku installs deps first. A build links against its build deps, and every dep
	// stays in the closure so that gc keeps it.
	wanted := m.Runtime.Deps
	if build {
		wanted = append(slices.Clone(m.Build.Deps), wanted...)
	}

	deps, err := e.installDeps(ctx, opts, req, wanted)
	if err != nil {
		return installed{}, err
	}

	var (
		realized store.Realized
		entry    lock.Platform
	)

	if build {
		if err := req.approve(m, host); err != nil {
			return installed{}, err
		}

		// A locked build must download the same packages again. Update drops the pin.
		pinnedVendor := ""
		if at := previous.Platforms[host.String()]; req.keepVersion &&
			previous.ManifestSHA256 == m.SHA256 {
			pinnedVendor = at.VendorSHA256
		}

		realized, err = e.store().Build(ctx, m, host, store.BuildOptions{
			Deps: deps.prefixes, Log: req.log, PinnedVendor: pinnedVendor, Progress: req.progress,
		})
		if err != nil {
			return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
		}

		entry = lock.Platform{
			Strategy: strategyBuild, Impure: realized.Impure, VendorSHA256: realized.VendorSHA256,
		}
	} else {
		// A digest that oku.lock pinned for this version and URL still applies,
		// even when the manifest gives none.
		pinned := ""
		if at := previous.Platforms[host.String()]; previous.Version == m.Version.Value &&
			at.URL == artifact.URL {
			pinned = at.SHA256
		}

		if req.acceptDigest && (artifact.SHA256 != "" || artifact.SHA256URL != "") {
			pinned = ""
		}

		if realized, err = e.store().Realize(ctx, m, artifact, host, pinned); err != nil {
			return installed{}, err
		}

		entry = lock.Platform{
			Strategy: strategyArtifact,
			URL:      artifact.URL,
			SHA256:   realized.SHA256,
		}
	}

	// Entries for other platforms stay while they describe the same manifest.
	platforms := map[string]lock.Platform{}

	if previous.ManifestSHA256 == m.SHA256 && previous.Ref == r.String() &&
		previous.Version == m.Version.Value {
		for name, at := range previous.Platforms {
			platforms[name] = at
		}
	}

	platforms[host.String()] = entry

	return installed{
		profile: profile.Package{
			Name:      m.Package.Name,
			Version:   m.Version.Value,
			Ref:       r.String(),
			StorePath: realized.Path,
			Closure:   deps.closure,
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
			Deps:           deps.locks,
		},
		closure:     append([]string{realized.Path}, deps.closure...),
		firstUse:    realized.FirstUse,
		inferred:    inferred,
		unsandboxed: realized.Unsandboxed,
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

// reportUnsandboxed warns that a build's commands ran without the sandbox.
func reportUnsandboxed(w io.Writer, got installed) {
	if got.unsandboxed != "" {
		fmt.Fprintf(
			w, "%s was built without the sandbox, because %s\n"+
				"its build commands could use the network and read your files\n",
			got.lock.Name, got.unsandboxed,
		)
	}
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

// depSet is what installing a package's deps produced.
type depSet struct {
	prefixes []store.Dep
	locks    []lock.Package
	closure  []string
}

// installDeps installs the deps of the package in parent, each through the same
// pipeline, so a dep may be an artifact or a build and may have deps of its own.
func (e env) installDeps(
	ctx context.Context,
	opts Options,
	parent request,
	wanted []manifest.Dep,
) (depSet, error) {
	var set depSet

	base := ""
	if parent.ref.Kind == ref.File {
		base = filepath.Dir(parent.ref.Location)
	}

	for _, dep := range wanted {
		r, err := ref.ParseIn(base, dep.Ref)
		if err != nil {
			return set, fmt.Errorf("dep %s: %w", dep.Ref, err)
		}

		if base == "" && r.Kind == ref.File {
			return set, fmt.Errorf(
				"dep %s: a remote manifest cannot depend on a local path",
				dep.Ref,
			)
		}

		stack := append(slices.Clone(parent.stack), parent.ref.String())
		if slices.Contains(stack, r.String()) {
			return set, fmt.Errorf(
				"dependency cycle: %s",
				strings.Join(append(stack, r.String()), " -> "),
			)
		}

		previous := parent.previous.FindDep(r.String())
		keep := parent.keepVersion && previous.Ref != ""

		commit, wantManifest := "", ""
		if keep {
			commit, wantManifest = previous.Commit, previous.ManifestSHA256
		}

		got, err := e.install(ctx, opts, request{
			ref:          r,
			commit:       commit,
			previous:     previous,
			wantManifest: wantManifest,
			acceptDigest: parent.acceptDigest,
			keepVersion:  keep,
			approve:      parent.approve,
			log:          parent.log,
			constraint:   dep.Version,
			stack:        stack,
		})
		if err != nil {
			return set, fmt.Errorf("dep %s: %w", r, err)
		}

		set.prefixes = append(
			set.prefixes,
			store.Dep{Name: got.lock.Name, Prefix: got.profile.StorePath},
		)
		set.locks = append(set.locks, got.lock)

		for _, path := range got.closure {
			if !slices.Contains(set.closure, path) {
				set.closure = append(set.closure, path)
			}
		}
	}

	return set, nil
}
