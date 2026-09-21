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

	"aead.dev/minisign"

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
	// TagCommit is the commit a moving tag pointed at when the version was
	// chosen.
	TagCommit string `toml:"-"`
}

type Package struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Homepage    string `toml:"homepage"`
	License     string `toml:"license"`
	// Relocatable declares that the built files contain no store path, so the
	// result works under any store root.
	Relocatable bool `toml:"relocatable"`
	// SigningKey is the minisign public key that signs every artifact. The
	// signature of an artifact is at its URL with ".minisig" appended.
	SigningKey string `toml:"signing_key"`
}

// Version is either fixed by Value or discovered from From.
type Version struct {
	Value string `toml:"value"`
	// From is FromGitHubReleases, FromGiteaReleases, FromGitLabReleases or
	// FromGitTags.
	From string `toml:"from"`
	// Repo is "owner/repo" or "host/owner/repo" for GitHub releases,
	// "host/owner/repo" for Gitea releases, and a git URL for git tags.
	Repo string `toml:"repo"`
	// StripPrefix is cut off a tag to get the version, such as "v". A tag
	// without it is ignored.
	StripPrefix string `toml:"strip_prefix"`
	// Tag names one tag that upstream moves, such as "nightly". oku follows that
	// release instead of listing releases.
	Tag string `toml:"tag"`
}

const (
	FromGitHubReleases = "github-releases"
	// FromGiteaReleases reads a Gitea or Forgejo server, such as codeberg.org.
	FromGiteaReleases = "gitea-releases"
	// FromGitLabReleases reads gitlab.com, or a GitLab server when the repo
	// starts with its host.
	FromGitLabReleases = "gitlab-releases"
	FromGitTags        = "git-tags"
	// FromNPM reads the versions of a package in the npm registry.
	FromNPM = "npm"
)

// Artifact is a prebuilt download for the platforms its selector matches.
type Artifact struct {
	Match     platform.Selector `toml:"match"`
	URL       string            `toml:"url"`
	SHA256    string            `toml:"sha256"`
	SHA256URL string            `toml:"sha256_url"`
	// Integrity is a sha512 digest the way npm publishes it, "sha512-" and the
	// digest in base64.
	Integrity string `toml:"integrity"`
	Strip     int    `toml:"strip"`
	// RawBin is "bin" as TOML gives it. Parse splits it into Bin, the files that
	// are programs, and Wrap, the programs that oku writes.
	RawBin      []any             `toml:"bin"`
	Bin         []string          `toml:"-"`
	Wrap        []Wrapper         `toml:"-"`
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

// Wrapper is a program that oku writes into the package. It runs Run with Args
// before the user's own arguments, the way a shell script with "exec" does. An
// interpreted program needs one, such as "node script.js".
type Wrapper struct {
	Name string
	// Run and Args expand {{prefix}}, {{dep.<name>.prefix}} and the artifact
	// variables.
	Run  string
	Args []string
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	repoRe = regexp.MustCompile(
		`^([A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+/)?[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`,
	)
	giteaRepoRe = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+/[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`,
	)
	gitlabRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)+$`)
	npmNameRe    = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)
	sha256Re     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	integrityRe  = regexp.MustCompile(`^sha512-[A-Za-z0-9+/]{86}==$`)
	templateRe   = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
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

	if text := m.Package.SigningKey; text != "" {
		var key minisign.PublicKey
		if err := key.UnmarshalText([]byte(text)); err != nil {
			errs = append(
				errs,
				fmt.Errorf("package.signing_key is not a minisign public key: %w", err),
			)
		}
	}

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
		errs = append(errs, errors.New(`version.repo must be "owner/repo" or "host/owner/repo" for github-releases`))
	case m.Version.From == FromGiteaReleases && !giteaRepoRe.MatchString(m.Version.Repo):
		errs = append(errs, errors.New(`version.repo must be "host/owner/repo" for gitea-releases`))
	case m.Version.From == FromGitLabReleases && !gitlabRepoRe.MatchString(m.Version.Repo):
		errs = append(
			errs,
			errors.New(`version.repo must be "group/project" or "host/group/project" for gitlab-releases`),
		)
	case m.Version.From == FromNPM && !npmNameRe.MatchString(m.Version.Repo):
		errs = append(
			errs, errors.New(`version.repo must be a package name such as "@scope/name" for npm`),
		)
	case m.Version.From == FromNPM && m.Version.StripPrefix != "":
		errs = append(errs, errors.New("version.strip_prefix does not apply to npm, which has no tags"))
	case m.Version.From == FromGitTags && m.Version.Repo == "":
		errs = append(errs, errors.New("version.repo must be a git URL for git-tags"))
	case m.Version.From != "" && !slices.Contains(
		[]string{FromGitHubReleases, FromGiteaReleases, FromGitLabReleases, FromGitTags, FromNPM},
		m.Version.From,
	):
		errs = append(errs, fmt.Errorf(
			"version.from %q must be %q, %q, %q, %q or %q", m.Version.From,
			FromGitHubReleases, FromGiteaReleases, FromGitLabReleases, FromGitTags, FromNPM,
		))
	}

	switch {
	case m.Version.Tag == "":
	case !slices.Contains(
		[]string{FromGitHubReleases, FromGiteaReleases, FromGitLabReleases}, m.Version.From,
	):
		errs = append(errs, errors.New(
			`version.tag needs version.from = "github-releases", "gitea-releases" or "gitlab-releases"`,
		))
	case m.Version.StripPrefix != "":
		errs = append(errs, errors.New("set version.tag or version.strip_prefix, not both"))
	}

	for i := range m.Artifacts {
		if err := m.Artifacts[i].splitBin(); err != nil {
			errs = append(errs, fmt.Errorf("artifact[%d]: %w", i, err))
		}
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

		if a.Integrity != "" && !integrityRe.MatchString(a.Integrity) {
			errs = append(errs, fmt.Errorf(
				`artifact[%d]: integrity must be "sha512-" and 88 base64 characters`, i,
			))
		}

		if a.SHA256 != "" && a.SHA256URL != "" {
			errs = append(errs, fmt.Errorf("artifact[%d]: set sha256 or sha256_url, not both", i))
		}

		if len(a.Bin)+len(a.Wrap)+len(a.Man)+len(a.Completions)+len(a.App)+len(a.Font) == 0 {
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

// splitBin reads RawBin. An entry is a path, or a table with name, run and
// args.
func (a *Artifact) splitBin() error {
	a.Bin, a.Wrap = nil, nil

	for i, value := range a.RawBin {
		switch v := value.(type) {
		case string:
			a.Bin = append(a.Bin, v)
		case map[string]any:
			var w Wrapper

			w.Name, _ = v["name"].(string)
			w.Run, _ = v["run"].(string)

			rawArgs, _ := v["args"].([]any)
			for _, arg := range rawArgs {
				text, ok := arg.(string)
				if !ok {
					return fmt.Errorf("bin[%d]: args must be strings", i)
				}

				w.Args = append(w.Args, text)
			}

			for key := range v {
				if key != "name" && key != "run" && key != "args" {
					return fmt.Errorf("bin[%d]: unknown key %q, use name, run and args", i, key)
				}
			}

			if strings.ContainsAny(w.Run+strings.Join(w.Args, ""), "\r\n") {
				return fmt.Errorf("bin[%d]: run and args must not hold a line break", i)
			}

			if !nameRe.MatchString(w.Name) || w.Run == "" {
				return fmt.Errorf(
					"bin[%d]: a table needs a name of lowercase letters, digits, '.', '_' or '-', and run",
					i,
				)
			}

			a.Wrap = append(a.Wrap, w)
		default:
			return fmt.Errorf("bin[%d]: want a path, or a table with name, run and args", i)
		}
	}

	return nil
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
