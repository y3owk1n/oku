// Package manifest reads and validates package manifests.
package manifest

import (
	"errors"
	"fmt"
	"os"
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
}

type Package struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Homepage    string `toml:"homepage"`
	License     string `toml:"license"`
}

type Version struct {
	Value string `toml:"value"`
	From  string `toml:"from"`
}

// Artifact is a prebuilt download for the platforms its selector matches.
type Artifact struct {
	Match       platform.Selector `toml:"match"`
	URL         string            `toml:"url"`
	SHA256      string            `toml:"sha256"`
	Strip       int               `toml:"strip"`
	Bin         []string          `toml:"bin"`
	Man         []string          `toml:"man"`
	Completions map[string]string `toml:"completions"`
}

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	templateRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)
)

// Load reads and validates the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest %s: %w", path, err)
	}

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
	case m.Version.From != "":
		errs = append(errs, fmt.Errorf(
			"version.from %q is not supported yet, set version.value",
			m.Version.From,
		))
	case m.Version.Value == "":
		errs = append(errs, errors.New("version.value is required"))
	}

	for i, a := range m.Artifacts {
		if a.URL == "" {
			errs = append(errs, fmt.Errorf("artifact[%d]: url is required", i))
		}

		if !sha256Re.MatchString(a.SHA256) {
			errs = append(errs, fmt.Errorf(
				"artifact[%d]: sha256 must be 64 lowercase hex characters",
				i,
			))
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
// template variables in its URL.
func (m *Manifest) Select(p platform.Platform) (Artifact, bool, error) {
	for _, a := range m.Artifacts {
		if !a.Match.Matches(p) {
			continue
		}

		url, err := expand(a.URL, map[string]string{
			"version": m.Version.Value,
			"os":      p.OS,
			"arch":    p.Arch,
			"libc":    p.Libc,
		})
		if err != nil {
			return Artifact{}, false, fmt.Errorf("artifact url: %w", err)
		}

		a.URL = url

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
