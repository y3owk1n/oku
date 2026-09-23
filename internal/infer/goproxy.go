package infer

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/goproxy"
)

// GoOptions say how FromGo writes a manifest.
type GoOptions struct {
	// Proxy replaces the URL of the Go module proxy when set.
	Proxy string
	// Version is the version the user wrote after "@". Empty means the newest.
	Version string
	// Go is the ref of a package that provides the go command, or "". Without
	// one, the build uses the go on the user's PATH.
	Go string
}

// majorRe is the last element of a package path that names a major version,
// such as the v2 of example.com/tool/v2.
var majorRe = regexp.MustCompile(`^v[0-9]+$`)

// FromGo returns manifest TOML for the Go package at pkg. The manifest follows
// the versions of the module that holds pkg. Its build downloads the module and
// its dependencies, which the go command checks against the checksum database,
// and installs the program from them with no proxy.
func (inf *Inferrer) FromGo(ctx context.Context, pkg string, opts GoOptions) (string, error) {
	module, err := goproxy.Module(ctx, inf.Hosts.HTTP, opts.Proxy, pkg)
	if errors.Is(err, goproxy.ErrNotFound) {
		return "", fmt.Errorf("go:%s: %w", pkg, err)
	}

	if err != nil {
		return "", fmt.Errorf("find the module of %s: %w", pkg, err)
	}

	if opts.Version != "" {
		versions, err := goproxy.Versions(ctx, inf.Hosts.HTTP, opts.Proxy, module)
		if err != nil {
			return "", fmt.Errorf("list versions of the Go module %s: %w", module, err)
		}

		if !slices.Contains(versions, opts.Version) {
			return "", fmt.Errorf("the Go module %s has no version %s", module, opts.Version)
		}
	}

	// go install names the program after the last element that is no major
	// version.
	name := path.Base(pkg)
	if majorRe.MatchString(name) {
		name = path.Base(path.Dir(pkg))
	}

	var b strings.Builder

	fmt.Fprintf(&b, "[package]\nname = %q\nhomepage = %q\n\n",
		strings.ToLower(name), "https://pkg.go.dev/"+pkg)
	fmt.Fprintf(&b, "[version]\nfrom = \"go\"\nrepo = %q\n\n[build]\n", module)

	if opts.Go != "" {
		fmt.Fprintf(&b, "deps = [%q]\n", opts.Go)
	} else {
		b.WriteString("needs = [\"go\"]\n")
	}

	// The step downloads the modules and then installs the package from them.
	fmt.Fprintf(&b, "\n[[build.step]]\nvendor = \"go\"\npackage = %q\n", pkg)

	return b.String(), nil
}
