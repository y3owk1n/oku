// Package infer writes a manifest for a repo that has none, from the assets of
// its newest release.
package infer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	xdg "github.com/y3owk1n/oku/internal/desktop"
	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/platform"
)

// Inspector downloads an asset and reports the files in it. The store provides
// it, so the download goes into oku's cache. auth is the login for the host of a
// private repo.
type Inspector func(ctx context.Context, url string, auth forge.Auth) ([]File, error)

// File is one regular file of an unpacked asset. Text holds the text of a
// Linux desktop entry or of an app bundle's Info.plist, GUI marks a Windows
// program that opens no console, and Setup a Windows setup program.
type File struct {
	Path       string
	Executable bool
	Text       string
	GUI        bool
	Setup      bool
}

var errNoRelease = errors.New("has no release")

// ErrNoVersion reports that a registry has no version the user asked for.
var ErrNoVersion = errors.New("has no version")

// ErrWebPage reports a download that is a web page, such as the page of a
// repo, where oku wanted a file to install.
var ErrWebPage = errors.New("a web page")

// ErrNoAsset reports that a release has no asset for the platform asked for.
var ErrNoAsset = errors.New("no release asset fits")

// ErrNeedsNPM reports an npm package that lists dependencies while no node
// package provides the npm that installs them.
var ErrNeedsNPM = errors.New("and only the npm of a node package installs them")

// Inferrer reads releases from a forge.
type Inferrer struct {
	Hosts   forge.Hosts
	Inspect Inspector
	// Checksum reads the sha256 of fileName from the checksum file at url.
	Checksum func(ctx context.Context, url, fileName string, auth forge.Auth) (string, error)
	// Download reads a small file at url, such as a signature.
	Download func(ctx context.Context, url string, auth forge.Auth) ([]byte, error)
}

// Options set the release, the asset and the program Manifest uses. With the
// zero value it reads the newest release and chooses the other two.
type Options struct {
	// Version is the release to read, the way the user wrote it after "@".
	Version string
	// Asset is a glob that names the asset for the host.
	Asset string
	// Bins are the file names of the programs inside the assets, or none to let
	// Manifest find them.
	Bins []string
	// Platforms are the ones the lock pins besides host. Manifest opens an asset
	// for those and for host, and for no other platform.
	Platforms []platform.Platform
	// Name is the package's name in the list, which the manifest takes. Without
	// it the manifest is named after its program.
	Name string
}

// target is one platform the inferred manifest may cover. A linux target with an
// empty libc takes an asset that names no libc.
type target struct {
	platform.Selector

	// fat are the words of a build that runs on every arch of the OS.
	words struct{ os, arch, fat, libc []string }
}

var (
	osWords = map[string][]string{
		"linux":   {"linux"},
		"darwin":  {"darwin", "macos", "macosx", "apple", "osx", "mac"},
		"windows": {"windows", "win64", "win32", "win"},
	}
	archWords = map[string][]string{
		"amd64":   {"x86_64", "x86-64", "amd64", "x64", "64bit", "64-bit"},
		"arm64":   {"aarch64", "arm64"},
		"386":     {"i386", "i686", "386", "ia32", "32bit", "32-bit"},
		"arm":     {"armv7", "armv7l", "armhf", "arm"},
		"riscv64": {"riscv64"},
	}
	fatWords = map[string][]string{
		"darwin": {"universal", "universal2", "all"},
	}
	libcWords = map[string][]string{
		"glibc": {"gnu", "glibc", "gnueabi", "gnueabihf"},
		"musl":  {"musl", "musleabi", "musleabihf"},
	}
	// otherArches are words of arches oku has no target for. An asset that
	// names one is no build for any arch, whatever its format.
	otherArches = []string{
		"ppc64le", "ppc64", "powerpc64le", "powerpc64", "powerpc", "s390x", "loong64", "loongarch64",
		"mips", "mipsel", "mipsle", "mips64", "mips64el", "mips64le", "sparc64", "m68k", "i586",
		"armv5", "armv5te", "armv6", "armv6l", "armel",
	}
	// fillers are words of a platform triple that say nothing about the build,
	// as "unknown" in "x86_64-unknown-linux-gnu".
	fillers = []string{"unknown", "pc", "msvc", "exe", "eabi", "eabihf"}
	// unpackable are the archive endings oku can unpack. A name with no known
	// ending is taken as a single binary, which is what an AppImage is.
	unpackable = []string{
		".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar.zst", ".zip", ".7z",
		".tar", ".deb", ".rpm", ".msi", ".dmg", ".pkg",
	}
	// installers are the endings that rank after a single binary. A .pkg ranks
	// after them. oku can open a .dmg or a .pkg on macOS only and an .msi on
	// Windows only, so inference for another platform leaves them out when it
	// cannot read them.
	installers = []string{".deb", ".rpm", ".msi", ".dmg"}
	// skipped are endings of files that are not the package itself, or that oku
	// cannot unpack.
	skipped = []string{
		".sha256", ".sha256sum", ".sha256sums", ".sha512", ".sha512sum", ".sha1", ".shasum", ".md5", ".md5sum",
		".sig", ".asc", ".pem", ".sbom", ".json",
		".txt", ".apk", ".minisig", ".crt", ".cert", ".bundle", ".intoto.jsonl", ".vsix", ".delta",
	}
	// signatures are endings of files that sign or describe a checksum file.
	signatures = []string{".sig", ".asc", ".pem", ".minisig", ".crt", ".sbom", ".json"}
)

// choice is one artifact of the inferred manifest.
type choice struct {
	platform.Selector

	asset string
	// others are the assets that fit as well as asset does.
	others []string
}

// pinned reports whether one of platforms takes c's artifact.
func (c choice) pinned(platforms []platform.Platform) bool {
	return slices.ContainsFunc(platforms, c.Matches)
}

// Inferred is a manifest oku wrote from a release.
type Inferred struct {
	// Text is the manifest TOML.
	Text string
	// Asset is the release asset the host's artifact downloads, and Others are
	// the assets that fit the host as well, which --asset may pick instead.
	Asset  string
	Others []string
	// Bins are the programs of the host's artifact, and Found the other
	// programs beside the first, which --bin may add.
	Bins  []string
	Found []string
	// Tag is the tag of the release the manifest was inferred from.
	Tag string
}

// Manifest returns the manifest inferred for the repo that a forge ref's
// scheme and location name. It needs an asset for host, because it opens that
// asset to find the executable.
func (inf *Inferrer) Manifest(
	ctx context.Context,
	scheme, location string,
	host platform.Platform,
	opts Options,
) (Inferred, error) {
	server, repo, err := inf.Hosts.Open(scheme, location)
	if err != nil {
		return Inferred{}, err
	}

	rel, err := wanted(ctx, server, repo, opts.Version)
	if err != nil {
		return Inferred{}, err
	}

	names := make([]string, len(rel.Assets))
	urls := map[string]string{}
	sizes := map[string]int64{}
	digests := map[string]string{}

	for i, asset := range rel.Assets {
		names[i] = asset.Name
		urls[asset.Name] = asset.URL
		sizes[asset.Name] = asset.Size
		digests[asset.Name] = asset.Digest
	}

	program := strings.ToLower(path.Base(repo))
	repoProgram := program

	chosen, picked, err := choose(names, sizes, program, host, opts.Asset)
	if err != nil {
		return Inferred{}, err
	}

	// The asset --asset picks may hold another program of the repo, as
	// tool-server of tool. Any other name stays the repo's, so the command of
	// a package added before keeps its name.
	if rest, ok := strings.CutPrefix(picked, program); ok && rest != "" && !isAlnum(rest[0]) {
		program = picked
	}

	// A repo's name may add a suffix to its program's, as "tool.zig" or
	// "tool-rs". When the host's asset is named after the part before it, that
	// part is the program.
	if i := slices.IndexFunc(chosen, func(c choice) bool { return c.Matches(host) }); i >= 0 && opts.Asset == "" {
		if short := stem(chosen[i].asset); short != "" && len(short) < len(program) &&
			strings.HasPrefix(program, short) && !isAlnum(program[len(short)]) {
			program = short
		}
	}

	name := cmp.Or(opts.Name, program)

	// Every asset may be named after a program whose name is not the repo's,
	// as shfmt of mvdan/sh or nvim of neovim/neovim. The package keeps the
	// repo's name, and oku looks for that program first.
	if shared := sharedStem(chosen); opts.Asset == "" && shared != "" {
		program = shared
	}

	result := Inferred{Tag: rel.Tag}

	if i := slices.IndexFunc(chosen, func(c choice) bool { return c.Matches(host) }); i >= 0 {
		result.Asset, result.Others = chosen[i].asset, chosen[i].others
	} else {
		// --asset picks a file for the host, so a [lock] platform needs another way.
		fix := "name one with --asset"
		if host != platform.Host() {
			fix = "leave " + host.String() + " out of the package's when, or write a manifest for it"
		}

		has := "has: " + strings.Join(names, ", ")
		if len(names) == 0 {
			has, fix = "has no assets", "name an older release with @<version>, or write a manifest for it"
		}

		return Inferred{}, fmt.Errorf(
			"%w %s\nrelease %s of %s %s\n%s",
			ErrNoAsset, Machine(host), rel.Tag, repo, has, fix,
		)
	}

	// The version starts at the tag's first digit, so "v1.2.0" and "jq-1.8.1" both
	// work. A tag without a digit is kept whole.
	prefix, version := "", rel.Tag
	if i := strings.IndexAny(rel.Tag, "0123456789"); i > 0 {
		prefix, version = rel.Tag[:i], rel.Tag[i:]
	}

	// Assets of one OS with the same ending come from the same packaging step, so
	// oku opens one of them for all. A zip for Windows is often laid out unlike
	// the tar archives next to it, and a zip for macOS may add an app bundle.
	// oku opens an asset for the host and the lock platforms only, and a
	// platform outside the lock gets an artifact when its asset has the ending
	// of one it opened anyway.
	layouts := map[string]layout{}
	anyOS := map[string]layout{}
	hostDone := false

	for _, c := range chosen {
		isHost := c.Matches(host) && !hostDone
		kind := ending(c.asset)

		if _, known := layouts[kind+" "+c.OS]; known || !isHost && !c.pinned(opts.Platforms) {
			continue
		}

		l, err := inf.layoutOf(ctx, server.Auth(), urls[c.asset], c.asset, versionless(c.asset, version),
			[]string{program, repoProgram}, opts.Bins)

		switch {
		case err != nil && isHost && len(c.others) > 0:
			return Inferred{}, fmt.Errorf(
				"%s: %w\nthese assets fit %s too: %s\nchoose one with --asset",
				c.asset, err, Machine(host), strings.Join(c.others, ", "),
			)
		case err != nil && isHost:
			return Inferred{}, fmt.Errorf("%s: %w", c.asset, err)
		case err != nil:
			// oku leaves out a platform whose asset it cannot read. A wrong
			// artifact would fail on that platform at install.
			continue
		}

		layouts[kind+" "+c.OS] = l

		if _, known := anyOS[kind]; !known {
			anyOS[kind] = l
		}

		if isHost {
			hostDone = true
		}
	}

	if i := slices.IndexFunc(chosen, func(c choice) bool { return c.Matches(host) }); i >= 0 {
		l := layouts[ending(chosen[i].asset)+" "+chosen[i].OS]
		result.Found = l.found

		for _, bin := range l.bins {
			result.Bins = append(result.Bins, strings.ToLower(path.Base(bin)))
		}
	}

	// The asset --asset picks may name a build of the program, as
	// tool-portable, rather than another program. The package keeps the repo's
	// name unless the asset holds a program of the other name.
	if i := slices.IndexFunc(chosen, func(c choice) bool { return c.Matches(host) }); i >= 0 &&
		opts.Name == "" && program != repoProgram {
		l := layouts[ending(chosen[i].asset)+" "+chosen[i].OS]
		if !slices.ContainsFunc(l.bins, func(bin string) bool { return strings.ToLower(path.Base(bin)) == program }) {
			name = repoProgram
		}
	}

	var b strings.Builder

	// version.repo names the host, which a "codeberg:" ref leaves out.
	versionRepo := repo
	if server.Host() != "" {
		versionRepo = server.Host() + "/" + repo
	}

	type artifact struct {
		choice
		layout
	}

	var artifacts []artifact

	for _, c := range chosen {
		kind := ending(c.asset)

		l, known := layouts[kind+" "+c.OS]
		if !known {
			// An app bundle is macOS's, so the layout of another OS gives none.
			// An exe with no Windows word may be a setup program, and only its
			// contents show that.
			l, known = anyOS[kind]
			l.app = nil

			if !known || len(l.bins) == 0 || exeByFormat(c.asset) {
				continue
			}
		}

		// When an asset's name holds the version, a program named after it has
		// another path on each release, and a bin path takes no variables.
		if l.named != "" && versionless(c.asset, version) == "" {
			continue
		}

		artifacts = append(artifacts, artifact{c, l})
	}

	// A release on github.com may carry Sigstore signatures of its files.
	signed := server.Kind() == forge.KindGitHub && server.Host() == ""

	assets := make([]string, 0, len(artifacts)+1)
	if signed {
		assets = append(assets, result.Asset)
	}

	for _, a := range artifacts {
		if !slices.Contains(assets, a.asset) {
			assets = append(assets, a.asset)
		}
	}

	sumsOf := inf.checksumFiles(ctx, server.Auth(), names, urls, digests, assets)

	var sign signing
	if signed {
		sign = inf.signaturesOf(ctx, server.Auth(), repo, names, urls, result.Asset, sumsOf[result.Asset],
			digests[result.Asset])
	}

	fmt.Fprintf(&b, "[package]\nname = %q\nhomepage = %q\n%s\n", name, server.Home(repo), sign.packageTOML())
	fmt.Fprintf(&b, "[version]\nfrom = %q\nrepo = %q\n", server.Kind()+"-releases", versionRepo)

	if prefix != "" {
		fmt.Fprintf(&b, "strip_prefix = %q\n", prefix)
	}

	for _, a := range artifacts {
		c, l := a.choice, a.layout

		b.WriteString("\n")
		fmt.Fprintf(&b, "[[artifact]]\nmatch = %s\n", selectorTOML(c.Selector))
		fmt.Fprintf(&b, "url = %q\n", template(urls[c.asset], rel.Tag, version))

		sums := sumsOf[c.asset]
		if sums != "" {
			fmt.Fprintf(&b, "sha256_url = %q\n", template(urls[sums], rel.Tag, version))
		}

		b.WriteString(sign.artifactTOML(names, c.asset, sums, func(name string) string {
			return template(urls[name], rel.Tag, version)
		}))

		b.WriteString(l.toml(c.OS, versionless(c.asset, version)))
	}

	result.Text = b.String()

	return result, nil
}

// toml writes the strip, bin, app and man lines of a layout for an artifact of
// os. file is the name of the program's file when a program is named after
// the artifact's asset, as "gdu_darwin_arm64" is of "gdu_darwin_arm64.tgz".
// A desktop entry is Linux's, so another OS gets none.
func (l layout) toml(os, file string) string {
	var b strings.Builder

	if l.strip > 0 {
		fmt.Fprintf(&b, "strip = %d\n", l.strip)
	}

	if len(l.bins) > 0 {
		entries := make([]string, len(l.bins))

		exe := ""
		if os == "windows" {
			exe = ".exe"
		}

		for i, bin := range l.bins {
			if i == 0 && l.named != "" {
				p := path.Join(path.Dir(bin), strings.TrimSuffix(file, ".exe")) + exe
				entries[i] = fmt.Sprintf("{ name = %q, path = %q }", l.named+exe, p)

				continue
			}

			entries[i] = fmt.Sprintf("%q", bin+exe)
		}

		fmt.Fprintf(&b, "bin = [%s]\n", strings.Join(entries, ", "))
	}

	apps := slices.DeleteFunc(slices.Clone(l.app), func(app string) bool {
		return os != "linux" && strings.HasSuffix(app, ".desktop")
	})
	if len(apps) > 0 {
		fmt.Fprintf(&b, "app = [%s]\n", quoteAll(apps))
	}

	if len(l.man) > 0 {
		fmt.Fprintf(&b, "man = [%s]\n", quoteAll(l.man))
	}

	return b.String()
}

// layoutOf opens an asset and finds the programs in it. file is the name of a
// program's file named after the asset, or "". names are the names the
// package's program may have, in the order oku looks for them.
func (inf *Inferrer) layoutOf(
	ctx context.Context,
	auth forge.Auth,
	url, asset, file string,
	names, bins []string,
) (layout, error) {
	files, err := inf.Inspect(ctx, url, auth)
	if err != nil {
		return layout{}, fmt.Errorf("inspect it: %w", err)
	}

	var first error

	for _, name := range names {
		l, err := findLayout(files, name, file, bins, isArchive(asset))
		if err == nil {
			return l, nil
		}

		first = cmp.Or(first, err)
	}

	return layout{}, first
}

// sharedStem returns the program every archive and single binary of chosen is
// named after, or "" when they name more than one or none. An installer is
// named after the distro's package, so its name does not count.
func sharedStem(chosen []choice) string {
	shared := ""

	for _, c := range chosen {
		if installerOS(strings.ToLower(c.asset)) != "" {
			continue
		}

		s := stem(c.asset)
		if s == "" || shared != "" && s != shared {
			return ""
		}

		shared = s
	}

	return shared
}

// versionless returns the name of the file an archive asset would hold for a
// program named after it, as "yq_darwin_arm64" of "yq_darwin_arm64.tar.gz".
// It returns "" for a single binary, and for a name that holds the version,
// since a bin path takes no variables.
func versionless(asset, version string) string {
	file := strings.TrimSuffix(asset, ending(asset))
	if !isArchive(asset) || strings.Contains(file, version) {
		return ""
	}

	return file
}

// choose lists the artifacts to write, in the order of targets, and returns the
// program they hold. sizes holds the bytes of each asset, or 0 when the host
// does not say. pkg is the program to prefer. A glob that names one asset
// takes it for the host. One that names more takes the best of them on every
// platform. The program is then the one the host's asset is named after, and
// the other platforms prefer its assets.
func choose(
	names []string,
	sizes map[string]int64,
	pkg string,
	host platform.Platform,
	glob string,
) ([]choice, string, error) {
	var chosen []choice

	if glob != "" {
		var named []string

		for _, name := range names {
			ok, err := path.Match(glob, name)
			if err != nil {
				return nil, "", fmt.Errorf("--asset %q: %w", glob, err)
			}

			if ok {
				named = append(named, name)
			}
		}

		// A glob that names several assets limits every platform to them.
		all := names
		if len(named) > 1 {
			names, named = named, nil

			for _, t := range targets() {
				if fits := pick(names, sizes, pkg, t); t.Matches(host) && len(fits) > 0 {
					named = fits[:1]

					break
				}
			}
		}

		if len(named) != 1 {
			return nil, "", fmt.Errorf(
				"--asset %q names %d assets for %s, want one of: %s",
				glob, len(named), Machine(host), strings.Join(all, ", "),
			)
		}

		pkg = cmp.Or(stem(named[0]), pkg)

		chosen = append(chosen, choice{
			Selector: platform.Selector(host),
			asset:    named[0],
		})
	}

	written := map[platform.Selector]bool{}

	for _, t := range targets() {
		fits := pick(names, sizes, pkg, t)
		if len(fits) == 0 || written[t.Selector] || glob != "" && t.Matches(host) {
			continue
		}

		written[t.Selector] = true

		// A universal build beside one for the arch is the same program, so it is
		// no alternative. Another format is, such as a .pkg beside a .dmg.
		others := slices.DeleteFunc(slices.Clone(fits[1:]), func(other string) bool {
			return t.fat(other) != t.fat(fits[0])
		})

		chosen = append(chosen, choice{Selector: t.Selector, asset: fits[0], others: others})
	}

	// Another program of the release is no build of this package. When some
	// platform has an asset of pkg, a platform with only another program's
	// gets no artifact, unless --asset picked it. An installer such as a .deb
	// is named after the distro's package, so its name says nothing.
	if slices.ContainsFunc(chosen, func(c choice) bool { return sibling(c.asset, pkg) == 0 }) {
		chosen = slices.DeleteFunc(chosen, func(c choice) bool {
			return sibling(c.asset, pkg) == 1 && installerOS(strings.ToLower(c.asset)) == "" &&
				(glob == "" || c.Selector != platform.Selector(host))
		})
	}

	return chosen, pkg, nil
}

// stem returns the part of an asset's name before its version or platform, as
// "tool-server" of "tool-server-x86_64-apple-darwin.tar.gz", or "" when the
// name starts with one.
func stem(asset string) string {
	lower := strings.ToLower(asset)

	for i := 1; i < len(lower); i++ {
		if !strings.ContainsRune("-_.", rune(lower[i-1])) {
			continue
		}

		if startsWithTag(lower[i:]) {
			return lower[:i-1]
		}
	}

	return ""
}

// startsWithTag reports whether name starts with a version or a platform word.
func startsWithTag(name string) bool {
	version := strings.TrimPrefix(name, "v")
	if version != "" && version[0] >= '0' && version[0] <= '9' {
		return true
	}

	for _, groups := range []map[string][]string{osWords, archWords, fatWords, libcWords} {
		for _, words := range groups {
			for _, word := range words {
				after, ok := strings.CutPrefix(name, word)
				if ok && (after == "" || !isAlnum(after[0])) {
					return true
				}
			}
		}
	}

	return false
}

// wanted returns the release for version, or the newest one for "". It tries
// version as a tag and as a "v" tag. With any other prefix it returns the newest
// release, whose asset names are the best guess there is.
func wanted(ctx context.Context, server forge.Forge, repo, version string) (forge.Release, error) {
	if version != "" {
		for _, tag := range []string{version, "v" + version} {
			rel, err := release(ctx, server, repo, tag)
			if !errors.Is(err, errNoRelease) {
				return rel, err
			}
		}
	}

	return release(ctx, server, repo, "")
}

// Latest returns the newest release of the GitHub repo at location, which is
// "owner/name" with an optional host in front.
func (inf *Inferrer) Latest(ctx context.Context, location string) (forge.Release, error) {
	return inf.Tagged(ctx, location, "")
}

// Tagged returns the release of location with that tag. Unlike Latest, it also
// returns a prerelease.
func (inf *Inferrer) Tagged(ctx context.Context, location, tag string) (forge.Release, error) {
	host, repo := forge.Split(location)

	return release(ctx, inf.Hosts.GitHub(host), repo, tag)
}

func release(ctx context.Context, server forge.Forge, repo, tag string) (forge.Release, error) {
	rel, err := server.Release(ctx, repo, tag)

	switch {
	case errors.Is(err, forge.ErrNotFound) && tag != "":
		return rel, fmt.Errorf("%s %w %s", repo, errNoRelease, tag)
	case errors.Is(err, forge.ErrNotFound):
		return rel, fmt.Errorf("%s has no manifest and no release to infer one from", repo)
	case err != nil && tag != "":
		return rel, fmt.Errorf("read the release %s of %s: %w", tag, repo, err)
	case err != nil:
		return rel, fmt.Errorf("read the newest release of %s: %w", repo, err)
	}

	return rel, nil
}

// targets lists the platforms in the order their artifacts are written. A glibc
// build comes before a musl build. The musl build has no libc in its match, so
// glibc hosts with no build of their own use it.
func targets() []target {
	var all []target

	add := func(os, arch, libc, matchLibc string) {
		t := target{Selector: platform.Selector{OS: os, Arch: arch, Libc: matchLibc}}
		t.words.os, t.words.arch, t.words.libc = osWords[os], archWords[arch], libcWords[libc]

		// A universal build holds the arches the OS still runs on.
		if arch == "amd64" || arch == "arm64" {
			t.words.fat = fatWords[os]
		}

		all = append(all, t)
	}

	for _, arch := range []string{"amd64", "arm64", "386", "arm", "riscv64"} {
		add("linux", arch, "glibc", "glibc")
		add("linux", arch, "musl", "")
		add("linux", arch, "", "")
		add("darwin", arch, "", "")
		add("windows", arch, "", "")
	}

	return all
}

// pick returns the assets that fit t, the best one first. It requires the OS and
// arch words in the name. A linux target also requires its libc word, or no libc
// word when it has none. A glibc target also takes an archive or a binary that
// names no libc when a musl build of the arch sits beside it, since that one is
// then the glibc build. Elsewhere "gnu" names a toolchain, as in
// "x86_64-pc-windows-gnu".
func pick(names []string, sizes map[string]int64, pkg string, t target) []string {
	var fits []string

	musl := slices.ContainsFunc(names, func(name string) bool {
		lower := strings.ToLower(name)

		return fitsPlatform(lower, t) && hasWord(lower, libcWords["musl"])
	})

	for _, name := range names {
		lower := strings.ToLower(name)
		if !fitsPlatform(lower, t) {
			continue
		}

		namesLibc := hasWord(lower, libcWords["glibc"]) || hasWord(lower, libcWords["musl"])
		glibc := !namesLibc && musl && installerOS(lower) == ""

		switch {
		case t.Libc == "glibc" && !hasWord(lower, t.words.libc) && !glibc:
			continue
		case t.Libc == "" && len(t.words.libc) > 0 && !hasWord(lower, t.words.libc):
			continue
		case len(t.words.libc) == 0 && namesLibc && t.OS == "linux":
			continue
		}

		fits = append(fits, name)
	}

	// An asset named after the package sorts before one of another program of
	// the same release, as "tool-x86_64" before "tool-server-x86_64", in any
	// format and for any arch. A build for the arch sorts before a universal
	// one. A command line build sorts before a desktop app, which holds no
	// program to link. A plain build sorts before a variant whose name adds a
	// word, as "tool-linux-amd64" before "tool-linux-amd64-baseline". On
	// Windows an msvc build sorts before a gnu one, as scoop and aqua pick it.
	// A tar archive keeps file modes, so it sorts before a zip, and both sort
	// before an installer, whose paths are the ones of an install tree. A smaller asset
	// sorts before a larger one, because a desktop app with a plain name still
	// bundles far more than a command line tool. A shorter name sorts before
	// variants such as "-debug".
	slices.SortFunc(fits, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(sibling(a, pkg), sibling(b, pkg)),
			cmp.Compare(t.fat(a), t.fat(b)),
			cmp.Compare(desktop(a), desktop(b)),
			cmp.Compare(variant(a), variant(b)),
			cmp.Compare(t.gnu(a), t.gnu(b)),
			cmp.Compare(rank(a), rank(b)),
			smaller(sizes[a], sizes[b]),
			cmp.Compare(len(a), len(b)),
			strings.Compare(a, b),
		)
	})

	return fits
}

// fitsPlatform reports whether an asset's lower-case name fits t's OS and arch.
// A name that holds no arch word fits the arches its OS is built for without
// one: amd64 and arm64 for an installer and for macOS, which builds universal
// apps, and amd64 elsewhere. A name with no OS word names it by its format, as
// ".dmg" or ".exe" do. oku targets no Android, so an Android build fits nothing.
func fitsPlatform(lower string, t target) bool {
	if hasAnySuffix(lower, skipped) || hasWord(lower, []string{"android", "androideabi"}) {
		return false
	}

	formatOS := installerOS(lower)
	if formatOS == "" && exeByFormat(lower) && desktop(lower) == 0 {
		formatOS = "windows"
	}

	namesOS := hasWord(lower, t.words.os)
	if !namesOS && formatOS != t.OS {
		return false
	}

	if hasWord(lower, t.words.arch) || hasWord(lower, t.words.fat) {
		return true
	}

	if namesArch(lower) {
		return false
	}

	universal := installerOS(lower) != "" || t.OS == "darwin" && namesOS

	return t.Arch == "amd64" || universal && t.Arch == "arm64"
}

// exeByFormat reports whether an asset fits Windows by its ".exe" alone, with
// no Windows word in its name.
func exeByFormat(name string) bool {
	lower := strings.ToLower(name)

	return strings.HasSuffix(lower, ".exe") && !hasWord(lower, osWords["windows"])
}

// variant counts the words of an asset's name that name neither the program,
// its version, nor its platform, as "baseline" in "tool-linux-x64-baseline.zip".
func variant(name string) int {
	lower := strings.ToLower(name)
	lower = strings.TrimSuffix(lower, ending(lower))
	lower = lower[len(stem(lower)):]

	for _, groups := range []map[string][]string{osWords, archWords, fatWords, libcWords} {
		for _, words := range groups {
			lower = blank(lower, words)
		}
	}

	lower = blank(lower, fillers)

	count := 0

	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return r > unicode.MaxASCII || !isAlnum(byte(r)) }) {
		if !startsWithTag(word) {
			count++
		}
	}

	return count
}

// blank replaces each whole word of words in name with a space.
func blank(name string, words []string) string {
	for _, word := range words {
		for from := 0; ; {
			at := strings.Index(name[from:], word)
			if at < 0 {
				break
			}

			start, end := from+at, from+at+len(word)
			if (start == 0 || !isAlnum(name[start-1])) && (end == len(name) || !isAlnum(name[end])) {
				name = name[:start] + strings.Repeat(" ", len(word)) + name[end:]
			}

			from = start + 1
		}
	}

	return name
}

// installerOS returns the OS an installer format or an AppImage runs on, or
// "" for any other file.
func installerOS(name string) string {
	switch {
	case strings.HasSuffix(name, ".dmg"), strings.HasSuffix(name, ".pkg"):
		return "darwin"
	case strings.HasSuffix(name, ".msi"):
		return "windows"
	case strings.HasSuffix(name, ".deb"), strings.HasSuffix(name, ".rpm"),
		strings.HasSuffix(name, ".appimage"):
		return "linux"
	default:
		return ""
	}
}

// namesArch reports whether name holds a word of any arch, one without a
// target included.
func namesArch(name string) bool {
	if hasWord(name, otherArches) {
		return true
	}

	for _, words := range archWords {
		if hasWord(name, words) {
			return true
		}
	}

	return false
}

// notProgram reports whether p is a library or a script that an archive may
// mark executable and that no one runs by name, as "lib/parser.so".
func notProgram(p string) bool {
	base := strings.ToLower(path.Base(p))

	return hasAnySuffix(base, []string{".so", ".dylib", ".dll", ".sh", ".ps1", ".bat", ".cmd"}) ||
		strings.Contains(base, ".so.")
}

// desktopWords name a desktop app rather than a command line build, such as
// "tool-desktop-mac-arm64.app.tar.gz" beside "tool-darwin-arm64.zip". A format
// such as .dmg is not a word here, because rank orders formats.
var desktopWords = []string{"desktop", "app", "gui", "installer", "setup"}

// desktop is 1 for an asset that is a desktop app.
func desktop(name string) int {
	lower := strings.ToLower(name)
	if strings.Contains(lower, ".app.") || hasWord(lower, desktopWords) {
		return 1
	}

	return 0
}

// smaller orders a before b when it has fewer bytes. A size of 0 is unknown and
// orders neither.
func smaller(a, b int64) int {
	if a == 0 || b == 0 {
		return 0
	}

	return cmp.Compare(a, b)
}

// rank orders the formats an asset comes in: tar archives, then zip and 7z,
// then a single binary, then installers, and a .pkg last. A .pkg holds an
// install tree with the app somewhere inside, and the .dmg beside it holds the
// app at its top.
func rank(name string) int {
	lower := strings.ToLower(name)

	switch {
	case strings.HasSuffix(lower, ".pkg"):
		return 4
	case hasAnySuffix(lower, installers):
		return 3
	case strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".7z"):
		return 1
	case isArchive(lower):
		return 0
	default:
		return 2
	}
}

// sibling is 1 for an asset of another program than pkg, as
// "tool-server-x86_64" is for tool. An asset named after pkg alone, as
// "tool.exe", is pkg's.
func sibling(name, pkg string) int {
	lower := strings.ToLower(name)
	if stem(name) == pkg || strings.TrimSuffix(strings.TrimSuffix(lower, ending(lower)), ".exe") == pkg {
		return 0
	}

	return 1
}

// gnu is 1 for a Windows build made with the GNU toolchain, as
// "x86_64-pc-windows-gnu".
func (t target) gnu(name string) int {
	if t.OS == "windows" && hasWord(strings.ToLower(name), []string{"gnu"}) {
		return 1
	}

	return 0
}

// fat is 1 for an asset that fits t as a universal build only.
func (t target) fat(name string) int {
	if hasWord(strings.ToLower(name), t.words.arch) {
		return 0
	}

	return 1
}

// hasWord reports whether name holds one of words as a whole word, so that "win"
// does not match inside "darwin". It searches the name and does not split it,
// because a word such as "x86_64" contains a separator itself.
func hasWord(name string, words []string) bool {
	return blank(name, words) != name
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func isArchive(name string) bool {
	return ending(name) != ""
}

// ending returns the archive ending of name, or "" for a single binary.
func ending(name string) string {
	lower := strings.ToLower(name)

	for _, suffix := range unpackable {
		if strings.HasSuffix(lower, suffix) {
			return suffix
		}
	}

	return ""
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}

	return false
}

// checksumFiles returns the checksum file of each of assets, as checksumFile
// finds it. Each asset may have a checksum file of its own, so it reads them
// several at once.
func (inf *Inferrer) checksumFiles(
	ctx context.Context,
	auth forge.Auth,
	names []string,
	urls, digests map[string]string,
	assets []string,
) map[string]string {
	sums := make([]string, len(assets))

	var (
		wg   sync.WaitGroup
		next atomic.Int64
	)

	for range min(checksumsAtOnce, len(assets)) {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < len(assets); i = int(next.Add(1)) - 1 {
				sums[i] = inf.checksumFile(ctx, auth, names, urls, digests[assets[i]], assets[i])
			}
		})
	}

	wg.Wait()

	sumsOf := make(map[string]string, len(assets))
	for i, asset := range assets {
		sumsOf[asset] = sums[i]
	}

	return sumsOf
}

// checksumsAtOnce is how many checksum files checksumFiles reads at once.
const checksumsAtOnce = 8

// checksumFile returns the asset that holds asset's sha256, as checksumAsset
// picks it. When the host reports digest for asset, oku skips a checksum file
// that states another digest, such as one that hashes the program inside the
// archive. oku then checks the download against the host's digest.
func (inf *Inferrer) checksumFile(
	ctx context.Context,
	auth forge.Auth,
	names []string,
	urls map[string]string,
	digest, asset string,
) string {
	for {
		sums := checksumAsset(names, asset)
		if sums == "" || digest == "" {
			return sums
		}

		if got, err := inf.Checksum(ctx, urls[sums], asset, auth); err == nil && got == digest {
			return sums
		}

		names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == sums })
	}
}

// checksumAsset returns the asset that holds asset's sha256: "<asset>.sha256"
// first, then the shared checksum file that fits asset best.
func checksumAsset(names []string, asset string) string {
	for _, suffix := range []string{".sha256", ".sha256sum"} {
		if slices.Contains(names, asset+suffix) {
			return asset + suffix
		}
	}

	// A release may hold one checksum file for each OS or each platform, such as
	// "tool-mac-checksums.txt" or "tool-linux-arm64-checksums.txt". The one for
	// another platform does not list the asset, so a generic file such as
	// "checksums.txt" or "SHA256SUMS" beats it, and a file that names the
	// asset's own OS and arch wins over both. "SHASUMS256.txt" and
	// "sha256.txt" are shared files too. A ".shasum" file holds a SHA-1, and
	// "other.deb.sha256sum" holds the digest of another asset alone.
	best, bestScore := "", 0

	for _, name := range names {
		lower := strings.ToLower(name)
		shared := strings.Contains(lower, "checksum") || strings.Contains(lower, "sha256sum") ||
			strings.Contains(lower, "shasums256") || lower == "sha256.txt"

		// A Sigstore bundle of a checksum file names "checksum" too, and a
		// ".shasum" file holds a SHA-1.
		if hasAnySuffix(lower, signatures) || hasAnySuffix(lower, []string{".bundle", ".shasum", ".intoto.jsonl"}) ||
			!shared || ownChecksum(names, name) {
			continue
		}

		if score := platformScore(lower, strings.ToLower(asset)); score > bestScore {
			best, bestScore = name, score
		}
	}

	return best
}

// ownChecksum reports whether name is the checksum file of one other asset of
// names, such as "tool.deb.sha256sum" beside "tool.deb".
func ownChecksum(names []string, name string) bool {
	for _, suffix := range []string{".sha256", ".sha256sum"} {
		if asset, ok := strings.CutSuffix(name, suffix); ok && slices.Contains(names, asset) {
			return true
		}
	}

	return false
}

// platformScore rates how well a checksum file fits asset: 3 when it names the
// asset's OS and arch, 2 when it names one of them and nothing else, 1 when it
// names no platform, and 0 when it names another OS or another arch.
func platformScore(name, asset string) int {
	os, arch := wordsOf(name, asset, osWords), wordsOf(name, asset, archWords)

	switch {
	case os < 0 || arch < 0:
		return 0
	case os > 0 && arch > 0:
		return 3
	case os > 0 || arch > 0:
		return 2
	default:
		return 1
	}
}

// wordsOf is 1 when name has a word of groups that asset has too, -1 when it
// has words of other groups only, and 0 when it has none.
func wordsOf(name, asset string, groups map[string][]string) int {
	found := 0

	for _, words := range groups {
		if !hasWord(name, words) {
			continue
		}

		if hasWord(asset, words) {
			return 1
		}

		found = -1
	}

	return found
}

// template swaps the tag and the version in a release URL for their variables.
// GitHub writes the "+" of a tag such as v1.30.0+k3s1 as %2B in its URLs, and
// serves the URL with either.
func template(url, tag, version string) string {
	for _, form := range []string{tag, strings.ReplaceAll(tag, "+", "%2B")} {
		url = swap(url, form, "{{tag}}")
	}

	if version != tag {
		for _, form := range []string{version, strings.ReplaceAll(version, "+", "%2B")} {
			url = swap(url, form, "{{version}}")
		}
	}

	return url
}

// swap replaces old in s where no digit is next to it, so that the version "1"
// becomes a variable in "tool-v1-arm64" and the "1" of "10" does not. A
// version of one number such as "2" behind a letter is part of a word, as in
// the repo name "tool2", unless the letter is a "v" that starts the word.
func swap(s, old, with string) string {
	var b strings.Builder

	from := 0

	for at := 0; ; {
		i := strings.Index(s[at:], old)
		if i < 0 {
			return b.String() + s[from:]
		}

		start, end := at+i, at+i+len(old)
		at = end

		if start > 0 && isDigit(s[start-1]) || end < len(s) && isDigit(s[end]) ||
			!strings.Contains(old, ".") && inWord(s, start) {
			continue
		}

		b.WriteString(s[from:start] + with)
		from = end
	}
}

// inWord reports whether a letter other than a leading "v" comes before s[at].
func inWord(s string, at int) bool {
	if at == 0 || !isLetter(s[at-1]) {
		return false
	}

	return s[at-1] != 'v' && s[at-1] != 'V' || at > 1 && isLetter(s[at-2])
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

const maxManPages = 8

type layout struct {
	strip int
	// bins are the programs. The first is the package's own, whose man page
	// stays when there are too many.
	bins []string
	// named is the program's name when the first of bins is named after the
	// asset. Each artifact then runs the file of its own asset.
	named string
	// found are the names of the other programs beside the first of bins,
	// which the manifest leaves out.
	found []string
	app   []string
	man   []string
}

// findLayout locates the programs among files. Each of wants names one.
// Without wants the program is the executable called name, or the only
// executable there is, and an executable beside it whose name starts with
// "<name>-", such as age-keygen beside age, is a program too. A macOS app
// bundle becomes an app, and the files inside it are no program.
func findLayout(files []File, name, file string, named []string, archive bool) (layout, error) {
	wants := make([]string, len(named))
	for i, want := range named {
		wants[i] = strings.ToLower(strings.TrimSuffix(want, ".exe"))
	}

	if !archive {
		var l layout

		if len(files) == 1 && files[0].Setup {
			return l, errors.New("it is a setup program, which oku does not run\nwrite a manifest for it")
		}

		switch len(wants) {
		case 0:
			l.bins = []string{name}
		case 1:
			l.bins = wants
		default:
			return l, errors.New("the download is a single program, so --bin names one")
		}

		// A Windows program for the GUI is an app, which oku stores under the
		// bin's name.
		if len(files) == 1 && files[0].GUI {
			l.app = []string{l.bins[0] + ".exe"}
		}

		return l, nil
	}

	var l layout

	top := ""
	shared := true

	for _, f := range files {
		first, _, nested := strings.Cut(f.Path, "/")
		if !nested || top != "" && first != top {
			shared = false
		}

		top = first
	}

	// The bundle is the package, so it stays where the artifact can name it.
	if shared && top != "" && !strings.HasSuffix(strings.ToLower(top), ".app") {
		l.strip = 1
	}

	inside := func(p string) string {
		if l.strip == 1 {
			_, rest, _ := strings.Cut(p, "/")

			return rest
		}

		return p
	}

	// plain are the files that are not executable. A zip made on Windows keeps
	// no modes, so its programs are among them. inBundle are the programs of
	// an app bundle, which the app runs, and some of which work in a terminal.
	var executables, plain, inBundle []string

	// mains are the programs that open each bundle's app, which its Info.plist
	// names.
	mains := map[string]string{}

	for _, f := range files {
		rel := inside(f.Path)
		if bundle := bundleOf(rel); bundle != "" && rel == bundle+"/Contents/Info.plist" {
			mains[bundle] = BundleExecutable(f.Text)
		}
	}

	for _, f := range files {
		// Some archives mark every file executable, so a man page is recognised by
		// its name first.
		switch bundle := bundleOf(inside(f.Path)); {
		case bundle != "":
			if !slices.Contains(l.app, bundle) {
				l.app = append(l.app, bundle)
			}

			if rel := inside(f.Path); f.Executable && bundleProgram(rel, bundle) {
				inBundle = append(inBundle, rel)
			}
		case strings.HasSuffix(f.Path, ".1"):
			l.man = append(l.man, inside(f.Path))
		case f.Executable && !notProgram(f.Path):
			executables = append(executables, inside(f.Path))
		default:
			plain = append(plain, inside(f.Path))
		}
	}

	slices.Sort(l.app)

	// find returns the program called want. A plain file counts when it is the
	// only one of that name and the user named it, or nothing is executable.
	find := func(want string, named bool) string {
		for _, p := range executables {
			if program(p) == want {
				return p
			}
		}

		var found []string

		for _, p := range plain {
			if program(p) == want {
				found = append(found, p)
			}
		}

		if len(found) == 1 && (named || len(executables) == 0) {
			return found[0]
		}

		// --bin may name any program of a bundle.
		for _, p := range inBundle {
			if named && program(p) == want {
				return p
			}
		}

		return ""
	}

	// A program of a bundle whose name starts with the package's is a
	// command. The program that opens the app is one only when it has the
	// package's name. A command replaces a program of the same name outside the
	// bundle, so the command runs the app's own program.
	commands := func() {
		for _, p := range inBundle {
			// A plist that oku cannot read leaves the program named after the
			// bundle as the one that opens the app.
			bundle := bundleOf(p)
			main := cmp.Or(mains[bundle], strings.TrimSuffix(path.Base(bundle), ".app"))

			base := path.Base(p)
			if !strings.HasPrefix(strings.ToLower(base), name) || base == main && base != name {
				continue
			}

			if i := slices.IndexFunc(l.bins, func(bin string) bool { return path.Base(bin) == base }); i >= 0 {
				l.bins[i] = p
			} else {
				l.bins = append(l.bins, p)
			}
		}
	}

	for _, want := range wants {
		p := find(want, true)
		if p == "" {
			return l, fmt.Errorf("no file in it is called %s", want)
		}

		l.bins = append(l.bins, p)
	}

	if len(wants) == 0 {
		main := find(name, false)

		// A program named after the asset, as "yq_darwin_arm64" in
		// "yq_darwin_arm64.tar.gz", runs under the package's name. A zip made
		// on Windows or a tar made without modes leaves it plain.
		if main == "" && file != "" {
			if main = find(program(file), true); main != "" {
				l.named = name
			}
		}

		switch {
		case main != "":
		case len(executables) == 1:
			main = executables[0]
		case len(l.app) > 0:
			// An app with helpers beside it is still an app.
			commands()

			return l, nil
		case len(executables) == 0:
			return l, errors.New("no file in it is executable\nwrite a manifest for it")
		default:
			return l, fmt.Errorf(
				"cannot tell which file is the program, executables found: %s\n"+
					"name it with --bin, or write a manifest for it",
				strings.Join(executables, ", "),
			)
		}

		l.bins = append(l.bins, main)
		l.bins = append(l.bins, siblings(main, executables, plain)...)
		commands()

		if l.named == "" {
			l.found = beside(main, l.bins, executables, plain)
		}
	}

	l.app = append(l.app, launched(files, l.bins, inside)...)

	for i, bin := range l.bins {
		l.bins[i] = strings.TrimSuffix(bin, ".exe")
	}

	slices.Sort(l.man)

	// A tool with one page per subcommand would fill the manifest, so past
	// maxManPages only the program's own page stays.
	if len(l.man) > maxManPages {
		own := path.Base(l.bins[0]) + ".1"
		l.man = slices.DeleteFunc(l.man, func(p string) bool { return path.Base(p) != own })
	}

	return l, nil
}

var bundleExecutableRe = regexp.MustCompile(`<key>CFBundleExecutable</key>\s*<string>([^<]+)</string>`)

// BundleExecutable returns the program an XML Info.plist says opens the app,
// or "". A binary plist holds no such text, so it returns "".
func BundleExecutable(plist string) string {
	if m := bundleExecutableRe.FindStringSubmatch(plist); m != nil {
		return strings.TrimSpace(m[1])
	}

	return ""
}

// bundleProgram reports whether rel, a file of bundle, is where a bundle keeps
// its programs: Contents/MacOS, or a bin directory under Contents/Resources.
func bundleProgram(rel, bundle string) bool {
	dir := path.Dir(strings.TrimPrefix(rel, bundle+"/"))

	return dir == "Contents/MacOS" || strings.HasPrefix(dir, "Contents/Resources/") && path.Base(dir) == "bin"
}

// launched returns the apps of Linux and Windows among files: a desktop entry
// that a desktop shows in its menu and that runs one of bins, and a Windows
// program of bins for the GUI. inside maps a file's path to the package's.
func launched(files []File, bins []string, inside func(string) string) []string {
	var apps []string

	for _, f := range files {
		rel := inside(f.Path)

		switch {
		case bundleOf(rel) != "":
		case strings.HasSuffix(rel, ".desktop") && f.Text != "":
			fields := xdg.Parse(f.Text)
			runs := program(xdg.Program(fields["Exec"]))

			if xdg.Launches(fields) && slices.ContainsFunc(bins, func(bin string) bool {
				return program(bin) == runs
			}) {
				apps = append(apps, rel)
			}
		case f.GUI && slices.Contains(bins, rel):
			apps = append(apps, rel)
		}
	}

	return apps
}

// siblings returns the programs in the directory of main whose names start
// with main's name and a "-". An .exe counts without a mode, since a zip made
// on Windows keeps none.
func siblings(main string, executables, plain []string) []string {
	prefix := program(main) + "-"

	var found []string

	for _, p := range executables {
		if p != main && path.Dir(p) == path.Dir(main) && strings.HasPrefix(program(p), prefix) {
			found = append(found, p)
		}
	}

	for _, p := range plain {
		if path.Dir(p) == path.Dir(main) && strings.HasSuffix(strings.ToLower(p), ".exe") &&
			strings.HasPrefix(program(p), prefix) {
			found = append(found, p)
		}
	}

	slices.Sort(found)

	return found
}

// beside returns the names of the programs in the directory of main that are
// none of bins, with the same rule for an .exe as siblings.
func beside(main string, bins, executables, plain []string) []string {
	var found []string

	for _, p := range executables {
		if path.Dir(p) == path.Dir(main) && !slices.Contains(bins, p) {
			found = append(found, program(p))
		}
	}

	for _, p := range plain {
		if path.Dir(p) == path.Dir(main) && strings.HasSuffix(strings.ToLower(p), ".exe") && !slices.Contains(bins, p) {
			found = append(found, program(p))
		}
	}

	slices.Sort(found)

	return found
}

// program returns the name a file runs as: its base name in lower case,
// without ".exe".
func program(p string) string {
	return strings.ToLower(strings.TrimSuffix(path.Base(p), ".exe"))
}

// bundleOf returns the outermost macOS app bundle that holds p, such as
// "Foo.app" for "Foo.app/Contents/MacOS/foo", or "" when none does. A bundle
// is a directory ending in ".app" with a Contents directory.
func bundleOf(p string) string {
	for at := 0; ; {
		i := strings.Index(p[at:], ".app/Contents/")
		if i < 0 {
			return ""
		}

		end := at + i + len(".app")
		if end < len(p) {
			return p[:end]
		}

		at = end
	}
}

func selectorTOML(s platform.Selector) string {
	parts := []string{fmt.Sprintf("os = %q", s.OS), fmt.Sprintf("arch = %q", s.Arch)}
	if s.Libc != "" {
		parts = append(parts, fmt.Sprintf("libc = %q", s.Libc))
	}

	return "{ " + strings.Join(parts, ", ") + " }"
}

func quoteAll(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}

	return strings.Join(quoted, ", ")
}

// Machine names p for a message: "this machine (darwin-arm64)" when p is the
// host, else the platform alone, such as for a platform of [lock].
func Machine(p platform.Platform) string {
	if p == platform.Host() {
		return "this machine (" + p.String() + ")"
	}

	return p.String()
}

// oneLine turns every control character of s into a space. The comments of a
// manifest oku translates quote names from the recipe, and a newline in
// one would start a line of TOML.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, s)
}
