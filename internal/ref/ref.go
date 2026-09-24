// Package ref parses refs and fetches the manifests they point at.
package ref

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/y3owk1n/oku/internal/crates"
	"github.com/y3owk1n/oku/internal/goproxy"
	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/pypi"
)

// Kind is the type of place a ref points at.
type Kind int

const (
	File Kind = iota
	HTTP
	// Forge is a repo on a host whose API oku reads. Ref.Scheme says which.
	Forge
	Git
	// NPM is a package in the npm registry. It has no manifest, so oku always
	// infers one.
	NPM
	// PyPI is a package in the Python Package Index. oku always infers its
	// manifest too.
	PyPI
	// Go is a Go package that builds a program, such as golang.org/x/tools/gopls.
	// oku infers its manifest from the module proxy.
	Go
	// Cargo is a crate on crates.io that builds programs. oku infers its
	// manifest too.
	Cargo
	// Cask is a Homebrew cask, a recipe that oku translates into a manifest of
	// its own.
	Cask
	// Scoop is a Scoop manifest, translated the same way.
	Scoop
	// Aqua is a GitHub repo's entry in the aqua registry, translated the same
	// way.
	Aqua
	// Winget is a package of winget's community manifests, translated the same
	// way.
	Winget
)

// Target is the kind of file Fetch reads a ref as. It sets the file names Fetch
// looks for in a repo.
type Target struct {
	// Default is the file read when the ref has no fragment.
	Default string
	// Dir is the directory searched for "<fragment>.toml" after the repo root.
	Dir string
}

var (
	// Manifest reads a package manifest.
	Manifest = Target{Default: "oku.pkg.toml", Dir: "packages"}
	// List reads a package list.
	List = Target{Default: "oku.toml", Dir: "lists"}
)

// Ref is a parsed pointer to a manifest.
type Ref struct {
	Kind Kind
	// Location is an absolute path (File), a URL (HTTP), "owner/repo" or
	// "host/owner/repo" (Forge), or a repository URL (Git).
	Location string
	// Scheme is "github", "gitea", "codeberg" or "gitlab" for a Forge ref, else "".
	Scheme string
	// Fragment is the text after "#". For a Forge ref it is a manifest name or a
	// path inside the repository, for Git a path.
	Fragment string
	// Version is the text after "@", empty when the ref does not pin one.
	Version string
}

var (
	// githubRe takes "owner/repo" and, for a GitHub Enterprise Server,
	// "host/owner/repo". A host has a dot, which an owner cannot have.
	githubRe = regexp.MustCompile(
		`^([A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+/)?[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`,
	)
	// An owner on a Gitea or Forgejo server may have a dot, so "gitea:" always
	// names the host and "codeberg:" never does.
	giteaRe = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+/[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`,
	)
	// gitlabRe takes a project in any depth of groups, with an optional host in
	// front.
	gitlabRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)+$`)
	codebergRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)
	npmRe      = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	gitSchema  = []string{"https://", "http://", "ssh://", "file://"}
)

// Parse reads a ref such as "./rg.toml", "https://host/rg.toml",
// "github:owner/repo#name@1.2.0", "codeberg:owner/repo" or "git+https://host/repo#path/rg.toml".
func Parse(s string) (Ref, error) {
	return ParseIn("", s)
}

// ParseIn is Parse with relative file paths resolved against dir. An empty dir
// means the working directory.
func ParseIn(dir, s string) (Ref, error) {
	if s == "" {
		return Ref{}, errors.New("empty ref")
	}

	var r Ref

	inDir := func(path string) string {
		if dir == "" || filepath.IsAbs(path) || strings.Contains(path, ":") {
			return path
		}

		return filepath.Join(dir, path)
	}

	body := s
	// The "@" of a cask such as cask:temurin@21 is part of its name, so a cask
	// ref takes no version.
	if _, err := os.Stat(inDir(s)); err != nil && !strings.HasPrefix(s, "cask:") {
		body, r.Version = splitVersion(s)
	}

	switch {
	case hasAnyPrefix(body, []string{"github:", "gitea:", "codeberg:", "gitlab:"}):
		r.Kind = Forge
		r.Scheme, body, _ = strings.Cut(body, ":")
		r.Location, r.Fragment, _ = strings.Cut(body, "#")

		switch {
		case r.Scheme == "github" && !githubRe.MatchString(r.Location):
			return Ref{}, fmt.Errorf(
				"%s: want github:owner/repo, github:owner/repo#name or github:host/owner/repo", s,
			)
		case r.Scheme == "gitea" && !giteaRe.MatchString(r.Location):
			return Ref{}, fmt.Errorf(
				"%s: want gitea:host/owner/repo or gitea:host/owner/repo#name",
				s,
			)
		case r.Scheme == "gitlab" && !gitlabRe.MatchString(r.Location):
			return Ref{}, fmt.Errorf(
				"%s: want gitlab:group/project, gitlab:group/project#name or gitlab:host/group/project",
				s,
			)
		case r.Scheme == "codeberg" && !codebergRe.MatchString(r.Location):
			return Ref{}, fmt.Errorf("%s: want codeberg:owner/repo or codeberg:owner/repo#name", s)
		}

		switch {
		case isPath(r.Fragment) && !filepath.IsLocal(filepath.FromSlash(r.Fragment)):
			return Ref{}, fmt.Errorf("%s: %q is outside the repository", s, r.Fragment)
		case r.Fragment != "" && !isPath(r.Fragment) && !nameRe.MatchString(r.Fragment):
			return Ref{}, fmt.Errorf("%s: %q is not a manifest name", s, r.Fragment)
		}
	case strings.HasPrefix(body, "npm:"):
		r.Kind = NPM
		r.Location = strings.TrimPrefix(body, "npm:")

		if !npmRe.MatchString(r.Location) {
			return Ref{}, fmt.Errorf("%s: want npm:name or npm:@scope/name", s)
		}
	case strings.HasPrefix(body, "pypi:"):
		r.Kind = PyPI
		r.Location = strings.TrimPrefix(body, "pypi:")

		if !pypi.ValidName(r.Location) {
			return Ref{}, fmt.Errorf("%s: want pypi:name", s)
		}
	case strings.HasPrefix(body, "go:"):
		r.Kind = Go
		r.Location = strings.TrimPrefix(body, "go:")

		if !goproxy.ValidPath(r.Location) {
			return Ref{}, fmt.Errorf("%s: want go:host/path, such as go:golang.org/x/tools/gopls", s)
		}
	case strings.HasPrefix(body, "cargo:"):
		r.Kind = Cargo
		r.Location = strings.TrimPrefix(body, "cargo:")

		if !crates.ValidName(r.Location) {
			return Ref{}, fmt.Errorf("%s: want cargo:name, such as cargo:ripgrep", s)
		}
	case strings.HasPrefix(body, "cask:"):
		r.Kind = Cask
		r.Location = strings.TrimPrefix(body, "cask:")

		if !infer.ValidCask(r.Location) {
			return Ref{}, fmt.Errorf("%s: want cask:name, such as cask:rectangle, or cask:owner/tap/name of another tap", s)
		}
	case strings.HasPrefix(body, "scoop:"):
		r.Kind = Scoop
		r.Location = strings.TrimPrefix(body, "scoop:")

		if !infer.ValidScoop(r.Location) {
			return Ref{}, fmt.Errorf(
				"%s: want scoop:name, scoop:bucket/name with a bucket that Scoop knows by name, or scoop:owner/repo/name of a bucket on GitHub", s,
			)
		}
	case strings.HasPrefix(body, "aqua:"):
		r.Kind = Aqua
		r.Location = strings.TrimPrefix(body, "aqua:")

		if !codebergRe.MatchString(r.Location) {
			return Ref{}, fmt.Errorf("%s: want aqua:owner/repo, such as aqua:BurntSushi/ripgrep", s)
		}
	case strings.HasPrefix(body, "winget:"):
		r.Kind = Winget
		r.Location = strings.TrimPrefix(body, "winget:")

		if !infer.ValidWinget(r.Location) {
			return Ref{}, fmt.Errorf("%s: want winget:Publisher.Package, such as winget:jqlang.jq", s)
		}
	case strings.HasPrefix(body, "git+"):
		r.Kind = Git
		r.Location, r.Fragment, _ = strings.Cut(strings.TrimPrefix(body, "git+"), "#")

		if !hasAnyPrefix(r.Location, gitSchema) {
			return Ref{}, fmt.Errorf("%s: want git+https://, git+ssh:// or git+file://", s)
		}

		if r.Fragment != "" && !filepath.IsLocal(filepath.FromSlash(r.Fragment)) {
			return Ref{}, fmt.Errorf("%s: %q is outside the repository", s, r.Fragment)
		}
	case strings.HasPrefix(body, "https://"), strings.HasPrefix(body, "http://"):
		r.Kind = HTTP
		r.Location = body
	case strings.Contains(body, "://"):
		return Ref{}, fmt.Errorf("%s: unsupported scheme", s)
	default:
		abs, err := filepath.Abs(inDir(body))
		if err != nil {
			return Ref{}, err
		}

		r.Kind = File
		r.Location = abs
	}

	return r, nil
}

// String renders the ref without its version. oku.toml and oku.lock store this
// form.
func (r Ref) String() string {
	s := r.Location

	switch r.Kind {
	case Forge:
		s = r.Scheme + ":" + s
	case Git:
		s = "git+" + s
	case NPM:
		s = "npm:" + s
	case PyPI:
		s = "pypi:" + s
	case Go:
		s = "go:" + s
	case Cargo:
		s = "cargo:" + s
	case Cask:
		s = "cask:" + s
	case Scoop:
		s = "scoop:" + s
	case Aqua:
		s = "aqua:" + s
	case Winget:
		s = "winget:" + s
	}

	if r.Fragment != "" {
		s += "#" + r.Fragment
	}

	return s
}

// InDir renders the absolute file path s as "./path" when the file is inside
// dir, so a list or lock in dir still finds the file in another checkout. Any
// other ref comes back unchanged.
func InDir(dir, s string) string {
	if !filepath.IsAbs(s) {
		return s
	}

	rel, err := filepath.Rel(dir, s)
	if err != nil || !filepath.IsLocal(rel) {
		return s
	}

	return "./" + filepath.ToSlash(rel)
}

// FromDir undoes InDir.
func FromDir(dir, s string) string {
	if !strings.HasPrefix(s, "./") {
		return s
	}

	return filepath.Join(dir, filepath.FromSlash(s))
}

// isPath reports whether a fragment is a path in the repository, such as
// "packages/fd.toml", and not a manifest name, such as "fd".
func isPath(fragment string) bool {
	return strings.Contains(fragment, "/") || strings.HasSuffix(fragment, ".toml")
}

// IsRelative reports whether s is a relative file path, such as
// "./packages/fd.toml" or "../base.toml", and no other kind of ref.
func IsRelative(s string) bool {
	return !strings.Contains(s, ":") && !filepath.IsAbs(s) && !strings.HasPrefix(s, "/")
}

// Beside returns the ref of the file at rel, a relative path, beside the file
// that got read from r. In a repo it is the file of the same repo, and for a URL
// the URL beside it. A path that leaves the repo fails. To read the file at the
// same commit, the caller passes got.Commit to Fetch.
func Beside(r Ref, got Fetched, rel string) (Ref, error) {
	rel = filepath.ToSlash(rel)

	switch r.Kind {
	case HTTP:
		base, err := url.Parse(got.Path)
		if err != nil {
			return Ref{}, err
		}

		at, err := url.Parse(rel)
		if err != nil {
			return Ref{}, err
		}

		return Ref{Kind: HTTP, Location: base.ResolveReference(at).String()}, nil
	case Forge, Git:
		at := path.Join(path.Dir(got.Path), rel)
		if !filepath.IsLocal(filepath.FromSlash(at)) {
			return Ref{}, fmt.Errorf("%s: %s is outside the repository", r, rel)
		}

		return Ref{Kind: r.Kind, Scheme: r.Scheme, Location: r.Location, Fragment: at}, nil
	default:
		return Ref{}, fmt.Errorf("%s: %s is not beside a file in a repo or at a URL", r, rel)
	}
}

// splitVersion cuts "@version" off the end of s. A version holds no "/" or ":",
// so splitVersion does not read the "@" in ssh://git@host/repo as one.
func splitVersion(s string) (string, string) {
	i := strings.LastIndex(s, "@")
	if i <= 0 || i == len(s)-1 || strings.ContainsAny(s[i+1:], "/:#") {
		return s, ""
	}

	return s[:i], s[i+1:]
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}

	return false
}
