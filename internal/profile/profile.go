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

// File is one path in the home directory that a generation sets up.
type File struct {
	Target string `toml:"target"`
	// Link is the path the target links to. It is empty for a file with content.
	Link string `toml:"link,omitempty"`
	// Content names the file under the generation's files directory that holds
	// the content. The name depends on Target only, so the target's link through
	// "current" stays the same from one generation to the next.
	Content string      `toml:"content,omitempty"`
	Mode    fs.FileMode `toml:"mode,omitempty"`
	// Hash is the sha256 of the bytes that Windows copies to Target. It is empty
	// for a linked directory.
	Hash string `toml:"hash,omitempty"`
	// Text is the content. The state file does not hold it.
	Text []byte `toml:"-"`
}

// Setting is one setting of the OS that a generation wants, with its value as
// the fragment that internal/settings writes.
type Setting struct {
	Domain string `toml:"domain"`
	Key    string `toml:"key"`
	Value  string `toml:"value"`
}

type state struct {
	Created  time.Time `toml:"created"`
	Packages []Package `toml:"package"`
	Files    []File    `toml:"file,omitempty"`
	Settings []Setting `toml:"setting,omitempty"`
}

// Generation is one numbered snapshot of the profile.
type Generation struct {
	Number   int
	Created  time.Time
	Packages []Package
	// Current marks the generation that "current" points at.
	Current bool
}

// filesDir is the directory in a generation that holds the content of files.
const filesDir = "files"

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
	return p.packagesIn(current)
}

// PackagesOf lists generation n. Generation 0 is the profile before its first
// generation, and has no packages.
func (p *Profile) PackagesOf(n int) ([]Package, error) {
	if n == 0 {
		return nil, nil
	}

	return p.packagesIn(genPrefix + strconv.Itoa(n))
}

func (p *Profile) packagesIn(gen string) ([]Package, error) {
	s, err := p.stateIn(gen)

	return s.Packages, err
}

func (p *Profile) stateIn(gen string) (state, error) {
	var s state

	data, err := os.ReadFile(filepath.Join(p.dir, gen, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return s, fmt.Errorf("read profile: %w", err)
	}

	if err := toml.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("read profile: %w", err)
	}

	return s, nil
}

// SettingsOf lists the settings of generation n.
func (p *Profile) SettingsOf(n int) ([]Setting, error) {
	if n == 0 {
		return nil, nil
	}

	s, err := p.stateIn(genPrefix + strconv.Itoa(n))

	return s.Settings, err
}

// FilesOf lists the files of generation n, without their text.
func (p *Profile) FilesOf(n int) ([]File, error) {
	if n == 0 {
		return nil, nil
	}

	s, err := p.stateIn(genPrefix + strconv.Itoa(n))

	return s.Files, err
}

// ContentPath is where the target of a file with content links to. The path
// goes through "current", so it names the content of the active generation.
func (p *Profile) ContentPath(f File) string {
	return filepath.Join(p.dir, current, filesDir, f.Content)
}

// files returns the files of the active generation with their text, which a
// new generation with the same files needs.
func (p *Profile) files() ([]File, error) {
	s, err := p.stateIn(current)
	if err != nil {
		return nil, err
	}

	for i, f := range s.Files {
		if f.Content == "" {
			continue
		}

		if s.Files[i].Text, err = os.ReadFile(p.ContentPath(f)); err != nil {
			return nil, fmt.Errorf("read profile: %w", err)
		}
	}

	return s.Files, nil
}

// Add stages a new generation that includes pkg, and returns its number. It
// replaces a package of the same name. lockData is saved in the generation as
// LockSnapshot. Activate makes a staged generation the active one.
func (p *Profile) Add(pkg Package, lockData []byte) (int, error) {
	pkgs, err := p.Packages()
	if err != nil {
		return 0, err
	}

	files, err := p.files()
	if err != nil {
		return 0, err
	}

	settings, err := p.SettingsOf(p.Current())
	if err != nil {
		return 0, err
	}

	pkgs = slices.DeleteFunc(pkgs, func(have Package) bool { return have.Name == pkg.Name })

	return p.stage(append(pkgs, pkg), files, settings, lockData)
}

// Remove stages a new generation without the package called name.
func (p *Profile) Remove(name string, lockData []byte) (int, error) {
	pkgs, err := p.Packages()
	if err != nil {
		return 0, err
	}

	kept := slices.DeleteFunc(
		slices.Clone(pkgs),
		func(have Package) bool { return have.Name == name },
	)
	if len(kept) == len(pkgs) {
		return 0, fmt.Errorf("%s: %w", name, ErrNotInstalled)
	}

	files, err := p.files()
	if err != nil {
		return 0, err
	}

	settings, err := p.SettingsOf(p.Current())
	if err != nil {
		return 0, err
	}

	return p.stage(kept, files, settings, lockData)
}

// Replace stages a new generation holding exactly pkgs, files and settings. It
// returns 0 when the active generation already holds them with the same lock.
func (p *Profile) Replace(
	pkgs []Package,
	files []File,
	settings []Setting,
	lockData []byte,
) (int, error) {
	have, err := p.Packages()
	if err != nil {
		return 0, err
	}

	haveFiles, err := p.files()
	if err != nil {
		return 0, err
	}

	haveSettings, err := p.SettingsOf(p.Current())
	if err != nil {
		return 0, err
	}

	pkgs = slices.Clone(pkgs)
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	same := func(a, b Package) bool {
		return a.Name == b.Name && a.Version == b.Version && a.Ref == b.Ref &&
			a.StorePath == b.StorePath && slices.Equal(a.Closure, b.Closure) &&
			maps.Equal(a.Env, b.Env) && a.Service == b.Service && a.System == b.System
	}

	sameFile := func(a, b File) bool {
		return a.Target == b.Target && a.Link == b.Link && a.Mode == b.Mode &&
			a.Hash == b.Hash && bytes.Equal(a.Text, b.Text)
	}

	if slices.EqualFunc(have, pkgs, same) && slices.EqualFunc(haveFiles, files, sameFile) &&
		slices.Equal(haveSettings, settings) &&
		bytes.Equal(lockData, p.LockSnapshotOfCurrent()) {
		return 0, nil
	}

	return p.stage(pkgs, files, settings, lockData)
}

// stage builds the next generation from pkgs and leaves "current" unchanged. A
// failure deletes the half-built generation.
func (p *Profile) stage(
	pkgs []Package,
	files []File,
	settings []Setting,
	lockData []byte,
) (int, error) {
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })

	next, err := p.nextGeneration()
	if err != nil {
		return 0, err
	}

	gen := filepath.Join(p.dir, genPrefix+strconv.Itoa(next))
	if err := os.MkdirAll(gen, 0o755); err != nil {
		return 0, fmt.Errorf("create generation: %w", err)
	}

	if err := build(gen, pkgs, files, settings, lockData); err != nil {
		os.RemoveAll(gen)

		return 0, err
	}

	return next, nil
}

// Current returns the number of the active generation, or 0 when the profile
// has none.
func (p *Profile) Current() int {
	active, err := os.Readlink(filepath.Join(p.dir, current))
	if err != nil {
		return 0
	}

	// A Windows junction reads back as an absolute path.
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(active), genPrefix))

	return n
}

// Activate points "current" at generation n. With 0 it removes "current", which
// is the profile before its first generation.
func (p *Profile) Activate(n int) error {
	if n == 0 {
		err := os.Remove(filepath.Join(p.dir, current))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("activate generation: %w", err)
		}

		return nil
	}

	return p.point(genPrefix + strconv.Itoa(n))
}

// Discard deletes generation n.
func (p *Profile) Discard(n int) error {
	if err := os.RemoveAll(filepath.Join(p.dir, genPrefix+strconv.Itoa(n))); err != nil {
		return fmt.Errorf("delete generation %d: %w", n, err)
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

// LockSnapshotOf returns the lock snapshot saved in generation n. The snapshot
// is nil for a generation written before snapshots existed.
func (p *Profile) LockSnapshotOf(n int) ([]byte, error) {
	gen := genPrefix + strconv.Itoa(n)

	if _, err := os.Stat(filepath.Join(p.dir, gen, stateFile)); err != nil {
		return nil, fmt.Errorf("generation %d does not exist", n)
	}

	snapshot, err := os.ReadFile(filepath.Join(p.dir, gen, LockSnapshot))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read generation %d: %w", n, err)
	}

	return snapshot, nil
}

func (p *Profile) nextGeneration() (int, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("read profile: %w", err)
	}

	highest := 0

	for _, entry := range entries {
		n, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), genPrefix))
		if err == nil && n > highest {
			highest = n
		}
	}

	return highest + 1, nil
}

// build links every file under each package's bin and share into gen, writes the
// content of files, saves the lock snapshot and writes the state file.
func build(gen string, pkgs []Package, files []File, settings []Setting, lockData []byte) error {
	owners := map[string]string{}

	for _, pkg := range pkgs {
		for _, sub := range []string{"bin", "share"} {
			if err := linkTree(gen, pkg, sub, owners); err != nil {
				return err
			}
		}
	}

	for _, f := range files {
		if f.Content == "" {
			continue
		}

		if err := os.MkdirAll(filepath.Join(gen, filesDir), 0o755); err != nil {
			return fmt.Errorf("write generation: %w", err)
		}

		// Read-only unless the list gives a mode, so an edit through the link fails
		// instead of changing a generation.
		mode := f.Mode
		if mode == 0 {
			mode = 0o444
		}

		path := filepath.Join(gen, filesDir, f.Content)
		if err := os.WriteFile(path, f.Text, mode); err != nil {
			return fmt.Errorf("write generation: %w", err)
		}

		// WriteFile applies the umask, and a mode from the list is meant exactly.
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("write generation: %w", err)
		}
	}

	if lockData != nil {
		if err := os.WriteFile(filepath.Join(gen, LockSnapshot), lockData, 0o644); err != nil {
			return fmt.Errorf("write generation: %w", err)
		}
	}

	data, err := toml.Marshal(
		state{
			Created:  time.Now().UTC().Truncate(time.Second),
			Packages: pkgs, Files: files, Settings: settings,
		},
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
