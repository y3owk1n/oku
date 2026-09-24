package infer

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/shape"
)

// CaskAPI is where Homebrew publishes its casks as JSON.
const CaskAPI = "https://formulae.brew.sh/api"

// caskTap is the repo that holds the Ruby source of every cask in CaskAPI.
const caskTap = "Homebrew/homebrew-cask"

// maxRecipe is the most bytes read from one recipe.
const maxRecipe = 4 << 20

var caskTokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9@._+-]*$`)

// ValidCask reports whether token is the name of a Homebrew cask, or
// owner/tap/token of a cask of another tap.
func ValidCask(token string) bool {
	return caskTokenRe.MatchString(token) || caskTapRe.MatchString(token)
}

// caskJSON is what the Homebrew API says about one cask, or about one platform
// of it in variations.
type caskJSON struct {
	Token          string              `json:"token"`
	Desc           string              `json:"desc"`
	Homepage       string              `json:"homepage"`
	URL            string              `json:"url"`
	Version        string              `json:"version"`
	SHA256         string              `json:"sha256"`
	Artifacts      []map[string]any    `json:"artifacts"`
	DependsOn      caskDepends         `json:"depends_on"`
	Variations     map[string]caskJSON `json:"variations"`
	RubySourcePath string              `json:"ruby_source_path"`
	TapGitHead     string              `json:"tap_git_head"`
	Disabled       bool                `json:"disabled"`
	DisableReason  string              `json:"disable_reason"`
}

type caskDepends struct {
	Arch []caskArch `json:"arch"`
}

type caskArch struct {
	Type string `json:"type"`
}

// fits reports whether a build for arch meets d.
func (d caskDepends) fits(arch string) bool {
	want := map[string]string{"arm64": "arm", "amd64": "intel"}[arch]

	return len(d.Arch) == 0 || slices.ContainsFunc(d.Arch, func(a caskArch) bool { return a.Type == want })
}

// FromCask returns manifest TOML translated from the Homebrew cask token. api
// replaces CaskAPI when set.
func (inf *Inferrer) FromCask(ctx context.Context, token, api string) (string, error) {
	if api == "" {
		api = CaskAPI
	}

	data, err := getRecipe(ctx, inf.Hosts.HTTP, strings.TrimRight(api, "/")+"/cask/"+token+".json")
	if errors.Is(err, forge.ErrNotFound) {
		return "", fmt.Errorf("cask:%s: Homebrew has no such cask", token)
	}

	if err != nil {
		return "", fmt.Errorf("read the cask %s: %w", token, err)
	}

	var c caskJSON
	if err := json.Unmarshal(data, &c); err != nil {
		return "", fmt.Errorf("read the cask %s: %w", token, err)
	}

	// The Homebrew API promises no format, so oku checks what it reads.
	if err := shape.Check(
		"the Homebrew API's answer for the cask "+token,
		shape.Field{Name: "url", Has: c.URL != ""},
		shape.Field{Name: "version", Has: c.Version != ""},
		shape.Field{Name: "ruby_source_path", Has: c.RubySourcePath != ""},
		shape.Field{Name: "tap_git_head", Has: c.TapGitHead != ""},
	); err != nil {
		return "", err
	}

	switch {
	case c.Disabled:
		return "", fmt.Errorf("cask:%s: Homebrew disabled the cask: %s", token, c.DisableReason)
	case c.Version == "latest":
		return "", fmt.Errorf(
			"cask:%s: the cask has no version, so oku cannot tell one download from the next",
			token,
		)
	}

	source, err := inf.Hosts.GitHub("").File(ctx, caskTap, c.TapGitHead, c.RubySourcePath)
	if err != nil {
		return "", fmt.Errorf("read the source of the cask %s: %w", token, err)
	}

	r, err := c.recipe(string(source))
	if err != nil {
		return "", fmt.Errorf("cask:%s: %w", token, err)
	}

	for i := range r.artifacts {
		if err := inf.openCask(ctx, token, &r.artifacts[i]); err != nil {
			return "", fmt.Errorf("cask:%s: %w", token, err)
		}
	}

	return r.text()
}

// getRecipe reads one recipe over HTTP.
func getRecipe(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, forge.ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRecipe+1))
	if err == nil && len(data) > maxRecipe {
		err = fmt.Errorf("%s answered with more than %d bytes", url, maxRecipe)
	}

	return data, err
}

// recipe translates the cask. source is its Ruby file, which holds the URL
// template and the livecheck that the JSON leaves out. oku reads them and never
// runs them.
func (c caskJSON) recipe(source string) (recipe, error) {
	r := recipe{
		source:      "the Homebrew cask " + c.Token,
		name:        packageName(c.Token),
		description: c.Desc,
		homepage:    c.Homepage,
		// A cask joins the parts of a version with commas, which a version of
		// oku cannot hold.
		version: strings.ReplaceAll(c.Version, ",", "+"),
	}

	checks, rest := rubyBlocks(source, "livecheck")
	templates := rubyURLs(rest)

	for _, at := range c.platforms() {
		a := recipeArtifact{sel: at.sel, url: at.url, sha256: at.sha256}
		if a.sha256 == "no_check" {
			a.sha256 = ""
		}

		if err := a.caskOutputs(at.artifacts); err != nil {
			// Only the cask's own platform, macOS, makes the translation fail. A
			// Linux build oku cannot place is left out.
			if at.sel.OS == "darwin" {
				return recipe{}, err
			}

			continue
		}

		var vars map[string]string

		for _, t := range templates {
			if a.template, vars = fillTemplate(t, r.version, at.url); a.template != "" {
				break
			}
		}

		// The livecheck is the cask's own rule for new versions. Without one,
		// Homebrew follows the releases of a download from GitHub, and so does oku.
		switch f, ok := githubFollow(a.template); {
		case a.template == "":
		case len(checks) > 0:
			a.follow = caskLivecheck(checks, vars, c.Homepage, a.template, r.version)
		case ok:
			a.follow = f
		}

		r.artifacts = append(r.artifacts, a)
	}

	r.follow = sharedFollow(r.artifacts)

	return r, nil
}

// sharedFollow returns the version source of every artifact when they share
// one, and clears it from them.
func sharedFollow(artifacts []recipeArtifact) *follow {
	if len(artifacts) == 0 || artifacts[0].follow == nil {
		return nil
	}

	first := *artifacts[0].follow

	for _, a := range artifacts {
		if a.follow == nil || *a.follow != first {
			return nil
		}
	}

	for i := range artifacts {
		artifacts[i].follow = nil
	}

	return &first
}

// caskPlatform is one download of a cask.
type caskPlatform struct {
	sel       platform.Selector
	url       string
	sha256    string
	artifacts []map[string]any
}

// platforms lists the downloads of the cask. The top of the JSON is the build
// for arm64 on the newest macOS. variations holds the others, keyed by the
// macOS release, "arm64_<release>" for the arm64 build of an older one, and
// "<arch>_linux" for Linux.
func (c caskJSON) platforms() []caskPlatform {
	arm := caskPlatform{
		sel: platform.Selector{OS: "darwin", Arch: "arm64"},
		url: c.URL, sha256: c.SHA256, artifacts: c.Artifacts,
	}

	archs := c.DependsOn.Arch
	if len(archs) == 1 && archs[0].Type == "intel" {
		arm.sel.Arch = "amd64"

		return append([]caskPlatform{arm}, c.linux()...)
	}

	// The Intel build of the newest macOS is the variation of any release that
	// has the cask's own version, as long as those agree.
	var intel []caskPlatform

	for key, v := range c.Variations {
		if strings.Contains(key, "_") || v.Version != "" && v.Version != c.Version {
			continue
		}

		p := c.variation(v, platform.Selector{OS: "darwin", Arch: "amd64"})
		if !slices.ContainsFunc(intel, func(q caskPlatform) bool { return q.url == p.url }) {
			intel = append(intel, p)
		}
	}

	out := []caskPlatform{arm}

	switch {
	case len(archs) == 1 && archs[0].Type == "arm":
	case len(intel) == 0 || len(intel) == 1 && intel[0].url == arm.url:
		// One download for both arches.
		out[0].sel.Arch = ""
	case len(intel) == 1:
		out = append(out, intel[0])
	}

	return append(out, c.linux()...)
}

func (c caskJSON) linux() []caskPlatform {
	var out []caskPlatform

	for _, arch := range [][2]string{{"arm64_linux", "arm64"}, {"x86_64_linux", "amd64"}} {
		// A Linux build places files of its own. One that has none is the macOS
		// build the API repeats for a cask that has no Linux build.
		if v, ok := c.Variations[arch[0]]; ok && v.Artifacts != nil &&
			(v.Version == "" || v.Version == c.Version) && v.DependsOn.fits(arch[1]) {
			out = append(out, c.variation(v, platform.Selector{OS: "linux", Arch: arch[1]}))
		}
	}

	return out
}

// variation fills what v leaves out from the top of the cask.
func (c caskJSON) variation(v caskJSON, sel platform.Selector) caskPlatform {
	p := caskPlatform{sel: sel, url: v.URL, sha256: v.SHA256, artifacts: v.Artifacts}

	if p.url == "" {
		p.url = c.URL
	}

	if p.sha256 == "" {
		p.sha256 = c.SHA256
	}

	if p.artifacts == nil {
		p.artifacts = c.Artifacts
	}

	return p
}

// caskOutputs reads the artifacts of a cask into a's outputs. oku runs no
// script of a recipe. A script that only sets up the app, before or after the
// install, is left out, and one that makes the files, an installer, makes the
// translation fail.
func (a *recipeArtifact) caskOutputs(artifacts []map[string]any) error {
	type stanza struct{ kind, first, target string }

	var (
		stanzas []stanza
		// moved maps where an artifact stanza puts a folder to the folder.
		moved = map[string]string{}
	)

	for _, artifact := range artifacts {
		for kind, value := range artifact {
			if kind == "target" {
				continue
			}

			args, _ := value.([]any)
			st := stanza{kind: kind}

			if len(args) > 0 {
				st.first, _ = args[0].(string)
			}

			// The target comes beside the list, or as a table in it.
			st.target, _ = artifact["target"].(string)

			for _, arg := range args {
				if m, ok := arg.(map[string]any); ok {
					if t, ok := m["target"].(string); ok {
						st.target = t
					}
				}

				if kind == "uninstall" {
					if err := a.caskUninstall(arg); err != nil {
						return err
					}
				}
			}

			if kind == "artifact" {
				moved[st.target] = st.first
			}

			stanzas = append(stanzas, st)
		}
	}

	for _, st := range stanzas {
		switch st.kind {
		case "app":
			a.apps = append(a.apps, st.first)
		case "font":
			a.fonts = append(a.fonts, st.first)
		case "manpage":
			a.man = append(a.man, st.first)
		case "pkg":
			a.pkg = true
		case "suite":
			a.suites = append(a.suites, st.first)
		case "binary":
			// A file that goes to a folder of shell completions is no program.
			if completionTarget(st.target) {
				continue
			}

			bin, err := caskBinary(st.first, st.target, moved)
			if err != nil {
				return err
			}

			if strings.HasPrefix(bin.path, "/") {
				a.payloadBins = append(a.payloadBins, bin)
			} else {
				a.bins = append(a.bins, bin)
			}
		case "app_image":
			// An AppImage is the program itself, which oku saves under the
			// program's name.
			name := path.Base(cmp.Or(st.target, st.first))
			a.bins = append(a.bins, recipeBin{
				path: strings.ToLower(strings.TrimSuffix(name, path.Ext(name))),
			})
		case "artifact", "uninstall", "zap", "uninstall_preflight", "uninstall_postflight",
			"uninstall_preflight_steps", "uninstall_postflight_steps",
			"bash_completion", "zsh_completion", "fish_completion", "command_wrapper",
			"generate_completions_from_executable":
		case "preflight", "postflight", "preflight_steps", "postflight_steps":
			a.leftOut = append(a.leftOut, st.kind)
		case "installer":
			return fmt.Errorf(
				"the cask runs an installer that makes its files, so %w, write a manifest for it",
				errUntranslatable,
			)
		default:
			return fmt.Errorf(
				"the cask uses the %s stanza, which oku does not place, so %w", st.kind, errUntranslatable,
			)
		}
	}

	switch {
	case len(a.payloadBins) > 0 && !a.pkg:
		return fmt.Errorf(
			"the cask links the program %s from outside its download, so %w",
			a.payloadBins[0].path, errUntranslatable,
		)
	case !a.pkg && len(a.suites) == 0 && len(a.apps)+len(a.fonts)+len(a.bins)+len(a.man) == 0:
		return errors.New(
			"the cask installs nothing that oku places, no app, program, font or man page",
		)
	}

	return nil
}

// caskUninstall reads one entry of a cask's uninstall. A kernel extension
// does not work from a folder, and a background service of the app is left out.
func (a *recipeArtifact) caskUninstall(entry any) error {
	m, _ := entry.(map[string]any)

	switch {
	case m["kext"] != nil:
		return fmt.Errorf("the cask installs a kernel extension, so %w", errUntranslatable)
	case m["launchctl"] != nil && !slices.Contains(a.leftOut, "launchctl"):
		a.leftOut = append(a.leftOut, "launchctl")
	}

	return nil
}

// completionTarget reports whether target is in a folder that a shell reads
// completions from.
func completionTarget(target string) bool {
	for _, dir := range []string{
		"/share/zsh/site-functions/", "/etc/bash_completion.d/",
		"/share/bash-completion/completions/", "/share/fish/vendor_completions.d/",
	} {
		if strings.Contains(target, dir) {
			return true
		}
	}

	return false
}

// caskBinary is a program of a cask. Its path starts at the app folder or in
// the download, or, for a package, is where the package installs it. moved
// maps the target of an artifact stanza to the folder of the download.
func caskBinary(source, target string, moved map[string]string) (recipeBin, error) {
	at, ok := strings.CutPrefix(source, "$APPDIR/")
	if !ok {
		// $HOMEBREW_PREFIX/Caskroom/<token>/<version>/ is where Homebrew unpacks
		// the download.
		if parts := strings.SplitN(source, "/", 5); len(parts) == 5 && parts[0] == "$HOMEBREW_PREFIX" &&
			parts[1] == "Caskroom" {
			at, ok = parts[4], true
		}
	}

	for to, from := range moved {
		if rest, found := strings.CutPrefix(source, to+"/"); found {
			at, ok = path.Join(from, rest), true
		}
	}

	if !ok && strings.HasPrefix(source, "$") {
		return recipeBin{}, fmt.Errorf(
			"the cask links the program %s from outside its download, so %w",
			source,
			errUntranslatable,
		)
	}

	if !ok {
		at = source
	}

	bin := recipeBin{path: at}
	if name := path.Base(cmp.Or(target, source)); name != path.Base(at) {
		bin.name = name
	}

	return bin, nil
}

// openCask finds the outputs of a package or a suite, which the cask token
// does not name file by file, in the download. A program that a package
// installs at an absolute path is the file of the payload whose path ends like
// it.
func (inf *Inferrer) openCask(ctx context.Context, token string, a *recipeArtifact) error {
	if !a.pkg && len(a.suites) == 0 {
		return nil
	}

	files, err := inf.Inspect(ctx, a.url, forge.Auth{})
	if err != nil {
		return fmt.Errorf("open %s to find what it installs: %w", a.url, err)
	}

	for _, bin := range a.payloadBins {
		found := ""

		for _, f := range files {
			// A flat package has its payload at the top.
			_, rel, ok := strings.Cut("/"+f.Path, "/Payload/")
			if ok && strings.HasSuffix(bin.path, "/"+rel) && len(f.Path) > len(found) {
				found = f.Path
			}
		}

		// A script of the package may link the program where the cask says,
		// from a file of the same name that it installs elsewhere.
		if found == "" {
			found = onlyExecutable(files, func(p string) bool { return path.Base(p) == path.Base(bin.path) })
		}

		if found == "" {
			return fmt.Errorf("the package %s holds no %s", a.url, bin.path)
		}

		if bin.name == path.Base(found) {
			bin.name = ""
		}

		a.bins = append(a.bins, recipeBin{name: bin.name, path: found})
	}

	var apps []string

	for _, f := range files {
		// The outermost bundle, since an app may hold other apps.
		i := strings.Index(f.Path, ".app/")
		if i < 0 {
			continue
		}

		app := f.Path[:i+len(".app")]
		inSuite := slices.ContainsFunc(a.suites, func(s string) bool { return path.Dir(app) == s })
		// A package installs its apps into Applications, which is either where
		// its payload goes or a folder in it. Other apps are its helpers.
		inApplications := slices.Contains([]string{"Payload", "Applications"}, path.Base(path.Dir(app)))

		if (a.pkg && inApplications || inSuite) && !slices.Contains(apps, app) {
			apps = append(apps, app)
		}
	}

	// A package or a suite installs its apps beside what the cask links.
	for _, app := range apps {
		if !slices.Contains(a.apps, app) {
			a.apps = append(a.apps, app)
		}
	}

	// A package with no app, such as a JDK, installs the programs of its bin
	// folders, or else the one program named after the cask.
	if a.pkg && len(a.apps)+len(a.bins) == 0 {
		for _, f := range files {
			if f.Executable && strings.Contains(f.Path, "/bin/") && !strings.Contains(f.Path, ".app/") {
				a.bins = append(a.bins, recipeBin{path: f.Path})
			}
		}
	}

	if a.pkg && len(a.apps)+len(a.bins) == 0 {
		if found := onlyExecutable(files, func(p string) bool { return path.Base(p) == token }); found != "" {
			a.bins = append(a.bins, recipeBin{path: found})
		}
	}

	if len(a.apps)+len(a.bins)+len(a.fonts)+len(a.man) == 0 {
		return fmt.Errorf("%s holds no app or program", a.url)
	}

	// The installer puts the parts of a package into one folder, and oku keeps
	// each part apart. A program found in two parts may need the other.
	var parts []string

	for _, b := range a.bins {
		if part, _, ok := strings.Cut(b.path, "/Payload/"); ok && !slices.Contains(parts, part) {
			parts = append(parts, part)
		}
	}

	if len(parts) > 1 {
		return fmt.Errorf(
			"its programs come from %s, parts of a package that the installer puts in one folder, so %w",
			strings.Join(parts, " and "), errUntranslatable,
		)
	}

	return nil
}

// rubyBlocks returns the bodies of the "name do" blocks in src, each up to the
// "end" at its own indent, and src without them.
func rubyBlocks(src, name string) ([]string, string) {
	var (
		bodies []string
		rest   []string
		body   []string
		indent = -1
	)

	for line := range strings.Lines(src) {
		trimmed := strings.TrimSpace(line)
		at := len(line) - len(strings.TrimLeft(line, " "))

		switch {
		case indent < 0 && trimmed == name+" do":
			indent = at
		case indent >= 0 && at == indent && trimmed == "end":
			bodies = append(bodies, strings.Join(body, ""))
			body, indent = nil, -1
		case indent >= 0:
			body = append(body, line)
		default:
			rest = append(rest, line)
		}
	}

	return bodies, strings.Join(rest, "")
}

var rubyURLRe = regexp.MustCompile(`(?m)^\s*url\s+"([^"]+)"`)

// rubyURLs returns every URL template of a cask, in order.
func rubyURLs(src string) []string {
	var urls []string

	for _, m := range rubyURLRe.FindAllStringSubmatch(src, -1) {
		urls = append(urls, m[1])
	}

	return urls
}

var interpolationRe = regexp.MustCompile(`#\{([^}]*)\}`)

// rubyVersions are the parts of a cask's version that a URL template may use,
// and the oku variables that give them. A cask joins the parts of its version
// with commas, where oku's version joins them with "+".
var rubyVersions = map[string]string{
	"version":                     "{{version}}",
	"version.csv.first":           "{{version_part1}}",
	"version.before_comma":        "{{version_part1}}",
	"version.csv.second":          "{{version_part2}}",
	"version.after_comma":         "{{version_part2}}",
	"version.csv.third":           "{{version_part3}}",
	"version.major":               "{{version_major}}",
	"version.minor":               "{{version_minor}}",
	"version.patch":               "{{version_patch}}",
	"version.major_minor":         "{{version_major}}.{{version_minor}}",
	"version.major_minor_patch":   "{{version_major}}.{{version_minor}}.{{version_patch}}",
	"version.no_dots":             "{{version_nodots}}",
	"version.dots_to_underscores": "{{version_underscores}}",
	"version.dots_to_hyphens":     "{{version_dashes}}",
}

// fillTemplate matches the Ruby URL template t against url, the download of
// version, which is oku's form of the cask's version. It returns url with oku's
// variables where t uses the version, and the values the other interpolations
// took, such as the "arm64" of #{arch}. It returns "" when t does not give url,
// or uses the version in a way oku has no variable for.
func fillTemplate(t, version, url string) (string, map[string]string) {
	parts := interpolationRe.FindAllStringSubmatchIndex(t, -1)
	if len(parts) == 0 {
		return "", nil
	}

	var (
		pattern strings.Builder
		exprs   []string
		last    int
	)

	pattern.WriteString("^")

	for _, p := range parts {
		pattern.WriteString(regexp.QuoteMeta(t[last:p[0]]))

		expr := t[p[2]:p[3]]

		oku, isVersion := rubyVersions[expr]

		switch {
		case isVersion:
			value, err := manifest.Expand(oku, map[string]string{"version": version})
			// A cask's own version holds commas, which oku's cannot give.
			if err != nil || expr == "version" && strings.Contains(version, "+") {
				return "", nil
			}

			pattern.WriteString("(" + regexp.QuoteMeta(value) + ")")
		case strings.Contains(expr, "version"):
			return "", nil
		default:
			pattern.WriteString("(.*?)")
		}

		exprs = append(exprs, expr)
		last = p[1]
	}

	pattern.WriteString(regexp.QuoteMeta(t[last:]) + "$")

	m := regexp.MustCompile(pattern.String()).FindStringSubmatchIndex(url)
	if m == nil || !slices.ContainsFunc(exprs, func(e string) bool { return rubyVersions[e] != "" }) {
		return "", nil
	}

	var (
		out  strings.Builder
		vars = map[string]string{}
	)

	last = 0

	for i, expr := range exprs {
		start, end := m[2+2*i], m[3+2*i]
		out.WriteString(url[last:start])

		if oku, isVersion := rubyVersions[expr]; isVersion {
			out.WriteString(oku)
		} else {
			out.WriteString(url[start:end])
			vars[expr] = url[start:end]
		}

		last = end
	}

	out.WriteString(url[last:])

	return out.String(), vars
}

var (
	rubyStrategyRe = regexp.MustCompile(`(?m)^\s*strategy\s+:(\w+)`)
	rubyJSONKeyRe  = regexp.MustCompile(`(?m)^\s*json\["([^"]+)"\]\s*$`)
	rubyRegexRe    = regexp.MustCompile(`(?m)^\s*regex\(?\s*(?:/(.*)/|%r\{(.*)\})([a-z]*)\)?\s*$`)
	rubyURLValueRe = regexp.MustCompile(`(?m)^\s*url\s+(?:"([^"]+)"|:(\w+))`)
	rubySkipRe     = regexp.MustCompile(`(?m)^\s*skip\b`)
)

// caskLivecheck translates the livecheck of a cask for one platform, whose
// interpolations took vars and whose download is template. version is oku's
// form of the cask's version, and a version of several parts needs a regex
// with a group for each. It returns nil when the livecheck says to skip, runs
// Ruby oku cannot read, or uses a strategy oku has no source for.
func caskLivecheck(bodies []string, vars map[string]string, homepage, template, version string) *follow {
	parts := strings.Count(version, "+") + 1
	var live []string

	for _, body := range bodies {
		if !rubySkipRe.MatchString(body) {
			live = append(live, body)
		}
	}

	if len(live) != 1 {
		return nil
	}

	body := live[0]

	repo := ""

	if m := rubyURLValueRe.FindStringSubmatch(body); m != nil {
		switch {
		case m[1] != "":
			repo = interpolationRe.ReplaceAllStringFunc(m[1], func(s string) string {
				expr := s[2 : len(s)-1]
				if v, ok := vars[expr]; ok {
					return v
				}

				// Homebrew fills a part of the version, as in a feed for each major
				// version, with the cask's own version.
				if oku, ok := rubyVersions[expr]; ok {
					if v, err := manifest.Expand(oku, map[string]string{"version": version}); err == nil {
						return v
					}
				}

				return s
			})
		case m[2] == "homepage":
			repo = homepage
		case m[2] == "url":
			// The cask's own download, which only names a repo of releases.
			repo = template
		}
	}

	if repo == "" || strings.Contains(repo, "#{") {
		return nil
	}

	strategy := ""
	if m := rubyStrategyRe.FindStringSubmatch(body); m != nil {
		strategy = m[1]
	}

	// A block that reads fields of a JSON or an XML feed becomes version.json
	// or a regex.
	if (strategy == "json" || strategy == "xml") && strings.Contains(body, " do |") &&
		!strings.Contains(repo, "{{") {
		regex, ok := "", true
		if m := rubyRegexRe.FindStringSubmatch(body); m != nil {
			regex, ok = liveRegex(m[1]+m[2], m[3])
		}

		if f := blockFollow(body, strategy, repo, regex, parts); ok && f != nil {
			return f
		}
	}

	switch {
	case parts > 1 && strategy == "sparkle" && (!strings.Contains(body, " do |") && !strings.Contains(body, "&:") ||
		strings.Contains(body, "nice_version")):
		// Sparkle's nice version is the short version and the build, as a cask
		// with two parts has it.
		return &follow{from: manifest.FromSparkle, repo: repo, join: "+"}
	case parts > 1 && !slices.Contains([]string{"", "page_match", "header_match"}, strategy):
		// Only a regex gives the parts one by one.
		return nil
	}

	switch strategy {
	case "github_latest", "github_releases":
		if repo == template {
			f, _ := githubFollow(template)

			return f
		}

		return githubRepoFollow(repo, template)
	}

	// A feed of the newest version names no version itself.
	if strings.Contains(repo, "{{") {
		return nil
	}

	switch strategy {
	case "json":
		m := rubyJSONKeyRe.FindAllStringSubmatch(body, -1)
		if len(m) != 1 {
			return nil
		}

		return &follow{from: manifest.FromPage, repo: repo, regex: keyRegex(m[0][1])}
	case "sparkle":
		// Homebrew's &:short_version is what oku reads. Without it, livecheck
		// joins both versions of the feed, which no URL of a cask uses alone.
		if !strings.Contains(body, "&:short_version") {
			return nil
		}

		return &follow{from: manifest.FromSparkle, repo: repo}
	case "electron_builder":
		return &follow{
			from:  manifest.FromPage,
			repo:  repo,
			regex: `(?m)^version:\s*` + versionPattern,
		}
	}

	// Any other strategy takes a regex, and a block would run Ruby, except one
	// that only joins the regex's groups with commas, in order.
	if strings.Contains(body, " do |") && !joinsGroups(body, parts) {
		return nil
	}

	var re string

	if m := rubyRegexRe.FindStringSubmatch(body); m != nil {
		// A part of the download's URL, such as #{arch}, is text of that URL.
		literal := interpolationRe.ReplaceAllStringFunc(m[1]+m[2], func(s string) string {
			if v, ok := vars[s[2:len(s)-1]]; ok {
				return regexp.QuoteMeta(v)
			}

			return s
		})

		var ok bool
		if re, ok = rubyRegex(literal, m[3], parts); !ok {
			return nil
		}
	} else if strategy == "header_match" && parts == 1 {
		// Without a regex, livecheck reads the version from the file name that
		// the URL redirects to.
		re = fileNameRegex(template)
	}

	if re == "" {
		return nil
	}

	join := ""
	if parts > 1 {
		join = "+"
	}

	switch strategy {
	case "", "page_match":
		return &follow{from: manifest.FromPage, repo: repo, regex: re, join: join}
	case "header_match":
		return &follow{from: manifest.FromRedirect, repo: repo, regex: re, join: join}
	}

	return nil
}

// rubyRegex turns a Ruby regex literal into Go syntax with a group for each
// of the version's parts.
func rubyRegex(body, flags string, parts int) (string, bool) {
	if strings.Contains(body, "#{") {
		return "", false
	}

	body = strings.ReplaceAll(strings.ReplaceAll(body, `\/`, "/"), `\h`, `[0-9a-fA-F]`)
	prefix := ""

	for _, f := range flags {
		switch f {
		case 'i':
			prefix += "i"
		case 'm':
			// Ruby's m lets a dot match a newline, which is Go's s.
			prefix += "s"
		default:
			return "", false
		}
	}

	if prefix != "" {
		body = "(?" + prefix + ")" + body
	}

	if parts > 1 {
		compiled, err := regexp.Compile(body)

		return body, err == nil && compiled.NumSubexp() == parts
	}

	return oneGroup(body)
}

// fileNameRegex finds the version in the file name of template, or is "" when
// the file name holds no version.
func fileNameRegex(template string) string {
	name := path.Base(template)
	if !strings.Contains(name, "{{version}}") {
		return ""
	}

	parts := strings.Split(name, "{{version}}")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}

	// Only the first version is the group, so the regex has exactly one.
	return parts[0] + versionPattern + strings.Join(parts[1:], `[0-9A-Za-z._+-]*`)
}

// onlyExecutable returns the one executable outside an app among files whose
// path fits, or "" when none or several do.
func onlyExecutable(files []File, fits func(string) bool) string {
	found := ""

	for _, f := range files {
		if !f.Executable || strings.Contains(f.Path, ".app/") || !fits(f.Path) {
			continue
		}

		if found != "" {
			return ""
		}

		found = f.Path
	}

	return found
}

var rubyJoinRe = regexp.MustCompile(`\.scan\(regex\)\.map \{ \|match\| "([^"]*)" \}`)

// joinsGroups reports whether a livecheck block does nothing but scan the page
// with its regex and join the groups 0 to parts-1 with commas, as in
// page.scan(regex).map { |match| "#{match[0]},#{match[1]}" }.
func joinsGroups(body string, parts int) bool {
	m := rubyJoinRe.FindStringSubmatch(body)
	if m == nil || parts < 2 {
		return false
	}

	// The scan is the only line of the block.
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		if line != "" && line != "end" && !rubyJoinRe.MatchString(line) &&
			!strings.HasPrefix(line, "url ") && !strings.HasPrefix(line, "regex") &&
			!strings.HasPrefix(line, "strategy ") {
			return false
		}
	}

	want := make([]string, parts)
	for i := range want {
		want[i] = fmt.Sprintf("#{match[%d]}", i)
	}

	return m[1] == strings.Join(want, ",")
}
