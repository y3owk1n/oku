// Package infer writes a manifest for a repo that has none, from the assets of
// its newest release.
package infer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/platform"
)

// Inspector downloads an asset and reports the files in it. The store provides
// it, so the download goes into oku's cache. auth is the login for the host of a
// private repo.
type Inspector func(ctx context.Context, url string, auth forge.Auth) ([]File, error)

// File is one regular file of an unpacked asset.
type File struct {
	Path       string
	Executable bool
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
		"windows": {"windows", "win64", "win"},
	}
	archWords = map[string][]string{
		"amd64":   {"x86_64", "x86-64", "amd64", "x64"},
		"arm64":   {"aarch64", "arm64"},
		"386":     {"i386", "i686", "386"},
		"arm":     {"armv7", "armv7l", "armhf", "arm"},
		"riscv64": {"riscv64"},
	}
	fatWords = map[string][]string{
		"darwin": {"universal", "universal2", "all"},
	}
	libcWords = map[string][]string{
		"glibc": {"gnu", "glibc"},
		"musl":  {"musl"},
	}
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
		".sha256", ".sha256sum", ".sha512", ".md5", ".sig", ".asc", ".pem", ".sbom", ".json",
		".txt", ".apk", ".minisig", ".crt", ".intoto.jsonl", ".vsix", ".delta",
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

	chosen, err := choose(names, sizes, host, opts.Asset)
	if err != nil {
		return Inferred{}, err
	}

	var result Inferred

	if i := slices.IndexFunc(chosen, func(c choice) bool { return c.Matches(host) }); i >= 0 {
		result.Asset, result.Others = chosen[i].asset, chosen[i].others
	} else {
		// --asset picks a file for the host, so a [lock] platform needs another way.
		fix := "name one with --asset"
		if host != platform.Host() {
			fix = "leave " + host.String() + " out of the package's when, or write a manifest for it"
		}

		return Inferred{}, fmt.Errorf(
			"%w %s\nrelease %s of %s has: %s\n%s",
			ErrNoAsset, Machine(host), rel.Tag, repo, strings.Join(names, ", "), fix,
		)
	}

	name := strings.ToLower(path.Base(repo))

	// The version starts at the tag's first digit, so "v1.2.0" and "jq-1.8.1" both
	// work. A tag without a digit is kept whole.
	prefix, version := "", rel.Tag
	if i := strings.IndexAny(rel.Tag, "0123456789"); i > 0 {
		prefix, version = rel.Tag[:i], rel.Tag[i:]
	}

	var b strings.Builder

	// version.repo names the host, which a "codeberg:" ref leaves out.
	versionRepo := repo
	if server.Host() != "" {
		versionRepo = server.Host() + "/" + repo
	}

	fmt.Fprintf(&b, "[package]\nname = %q\nhomepage = %q\n\n", name, server.Home(repo))
	fmt.Fprintf(&b, "[version]\nfrom = %q\nrepo = %q\n", server.Kind()+"-releases", versionRepo)

	if prefix != "" {
		fmt.Fprintf(&b, "strip_prefix = %q\n", prefix)
	}

	// Assets with the same ending come from the same packaging step, so oku opens
	// one of them for all. A zip for Windows is often laid out unlike the tar
	// archives next to it. oku opens an asset for the host and the lock platforms
	// only, and a platform outside the lock gets an artifact when its asset has
	// the ending of one it opened anyway.
	layouts := map[string]layout{}
	hostDone := false

	for _, c := range chosen {
		isHost := c.Matches(host) && !hostDone
		kind := ending(c.asset)

		if _, known := layouts[kind]; known || !isHost && !c.pinned(opts.Platforms) {
			continue
		}

		l, err := inf.layoutOf(ctx, server.Auth(), urls[c.asset], c.asset, name, opts.Bins)

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

		layouts[kind] = l

		if isHost {
			hostDone = true
		}
	}

	hostDone = false

	for _, c := range chosen {
		isHost := c.Matches(host) && !hostDone

		l, known := layouts[ending(c.asset)]
		if !known {
			continue
		}

		b.WriteString("\n")

		if isHost {
			hostDone = true

			if len(c.others) > 0 {
				fmt.Fprintf(
					&b, "# These assets fit %s too: %s\n# Choose one with --asset.\n",
					host, strings.Join(c.others, ", "),
				)
			}
		}

		fmt.Fprintf(&b, "[[artifact]]\nmatch = %s\n", selectorTOML(c.Selector))
		fmt.Fprintf(&b, "url = %q\n", template(urls[c.asset], rel.Tag, version))

		if sums := inf.checksumFile(ctx, server.Auth(), names, urls, digests[c.asset], c.asset); sums != "" {
			fmt.Fprintf(&b, "sha256_url = %q\n", template(urls[sums], rel.Tag, version))
		}

		b.WriteString(l.toml(c.OS))
	}

	result.Text = b.String()

	return result, nil
}

// toml writes the strip, bin, app and man lines of a layout for an artifact of os.
func (l layout) toml(os string) string {
	var b strings.Builder

	if l.strip > 0 {
		fmt.Fprintf(&b, "strip = %d\n", l.strip)
	}

	if len(l.bins) > 0 {
		bins := slices.Clone(l.bins)
		if os == "windows" {
			for i := range bins {
				bins[i] += ".exe"
			}
		}

		fmt.Fprintf(&b, "bin = [%s]\n", quoteAll(bins))
	}

	if len(l.app) > 0 {
		fmt.Fprintf(&b, "app = [%s]\n", quoteAll(l.app))
	}

	if len(l.man) > 0 {
		fmt.Fprintf(&b, "man = [%s]\n", quoteAll(l.man))
	}

	return b.String()
}

func (inf *Inferrer) layoutOf(
	ctx context.Context,
	auth forge.Auth,
	url, asset, name string,
	bins []string,
) (layout, error) {
	files, err := inf.Inspect(ctx, url, auth)
	if err != nil {
		return layout{}, fmt.Errorf("inspect it: %w", err)
	}

	return findLayout(files, name, bins, isArchive(asset))
}

// choose lists the artifacts to write, in the order of targets. sizes holds the
// bytes of each asset, or 0 when the host does not say. A glob puts the asset it
// names first, for the host alone.
func choose(
	names []string,
	sizes map[string]int64,
	host platform.Platform,
	glob string,
) ([]choice, error) {
	var chosen []choice

	if glob != "" {
		var named []string

		for _, name := range names {
			ok, err := path.Match(glob, name)
			if err != nil {
				return nil, fmt.Errorf("--asset %q: %w", glob, err)
			}

			if ok {
				named = append(named, name)
			}
		}

		if len(named) != 1 {
			return nil, fmt.Errorf(
				"--asset %q names %d assets, want one of: %s",
				glob, len(named), strings.Join(names, ", "),
			)
		}

		chosen = append(chosen, choice{
			Selector: platform.Selector(host),
			asset:    named[0],
		})
	}

	written := map[platform.Selector]bool{}

	for _, t := range targets() {
		fits := pick(names, sizes, t)
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

	return chosen, nil
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
// word when it has none. Elsewhere "gnu" names a toolchain, as in
// "x86_64-pc-windows-gnu".
func pick(names []string, sizes map[string]int64, t target) []string {
	var fits []string

	for _, name := range names {
		lower := strings.ToLower(name)

		// A macOS-only app often names no platform at all, as "Tool1.2.dmg". Its
		// format names the OS, and with no arch word oku takes it as a build for
		// the arches the OS still runs on.
		formatOS := installerOS(lower)
		anyArch := formatOS != "" && !namesArch(lower) && (t.Arch == "amd64" || t.Arch == "arm64")

		if hasAnySuffix(lower, skipped) || !hasWord(lower, t.words.os) && formatOS != t.OS ||
			!hasWord(lower, t.words.arch) && !hasWord(lower, t.words.fat) && !anyArch {
			continue
		}

		namesLibc := hasWord(lower, libcWords["glibc"]) || hasWord(lower, libcWords["musl"])
		if len(t.words.libc) > 0 && !hasWord(lower, t.words.libc) ||
			len(t.words.libc) == 0 && namesLibc && t.OS == "linux" {
			continue
		}

		fits = append(fits, name)
	}

	// A build for the arch sorts before a universal one. A command line build
	// sorts before a desktop app, which holds no program to link. A tar archive
	// keeps file modes, so it sorts before a zip, and both sort before an
	// installer, whose paths are the ones of an install tree. A smaller asset
	// sorts before a larger one, because a desktop app with a plain name still
	// bundles far more than a command line tool. A shorter name sorts before variants such as
	// "-debug".
	slices.SortFunc(fits, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(t.fat(a), t.fat(b)),
			cmp.Compare(desktop(a), desktop(b)),
			cmp.Compare(rank(a), rank(b)),
			smaller(sizes[a], sizes[b]),
			cmp.Compare(len(a), len(b)),
			strings.Compare(a, b),
		)
	})

	return fits
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

// namesArch reports whether name holds a word of any arch.
func namesArch(name string) bool {
	for _, words := range archWords {
		if hasWord(name, words) {
			return true
		}
	}

	return false
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
	for _, word := range words {
		for from := 0; ; {
			at := strings.Index(name[from:], word)
			if at < 0 {
				break
			}

			start, end := from+at, from+at+len(word)
			if (start == 0 || !isAlnum(name[start-1])) &&
				(end == len(name) || !isAlnum(name[end])) {
				return true
			}

			from = start + 1
		}
	}

	return false
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
	// asset's own OS and arch wins over both.
	best, bestScore := "", 0

	for _, name := range names {
		lower := strings.ToLower(name)
		if hasAnySuffix(lower, signatures) ||
			!strings.Contains(lower, "checksum") && !strings.Contains(lower, "sha256sum") {
			continue
		}

		if score := platformScore(lower, strings.ToLower(asset)); score > bestScore {
			best, bestScore = name, score
		}
	}

	return best
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
func template(url, tag, version string) string {
	url = swap(url, tag, "{{tag}}")

	if version != tag {
		url = swap(url, version, "{{version}}")
	}

	return url
}

// swap replaces old in s where no digit is next to it, so that the version "1"
// becomes a variable in "tool-v1-arm64" and the "1" of "10" does not.
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

		if start > 0 && isDigit(s[start-1]) || end < len(s) && isDigit(s[end]) {
			continue
		}

		b.WriteString(s[from:start] + with)
		from = end
	}
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
	app  []string
	man  []string
}

// findLayout locates the programs among files. Each of wants names one.
// Without wants the program is the executable called name, or the only
// executable there is, and an executable beside it whose name starts with
// "<name>-", such as age-keygen beside age, is a program too. A macOS app
// bundle becomes an app, and the files inside it are no program.
func findLayout(files []File, name string, named []string, archive bool) (layout, error) {
	wants := make([]string, len(named))
	for i, want := range named {
		wants[i] = strings.ToLower(strings.TrimSuffix(want, ".exe"))
	}

	if !archive {
		switch len(wants) {
		case 0:
			return layout{bins: []string{name}}, nil
		case 1:
			return layout{bins: wants}, nil
		default:
			return layout{}, errors.New("the download is a single program, so --bin names one")
		}
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
	// no modes, so its programs are among them.
	var executables, plain []string

	for _, f := range files {
		// Some archives mark every file executable, so a man page is recognised by
		// its name first.
		switch bundle := bundleOf(inside(f.Path)); {
		case bundle != "":
			if !slices.Contains(l.app, bundle) {
				l.app = append(l.app, bundle)
			}
		case strings.HasSuffix(f.Path, ".1"):
			l.man = append(l.man, inside(f.Path))
		case f.Executable:
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

		return ""
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

		switch {
		case main != "":
		case len(executables) == 1:
			main = executables[0]
		case len(l.app) > 0:
			// An app with helpers beside it is still an app.
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
	}

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
