// Package ref parses refs and fetches the manifests they point at.
package ref

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind is the type of place a ref points at.
type Kind int

const (
	File Kind = iota
	HTTP
	GitHub
	Git
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
	// "host/owner/repo" (GitHub), or a repository URL (Git).
	Location string
	// Fragment is the text after "#". For GitHub it is a manifest name, for Git
	// a path inside the repository.
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
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	gitSchema = []string{"https://", "http://", "ssh://", "file://"}
)

// Parse reads a ref such as "./rg.toml", "https://host/rg.toml",
// "github:owner/repo#name@1.2.0" or "git+https://host/repo#path/rg.toml".
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
	if _, err := os.Stat(inDir(s)); err != nil {
		body, r.Version = splitVersion(s)
	}

	switch {
	case strings.HasPrefix(body, "github:"):
		r.Kind = GitHub
		r.Location, r.Fragment, _ = strings.Cut(strings.TrimPrefix(body, "github:"), "#")

		if !githubRe.MatchString(r.Location) {
			return Ref{}, fmt.Errorf(
				"%s: want github:owner/repo, github:owner/repo#name or github:host/owner/repo", s,
			)
		}

		if r.Fragment != "" && !nameRe.MatchString(r.Fragment) {
			return Ref{}, fmt.Errorf("%s: %q is not a manifest name", s, r.Fragment)
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
	case GitHub:
		s = "github:" + s
	case Git:
		s = "git+" + s
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
