package store

import (
	"debug/elf"
	"debug/macho"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// MissingDep is a store package that a built file loads at runtime and that
// runtime.deps does not name. Such a program works until gc removes the package
// or until the manifest is installed on another machine.
type MissingDep struct {
	// File is the first file that loads the package, relative to the prefix.
	File string
	// Package is the name of the loaded package.
	Package string
}

// missingDeps walks bin and lib of prefix and returns each store package that
// a Mach-O or ELF file in them loads, other than prefix itself and the runtime
// deps. Windows has no such link, so it reports nothing.
func (s *Store) missingDeps(prefix string, runtimeDeps []Dep) []MissingDep {
	if runtime.GOOS == "windows" {
		return nil
	}

	var (
		missing []MissingDep
		seen    []string
	)

	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}

		for _, lib := range linkedLibraries(path) {
			pkg := s.packageOf(lib)
			if pkg == "" || pkg == prefix || slices.Contains(seen, pkg) {
				continue
			}

			seen = append(seen, pkg)

			name := packageName(pkg)
			if slices.ContainsFunc(runtimeDeps, func(dep Dep) bool {
				return dep.Prefix == pkg || dep.Name == name
			}) {
				continue
			}

			rel, _ := filepath.Rel(prefix, path)
			missing = append(missing, MissingDep{File: filepath.ToSlash(rel), Package: name})
		}

		return nil
	}

	for _, dir := range []string{"bin", "lib"} {
		_ = filepath.WalkDir(filepath.Join(prefix, dir), visit)
	}

	return missing
}

// packageOf returns the store path that holds lib, or empty when lib is outside
// the store.
func (s *Store) packageOf(lib string) string {
	rel, err := filepath.Rel(s.dir, lib)
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}

	top, _, _ := strings.Cut(rel, string(filepath.Separator))

	return filepath.Join(s.dir, top)
}

// packageName reads the name a store path was realized under, and falls back to
// the directory name.
func packageName(storePath string) string {
	if meta, err := ReadMeta(storePath); err == nil && meta.Name != "" {
		return meta.Name
	}

	return filepath.Base(storePath)
}

// linkedLibraries returns the absolute paths of the shared libraries that the
// Mach-O or ELF file at path loads, resolved through its rpaths, and nothing for
// any other file. A library that no rpath holds is left out, because the loader
// would not find it either.
func linkedLibraries(path string) []string {
	if libs, ok := machoLibraries(path); ok {
		return libs
	}

	return elfLibraries(path)
}

// machoLibraries reads LC_LOAD_DYLIB and LC_RPATH of a Mach-O file, thin or fat.
func machoLibraries(path string) ([]string, bool) {
	var files []*macho.File

	if f, err := macho.Open(path); err == nil {
		defer func() { _ = f.Close() }()

		files = append(files, f)
	} else if fat, err := macho.OpenFat(path); err == nil {
		defer func() { _ = fat.Close() }()

		for _, arch := range fat.Arches {
			files = append(files, arch.File)
		}
	} else {
		return nil, false
	}

	dir := filepath.Dir(path)

	var libs []string

	for _, f := range files {
		var rpaths []string

		for _, load := range f.Loads {
			if r, ok := load.(*macho.Rpath); ok {
				rpaths = append(rpaths, expandLoader(r.Path, dir))
			}
		}

		imported, err := f.ImportedLibraries()
		if err != nil {
			continue
		}

		for _, lib := range imported {
			if rest, ok := strings.CutPrefix(lib, "@rpath/"); ok {
				libs = append(libs, firstExisting(rpaths, rest)...)

				continue
			}

			libs = append(libs, expandLoader(lib, dir))
		}
	}

	return libs, true
}

// elfLibraries reads DT_NEEDED of an ELF file and resolves each name through
// DT_RUNPATH, or DT_RPATH when there is no runpath.
func elfLibraries(path string) []string {
	f, err := elf.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	needed, err := f.ImportedLibraries()
	if err != nil {
		return nil
	}

	dir := filepath.Dir(path)

	var search []string

	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		values, err := f.DynString(tag)
		if err != nil || len(values) == 0 {
			continue
		}

		for _, value := range values {
			for _, entry := range strings.Split(value, ":") {
				if entry != "" {
					search = append(search, expandLoader(entry, dir))
				}
			}
		}

		break
	}

	var libs []string

	for _, lib := range needed {
		if strings.Contains(lib, "/") {
			libs = append(libs, expandLoader(lib, dir))

			continue
		}

		libs = append(libs, firstExisting(search, lib)...)
	}

	return libs
}

// expandLoader replaces the loader's own-directory tokens with dir.
func expandLoader(entry, dir string) string {
	for _, token := range []string{"@loader_path", "@executable_path", "${ORIGIN}", "$ORIGIN"} {
		entry = strings.ReplaceAll(entry, token, dir)
	}

	return filepath.Clean(entry)
}

// firstExisting returns name in the first of dirs that holds it, or nothing.
func firstExisting(dirs []string, name string) []string {
	for _, dir := range dirs {
		if candidate := filepath.Join(dir, name); exists(candidate) {
			return []string{candidate}
		}
	}

	return nil
}
