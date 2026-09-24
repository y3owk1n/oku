package infer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/pypi"
)

// UV is the ref of uv, which installs a Python package with its dependencies.
// oku infers its manifest from its releases, so it needs no configuration.
const UV = "github:astral-sh/uv"

// PyPIOptions say how FromPyPI writes a manifest.
type PyPIOptions struct {
	// Index replaces the URL of the Python Package Index when set.
	Index string
	// Version is the version the user wrote after "@". Empty means the newest.
	Version string
	// Python is the package that provides python3, or none. Without one,
	// the build and the programs use the python3 on the build's PATH.
	Python manifest.Dep
	// UV is the package that provides uv. Without one, the build uses UV.
	UV manifest.Dep
}

// FromPyPI returns manifest TOML for the Python package called name. The
// manifest follows the package's versions. Its build installs the package with
// its dependencies, and the programs are the package's console scripts.
func (inf *Inferrer) FromPyPI(ctx context.Context, name string, opts PyPIOptions) (string, error) {
	pkg, err := pypi.Read(ctx, inf.Hosts.HTTP, opts.Index, name)
	if errors.Is(err, pypi.ErrNotFound) {
		return "", fmt.Errorf("pypi:%s: %w", name, err)
	}

	if err != nil {
		return "", fmt.Errorf("read the Python package %s: %w", name, err)
	}

	if opts.Version != "" {
		if _, ok := pkg.Versions[opts.Version]; !ok {
			return "", fmt.Errorf("the Python package %s %w %s", name, ErrNoVersion, opts.Version)
		}
	}

	var b strings.Builder

	fmt.Fprintf(&b, "[package]\nname = %q\n", pypi.Normalize(name))

	if pkg.Summary != "" {
		fmt.Fprintf(&b, "description = %q\n", pkg.Summary)
	}

	fmt.Fprintf(&b, "homepage = %q\n\n", "https://pypi.org/project/"+pypi.Normalize(name)+"/")
	fmt.Fprintf(&b, "[version]\nfrom = \"pypi\"\nrepo = %q\n", name)

	uvDep := opts.UV
	if uvDep.Ref == "" {
		uvDep = manifest.Dep{Ref: UV}
	}

	uv := uvDep.TOML()
	deps := uv

	// The programs run through this python, so gc must keep it.
	if opts.Python.Ref != "" {
		fmt.Fprintf(&b, "\n[runtime]\ndeps = [%s]\n", opts.Python.TOML())
		deps = opts.Python.TOML() + ", " + uv
	}

	fmt.Fprintf(&b, "\n[build]\ndeps = [%s]\n", deps)
	fmt.Fprintf(&b, "\n[[build.step]]\nvendor = \"pip\"\npackage = %q\n", name)

	return b.String(), nil
}
