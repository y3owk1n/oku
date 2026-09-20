package manifest

import "github.com/y3owk1n/oku/internal/platform"

// Build is how a package is produced from source.
type Build struct {
	// Needs are host tools the build requires. oku checks them and never installs
	// them.
	Needs []string `toml:"needs"`
	// Deps are other oku packages. oku does not resolve them yet.
	Deps   []any  `toml:"deps"`
	Source Source `toml:"source"`
	Steps  []Step `toml:"step"`
}

// Source is where the code comes from: a git tag, or an archive with a digest.
type Source struct {
	Git    string `toml:"git"`
	Tag    string `toml:"tag"`
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
	Strip  int    `toml:"strip"`
}

// Step is one build step. Exactly one of the type keys must be set.
type Step struct {
	Run     *string  `toml:"run"`
	Install *Install `toml:"install"`
	Fetch   *Fetch   `toml:"fetch"`
	Extract *Extract `toml:"extract"`
	Copy    *Copy    `toml:"copy"`
	// Patch and Vendor are part of the schema. oku does not run them yet.
	Patch  map[string]any `toml:"patch"`
	Vendor *string        `toml:"vendor"`

	When    platform.Selector `toml:"when"`
	Shell   string            `toml:"shell"`
	Env     map[string]string `toml:"env"`
	Network bool              `toml:"network"`
}

// Install names the files a build puts into the package. Paths are relative to
// the source directory.
type Install struct {
	Bin         []string          `toml:"bin"`
	Lib         []string          `toml:"lib"`
	Include     []string          `toml:"include"`
	Man         []string          `toml:"man"`
	Share       []string          `toml:"share"`
	Completions map[string]string `toml:"completions"`
}

// Fetch downloads URL to the path To inside the source directory.
type Fetch struct {
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
	To     string `toml:"to"`
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

// RunSteps returns the run steps that apply to p, with their positions.
func (b *Build) RunSteps(p platform.Platform) map[int]Step {
	found := map[int]Step{}

	for i, step := range b.Steps {
		if step.Run != nil && step.When.Matches(p) {
			found[i] = step
		}
	}

	return found
}
