package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
)

// listedFile is one [files] entry of the merged list.
type listedFile struct {
	file list.File
	// dir is the directory of the list that declares the entry, where a relative
	// link source starts.
	dir string
}

// pkgPrefix starts a link source inside a package of the list.
const pkgPrefix = "{{pkg."

// locations returns the variables a target may start with on this OS.
func (e env) locations() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// e.config is "<config home>/oku", and e.data likewise.
	vars := map[string]string{
		"home": home, "config": filepath.Dir(e.config), "data": filepath.Dir(e.data),
	}

	if runtime.GOOS == "windows" {
		vars["appdata"] = os.Getenv("APPDATA")
		vars["localappdata"] = os.Getenv("LOCALAPPDATA")
	}

	return vars, nil
}

// target expands the location variable that written starts with.
func target(written string, locations map[string]string) (string, error) {
	variable, isVariable := strings.CutPrefix(written, "{{")

	name, rest, closed := strings.Cut(variable, "}}")
	if !isVariable || !closed {
		return "", errors.New("a target starts with a location such as {{home}}")
	}

	base, known := locations[name]

	switch {
	case !known && (name == "appdata" || name == "localappdata"):
		return "", fmt.Errorf(
			"{{%s}} exists on Windows only, add when = { os = \"windows\" } to the entry", name,
		)
	case !known:
		return "", fmt.Errorf(
			"{{%s}} is not a location, use {{home}}, {{config}}, {{data}}, {{appdata}} or {{localappdata}}",
			name,
		)
	case strings.Trim(rest, `/\`) == "":
		return "", fmt.Errorf("the target is {{%s}} itself, name a path inside it", name)
	}

	return filepath.Join(base, filepath.FromSlash(rest)), nil
}

// linkSource returns the absolute path a link entry points at.
func linkSource(f listedFile, pkgs []profile.Package) (string, error) {
	source := f.file.Link

	variable, inPackage := strings.CutPrefix(source, pkgPrefix)

	if name, rest, closed := strings.Cut(variable, "}}"); inPackage && closed {
		for _, pkg := range pkgs {
			if pkg.Name != name {
				continue
			}

			// An artifact's download is unpacked under "pkg". A build puts its files
			// at the top of the store path.
			files := filepath.Join(pkg.StorePath, "pkg")
			if _, err := os.Stat(files); err != nil {
				files = pkg.StorePath
			}

			return filepath.Join(files, filepath.FromSlash(rest)), nil
		}

		return "", fmt.Errorf("%s is not a package of the list on this machine", name)
	}

	if !filepath.IsAbs(source) {
		source = filepath.Join(f.dir, filepath.FromSlash(source))
	}

	return source, nil
}

// resolveFiles turns the [files] of the merged list into the files of a
// generation that holds pkgs. It changes nothing.
func (e env) resolveFiles(listed []listedFile, pkgs []profile.Package) ([]profile.File, error) {
	locations, err := e.locations()
	if err != nil {
		return nil, err
	}

	host := platform.Host()
	owners := map[string]string{}

	var files []profile.File

	for _, f := range listed {
		if !f.file.When.Matches(host) {
			continue
		}

		path, err := target(f.file.Target, locations)
		if err != nil {
			return nil, fmt.Errorf("files.%q: %w", f.file.Target, err)
		}

		if other, taken := owners[path]; taken {
			return nil, fmt.Errorf("files.%q and files.%q are both %s", other, f.file.Target, path)
		}

		owners[path] = f.file.Target

		if f.file.HasText {
			sum := sha256.Sum256([]byte(path))

			text := sha256.Sum256([]byte(f.file.Text))

			files = append(files, profile.File{
				Target:  path,
				Content: hex.EncodeToString(sum[:])[:12] + "-" + filepath.Base(path),
				Mode:    f.file.Mode,
				Hash:    hex.EncodeToString(text[:]),
				Text:    []byte(f.file.Text),
			})

			continue
		}

		source, err := linkSource(f, pkgs)
		if err != nil {
			return nil, fmt.Errorf("files.%q: %w", f.file.Target, err)
		}

		info, err := os.Stat(source)
		if err != nil {
			return nil, fmt.Errorf(
				"files.%q: the link source %s does not exist",
				f.file.Target,
				source,
			)
		}

		linked := profile.File{Target: path, Link: source}

		// Windows copies a linked file, so the generation records which bytes.
		if runtime.GOOS == "windows" && !info.IsDir() {
			if linked.Hash, err = expose.FileHash(source); err != nil {
				return nil, fmt.Errorf("files.%q: %w", f.file.Target, err)
			}
		}

		files = append(files, linked)
	}

	return files, nil
}
