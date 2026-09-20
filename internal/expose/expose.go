// Package expose puts a package's apps and fonts where the OS looks for them,
// and records every file it writes in a ledger so it can remove them again.
package expose

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/list"
)

// Item is one thing oku placed outside its own directories.
type Item struct {
	// Kind is "app" or "font".
	Kind string `toml:"kind"`
	// Package is the package that ships it.
	Package string `toml:"package"`
	// Source is the file or directory in the store.
	Source string `toml:"source"`
	// Target is where oku put it.
	Target string `toml:"target"`
	// Name and Enabled describe a service.
	Name    string `toml:"name,omitempty"`
	Enabled bool   `toml:"enabled,omitempty"`
	// System reports that Target is in system scope, so writing and removing it
	// needs administrator rights.
	System bool `toml:"system,omitempty"`
}

// Handler places and removes items of one kind. Apps and fonts are files, and
// oku copies them itself. A service goes through the OS's service manager.
type Handler struct {
	Place  func(Item) error
	Remove func(Item) error
}

// Ledger is the record of everything oku exposed. "oku self uninstall" replays it.
type Ledger struct {
	path  string
	Items []Item `toml:"item"`
}

// Launcher is a desktop entry for Linux, from a manifest's [[app]] table.
type Launcher struct {
	Name string `toml:"name"`
	Exec string `toml:"exec"`
	Icon string `toml:"icon"`
}

// ReadLedger loads the ledger under dataDir. A missing file is an empty ledger.
func ReadLedger(dataDir string) (*Ledger, error) {
	l := &Ledger{path: filepath.Join(dataDir, "exposed.toml")}

	data, err := os.ReadFile(l.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", l.path, err)
	}

	if err := toml.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", l.path, err)
	}

	return l, nil
}

func (l *Ledger) write() error {
	data, err := toml.Marshal(l)
	if err != nil {
		return fmt.Errorf("write %s: %w", l.path, err)
	}

	if err := list.WriteFile(l.path, data); err != nil {
		return fmt.Errorf("write %s: %w", l.path, err)
	}

	return nil
}

// Dirs are the per-user places the OS reads apps and fonts from.
type Dirs struct {
	Apps  string
	Fonts string
}

// UserDirs returns the per-user directories for this OS, under home. dataHome is
// the XDG data directory, which Linux uses.
func UserDirs(home, dataHome string) Dirs {
	if runtime.GOOS == "darwin" {
		return Dirs{
			Apps:  filepath.Join(home, "Applications"),
			Fonts: filepath.Join(home, "Library", "Fonts"),
		}
	}

	if runtime.GOOS == "windows" {
		return Dirs{
			Apps: filepath.Join(
				os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs",
			),
			Fonts: filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts"),
		}
	}

	return Dirs{
		Apps:  filepath.Join(dataHome, "applications"),
		Fonts: filepath.Join(dataHome, "fonts", "oku"),
	}
}

// SystemDirs returns the directories this OS reads apps and fonts from for
// every user. Only root can write to them.
func SystemDirs() Dirs {
	switch runtime.GOOS {
	case "darwin":
		return Dirs{Apps: "/Applications", Fonts: "/Library/Fonts"}
	case "windows":
		return Dirs{
			Apps: filepath.Join(
				os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs",
			),
			Fonts: filepath.Join(os.Getenv("SystemRoot"), "Fonts"),
		}
	}

	return Dirs{Apps: "/usr/local/share/applications", Fonts: "/usr/local/share/fonts/oku"}
}

// Wanted lists what the package at storePath exposes. On macOS an app is a
// bundle under apps/. On Linux it is a launcher from the package's meta file,
// written as a desktop entry.
func Wanted(name, storePath string, launchers []Launcher, dirs Dirs, system bool) []Item {
	var items []Item

	switch runtime.GOOS {
	case "darwin":
		for _, bundle := range children(filepath.Join(storePath, "apps")) {
			items = append(items, Item{
				Kind: "app", Package: name, Source: bundle, System: system,
				Target: filepath.Join(dirs.Apps, filepath.Base(bundle)),
			})
		}
	case "windows":
		// A shortcut shows the icon of its program, so the launcher's icon is unused.
		for _, launcher := range launchers {
			items = append(items, Item{
				Kind: "app", Package: name, System: system,
				Source: filepath.Join(storePath, filepath.FromSlash(launcher.Exec)),
				Target: filepath.Join(dirs.Apps, "oku-"+slug(launcher.Name)+shortcutExt),
			})
		}
	default:
		for _, launcher := range launchers {
			items = append(items, Item{
				Kind: "app", Package: name, System: system,
				Source: desktopEntry(launcher, storePath),
				Target: filepath.Join(dirs.Apps, "oku-"+slug(launcher.Name)+".desktop"),
			})
		}
	}

	for _, font := range children(filepath.Join(storePath, "fonts")) {
		items = append(items, Item{
			Kind: "font", Package: name, Source: font, System: system,
			Target: filepath.Join(dirs.Fonts, filepath.Base(font)),
		})
	}

	return items
}

// Sync makes what is exposed match wanted. It removes ledger items that are no
// longer wanted, adds new ones, and saves the ledger after each change, so a
// crash never leaves a file the ledger does not know.
func (l *Ledger) Sync(wanted []Item, handlers map[string]Handler) error {
	for _, have := range slices.Clone(l.Items) {
		if slices.Contains(wanted, have) {
			continue
		}

		if err := l.remove(have, handlers[have.Kind]); err != nil {
			return err
		}
	}

	for _, want := range wanted {
		if slices.Contains(l.Items, want) {
			continue
		}

		if _, err := os.Lstat(want.Target); err == nil && want.Target != "" {
			return fmt.Errorf(
				"%s already exists and oku did not put it there, so %s cannot expose its %s",
				want.Target, want.Package, want.Kind,
			)
		}

		// oku records the target in the ledger before it creates it.
		l.Items = append(l.Items, want)
		if err := l.write(); err != nil {
			return err
		}

		placeItem := Place
		if custom := handlers[want.Kind].Place; custom != nil {
			placeItem = custom
		}

		if err := placeItem(want); err != nil {
			return fmt.Errorf("expose %s %s: %w", want.Kind, want.Target, err)
		}
	}

	return nil
}

// RemoveAll removes everything in the ledger.
func (l *Ledger) RemoveAll(handlers map[string]Handler) error {
	return l.Sync(nil, handlers)
}

func (l *Ledger) remove(item Item, handler Handler) error {
	removeItem := Remove
	if handler.Remove != nil {
		removeItem = handler.Remove
	}

	if err := removeItem(item); err != nil {
		return fmt.Errorf("remove %s %s: %w", item.Kind, item.Target, err)
	}

	l.Items = slices.DeleteFunc(l.Items, func(have Item) bool { return have == item })

	return l.write()
}

// shortcutExt ends a Windows Start Menu shortcut.
const shortcutExt = ".lnk"

// Remove deletes an app or a font. A Windows font also leaves the registry.
func Remove(item Item) error {
	if item.Kind == "font" {
		if err := unregisterFont(item); err != nil {
			return err
		}
	}

	return os.RemoveAll(item.Target)
}

// Place copies an app or a font to its target.
func Place(item Item) error {
	if err := os.MkdirAll(filepath.Dir(item.Target), 0o755); err != nil {
		return err
	}

	if strings.HasSuffix(item.Target, shortcutExt) {
		return placeShortcut(item.Source, item.Target)
	}

	// A Linux launcher's source is the text of the desktop entry.
	if strings.HasPrefix(item.Source, "[Desktop Entry]") {
		return os.WriteFile(item.Target, []byte(item.Source), 0o644)
	}

	source, err := filepath.EvalSymlinks(item.Source)
	if err != nil {
		return err
	}

	// oku copies, because Finder, Spotlight and font services do not treat a
	// symlinked bundle or font as installed.
	if err := CopyTree(source, item.Target); err != nil {
		return err
	}

	if item.Kind == "font" {
		return registerFont(item)
	}

	return nil
}

func desktopEntry(l Launcher, storePath string) string {
	entry := "[Desktop Entry]\nType=Application\nName=" + l.Name + "\nExec=" +
		filepath.Join(storePath, filepath.FromSlash(l.Exec)) + "\n"

	if l.Icon != "" {
		entry += "Icon=" + filepath.Join(storePath, filepath.FromSlash(l.Icon)) + "\n"
	}

	return entry
}

func slug(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, name)
}

func children(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}

	return paths
}

// CopyTree copies a file or a directory, keeping modes and the symlinks inside
// it. App bundles use relative symlinks for their frameworks.
func CopyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		dest := filepath.Join(target, rel)

		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case entry.IsDir():
			return os.MkdirAll(dest, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}

			return os.Symlink(link, dest)
		default:
			return copyFile(path, dest, info.Mode().Perm())
		}
	})
}

func copyFile(source, dest string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode|0o600)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	return err
}
