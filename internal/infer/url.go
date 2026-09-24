package infer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/platform"
)

// versionRe finds a version in a file name, such as the "1.2.3" of
// "tool-v1.2.3-linux-amd64.tar.gz".
var versionRe = regexp.MustCompile(`(^|[-_.])v?(\d+(\.\d+)+)`)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// platformless cuts stem before its first OS or arch word, so that
// "tool-linux-amd64" gives "tool".
func platformless(stem string) string {
	lower := strings.ToLower(stem)
	cut := len(stem)

	for _, words := range []map[string][]string{osWords, archWords} {
		for _, list := range words {
			for _, word := range list {
				for _, sep := range "-_." {
					if i := strings.Index(lower, string(sep)+word); i > 0 && i < cut {
						cut = i
					}
				}
			}
		}
	}

	return stem[:cut]
}

// FromURL returns manifest TOML for a download that is the package itself. The
// manifest has one artifact, for host, because one URL is a download for one
// platform. bins are the file names of the programs, or none to let FromURL
// find them.
func (inf *Inferrer) FromURL(
	ctx context.Context,
	at string,
	host platform.Platform,
	bins []string,
) (string, error) {
	parsed, err := url.Parse(at)
	if err != nil {
		return "", err
	}

	asset := path.Base(parsed.Path)
	if asset == "" || asset == "." || asset == "/" {
		return "", fmt.Errorf("%s: the URL names no file", at)
	}

	// The name is the file name up to its version. Without a version it is the
	// name up to the first OS or arch word, and the version is "0".
	name, version := platformless(asset[:len(asset)-len(ending(asset))]), "0"
	if m := versionRe.FindStringSubmatchIndex(asset); m != nil && m[0] > 0 {
		name, version = asset[:m[0]], asset[m[4]:m[5]]
	} else if v := folderVersion(parsed.Path); v != "" {
		version = v
	}

	name = strings.ToLower(name)
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf(
			"%s: cannot take a package name from %q, write a manifest for it",
			at,
			asset,
		)
	}

	files, err := inf.Inspect(ctx, at, forge.Auth{})
	if errors.Is(err, ErrWebPage) {
		return "", fmt.Errorf("%s: %w. %s", at, err, pageHint(parsed))
	}

	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", asset, err)
	}

	l, err := findLayout(files, name, bins, isArchive(asset))
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset, err)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "[package]\nname = %q\n\n[version]\nvalue = %q\n\n", name, version)
	// The artifact has no sha256, so the store pins the download's digest in
	// oku.lock and tells the user that it trusted the first download.
	fmt.Fprintf(
		&b, "[[artifact]]\nmatch = %s\nurl = %q\n",
		selectorTOML(platform.Selector{OS: host.OS, Arch: host.Arch}), at,
	)

	b.WriteString(l.toml(host.OS))

	return b.String(), nil
}

// pageHint says what to give oku in place of the web page at u. The page of a
// repo on a forge has a ref of its own.
func pageHint(u *url.URL) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || u.RawQuery != "" {
		return "Give the URL of the file to download"
	}

	repo := strings.Join(parts, "/")

	switch strings.ToLower(u.Host) {
	case "github.com":
		return "It is a repo on GitHub, so add github:" + repo
	case "gitlab.com":
		return "It is a repo on GitLab, so add gitlab:" + repo
	case "codeberg.org":
		return "It is a repo on Codeberg, so add codeberg:" + repo
	}

	return fmt.Sprintf(
		"For a repo on a Gitea or Forgejo server add gitea:%[1]s/%[2]s, on a GitLab server gitlab:%[1]s/%[2]s, "+
			"or give the URL of the file to download",
		u.Host, repo,
	)
}

// folderRe matches a folder that names a version, such as "v1.19.0" or
// "jq-1.8.1".
var folderRe = regexp.MustCompile(`^(?:.*[-_])?v?(\d+(?:\.\d+)+)$`)

// folderVersion returns the version that the folder nearest the file in p
// names, or "" when none does. A release often keeps its files in a folder
// named after its tag, as GitHub does.
func folderVersion(p string) string {
	parts := strings.Split(path.Dir(p), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if m := folderRe.FindStringSubmatch(parts[i]); m != nil {
			return m[1]
		}
	}

	return ""
}
