// Package manifest reads and validates package manifests.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/platform"
)

// Manifest is one TOML file describing one package.
type Manifest struct {
	Package   Package        `toml:"package"`
	Version   Version        `toml:"version"`
	Artifacts []Artifact     `toml:"artifact"`
	Build     map[string]any `toml:"build"`

	// SHA256 is the hex digest of the manifest data. The store hash includes it.
	SHA256 string `toml:"-"`
	// Tag is the upstream tag of the chosen version. {{tag}} expands to it.
	Tag string `toml:"-"`
}

type Package struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Homepage    string `toml:"homepage"`
	License     string `toml:"license"`
}

// Version is either fixed by Value or discovered from From.
type Version struct {
	Value string `toml:"value"`
	// From is FromGitHubReleases or FromGitTags.
	From string `toml:"from"`
	// Repo is "owner/repo" for GitHub releases and a git URL for git tags.
	Repo string `toml:"repo"`
	// StripPrefix is cut off a tag to get the version, such as "v". A tag
	// without it is ignored.
	StripPrefix string `toml:"strip_prefix"`
}

const (
	FromGitHubReleases = "github-releases"
	FromGitTags        = "git-tags"
)

// Artifact is a prebuilt download for the platforms its selector matches.
type Artifact struct {
	Match       platform.Selector `toml:"match"`
	URL         string            `toml:"url"`
	SHA256      string            `toml:"sha256"`
	SHA256URL   string            `toml:"sha256_url"`
	Strip       int               `toml:"strip"`
	Bin         []string          `toml:"bin"`
	Man         []string          `toml:"man"`
	Completions map[string]string `toml:"completions"`
}

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	templateRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
)

// Parse validates manifest data. origin names the data in error messages.
func Parse(data []byte, origin string) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", origin, err)
	}

	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest %s: %w", origin, err)
	}

	sum := sha256.Sum256(data)
	m.SHA256 = hex.EncodeToString(sum[:])

	return &m, nil
}

func (m *Manifest) validate() error {
	var errs []error

	if !nameRe.MatchString(m.Package.Name) {
		errs = append(errs, fmt.Errorf(
			"package.name %q must be lowercase letters, digits, '.', '_' or '-'",
			m.Package.Name,
		))
	}

	switch {
	case m.Version.From == "" && m.Version.Value == "":
		errs = append(errs, errors.New("set version.value or version.from"))
	case m.Version.From != "" && m.Version.Value != "":
		errs = append(errs, errors.New("set version.value or version.from, not both"))
	case m.Version.From == FromGitHubReleases && !repoRe.MatchString(m.Version.Repo):
		errs = append(errs, errors.New(`version.repo must be "owner/repo" for github-releases`))
	case m.Version.From == FromGitTags && m.Version.Repo == "":
		errs = append(errs, errors.New("version.repo must be a git URL for git-tags"))
	case m.Version.From != "" && m.Version.From != FromGitHubReleases &&
		m.Version.From != FromGitTags:
		errs = append(errs, fmt.Errorf(
			"version.from %q must be %q or %q",
			m.Version.From, FromGitHubReleases, FromGitTags,
		))
	}

	for i, a := range m.Artifacts {
		if a.URL == "" {
			errs = append(errs, fmt.Errorf("artifact[%d]: url is required", i))
		}

		if a.SHA256 != "" && !sha256Re.MatchString(a.SHA256) {
			errs = append(errs, fmt.Errorf(
				"artifact[%d]: sha256 must be 64 lowercase hex characters",
				i,
			))
		}

		if a.SHA256 != "" && a.SHA256URL != "" {
			errs = append(errs, fmt.Errorf("artifact[%d]: set sha256 or sha256_url, not both", i))
		}

		if len(a.Bin)+len(a.Man)+len(a.Completions) == 0 {
			errs = append(errs, fmt.Errorf(
				"artifact[%d]: set at least one of bin, man or completions",
				i,
			))
		}

		if a.Strip < 0 {
			errs = append(errs, fmt.Errorf("artifact[%d]: strip must not be negative", i))
		}
	}

	return errors.Join(errs...)
}

// HasBuild reports whether the manifest declares a source build.
func (m *Manifest) HasBuild() bool {
	return len(m.Build) > 0
}

// Select returns the first artifact whose selector matches p and expands the
// template variables in its URLs.
func (m *Manifest) Select(p platform.Platform) (Artifact, bool, error) {
	for _, a := range m.Artifacts {
		if !a.Match.Matches(p) {
			continue
		}

		vars := map[string]string{
			"version": m.Version.Value,
			"tag":     m.Tag,
			"os":      p.OS,
			"arch":    p.Arch,
			"libc":    p.Libc,
		}

		var err error
		if a.URL, err = expand(a.URL, vars); err != nil {
			return Artifact{}, false, fmt.Errorf("artifact url: %w", err)
		}

		if a.SHA256URL, err = expand(a.SHA256URL, vars); err != nil {
			return Artifact{}, false, fmt.Errorf("artifact sha256_url: %w", err)
		}

		return a, true, nil
	}

	return Artifact{}, false, nil
}

// expand replaces {{name}} with vars[name] and fails on an unknown name.
func expand(s string, vars map[string]string) (string, error) {
	var unknown []string

	out := templateRe.ReplaceAllStringFunc(s, func(match string) string {
		name := templateRe.FindStringSubmatch(match)[1]

		v, ok := vars[name]
		if !ok {
			unknown = append(unknown, name)
		}

		return v
	})

	if len(unknown) > 0 {
		return "", fmt.Errorf("unknown template variable %q", unknown)
	}

	return out, nil
}
