package infer

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/shape"
)

// scoopBuckets are the buckets that Scoop knows by name, and the repo of each.
// A bare name is looked up in main, then extras. nonportable is left out,
// because its packages run installers.
var scoopBuckets = map[string]string{
	"main":         "ScoopInstaller/Main",
	"extras":       "ScoopInstaller/Extras",
	"versions":     "ScoopInstaller/Versions",
	"nirsoft":      "ScoopInstaller/Nirsoft",
	"sysinternals": "niheaven/scoop-sysinternals",
	"php":          "ScoopInstaller/PHP",
	"nerd-fonts":   "matthewjberger/scoop-nerd-fonts",
	"java":         "ScoopInstaller/Java",
	"games":        "Calinou/scoop-games",
}

var scoopNameRe = regexp.MustCompile(`^([a-z-]+/)?[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// ValidScoop reports whether s is "name" or "bucket/name" of a known bucket.
func ValidScoop(s string) bool {
	bucket, _, found := strings.Cut(s, "/")

	return scoopNameRe.MatchString(s) && (!found || scoopBuckets[bucket] != "")
}

// scoopJSON is a Scoop manifest, or the part of one for an arch.
type scoopJSON struct {
	Version     string               `json:"version"`
	Description string               `json:"description"`
	Homepage    string               `json:"homepage"`
	URL         any                  `json:"url"`
	Hash        any                  `json:"hash"`
	ExtractDir  string               `json:"extract_dir"`
	Bin         any                  `json:"bin"`
	Shortcuts   [][]string           `json:"shortcuts"`
	PreInstall  any                  `json:"pre_install"`
	PostInstall any                  `json:"post_install"`
	EnvSet      any                  `json:"env_set"`
	Persist     any                  `json:"persist"`
	Installer   any                  `json:"installer"`
	InnoSetup   bool                 `json:"innosetup"`
	Depends     any                  `json:"depends"`
	EnvAddPath  any                  `json:"env_add_path"`
	Arch        map[string]scoopJSON `json:"architecture"`
	Checkver    any                  `json:"checkver"`
	Autoupdate  *scoopJSON           `json:"autoupdate"`
}

// FromScoop returns manifest TOML translated from the Scoop manifest name, which
// may start with its bucket.
func (inf *Inferrer) FromScoop(ctx context.Context, name string) (string, error) {
	buckets := []string{"main", "extras"}
	if bucket, rest, found := strings.Cut(name, "/"); found {
		buckets, name = []string{bucket}, rest
	}

	var (
		data []byte
		err  error
		from string
	)

	for _, bucket := range buckets {
		from = bucket
		data, err = inf.Hosts.GitHub("").
			File(ctx, scoopBuckets[bucket], "HEAD", "bucket/"+name+".json")

		if !errors.Is(err, forge.ErrNotFound) {
			break
		}
	}

	if errors.Is(err, forge.ErrNotFound) {
		return "", fmt.Errorf(
			"scoop:%s: no bucket of %s has it",
			name,
			strings.Join(buckets, " or "),
		)
	}

	if err != nil {
		return "", fmt.Errorf("read the Scoop manifest %s: %w", name, err)
	}

	var s scoopJSON
	if err := json.Unmarshal(data, &s); err != nil {
		return "", fmt.Errorf("read the Scoop manifest %s: %w", name, err)
	}

	if err := shape.Check(
		"the Scoop manifest "+from+"/"+name,
		shape.Field{Name: "version", Has: s.Version != ""},
		shape.Field{Name: "url", Has: s.URL != nil || len(s.Arch) > 0},
	); err != nil {
		return "", err
	}

	r, err := s.recipe(from, name)
	if err != nil {
		return "", fmt.Errorf("scoop:%s/%s: %w", from, name, err)
	}

	for i := range r.artifacts {
		if err := inf.pathPrograms(ctx, &r.artifacts[i]); err != nil {
			return "", fmt.Errorf("scoop:%s/%s: %w", from, name, err)
		}
	}

	return r.text()
}

var scoopArchs = []struct{ key, arch string }{
	{"64bit", "amd64"}, {"arm64", "arm64"}, {"32bit", "386"},
}

func (s scoopJSON) recipe(bucket, name string) (recipe, error) {
	switch {
	case s.InnoSetup || installerFile(s.Installer):
		return recipe{}, fmt.Errorf("the manifest runs an installer, so %w", errUntranslatable)
	case unpacks(s.Installer):
		return recipe{}, fmt.Errorf(
			"its installer script unpacks the download, so %w", errUntranslatable,
		)
	}

	// Scripts that only set up the user's environment are left out, and the
	// manifest says which.
	var left []string

	for _, script := range []struct {
		key   string
		value any
	}{
		{"installer", s.Installer},
		{"pre_install", s.PreInstall},
		{"post_install", s.PostInstall},
		{"env_set", s.EnvSet},
		{"persist", s.Persist},
	} {
		if script.value != nil {
			left = append(left, script.key)
		}
	}

	r := recipe{
		deps:        scoopDeps(s.Depends),
		leftOut:     left,
		source:      "the Scoop manifest " + bucket + "/" + name,
		name:        packageName(name),
		description: s.Description,
		homepage:    s.Homepage,
		version:     s.Version,
	}

	type platformOf struct {
		sel   platform.Selector
		part  scoopJSON
		later *scoopJSON
	}

	platforms := []platformOf{{sel: platform.Selector{OS: "windows"}, later: s.Autoupdate}}

	if len(s.Arch) > 0 {
		platforms = nil

		for _, a := range scoopArchs {
			part, ok := s.Arch[a.key]
			if !ok {
				continue
			}

			var later *scoopJSON

			if s.Autoupdate != nil {
				l := *s.Autoupdate
				if at, ok := l.Arch[a.key]; ok {
					l = at.over(l)
				}

				later = &l
			}

			platforms = append(platforms, platformOf{
				sel: platform.Selector{OS: "windows", Arch: a.arch}, part: part, later: later,
			})
		}
	}

	for _, p := range platforms {
		part := p.part.over(s)
		if unpacks(part.PreInstall) {
			// The download is not usable on this arch until a script unpacks it.
			r.dropped = "its download needs a pre_install script to unpack it"

			continue
		}

		url, ok := part.URL.(string)
		if !ok {
			return recipe{}, fmt.Errorf(
				"the manifest has several downloads, so %w",
				errUntranslatable,
			)
		}

		url, _, _ = strings.Cut(url, "#")

		a := recipeArtifact{sel: p.sel, url: url, sha256: scoopHash(part.Hash)}
		if part.ExtractDir != "" {
			a.strip = len(strings.Split(strings.Trim(part.ExtractDir, `\/`), `\`))
		}

		a.pathDirs = scoopPathDirs(s.EnvAddPath)

		if err := a.scoopOutputs(part); err != nil {
			return recipe{}, err
		}

		if p.later != nil {
			a.template = scoopTemplate(p.later.URL, s.Version, url)
		}

		// checkver is the manifest's own rule for new versions. "github" in it
		// follows the releases that the download comes from.
		if a.template != "" {
			a.follow = scoopCheckver(s.Checkver, s.Homepage, a.template)
		}

		r.artifacts = append(r.artifacts, a)
	}

	r.follow = sharedFollow(r.artifacts)

	return r, nil
}

// over returns part with what it leaves out taken from whole.
func (part scoopJSON) over(whole scoopJSON) scoopJSON {
	if part.URL == nil {
		part.URL = whole.URL
	}

	if part.Hash == nil {
		part.Hash = whole.Hash
	}

	if part.ExtractDir == "" {
		part.ExtractDir = whole.ExtractDir
	}

	if part.Bin == nil {
		part.Bin = whole.Bin
	}

	if part.Shortcuts == nil {
		part.Shortcuts = whole.Shortcuts
	}

	if part.PreInstall == nil {
		part.PreInstall = whole.PreInstall
	}

	return part
}

// scoopHash is the sha256 of a Scoop hash, or "" for another kind of digest.
func scoopHash(h any) string {
	s, _ := h.(string)
	s = strings.ToLower(strings.TrimPrefix(s, "sha256:"))

	if len(s) != 64 || strings.Trim(s, "0123456789abcdef") != "" {
		return ""
	}

	return s
}

var scoopVarRe = regexp.MustCompile(`\$[A-Za-z]`)

// scoopVersions are the version variables of Scoop's autoupdate, and the oku
// variables that give them. $cleanVersion also drops dashes, so it is
// {{version_nodots}} only for a version with none.
var scoopVersions = strings.NewReplacer(
	"$cleanVersion", "{{version_nodots}}",
	"$majorVersion", "{{version_major}}",
	"$minorVersion", "{{version_minor}}",
	"$patchVersion", "{{version_patch}}",
	"$underscoreVersion", "{{version_underscores}}",
	"$dashVersion", "{{version_dashes}}",
	"$version", "{{version}}",
)

// scoopTemplate turns the autoupdate URL of a Scoop manifest into an oku one.
// It returns "" when the URL uses a variable oku has none for, or does not give
// url for version.
func scoopTemplate(later any, version, url string) string {
	s, ok := later.(string)
	if !ok || strings.Contains(s, "$cleanVersion") && strings.Contains(version, "-") {
		return ""
	}

	s, _, _ = strings.Cut(s, "#")
	s = scoopVersions.Replace(s)

	if scoopVarRe.MatchString(s) || !strings.Contains(s, "{{version") {
		return ""
	}

	if filled, err := manifest.Expand(s, map[string]string{"version": version}); err != nil || filled != url {
		return ""
	}

	return s
}

// scoopOutputs reads the programs and shortcuts of part.
func (a *recipeArtifact) scoopOutputs(part scoopJSON) error {
	var entries []any

	switch bin := part.Bin.(type) {
	case string:
		entries = []any{bin}
	case []any:
		entries = bin
	}

	for _, entry := range entries {
		switch e := entry.(type) {
		case string:
			a.bins = append(a.bins, recipeBin{path: winPath(e)})
		case []any:
			at, _ := e[0].(string)
			bin := recipeBin{path: winPath(at)}

			if len(e) >= 2 {
				alias, _ := e[1].(string)
				bin.name = alias + path.Ext(bin.path)
			}

			if len(e) >= 3 {
				line, _ := e[2].(string)
				bin.name = strings.TrimSuffix(bin.name, path.Ext(bin.name))

				// Arguments that name a folder of Scoop's make no sense in the
				// store, which no program writes to. The program runs without them.
				var ok bool
				if bin.args, ok = scoopArgs(line); !ok {
					bin.name += path.Ext(bin.path)
					a.leftArgs = append(a.leftArgs, bin.name)
				}
			}

			a.bins = append(a.bins, bin)
		}
	}

	// A shortcut runs a program, which the launcher reaches through bin.
	for _, shortcut := range part.Shortcuts {
		if len(shortcut) < 2 || len(shortcut) > 2 && shortcut[2] != "" ||
			!strings.EqualFold(path.Ext(shortcut[0]), ".exe") {
			continue
		}

		program := winPath(shortcut[0])
		if !slices.ContainsFunc(
			a.bins,
			func(b recipeBin) bool { return b.name == "" && b.path == program },
		) {
			a.bins = append(a.bins, recipeBin{path: program})
		}

		a.launchers = append(
			a.launchers,
			[2]string{path.Base(winPath(shortcut[1])), path.Base(program)},
		)
	}

	if len(a.bins) == 0 && len(a.pathDirs) == 0 {
		return errors.New("the manifest installs no program")
	}

	return nil
}

func winPath(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

var jsonPathKeyRe = regexp.MustCompile(`^\$(?:\[0\])?\.([A-Za-z0-9_]+)$`)

// scoopCheckver translates the checkver of a Scoop manifest whose download is
// template. It returns nil for a script, a URL with variables, or a JSON path
// deeper than one key.
func scoopCheckver(checkver any, homepage, template string) *follow {
	var c map[string]any

	switch v := checkver.(type) {
	case string:
		if v == "github" {
			return githubRepoFollow(homepage, template)
		}

		c = map[string]any{"regex": v}
	case map[string]any:
		c = v
	default:
		return nil
	}

	str := func(key string) string {
		s, _ := c[key].(string)

		return s
	}

	if _, ok := c["script"]; ok {
		return nil
	}

	re := cmp.Or(str("regex"), str("re"))

	if gh := str("github"); gh != "" && re == "" && str("jsonpath") == "" {
		return githubRepoFollow(gh, template)
	}

	url := cmp.Or(str("url"), homepage)
	if url == "" || scoopVarRe.MatchString(url) {
		return nil
	}

	// A JSON path picks one value and the regex reads it. oku reads the first
	// match in the whole answer, which is that value when the path names the
	// first of a list, as feeds that list the newest first do.
	if m := jsonPathKeyRe.FindStringSubmatch(str("jsonpath")); m != nil && re == "" {
		return &follow{from: manifest.FromPage, repo: url, regex: keyRegex(m[1])}
	}

	if re == "" {
		return nil
	}

	if re, ok := oneGroup(re); ok {
		return &follow{from: manifest.FromPage, repo: url, regex: re}
	}

	return nil
}

// githubRepoFollow follows the releases of a github.com repo URL. A download
// from the releases of that repo keeps the tag prefix of its URL.
func githubRepoFollow(url, template string) *follow {
	f, ok := githubFollow(template)
	if ok && strings.EqualFold("https://github.com/"+f.repo, strings.TrimSuffix(url, "/")) {
		return f
	}

	f, _ = githubFollow(strings.TrimSuffix(url, "/") + "/releases/download/{{version}}/")

	return f
}

// installerFile reports whether a Scoop installer runs a program.
func installerFile(installer any) bool {
	m, _ := installer.(map[string]any)

	return m["file"] != nil
}

// unpackRe finds what a Scoop script does to unpack a download, with the
// helpers Scoop has for it or by running a program.
var unpackRe = regexp.MustCompile(`(?i)\bExpand-\w+|Invoke-ExternalCommand|Start-Process|msiexec`)

// unpacks reports whether a Scoop script, or the script of an installer,
// unpacks or runs something, which leaves the download unusable without it.
func unpacks(script any) bool {
	if m, ok := script.(map[string]any); ok {
		script = m["script"]
	}

	data, _ := json.Marshal(script)

	return unpackRe.Match(data)
}

// scoopPathDirs are the directories that env_add_path puts on PATH, relative to
// the unpacked download.
func scoopPathDirs(value any) []string {
	var dirs []string

	switch v := value.(type) {
	case string:
		dirs = []string{v}
	case []any:
		for _, d := range v {
			if s, ok := d.(string); ok {
				dirs = append(dirs, s)
			}
		}
	}

	for i, d := range dirs {
		dirs[i] = strings.TrimPrefix(strings.Trim(winPath(strings.TrimPrefix(d, "$dir")), "/"), ".")
	}

	return dirs
}

// pathPrograms makes every program in the directories a puts on PATH a
// program of a. oku links programs one by one, so it opens the download to
// find them.
func (inf *Inferrer) pathPrograms(ctx context.Context, a *recipeArtifact) error {
	if len(a.pathDirs) == 0 {
		return nil
	}

	files, err := inf.Inspect(ctx, a.url, forge.Auth{})
	if err != nil {
		return fmt.Errorf("open %s to find its programs: %w", a.url, err)
	}

	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		if len(parts) <= a.strip {
			continue
		}

		at := strings.Join(parts[a.strip:], "/")
		dir := path.Dir(at)

		if dir == "." {
			dir = ""
		}

		if !slices.Contains(a.pathDirs, dir) || !strings.EqualFold(path.Ext(at), ".exe") ||
			slices.ContainsFunc(a.bins, func(b recipeBin) bool { return b.path == at }) {
			continue
		}

		a.bins = append(a.bins, recipeBin{path: at})
	}

	if len(a.bins) == 0 {
		return fmt.Errorf("%s holds no program in %s", a.url, strings.Join(a.pathDirs, ", "))
	}

	return nil
}

// scoopHelpers are the packages Scoop depends on to unpack a download, which
// oku does itself.
var scoopHelpers = []string{"7zip", "lessmsi", "innounp", "dark"}

// scoopDeps are the refs of the Scoop packages that depends names.
func scoopDeps(depends any) []string {
	var names, refs []string

	switch d := depends.(type) {
	case string:
		names = []string{d}
	case []any:
		for _, n := range d {
			if s, ok := n.(string); ok {
				names = append(names, s)
			}
		}
	}

	for _, n := range names {
		if !slices.Contains(scoopHelpers, strings.TrimPrefix(n, "main/")) {
			refs = append(refs, "scoop:"+n)
		}
	}

	return refs
}

// scoopArgs splits the arguments Scoop passes to a program. Arguments with a
// variable of Scoop's, such as $dir or $persist_dir, cannot be translated.
func scoopArgs(line string) ([]string, bool) {
	if strings.Contains(line, "$") {
		return nil, false
	}

	var (
		args   []string
		cur    strings.Builder
		quoted bool
		inArg  bool
	)

	for _, r := range line {
		switch {
		case r == '"':
			quoted, inArg = !quoted, true
		case r == ' ' && !quoted:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
			}

			inArg = false
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}

	if inArg {
		args = append(args, cur.String())
	}

	return args, !quoted
}
