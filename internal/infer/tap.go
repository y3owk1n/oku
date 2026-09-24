package infer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/manifest"
)

// caskTapRe is a cask of a tap of its own: owner/tap/token, from the GitHub
// repo owner/homebrew-tap.
var caskTapRe = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+/[a-z0-9][a-z0-9@._+-]*$`)

// FromTap returns manifest TOML translated from a cask of a Homebrew tap. The
// Homebrew API serves the official casks alone, so oku reads the Ruby file of
// the cask from the tap's repo, as far as it declares values, and never runs it.
func (inf *Inferrer) FromTap(ctx context.Context, location string) (string, error) {
	parts := strings.Split(location, "/")
	repo, token := parts[0]+"/homebrew-"+strings.TrimPrefix(parts[1], "homebrew-"), parts[2]

	var (
		source []byte
		err    error
	)

	// A big tap keeps its casks in a folder per first letter, as homebrew-cask does.
	for _, path := range []string{"Casks/" + token + ".rb", "Casks/" + token[:1] + "/" + token + ".rb"} {
		source, err = inf.Hosts.GitHub("").File(ctx, repo, "HEAD", path)
		if !errors.Is(err, forge.ErrNotFound) {
			break
		}
	}

	if errors.Is(err, forge.ErrNotFound) {
		return "", fmt.Errorf("cask:%s: the tap %s has no cask %s", location, repo, token)
	}

	if err != nil {
		return "", fmt.Errorf("read the cask %s: %w", location, err)
	}

	c, err := readCaskRuby(string(source), token)
	if err != nil {
		return "", fmt.Errorf("cask:%s: %w", location, err)
	}

	if c.Version == "latest" {
		return "", fmt.Errorf(
			"cask:%s: the cask has no version, so oku cannot tell one download from the next",
			location,
		)
	}

	r, err := c.recipe(string(source))
	if err != nil {
		return "", fmt.Errorf("cask:%s: %w", location, err)
	}

	r.source = "the Homebrew cask " + location

	for i := range r.artifacts {
		if err := inf.openCask(ctx, location, &r.artifacts[i]); err != nil {
			return "", fmt.Errorf("cask:%s: %w", location, err)
		}
	}

	return r.text()
}

// caskStanza is one statement of a cask: a name and its arguments, the first
// of which is a string or a symbol, and options such as target: "x".
type caskStanza struct {
	name    string
	value   string
	symbol  bool
	options map[string]string
}

// caskArtifactStanzas are the stanzas that place what a cask installs, in the
// form the Homebrew API gives them.
var caskArtifactStanzas = map[string]bool{
	"app": true, "binary": true, "font": true, "manpage": true, "pkg": true, "suite": true,
	"artifact": true, "installer": true, "app_image": true,
	"preflight": true, "postflight": true, "preflight_steps": true, "postflight_steps": true,
}

var (
	caskStanzaRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*)\s+(.*)$`)
	caskOptionRe = regexp.MustCompile(`([a-z_]+):\s*(?:"([^"]*)"|'([^']*)'|:([a-z0-9_]+))`)
	caskValueRe  = regexp.MustCompile(`^(?:"([^"]*)"|'([^']*)'|:([a-z0-9_]+))`)
	// caskAssignRe gives a local variable a string, which a stanza may name.
	caskAssignRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*)\s*=\s*(?:"([^"]*)"|'([^']*)')$`)
	caskIdentRe  = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	// caskHookRe opens a block that runs Ruby before or after the install.
	caskHookRe = regexp.MustCompile(`^((?:pre|post)flight(?:_steps)?) do\b`)
)

// readCaskRuby reads the declared values of the cask in src into the form of
// the Homebrew API. It takes version, sha256, url, desc, homepage, arch,
// depends_on arch:, the stanzas that place files, and on_arm and on_intel
// blocks of those. A value that Ruby computes fails, since oku reads the file
// and never runs it.
func readCaskRuby(src, token string) (caskJSON, error) {
	statements, err := caskStatements(src)
	if err != nil {
		return caskJSON{}, err
	}

	var (
		top, onArm, onIntel []caskStanza
		archs               = map[string]string{}
	)

	for _, st := range statements {
		switch {
		case strings.HasPrefix(st.name, "on_arm/"):
			st.name = strings.TrimPrefix(st.name, "on_arm/")
			onArm = append(onArm, st)
		case strings.HasPrefix(st.name, "on_intel/"):
			st.name = strings.TrimPrefix(st.name, "on_intel/")
			onIntel = append(onIntel, st)
		case st.name == "arch":
			archs = st.options
		default:
			top = append(top, st)
		}
	}

	c := caskJSON{Token: token}

	arm, err := caskBuild(append(append([]caskStanza{}, top...), onArm...), "arm", archs)
	if err != nil {
		return caskJSON{}, err
	}

	intel, err := caskBuild(append(append([]caskStanza{}, top...), onIntel...), "intel", archs)
	if err != nil {
		return caskJSON{}, err
	}

	c.Desc, c.Homepage, c.Version, c.URL, c.SHA256, c.Artifacts, c.DependsOn =
		arm.Desc, arm.Homepage, arm.Version, arm.URL, arm.SHA256, arm.Artifacts, arm.DependsOn

	if c.URL == "" || c.Version == "" {
		return caskJSON{}, errors.New("the cask declares no url or no version that oku can read")
	}

	// The Intel build is a variation of the newest macOS, as in the API.
	c.Variations = map[string]caskJSON{"intel": {
		URL: intel.URL, SHA256: intel.SHA256, Version: intel.Version, Artifacts: intel.Artifacts,
	}}

	return c, nil
}

// caskBuild reads the stanzas of one arch, "arm" or "intel", into the form of
// the API. archs holds the values of the arch stanza.
func caskBuild(stanzas []caskStanza, arch string, archs map[string]string) (caskJSON, error) {
	var c caskJSON

	// The version comes first, since the other values may name it.
	for _, st := range stanzas {
		if st.name == "version" {
			c.Version = st.value
		}
	}

	expand := func(s string) (string, error) {
		var failed error

		out := interpolationRe.ReplaceAllStringFunc(s, func(m string) string {
			expr := m[2 : len(m)-1]

			// The folders of Homebrew, in the form of the API.
			switch expr {
			case "arch":
				return archs[arch]
			case "HOMEBREW_PREFIX":
				return "$HOMEBREW_PREFIX"
			case "appdir":
				return "$APPDIR"
			}

			oku, ok := rubyVersions[expr]
			if !ok {
				failed = fmt.Errorf("the cask computes #{%s}, which oku does not read", expr)

				return ""
			}

			value, err := manifest.Expand(oku, map[string]string{"version": strings.ReplaceAll(c.Version, ",", "+")})
			if err != nil {
				failed = err
			}

			return value
		})

		return out, failed
	}

	for _, st := range stanzas {
		var err error

		switch st.name {
		case "desc":
			c.Desc = st.value
		case "homepage":
			c.Homepage, err = expand(st.value)
		case "url":
			c.URL, err = expand(st.value)
		case "sha256":
			switch {
			case st.symbol:
				c.SHA256 = st.value
			case st.value != "":
				c.SHA256 = st.value
			default:
				c.SHA256 = st.options[arch]
			}
		case "depends_on":
			if want := st.options["arch"]; want != "" {
				c.DependsOn.Arch = []caskArch{{Type: map[string]string{"arm64": "arm", "x86_64": "intel"}[want]}}
			}
		default:
			if !caskArtifactStanzas[st.name] {
				continue
			}

			value, err := expand(st.value)
			if err != nil {
				return caskJSON{}, err
			}

			args := []any{value}

			if target := st.options["target"]; target != "" {
				if target, err = expand(target); err != nil {
					return caskJSON{}, err
				}

				args = append(args, map[string]any{"target": target})
			}

			c.Artifacts = append(c.Artifacts, map[string]any{st.name: args})
		}

		if err != nil {
			return caskJSON{}, err
		}
	}

	return c, nil
}

// caskStatements reads the top-level statements of the cask block in src, and
// those of its on_arm and on_intel blocks with the block's name in front. It
// skips other blocks, such as livecheck, zap and uninstall, whose contents the
// translation reads from src itself or does not need.
func caskStatements(src string) ([]caskStanza, error) {
	var (
		out   []caskStanza
		depth int
		block string
		vars  = map[string]string{}
	)

	lines := strings.Split(src, "\n")

	for i := 0; i < len(lines); i++ {
		line := stripComment(lines[i])

		// A statement goes on while a line ends with a comma or opens a bracket.
		for (strings.HasSuffix(line, ",") || strings.Count(line, "[") > strings.Count(line, "]")) &&
			i+1 < len(lines) {
			i++
			line += " " + stripComment(lines[i])
		}

		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "cask ") && strings.HasSuffix(line, " do"):
			depth = 1

			continue
		case line == "end":
			depth--
			if depth == 1 {
				block = ""
			}

			continue
		case strings.HasSuffix(line, " do") || strings.Contains(line, " do |"):
			// A hook runs Ruby, so the manifest says oku left it out.
			if m := caskHookRe.FindStringSubmatch(line); m != nil && depth == 1 {
				out = append(out, caskStanza{name: m[1], options: map[string]string{}})
			}

			depth++
			if depth == 2 && (line == "on_arm do" || line == "on_intel do") {
				block = strings.TrimSuffix(line, " do")
			}

			continue
		case strings.HasPrefix(line, "if ") || strings.HasPrefix(line, "unless ") || strings.HasPrefix(line, "case "):
			if depth == 1 || block != "" {
				return nil, errors.New("the cask chooses its values with Ruby logic, which oku does not run")
			}

			depth++

			continue
		}

		if depth != 1 && block == "" {
			continue
		}

		if a := caskAssignRe.FindStringSubmatch(line); a != nil {
			vars[a[1]] = a[2] + a[3]

			continue
		}

		m := caskStanzaRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		// A stanza may name a variable that holds its string.
		if value, ok := vars[strings.TrimSpace(m[2])]; ok && caskIdentRe.MatchString(strings.TrimSpace(m[2])) {
			m[2] = `"` + value + `"`
		}

		st := caskStanza{name: m[1], options: map[string]string{}}
		if v := caskValueRe.FindStringSubmatch(m[2]); v != nil {
			st.value, st.symbol = v[1]+v[2], v[3] != ""
			if st.symbol {
				st.value = v[3]
			}
		}

		for _, o := range caskOptionRe.FindAllStringSubmatch(m[2], -1) {
			st.options[o[1]] = o[2] + o[3] + o[4]
		}

		if block != "" {
			st.name = block + "/" + st.name
		}

		out = append(out, st)
	}

	return out, nil
}

// stripComment returns line without its comment and the spaces around it. A #
// inside a string, as in "#{version}", is no comment.
func stripComment(line string) string {
	var quote byte

	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0 && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			return strings.TrimSpace(line[:i])
		}
	}

	return strings.TrimSpace(line)
}
