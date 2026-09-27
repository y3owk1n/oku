package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// linkDepDLLs puts a hard link to each DLL of deps beside the programs in dirs,
// unless the package has a file of that name there, and returns the links it
// made relative to root. Windows looks for a DLL in the program's directory
// first, and in the working directory before PATH, so without the link a DLL of
// the same name in the folder the user runs the program from would load
// instead of the dep's.
func linkDepDLLs(root string, dirs []string, deps []Dep) ([]string, error) {
	if runtime.GOOS != "windows" || len(deps) == 0 {
		return nil, nil
	}

	var linked []string

	var dlls []string

	for _, dir := range dllDirs(deps) {
		found, _ := filepath.Glob(filepath.Join(dir, "*.dll"))
		dlls = append(dlls, found...)
	}

	for _, dir := range dirs {
		for _, dll := range dlls {
			dest := filepath.Join(dir, filepath.Base(dll))
			if _, err := os.Lstat(dest); !errors.Is(err, fs.ErrNotExist) {
				continue
			}

			// A dep in another store has another volume, where a hard link fails.
			if err := os.Link(dll, dest); err != nil {
				if err := copyFile(dll, dest); err != nil {
					return nil, err
				}
			}

			rel, err := filepath.Rel(root, dest)
			if err != nil {
				return nil, err
			}

			linked = append(linked, filepath.ToSlash(rel))
		}
	}

	return linked, nil
}

// programDirs returns the directories under pkg of the programs that bin and
// the file wrappers name, once each.
func programDirs(pkg string, bins []string, wraps []string) []string {
	var dirs []string

	for _, rel := range append(slices.Clone(bins), wraps...) {
		if !strings.HasSuffix(strings.ToLower(rel), ".exe") {
			continue
		}

		dir := filepath.Dir(filepath.Join(pkg, filepath.FromSlash(rel)))
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}
