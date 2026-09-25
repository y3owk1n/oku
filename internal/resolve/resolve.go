// Package resolve discovers the versions a manifest can install and picks one.
package resolve

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/tempdir"
)

// Resolver lists versions.
type Resolver struct {
	Hosts forge.Hosts
	// NPM replaces the URL of the npm registry when set, which tests do.
	NPM string
	// PyPI replaces the URL of the Python Package Index when set.
	PyPI string
	// GoProxy replaces the URL of the Go module proxy when set.
	GoProxy string
	// Crates and CrateDownloads replace the URLs of the crates.io API and of
	// its downloads when set.
	Crates, CrateDownloads string
}

// Release is one installable version and the upstream tag it came from.
type Release struct {
	Version string
	Tag     string
	// Commit is the commit a moving tag points at. It is empty for every other
	// release.
	Commit string
	// Digests maps a download URL of the release to the sha256 that the host
	// reports for it.
	Digests map[string]string
	// Integrity maps the download URL of an npm version to the digest the
	// registry publishes for it, such as "sha512-...".
	Integrity map[string]string
}

// Pick returns the release of v to install. An empty want selects the newest.
// Otherwise want is an exact version, a prefix such as "22" for the newest 22.x,
// or a range such as "^1.4", as Matches reads it. A manifest with a fixed
// version has one release, whose tag equals its version.
func (r *Resolver) Pick(ctx context.Context, v manifest.Version, want string) (Release, error) {
	if v.From == "" {
		ok, err := Matches(v.Value, want)
		if err != nil {
			return Release{}, err
		}

		if !ok {
			return Release{}, fmt.Errorf("the manifest provides version %s, not %s", v.Value, want)
		}

		return Release{Version: v.Value, Tag: v.Value}, nil
	}

	if IsRange(want) {
		return r.PickWithin(ctx, v, want)
	}

	releases, err := r.List(ctx, v)
	if err != nil {
		return Release{}, err
	}

	if len(releases) == 0 {
		return Release{}, fmt.Errorf("%s %s has no versions", v.From, v.Repo)
	}

	if want == "" {
		return releases[0], nil
	}

	for _, release := range releases {
		if release.Version == want {
			return release, nil
		}
	}

	// The releases are newest first, with every prerelease behind them.
	for _, release := range releases {
		if strings.HasPrefix(release.Version, want+".") {
			return release, nil
		}
	}

	newest := releases[:min(5, len(releases))]
	names := make([]string, len(newest))

	for i, release := range newest {
		names[i] = release.Version
	}

	return Release{}, fmt.Errorf(
		"%s %s has no version %s, the newest are %s",
		v.From, v.Repo, want, strings.Join(names, ", "),
	)
}

// PickWithin returns the newest release of v that satisfies constraint, such as
// ">=3" or ">=1.2, <2". An empty constraint accepts every version.
func (r *Resolver) PickWithin(
	ctx context.Context,
	v manifest.Version,
	constraint string,
) (Release, error) {
	releases := []Release{{Version: v.Value, Tag: v.Value}}

	if v.From != "" {
		var err error
		if releases, err = r.List(ctx, v); err != nil {
			return Release{}, err
		}
	}

	var seen []string

	for _, release := range releases {
		ok, err := Satisfies(release.Version, constraint)
		if err != nil {
			return Release{}, err
		}

		if ok {
			return release, nil
		}

		if len(seen) < 5 {
			seen = append(seen, release.Version)
		}
	}

	return Release{}, fmt.Errorf(
		"no version satisfies %q, the versions found are %s", constraint, strings.Join(seen, ", "),
	)
}

// IsRange reports whether want is a range for Satisfies, which starts with an
// operator or holds several parts, and not a version or a prefix of one.
func IsRange(want string) bool {
	return want != "" && (strings.ContainsAny(want[:1], "^~<>=") || strings.Contains(want, ","))
}

// Matches reports whether version is one that want selects. An empty want
// selects every version, a range those that satisfy it, and a version itself
// and every version it is a prefix of, so "22" selects 22.1.0.
func Matches(version, want string) (bool, error) {
	switch {
	case want == "":
		return true, nil
	case IsRange(want):
		return Satisfies(version, want)
	default:
		return version == want || strings.HasPrefix(version, want+"."), nil
	}
}

// Satisfies reports whether version meets every comma-separated part of
// constraint. A part is an operator (>=, >, <=, <, =, ^, ~) and a version, and a
// bare version means "=". As in npm, ^1.4 allows versions below 2 and ^0.4
// those below 0.5, and ~1.4 allows versions below 1.5.
func Satisfies(version, constraint string) (bool, error) {
	for _, part := range strings.Split(constraint, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		op := "="

		for _, candidate := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
			if rest, ok := strings.CutPrefix(part, candidate); ok {
				op, part = candidate, strings.TrimSpace(rest)

				break
			}
		}

		if part == "" || !unicode.IsDigit(rune(part[0])) {
			return false, fmt.Errorf(
				"version constraint %q: want an operator and a version, such as >=1.2",
				constraint,
			)
		}

		order := Compare(version, part)

		if op == "^" || op == "~" {
			below, err := ceiling(part, op)
			if err != nil {
				return false, fmt.Errorf("version constraint %q: %w", constraint, err)
			}

			if order < 0 || Compare(version, below) >= 0 {
				return false, nil
			}

			continue
		}

		met := map[string]bool{
			">=": order >= 0, ">": order > 0, "<=": order <= 0, "<": order < 0, "=": order == 0,
		}[op]
		if !met {
			return false, nil
		}
	}

	return true, nil
}

// ceiling returns the first version that ^part or ~part excludes. ~ raises
// the second number, or the first when part has one. ^ raises the first number
// that is not 0, or the last when all are 0.
func ceiling(part, op string) (string, error) {
	fields := strings.Split(part, ".")
	nums := make([]int, len(fields))

	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return "", fmt.Errorf("%s%s: want numbers, such as %s1.4", op, part, op)
		}

		nums[i] = n
	}

	raise := min(1, len(nums)-1)
	if op == "^" {
		raise = len(nums) - 1

		for i, n := range nums {
			if n != 0 {
				raise = i

				break
			}
		}
	}

	out := make([]string, raise+1)
	for i := range raise {
		out[i] = strconv.Itoa(nums[i])
	}

	out[raise] = strconv.Itoa(nums[raise] + 1)

	return strings.Join(out, "."), nil
}

type memoKey struct{}

// memo holds the releases that List found during one command, by version source.
type memo struct {
	mu    sync.Mutex
	lists map[string]*listing
}

type listing struct {
	once     sync.Once
	releases []Release
	err      error
}

// WithMemo returns a context in which List asks each version source once.
// Packages of one command that share a source, and a second lookup of one
// package, cost no more requests. It covers git tags and branches, which
// forge.WithAnswers does not see.
func WithMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey{}, &memo{lists: map[string]*listing{}})
}

// List returns the releases of v, newest first.
func (r *Resolver) List(ctx context.Context, v manifest.Version) ([]Release, error) {
	m, _ := ctx.Value(memoKey{}).(*memo)
	if m == nil {
		return r.list(ctx, v)
	}

	key := fmt.Sprintf("%#v", v)

	m.mu.Lock()

	l := m.lists[key]
	if l == nil {
		l = &listing{}
		m.lists[key] = l
	}

	m.mu.Unlock()

	l.once.Do(func() { l.releases, l.err = r.list(ctx, v) })

	return l.releases, l.err
}

func (r *Resolver) list(ctx context.Context, v manifest.Version) ([]Release, error) {
	defer status.Start(ctx, "looking up the versions of %s", v.Repo)()

	if v.Tag != "" {
		release, err := r.movingTag(ctx, v)
		if err != nil {
			return nil, err
		}

		return []Release{release}, nil
	}

	if v.From == manifest.FromNPM {
		return r.npmVersions(ctx, v.Repo)
	}

	if v.From == manifest.FromPyPI {
		return r.pypiVersions(ctx, v.Repo)
	}

	if v.From == manifest.FromGo {
		return r.goVersions(ctx, v.Repo)
	}

	if v.From == manifest.FromCrates {
		return r.crateVersions(ctx, v.Repo)
	}

	if v.From == manifest.FromSparkle {
		release, err := r.sparkle(ctx, v)
		if err != nil {
			return nil, err
		}

		return []Release{release}, nil
	}

	if v.From == manifest.FromRedirect || v.From == manifest.FromPage {
		release, err := r.scrape(ctx, v)
		if err != nil {
			return nil, err
		}

		return []Release{release}, nil
	}

	if v.From == manifest.FromGitBranch {
		release, err := branchHead(ctx, v)
		if err != nil {
			return nil, err
		}

		return []Release{release}, nil
	}

	var (
		tags    []string
		digests map[string]map[string]string
		err     error
	)

	switch v.From {
	case manifest.FromGitHubReleases, manifest.FromGiteaReleases, manifest.FromGitLabReleases:
		tags, digests, err = r.published(ctx, v)
	case manifest.FromGitTags:
		tags, err = gitTags(ctx, v.Repo)
	default:
		return nil, fmt.Errorf("version.from %q is not supported", v.From)
	}

	if err != nil {
		return nil, err
	}

	var releases []Release

	// Many repos switched once between tags such as 1.2.0 and v1.2.0, so a "v"
	// is optional either way. When a version has both tags, oku keeps the one in
	// the declared form.
	at := map[string]int{}

	for _, tag := range tags {
		version, ok := strings.CutPrefix(tag, v.StripPrefix)

		declared := ok
		if !ok && v.StripPrefix == "v" {
			version, ok = tag, true
		} else if ok && v.StripPrefix == "" && strings.HasPrefix(tag, "v") {
			version, declared = tag[1:], false
		}

		if !ok || version == "" || !unicode.IsDigit(rune(version[0])) {
			continue
		}

		if i, seen := at[version]; seen {
			if declared {
				releases[i].Tag, releases[i].Digests = tag, digests[tag]
			}

			continue
		}

		at[version] = len(releases)
		releases = append(releases, Release{Version: version, Tag: tag, Digests: digests[tag]})
	}

	// A prerelease sorts behind every release, so the newest is never one, and
	// `add <ref>@x` still finds it.
	slices.SortFunc(releases, func(a, b Release) int {
		preA, preB := prerelease.MatchString(a.Version), prerelease.MatchString(b.Version)

		switch {
		case preA && !preB:
			return 1
		case preB && !preA:
			return -1
		default:
			return Compare(b.Version, a.Version)
		}
	})

	return releases, nil
}

// movingTag returns the one release of a tag that upstream moves, such as
// "nightly". Its version is the day of the commit the tag points at and that
// commit, such as 2026.09.20-a73243f, so one commit always has one version. A
// prerelease counts, a draft does not.
func (r *Resolver) movingTag(ctx context.Context, v manifest.Version) (Release, error) {
	tag := v.Tag
	what := "read the release " + tag + " of " + v.Repo

	host, repo, err := r.open(v)
	if err != nil {
		return Release{}, err
	}

	found, err := host.Release(ctx, repo, tag)
	if err != nil {
		return Release{}, explain(err, what, "the repository has no such release")
	}

	if found.Draft {
		return Release{}, fmt.Errorf("%s: the release is a draft", what)
	}

	commit, err := host.TagCommit(ctx, repo, tag)
	if err != nil {
		return Release{}, explain(err, what, "the repository has no such tag")
	}

	if len(commit.SHA) < 7 {
		return Release{}, fmt.Errorf("%s: the host named no commit for the tag", what)
	}

	return Release{
		Version: commit.Date.UTC().Format("2006.01.02") + "-" + commit.SHA[:7],
		Tag:     tag,
		Commit:  commit.SHA,
		Digests: assetDigests(found),
	}, nil
}

// open returns the forge that v reads releases from.
func (r *Resolver) open(v manifest.Version) (forge.Forge, string, error) {
	return r.Hosts.Open(strings.TrimSuffix(v.From, "-releases"), v.Repo)
}

// published returns the tags of published releases, and the digests of each
// one's files by tag. It skips drafts and prereleases.
func (r *Resolver) published(
	ctx context.Context,
	v manifest.Version,
) ([]string, map[string]map[string]string, error) {
	host, repo, err := r.open(v)
	if err != nil {
		return nil, nil, err
	}

	found, err := host.Releases(ctx, repo)
	if err != nil {
		return nil, nil, explain(err, "list releases of "+v.Repo, "the repository was not found")
	}

	var tags []string

	digests := map[string]map[string]string{}

	for _, release := range found {
		if !release.Draft && !release.Prerelease {
			tags = append(tags, release.Tag)
			digests[release.Tag] = assetDigests(release)
		}
	}

	return tags, digests, nil
}

// assetDigests maps the download URL of each file of release to the sha256
// that the host reports for it. GitHub reports one for most files uploaded
// since mid 2025, and other hosts report none.
func assetDigests(release forge.Release) map[string]string {
	digests := map[string]string{}

	for _, asset := range release.Assets {
		if asset.Digest != "" {
			digests[asset.URL] = asset.Digest
		}
	}

	return digests
}

// explain names the request in a forge's error. missing says what a missing
// page means for it.
func explain(err error, what, missing string) error {
	if errors.Is(err, forge.ErrNotFound) {
		return fmt.Errorf("%s: %s", what, missing)
	}

	return fmt.Errorf("%s: %w", what, err)
}

// branchHead returns the one release of a branch, which is its newest commit.
// Its version has the form of a moving tag's, such as 2026.09.20-a73243f, and
// its tag is the branch. The clone holds that commit and no files.
func branchHead(ctx context.Context, v manifest.Version) (Release, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Release{}, errors.New(`version.from = "git-branch" needs git on PATH`)
	}

	what := "read the branch " + v.Branch + " of " + v.Repo

	dir, err := tempdir.Dir("branch")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(dir)

	clone := exec.CommandContext(
		ctx, "git", "clone", "--quiet", "--bare", "--depth", "1", "--filter=tree:0",
		"--single-branch", "--branch", v.Branch, "--", v.Repo, dir,
	)
	// A credential prompt would hang a non-interactive install.
	clone.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if out, err := clone.CombinedOutput(); err != nil {
		return Release{}, fmt.Errorf("%s: %w: %s", what, err, strings.TrimSpace(string(out)))
	}

	// git log would fetch the commit's tree, which the clone left out, and so
	// ask the host a second time. for-each-ref reads only the commit.
	out, err := exec.CommandContext(
		ctx, "git", "-C", dir, "for-each-ref", "--format=%(objectname) %(committerdate:unix)",
		"refs/heads/"+v.Branch,
	).Output()
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w", what, err)
	}

	sha, seconds, _ := strings.Cut(strings.TrimSpace(string(out)), " ")

	unix, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || len(sha) < 7 {
		return Release{}, fmt.Errorf("%s: git printed %q for its newest commit", what, out)
	}

	return Release{
		Version: time.Unix(unix, 0).UTC().Format("2006.01.02") + "-" + sha[:7],
		Tag:     v.Branch,
		Commit:  sha,
	}, nil
}

func gitTags(ctx context.Context, url string) ([]string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New(`version.from = "git-tags" needs git on PATH`)
	}

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--refs", "--", url)
	// A credential prompt would hang a non-interactive install.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tags of %s: %w", url, err)
	}

	var tags []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if _, name, ok := strings.Cut(line, "refs/tags/"); ok {
			tags = append(tags, name)
		}
	}

	return tags, nil
}

// Compare orders two versions by their dot-separated numbers, so 1.10.0 is newer
// than 1.9.0. A version with a "-" suffix, such as 2.0.0-rc1, is older than the
// same version without one. Two suffixes compare piece by piece, and a piece
// that is a number compares as one, so 7.1.2-31 is newer than 7.1.2-9 and rc10
// is newer than rc9. A number with letters behind it compares as the number
// first, so 1.9rc2 is older than 1.26. When the numbers are equal, 1.26rc1 is
// older than 1.26 because rc is a prerelease word, and 1.1.1w is newer than
// 1.1.1 because w is not. It returns -1, 0 or 1.
func Compare(a, b string) int {
	coreA, preA, _ := strings.Cut(a, "-")
	coreB, preB, _ := strings.Cut(b, "-")

	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")

	for i := range max(len(partsA), len(partsB)) {
		var x, y string
		if i < len(partsA) {
			x = partsA[i]
		}

		if i < len(partsB) {
			y = partsB[i]
		}

		if order := comparePart(x, y); order != 0 {
			return order
		}
	}

	switch {
	case preA == "" && preB != "":
		return 1
	case preA != "" && preB == "":
		return -1
	default:
		return compareSuffix(preA, preB)
	}
}

// comparePart orders two dot-separated parts of a version.
func comparePart(x, y string) int {
	numX, tailX := splitNumber(x)
	numY, tailY := splitNumber(y)

	nx, errX := strconv.Atoi(numX)
	ny, errY := strconv.Atoi(numY)

	switch {
	case errX != nil || errY != nil:
		return strings.Compare(x, y)
	case nx != ny:
		return cmp.Compare(nx, ny)
	case tailX == "" && tailY == "":
		return 0
	case tailX == "":
		return tailRank(tailY)
	case tailY == "":
		return -tailRank(tailX)
	default:
		return compareSuffix(tailX, tailY)
	}
}

// splitNumber cuts s behind its leading digits, so "9rc2" is "9", "rc2".
func splitNumber(s string) (number, tail string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}

	return s[:i], s[i:]
}

// tailRank compares a bare number to the same number with tail behind it. It
// returns 1 when tail is a prerelease word, and -1 for any other letters.
func tailRank(tail string) int {
	if prerelease.MatchString(tail) {
		return 1
	}

	return -1
}

// prerelease matches a word that marks a version as a prerelease, such as the
// rc in 1.26rc1 or 2.0.0-rc.1.
var prerelease = regexp.MustCompile(
	`(?i)(^|[^a-z])(rc|alpha|beta|pre|preview|dev|snapshot)([^a-z]|$)`,
)

// compareSuffix orders two "-" suffixes. It splits each into runs of digits and
// runs of anything else, and compares run by run.
func compareSuffix(a, b string) int {
	runsA, runsB := runs(a), runs(b)

	for i := range min(len(runsA), len(runsB)) {
		x, y := runsA[i], runsB[i]

		nx, errX := strconv.Atoi(x)
		ny, errY := strconv.Atoi(y)

		switch {
		case errX == nil && errY == nil && nx != ny:
			return cmp.Compare(nx, ny)
		case (errX != nil || errY != nil) && x != y:
			return strings.Compare(x, y)
		}
	}

	return cmp.Compare(len(runsA), len(runsB))
}

// runs splits s where a digit meets another character, so "rc10" is "rc", "10".
func runs(s string) []string {
	var out []string

	start := 0

	for i := 1; i <= len(s); i++ {
		if i == len(s) || isDigit(s[i]) != isDigit(s[i-1]) {
			out = append(out, s[start:i])
			start = i
		}
	}

	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
