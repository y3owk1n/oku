package infer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/npm"
)

// NPMOptions say how FromNPM writes a manifest.
type NPMOptions struct {
	// Registry replaces the URL of the npm registry when set.
	Registry string
	// Version is the version whose programs FromNPM reads, the way the user
	// wrote it after "@". Empty means the newest.
	Version string
	// Node is the ref of a package that provides node, or "". With one, every
	// program runs through it. Without one, a program runs the node on PATH.
	Node string
	// NodeName is the name of the package at Node.
	NodeName string
}

// FromNPM returns manifest TOML for the npm package called name. The manifest
// follows the package's versions, and its programs are the package's "bin"
// entries.
func (inf *Inferrer) FromNPM(ctx context.Context, name string, opts NPMOptions) (string, error) {
	pkg, err := npm.Read(ctx, inf.Hosts.HTTP, opts.Registry, name)
	if errors.Is(err, npm.ErrNotFound) {
		return "", fmt.Errorf("npm:%s: %w", name, err)
	}

	if err != nil {
		return "", fmt.Errorf("read the npm package %s: %w", name, err)
	}

	version := opts.Version
	if version == "" {
		version = pkg.Latest
	}

	published, ok := pkg.Versions[version]
	if !ok {
		return "", fmt.Errorf("the npm package %s has no version %s", name, version)
	}

	if len(published.Bin) == 0 {
		return "", fmt.Errorf(
			"the npm package %s has no programs, its package.json has no \"bin\"", name,
		)
	}

	programs := make([]string, 0, len(published.Bin))
	for program := range published.Bin {
		programs = append(programs, program)
	}

	slices.Sort(programs)

	var b strings.Builder

	fmt.Fprintf(
		&b, "[package]\nname = %q\nhomepage = %q\n\n",
		strings.ToLower(npm.BaseName(name)), "https://www.npmjs.com/package/"+name,
	)
	fmt.Fprintf(&b, "[version]\nfrom = \"npm\"\nrepo = %q\n", name)

	if opts.Node != "" {
		fmt.Fprintf(&b, "\n[runtime]\ndeps = [%q]\n", opts.Node)
	}

	// A package that lists dependencies gets them installed with it. That takes
	// npm, which comes with the node package.
	if published.Dependencies && opts.Node != "" {
		b.WriteString(npmBuild(name, opts, programs, published.Bin))

		return b.String(), nil
	}

	if published.Dependencies {
		b.WriteString(
			"\n# This package lists dependencies. oku installs its download and nothing else,\n" +
				"# so it runs when the download bundles them. With runtimes.node in config.toml,\n" +
				"# oku installs the dependencies too.\n",
		)
	}

	url := swap(published.Tarball, version, "{{version}}")

	// The tarball keeps everything under "package/".
	artifact := func(match, node string) {
		fmt.Fprintf(&b, "\n[[artifact]]\n%surl = %q\nstrip = 1\nbin = [", match, url)

		for _, program := range programs {
			script := "{{pkg}}/" + strings.TrimPrefix(published.Bin[program], "./")

			// "env" finds node on PATH. A script's own "#!/usr/bin/env node" line
			// would do the same, and needs a mode that a download may not have.
			run, args := "/usr/bin/env", fmt.Sprintf("%q, %q", "node", script)
			if node != "" {
				run, args = "{{dep."+opts.NodeName+".prefix}}/bin/"+node, fmt.Sprintf("%q", script)
			}

			fmt.Fprintf(&b, "\n  { name = %q, run = %q, args = [%s] },", program, run, args)
		}

		b.WriteString("\n]\n")
	}

	if opts.Node == "" {
		artifact("match = { os = \"linux\" }\n", "")
		artifact("match = { os = \"darwin\" }\n", "")

		return b.String(), nil
	}

	artifact("match = { os = \"windows\" }\n", "node.exe")
	artifact("", "node")

	return b.String(), nil
}

// npmBuild returns the [build] of an npm package that needs its dependencies.
// One vendor step installs the package with them, as of the time the version
// was published, and an install step writes the programs.
func npmBuild(name string, opts NPMOptions, programs []string, bin map[string]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "\n[build]\ndeps = [%q]\n", opts.Node)
	fmt.Fprintf(&b, "\n[[build.step]]\nvendor = \"npm\"\npackage = %q\n", name)

	// oku cannot run this build on Windows yet, so the programs are for unix.
	b.WriteString("\n[[build.step]]\ninstall = { bin = [")

	for _, program := range programs {
		fmt.Fprintf(
			&b, "\n  { name = %q, run = %q, args = [%q] },", program,
			"{{dep."+opts.NodeName+".prefix}}/bin/node",
			"{{prefix}}/lib/node_modules/"+name+"/"+strings.TrimPrefix(bin[program], "./"),
		)
	}

	b.WriteString("\n] }\n")

	return b.String()
}
