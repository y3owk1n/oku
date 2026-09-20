// Package lock reads and writes oku.lock, the resolved state beside an oku.toml.
package lock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/ref"
)

// FileName is the lock's file name.
const FileName = "oku.lock"

const header = "# Written by oku. Commit this file, and change it with oku commands only.\n\n"

// Lock is a parsed oku.lock.
type Lock struct {
	Includes []Include `toml:"include"`
	Packages []Package `toml:"package"`
}

// Include pins one included list.
type Include struct {
	Ref    string `toml:"ref"`
	Commit string `toml:"commit,omitempty"`
	SHA256 string `toml:"sha256"`
}

// FindInclude returns the pin of the included list at ref.
func (l *Lock) FindInclude(ref string) (Include, bool) {
	i := slices.IndexFunc(l.Includes, func(inc Include) bool { return inc.Ref == ref })
	if i < 0 {
		return Include{}, false
	}

	return l.Includes[i], true
}

// Package pins one package of the list.
type Package struct {
	Name           string `toml:"name"`
	Ref            string `toml:"ref"`
	Commit         string `toml:"commit,omitempty"`
	ManifestSHA256 string `toml:"manifest_sha256"`
	Version        string `toml:"version"`
	// SigningKey pins the manifest's signing key. oku refuses a manifest that
	// changes or drops it until the user accepts the change.
	SigningKey string `toml:"signing_key,omitempty"`
	// Tag is the upstream tag of Version, kept so sync can expand {{tag}} without
	// listing versions again.
	Tag string `toml:"tag,omitempty"`
	// TagCommit is the commit a moving tag pointed at for Version. Sync refuses
	// to download once upstream moved the tag off it.
	TagCommit string `toml:"tag_commit,omitempty"`
	// Inferred marks a package whose repo has no manifest. Manifest then holds the
	// manifest oku wrote for it, so sync installs from the same text.
	Inferred bool   `toml:"inferred,omitempty"`
	Manifest string `toml:"manifest,omitempty"`
	// Platforms is keyed by platform.Platform.String(), such as "linux-amd64-musl".
	Platforms map[string]Platform `toml:"platform"`
	// Deps pins the packages this one depends on. Each package pins its own, so
	// two packages may hold different versions of the same dep.
	Deps []Package `toml:"dep,omitempty"`
}

// FindDep returns the pinned dep that came from ref.
func (p Package) FindDep(ref string) Package {
	for _, dep := range p.Deps {
		if dep.Ref == ref {
			return dep
		}
	}

	return Package{}
}

// Platform pins what one platform installs.
type Platform struct {
	Strategy string `toml:"strategy"`
	URL      string `toml:"url,omitempty"`
	SHA256   string `toml:"sha256,omitempty"`
	// Impure marks a build whose run steps could use the network.
	Impure bool `toml:"impure,omitempty"`
	// VendorSHA256 pins what the build's vendor steps downloaded.
	VendorSHA256 string `toml:"vendor_sha256,omitempty"`
}

// Read parses the lock at path. A missing file is an empty lock. File refs that
// Write stored relative to the lock come back absolute.
func Read(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	l, err := Parse(data, path)
	if err != nil {
		return nil, err
	}

	return l.mapRefs(func(s string) string { return ref.FromDir(filepath.Dir(path), s) }), nil
}

// mapRefs returns a copy of l with f applied to every ref in it.
func (l *Lock) mapRefs(f func(string) string) *Lock {
	out := &Lock{Includes: slices.Clone(l.Includes), Packages: mapPackageRefs(l.Packages, f)}
	for i := range out.Includes {
		out.Includes[i].Ref = f(out.Includes[i].Ref)
	}

	return out
}

func mapPackageRefs(pkgs []Package, f func(string) string) []Package {
	out := slices.Clone(pkgs)
	for i := range out {
		out[i].Ref = f(out[i].Ref)
		out[i].Deps = mapPackageRefs(out[i].Deps, f)
	}

	return out
}

// Parse reads lock data. origin names the data in error messages.
func Parse(data []byte, origin string) (*Lock, error) {
	var l Lock
	if err := toml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", origin, err)
	}

	return &l, nil
}

// Find returns the package called name.
func (l *Lock) Find(name string) (Package, bool) {
	i := slices.IndexFunc(l.Packages, func(p Package) bool { return p.Name == name })
	if i < 0 {
		return Package{}, false
	}

	return l.Packages[i], true
}

// Set replaces the package of the same name, or adds pkg.
func (l *Lock) Set(pkg Package) {
	l.Delete(pkg.Name)
	l.Packages = append(l.Packages, pkg)
}

// Delete drops the package called name.
func (l *Lock) Delete(name string) {
	l.Packages = slices.DeleteFunc(l.Packages, func(p Package) bool { return p.Name == name })
}

// Bytes renders the lock as Write saves it to path. It sorts packages by name,
// so the same state always produces the same bytes. It renders a ref to a file
// inside the lock's directory relative to it, so a committed lock works in
// another checkout.
func (l *Lock) Bytes(path string) ([]byte, error) {
	slices.SortFunc(l.Packages, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	portable := l.mapRefs(func(s string) string { return ref.InDir(filepath.Dir(path), s) })

	data, err := toml.Marshal(portable)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", FileName, err)
	}

	return append([]byte(header), data...), nil
}

// Write saves the lock to path.
func (l *Lock) Write(path string) error {
	data, err := l.Bytes(path)
	if err != nil {
		return err
	}

	if err := list.WriteFile(path, data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
