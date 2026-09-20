// Package infer writes a manifest for a GitHub repo that has none, from the
// assets of its newest release.
package infer

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"

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

// Inferrer reads releases from GitHub.
type Inferrer struct {
	HTTP      *http.Client
	GitHubAPI string
	Token     string
	Inspect   Inspector
}

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// target is one platform the inferred manifest may cover. A target with an empty
// libc takes an asset that names no libc.
type target struct {
	platform.Selector

	words struct{ os, arch, libc []string }
}

var (
	osWords = map[string][]string{
		"linux":   {"linux"},
		"darwin":  {"darwin", "macos", "apple", "osx", "mac"},
		"windows": {"windows", "win64", "win"},
	}
	archWords = map[string][]string{
		"amd64": {"x86_64", "amd64", "x64"},
		"arm64": {"aarch64", "arm64"},
	}
	libcWords = map[string][]string{
		"glibc": {"gnu", "glibc"},
		"musl":  {"musl"},
	}
	// unpackable are the archive endings oku can unpack. A name with no known
	// ending is taken as a single binary.
	unpackable = []string{".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".zip", ".tar"}
	// skipped are endings of files that are not the package itself, or that oku
	// cannot unpack yet.
	skipped = []string{
		".sha256", ".sha256sum", ".sha512", ".md5", ".sig", ".asc", ".pem", ".sbom", ".json",
		".txt", ".deb", ".rpm", ".apk", ".msi", ".pkg", ".dmg", ".appimage", ".tar.xz", ".txz",
		".tar.zst", ".7z", ".minisig", ".crt", ".intoto.jsonl",
	}
)

// Manifest returns manifest TOML for the GitHub repo "owner/repo". It needs an
// asset for host, because it opens that asset to find the executable.
func (inf *Inferrer) Manifest(
	ctx context.Context,
	repo string,
	host platform.Platform,
) (string, error) {
	rel, err := inf.latest(ctx, repo)
	if err != nil {
		return "", err
	}

	names := make([]string, len(rel.Assets))
	urls := map[string]string{}

	for i, asset := range rel.Assets {
		names[i] = asset.Name
		urls[asset.Name] = asset.URL
	}

	hostAsset := ""

	for _, t := range targets() {
		if t.Matches(host) {
			if hostAsset = pick(names, t); hostAsset != "" {
				break
			}
		}
	}

	if hostAsset == "" {
		return "", fmt.Errorf(
			"no release asset fits this machine (%s)\nrelease %s of %s has: %s",
			host, rel.Tag, repo, strings.Join(names, ", "),
		)
	}

	files, err := inf.Inspect(ctx, urls[hostAsset])
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", hostAsset, err)
	}

	name := strings.ToLower(path.Base(repo))

	layout, err := findLayout(files, name, isArchive(hostAsset))
	if err != nil {
		return "", fmt.Errorf("%s: %w", hostAsset, err)
	}

	// The version starts at the tag's first digit, so "v1.2.0" and "jq-1.8.1" both
	// work. A tag without a digit is kept whole.
	prefix, version := "", rel.Tag
	if i := strings.IndexAny(rel.Tag, "0123456789"); i > 0 {
		prefix, version = rel.Tag[:i], rel.Tag[i:]
	}

	var b strings.Builder

	fmt.Fprintf(&b, "[package]\nname = %q\nhomepage = %q\n\n", name, "https://github.com/"+repo)
	fmt.Fprintf(&b, "[version]\nfrom = \"github-releases\"\nrepo = %q\n", repo)

	if prefix != "" {
		fmt.Fprintf(&b, "strip_prefix = %q\n", prefix)
	}

	written := map[platform.Selector]bool{}

	for _, t := range targets() {
		asset := pick(names, t)
		if asset == "" || written[t.Selector] {
			continue
		}

		written[t.Selector] = true

		fmt.Fprintf(&b, "\n[[artifact]]\nmatch = %s\n", selectorTOML(t.Selector))
		fmt.Fprintf(&b, "url = %q\n", template(urls[asset], rel.Tag, version))

		if sums := checksumAsset(names, asset); sums != "" {
			fmt.Fprintf(&b, "sha256_url = %q\n", template(urls[sums], rel.Tag, version))
		}

		bin := layout.bin
		if t.OS == "windows" {
			bin += ".exe"
		}

		if isArchive(asset) && layout.strip > 0 {
			fmt.Fprintf(&b, "strip = %d\n", layout.strip)
		}

		fmt.Fprintf(&b, "bin = [%q]\n", bin)

		if len(layout.man) > 0 && isArchive(asset) {
			fmt.Fprintf(&b, "man = [%s]\n", quoteAll(layout.man))
		}
	}

	return b.String(), nil
}

func (inf *Inferrer) latest(ctx context.Context, repo string) (release, error) {
	var rel release

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, inf.GitHubAPI+"/repos/"+repo+"/releases/latest", nil,
	)
	if err != nil {
		return rel, err
	}

	req.Header.Set("User-Agent", "oku")
	req.Header.Set("Accept", "application/vnd.github+json")

	if inf.Token != "" {
		req.Header.Set("Authorization", "Bearer "+inf.Token)
	}

	resp, err := inf.HTTP.Do(req)
	if err != nil {
		return rel, fmt.Errorf("read the newest release of %s: %w", repo, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return rel, fmt.Errorf("%s has no manifest and no release to infer one from", repo)
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return rel, errors.New("GitHub rate limit reached, set GITHUB_TOKEN to raise it")
	case resp.StatusCode != http.StatusOK:
		return rel, fmt.Errorf(
			"read the newest release of %s: server returned %s",
			repo,
			resp.Status,
		)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
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
		all = append(all, t)
	}

	for _, arch := range []string{"amd64", "arm64"} {
		add("linux", arch, "glibc", "glibc")
		add("linux", arch, "musl", "")
		add("linux", arch, "", "")
		add("darwin", arch, "", "")
		add("windows", arch, "", "")
	}

	return all
}

// pick returns the asset that fits t, or "". It requires the OS and arch words
// in the name, the libc word when t has one, and no libc word when t has none.
func pick(names []string, t target) string {
	var fits []string

	for _, name := range names {
		lower := strings.ToLower(name)

		if hasAnySuffix(lower, skipped) || !hasWord(lower, t.words.os) ||
			!hasWord(lower, t.words.arch) {
			continue
		}

		namesLibc := hasWord(lower, libcWords["glibc"]) || hasWord(lower, libcWords["musl"])
		if len(t.words.libc) > 0 && !hasWord(lower, t.words.libc) ||
			len(t.words.libc) == 0 && namesLibc {
			continue
		}

		fits = append(fits, name)
	}

	// A tar archive keeps file modes, so it sorts before a zip. A shorter name
	// sorts before variants such as "-debug".
	slices.SortFunc(fits, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(rank(a), rank(b)),
			cmp.Compare(len(a), len(b)),
			strings.Compare(a, b),
		)
	})

	if len(fits) == 0 {
		return ""
	}

	return fits[0]
}

func rank(name string) int {
	lower := strings.ToLower(name)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		return 1
	case isArchive(lower):
		return 0
	default:
		return 2
	}
}

// hasWord reports whether name holds one of words between separators, so that
// "win" does not match inside "darwin".
func hasWord(name string, words []string) bool {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})

	for _, word := range words {
		if slices.Contains(parts, word) {
			return true
		}
	}

	return false
}

func isArchive(name string) bool {
	return hasAnySuffix(strings.ToLower(name), unpackable)
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
		if strings.Contains(lower, "checksum") || strings.Contains(lower, "sha256sum") {
			return name
		}
	}

	return ""
}

// template swaps the tag and the version in a release URL for their variables.
func template(url, tag, version string) string {
	url = strings.ReplaceAll(url, tag, "{{tag}}")

	if version != tag {
		url = strings.ReplaceAll(url, version, "{{version}}")
	}

	return url
}

const maxManPages = 8

type layout struct {
	strip int
	bin   string
	man   []string
}

// findLayout locates the executable called name among files. Without one it
// takes the only executable there is.
func findLayout(files []File, name string, archive bool) (layout, error) {
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

	var executables []string

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
		}
	}

	if l.bin == "" && len(executables) == 1 {
		l.bin = executables[0]
	}

	if l.bin == "" {
		return l, fmt.Errorf(
			"cannot tell which file is the program, executables found: %s\nwrite a manifest for it",
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
