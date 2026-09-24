package infer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
)

// aquaRegistry is the repo of the aqua registry, which records how thousands
// of GitHub repos name their release files.
const aquaRegistry = "aquaproj/aqua-registry"

// aquaPackage is one package of the aqua registry, or one of its overrides.
type aquaPackage struct {
	Type                string            `yaml:"type"`
	RepoOwner           string            `yaml:"repo_owner"`
	RepoName            string            `yaml:"repo_name"`
	Description         string            `yaml:"description"`
	Asset               string            `yaml:"asset"`
	URL                 string            `yaml:"url"`
	Format              string            `yaml:"format"`
	Files               []aquaFile        `yaml:"files"`
	Replacements        map[string]string `yaml:"replacements"`
	Overrides           []aquaPackage     `yaml:"overrides"`
	GOOS                string            `yaml:"goos"`
	GOArch              string            `yaml:"goarch"`
	SupportedEnvs       []string          `yaml:"supported_envs"`
	Rosetta2            bool              `yaml:"rosetta2"`
	WindowsArmEmulation bool              `yaml:"windows_arm_emulation"`
	CompleteWindowsExt  *bool             `yaml:"complete_windows_ext"`
	VersionPrefix       string            `yaml:"version_prefix"`
	VersionConstraint   string            `yaml:"version_constraint"`
	VersionOverrides    []aquaPackage     `yaml:"version_overrides"`
	NoAsset             bool              `yaml:"no_asset"`
	Checksum            *aquaChecksum     `yaml:"checksum"`
}

type aquaFile struct {
	Name string `yaml:"name"`
	Src  string `yaml:"src"`
}

type aquaChecksum struct {
	Type      string `yaml:"type"`
	Asset     string `yaml:"asset"`
	Algorithm string `yaml:"algorithm"`
	Enabled   *bool  `yaml:"enabled"`
}

// FromAqua returns manifest TOML for the GitHub repo owner/repo, translated
// from its entry in the aqua registry, which names the release files of each
// platform. The manifest follows the repo's releases.
func (inf *Inferrer) FromAqua(ctx context.Context, repo string) (string, error) {
	data, err := inf.Hosts.GitHub("").File(ctx, aquaRegistry, "HEAD", "pkgs/"+repo+"/registry.yaml")
	if errors.Is(err, forge.ErrNotFound) {
		return "", fmt.Errorf("aqua:%s: the aqua registry has no entry for it", repo)
	}

	if err != nil {
		return "", fmt.Errorf("read the aqua registry's entry for %s: %w", repo, err)
	}

	var registry struct {
		Packages []aquaPackage `yaml:"packages"`
	}

	if err := yaml.Unmarshal(data, &registry); err != nil {
		return "", fmt.Errorf("read the aqua registry's entry for %s: %w", repo, err)
	}

	for _, p := range registry.Packages {
		if strings.EqualFold(p.RepoOwner+"/"+p.RepoName, repo) {
			r, err := p.recipe()
			if err != nil {
				return "", fmt.Errorf("the aqua registry's entry for %s: %w", repo, err)
			}

			return r.text()
		}
	}

	return "", fmt.Errorf("the aqua registry's entry for %s names another repo", repo)
}

// current returns the package as its newest releases have it. The registry
// keeps old releases apart with version_overrides, and the one whose constraint
// is "true" covers the rest.
func (p aquaPackage) current() (aquaPackage, bool) {
	if p.VersionConstraint == "" || p.VersionConstraint == "true" {
		return p, true
	}

	for _, o := range slices.Backward(p.VersionOverrides) {
		if o.VersionConstraint == "true" {
			return o.over(p), true
		}
	}

	return aquaPackage{}, false
}

// over returns o with what it leaves out taken from p.
func (o aquaPackage) over(p aquaPackage) aquaPackage {
	o.Type = cmp.Or(o.Type, p.Type)
	o.RepoOwner, o.RepoName = p.RepoOwner, p.RepoName
	o.Description = cmp.Or(o.Description, p.Description)
	o.Asset = cmp.Or(o.Asset, p.Asset)
	o.URL = cmp.Or(o.URL, p.URL)
	o.Format = cmp.Or(o.Format, p.Format)
	o.VersionPrefix = cmp.Or(o.VersionPrefix, p.VersionPrefix)

	if o.Files == nil {
		o.Files = p.Files
	}

	// The replacements of an override add to those it overrides.
	replacements := maps.Clone(p.Replacements)
	if replacements == nil {
		replacements = map[string]string{}
	}

	maps.Copy(replacements, o.Replacements)
	o.Replacements = replacements

	if o.SupportedEnvs == nil {
		o.SupportedEnvs = p.SupportedEnvs
	}

	if o.Checksum == nil {
		o.Checksum = p.Checksum
	}

	if o.CompleteWindowsExt == nil {
		o.CompleteWindowsExt = p.CompleteWindowsExt
	}

	return o
}

// aquaPlatforms are the platforms a translated manifest may cover.
var aquaPlatforms = []platform.Selector{
	{OS: "darwin", Arch: "arm64"},
	{OS: "darwin", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
	{OS: "linux", Arch: "amd64"},
	{OS: "windows", Arch: "arm64"},
	{OS: "windows", Arch: "amd64"},
}

func (p aquaPackage) recipe() (recipe, error) {
	cur, ok := p.current()

	switch {
	case !ok:
		return recipe{}, fmt.Errorf("no rule covers its newest releases, so %w", errUntranslatable)
	case cur.NoAsset:
		return recipe{}, fmt.Errorf("its releases have no files, so %w", errUntranslatable)
	case cur.Type != "github_release" && cur.Type != "http":
		return recipe{}, fmt.Errorf("oku reads github_release and http entries, not %s, so %w",
			cur.Type, errUntranslatable)
	}

	r := recipe{
		source:      "the aqua registry's entry for " + p.RepoOwner + "/" + p.RepoName,
		name:        packageName(p.RepoName),
		description: cur.Description,
		homepage:    "https://github.com/" + p.RepoOwner + "/" + p.RepoName,
		follow: &follow{
			from: manifest.FromGitHubReleases, repo: p.RepoOwner + "/" + p.RepoName,
			stripPrefix: cur.VersionPrefix,
		},
	}

	for _, sel := range aquaPlatforms {
		a, ok, err := cur.artifact(sel)
		if err != nil {
			return recipe{}, err
		}

		if ok {
			r.artifacts = append(r.artifacts, a)
		}
	}

	return r, nil
}

// artifact is the download of cur for sel, and whether cur has one.
func (cur aquaPackage) artifact(sel platform.Selector) (recipeArtifact, bool, error) {
	// An Intel build runs on arm64 under Rosetta 2 on macOS, and under
	// emulation on Windows.
	build := sel
	if sel.Arch == "arm64" && (sel.OS == "darwin" && cur.Rosetta2 || sel.OS == "windows" && cur.WindowsArmEmulation) {
		build.Arch = "amd64"
	}

	if !cur.supports(sel) && !cur.supports(build) {
		return recipeArtifact{}, false, nil
	}

	p := cur

	for _, o := range cur.Overrides {
		if (o.GOOS == "" || o.GOOS == build.OS) && (o.GOArch == "" || o.GOArch == build.Arch) {
			p = o.over(cur)

			break
		}
	}

	vars := map[string]string{
		"OS":     cmp.Or(p.Replacements[build.OS], build.OS),
		"Arch":   cmp.Or(p.Replacements[build.Arch], build.Arch),
		"Format": p.Format,
	}

	asset, err := p.render(p.Asset, vars)
	if err != nil {
		return recipeArtifact{}, false, err
	}

	// aqua names a program that is its own download with ".exe" on Windows.
	if p.Format == "raw" && sel.OS == "windows" && p.windowsExt() && path.Ext(asset) == "" {
		asset += ".exe"
	}

	vars["Asset"] = asset
	vars["AssetWithoutExt"] = strings.TrimSuffix(asset, "."+p.Format)

	releases := "https://github.com/" + p.RepoOwner + "/" + p.RepoName + "/releases/download/{{tag}}/"

	url := releases + asset
	if p.Type == "http" {
		if url, err = p.render(p.URL, vars); err != nil {
			return recipeArtifact{}, false, err
		}
	}

	a := recipeArtifact{sel: sel, template: url}

	if c := p.Checksum; c != nil && (c.Enabled == nil || *c.Enabled) &&
		c.Type == "github_release" && c.Algorithm == "sha256" {
		sums, err := p.render(c.Asset, vars)
		if err != nil {
			return recipeArtifact{}, false, err
		}

		a.sha256URL = releases + sums
	}

	if err := a.aquaFiles(p, vars, sel.OS == "windows"); err != nil {
		return recipeArtifact{}, false, err
	}

	return a, true, nil
}

// supports reports whether supported_envs takes sel. An entry is an OS, an
// arch, "os/arch" or "all".
func (p aquaPackage) supports(sel platform.Selector) bool {
	return len(p.SupportedEnvs) == 0 || slices.ContainsFunc(p.SupportedEnvs, func(env string) bool {
		return env == "all" || env == sel.OS || env == sel.Arch || env == sel.OS+"/"+sel.Arch
	})
}

// aquaFiles makes the programs of p. A folder named after the version at the
// top of the archive becomes strip, since a path in bin holds no version.
func (a *recipeArtifact) aquaFiles(p aquaPackage, vars map[string]string, windows bool) error {
	files := p.Files
	if len(files) == 0 {
		files = []aquaFile{{Name: p.RepoName}}
	}

	withExt := func(s string) string {
		if windows && p.windowsExt() && path.Ext(s) != ".exe" {
			return s + ".exe"
		}

		return s
	}

	for _, f := range files {
		// oku saves a download that is the program itself under its name.
		if p.Format == "raw" {
			a.bins = append(a.bins, recipeBin{path: withExt(f.Name)})

			continue
		}

		src, err := p.render(cmp.Or(f.Src, f.Name), vars)
		if err != nil {
			return err
		}

		parts := strings.Split(src, "/")
		strip := 0

		for strip < len(parts)-1 && strings.Contains(parts[strip], "{{") {
			strip++
		}

		if strings.Contains(strings.Join(parts[strip:], "/"), "{{") || a.strip != 0 && a.strip != strip {
			return fmt.Errorf("the file %s is named after the version, so %w", src, errUntranslatable)
		}

		a.strip = strip
		bin := recipeBin{path: withExt(strings.Join(parts[strip:], "/"))}

		if name := withExt(f.Name); name != path.Base(bin.path) {
			bin.name = name
		}

		a.bins = append(a.bins, bin)
	}

	return nil
}

var aquaTemplateRe = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)

// render expands an aqua template. The version becomes oku's {{tag}} or
// {{version}}, and the rest takes its value from vars. A function that oku has
// no match for fails.
func (p aquaPackage) render(t string, vars map[string]string) (string, error) {
	var bad string

	out := aquaTemplateRe.ReplaceAllStringFunc(t, func(m string) string {
		expr := strings.Join(strings.Fields(aquaTemplateRe.FindStringSubmatch(m)[1]), " ")

		switch expr {
		case ".Version":
			return "{{tag}}"
		case ".SemVer":
			return "{{version}}"
		case "trimV .Version":
			// oku reads a leading "v" as optional, so the version is the tag
			// without it, unless another prefix comes first.
			if p.VersionPrefix == "" {
				return "{{version}}"
			}
		}

		if v, ok := vars[strings.TrimPrefix(expr, ".")]; ok && strings.HasPrefix(expr, ".") {
			return v
		}

		bad = cmp.Or(bad, m)

		return m
	})

	if bad != "" {
		return "", fmt.Errorf("oku has no match for %s in %s, so %w", bad, t, errUntranslatable)
	}

	return out, nil
}

// windowsExt reports whether aqua adds ".exe" to the programs of p on Windows,
// which it does unless complete_windows_ext is false.
func (p aquaPackage) windowsExt() bool {
	return p.CompleteWindowsExt == nil || *p.CompleteWindowsExt
}
