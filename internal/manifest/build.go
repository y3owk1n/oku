package manifest

import (
	"errors"
	"fmt"

	"github.com/y3owk1n/oku/internal/platform"
)

// Build is how a package is produced from source.
type Build struct {
	// Needs are host tools the build requires. oku checks them and never installs
	// them.
	Needs []string `toml:"needs"`
	// RawDeps is "deps" as TOML gives it. Parse converts it into Deps, the other
	// oku packages the build uses.
	RawDeps []any  `toml:"deps"`
	Deps    []Dep  `toml:"-"`
	Source  Source `toml:"source"`
	Steps   []Step `toml:"step"`
}

// Source is where the code comes from: a git tag, or an archive with a digest.
type Source struct {
	Git    string `toml:"git"`
	Tag    string `toml:"tag"`
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
	// SHA256URL is a checksum file that upstream publishes beside the archive.
	// With neither, oku trusts the first download and pins its digest in oku.lock.
	SHA256URL string `toml:"sha256_url"`
	Strip     int    `toml:"strip"`
}

// Step is one build step. Exactly one of the type keys must be set.
type Step struct {
	Run     *string  `toml:"run"`
	Install *Install `toml:"install"`
	Fetch   *Fetch   `toml:"fetch"`
	Extract *Extract `toml:"extract"`
	Copy    *Copy    `toml:"copy"`
	// Vendor downloads a language's packages: "cargo", "go", "npm" or "pip".
	Vendor *string `toml:"vendor"`
	// Package makes an npm or a pip vendor step install that package from its
	// registry, with its dependencies, into the package's lib directory. Without
	// it the step installs what the source's lockfile or requirements.txt lists.
	Package string `toml:"package"`
	// Scripts names the packages of an npm step with Package whose install
	// scripts run after the install. A script that downloads or builds a binary
	// makes the build impure.
	Scripts []string `toml:"scripts"`
	Patch   *Patch   `toml:"patch"`

	When    platform.Selector `toml:"when"`
	Shell   string            `toml:"shell"`
	Env     map[string]string `toml:"env"`
	Network bool              `toml:"network"`
}

// Install names the files a build puts into the package. Paths are relative to
// the source directory.
type Install struct {
	// RawBin is "bin" as TOML gives it. Parse splits it into Bin, the files that
	// are programs, and Wrap, the programs that oku writes.
	RawBin  []any     `toml:"bin"`
	Bin     []string  `toml:"-"`
	Wrap    []Wrapper `toml:"-"`
	Lib     []string  `toml:"lib"`
	Include []string  `toml:"include"`
	Man     []string  `toml:"man"`
	Share   []string  `toml:"share"`
	// RawCompletions is "completions" as TOML gives it. Parse reads it into
	// Completions.
	RawCompletions any         `toml:"completions"`
	Completions    Completions `toml:"-"`
	App            []string    `toml:"app"`
	Font           []string    `toml:"font"`
}

// Fetch downloads URL to the path To inside the source directory.
type Fetch struct {
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
	To     string `toml:"to"`
}

// Patch applies the unified diff File, which is in the source directory, to the
// files of the source directory. Strip removes that many leading directories
// from the file names in the diff. A git diff needs none, because its a/ and b/
// prefixes are already left out.
type Patch struct {
	File  string `toml:"file"`
	Strip int    `toml:"strip"`
}

// Extract unpacks the archive File into the directory To. Both are relative to
// the source directory.
type Extract struct {
	File  string `toml:"file"`
	To    string `toml:"to"`
	Strip int    `toml:"strip"`
}

// Copy copies From, relative to the source directory, to To, relative to the
// package prefix.
type Copy struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// Kinds returns the type keys the step sets, sorted.
func (s Step) Kinds() []string {
	var set []string

	for _, kind := range []struct {
		name string
		on   bool
	}{
		{"copy", s.Copy != nil},
		{"extract", s.Extract != nil},
		{"fetch", s.Fetch != nil},
		{"install", s.Install != nil},
		{"patch", s.Patch != nil},
		{"run", s.Run != nil},
		{"vendor", s.Vendor != nil},
	} {
		if kind.on {
			set = append(set, kind.name)
		}
	}

	return set
}

// Impure reports whether the step may put something into the build that the
// lock cannot check: a run step with the network, or an npm step that runs
// install scripts, which may download a binary.
func (s Step) Impure() bool {
	return s.Run != nil && s.Network || s.Vendor != nil && len(s.Scripts) > 0
}

// CommandSteps returns the steps that run a program on p, with their positions:
// run steps, vendor steps and install steps that generate completions.
func (b *Build) CommandSteps(p platform.Platform) map[int]Step {
	found := map[int]Step{}

	for i, step := range b.Steps {
		if (step.Run != nil || step.Vendor != nil || step.Generates()) && step.When.Matches(p) {
			found[i] = step
		}
	}

	return found
}

// Generates reports whether the step is an install step that runs the package
// to generate its completions.
func (s Step) Generates() bool {
	return s.Install != nil && s.Install.Completions.Generate != ""
}

// Runtime holds what the package needs after it is installed.
type Runtime struct {
	RawDeps []any `toml:"deps"`
	Deps    []Dep `toml:"-"`
}

// Dep is another oku package, as a ref with an optional version constraint such
// as ">=3" or ">=1.2, <2". TOML writes it as a ref string or as { ref, version }.
type Dep struct {
	Ref     string
	Version string
}

// parseDeps converts the two TOML forms of a dep.
func parseDeps(raw []any) ([]Dep, error) {
	deps := make([]Dep, 0, len(raw))

	for i, value := range raw {
		d, err := ParseDep(value)
		if err != nil {
			return nil, fmt.Errorf("deps[%d]: %w", i, err)
		}

		deps = append(deps, d)
	}

	return deps, nil
}

// ParseDep converts a dep in either TOML form.
func ParseDep(value any) (Dep, error) {
	var d Dep

	switch v := value.(type) {
	case string:
		d.Ref = v
	case map[string]any:
		d.Ref, _ = v["ref"].(string)
		d.Version, _ = v["version"].(string)

		for key := range v {
			if key != "ref" && key != "version" {
				return Dep{}, fmt.Errorf("unknown key %q, use ref and version", key)
			}
		}
	default:
		return Dep{}, errors.New("want a ref string or a table with ref and version")
	}

	if d.Ref == "" {
		return Dep{}, errors.New("ref is required")
	}

	return d, nil
}

// TOML writes d in the form ParseDep reads, as a string without a version.
func (d Dep) TOML() string {
	if d.Version == "" {
		return fmt.Sprintf("%q", d.Ref)
	}

	return fmt.Sprintf("{ ref = %q, version = %q }", d.Ref, d.Version)
}
