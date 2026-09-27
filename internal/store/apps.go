package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/infer"
)

// Apps returns the apps of the package at storePath as launchers, whose Exec is
// a path inside the package. On macOS they are the bundles under apps/, which is
// what oku exposes there. On Linux and Windows they are the launchers the
// package's spec records.
func Apps(storePath string) ([]expose.Launcher, error) {
	if runtime.GOOS == "darwin" {
		return bundleApps(storePath)
	}

	meta, err := ReadMeta(storePath)
	if err != nil {
		return nil, err
	}

	return meta.Launchers, nil
}

// bundleApps returns a launcher for each macOS bundle under apps/.
func bundleApps(storePath string) ([]expose.Launcher, error) {
	entries, err := os.ReadDir(filepath.Join(storePath, "apps"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	apps := make([]expose.Launcher, 0, len(entries))

	for _, entry := range entries {
		program, err := bundleMain(filepath.Join(storePath, "apps", entry.Name()))
		if err != nil {
			return nil, err
		}

		apps = append(apps, expose.Launcher{
			Name: strings.TrimSuffix(entry.Name(), ".app"),
			Exec: path.Join("apps", entry.Name(), "Contents", "MacOS", program),
		})
	}

	return apps, nil
}

// bundleMain returns the program in Contents/MacOS that opens bundle. It is the
// one Info.plist names, and otherwise the only program the directory holds. A
// plist in the binary format holds no text to read.
func bundleMain(bundle string) (string, error) {
	dir := filepath.Join(bundle, "Contents", "MacOS")

	plist, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	if err == nil {
		if name := infer.BundleExecutable(string(plist)); name != "" &&
			isFile(filepath.Join(dir, name)) {
			return name, nil
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("%s holds no Contents/MacOS", filepath.Base(bundle))
	}

	var programs []string

	for _, entry := range entries {
		if !entry.IsDir() {
			programs = append(programs, entry.Name())
		}
	}

	if len(programs) != 1 {
		return "", fmt.Errorf("%s does not say which program opens it", filepath.Base(bundle))
	}

	return programs[0], nil
}
