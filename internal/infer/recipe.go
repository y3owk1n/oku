package infer

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
)

// A recipe is what oku takes from the package of another package manager, such
// as a Homebrew cask. oku translates it into a manifest of its own that
// downloads from the vendor and follows the vendor's versions, so the other
// package manager is out of the loop once the manifest exists.
type recipe struct {
	// source names the recipe for messages, such as "the Homebrew cask foo".
	source      string
	name        string
	description string
	homepage    string
	// version is the version the recipe installs today.
	version   string
	artifacts []recipeArtifact
	// follow is where the vendor publishes new versions. It is nil when some
	// artifact has its own, or when oku could not translate the recipe's rule.
	follow *follow
	// fixed says why the manifest cannot follow versions. It is empty when it can.
	fixed string
	// dropped says why a platform of the recipe has no artifact.
	dropped string
	// leftOut are the scripts of the recipe that the manifest leaves out.
	leftOut []string
	// deps are refs of the packages the recipe needs beside it.
	deps []string
}

// recipeArtifact is the download of the recipe for one platform.
type recipeArtifact struct {
	sel platform.Selector
	// url and sha256 are the download of the recipe's version. sha256 is empty
	// when the recipe states none.
	url    string
	sha256 string
	// template is the URL of any version, with "{{version}}" in it. It is empty
	// when the recipe gives no template oku can read.
	template string
	// follow is where versions of this artifact come from, when they differ by
	// platform.
	follow *follow
	strip  int
	bins   []recipeBin
	apps   []string
	fonts  []string
	man    []string
	// launchers are Start Menu shortcuts, each a name and a program in bin.
	launchers [][2]string
	// pathDirs are directories of the download whose programs are all
	// programs of the package.
	pathDirs []string
	// leftArgs are the programs whose arguments the manifest leaves out.
	leftArgs []string
	// leftOut are the scripts of the recipe for this platform that the
	// manifest leaves out.
	leftOut []string
	// pkg says the download is a macOS installer package, and suites are
	// folders of apps in the download. oku opens both to find the apps.
	pkg    bool
	suites []string
	// payloadBins are programs that a package installs at an absolute path.
	payloadBins []recipeBin
}

// recipeBin is a program. name is empty when the program keeps the file's name.
type recipeBin struct {
	name, path string
	// args make oku write a program that runs path with them.
	args []string
}

// follow is a translated [version] table.
type follow struct {
	from, repo, regex, stripPrefix string
}

// versionPattern stands for a version inside a regex that oku builds.
const versionPattern = `([0-9][0-9A-Za-z._+-]*)`

var githubDownloadRe = regexp.MustCompile(
	`^https://github\.com/([A-Za-z0-9-]+/[A-Za-z0-9._-]+)/releases/download/([^/]*)/`,
)

// githubFollow returns the release source of a template that downloads from a
// GitHub release, and whether it is one. The tag must be the version with an
// optional prefix, which becomes strip_prefix.
func githubFollow(template string) (*follow, bool) {
	m := githubDownloadRe.FindStringSubmatch(template)
	if m == nil {
		return nil, false
	}

	prefix, ok := strings.CutSuffix(m[2], "{{version}}")
	if !ok || strings.Contains(prefix, "{{") {
		return nil, false
	}

	f := &follow{from: manifest.FromGitHubReleases, repo: m[1]}
	if prefix != "v" {
		f.stripPrefix = prefix
	}

	return f, true
}

// keyRegex is a regex that reads the string value of key in JSON text.
func keyRegex(key string) string {
	return `"` + regexp.QuoteMeta(key) + `"\s*:\s*"` + versionPattern + `"`
}

// oneGroup returns re when Go can compile it and it has exactly one group,
// which is the version. A regex with no group gets one around the whole.
func oneGroup(re string) (string, bool) {
	compiled, err := regexp.Compile(re)
	if err != nil {
		return "", false
	}

	switch compiled.NumSubexp() {
	case 0:
		return "(" + re + ")", true
	case 1:
		return re, true
	}

	return "", false
}

// errUntranslatable reports that a recipe needs something an oku manifest
// cannot express.
var errUntranslatable = errors.New("oku cannot translate it")

// text writes the manifest. When every artifact has a template and a version
// source, the manifest follows the vendor. Otherwise it pins the recipe's
// version with the recipe's downloads and digests.
func (r recipe) text() (string, error) {
	switch {
	case len(r.artifacts) == 0 && r.dropped != "":
		return "", fmt.Errorf("%s: %s, so %w", r.source, r.dropped, errUntranslatable)
	case len(r.artifacts) == 0:
		return "", fmt.Errorf("%s has no download for macOS, Linux or Windows", r.source)
	}

	fixed := r.fixed
	perArtifact := r.follow == nil

	for _, a := range r.artifacts {
		switch {
		case fixed != "":
		case a.template == "":
			fixed = "its download URL does not follow the version"
		case a.namedAfter(r.version):
			fixed = "the files inside its download are named after the version"
		case perArtifact && a.follow == nil:
			fixed = "it names no source of new versions"
		case perArtifact && !slices.Contains(
			[]string{manifest.FromRedirect, manifest.FromPage, manifest.FromSparkle}, a.follow.from,
		):
			fixed = "each platform has its own source of versions"
		}
	}

	var b strings.Builder

	fmt.Fprintf(&b, "# Translated from %s.\n", r.source)

	var leftArgs []string

	for _, a := range r.artifacts {
		for _, name := range a.leftArgs {
			if !slices.Contains(leftArgs, name) {
				leftArgs = append(leftArgs, name)
			}
		}
	}

	if len(leftArgs) > 0 {
		fmt.Fprintf(
			&b, "# The arguments of %s name folders of Scoop's, so oku runs it without them.\n",
			strings.Join(leftArgs, ", "),
		)
	}

	for _, a := range r.artifacts {
		for _, kind := range a.leftOut {
			if !slices.Contains(r.leftOut, kind) {
				r.leftOut = append(r.leftOut, kind)
			}
		}
	}

	if len(r.leftOut) > 0 {
		fmt.Fprintf(&b, "# oku runs no script of a recipe, so it left out %s.\n", strings.Join(r.leftOut, ", "))
	}

	if fixed != "" {
		fmt.Fprintf(
			&b,
			"# It pins %s because %s. oku update translates it again.\n",
			r.version,
			fixed,
		)
	}

	fmt.Fprintf(&b, "[package]\nname = %q\n", r.name)

	if r.description != "" {
		fmt.Fprintf(&b, "description = %q\n", strings.Join(strings.Fields(r.description), " "))
	}

	if r.homepage != "" {
		fmt.Fprintf(&b, "homepage = %q\n", r.homepage)
	}

	if len(r.deps) > 0 {
		fmt.Fprintf(&b, "\n[runtime]\ndeps = [%s]\n", quoteAll(r.deps))
	}

	switch {
	case fixed != "":
		fmt.Fprintf(&b, "\n[version]\nvalue = %q\n", r.version)
	case !perArtifact:
		fmt.Fprintf(&b, "\n[version]\n%s", r.follow.toml("\n"))
	}

	var launchers [][2]string

	for _, a := range r.artifacts {
		fmt.Fprintf(&b, "\n[[artifact]]\nmatch = %s\n", matchTOML(a.sel))

		if fixed == "" && perArtifact {
			fmt.Fprintf(&b, "version = { %s }\n", strings.TrimSuffix(a.follow.toml(", "), ", "))
		}

		if fixed != "" {
			fmt.Fprintf(&b, "url = %q\n", a.url)

			if a.sha256 != "" {
				fmt.Fprintf(&b, "sha256 = %q\n", a.sha256)
			}
		} else {
			fmt.Fprintf(&b, "url = %q\n", a.template)
		}

		b.WriteString(a.outputs())

		for _, l := range a.launchers {
			if !slices.Contains(launchers, l) {
				launchers = append(launchers, l)
			}
		}
	}

	for _, l := range launchers {
		fmt.Fprintf(&b, "\n[[app]]\nname = %q\nexec = %q\n", l[0], "bin/"+l[1])
	}

	return b.String(), nil
}

// toml writes the keys of f, each followed by sep.
func (f follow) toml(sep string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "from = %q%srepo = %q%s", f.from, sep, f.repo, sep)

	if f.regex != "" {
		fmt.Fprintf(&b, "regex = %q%s", f.regex, sep)
	}

	if f.stripPrefix != "" {
		fmt.Fprintf(&b, "strip_prefix = %q%s", f.stripPrefix, sep)
	}

	return b.String()
}

func (a recipeArtifact) outputs() string {
	var b strings.Builder

	if a.strip > 0 {
		fmt.Fprintf(&b, "strip = %d\n", a.strip)
	}

	if len(a.bins) > 0 {
		entries := make([]string, len(a.bins))

		for i, bin := range a.bins {
			switch {
			case len(bin.args) > 0:
				entries[i] = fmt.Sprintf(
					"{ name = %q, run = %q, args = [%s] }", bin.name, "{{pkg}}/"+bin.path, quoteAll(bin.args),
				)
			case bin.name != "" && bin.name != path.Base(bin.path):
				entries[i] = fmt.Sprintf("{ name = %q, path = %q }", bin.name, bin.path)
			default:
				entries[i] = fmt.Sprintf("%q", bin.path)
			}
		}

		fmt.Fprintf(&b, "bin = [%s]\n", strings.Join(entries, ", "))
	}

	for _, out := range []struct {
		key   string
		paths []string
	}{{"app", a.apps}, {"font", a.fonts}, {"man", a.man}} {
		if len(out.paths) > 0 {
			fmt.Fprintf(&b, "%s = [%s]\n", out.key, quoteAll(out.paths))
		}
	}

	return b.String()
}

// packageName makes a package name of a recipe's name, such as
// "firefox@developer-edition".
func packageName(name string) string {
	name = strings.ToLower(name)

	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}

		return '-'
	}, name)
}

// matchTOML writes the keys of s that are set.
func matchTOML(s platform.Selector) string {
	var parts []string

	for _, kv := range [][2]string{{"os", s.OS}, {"arch", s.Arch}, {"libc", s.Libc}} {
		if kv[1] != "" {
			parts = append(parts, fmt.Sprintf("%s = %q", kv[0], kv[1]))
		}
	}

	return "{ " + strings.Join(parts, ", ") + " }"
}

// namedAfter reports whether a path of an output holds version, which the
// next version would not have.
func (a recipeArtifact) namedAfter(version string) bool {
	paths := slices.Concat(a.apps, a.fonts, a.man)
	for _, b := range a.bins {
		paths = append(paths, b.path)
	}

	return slices.ContainsFunc(paths, func(p string) bool { return strings.Contains(p, version) })
}
