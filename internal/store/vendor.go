package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
)

// vendorKind is how one language's packages are downloaded into the source
// directory. The build steps after it then work offline.
type vendorKind struct {
	// tool must be on the build's PATH, through needs or a dep.
	tools []string
	// script runs in the source directory with the network on. "$tool" is the
	// tool that was found.
	script string
	// output is the directory the script fills. oku hashes it.
	output string
	// env is added to the step's environment.
	env []string
	// portable reports that the script fills output with the same files on every
	// platform, so one digest serves them all. npm and pip pick packages by
	// platform.
	portable bool
}

const npmPackageKind = "npm package"

var vendorKinds = map[string]vendorKind{
	"go": {
		tools:  []string{"go"},
		script: `"$tool" mod vendor`,
		output: "vendor",
		// A toolchain download would be a second, unhashed download.
		env:      []string{"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod"},
		portable: true,
	},
	"cargo": {
		tools: []string{"cargo"},
		script: `"$tool" vendor --locked vendor >/dev/null
mkdir -p .cargo
printf '\n[source.crates-io]\nreplace-with = "vendored-sources"\n\n[source.vendored-sources]\ndirectory = "vendor"\n' >> .cargo/config.toml`,
		output:   "vendor",
		portable: true,
	},
	"npm": {
		tools:  []string{"npm"},
		script: `"$tool" ci --ignore-scripts --no-audit --no-fund`,
		output: "node_modules",
	},
	// npmPackageKind is an npm step with "package". It installs one package with
	// its dependencies and runs none of their scripts.
	npmPackageKind: {
		tools: []string{"npm"},
		script: `"$tool" install --ignore-scripts --omit=dev --no-audit --no-fund --no-package-lock \
  --before="$OKU_NPM_BEFORE" --prefix "$OKU_PREFIX/lib" "$OKU_NPM_PACKAGE"`,
		output: "lib/node_modules",
		env:    []string{"npm_config_update_notifier=false"},
	},
	"pip": {
		tools:  []string{"pip", "pip3"},
		script: `"$tool" download --disable-pip-version-check -q -r requirements.txt -d vendor/pip`,
		output: "vendor/pip",
	},
}

// VendorPortable reports whether the digest of what b vendors is the same on
// every platform. That needs a vendor step, and each one must run on every
// platform and be of a portable kind.
func VendorPortable(b *manifest.Build) bool {
	found := false

	for _, step := range b.Steps {
		if step.Vendor == nil {
			continue
		}

		if step.When != (platform.Selector{}) || !vendorKinds[*step.Vendor].portable {
			return false
		}

		found = true
	}

	return found
}

// CanCrossVendor reports whether oku can download what b vendors for platform
// p on a machine of another platform. That needs npm vendor steps alone, since
// npm installs for the platform it is told, and no command of the manifest
// before them, because a command for p may not run here.
func CanCrossVendor(b *manifest.Build, p platform.Platform) bool {
	last := -1

	for i, step := range b.Steps {
		if step.Vendor != nil && step.When.Matches(p) {
			if *step.Vendor != "npm" {
				return false
			}

			last = i
		}
	}

	for _, step := range b.Steps[:last+1] {
		if step.Run != nil && step.When.Matches(p) {
			return false
		}
	}

	return last >= 0
}

// npmTarget returns the variables that make npm install the optional packages
// of platform p and not those of the host.
func npmTarget(p platform.Platform) []string {
	os, cpu := p.OS, p.Arch
	if os == "windows" {
		os = "win32"
	}

	if cpu == "amd64" {
		cpu = "x64"
	}

	env := []string{"npm_config_os=" + os, "npm_config_cpu=" + cpu}
	if p.Libc != "" {
		env = append(env, "npm_config_libc="+p.Libc)
	}

	return env
}

// BuildPin is what oku.lock holds for a build before a machine of its platform
// ran it.
type BuildPin struct {
	Impure bool
	// SourceURL and SHA256 are the source archive and its digest, or empty for a
	// git source. FirstUse reports that oku trusted the download.
	SourceURL string
	SHA256    string
	FirstUse  bool
}

// PinBuild returns the pin of m's build for platform p. It builds nothing. It
// takes the digest of a source archive from the manifest, else from its
// checksum file, else from a download.
func (s *Store) PinBuild(
	ctx context.Context,
	m *manifest.Manifest,
	p platform.Platform,
) (BuildPin, error) {
	var pin BuildPin

	for _, step := range m.Build.Steps {
		pin.Impure = pin.Impure || step.Run != nil && step.Network && step.When.Matches(p)
	}

	source := m.Build.Source
	if source.URL == "" {
		return pin, nil
	}

	vars := map[string]string{
		"version": m.Version.Value, "tag": m.Tag, "os": p.OS, "arch": p.Arch, "libc": p.Libc,
	}

	var err error
	if pin.SourceURL, err = manifest.Expand(source.URL, vars); err != nil {
		return pin, err
	}

	switch {
	case source.SHA256 != "":
		pin.SHA256 = source.SHA256
	case source.SHA256URL != "":
		var checksums string
		if checksums, err = manifest.Expand(source.SHA256URL, vars); err != nil {
			return pin, err
		}

		pin.SHA256, err = s.publishedSHA256(ctx, checksums, path.Base(pin.SourceURL))
	default:
		pin.FirstUse = true
		_, pin.SHA256, err = s.fetch(ctx, pin.SourceURL, "")
	}

	return pin, err
}

// hashTree returns one digest for every file under dir: its path, whether it is
// executable, and its content. A symlink counts by its target.
func hashTree(dir string) (string, error) {
	sum := sha256.New()

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			fmt.Fprintf(sum, "link %s -> %s\n", filepath.ToSlash(rel), target)

			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		content := sha256.New()
		if _, err := io.Copy(content, file); err != nil {
			return err
		}

		fmt.Fprintf(
			sum, "file %s %t %s\n",
			filepath.ToSlash(rel), info.Mode()&0o111 != 0, hex.EncodeToString(content.Sum(nil)),
		)

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", dir, err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// findTool returns the first of tools that is in one of the PATH directories of
// env.
func findTool(tools, env []string) (string, error) {
	var dirs []string

	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok {
			dirs = filepath.SplitList(value)
		}
	}

	for _, tool := range tools {
		for _, dir := range dirs {
			candidate := filepath.Join(dir, tool)
			if info, err := os.Stat(
				candidate,
			); err == nil && !info.IsDir() &&
				info.Mode()&0o111 != 0 {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf(
		"needs %s, which is not among the build's tools, add it to needs",
		strings.Join(tools, " or "),
	)
}
