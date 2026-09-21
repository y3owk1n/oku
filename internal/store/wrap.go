package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/shim"
)

// writeWrappers writes the programs of a.Wrap into <tmp>/bin. final is where
// tmp ends up, so the paths in a wrapper are the ones that exist afterwards.
//
// A wrapper is a shell script that ends in "exec". A Windows script cannot pass
// arguments and signals through unchanged. There the wrapper is the spec file
// of a shim, and the profile puts the shim beside it.
func writeWrappers(
	tmp, final string,
	m *manifest.Manifest,
	a manifest.Artifact,
	p platform.Platform,
	deps []Dep,
) error {
	if len(a.Wrap) == 0 {
		return nil
	}

	vars := map[string]string{
		"prefix": final, "pkg": filepath.Join(final, "pkg"),
		"version": m.Version.Value, "tag": m.Tag, "os": p.OS, "arch": p.Arch, "libc": p.Libc,
	}
	for _, dep := range deps {
		vars["dep."+dep.Name+".prefix"] = dep.Prefix
	}

	if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0o755); err != nil {
		return err
	}

	for _, w := range a.Wrap {
		words := make([]string, 0, len(w.Args)+1)

		for _, text := range append([]string{w.Run}, w.Args...) {
			word, err := manifest.Expand(text, vars)
			if err != nil {
				return fmt.Errorf("bin %q: %w", w.Name, err)
			}

			words = append(words, filepath.FromSlash(word))
		}

		var err error

		dest := filepath.Join(tmp, "bin", w.Name)

		if p.OS == "windows" {
			err = shim.Write(dest+".exe", shim.Spec{Target: words[0], Args: words[1:]})
		} else {
			err = writeScript(dest, words)
		}

		if os.IsExist(err) {
			return fmt.Errorf("two outputs are both named %s", w.Name)
		}

		if err != nil {
			return fmt.Errorf("bin %q: %w", w.Name, err)
		}
	}

	return nil
}

// writeScript writes a shell script that replaces itself with words and the
// arguments it got.
func writeScript(dest string, words []string) error {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
	}

	script := "#!/bin/sh\nexec " + strings.Join(quoted, " ") + ` "$@"` + "\n"

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(script); err != nil {
		f.Close()

		return err
	}

	return f.Close()
}
