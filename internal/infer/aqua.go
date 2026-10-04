package infer

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/pem"
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
	"github.com/y3owk1n/oku/internal/shape"
	"github.com/y3owk1n/oku/internal/sigstore"
)

// aquaRegistry is the repo of the aqua registry, which records how thousands
// of GitHub repos name their release files.
const aquaRegistry = "aquaproj/aqua-registry"

// aquaMajor is the major version of the registry whose format oku reads. The
// registry changes its format only in a new major.
const aquaMajor = "v4."

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
	// Cosign signs the download, and Attestations are GitHub artifact
	// attestations of it.
	Cosign       *aquaCosign       `yaml:"cosign"`
	Attestations *aquaAttestations `yaml:"github_artifact_attestations"`
	// Provenance is the SLSA provenance that slsa-github-generator made of it.
	Provenance *aquaProvenance `yaml:"slsa_provenance"`
}

type aquaFile struct {
	Name string `yaml:"name"`
	Src  string `yaml:"src"`
}

type aquaChecksum struct {
	Type      string      `yaml:"type"`
	Asset     string      `yaml:"asset"`
	Algorithm string      `yaml:"algorithm"`
	Enabled   *bool       `yaml:"enabled"`
	Cosign    *aquaCosign `yaml:"cosign"`
}

// aquaCosign is how cosign checks a file: a bundle beside it, and the options
// of cosign verify-blob, which name the certificate's identity.
type aquaCosign struct {
	Enabled *bool `yaml:"enabled"`
	Bundle  *struct {
		Type  string `yaml:"type"`
		Asset string `yaml:"asset"`
	} `yaml:"bundle"`
	Opts []string `yaml:"opts"`
}

type aquaProvenance struct {
	Enabled   *bool  `yaml:"enabled"`
	Type      string `yaml:"type"`
	Asset     string `yaml:"asset"`
	SourceURI string `yaml:"source_uri"`
	SourceTag string `yaml:"source_tag"`
}

type aquaAttestations struct {
	Enabled        *bool  `yaml:"enabled"`
	SignerWorkflow string `yaml:"signer_workflow"`
}

// githubIdentityRe is the certificate identity of a GitHub Actions workflow
// that runs for a release's tag, and githubIdentityPatternRe the pattern of a
// workflow at any ref, which cosign takes as --certificate-identity-regexp.
var (
	githubIdentityRe = regexp.MustCompile(
		`^https://github\.com/([^/]+/[^/]+/\.github/workflows/[^/@]+)@refs/tags/\{\{\s*\.Version\s*\}\}$`,
	)
	githubIdentityPatternRe = regexp.MustCompile(
		`^\^https://github\\\.com/([A-Za-z0-9_.\\-]+/[A-Za-z0-9_.\\-]+/\\\.github/workflows/[A-Za-z0-9_.\\-]+)@\.\+\$$`,
	)
	tagRefRe = regexp.MustCompile(`^refs/tags/\{\{\s*\.Version\s*\}\}$`)
)

// cosignCheck is a cosign check oku can make: the workflow that signs the
// file or the URL of the developer's public key, and a bundle asset of the
// release, or the URLs of a signature and its certificate.
type cosignCheck struct {
	workflow, key, bundle, signature, certificate string
}

// check returns the check that c names, when oku can make it: a bundle, or a
// signature with its certificate, by a GitHub Actions workflow of repo for the
// release's tag, or a bundle or signature by a key. oku needs the log to hold
// a signature by a key, as cosign does by default, so an entry that tells
// cosign to skip the log stays out.
func (c *aquaCosign) check(repo string) (cosignCheck, bool) {
	if c == nil || c.Enabled != nil && !*c.Enabled {
		return cosignCheck{}, false
	}

	// A flag takes the next option as its value, unless that is a flag too.
	opts := map[string]string{}
	for i, opt := range c.Opts {
		if strings.HasPrefix(opt, "--") && i+1 < len(c.Opts) && !strings.HasPrefix(c.Opts[i+1], "--") {
			opts[opt] = c.Opts[i+1]
		}
	}

	keyed := opts["--key"] != "" && opts["--certificate-oidc-issuer"] == "" &&
		!slices.Contains(c.Opts, "--insecure-ignore-tlog")

	if !keyed && (opts["--certificate-oidc-issuer"] != sigstore.GitHubIssuer ||
		opts["--certificate-github-workflow-repository"] != "" && opts["--certificate-github-workflow-repository"] != repo) {
		return cosignCheck{}, false
	}

	var check cosignCheck

	switch {
	case c.Bundle != nil && c.Bundle.Type == "github_release" && c.Bundle.Asset != "":
		check.bundle = c.Bundle.Asset
	case opts["--signature"] != "" && (keyed || opts["--certificate"] != ""):
		check.signature, check.certificate = opts["--signature"], opts["--certificate"]
	default:
		return cosignCheck{}, false
	}

	if keyed {
		check.key, check.certificate = opts["--key"], ""

		return check, true
	}

	// A pattern names the workflow at any ref, so the ref of the run must be
	// the tag.
	if m := githubIdentityPatternRe.FindStringSubmatch(opts["--certificate-identity-regexp"]); m != nil &&
		tagRefRe.MatchString(opts["--certificate-github-workflow-ref"]) {
		check.workflow = strings.ReplaceAll(m[1], `\`, "")

		return check, true
	}

	if m := githubIdentityRe.FindStringSubmatch(opts["--certificate-identity"]); m != nil {
		check.workflow = m[1]

		return check, true
	}

	return cosignCheck{}, false
}

// urls renders the bundle, signature and certificate of check for one
// download of p, whose release files are at releases, and the URL of its key.
func (check cosignCheck) urls(
	p aquaPackage,
	releases string,
	vars map[string]string,
) (bundle, signature, certificate, key string, err error) {
	if key, err = p.render(check.key, vars); err != nil {
		return "", "", "", "", err
	}

	if check.bundle != "" {
		if bundle, err = p.render(check.bundle, vars); err != nil {
			return "", "", "", "", err
		}

		return releases + bundle, "", "", key, nil
	}

	if signature, err = p.render(check.signature, vars); err != nil {
		return "", "", "", "", err
	}

	certificate, err = p.render(check.certificate, vars)

	return "", signature, certificate, key, err
}

// FromAqua returns manifest TOML for the GitHub repo owner/repo, translated
// from its entry in the aqua registry, which names the release files of each
// platform. The manifest follows the repo's releases.
func (inf *Inferrer) FromAqua(ctx context.Context, repo string) (string, error) {
	latest, err := inf.Hosts.GitHub("").Release(ctx, aquaRegistry, "")
	if err != nil {
		return "", fmt.Errorf("read the newest release of the aqua registry: %w", err)
	}

	if !strings.HasPrefix(latest.Tag, aquaMajor) {
		return "", fmt.Errorf(
			"aqua:%s: the aqua registry is at %s, whose format this oku does not read. Check for a newer oku",
			repo, latest.Tag,
		)
	}

	data, err := inf.Hosts.GitHub("").File(ctx, aquaRegistry, latest.Tag, "pkgs/"+repo+"/registry.yaml")
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
		if err := shape.Check(
			"the aqua registry's entry for "+repo,
			shape.Field{Name: "type", Has: p.Type != "" || p.VersionConstraint == "false"},
			shape.Field{Name: "repo_owner and repo_name", Has: p.RepoOwner != "" && p.RepoName != ""},
		); err != nil {
			return "", err
		}

		if strings.EqualFold(p.RepoOwner+"/"+p.RepoName, repo) {
			r, err := p.recipe()
			if err != nil {
				return "", fmt.Errorf("the aqua registry's entry for %s: %w", repo, err)
			}

			if r.keyURL != "" {
				inf.signingKey(ctx, &r)
			}

			return r.text()
		}
	}

	return "", fmt.Errorf("the aqua registry's entry for %s names another repo", repo)
}

// signingKey reads the public key at r.keyURL for the newest release of the
// repo, and makes it the recipe's signing key. The manifest holds the key and
// not its URL, so oku.lock pins it, and a later translation that finds another
// key needs --accept-key. With no key oku can read, the signatures by it stay
// out.
func (inf *Inferrer) signingKey(ctx context.Context, r *recipe) {
	key := ""

	if rel, err := inf.Hosts.GitHub("").Release(ctx, r.follow.repo, ""); err == nil {
		version := strings.TrimPrefix(strings.TrimPrefix(rel.Tag, r.follow.stripPrefix), "v")
		if url, err := manifest.Expand(r.keyURL, map[string]string{"tag": rel.Tag, "version": version}); err == nil {
			if data, err := inf.Download(ctx, url, inf.Hosts.GitHub("").Auth()); err == nil {
				key = cosignKeyText(data)
			}
		}
	}

	if key == "" {
		for i := range r.artifacts {
			r.artifacts[i].sigstore = sigstoreFiles{}
		}
	}

	r.signingKey = key
}

// cosignKeyText returns a cosign public key in PEM as a manifest names it, the
// base64 between its lines, or "" when data holds none.
func cosignKeyText(data []byte) string {
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}

	text := base64.StdEncoding.EncodeToString(block.Bytes)
	if !manifest.CosignKey(text) {
		return ""
	}

	return text
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

	if o.Cosign == nil {
		o.Cosign = p.Cosign
	}

	if o.Attestations == nil {
		o.Attestations = p.Attestations
	}

	if o.Provenance == nil {
		o.Provenance = p.Provenance
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

	if at := cur.Attestations; at != nil && (at.Enabled == nil || *at.Enabled) && at.SignerWorkflow != "" {
		r.signerWorkflow, r.attestations = at.SignerWorkflow, true
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

	// A manifest names one signer, a workflow or else a key. A signature by
	// another stays out.
	for _, a := range r.artifacts {
		r.signerWorkflow = cmp.Or(r.signerWorkflow, a.signer)
	}

	if r.signerWorkflow == "" {
		for _, a := range r.artifacts {
			r.keyURL = cmp.Or(r.keyURL, a.keyURL)
		}
	}

	for i, a := range r.artifacts {
		if a.signer != r.signerWorkflow || a.keyURL != r.keyURL {
			r.artifacts[i].sigstore = sigstoreFiles{}
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

	repo := p.RepoOwner + "/" + p.RepoName
	releases := "https://github.com/" + repo + "/releases/download/{{tag}}/"

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

		if check, ok := c.Cosign.check(repo); ok {
			if bundle, sig, cert, key, err := check.urls(p, releases, vars); err == nil {
				a.sigstore.sha256URLBundle, a.sigstore.sha256URLSignature, a.sigstore.sha256URLCertificate = bundle, sig, cert
				a.signer, a.keyURL = check.workflow, key
			}
		}
	}

	// oku checks provenance for the repo of the releases and the release's
	// tag, so an entry that names another source or tag stays out.
	if pr := p.Provenance; pr != nil && (pr.Enabled == nil || *pr.Enabled) && pr.Type == "github_release" &&
		pr.Asset != "" && (pr.SourceURI == "" || pr.SourceURI == "github.com/"+repo) && pr.SourceTag == "" &&
		p.Type == "github_release" {
		if asset, err := p.render(pr.Asset, vars); err == nil {
			a.provenance = releases + asset
		}
	}

	// The download and its checksum file must have one signer.
	if check, ok := p.Cosign.check(repo); ok {
		if bundle, sig, cert, key, err := check.urls(p, releases, vars); err == nil &&
			(a.signer == "" && a.keyURL == "" || a.signer == check.workflow && a.keyURL == key) {
			a.sigstore.sigstoreBundle, a.sigstore.sigstoreSignature, a.sigstore.sigstoreCertificate = bundle, sig, cert
			a.signer, a.keyURL = check.workflow, key
		}
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
