// Package infer writes a manifest for a GitHub repo that has none, from the
// assets of its newest release.
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
// it, so the download goes into oku's cache.
type Inspector func(ctx context.Context, url string) ([]File, error)

// File is one regular file of an unpacked asset.
type File struct {
	Path       string
	Executable bool
}

var errNoRelease = errors.New("has no release")

// Inferrer reads releases from a forge.
type Inferrer struct {
	Hosts   forge.Hosts
	Inspect Inspector
}

// Options set the release, the asset and the program Manifest uses. With the
// zero value it reads the newest release and chooses the other two.
type Options struct {
	// Version is the release to read, the way the user wrote it after "@".
	Version string
	// Asset is a glob that names the asset for the host.
	Asset string
	// Bin is the file name of the program inside the assets.
	Bin string
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
	// ending is taken as a single binary.
	unpackable = []string{
		".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar.zst", ".zip", ".7z",
		".tar",
	}
	// skipped are endings of files that are not the package itself, or that oku
	// cannot unpack yet.
	skipped = []string{
		".sha256", ".sha256sum", ".sha512", ".md5", ".sig", ".asc", ".pem", ".sbom", ".json",
		".txt", ".deb", ".rpm", ".apk", ".msi", ".pkg", ".dmg", ".appimage",
		".minisig", ".crt", ".intoto.jsonl", ".vsix",
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

// Manifest returns manifest TOML for the GitHub repo "owner/repo", which may
// have a host in front. It needs an
// asset for host, because it opens that asset to find the executable.
func (inf *Inferrer) Manifest(
	ctx context.Context,
	repo string,
	host platform.Platform,
	opts Options,
) (string, error) {
	rel, err := inf.wanted(ctx, repo, opts.Version)
	if err != nil {
		return "", err
	}

	names := make([]string, len(rel.Assets))
	urls := map[string]string{}

	for i, asset := range rel.Assets {
		names[i] = asset.Name
		urls[asset.Name] = asset.URL
	}

	chosen, err := choose(names, host, opts.Asset)
	if err != nil {
		return "", err
	}

	if !slices.ContainsFunc(chosen, func(c choice) bool { return c.Matches(host) }) {
		return "", fmt.Errorf(
			"no release asset fits this machine (%s)\nrelease %s of %s has: %s\n"+
				"name one with --asset",
			host, rel.Tag, repo, strings.Join(names, ", "),
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

	server, onServer := forge.Split(repo)

	fmt.Fprintf(
		&b, "[package]\nname = %q\nhomepage = %q\n\n",
		name, inf.Hosts.GitHub(server).Home(onServer),
	)
	fmt.Fprintf(&b, "[version]\nfrom = \"github-releases\"\nrepo = %q\n", repo)

	if prefix != "" {
		fmt.Fprintf(&b, "strip_prefix = %q\n", prefix)
	}

	// Assets with the same ending come from the same packaging step, so oku opens
	// one of them for all. A zip for Windows is often laid out unlike the tar
	// archives next to it.
	layouts := map[string]layout{}
	hostDone := false

	for _, c := range chosen {
		isHost := c.Matches(host) && !hostDone
		kind := ending(c.asset)

		l, known := layouts[kind]
		if !known {
			l, err = inf.layoutOf(ctx, urls[c.asset], c.asset, name, opts.Bin)

			switch {
			case err != nil && isHost:
				return "", fmt.Errorf("%s: %w", c.asset, err)
			case err != nil:
				// oku leaves out a platform whose asset it cannot read. A wrong
				// artifact would fail on that platform at install.
				continue
			}

			layouts[kind] = l
		}

		b.WriteString("\n")

		if isHost {
			hostDone = true

			if len(c.others) > 0 {
				fmt.Fprintf(
					&b, "# These assets fit this machine too: %s\n# Choose one with --asset.\n",
					strings.Join(c.others, ", "),
				)
			}
		}

		fmt.Fprintf(&b, "[[artifact]]\nmatch = %s\n", selectorTOML(c.Selector))
		fmt.Fprintf(&b, "url = %q\n", template(urls[c.asset], rel.Tag, version))

		if sums := checksumAsset(names, c.asset); sums != "" {
			fmt.Fprintf(&b, "sha256_url = %q\n", template(urls[sums], rel.Tag, version))
		}

		bin := l.bin
		if c.OS == "windows" {
			bin += ".exe"
		}

		if l.strip > 0 {
			fmt.Fprintf(&b, "strip = %d\n", l.strip)
		}

		fmt.Fprintf(&b, "bin = [%q]\n", bin)

		if len(l.man) > 0 {
			fmt.Fprintf(&b, "man = [%s]\n", quoteAll(l.man))
		}
	}

	return b.String(), nil
}

func (inf *Inferrer) layoutOf(ctx context.Context, url, asset, name, bin string) (layout, error) {
	files, err := inf.Inspect(ctx, url)
	if err != nil {
		return layout{}, fmt.Errorf("inspect it: %w", err)
	}

	return findLayout(files, name, bin, isArchive(asset))
}

// choose lists the artifacts to write, in the order of targets. A glob puts the
// asset it names first, for the host alone.
func choose(names []string, host platform.Platform, glob string) ([]choice, error) {
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
		fits := pick(names, t)
		if len(fits) == 0 || written[t.Selector] || glob != "" && t.Matches(host) {
			continue
		}

		written[t.Selector] = true

		// An asset of another rank is the same build in another archive format.
		others := slices.DeleteFunc(slices.Clone(fits[1:]), func(other string) bool {
			return rank(other) != rank(fits[0]) || t.fat(other) != t.fat(fits[0])
		})

		chosen = append(chosen, choice{Selector: t.Selector, asset: fits[0], others: others})
	}

	return chosen, nil
}

// wanted returns the release for version, or the newest one for "". It tries
// version as a tag and as a "v" tag. With any other prefix it returns the newest
// release, whose asset names are the best guess there is.
func (inf *Inferrer) wanted(ctx context.Context, repo, version string) (forge.Release, error) {
	if version != "" {
		for _, tag := range []string{version, "v" + version} {
			rel, err := inf.Tagged(ctx, repo, tag)
			if !errors.Is(err, errNoRelease) {
				return rel, err
			}
		}
	}

	return inf.Latest(ctx, repo)
}

// Latest returns the newest release of the GitHub repo at location, which is
// "owner/name" with an optional host in front.
func (inf *Inferrer) Latest(ctx context.Context, location string) (forge.Release, error) {
	return inf.release(ctx, location, "")
}

// Tagged returns the release of location with that tag. Unlike Latest, it also
// returns a prerelease.
func (inf *Inferrer) Tagged(ctx context.Context, location, tag string) (forge.Release, error) {
	return inf.release(ctx, location, tag)
}

func (inf *Inferrer) release(ctx context.Context, location, tag string) (forge.Release, error) {
	host, repo := forge.Split(location)

	rel, err := inf.Hosts.GitHub(host).Release(ctx, repo, tag)

	switch {
	case errors.Is(err, forge.ErrNotFound) && tag != "":
		return rel, fmt.Errorf("%s %w %s", location, errNoRelease, tag)
	case errors.Is(err, forge.ErrNotFound):
		return rel, fmt.Errorf("%s has no manifest and no release to infer one from", location)
	case err != nil && tag != "":
		return rel, fmt.Errorf("read the release %s of %s: %w", tag, location, err)
	case err != nil:
		return rel, fmt.Errorf("read the newest release of %s: %w", location, err)
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
func pick(names []string, t target) []string {
	var fits []string

	for _, name := range names {
		lower := strings.ToLower(name)

		if hasAnySuffix(lower, skipped) || !hasWord(lower, t.words.os) ||
			!hasWord(lower, t.words.arch) && !hasWord(lower, t.words.fat) {
			continue
		}

		namesLibc := hasWord(lower, libcWords["glibc"]) || hasWord(lower, libcWords["musl"])
		if len(t.words.libc) > 0 && !hasWord(lower, t.words.libc) ||
			len(t.words.libc) == 0 && namesLibc && t.OS == "linux" {
			continue
		}

		fits = append(fits, name)
	}

	// A build for the arch sorts before a universal one. A tar archive keeps file
	// modes, so it sorts before a zip. A shorter name sorts before variants such
	// as "-debug".
	slices.SortFunc(fits, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(t.fat(a), t.fat(b)),
			cmp.Compare(rank(a), rank(b)),
			cmp.Compare(len(a), len(b)),
			strings.Compare(a, b),
		)
	})

	return fits
}

func rank(name string) int {
	lower := strings.ToLower(name)

	switch {
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

// checksumAsset returns the asset that holds asset's sha256: "<asset>.sha256"
// first, then a shared checksum file.
func checksumAsset(names []string, asset string) string {
	for _, suffix := range []string{".sha256", ".sha256sum"} {
		if slices.Contains(names, asset+suffix) {
			return asset + suffix
		}
	}

	for _, name := range names {
		lower := strings.ToLower(name)
		if hasAnySuffix(lower, signatures) {
			continue
		}

		if strings.Contains(lower, "checksum") || strings.Contains(lower, "sha256sum") {
			return name
		}
	}

	return ""
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
	bin   string
	man   []string
}

// findLayout locates the program among files. That is the file called want, or
// without a want the executable called name, or the only executable there is.
func findLayout(files []File, name, want string, archive bool) (layout, error) {
	if want != "" {
		name = strings.ToLower(strings.TrimSuffix(want, ".exe"))
	}

	if !archive {
		return layout{bin: name}, nil
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

	if shared && top != "" {
		l.strip = 1
	}

	inside := func(p string) string {
		if l.strip == 1 {
			_, rest, _ := strings.Cut(p, "/")

			return rest
		}

		return p
	}

	// plain are the files called name that are not executable. A zip made on
	// Windows keeps no modes, so its program is one of them.
	var executables, plain []string

	for _, f := range files {
		base := strings.ToLower(strings.TrimSuffix(path.Base(f.Path), ".exe"))

		// Some archives mark every file executable, so a man page is recognised by
		// its name first.
		switch {
		case strings.HasSuffix(f.Path, ".1"):
			l.man = append(l.man, inside(f.Path))
		case f.Executable && base == name:
			l.bin = inside(f.Path)
		case f.Executable:
			executables = append(executables, inside(f.Path))
		case base == name:
			plain = append(plain, inside(f.Path))
		}
	}

	switch {
	case l.bin != "":
	case len(plain) == 1 && (want != "" || len(executables) == 0):
		l.bin = plain[0]
	case want != "":
		return l, fmt.Errorf("no file in it is called %s", want)
	case len(executables) == 1:
		l.bin = executables[0]
	case len(executables) == 0:
		return l, errors.New("no file in it is executable\nwrite a manifest for it")
	default:
		return l, fmt.Errorf(
			"cannot tell which file is the program, executables found: %s\n"+
				"name it with --bin, or write a manifest for it",
			strings.Join(executables, ", "),
		)
	}

	l.bin = strings.TrimSuffix(l.bin, ".exe")
	slices.Sort(l.man)

	// A tool with one page per subcommand would fill the manifest, so past
	// maxManPages only the program's own page stays.
	if len(l.man) > maxManPages {
		own := path.Base(l.bin) + ".1"
		l.man = slices.DeleteFunc(l.man, func(p string) bool { return path.Base(p) != own })
	}

	return l, nil
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
