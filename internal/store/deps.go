package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/osv"
)

// ErrMalicious reports a package that OSV lists as malicious.
var ErrMalicious = errors.New("OSV lists as malicious")

// checkVendored asks OSV whether a package that the vendor step of kind put in
// output is malicious, and fails when one is. When oku cannot ask, it says so
// in result and goes on.
func (s *Store) checkVendored(ctx context.Context, kind, output string, result *Realized) error {
	pkgs := vendoredPackages(kind, output)
	if s.OSV == nil || len(pkgs) == 0 {
		return nil
	}

	findings, err := s.OSV.MaliciousOf(ctx, pkgs)
	if err != nil {
		result.MalwareUnchecked = fmt.Sprintf("%s step, %v", kind, err)

		return nil
	}

	if len(findings) == 0 {
		return nil
	}

	lines := make([]string, len(findings))
	for i, f := range findings {
		lines[i] = fmt.Sprintf("  %s %s, see https://osv.dev/vulnerability/%s", f.Name, f.Version, f.IDs[0])
	}

	return fmt.Errorf("the %s step installed packages that %w:\n%s", kind, ErrMalicious, strings.Join(lines, "\n"))
}

// vendoredPackages returns the packages that a vendor step of kind put in
// output, by OSV's names, so oku can ask whether one is malicious. A kind
// whose packages oku cannot list returns none.
func vendoredPackages(kind, output string) []osv.Package {
	var pkgs []osv.Package

	switch kind {
	case "npm", npmPackageKind:
		pkgs = npmPackages(output)
	case "pip":
		pkgs = pipDownloads(output)
	case pipPackageKind:
		pkgs = pipInstalled(output)
	case "cargo", cargoPackageKind:
		pkgs = cargoCrates(output)
	case "go":
		pkgs = goVendored(output)
	case goPackageKind:
		pkgs = goDownloads(output)
	}

	slices.SortFunc(pkgs, func(a, b osv.Package) int {
		return strings.Compare(a.Name+"@"+a.Version, b.Name+"@"+b.Version)
	})

	return slices.Compact(pkgs)
}

// npmPackages reads the name and version of every package in the node_modules
// tree at root, nested ones too.
func npmPackages(root string) []osv.Package {
	var pkgs []osv.Package

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "package.json" {
			return nil
		}

		// A package is node_modules/<name> or node_modules/@<scope>/<name>.
		dir := filepath.Dir(path)
		parent := filepath.Dir(dir)
		scoped := strings.HasPrefix(filepath.Base(parent), "@") && filepath.Base(filepath.Dir(parent)) == "node_modules"

		if filepath.Base(parent) != "node_modules" && !scoped {
			return nil
		}

		var pkg struct {
			Name, Version string
		}

		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &pkg) == nil &&
			pkg.Name != "" && pkg.Version != "" {
			pkgs = append(pkgs, osv.Package{Ecosystem: "npm", Name: pkg.Name, Version: pkg.Version})
		}

		return nil
	})

	return pkgs
}

// pipInstalled reads the .dist-info directories that uv wrote into dir.
func pipInstalled(dir string) []osv.Package {
	entries, _ := os.ReadDir(dir)

	var pkgs []osv.Package

	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".dist-info")
		if !ok || !e.IsDir() {
			continue
		}

		if name, version, ok := strings.Cut(base, "-"); ok {
			pkgs = append(pkgs, osv.Package{Ecosystem: "PyPI", Name: pypiName(name), Version: version})
		}
	}

	return pkgs
}

// pipDownloads reads the names of the wheels and source archives that pip
// downloaded into dir, which start "<name>-<version>".
func pipDownloads(dir string) []osv.Package {
	entries, _ := os.ReadDir(dir)

	var pkgs []osv.Package

	for _, e := range entries {
		base := e.Name()
		for _, ext := range []string{".whl", ".tar.gz", ".zip"} {
			base = strings.TrimSuffix(base, ext)
		}

		if base == e.Name() {
			continue
		}

		parts := strings.SplitN(base, "-", 3)
		if len(parts) >= 2 {
			pkgs = append(pkgs, osv.Package{Ecosystem: "PyPI", Name: pypiName(parts[0]), Version: parts[1]})
		}
	}

	return pkgs
}

// cargoCrates reads the [package] of each crate that cargo vendored into dir.
func cargoCrates(dir string) []osv.Package {
	entries, _ := os.ReadDir(dir)

	var pkgs []osv.Package

	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "Cargo.toml"))
		if err != nil {
			continue
		}

		var crate struct {
			Package struct {
				Name    string `toml:"name"`
				Version string `toml:"version"`
			} `toml:"package"`
		}

		if toml.Unmarshal(data, &crate) == nil && crate.Package.Name != "" {
			pkgs = append(pkgs, osv.Package{Ecosystem: "crates.io", Name: crate.Package.Name, Version: crate.Package.Version})
		}
	}

	return pkgs
}

// goVendored reads the "# module version" lines of vendor/modules.txt, whose
// vendor directory is dir.
func goVendored(dir string) []osv.Package {
	f, err := os.Open(filepath.Join(dir, "modules.txt"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var pkgs []osv.Package

	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) >= 3 && fields[0] == "#" && strings.HasPrefix(fields[2], "v") {
			pkgs = append(pkgs, osv.Package{Ecosystem: "Go", Name: fields[1], Version: fields[2]})
		}
	}

	return pkgs
}

// goModuleZipRe is the zip of a module version in a module cache's download
// directory: <escaped module>/@v/<version>.zip.
var goModuleZipRe = regexp.MustCompile(`^(.+)/@v/(v[^/]+)\.zip$`)

// goDownloads reads the module zips in dir, the download directory of a
// module cache. The cache writes an upper-case letter as "!" and its lower case.
func goDownloads(dir string) []osv.Package {
	var pkgs []osv.Package

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}

		if m := goModuleZipRe.FindStringSubmatch(filepath.ToSlash(rel)); m != nil {
			pkgs = append(pkgs, osv.Package{Ecosystem: "Go", Name: unescapeModule(m[1]), Version: m[2]})
		}

		return nil
	})

	return pkgs
}

func unescapeModule(s string) string {
	var b strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] == '!' && i+1 < len(s) {
			i++
			b.WriteString(strings.ToUpper(s[i : i+1]))

			continue
		}

		b.WriteByte(s[i])
	}

	return b.String()
}

// pypiName is the normal form of a Python package's name, which a file name
// spells with "_" or ".".
func pypiName(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "-", ".", "-").Replace(name))
}
