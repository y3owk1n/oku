package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/source"
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
	// substituted reports that the package came from a cache, and cacheNotes
	// lists the cache entries oku ignored. Both cover the deps too.
	substituted []string
	cacheNotes  []string
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
	// acceptKey lets the manifest's signing key differ from the one pinned in
	// previous. "--accept-key" sets it.
	acceptKey bool
	// keepVersion installs the version in previous without listing versions
	// again. "oku sync" sets it.
	keepVersion bool
	// asset and bin name the asset and the program for an inferred manifest.
	// "--asset" and "--bin" set them.
	asset, bin string
	// service enables the package's services.
	service bool
	// system puts the package's apps, fonts and services in system scope.
	system bool
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

	// A pin in the list decides the version. Without one, sync stays on the locked
	// version, and add and update take the newest.
	release := resolve.Release{
		Version: previous.Version, Tag: previous.Tag, Commit: previous.TagCommit,
	}

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

	m.Version.Value, m.Tag, m.TagCommit = release.Version, release.Tag, release.Commit

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
	if at := previous.Platforms[host.String()]; previous.Version == m.Version.Value &&
		at.Strategy == strategyBuild && !req.acceptDigest {
		pinnedSource = at.SHA256
	}

	if build {
		realized.Path = e.store().BuildPath(m, host, deps.prefixes)

		var notes []string
		if cached, notes, err = e.substitute(ctx, realized.Path); err != nil {
			return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
		}

		deps.cacheNotes = append(deps.cacheNotes, notes...)
	}

	switch {
	case cached:
		// A package from a cache runs none of the manifest's commands, so it needs
		// no approval. A cache never holds an impure package.
		deps.substituted = append(deps.substituted, m.Package.Name)
		entry = lock.Platform{Strategy: strategyBuild, VendorSHA256: pinnedVendor}

		if at := previous.Platforms[host.String()]; pinnedSource != "" {
			entry.URL, entry.SHA256 = at.URL, at.SHA256
		}
	case build:
		if err := req.approve(m, host); err != nil {
			return installed{}, err
		}

		realized, err = e.store().Build(ctx, m, host, store.BuildOptions{
			Deps: deps.prefixes, Log: req.log, PinnedVendor: pinnedVendor, Progress: req.progress,
			NPMRegistry: opts.NPMRegistry, PinnedSource: pinnedSource,
		})
		if err != nil {
			return installed{}, fmt.Errorf("%s: %w", m.Package.Name, err)
		}

		entry = lock.Platform{
			Strategy: strategyBuild, Impure: realized.Impure, VendorSHA256: realized.VendorSHA256,
			URL: realized.SourceURL, SHA256: realized.SHA256,
		}

		// A package that was in the store already was not downloaded again, so the
		// pin it had stays.
		if at := previous.Platforms[host.String()]; realized.SHA256 == "" && pinnedSource != "" {
			entry.URL, entry.SHA256 = at.URL, at.SHA256
		}
	default:
		// A digest that oku.lock pinned for this version and URL still applies,
		// even when the manifest gives none.
		pinned := ""
		if at := previous.Platforms[host.String()]; previous.Version == m.Version.Value &&
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

		if realized, err = e.store().As(auth).Realize(
			ctx, m, artifact, host, pinned, deps.prefixes,
		); err != nil {
			return installed{}, err
		}

		entry = lock.Platform{
			Strategy: strategyArtifact,
			URL:      artifact.URL,
			SHA256:   realized.SHA256,
		}
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
			Env:       env,
			Service:   req.service,
			System:    req.system,
		},
		lock: lock.Package{
			Name:           m.Package.Name,
			Ref:            r.String(),
			Commit:         fetched.Commit,
			ManifestSHA256: m.SHA256,
			Version:        m.Version.Value,
			SigningKey:     m.Package.SigningKey,
			Tag:            tagFor(m),
			TagCommit:      m.TagCommit,
			Inferred:       inferred != "" || req.previous.Inferred && req.keepVersion,
			Manifest:       inferredText(inferred, req),
			Platforms:      platforms,
			Deps:           deps.locks,
		},
		closure:     append([]string{realized.Path}, deps.closure...),
		firstUse:    realized.FirstUse,
		inferred:    inferred,
		unsandboxed: realized.Unsandboxed,
		substituted: deps.substituted,
		cacheNotes:  deps.cacheNotes,
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

	if req.ref.Kind == ref.NPM {
		text, err := e.inferNPM(ctx, opts, req)

		return ref.Fetched{Data: []byte(text)}, text, err
	}

	fetched, err := e.fetcher(opts).Fetch(ctx, req.ref, req.commit, ref.Manifest)

	if req.ref.Kind == ref.HTTP && isDownload(req.ref, fetched.Data, err) {
		if req.asset != "" {
			return fetched, "", fmt.Errorf("--asset does not apply, %s is the asset", req.ref)
		}

		text, err := e.inferrer(opts).FromURL(ctx, req.ref.Location, platform.Host(), req.bin)
		if err != nil {
			return ref.Fetched{}, "", err
		}

		return ref.Fetched{Data: []byte(text)}, text, nil
	}

	if err == nil && (req.asset != "" || req.bin != "") {
		return fetched, "", fmt.Errorf(
			"--asset and --bin apply when oku infers a manifest, and %s has one", req.ref,
		)
	}

	if err == nil || !errors.Is(err, ref.ErrNotFound) ||
		req.ref.Kind != ref.Forge || req.ref.Fragment != "" {
		return fetched, "", err
	}

	text, err := e.inferrer(opts).Manifest(
		ctx, req.ref.Scheme, req.ref.Location, platform.Host(), infer.Options{
			Version: req.ref.Version,
			Asset:   req.asset,
			Bin:     req.bin,
		})
	if err != nil {
		return ref.Fetched{}, "", err
	}

	return ref.Fetched{Data: []byte(text), Commit: fetched.Commit}, text, nil
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
// package that config.toml names for node, or through the node on PATH.
func (e env) inferNPM(ctx context.Context, opts Options, req request) (string, error) {
	if req.asset != "" || req.bin != "" {
		return "", fmt.Errorf(
			"--asset and --bin do not apply, %s lists its download and its programs", req.ref,
		)
	}

	config, err := source.Read(e.configPath())
	if err != nil {
		return "", err
	}

	npmOpts := infer.NPMOptions{Registry: opts.NPMRegistry, Version: req.ref.Version}

	if node := config.Runtimes["node"]; node != "" {
		// A relative path in config.toml starts at the directory of config.toml,
		// not at the working directory.
		node, err := config.Expand(node)
		if err != nil {
			return "", fmt.Errorf("runtimes.node in %s: %w", e.configPath(), err)
		}

		r, err := ref.ParseIn(filepath.Dir(e.configPath()), node)
		if err != nil {
			return "", fmt.Errorf("runtimes.node in %s: %w", e.configPath(), err)
		}

		fetched, err := e.fetcher(opts).Fetch(ctx, r, "", ref.Manifest)
		if err != nil {
			return "", fmt.Errorf("runtimes.node in %s: %w", e.configPath(), err)
		}

		m, err := manifest.Parse(fetched.Data, r.String())
		if err != nil {
			return "", fmt.Errorf("runtimes.node in %s: %w", e.configPath(), err)
		}

		npmOpts.Node, npmOpts.NodeName = r.String(), m.Package.Name
	} else if platform.Host().OS == "windows" {
		return "", fmt.Errorf(
			"%s needs node, and Windows cannot run a script through PATH\n"+
				"set runtimes.node in %s to the ref of a package that provides node",
			req.ref, e.configPath(),
		)
	}

	text, err := e.inferrer(opts).FromNPM(ctx, req.ref.Location, npmOpts)

	// The build runs npm through sh. The npm.cmd of a Windows node package also
	// looks for its files in its own directory, and the store's bin holds a copy
	// of npm.cmd without them.
	if err == nil && platform.Host().OS == "windows" && strings.Contains(text, "\n[build]\n") {
		return "", fmt.Errorf(
			"%s lists dependencies, and oku cannot install those on Windows yet", req.ref,
		)
	}

	return text, err
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

// reportCache says which packages came from a cache and which entries oku ignored.
func reportCache(w io.Writer, got installed) {
	for _, note := range got.cacheNotes {
		fmt.Fprintln(w, note)
	}

	for _, name := range got.substituted {
		fmt.Fprintf(w, "%s came from a cache, nothing was built\n", name)
	}
}

// reportInferred prints a manifest that oku just inferred.
func reportInferred(w io.Writer, got installed) {
	if got.inferred == "" {
		return
	}

	if strings.HasPrefix(got.lock.Ref, "npm:") {
		fmt.Fprintf(
			w, "%s is an npm package, so oku inferred this manifest from the registry:\n\n%s\n",
			got.lock.Ref, got.inferred,
		)

		if !strings.Contains(got.inferred, "[runtime]") {
			fmt.Fprintln(
				w, "its programs run the node on PATH. To pin one, set runtimes.node in config.toml "+
					"to the ref of a package that provides node",
			)
		}

		return
	}

	if strings.HasPrefix(got.lock.Ref, "http") {
		fmt.Fprintf(
			w, "%s is a download and no manifest, so oku inferred this one from it:\n\n%s\n",
			got.lock.Ref, got.inferred,
		)

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
	// substituted and cacheNotes collect what the deps report, see installed.
	substituted []string
	cacheNotes  []string
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

		// The node of an npm ref comes from the user's own config.toml.
		if base == "" && r.Kind == ref.File && parent.ref.Kind != ref.NPM {
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
			acceptKey:    parent.acceptKey,
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
		set.substituted = append(set.substituted, got.substituted...)
		set.cacheNotes = append(set.cacheNotes, got.cacheNotes...)

		for _, path := range got.closure {
			if !slices.Contains(set.closure, path) {
				set.closure = append(set.closure, path)
			}
		}
	}

	return set, nil
}
