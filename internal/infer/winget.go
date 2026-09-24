package infer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/resolve"
)

// wingetRepo is the repo of winget's community manifests.
const wingetRepo = "microsoft/winget-pkgs"

var wingetIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_+-]*(\.[A-Za-z0-9_+-]+)+$`)

// ValidWinget reports whether id is a winget package identifier, such as
// "BurntSushi.ripgrep.MSVC".
func ValidWinget(id string) bool {
	return wingetIDRe.MatchString(id)
}

// wingetInstaller is one installer of a winget manifest, or the defaults at its
// top that each installer may override.
type wingetInstaller struct {
	Architecture         string       `yaml:"Architecture"`
	InstallerType        string       `yaml:"InstallerType"`
	NestedInstallerType  string       `yaml:"NestedInstallerType"`
	NestedInstallerFiles []wingetFile `yaml:"NestedInstallerFiles"`
	InstallerURL         string       `yaml:"InstallerUrl"`
	InstallerSha256      string       `yaml:"InstallerSha256"`
	Scope                string       `yaml:"Scope"`
	InstallerLocale      string       `yaml:"InstallerLocale"`
	Commands             []string     `yaml:"Commands"`
}

type wingetFile struct {
	RelativeFilePath     string `yaml:"RelativeFilePath"`
	PortableCommandAlias string `yaml:"PortableCommandAlias"`
}

// over returns i with what it leaves out taken from top.
func (i wingetInstaller) over(top wingetInstaller) wingetInstaller {
	i.InstallerType = cmp.Or(i.InstallerType, top.InstallerType)
	i.NestedInstallerType = cmp.Or(i.NestedInstallerType, top.NestedInstallerType)
	i.Scope = cmp.Or(i.Scope, top.Scope)
	i.InstallerLocale = cmp.Or(i.InstallerLocale, top.InstallerLocale)

	if i.NestedInstallerFiles == nil {
		i.NestedInstallerFiles = top.NestedInstallerFiles
	}

	if i.Commands == nil {
		i.Commands = top.Commands
	}

	return i
}

// FromWinget returns manifest TOML translated from the newest version of the
// winget package id.
func (inf *Inferrer) FromWinget(ctx context.Context, id string) (string, error) {
	dir := "manifests/" + strings.ToLower(id[:1]) + "/" + strings.ReplaceAll(id, ".", "/")

	names, err := inf.Hosts.GitHubDir(ctx, wingetRepo, "HEAD", dir)
	if errors.Is(err, forge.ErrNotFound) {
		return "", fmt.Errorf("winget:%s: winget has no such package", id)
	}

	if err != nil {
		return "", fmt.Errorf("list the versions of the winget package %s: %w", id, err)
	}

	// A folder is a version. A package whose identifier is a prefix of another's
	// has the other's folders beside its versions, which start with a letter.
	version := ""

	for _, name := range names {
		if name != "" && name[0] >= '0' && name[0] <= '9' && (version == "" || resolve.Compare(name, version) > 0) {
			version = name
		}
	}

	if version == "" {
		return "", fmt.Errorf("winget:%s: the package has no version", id)
	}

	read := func(file string, into any) error {
		data, err := inf.Hosts.GitHub("").File(ctx, wingetRepo, "HEAD", dir+"/"+version+"/"+file)
		if err != nil {
			return fmt.Errorf("read %s of the winget package %s: %w", file, id, err)
		}

		return yaml.Unmarshal(data, into)
	}

	var installers struct {
		wingetInstaller `yaml:",inline"`

		Installers []wingetInstaller `yaml:"Installers"`
	}

	if err := read(id+".installer.yaml", &installers); err != nil {
		return "", err
	}

	var versionFile struct {
		DefaultLocale string `yaml:"DefaultLocale"`
	}

	if err := read(id+".yaml", &versionFile); err != nil {
		return "", err
	}

	var locale struct {
		ShortDescription string `yaml:"ShortDescription"`
		PackageURL       string `yaml:"PackageUrl"`
	}

	// A package without its default locale still translates.
	_ = read(id+".locale."+versionFile.DefaultLocale+".yaml", &locale)

	r := recipe{
		source: "the winget package " + id + " " + version,
		// An identifier is Publisher.Package, with a variant after it at times.
		name:        packageName(strings.Split(id, ".")[1]),
		description: locale.ShortDescription,
		homepage:    locale.PackageURL,
		version:     version,
	}

	var refused error

	for _, arch := range []struct{ winget, oku string }{
		{"x64", "amd64"}, {"arm64", "arm64"}, {"x86", "386"}, {"neutral", ""},
	} {
		i, ok := pickInstaller(installers.Installers, installers.wingetInstaller, arch.winget)
		if !ok {
			continue
		}

		a, err := inf.wingetArtifact(ctx, i, r.name, version, platform.Selector{OS: "windows", Arch: arch.oku})
		if err != nil {
			refused = cmp.Or(refused, err)

			continue
		}

		r.artifacts = append(r.artifacts, a)
	}

	if len(r.artifacts) == 0 && refused != nil {
		return "", fmt.Errorf("winget:%s: %w", id, refused)
	}

	// winget has no rule for new versions, so a manifest follows the releases
	// its download comes from, and any other pins its version.
	for i := range r.artifacts {
		if f, ok := githubFollow(r.artifacts[i].template); ok {
			r.artifacts[i].follow = f
		}
	}

	r.follow = sharedFollow(r.artifacts)

	return r.text()
}

// pickInstaller returns the installer for arch, preferring one that oku can
// place, then one for the user's scope and for English.
func pickInstaller(all []wingetInstaller, top wingetInstaller, arch string) (wingetInstaller, bool) {
	var fit []wingetInstaller

	for _, i := range all {
		if i = i.over(top); i.Architecture == arch {
			fit = append(fit, i)
		}
	}

	if len(fit) == 0 {
		return wingetInstaller{}, false
	}

	slices.SortStableFunc(fit, func(a, b wingetInstaller) int {
		rank := func(i wingetInstaller) int {
			n := 0
			if !wingetPlaced(i) {
				n += 4
			}

			if i.Scope == "machine" {
				n += 2
			}

			if i.InstallerLocale != "" && !strings.HasPrefix(i.InstallerLocale, "en") {
				n++
			}

			return n
		}

		return rank(a) - rank(b)
	})

	return fit[0], true
}

// wingetArtifact translates one installer of the package called name. oku
// places a portable program or a zip of them as it is, and opens an MSI to find
// the programs its commands name. It runs no other installer.
func (inf *Inferrer) wingetArtifact(
	ctx context.Context,
	i wingetInstaller,
	name, version string,
	sel platform.Selector,
) (recipeArtifact, error) {
	a := recipeArtifact{sel: sel, url: i.InstallerURL, sha256: strings.ToLower(i.InstallerSha256)}

	if strings.Contains(i.InstallerURL, version) {
		a.template = strings.ReplaceAll(i.InstallerURL, version, "{{version}}")
	}

	switch kind := i.InstallerType; {
	case kind == "portable":
		name := cmp.Or(first(i.Commands), strings.TrimSuffix(path.Base(i.InstallerURL), path.Ext(i.InstallerURL)))
		a.bins = []recipeBin{{path: name + ".exe"}}
	case kind == "zip" && i.NestedInstallerType == "portable":
		for _, f := range i.NestedInstallerFiles {
			at := winPath(f.RelativeFilePath)

			// strip leaves out a folder named after the version, since the next
			// version names it otherwise.
			if top, rest, ok := strings.Cut(at, "/"); ok && strings.Contains(top, version) {
				a.strip, at = 1, rest
			}

			bin := recipeBin{path: at}
			if f.PortableCommandAlias != "" {
				bin.name = f.PortableCommandAlias + path.Ext(at)
			}

			a.bins = append(a.bins, bin)
		}
	case wingetPlaced(i):
		files, err := inf.Inspect(ctx, a.url, forge.Auth{})
		if err != nil {
			return recipeArtifact{}, fmt.Errorf("open %s to find its programs: %w", a.url, err)
		}

		// Without commands, the program is the one named after the package.
		commands := i.Commands
		if len(commands) == 0 {
			commands = []string{name}
		}

		for _, command := range commands {
			found := onlyExecutable(files, func(p string) bool {
				return strings.EqualFold(path.Base(p), command+".exe")
			})
			if found == "" {
				return recipeArtifact{}, fmt.Errorf("%s holds no program %s.exe", a.url, command)
			}

			a.bins = append(a.bins, recipeBin{path: found})
		}
	default:
		return recipeArtifact{}, fmt.Errorf(
			"its %s installer runs when it installs, and oku runs no installer, so %w",
			cmp.Or(i.NestedInstallerType, kind), errUntranslatable,
		)
	}

	return a, nil
}

func first(items []string) string {
	if len(items) == 0 {
		return ""
	}

	return items[0]
}

// wingetPlaced reports whether oku can place what installer i installs,
// without running it.
func wingetPlaced(i wingetInstaller) bool {
	kind := i.InstallerType
	if kind == "zip" {
		kind = i.NestedInstallerType
	}

	return slices.Contains([]string{"portable", "msi", "wix"}, kind)
}
