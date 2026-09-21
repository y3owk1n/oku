package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
}

const npmPackageKind = "npm package"

var vendorKinds = map[string]vendorKind{
	"go": {
		tools:  []string{"go"},
		script: `"$tool" mod vendor`,
		output: "vendor",
		// A toolchain download would be a second, unhashed download.
		env: []string{"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod"},
	},
	"cargo": {
		tools: []string{"cargo"},
		script: `"$tool" vendor --locked vendor >/dev/null
mkdir -p .cargo
printf '\n[source.crates-io]\nreplace-with = "vendored-sources"\n\n[source.vendored-sources]\ndirectory = "vendor"\n' >> .cargo/config.toml`,
		output: "vendor",
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
