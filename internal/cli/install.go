package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
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
	// firstUseOthers names the other platforms whose download oku trusted.
	firstUseOthers []string
	// inferred is the manifest text when oku inferred it during this install.
	inferred string
	// unsupported holds the platforms install was asked for that the manifest
	// has no artifact and no build for. when is then the entry's when narrowed to
	// the platforms it fits, or empty when it fits none of them.
	unsupported []platform.Platform
	when        platform.When
	// support holds every platform the manifest has an artifact or a build for.
	support []platform.Platform
	// lockOnly says that nothing was installed, since the manifest does not fit
	// the host or the request only locks.
	lockOnly bool
	// unsandboxed says why the build ran without the sandbox, or is empty.
	unsandboxed string
	// substituted reports that the package came from a cache, and cacheNotes
	// lists the cache entries oku ignored. Both cover the deps too.
	substituted []string
	cacheNotes  []string
	// linkNotes warns for each store package that a build loads without naming
	// it in runtime.deps, for the deps too.
	linkNotes []string
}

// fitMode says what install does with a platform that the manifest has no
// artifact and no build for.
type fitMode int

const (
	// fitStrict makes it an error, as for a dep, `oku shell` and `manifest test`.
	fitStrict fitMode = iota
	// fitNarrow leaves it out, and the result has the narrowed when, which add
	// and update write to the list. A package that fits none is an error.
	fitNarrow
	// fitReport leaves it out, and sync reports the package. A package that fits
	// none keeps its lock entry and installs nothing.
	fitReport
)

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
	// acceptKey lets the manifest's signing key differ from the one pinned in
	// previous. "--accept-key" sets it.
	acceptKey bool
	// keepVersion installs the version in previous without listing versions
	// again. "oku sync" sets it.
	keepVersion bool
	// asset and bins name the asset and the programs for an inferred manifest.
	// "--asset" and "--bin" set them.
	asset string
	bins  []string
	// verbose adds the inferred manifest to an error from it.
	verbose bool
	// service enables the package's services.
	service bool
	// system puts the package's apps, fonts and services in system scope.
	system bool
	// fromSource builds even when a prebuilt artifact fits the host.
	fromSource bool
	// platforms are the platforms besides the host that the lock entry covers.
	// With strictPlatforms, one that install cannot pin is an error and sync pins
	// the missing ones too. Without it, add and update pin the ones they can.
	platforms       []platform.Platform
	strictPlatforms bool
	// rebuild builds the package again when the store holds its build. "sync
	// --rebuild" sets it. Deps do not inherit it.
	rebuild bool
	// lockOnly pins the package for platforms and installs nothing. sync sets it
	// for a package whose when leaves out the host.
	lockOnly bool
	// when is the list entry's when, and fit says what install does with a
	// platform the manifest has no artifact or build for.
	when platform.When
	fit  fitMode
	// approve decides whether a manifest may run its build commands, or, with an
	// artifact, the command that generates the artifact's completions.
	approve func(m *manifest.Manifest, host platform.Platform, a *manifest.Artifact) error
	// log receives the output of build commands, or is nil.
	log io.Writer
	// constraint limits the version of a dep, as a range such as ">=3" or a
	// prefix such as "22", as Pick reads it. A version pin in ref overrides it.
	constraint string
	// progress reports each build step of this package, and may be nil. Deps do
	// not inherit it.
	progress func(step, total int, kind string, err error)
	// stack holds the refs being installed above this one. A ref that is already
	// in it is a dependency cycle.
	stack []string
	// root names the package of the list that this install belongs to.
	root string
	// deps holds the deps this run installs, so packages that share a dep install
	// it once. With nil, install installs every dep again.
	deps *depCache
}

// buildMu lets one package build at a time. A build uses every core, and its
// approval prompt needs the terminal to itself.
var buildMu sync.Mutex

// depCache shares deps between the packages of one run, which install in
// parallel.
type depCache struct {
	mu      sync.Mutex
	entries map[string]*depEntry
	// waiting holds the key each root waits for. A cycle between the deps of two
	// roots would otherwise leave both waiting forever.
	waiting map[string]string
}

type depEntry struct {
	root string
	done chan struct{}
	got  installed
	err  error
}

func newDepCache() *depCache {
	return &depCache{entries: map[string]*depEntry{}, waiting: map[string]string{}}
}

// do returns what install gives for key, and runs install once for all roots.
func (c *depCache) do(root, key string, install func() (installed, error)) (installed, error) {
	if c == nil {
		return install()
	}

	c.mu.Lock()

	entry, ok := c.entries[key]
	if !ok {
		entry = &depEntry{root: root, done: make(chan struct{})}
		c.entries[key] = entry
		c.mu.Unlock()

		got, err := install()

		// Only the first package reports the dep's cache notes.
		entry.got = installed{profile: got.profile, lock: got.lock, closure: got.closure}
		entry.err = err
		close(entry.done)

		return got, err
	}

	select {
	case <-entry.done:
		c.mu.Unlock()

		return entry.got, entry.err
	default:
	}

	for at := entry; ; {
		if at.root == root {
			c.mu.Unlock()

			return installed{}, errors.New("dependency cycle between the deps of two packages")
		}

		next, waits := c.entries[c.waiting[at.root]]
		if !waits {
			break
		}

		at = next
	}

	c.waiting[root] = key
	c.mu.Unlock()

	<-entry.done

	c.mu.Lock()
	delete(c.waiting, root)
	c.mu.Unlock()

	return entry.got, entry.err
}

// install fetches the manifest, realizes the host's artifact and returns the
// profile and lock entries for it. It changes the store only.
func (e env) install(ctx context.Context, opts Options, req request) (installed, error) {
	fetched, inferred, err := e.manifestData(ctx, opts, req)
	if err != nil {
		return installed{}, err
	}

	m, err := manifest.Parse(fetched.Data, req.ref.String())
	if err != nil {
		return installed{}, err
	}

	// install drops a platform that the manifest has no artifact and no build
	// for from the request, so the rest of the list still installs there.
	fits, unsupported := supportedTargets(m, req)
	if req.fit == fitStrict {
		unsupported = nil
	}

	if len(unsupported) > 0 && len(fits) == 0 {
		if req.fit == fitNarrow {
			return installed{}, noSupportError(m, unsupported)
		}

		// sync keeps what oku.lock pins, installs nothing, and reports the package.
		return installed{
			lock: req.previous, lockOnly: true, unsupported: unsupported,
			when: narrowedWhen(m, req.when),
		}, nil
	}

	if len(unsupported) > 0 {
		req.platforms = slices.DeleteFunc(slices.Clone(req.platforms), func(p platform.Platform) bool {
			return !m.Supports(p)
		})
		req.lockOnly = req.lockOnly || !m.Supports(platform.Host())
	}

	got, err := e.installFrom(ctx, opts, req, fetched, inferred.Text)

	// The user never saw an inferred manifest, so an error from it says what oku
	// chose and what to type instead.
	if err != nil && inferred.Text != "" {
		return installed{}, fmt.Errorf("%w\n%s", err, inferredHints(req, inferred))
	}

	got.lockOnly = req.lockOnly
	got.support = slices.DeleteFunc(platform.All(), func(p platform.Platform) bool {
		return !m.Supports(p)
	})

	if len(unsupported) > 0 {
		got.unsupported = unsupported
		got.when = narrowedWhen(m, req.when)
	}

	return got, err
}

// supportedTargets splits the platforms install works for, the host unless
// req only locks and the platforms of req, into those that m has an artifact
// or a build for and the rest.
func supportedTargets(m *manifest.Manifest, req request) (fits, unsupported []platform.Platform) {
	targets := req.platforms
	if !req.lockOnly {
		targets = append([]platform.Platform{platform.Host()}, targets...)
	}

	for _, p := range targets {
		if m.Supports(p) {
			fits = append(fits, p)
		} else {
			unsupported = append(unsupported, p)
		}
	}

	return fits, unsupported
}

// narrowedWhen returns the when that leaves out every platform m has no
// artifact or build for, within the platforms that when already matches. It is
// empty when no platform is left.
func narrowedWhen(m *manifest.Manifest, when platform.When) platform.When {
	left := slices.DeleteFunc(when.Of(), func(p platform.Platform) bool { return !m.Supports(p) })
	if len(left) == 0 {
		return nil
	}

	return platform.Cover(left)
}

// noSupportError says that m fits none of the platforms oku works for, and
// which ones it does fit.
func noSupportError(m *manifest.Manifest, unsupported []platform.Platform) error {
	var has []string

	for _, p := range platform.All() {
		if m.Supports(p) {
			has = append(has, p.String())
		}
	}

	if len(has) == 0 {
		return fmt.Errorf(
			"%s has no artifact and no build for %s, nor for any other platform",
			m.Package.Name, platformNames(unsupported),
		)
	}

	return fmt.Errorf(
		"%s has no artifact or build for %s, only for %s",
		m.Package.Name, platformNames(unsupported), strings.Join(has, ", "),
	)
}

// inferredHints tells the user what oku chose from the release and how to
// choose otherwise. With verbose it ends with the manifest.
func inferredHints(req request, inferred infer.Inferred) string {
	var b strings.Builder

	fmt.Fprintf(&b, "oku inferred a manifest for %s from its release", req.ref)

	if !req.verbose {
		b.WriteString(", --verbose prints it")
	}

	if inferred.Asset != "" {
		fmt.Fprintf(&b, "\nit chose the asset %s for this machine", inferred.Asset)
	}

	if len(inferred.Others) > 0 {
		fmt.Fprintf(
			&b, "\nthese fit too: %s\npick one with: oku add %s --asset %s",
			strings.Join(inferred.Others, ", "), req.ref, inferred.Others[0],
		)
	}

	if req.verbose {
		fmt.Fprintf(&b, "\n\n%s", strings.TrimSpace(inferred.Text))
	}

	return b.String()
}

// installFrom installs the manifest in fetched, which is inferred when oku
// wrote it during this install.
func (e env) installFrom(
	ctx context.Context,
	opts Options,
	req request,
	fetched ref.Fetched,
	inferred string,
) (installed, error) {
	r, previous, wantManifest := req.ref, req.previous, req.wantManifest

	m, err := manifest.Parse(fetched.Data, r.String())
	if err != nil {
		return installed{}, err
	}

	if wantManifest != "" && wantManifest != m.SHA256 {
		return installed{}, errManifestChanged
	}

	if pinned := previous.SigningKey; pinned != "" && pinned != m.Package.SigningKey &&
		!req.acceptKey {
		now := "no signing key"
		if m.Package.SigningKey != "" {
			now = "the signing key " + m.Package.SigningKey
		}

		return installed{}, fmt.Errorf(
			"%s: oku.lock pinned the signing key %s, and the manifest now has %s\n"+
				"if the developer announced this change, run the command again with --accept-key",
			m.Package.Name, pinned, now,
		)
	}

	// A version in the list limits the versions oku may pick. sync stays on the
	// locked version while the list allows it, and add and update take the
	// newest that it allows.
	release := resolve.Release{
		Version: previous.Version, Tag: previous.Tag, Commit: previous.TagCommit,
	}

	if m.PerArtifact() && (r.Version != "" || req.constraint != "") {
		return installed{}, fmt.Errorf(
			"%s: each artifact finds its own version, so you cannot pick one", r,
		)
	}

	allowed, err := resolve.Matches(previous.Version, r.Version)
	if err != nil {
		return installed{}, fmt.Errorf("%s: %w", r, err)
	}

	keep := req.keepVersion && previous.Version != "" && allowed
	switch {
	case m.PerArtifact():
		release, err = e.artifactVersions(ctx, opts, req, m, keep)
	case keep:
	case r.Version == "" && req.constraint != "":
		release, err = e.resolver(opts).Pick(ctx, m.Version, req.constraint)
	default:
		release, err = e.resolver(opts).Pick(ctx, m.Version, r.Version)
	}

	if err != nil {
		return installed{}, fmt.Errorf("%s: %w", r, err)
	}

	if release.Tag == "" {
		release.Tag = release.Version
	}

	m.Version.Value, m.Tag, m.TagCommit = release.Version, release.Tag, release.Commit
	ctx = status.Scope(ctx, m.Package.Name+" "+m.Version.Value)

	if req.lockOnly {
		return e.resolveOnly(ctx, opts, req, m, release, fetched, inferred)
	}

	host := platform.Host()

	artifact, ok, err := m.Select(host)
	if err != nil {
		return installed{}, err
	}

	// A platform whose lock entry says "build" is built from source again.
	build := req.fromSource || previous.Platforms[host.String()].Strategy == strategyBuild &&
		previous.VersionOn(host.String()) == m.Version.Value

	switch {
	case (build || !ok) && m.BuildsOn(host):
		build = true
	case build:
		if m.HasBuild() {
			return installed{}, fmt.Errorf(
				"the [build] of %s leaves out %s in its when", m.Package.Name, host,
			)
		}

		return installed{}, fmt.Errorf(
			"%s has no [build], so it cannot be built from source",
			m.Package.Name,
		)
	case !ok:
		return installed{}, fmt.Errorf("%s has no artifact for %s", m.Package.Name, host)
	}

	// oku installs deps first. A build links against its build deps, and every dep
	// stays in the closure so that gc keeps it.
	wanted, buildDeps := m.Runtime.Deps, 0
	if build {
		wanted = append(slices.Clone(m.Build.Deps), wanted...)
		buildDeps = len(m.Build.Deps)
	}

	deps, err := e.installDeps(ctx, opts, req, fetched, wanted)
	if err != nil {
		return installed{}, err
	}

	var (
		realized store.Realized
		entry    lock.Platform
	)

	var cached bool

	// A locked build must download the same packages again. Update drops the pin.
	pinnedVendor := ""
	if at := previous.Platforms[host.String()]; req.keepVersion &&
		previous.ManifestSHA256 == m.SHA256 {
		pinnedVendor = at.VendorSHA256
	}

	// The digest of a source archive stays pinned for the version it was pinned
	// for. Update drops it only together with the version, or when the manifest
	// now states a digest itself.
	pinnedSource := ""
	if at := previous.Platforms[host.String()]; previous.VersionOn(host.String()) == m.Version.Value &&
		at.Strategy == strategyBuild && !req.acceptDigest {
		pinnedSource = at.SHA256
	}

	if req.rebuild && !build {
		return installed{}, fmt.Errorf(
			"%s is a download on %s, and --rebuild is for a package that oku builds",
			m.Package.Name, host,
		)
	}

	if build {
		buildMu.Lock()
		defer buildMu.Unlock()

		realized.Path = e.store().BuildPath(m, host, deps.prefixes)

		// A rebuild runs the build on this machine and takes none from a cache.
		if !req.rebuild {
			var notes []string
			if cached, notes, err = e.substitute(ctx, realized.Path); err != nil {
				return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
			}

			deps.cacheNotes = append(deps.cacheNotes, notes...)
		}
	}

	switch {
	case cached:
		// A package from a cache runs none of the manifest's commands, so it needs
		// no approval. A cache never holds an impure package.
		deps.substituted = append(deps.substituted, m.Package.Name)

		// The cache entry holds the pins of the build that made it.
		meta, _ := store.ReadMeta(realized.Path)
		if pinnedVendor != "" && meta.VendorSHA256 != "" && pinnedVendor != meta.VendorSHA256 {
			return installed{}, fmt.Errorf(
				"%s: %w: oku.lock pinned %s, the build in the cache downloaded %s",
				m.Package.Name, store.ErrVendorChanged, pinnedVendor, meta.VendorSHA256,
			)
		}

		entry = keepPins(lock.Platform{
			Strategy:     strategyBuild,
			VendorSHA256: meta.VendorSHA256,
			URL:          meta.URL,
			SHA256:       meta.SHA256,
		}, previous, m, host)
	case build:
		if err := req.approve(m, host, nil); err != nil {
			return installed{}, err
		}

		// crates.io and GitHub publish the digest of each version's source, which
		// the manifest cannot state for every version.
		if src := &m.Build.Source; src.URL != "" && src.SHA256 == "" && src.SHA256URL == "" {
			vars := map[string]string{"version": m.Version.Value, "tag": m.Tag}
			if at, err := manifest.Expand(src.URL, vars); err == nil {
				src.SHA256 = release.Digests[at]
			}
		}

		realized, err = e.store().Build(ctx, m, host, store.BuildOptions{
			Deps: deps.prefixes, Log: req.log, PinnedVendor: pinnedVendor, Progress: req.progress,
			NPMRegistry: opts.NPMRegistry, PyPIIndex: opts.PyPIIndex, GoProxy: opts.GoProxy,
			PinnedSource: pinnedSource, Rebuild: req.rebuild,
			RuntimeDeps: deps.prefixes[buildDeps:],
		})
		if err != nil {
			return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
		}

		for _, missing := range realized.MissingDeps {
			deps.linkNotes = append(deps.linkNotes, fmt.Sprintf(
				"%s: %s loads %s, which is not in runtime.deps, so it breaks after `oku gc` or on another machine",
				m.Package.Name,
				missing.File,
				missing.Package,
			))
		}

		entry = keepPins(lock.Platform{
			Strategy: strategyBuild, Impure: realized.Impure, VendorSHA256: realized.VendorSHA256,
			URL: realized.SourceURL, SHA256: realized.SHA256,
		}, previous, m, host)

		// A build from before oku recorded the source archive has no pin for it.
		if entry.SHA256 == "" {
			pin, err := e.store().PinBuild(ctx, m, host, store.BuildPin{})
			if err != nil {
				return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
			}

			entry.URL, entry.SHA256 = pin.SourceURL, pin.SHA256
			realized.FirstUse = realized.FirstUse || pin.FirstUse
		}
	default:
		// A digest that oku.lock pinned for this version and URL still applies,
		// even when the manifest gives none.
		pinned := ""
		if at := previous.Platforms[host.String()]; previous.VersionOn(host.String()) == m.Version.Value &&
			at.URL == artifact.URL {
			pinned = at.SHA256
		}

		// The locked build of a moving tag is gone once upstream moved the tag, so
		// a download would be a newer build under the locked version.
		if keep && m.Version.Tag != "" && !e.store().Has(m, artifact, host, pinned, deps.prefixes) {
			now, err := e.resolver(opts).Pick(ctx, m.Version, "")
			if err != nil {
				return installed{}, fmt.Errorf("%s: %w", r, err)
			}

			if now.Commit != previous.TagCommit {
				return installed{}, fmt.Errorf(
					"upstream moved the tag %s to %s since oku.lock was written, "+
						"and the locked build %s is gone\nrun `oku update %s` to take the new build",
					m.Version.Tag, now.Version, previous.Version, m.Package.Name,
				)
			}

			release.Digests = now.Digests
		}

		if artifact.SHA256 == "" && artifact.SHA256URL == "" {
			artifact.SHA256 = release.Digests[artifact.URL]
		}

		// The npm registry publishes a sha512 for every version's download.
		if artifact.SHA256 == "" && artifact.SHA256URL == "" && artifact.Integrity == "" {
			artifact.Integrity = release.Integrity[artifact.URL]
		}

		if req.acceptDigest && (artifact.SHA256 != "" || artifact.SHA256URL != "") {
			pinned = ""
		}

		auth := e.fetcher(opts).Hosts.AuthFor(m.Version.From, m.Version.Repo)

		if artifact.Completions.Generate != "" {
			if err := req.approve(m, host, &artifact); err != nil {
				return installed{}, err
			}
		}

		if realized, err = e.store().As(auth).Realize(
			ctx, m, artifact, host, pinned, deps.prefixes,
		); err != nil {
			return installed{}, err
		}

		entry = lock.Platform{
			Strategy: strategyArtifact,
			URL:      artifact.URL,
			SHA256:   realized.SHA256,
			Commands: artifact.Completions.Generate != "",
		}
	}

	if m.PerArtifact() {
		entry.Version = m.Version.Value
	}

	env := map[string]string{}

	// A build puts its files at the top of the store path. An artifact's
	// download is unpacked under "pkg".
	files := realized.Path
	if !build {
		files = filepath.Join(realized.Path, "pkg")
	}

	for name, value := range m.Env {
		expanded, err := manifest.Expand(value, map[string]string{
			"prefix": realized.Path, "pkg": files, "version": m.Version.Value, "tag": m.Tag,
		})
		if err != nil {
			return installed{}, fmt.Errorf("env.%s: %w", name, err)
		}

		env[name] = expanded
	}

	platforms := keptPlatforms(previous, m, r)
	platforms[host.String()] = entry

	firstUseOthers, err := e.lockOthers(ctx, opts, req, m, release, platforms, hostBuild{
		entry: entry, deps: deps.prefixes, npmRegistry: opts.NPMRegistry,
		pypiIndex: opts.PyPIIndex, goProxy: opts.GoProxy, log: req.log,
	})
	if err != nil {
		return installed{}, err
	}

	return installed{
		profile: profile.Package{
			Name:      m.Package.Name,
			Version:   m.Version.Value,
			Ref:       r.String(),
			StorePath: realized.Path,
			Closure:   deps.closure,
			BuildOnly: deps.buildOnly(buildDeps),
			Env:       env,
			Service:   req.service,
			System:    req.system,
		},
		lock:           lockEntry(req, m, fetched, inferred, platforms, deps.locks),
		closure:        append([]string{realized.Path}, deps.closure...),
		firstUse:       realized.FirstUse,
		firstUseOthers: firstUseOthers,
		inferred:       inferred,
		unsandboxed:    realized.Unsandboxed,
		substituted:    deps.substituted,
		cacheNotes:     deps.cacheNotes,
		linkNotes:      deps.linkNotes,
	}, nil
}

// keepPins fills the pins that entry lacks from the entry of the same build in
// previous. A store path from before oku recorded the pins of a build reports
// none, and the lock would lose them otherwise.
func keepPins(
	entry lock.Platform,
	previous lock.Package,
	m *manifest.Manifest,
	host platform.Platform,
) lock.Platform {
	at := previous.Platforms[host.String()]
	if at.Strategy != strategyBuild || previous.ManifestSHA256 != m.SHA256 ||
		previous.VersionOn(host.String()) != m.Version.Value {
		return entry
	}

	entry.VendorSHA256 = cmp.Or(entry.VendorSHA256, at.VendorSHA256)

	if entry.SHA256 == "" {
		entry.URL, entry.SHA256 = at.URL, at.SHA256
	}

	return entry
}

// keptPlatforms returns the platform entries of previous that still describe m.
// When each artifact finds its own version, an entry stays unless oku found
// another version for its platform.
func keptPlatforms(
	previous lock.Package,
	m *manifest.Manifest,
	r ref.Ref,
) map[string]lock.Platform {
	platforms := map[string]lock.Platform{}

	if previous.ManifestSHA256 != m.SHA256 || previous.Ref != r.String() {
		return platforms
	}

	if !m.PerArtifact() {
		if previous.Version == m.Version.Value {
			maps.Copy(platforms, previous.Platforms)
		}

		return platforms
	}

	for key, at := range previous.Platforms {
		if now := m.Versions[key]; now == "" || now == at.Version {
			platforms[key] = at
		}
	}

	return platforms
}

// lockedVersion returns the version that the lock entry of m names. When each
// artifact finds its own version, that is the version of the first platform in
// order, so every machine writes the same one.
func lockedVersion(m *manifest.Manifest, platforms map[string]lock.Platform) string {
	if !m.PerArtifact() {
		return m.Version.Value
	}

	for _, key := range slices.Sorted(maps.Keys(platforms)) {
		if v := platforms[key].Version; v != "" {
			return v
		}
	}

	return m.Version.Value
}

// artifactVersions finds the version of each platform that req installs or
// pins, for a manifest whose artifacts find their own versions, and keeps them
// in m.Versions. With keep, a platform keeps the version oku.lock pins for it.
// It returns the release of the platform that req installs.
func (e env) artifactVersions(
	ctx context.Context,
	opts Options,
	req request,
	m *manifest.Manifest,
	keep bool,
) (resolve.Release, error) {
	targets := []platform.Platform{req.target()}

	// lockOthers pins other platforms only then.
	if !req.keepVersion || req.strictPlatforms || req.lockOnly {
		targets = append(targets, req.platforms...)
	}

	m.Versions = map[string]string{}
	found := map[manifest.Version]string{}

	for _, p := range targets {
		i := slices.IndexFunc(m.Artifacts, func(a manifest.Artifact) bool { return a.Match.Matches(p) })
		if i < 0 {
			continue
		}

		source := *m.Artifacts[i].Version

		version := ""
		if keep {
			version = req.previous.Platforms[p.String()].Version
		}

		if version == "" {
			version = found[source]
		}

		if version == "" {
			release, err := e.resolver(opts).Pick(ctx, source, "")

			// lockOthers skips a platform it cannot pin, unless the platforms are
			// strict.
			if err != nil && p != req.target() && !req.strictPlatforms {
				continue
			}

			if err != nil {
				return resolve.Release{}, fmt.Errorf("%s for %s: %w", m.Package.Name, p, err)
			}

			version = release.Version
			found[source] = version
		}

		m.Versions[p.String()] = version
	}

	version := m.Versions[req.target().String()]

	return resolve.Release{Version: version, Tag: version}, nil
}

// lockEntry returns the lock entry of m.
func lockEntry(
	req request,
	m *manifest.Manifest,
	fetched ref.Fetched,
	inferred string,
	platforms map[string]lock.Platform,
	deps []lock.Package,
) lock.Package {
	return lock.Package{
		Name:           m.Package.Name,
		Ref:            req.ref.String(),
		Commit:         fetched.Commit,
		ManifestSHA256: m.SHA256,
		Version:        lockedVersion(m, platforms),
		SigningKey:     m.Package.SigningKey,
		Tag:            tagFor(m),
		TagCommit:      m.TagCommit,
		Inferred:       inferred != "" || req.previous.Inferred && req.keepVersion,
		Manifest:       inferredText(inferred, req),
		Asset:          inferredAs(inferred, req, req.inferAsset()),
		Bins:           inferredAs(inferred, req, req.inferBins()),
		Platforms:      platforms,
		Deps:           deps,
	}
}

// resolveOnly returns the lock entry of m for the platforms of req, none of
// which is the host, and the entries of its deps. It installs nothing.
func (e env) resolveOnly(
	ctx context.Context,
	opts Options,
	req request,
	m *manifest.Manifest,
	release resolve.Release,
	fetched ref.Fetched,
	inferred string,
) (installed, error) {
	platforms := keptPlatforms(req.previous, m, req.ref)

	firstUse, err := e.lockOthers(ctx, opts, req, m, release, platforms, hostBuild{})
	if err != nil {
		return installed{}, err
	}

	// A platform that builds needs the build deps too.
	wanted := m.Runtime.Deps

	for _, at := range platforms {
		if at.Strategy == strategyBuild {
			wanted = append(slices.Clone(m.Build.Deps), wanted...)

			break
		}
	}

	deps, err := e.installDeps(ctx, opts, req, fetched, wanted)
	if err != nil {
		return installed{}, err
	}

	return installed{
		lock:           lockEntry(req, m, fetched, inferred, platforms, deps.locks),
		firstUseOthers: firstUse,
		inferred:       inferred,
	}, nil
}

// lockOthers pins m in platforms for each platform of req that has no entry
// yet, or whose build entry lacks a pin. It installs nothing.
// host is the install on this machine, or the zero value. It returns the
// platforms whose download it trusted on first use.
func (e env) lockOthers(
	ctx context.Context,
	opts Options,
	req request,
	m *manifest.Manifest,
	release resolve.Release,
	platforms map[string]lock.Platform,
	host hostBuild,
) ([]string, error) {
	if req.keepVersion && !req.strictPlatforms && !req.lockOnly {
		return nil, nil
	}

	var firstUse []string

	auth := e.fetcher(opts).Hosts.AuthFor(m.Version.From, m.Version.Repo)

	for _, p := range req.platforms {
		// An artifact that is pinned is complete. A build may still lack a pin that
		// an older oku did not write, and pinFor adds only what is missing.
		at, ok := platforms[p.String()]
		if ok && at.Strategy != strategyBuild {
			continue
		}

		// Progress names the version of p, not the host's.
		scoped := ctx
		if m.PerArtifact() {
			scoped = status.Scope(ctx, m.Package.Name+" "+m.Versions[p.String()])
		}

		entry, trusted, err := pinFor(scoped, e.store().As(auth), m, release, p, host, at)
		if err != nil && req.strictPlatforms {
			return nil, err
		}

		if err != nil {
			continue
		}

		if trusted {
			firstUse = append(firstUse, p.String())
		}

		platforms[p.String()] = entry
	}

	return firstUse, nil
}

// hostBuild is what pinning another platform takes from the install on this
// machine: its lock entry, and the deps and options of its build.
type hostBuild struct {
	entry       lock.Platform
	deps        []store.Dep
	npmRegistry string
	pypiIndex   string
	goProxy     string
	log         io.Writer
}

// pinFor returns the lock entry of m for platform p, and whether oku trusted a
// download for it. host is the install on this machine, and at is the entry
// that oku.lock holds for p, whose pins stay.
func pinFor(
	ctx context.Context,
	s *store.Store,
	m *manifest.Manifest,
	release resolve.Release,
	p platform.Platform,
	host hostBuild,
	at lock.Platform,
) (lock.Platform, bool, error) {
	artifact, ok, err := m.Select(p)

	switch {
	case err != nil:
		return lock.Platform{}, false, err
	case !ok && m.BuildsOn(p):
		pin, err := s.PinBuild(ctx, m, p, store.BuildPin{SourceURL: at.URL, SHA256: at.SHA256})
		if err != nil {
			return lock.Platform{}, false, fmt.Errorf("%s for %s: %w", m.Package.Name, p, err)
		}

		entry := lock.Platform{
			Strategy: strategyBuild, Impure: pin.Impure, URL: pin.SourceURL, SHA256: pin.SHA256,
			VendorSHA256: at.VendorSHA256,
		}

		// Go and cargo vendor the same files on every platform, so the digest of
		// the build on this machine holds for p too. npm installs the packages of
		// the platform it is told, so oku downloads those of p and builds nothing.
		switch {
		case host.entry.Strategy != strategyBuild || entry.VendorSHA256 != "":
		case store.VendorPortable(m.Build):
			entry.VendorSHA256 = host.entry.VendorSHA256
		case store.CanCrossVendor(m.Build, p):
			vendored, err := s.Build(ctx, m, p, store.BuildOptions{
				Deps: host.deps, Log: host.log, NPMRegistry: host.npmRegistry,
				PyPIIndex: host.pypiIndex, GoProxy: host.goProxy, VendorOnly: true,
			})
			if err != nil {
				return lock.Platform{}, false, fmt.Errorf("%s for %s: %w", m.Package.Name, p, err)
			}

			entry.VendorSHA256 = vendored.VendorSHA256
		}

		// The install on this machine already reported the archive they share.
		return entry, pin.FirstUse && pin.SourceURL != host.entry.URL, nil
	case !ok:
		return lock.Platform{}, false, fmt.Errorf("%s has no artifact for %s", m.Package.Name, p)
	}

	if artifact.SHA256 == "" && artifact.SHA256URL == "" {
		artifact.SHA256 = release.Digests[artifact.URL]
		artifact.Integrity = cmp.Or(artifact.Integrity, release.Integrity[artifact.URL])
	}

	sum, trusted, err := s.Pin(ctx, m, artifact)
	if err != nil {
		return lock.Platform{}, false, fmt.Errorf("%s for %s: %w", m.Package.Name, p, err)
	}

	return lock.Platform{
		Strategy: strategyArtifact, URL: artifact.URL, SHA256: sum,
		Commands: artifact.Completions.Generate != "", Version: m.Versions[p.String()],
	}, trusted, nil
}

// target returns the platform that an inferred manifest must fit. That is the
// host, or the first platform of a request that only locks.
func (req request) target() platform.Platform {
	if req.lockOnly {
		return req.platforms[0]
	}

	return platform.Host()
}

// manifestData returns the manifest for req. A GitHub repo without a manifest
// gets an inferred one, which is returned a second time as text. Sync reuses the
// inferred text in the lock, so it installs from what the user saw.
func (e env) manifestData(
	ctx context.Context,
	opts Options,
	req request,
) (ref.Fetched, infer.Inferred, error) {
	if req.keepVersion && req.previous.Inferred {
		return ref.Fetched{
			Data:   []byte(req.previous.Manifest),
			Commit: req.previous.Commit,
		}, infer.Inferred{}, nil
	}

	if write := e.inferrerOf(req.ref.Kind); write != nil {
		text, err := e.inferAt(ctx, opts, req.ref.Version, func(version string) (string, error) {
			at := req
			at.ref.Version = version

			return write(ctx, opts, at)
		})

		return ref.Fetched{Data: []byte(text)}, infer.Inferred{Text: text}, err
	}

	fetched, err := e.fetcher(opts).Fetch(ctx, req.ref, req.commit, ref.Manifest)

	if req.ref.Kind == ref.HTTP && isDownload(req.ref, fetched.Data, err) {
		if req.asset != "" {
			return fetched, infer.Inferred{}, fmt.Errorf("--asset does not apply, %s is the asset", req.ref)
		}

		text, err := e.inferrer(opts).FromURL(ctx, req.ref.Location, req.target(), req.inferBins())
		if err != nil {
			return ref.Fetched{}, infer.Inferred{}, err
		}

		return ref.Fetched{Data: []byte(text)}, infer.Inferred{Text: text}, nil
	}

	if err == nil && (req.asset != "" || len(req.bins) > 0) {
		return fetched, infer.Inferred{}, fmt.Errorf(
			"--asset and --bin apply when oku infers a manifest, and %s has one", req.ref,
		)
	}

	if err == nil || !errors.Is(err, ref.ErrNotFound) ||
		req.ref.Kind != ref.Forge || req.ref.Fragment != "" {
		return fetched, infer.Inferred{}, err
	}

	var (
		inferred infer.Inferred
		first    error
	)

	// A release with no asset for the host may still have one for a platform of
	// the lock, which add and sync then pin without installing it here.
	targets := []platform.Platform{req.target()}
	if req.fit != fitStrict && !req.lockOnly {
		targets = append(targets, req.platforms...)
	}

	for i, target := range targets {
		_, err = e.inferAt(ctx, opts, req.ref.Version, func(version string) (string, error) {
			var err error

			inferred, err = e.inferrer(opts).Manifest(
				ctx, req.ref.Scheme, req.ref.Location, target, infer.Options{
					Version:   version,
					Asset:     req.inferAsset(),
					Bins:      req.inferBins(),
					Platforms: req.platforms,
				},
			)

			return inferred.Text, err
		})

		if i == 0 && err != nil {
			first = err
		}

		if !errors.Is(err, infer.ErrNoAsset) {
			break
		}
	}

	// The host's error names the assets, unless another platform failed for a
	// reason of its own.
	if errors.Is(err, infer.ErrNoAsset) {
		err = first
	}

	if err != nil {
		return ref.Fetched{}, infer.Inferred{}, err
	}

	return ref.Fetched{Data: []byte(inferred.Text), Commit: fetched.Commit}, inferred, nil
}

// inferAt infers a manifest with write for the version that want selects. An
// inferrer reads one exact version, and a forge inferrer falls back to the
// newest release when no tag matches. inferAt picks the version from the
// versions of the first manifest, and infers again when the pick differs from
// the version the first inference read, as it does for a range or a prefix
// such as "22".
func (e env) inferAt(
	ctx context.Context,
	opts Options,
	want string,
	write func(version string) (string, error),
) (string, error) {
	if want == "" {
		return write("")
	}

	first := want
	if resolve.IsRange(want) {
		first = ""
	}

	text, err := write(first)
	if errors.Is(err, infer.ErrNoVersion) {
		first = ""
		text, err = write(first)
	}

	if err != nil {
		return "", err
	}

	m, err := manifest.Parse([]byte(text), "the inferred manifest")
	if err != nil {
		return "", err
	}

	release, err := e.resolver(opts).Pick(ctx, m.Version, want)
	if err != nil || release.Version == first {
		return text, err
	}

	return write(release.Version)
}

// isDownload reports whether a URL is the package itself and not a manifest.
// data and err are what reading it as a manifest gave. A path that ends in
// ".toml" is always a manifest, and so is a URL that does not exist, so that
// the user sees that error. Anything else is a download unless it parses as a
// manifest. Reading a download over the manifest size limit fails, so err is
// set for it.
func isDownload(r ref.Ref, data []byte, err error) bool {
	at, parseErr := url.Parse(r.Location)
	if parseErr != nil || strings.HasSuffix(at.Path, ".toml") || errors.Is(err, ref.ErrNotFound) {
		return false
	}

	if err != nil {
		return true
	}

	_, err = manifest.Parse(data, r.String())

	return err != nil
}

// inferNPM writes the manifest of an npm ref. The programs run through the
// package that [runtimes] of the list names for node, else the one config.toml
// names, else through the node on PATH.
func (e env) inferNPM(ctx context.Context, opts Options, req request) (string, error) {
	if req.asset != "" || len(req.bins) > 0 {
		return "", fmt.Errorf(
			"--asset and --bin do not apply, %s lists its download and its programs", req.ref,
		)
	}

	npmOpts := infer.NPMOptions{Registry: opts.NPMRegistry, Version: req.ref.Version}

	var err error

	npmOpts.Node, npmOpts.NodeName, err = e.runtime(ctx, opts, "node")
	if err != nil {
		return "", err
	}

	if npmOpts.Node.Ref == "" && platform.Host().OS == "windows" {
		return "", fmt.Errorf(
			"%s needs node, and Windows cannot run a script through PATH\n"+
				"set runtimes.node in %s to the ref of a package that provides node",
			req.ref, e.listPath(),
		)
	}

	text, err := e.inferrer(opts).FromNPM(ctx, req.ref.Location, npmOpts)

	return text, err
}

// fromRegistry reports whether a ref of kind names a package of a registry,
// whose manifest oku always infers.
func fromRegistry(kind ref.Kind) bool {
	return slices.Contains([]ref.Kind{ref.NPM, ref.PyPI, ref.Go, ref.Cargo}, kind)
}

// inferrerOf returns what writes the manifest of a ref of a registry, or nil
// for a ref that points at a manifest.
func (e env) inferrerOf(kind ref.Kind) func(context.Context, Options, request) (string, error) {
	return map[ref.Kind]func(context.Context, Options, request) (string, error){
		ref.NPM: e.inferNPM, ref.PyPI: e.inferPyPI, ref.Go: e.inferGo, ref.Cargo: e.inferCargo,
	}[kind]
}

// inferCargo writes the manifest of a cargo ref. The build runs the cargo of
// the package that [runtimes] names for rust, else the cargo on the user's PATH.
func (e env) inferCargo(ctx context.Context, opts Options, req request) (string, error) {
	if req.asset != "" || len(req.bins) > 0 {
		return "", fmt.Errorf("--asset and --bin do not apply, %s names its programs", req.ref)
	}

	rust, _, err := e.runtime(ctx, opts, "rust")
	if err != nil {
		return "", err
	}

	return e.inferrer(opts).FromCrates(ctx, req.ref.Location, infer.CratesOptions{
		API: opts.CratesAPI, Downloads: opts.CrateDownloads, Version: req.ref.Version, Rust: rust,
	})
}

// inferGo writes the manifest of a go ref. The build runs the go of the
// package that [runtimes] names for go, else the go on the user's PATH.
func (e env) inferGo(ctx context.Context, opts Options, req request) (string, error) {
	if req.asset != "" || len(req.bins) > 0 {
		return "", fmt.Errorf(
			"--asset and --bin do not apply, %s names its program", req.ref,
		)
	}

	goRef, _, err := e.runtime(ctx, opts, "go")
	if err != nil {
		return "", err
	}

	return e.inferrer(opts).FromGo(ctx, req.ref.Location, infer.GoOptions{
		Proxy: opts.GoProxy, Version: req.ref.Version, Go: goRef,
	})
}

// inferPyPI writes the manifest of a pypi ref. uv installs the package, and the
// programs run through the package that [runtimes] names for python, else
// through the python3 on the build's PATH.
func (e env) inferPyPI(ctx context.Context, opts Options, req request) (string, error) {
	if req.asset != "" || len(req.bins) > 0 {
		return "", fmt.Errorf(
			"--asset and --bin do not apply, %s lists its download and its programs", req.ref,
		)
	}

	python, _, err := e.runtime(ctx, opts, "python")
	if err != nil {
		return "", err
	}

	uv, _, err := e.runtime(ctx, opts, "uv")
	if err != nil {
		return "", err
	}

	return e.inferrer(opts).FromPyPI(ctx, req.ref.Location, infer.PyPIOptions{
		Index: opts.PyPIIndex, Version: req.ref.Version, Python: python, UV: uv,
	})
}

// runtime returns the package that [runtimes] names for the interpreter name,
// in the list or else in config.toml, with its version constraint, and that
// package's name. Both are empty when neither names one. The lock stores the
// manifest that holds the ref, so runtime names a package inside the list's
// directory relative to that directory. The lock then works in another checkout
// or home directory.
func (e env) runtime(ctx context.Context, opts Options, name string) (manifest.Dep, string, error) {
	// The list's refs are resolved already. A relative path in config.toml
	// starts at the directory of config.toml, not at the working directory.
	d, origin := e.runtimes[name], e.listPath()
	if d.Ref == "" {
		config, err := source.Read(e.configPath())
		if err != nil {
			return manifest.Dep{}, "", err
		}

		origin = e.configPath()

		if value, ok := config.Runtimes[name]; ok {
			if d, err = manifest.ParseDep(value); err != nil {
				return manifest.Dep{}, "", fmt.Errorf("runtimes.%s in %s: %w", name, origin, err)
			}

			if d.Ref, err = config.Expand(d.Ref); err != nil {
				return manifest.Dep{}, "", fmt.Errorf("runtimes.%s in %s: %w", name, origin, err)
			}
		}
	}

	if d.Ref == "" {
		return manifest.Dep{}, "", nil
	}

	r, err := ref.ParseIn(filepath.Dir(e.configPath()), d.Ref)
	if err != nil {
		return manifest.Dep{}, "", fmt.Errorf("runtimes.%s in %s: %w", name, origin, err)
	}

	fetched, err := e.fetcher(opts).Fetch(ctx, r, "", ref.Manifest)
	if err != nil {
		return manifest.Dep{}, "", fmt.Errorf("runtimes.%s in %s: %w", name, origin, err)
	}

	m, err := manifest.Parse(fetched.Data, r.String())
	if err != nil {
		return manifest.Dep{}, "", fmt.Errorf("runtimes.%s in %s: %w", name, origin, err)
	}

	d.Ref = ref.InDir(e.listDir(), r.String())

	return d, m.Package.Name, nil
}

// inferAsset and inferBins are the asset and the programs to infer with. A
// flag wins, and without one an update infers the way the lock recorded.
func (r request) inferAsset() string {
	if r.asset != "" {
		return r.asset
	}

	return r.previous.Asset
}

func (r request) inferBins() []string {
	if len(r.bins) > 0 {
		return r.bins
	}

	return r.previous.Bins
}

// inferredAs returns v for the lock entry of an inferred manifest, and the zero
// value for any other.
func inferredAs[T any](inferred string, req request, v T) T {
	if inferred != "" || req.previous.Inferred && req.keepVersion {
		return v
	}

	var zero T

	return zero
}

func inferredText(inferred string, req request) string {
	if inferred == "" && req.previous.Inferred && req.keepVersion {
		return req.previous.Manifest
	}

	return inferred
}

// warn prints a note the user should read but need not act on. On a terminal
// it starts with a glyph, and the lines after the first are indented under it.
func warn(w io.Writer, format string, args ...any) {
	s := ui.For(w)
	text := fmt.Sprintf(format, args...)

	if s.On() {
		text = s.Wrap(s.Note()+" "+strings.ReplaceAll(s.Code(s.Homes(text)), "\n", "\n  "), 2)
	}

	fmt.Fprintln(w, text)
}

// reportNarrowed says which platforms the package has no artifact or build
// for, and the when that list now says for it.
func reportNarrowed(w io.Writer, got installed, listPath string) {
	if len(got.unsupported) == 0 {
		return
	}

	warn(
		w, "%s has no artifact or build for %s, so its entry in %s says when = %s",
		got.lock.Name, platformNames(got.unsupported), listPath, got.when.TOML(),
	)
}

// platformNames joins the names of platforms with commas.
func platformNames(platforms []platform.Platform) string {
	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.String()
	}

	return strings.Join(names, ", ")
}

// reportUnsandboxed warns that a build's commands ran without the sandbox.
func reportUnsandboxed(w io.Writer, got installed) {
	if got.unsandboxed != "" {
		warn(
			w, "%s was built without the sandbox, because %s\n"+
				"its build commands could use the network and read your files",
			got.lock.Name, got.unsandboxed,
		)
	}
}

// reportLinks warns for each store package that a build loads and that its
// manifest does not name in runtime.deps.
func reportLinks(w io.Writer, got installed) {
	for _, note := range got.linkNotes {
		warn(w, "%s", note)
	}
}

// reportCache says which packages came from a cache and which entries oku ignored.
func reportCache(w io.Writer, got installed) {
	for _, note := range got.cacheNotes {
		warn(w, "%s", note)
	}

	for _, name := range got.substituted {
		fmt.Fprintf(w, "%s came from a cache, nothing was built\n", name)
	}
}

// reportInferred says that oku just inferred a manifest, and prints the
// manifest itself when verbose.
func reportInferred(w io.Writer, got installed, verbose bool) {
	if got.inferred == "" {
		return
	}

	why, _ := inferredWhy(got)

	if verbose {
		fmt.Fprintf(w, "%s %s:\n\n%s\n", got.lock.Ref, why, got.inferred)
	} else {
		warn(w, "%s %s, --verbose prints it", got.lock.Ref, why)
	}

	reportNodeRuntime(w, got)
}

// inferredWhy says where oku took an inferred manifest from, for one package
// and for several.
func inferredWhy(got installed) (one, many string) {
	switch {
	case strings.HasPrefix(got.lock.Ref, "npm:"):
		return "is an npm package, so oku inferred a manifest from the registry",
			"are npm packages, so oku inferred their manifests from the registry"
	case strings.HasPrefix(got.lock.Ref, "http"):
		return "is a download and no manifest, so oku inferred one from it",
			"are downloads and no manifests, so oku inferred one from each"
	}

	return "has no manifest, so oku inferred one from its newest release",
		"have no manifest, so oku inferred one for each from its newest release"
}

// reportInferredTogether is reportInferred for the packages of one sync on a
// terminal: one note for each source of manifests, which names the packages.
func reportInferredTogether(w io.Writer, all []installed) {
	var (
		order  []string
		groups = map[string][]installed{}
	)

	for _, got := range all {
		if got.inferred == "" {
			continue
		}

		one, _ := inferredWhy(got)
		if _, ok := groups[one]; !ok {
			order = append(order, one)
		}

		groups[one] = append(groups[one], got)
	}

	for _, one := range order {
		group := groups[one]
		if len(group) == 1 {
			reportInferred(w, group[0], false)

			continue
		}

		names := make([]string, len(group))
		for i, got := range group {
			names[i] = got.lock.Name
		}

		_, many := inferredWhy(group[0])
		warn(w, "%d packages %s: %s, --verbose prints them", len(group), many, strings.Join(names, ", "))

		for _, got := range group {
			reportNodeRuntime(w, got)
		}
	}
}

// reportNodeRuntime warns that an npm package runs the node on PATH.
func reportNodeRuntime(w io.Writer, got installed) {
	if strings.HasPrefix(got.lock.Ref, "npm:") && !strings.Contains(got.inferred, "[runtime]") {
		warn(
			w, "its programs run the node on PATH. To pin one, set runtimes.node in config.toml "+
				"to the ref of a package that provides node",
		)
	}
}

// reportFirstUse tells the user that oku trusted a download unverified.
func (e env) reportFirstUse(w io.Writer, got installed) {
	if len(got.firstUseOthers) > 0 {
		warn(
			w,
			"%s publishes no checksum for %s, so oku trusted those downloads and pinned them in %s",
			got.lock.Name,
			strings.Join(got.firstUseOthers, ", "),
			e.lockPath(),
		)
	}

	if !got.firstUse {
		return
	}

	// A terminal gets the start of the checksum, which is enough to compare.
	sum := got.lock.Platforms[platform.Host().String()].SHA256
	if ui.For(w).On() && len(sum) > 12 {
		sum = sum[:12]
	}

	warn(
		w,
		"%s publishes no checksum, so oku trusted this download and pinned sha256 %s in %s",
		got.lock.Name, sum, e.lockPath(),
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
	// runtimes holds, for each dep in the order of prefixes, the dep and the
	// store paths it needs at run time.
	runtimes [][]string
	// substituted, cacheNotes and linkNotes collect what the deps report, see
	// installed.
	substituted []string
	cacheNotes  []string
	linkNotes   []string
}

// installDeps installs the deps of the package in parent, each through the same
// pipeline, so a dep may be an artifact or a build and may have deps of its own.
// at is where the parent's manifest was read. A relative dep of a manifest in a
// repo or at a URL is the file beside it there, read at the same commit.
func (e env) installDeps(
	ctx context.Context,
	opts Options,
	parent request,
	at ref.Fetched,
	wanted []manifest.Dep,
) (depSet, error) {
	var set depSet

	base := ""

	switch parent.ref.Kind {
	case ref.File:
		base = filepath.Dir(parent.ref.Location)
	case ref.NPM, ref.PyPI, ref.Go, ref.Cargo:
		// These infer a manifest that names its runtime relative to the list.
		base = e.listDir()
	}

	remote := parent.ref.Kind == ref.Forge || parent.ref.Kind == ref.Git ||
		parent.ref.Kind == ref.HTTP

	for _, dep := range wanted {
		beside := remote && ref.IsRelative(dep.Ref)

		var (
			r   ref.Ref
			err error
		)

		if beside {
			r, err = ref.Beside(parent.ref, at, dep.Ref)
		} else {
			r, err = ref.ParseIn(base, dep.Ref)
		}

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

		switch {
		case keep:
			commit, wantManifest = previous.Commit, previous.ManifestSHA256
		case beside:
			commit = at.Commit
		}

		// Each package pins its own deps, so install reuses a dep only when the
		// ref, the constraint and the lock entry all match.
		key := fmt.Sprintf(
			"%s %s %v %v %v %v %v", r, dep.Version, keep, parent.acceptDigest, previous,
			parent.platforms, parent.lockOnly,
		)

		got, err := parent.deps.do(parent.root, key, func() (installed, error) {
			return e.install(ctx, opts, request{
				ref:             r,
				commit:          commit,
				previous:        previous,
				wantManifest:    wantManifest,
				acceptDigest:    parent.acceptDigest,
				acceptKey:       parent.acceptKey,
				keepVersion:     keep,
				platforms:       parent.platforms,
				strictPlatforms: parent.strictPlatforms,
				lockOnly:        parent.lockOnly,
				approve:         parent.approve,
				log:             parent.log,
				constraint:      dep.Version,
				stack:           stack,
				root:            parent.root,
				deps:            parent.deps,
			})
		})
		if err != nil {
			return set, fmt.Errorf("dep %s: %w", r, err)
		}

		set.prefixes = append(
			set.prefixes,
			store.Dep{Name: got.lock.Name, Prefix: got.profile.StorePath},
		)
		set.locks = append(set.locks, got.lock)
		set.substituted = append(set.substituted, got.substituted...)
		set.cacheNotes = append(set.cacheNotes, got.cacheNotes...)
		set.linkNotes = append(set.linkNotes, got.linkNotes...)

		for _, path := range got.closure {
			if !slices.Contains(set.closure, path) {
				set.closure = append(set.closure, path)
			}
		}

		var runtime []string

		for _, path := range got.closure {
			if !slices.Contains(got.profile.BuildOnly, path) {
				runtime = append(runtime, path)
			}
		}

		set.runtimes = append(set.runtimes, runtime)
	}

	return set, nil
}

// buildOnly returns the paths of set.closure that no dep from the index first
// on needs at run time. The deps before first are build deps.
func (set depSet) buildOnly(first int) []string {
	var runtime []string
	for _, paths := range set.runtimes[first:] {
		runtime = append(runtime, paths...)
	}

	var only []string

	for _, path := range set.closure {
		if !slices.Contains(runtime, path) {
			only = append(only, path)
		}
	}

	return only
}
