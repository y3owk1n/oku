package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// npmTree returns the packages under the node_modules folder root, at any
// depth, and those of them whose install npm would run a script for:
// preinstall, install or postinstall, or a binding.gyp, for which npm runs
// node-gyp.
func npmTree(root string) (all, scripted []string) {
	var walk func(dir, scope string)

	walk = func(dir, scope string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == ".bin" {
				continue
			}

			at := filepath.Join(dir, entry.Name())

			// A scope, such as @img, holds packages.
			if entry.Name()[0] == '@' {
				walk(at, entry.Name()+"/")

				continue
			}

			name, hasScript := npmScriptPackage(at, scope+entry.Name())
			if name != "" && !slices.Contains(all, name) {
				all = append(all, name)
			}

			if hasScript && !slices.Contains(scripted, name) {
				scripted = append(scripted, name)
			}

			walk(filepath.Join(at, "node_modules"), "")
		}
	}

	walk(root, "")
	slices.Sort(scripted)

	return all, scripted
}

// npmScriptPackage returns the name of the package in dir, from its
// package.json or else folder, and whether npm runs a script when it installs
// it.
func npmScriptPackage(dir, folder string) (string, bool) {
	var pkg struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
	}

	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		_ = json.Unmarshal(data, &pkg)
	}

	if pkg.Name == "" {
		pkg.Name = folder
	}

	for _, hook := range []string{"preinstall", "install", "postinstall"} {
		if pkg.Scripts[hook] != "" {
			return pkg.Name, true
		}
	}

	_, err := os.Stat(filepath.Join(dir, "binding.gyp"))

	return pkg.Name, err == nil
}
