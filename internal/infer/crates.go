package infer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/y3owk1n/oku/internal/crates"
)

// CratesOptions say how FromCrates writes a manifest.
type CratesOptions struct {
	// API and Downloads replace the URLs of the crates.io API and of its
	// downloads when set.
	API, Downloads string
	// Version is the version the user wrote after "@". Empty means the newest.
	Version string
	// Rust is the ref of a package that provides cargo and rustc, or "".
	// Without one, the build uses the cargo on the user's PATH.
	Rust string
}

// FromCrates returns manifest TOML for the crate called name. The manifest
// follows the crate's versions. Its build downloads the .crate file, which oku
// checks against the sha256 that crates.io publishes, vendors its dependencies
// as its Cargo.lock pins them, and installs its programs from them.
func (inf *Inferrer) FromCrates(ctx context.Context, name string, opts CratesOptions) (string, error) {
	c, err := crates.Read(ctx, inf.Hosts.HTTP, opts.API, name)
	if errors.Is(err, crates.ErrNotFound) {
		return "", fmt.Errorf("cargo:%s: %w", name, err)
	}

	if err != nil {
		return "", fmt.Errorf("read the crate %s: %w", name, err)
	}

	// The programs of the newest release tell whether the crate has any.
	var picked *crates.Version

	for i, v := range c.Versions {
		if opts.Version == v.Number || opts.Version == "" && !v.Yanked && !strings.Contains(v.Number, "-") {
			picked = &c.Versions[i]

			break
		}
	}

	switch {
	case picked == nil && opts.Version != "":
		return "", fmt.Errorf("the crate %s has no version %s", name, opts.Version)
	case picked == nil:
		return "", fmt.Errorf("the crate %s has no release", name)
	case len(picked.Programs) == 0:
		return "", fmt.Errorf("the crate %s %s has no programs, it is a library", name, picked.Number)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "[package]\nname = %q\n", strings.ToLower(name))

	if c.Description != "" {
		fmt.Fprintf(&b, "description = %q\n", strings.Join(strings.Fields(c.Description), " "))
	}

	fmt.Fprintf(&b, "homepage = %q\n\n", "https://crates.io/crates/"+name)
	fmt.Fprintf(&b, "[version]\nfrom = \"crates\"\nrepo = %q\n\n[build]\n", name)

	if opts.Rust != "" {
		fmt.Fprintf(&b, "deps = [%q]\n", opts.Rust)
	} else {
		b.WriteString("needs = [\"cargo\"]\n")
	}

	// oku takes the digest of the .crate file from crates.io, so the source
	// names none.
	fmt.Fprintf(&b, "source = { url = %q, strip = 1 }\n",
		crates.URL(opts.Downloads, name, "{{version}}"))
	// The step vendors what Cargo.lock pins and then installs from it.
	fmt.Fprintf(&b, "\n[[build.step]]\nvendor = \"cargo\"\npackage = %q\n", name)

	return b.String(), nil
}
