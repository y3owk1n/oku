// Package manifest reads and validates package manifests.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/platform"
)

// Manifest is one TOML file describing one package.
type Manifest struct {
	Package   Package    `toml:"package"`
	Version   Version    `toml:"version"`
	Artifacts []Artifact `toml:"artifact"`
	Build     *Build     `toml:"build"`
	Runtime   Runtime    `toml:"runtime"`
	// Apps holds launcher entries for Linux desktops.
	Apps []App `toml:"app"`
	// Services holds long-running programs the OS's service manager can run.
	Services []Service `toml:"service"`
	// Env holds variables the shell hook exports while the package is installed.
	// Values expand {{prefix}} and {{version}}.
	Env map[string]string `toml:"env"`

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
	// App holds macOS app bundles, such as "Foo.app". Font holds font files.
	App  []string `toml:"app"`
	Font []string `toml:"font"`
}

// Service is a long-running program, from a [[service]] table. Command is a
// path inside the installed package, such as "bin/food".
type Service struct {
	Name    string            `toml:"name"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	Restart string            `toml:"restart"`
}

// App is a launcher entry for Linux desktops, from a [[app]] table.
type App struct {
	Name string `toml:"name"`
	Exec string `toml:"exec"`
	Icon string `toml:"icon"`
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

	if m.Build != nil {
		for i, step := range m.Build.Steps {
			switch kinds := step.Kinds(); len(kinds) {
			case 0:
				errs = append(errs, fmt.Errorf(
					"build.step[%d]: needs one of run, install, patch, fetch, extract, copy, vendor",
					i,
				))
			case 1:
			default:
				errs = append(errs, fmt.Errorf(
					"build.step[%d]: has %s, a step takes exactly one",
					i,
					strings.Join(kinds, " and "),
				))
			}
		}

		deps, err := parseDeps(m.Build.RawDeps)
		if err != nil {
			errs = append(errs, fmt.Errorf("build.%w", err))
		}

		m.Build.Deps = deps
	}

	for i, app := range m.Apps {
		if app.Name == "" || app.Exec == "" {
			errs = append(errs, fmt.Errorf("app[%d]: name and exec are required", i))
		}
	}

	for i, svc := range m.Services {
		switch {
		case !nameRe.MatchString(svc.Name):
			errs = append(errs, fmt.Errorf(
				"service[%d]: name %q must be lowercase letters, digits, '.', '_' or '-'",
				i,
				svc.Name,
			))
		case svc.Command == "":
			errs = append(errs, fmt.Errorf("service[%d]: command is required", i))
		case svc.Restart != "" && svc.Restart != "never" && svc.Restart != "on-failure" &&
			svc.Restart != "always":
			errs = append(errs, fmt.Errorf(
				"service[%d]: restart %q must be never, on-failure or always", i, svc.Restart,
			))
		}
	}

	for name := range m.Env {
		if !envNameRe.MatchString(name) || reservedEnv(name) {
			errs = append(errs, fmt.Errorf(
				"env.%s: a package may not set this variable, because it changes how other programs load or run",
				name,
			))
		}
	}

	deps, err := parseDeps(m.Runtime.RawDeps)
	if err != nil {
		errs = append(errs, fmt.Errorf("runtime.%w", err))
	}

	m.Runtime.Deps = deps

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

		if len(a.Bin)+len(a.Man)+len(a.Completions)+len(a.App)+len(a.Font) == 0 {
			errs = append(errs, fmt.Errorf(
				"artifact[%d]: set at least one of bin, man, completions, app or font",
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
	return m.Build != nil && len(m.Build.Steps) > 0
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
		if a.URL, err = Expand(a.URL, vars); err != nil {
			return Artifact{}, false, fmt.Errorf("artifact url: %w", err)
		}

		if a.SHA256URL, err = Expand(a.SHA256URL, vars); err != nil {
			return Artifact{}, false, fmt.Errorf("artifact sha256_url: %w", err)
		}

		return a, true, nil
	}

	return Artifact{}, false, nil
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnv reports variables that would let a package control the user's
// shell or other programs.
func reservedEnv(name string) bool {
	upper := strings.ToUpper(name)

	for _, prefix := range []string{"LD_", "DYLD_", "OKU_"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	return slices.Contains([]string{
		"PATH", "HOME", "SHELL", "IFS", "ENV", "BASH_ENV", "PROMPT_COMMAND", "PS1", "USER",
	}, upper)
}

// Expand replaces {{name}} with vars[name] and fails on an unknown name.
func Expand(s string, vars map[string]string) (string, error) {
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
