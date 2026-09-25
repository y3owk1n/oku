package store

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/y3owk1n/oku/internal/desktop"
	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/manifest"
)

// launchers turns the app entries that are no macOS bundle into launchers.
// root holds the entries' files. exec maps what a launcher runs, a file under
// root or a program's name, to its path in the package, and icon maps a file
// under root.
func launchers(
	entries []manifest.AppEntry, root string, exec, icon func(rel string) (string, error),
) ([]expose.Launcher, error) {
	var out []expose.Launcher

	for _, e := range entries {
		if e.Bundle() {
			continue
		}

		l, err := launcher(e, root, exec, icon)
		if err != nil {
			return nil, fmt.Errorf("app %q: %w", e.Path, err)
		}

		out = append(out, l)
	}

	return out, nil
}

func launcher(
	e manifest.AppEntry, root string, exec, icon func(rel string) (string, error),
) (expose.Launcher, error) {
	if !filepath.IsLocal(filepath.FromSlash(e.Path)) {
		return expose.Launcher{}, errors.New("the path is outside the package")
	}

	l := expose.Launcher{Name: e.Name}
	runs, iconName := e.Path, ""

	if strings.HasSuffix(e.Path, ".desktop") {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil {
			return l, err
		}

		fields := desktop.Parse(string(data))
		program := desktop.Program(fields["Exec"])

		if program == "" {
			return l, errors.New("the desktop entry has no Exec")
		}

		// A package unpacked from a .deb holds /usr/bin/foo as usr/bin/foo.
		runs = strings.TrimPrefix(program, "/")
		if !isFile(filepath.Join(root, filepath.FromSlash(runs))) {
			runs = path.Base(program)
		}

		l.Name = cmp.Or(l.Name, fields["Name"])
		iconName = fields["Icon"]
	}

	l.Name = cmp.Or(l.Name, strings.TrimSuffix(path.Base(runs), ".exe"))

	var err error
	if l.Exec, err = exec(runs); err != nil {
		return l, err
	}

	file := e.Icon
	if file == "" {
		file = findIcon(root, iconName)
	}

	if file != "" {
		if l.Icon, err = icon(file); err != nil {
			return l, err
		}
	}

	return l, nil
}

var iconSizeRe = regexp.MustCompile(`(\d+)x\d+`)

// findIcon returns the file under root that the Icon of a desktop entry names,
// or "". The value is a path, or the name of an icon theme's file, of which a
// scalable one wins and then the largest.
func findIcon(root, value string) string {
	if value == "" {
		return ""
	}

	if strings.Contains(value, "/") {
		rel := strings.TrimPrefix(value, "/")
		if isFile(filepath.Join(root, filepath.FromSlash(rel))) {
			return rel
		}

		return ""
	}

	best, bestRank := "", -1

	_ = filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil //nolint:nilerr // an unreadable directory holds no icon to take.
		}

		ext := path.Ext(entry.Name())
		if strings.TrimSuffix(entry.Name(), ext) != value {
			return nil
		}

		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)

		rank := -1

		switch ext {
		case ".svg":
			rank = 1 << 20
		case ".png", ".xpm":
			rank = 0
			if m := iconSizeRe.FindStringSubmatch(rel); m != nil {
				rank, _ = strconv.Atoi(m[1])
			}
		}

		if rank > bestRank {
			best, bestRank = rel, rank
		}

		return nil
	})

	return best
}

func isFile(p string) bool {
	info, err := os.Stat(p)

	return err == nil && !info.IsDir()
}
