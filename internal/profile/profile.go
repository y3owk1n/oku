// Package profile links store paths into numbered generations. The "current"
// link names the active generation.
package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	stateFile = "oku-gen.toml"
	current   = "current"
	genPrefix = "gen-"
)

// ErrNotInstalled reports a package missing from the profile.
var ErrNotInstalled = errors.New("not installed")

// Package is one entry of a generation.
type Package struct {
	Name      string `toml:"name"`
	Version   string `toml:"version"`
	Ref       string `toml:"ref"`
	StorePath string `toml:"store_path"`
}

type state struct {
	Packages []Package `toml:"package"`
}

// Profile is a directory of generations.
type Profile struct {
	dir string
}

// Open returns the profile called name under dataDir.
func Open(dataDir, name string) *Profile {
	return &Profile{dir: filepath.Join(dataDir, "profiles", name)}
}

// BinDir is the directory a user puts on PATH.
func (p *Profile) BinDir() string {
	return filepath.Join(p.dir, current, "bin")
}

// Packages lists the active generation, sorted by name. A profile with no
// generation has no packages.
func (p *Profile) Packages() ([]Package, error) {
	data, err := os.ReadFile(filepath.Join(p.dir, current, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}

	var s state
	if err := toml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}

	return s.Packages, nil
}

// Add activates a new generation that includes pkg. It replaces a package of
// the same name.
func (p *Profile) Add(pkg Package) error {
	pkgs, err := p.Packages()
	if err != nil {
		return err
	}

	pkgs = slices.DeleteFunc(pkgs, func(have Package) bool { return have.Name == pkg.Name })

	return p.activate(append(pkgs, pkg))
}

// Remove activates a new generation without the package called name.
func (p *Profile) Remove(name string) error {
	pkgs, err := p.Packages()
	if err != nil {
		return err
	}

	kept := slices.DeleteFunc(
		slices.Clone(pkgs),
		func(have Package) bool { return have.Name == name },
	)
	if len(kept) == len(pkgs) {
		return fmt.Errorf("%s: %w", name, ErrNotInstalled)
	}

	return p.activate(kept)
}

// activate builds the next generation from pkgs and points "current" at it. A
// failure deletes the half-built generation and leaves "current" unchanged.
func (p *Profile) activate(pkgs []Package) error {
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	next, err := p.nextGeneration()
	if err != nil {
		return err
	}

	gen := filepath.Join(p.dir, next)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		return fmt.Errorf("create generation: %w", err)
	}

	if err := build(gen, pkgs); err != nil {
		os.RemoveAll(gen)

		return err
	}

	// Rename replaces "current" in one step, so the link exists at every moment.
	tmp := filepath.Join(p.dir, current+".tmp")
	os.Remove(tmp)

	if err := os.Symlink(next, tmp); err != nil {
		os.RemoveAll(gen)

		return fmt.Errorf("activate generation: %w", err)
	}

	if err := os.Rename(tmp, filepath.Join(p.dir, current)); err != nil {
		os.RemoveAll(gen)

		return fmt.Errorf("activate generation: %w", err)
	}

	return nil
}

func (p *Profile) nextGeneration() (string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read profile: %w", err)
	}

	highest := 0

	for _, entry := range entries {
		n, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), genPrefix))
		if err == nil && n > highest {
			highest = n
		}
	}

	return genPrefix + strconv.Itoa(highest+1), nil
}

// build links every file under each package's bin and share into gen and writes
// the state file.
func build(gen string, pkgs []Package) error {
	owners := map[string]string{}

	for _, pkg := range pkgs {
		for _, sub := range []string{"bin", "share"} {
			if err := linkTree(gen, pkg, sub, owners); err != nil {
				return err
			}
		}
	}

	data, err := toml.Marshal(state{Packages: pkgs})
	if err != nil {
		return fmt.Errorf("write generation: %w", err)
	}

	if err := os.WriteFile(filepath.Join(gen, stateFile), data, 0o644); err != nil {
		return fmt.Errorf("write generation: %w", err)
	}

	return nil
}

func linkTree(gen string, pkg Package, sub string, owners map[string]string) error {
	root := filepath.Join(pkg.StorePath, sub)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		rel, err := filepath.Rel(pkg.StorePath, path)
		if err != nil {
			return err
		}

		if owner, taken := owners[rel]; taken {
			return fmt.Errorf("%s and %s both provide %s", owner, pkg.Name, filepath.ToSlash(rel))
		}

		owners[rel] = pkg.Name

		dest := filepath.Join(gen, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}

		return os.Symlink(path, dest)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return err
}
