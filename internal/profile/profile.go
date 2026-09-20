// Package profile links store paths into numbered generations. The "current"
// link names the active generation.
package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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
	// Closure holds the store paths of every package this one depends on. They
	// are not linked into the profile, and they keep gc from deleting them.
	Closure []string `toml:"closure,omitempty"`
	// Env holds the package's [env] with its values expanded. The shell hook
	// reads it from here, so it never has to open a manifest.
	Env map[string]string `toml:"env,omitempty"`
	// Service reports that the list enables the package's services. Rollback
	// restores it with the generation.
	Service bool `toml:"service,omitempty"`
	// System reports that the list puts the package's apps, fonts and services
	// in system scope.
	System bool `toml:"system,omitempty"`
}

type state struct {
	Created  time.Time `toml:"created"`
	Packages []Package `toml:"package"`
}

// Generation is one numbered snapshot of the profile.
type Generation struct {
	Number   int
	Created  time.Time
	Packages []Package
	// Current marks the generation that "current" points at.
	Current bool
}

// LockSnapshot is the file in a generation that holds oku.lock as it was when
// the generation was activated.
const LockSnapshot = "oku.lock"

// Profile is a directory of generations.
type Profile struct {
	dir string
}

// Open returns the profile called name under dataDir.
func Open(dataDir, name string) *Profile {
	return &Profile{dir: filepath.Join(dataDir, "profiles", name)}
}

// LockSnapshotOfCurrent returns the oku.lock saved in the active generation, or
// nil when there is none.
func (p *Profile) LockSnapshotOfCurrent() []byte {
	data, _ := os.ReadFile(filepath.Join(p.dir, current, LockSnapshot))

	return data
}

// Name is "global" or "project-<hash>".
func (p *Profile) Name() string {
	return filepath.Base(p.dir)
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
// the same name. lockData is saved in the generation as LockSnapshot.
func (p *Profile) Add(pkg Package, lockData []byte) error {
	pkgs, err := p.Packages()
	if err != nil {
		return err
	}

	pkgs = slices.DeleteFunc(pkgs, func(have Package) bool { return have.Name == pkg.Name })

	return p.activate(append(pkgs, pkg), lockData)
}

// Remove activates a new generation without the package called name.
func (p *Profile) Remove(name string, lockData []byte) error {
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

	return p.activate(kept, lockData)
}

// Replace activates a new generation holding exactly pkgs. It does nothing when
// the active generation already holds them with the same lock, and reports
// whether it changed.
func (p *Profile) Replace(pkgs []Package, lockData []byte) (bool, error) {
	have, err := p.Packages()
	if err != nil {
		return false, err
	}

	pkgs = slices.Clone(pkgs)
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	same := func(a, b Package) bool {
		return a.Name == b.Name && a.Version == b.Version && a.Ref == b.Ref &&
			a.StorePath == b.StorePath && slices.Equal(a.Closure, b.Closure) &&
			maps.Equal(a.Env, b.Env) && a.Service == b.Service && a.System == b.System
	}

	if slices.EqualFunc(have, pkgs, same) && bytes.Equal(lockData, p.LockSnapshotOfCurrent()) {
		return false, nil
	}

	return true, p.activate(pkgs, lockData)
}

// activate builds the next generation from pkgs and points "current" at it. A
// failure deletes the half-built generation and leaves "current" unchanged.
func (p *Profile) activate(pkgs []Package, lockData []byte) error {
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	next, err := p.nextGeneration()
	if err != nil {
		return err
	}

	gen := filepath.Join(p.dir, next)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		return fmt.Errorf("create generation: %w", err)
	}

	if err := build(gen, pkgs, lockData); err != nil {
		os.RemoveAll(gen)

		return err
	}

	if err := p.point(next); err != nil {
		os.RemoveAll(gen)

		return err
	}

	return nil
}

// Generations lists every generation, oldest first.
func (p *Profile) Generations() ([]Generation, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read profile: %w", err)
	}

	active, _ := os.Readlink(filepath.Join(p.dir, current))

	var gens []Generation

	for _, entry := range entries {
		n, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), genPrefix))
		if err != nil || !strings.HasPrefix(entry.Name(), genPrefix) {
			continue
		}

		data, err := os.ReadFile(filepath.Join(p.dir, entry.Name(), stateFile))
		if err != nil {
			return nil, fmt.Errorf("read generation %d: %w", n, err)
		}

		var s state
		if err := toml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("read generation %d: %w", n, err)
		}

		gens = append(gens, Generation{
			Number:   n,
			Created:  s.Created,
			Packages: s.Packages,
			// A Windows junction reads back as an absolute path.
			Current: entry.Name() == filepath.Base(active),
		})
	}

	slices.SortFunc(gens, func(a, b Generation) int { return a.Number - b.Number })

	return gens, nil
}

// Switch points "current" at generation n and returns the lock snapshot saved
// in it. The snapshot is nil for a generation written before snapshots existed.
func (p *Profile) Switch(n int) ([]byte, error) {
	gen := genPrefix + strconv.Itoa(n)

	if _, err := os.Stat(filepath.Join(p.dir, gen, stateFile)); err != nil {
		return nil, fmt.Errorf("generation %d does not exist", n)
	}

	snapshot, err := os.ReadFile(filepath.Join(p.dir, gen, LockSnapshot))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read generation %d: %w", n, err)
	}

	return snapshot, p.point(gen)
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

// build links every file under each package's bin and share into gen, saves the
// lock snapshot and writes the state file.
func build(gen string, pkgs []Package, lockData []byte) error {
	owners := map[string]string{}

	for _, pkg := range pkgs {
		for _, sub := range []string{"bin", "share"} {
			if err := linkTree(gen, pkg, sub, owners); err != nil {
				return err
			}
		}
	}

	if lockData != nil {
		if err := os.WriteFile(filepath.Join(gen, LockSnapshot), lockData, 0o644); err != nil {
			return fmt.Errorf("write generation: %w", err)
		}
	}

	data, err := toml.Marshal(
		state{Created: time.Now().UTC().Truncate(time.Second), Packages: pkgs},
	)
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

		return linkEntry(path, dest, pkg)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return err
}

// All returns every profile under dataDir.
func All(dataDir string) ([]*Profile, error) {
	root := filepath.Join(dataDir, "profiles")

	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read profiles: %w", err)
	}

	var profiles []*Profile

	for _, entry := range entries {
		if entry.IsDir() {
			profiles = append(profiles, &Profile{dir: filepath.Join(root, entry.Name())})
		}
	}

	return profiles, nil
}

// Prune deletes every generation except the newest keep and the active one. It
// returns the numbers it deleted. With dryRun it deletes nothing.
func (p *Profile) Prune(keep int, dryRun bool) ([]int, error) {
	gens, err := p.Generations()
	if err != nil {
		return nil, err
	}

	var removed []int

	for i, gen := range gens {
		if gen.Current || i >= len(gens)-keep {
			continue
		}

		if !dryRun {
			dir := filepath.Join(p.dir, genPrefix+strconv.Itoa(gen.Number))
			if err := os.RemoveAll(dir); err != nil {
				return removed, fmt.Errorf("delete generation %d: %w", gen.Number, err)
			}
		}

		removed = append(removed, gen.Number)
	}

	return removed, nil
}
