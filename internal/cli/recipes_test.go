package cli_test

import (
	"cmp"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// caskHead is the commit of the fake homebrew-cask repo.
const caskHead = "0123abcd"

// recipeServer is a fake Homebrew API, raw GitHub and vendor. The vendor serves
// the program tool at each of versions for every platform, and says in
// /latest.json that the newest is latest. casks maps a token to its JSON and
// Ruby source, scoop maps a bucket path such as "ScoopInstaller/Main/tool" to
// its JSON. Both may use SERVER for the server's URL.
type recipeServer struct {
	versions []string
	latest   string
	casks    map[string][2]string
	scoop    map[string]string
	// appcast is the text of /appcast.xml.
	appcast string
	// files replace the one program in each download.
	files map[string]string
	// aqua is the aqua registry's entry for owner/tool. The server's GitHub API
	// lists versions as releases of owner/tool, with a "v" in their tags.
	aqua string
	// aquaTag is the aqua registry's newest release, v4.300.0 when empty.
	aquaTag string
	// winget maps a version of the winget package Owner.Tool to the text of its
	// installer manifest.
	winget map[string]string
}

// start serves s for m and returns the server's URL.
func (s recipeServer) start(t *testing.T, m *machine) string {
	t.Helper()

	files := map[string]string{}
	sums := map[string]string{}

	for _, v := range s.versions {
		content := s.files
		if content == nil {
			content = map[string]string{"tool": "#!/bin/sh\necho tool " + v + "\n"}
		}

		archive, sum := m.archive(t, "tool-"+v, content)
		files[v], sums[v] = archive, sum
	}

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fill := func(s string) string {
			s = strings.ReplaceAll(s, "SERVER", server.URL)
			for v, sum := range sums {
				s = strings.ReplaceAll(s, "SUM"+v, sum)
			}

			return s
		}

		parts := strings.Split(r.URL.Path, "/")

		switch {
		case r.URL.Path == "/api/repos/aquaproj/aqua-registry/releases/latest":
			_, _ = fmt.Fprintf(w, `{"tag_name": %q, "assets": []}`, cmp.Or(s.aquaTag, "v4.300.0"))
		case r.URL.Path == "/raw/aquaproj/aqua-registry/v4.300.0/pkgs/owner/tool/registry.yaml" && s.aqua != "":
			_, _ = fmt.Fprint(w, fill(s.aqua))
		case r.URL.Path == "/api/repos/owner/tool/releases":
			var releases []string
			for _, v := range s.versions {
				releases = append(releases, fmt.Sprintf(`{"tag_name": "v%s", "assets": []}`, v))
			}

			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(releases, ","))
		case r.URL.Path == "/api/repos/microsoft/winget-pkgs/contents/manifests/o/Owner/Tool" && s.winget != nil:
			var entries []string
			for v := range s.winget {
				entries = append(entries, fmt.Sprintf(`{"name": %q, "type": "dir"}`, v))
			}

			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(entries, ","))
		case strings.HasPrefix(r.URL.Path, "/raw/microsoft/winget-pkgs/HEAD/manifests/o/Owner/Tool/"):
			version, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/raw/microsoft/winget-pkgs/HEAD/manifests/o/Owner/Tool/"), "/")

			switch installer, ok := s.winget[version]; {
			case !ok:
				http.NotFound(w, r)
			case file == "Owner.Tool.installer.yaml":
				_, _ = fmt.Fprint(w, fill(installer))
			case file == "Owner.Tool.yaml":
				_, _ = fmt.Fprint(w, "PackageIdentifier: Owner.Tool\nDefaultLocale: en-US\n")
			case file == "Owner.Tool.locale.en-US.yaml":
				_, _ = fmt.Fprint(w, "ShortDescription: A tool\nPackageUrl: https://tool.example\n")
			default:
				http.NotFound(w, r)
			}
		case r.URL.Path == "/appcast.xml":
			_, _ = fmt.Fprint(w, s.appcast)
		case r.URL.Path == "/latest.json":
			_, _ = fmt.Fprintf(w, `{"name": "tool", "version": %q}`, s.latest)
		case len(parts) == 5 && parts[1] == "dl":
			// /dl/<version>/<platform>/tool.tar.gz
			if file, ok := files[parts[2]]; ok {
				http.ServeFile(w, r, file)

				return
			}

			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/cask/"):
			c, ok := s.casks[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/cask/"), ".json")]
			if !ok {
				http.NotFound(w, r)

				return
			}

			_, _ = fmt.Fprint(w, fill(c[0]))
		case strings.HasPrefix(r.URL.Path, "/raw/Homebrew/homebrew-cask/"+caskHead+"/Casks/"):
			token := strings.TrimSuffix(parts[len(parts)-1], ".rb")
			if c, ok := s.casks[token]; ok {
				_, _ = fmt.Fprint(w, fill(c[1]))

				return
			}

			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/raw/"):
			// /raw/<owner>/<repo>/HEAD/bucket/<name>.json
			key := parts[2] + "/" + parts[3] + "/" + strings.TrimSuffix(parts[len(parts)-1], ".json")
			if body, ok := s.scoop[key]; ok {
				_, _ = fmt.Fprint(w, fill(body))

				return
			}

			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.CaskAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"
	m.opts.GitHubAPI = server.URL + "/api"

	return server.URL
}

// caskJSON is the API's JSON of the cask tool at 1.2.0, with a build for
// every platform oku tests on and a binary in each.
const caskJSON = `{
  "token": "tool", "desc": "A tool", "homepage": "https://tool.example",
  "version": "1.2.0",
  "url": "SERVER/dl/1.2.0/mac-arm64/tool.tar.gz", "sha256": "SUM1.2.0",
  "artifacts": [{"binary": ["tool"]}, {"uninstall": [{"quit": "tool"}]}, {"zap": [{"trash": ["~/.tool"]}]}],
  "depends_on": {},
  "variations": {
    "sequoia": {"url": "SERVER/dl/1.2.0/mac-x64/tool.tar.gz"},
    "x86_64_linux": {"url": "SERVER/dl/1.2.0/linux-x64/tool.tar.gz", "artifacts": [{"binary": ["tool"]}]},
    "arm64_linux": {"url": "SERVER/dl/1.2.0/linux-arm64/tool.tar.gz", "artifacts": [{"binary": ["tool"]}]}
  },
  "ruby_source_path": "Casks/t/tool.rb", "tap_git_head": "` + caskHead + `"
}`

// caskRuby is the Ruby source of caskJSON with a livecheck of the vendor's
// JSON feed.
const caskRuby = `cask "tool" do
  arch arm: "arm64", intel: "x64"
  os macos: "mac", linux: "linux"

  version "1.2.0"

  url "SERVER/dl/#{version}/#{os}-#{arch}/tool.tar.gz"
  name "Tool"

  livecheck do
    url "SERVER/latest.json"
    strategy :json do |json|
      json["version"]
    end
  end

  binary "tool"
end
`

func TestB291AddTranslatesACaskAndFollowsTheVendorsVersions(t *testing.T) {
	m := newMachine(t)
	recipeServer{
		versions: []string{"1.2.0", "1.3.0"},
		latest:   "1.3.0",
		casks:    map[string][2]string{"tool": {caskJSON, caskRuby}},
	}.start(t, &m)

	out, err := m.run(t, "", "add", "cask:tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// The cask is at 1.2.0, and the vendor's feed says 1.3.0.
	if got := m.toolOutput(t); got != "tool 1.3.0" {
		t.Fatalf("tool printed %q, want the vendor's newest 1.3.0", got)
	}

	if !strings.Contains(out, "translated it into a manifest") {
		t.Fatalf("add did not say that it translated the cask:\n%s", out)
	}

	lock, err := os.ReadFile(m.config + "/oku.lock")
	must(t, err)

	for _, want := range []string{"cask:tool", "inferred = true", "latest.json"} {
		if !strings.Contains(string(lock), want) {
			t.Fatalf("oku.lock lacks %q:\n%s", want, lock)
		}
	}
}

func TestB293ATranslationFollowsTheRecipesRuleForEachPlatform(t *testing.T) {
	m := newMachine(t)
	// The feed's URL names the platform, so each artifact gets its own.
	ruby := strings.Replace(caskRuby, `url "SERVER/latest.json"`, `url "SERVER/latest.json?p=#{arch}"`, 1)
	recipeServer{casks: map[string][2]string{"tool": {caskJSON, ruby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, want := range []string{
		`match = { os = "darwin", arch = "arm64" }`,
		`match = { os = "darwin", arch = "amd64" }`,
		`match = { os = "linux", arch = "amd64" }`,
		`/dl/{{version}}/mac-x64/tool.tar.gz"`,
		`/dl/{{version}}/linux-arm64/tool.tar.gz"`,
		`version = { from = "page", repo = "http://127.0.0.1`,
		`latest.json?p=x64"`,
		`bin = ["tool"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "[version]") || strings.Contains(out, "sha256") {
		t.Fatalf("a manifest that follows versions per platform pins one:\n%s", out)
	}
}

func TestB293ARecipeWithNoRuleFollowsTheGitHubReleasesItDownloadsFrom(t *testing.T) {
	m := newMachine(t)
	json := strings.ReplaceAll(caskJSON, "SERVER/dl/1.2.0/", "https://github.com/owner/tool/releases/download/v1.2.0/")
	ruby := `cask "tool" do
  version "1.2.0"
  url "https://github.com/owner/tool/releases/download/v#{version}/tool-#{os}-#{arch}.tar.gz"
  binary "tool"
end
`
	json = strings.ReplaceAll(json, "/v1.2.0/mac-arm64/tool.tar.gz", "/v1.2.0/tool-mac-arm64.tar.gz")
	json = strings.ReplaceAll(json, "/v1.2.0/mac-x64/tool.tar.gz", "/v1.2.0/tool-mac-x64.tar.gz")
	json = strings.ReplaceAll(json, "/v1.2.0/linux-x64/tool.tar.gz", "/v1.2.0/tool-linux-x64.tar.gz")
	json = strings.ReplaceAll(json, "/v1.2.0/linux-arm64/tool.tar.gz", "/v1.2.0/tool-linux-arm64.tar.gz")
	recipeServer{casks: map[string][2]string{"tool": {json, ruby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, want := range []string{
		"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\n",
		`url = "https://github.com/owner/tool/releases/download/v{{version}}/tool-mac-arm64.tar.gz"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}
}

func TestB294ARecipeOkuCannotFollowPinsItsVersionAndDigests(t *testing.T) {
	m := newMachine(t)
	// A strategy oku has no source for.
	ruby := strings.Replace(caskRuby, "strategy :json do |json|\n      json[\"version\"]\n    end",
		"strategy :sparkle", 1)
	recipeServer{
		versions: []string{"1.2.0", "1.3.0"},
		latest:   "1.3.0",
		casks:    map[string][2]string{"tool": {caskJSON, ruby}},
	}.start(t, &m)

	out, err := m.run(t, "", "add", "cask:tool", "--yes", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "tool 1.2.0" {
		t.Fatalf("tool printed %q, want the cask's own 1.2.0", got)
	}

	for _, want := range []string{
		"It pins 1.2.0 because it names no source of new versions",
		"[version]\nvalue = \"1.2.0\"",
		`/dl/1.2.0/mac-x64/tool.tar.gz"`,
		"sha256 = ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	// The digest came from the cask, so oku trusted nothing on first use.
	if strings.Contains(out, "trusted this download") {
		t.Fatalf("oku trusted a download that the cask names a digest for:\n%s", out)
	}
}

func TestB294ATemplateThatDoesNotGiveTheRecipesURLIsNotUsed(t *testing.T) {
	m := newMachine(t)
	// The template names another file than the cask downloads.
	ruby := strings.Replace(caskRuby, "/tool.tar.gz", "/tool.tgz", 1)
	recipeServer{casks: map[string][2]string{"tool": {caskJSON, ruby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, "It pins 1.2.0 because its download URL does not follow the version") ||
		strings.Contains(out, "{{version}}") {
		t.Fatalf("a template that gives another URL was used:\n%s", out)
	}
}

func TestB295ARecipeWhoseScriptMakesItsFilesIsRefused(t *testing.T) {
	for _, tc := range []struct{ ref, cask, scoop, want string }{
		{
			ref: "cask:tool",
			cask: strings.Replace(caskJSON, `{"binary": ["tool"]}, {"uninstall"`,
				`{"binary": ["tool"]}, {"installer": [{"script": {"executable": "install.sh"}}]}, {"uninstall"`, 1),
			want: "the cask runs an installer that makes its files",
		},
		{
			ref:  "cask:tool",
			cask: strings.Replace(caskJSON, `{"quit": "tool"}`, `{"kext": "com.tool.driver"}`, 1),
			want: "the cask installs a kernel extension",
		},
		{
			ref:   "scoop:tool",
			scoop: `{"version": "1.2.0", "url": "SERVER/dl/1.2.0/win/tool.exe", "bin": "tool.exe", "installer": {"file": "tool.exe", "args": "/S"}}`,
			want:  "the manifest runs an installer",
		},
		{
			ref:   "scoop:tool",
			scoop: `{"version": "1.2.0", "url": "SERVER/dl/1.2.0/win/tool.zip", "bin": "tool.exe", "pre_install": "Expand-7zipArchive x"}`,
			want:  "its download needs a pre_install script to unpack it",
		},
	} {
		m := newMachine(t)
		s := recipeServer{scoop: map[string]string{"ScoopInstaller/Main/tool": tc.scoop}}

		if tc.cask != "" {
			s.casks = map[string][2]string{"tool": {tc.cask, caskRuby}}
		}

		s.start(t, &m)

		_, err := m.run(t, "", "add", tc.ref, "--yes")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want a refusal with %q, got %v", tc.ref, tc.want, err)
		}

		if exists(m.profile("bin", "tool")) {
			t.Fatalf("%s: a recipe that runs an installer was installed", tc.ref)
		}
	}
}

func TestB295ScriptsThatOnlySetUpTheAppAreLeftOutAndNamed(t *testing.T) {
	m := newMachine(t)
	cask := strings.Replace(caskJSON, `{"binary": ["tool"]}, {"uninstall"`,
		`{"binary": ["tool"]}, {"postflight": null}, {"postflight_steps": [{"steps": [{"type": "run"}]}]}, {"uninstall"`, 1)
	recipeServer{
		versions: []string{"1.3.0"},
		latest:   "1.3.0",
		casks:    map[string][2]string{"tool": {cask, caskRuby}},
	}.start(t, &m)

	out, err := m.run(t, "", "add", "cask:tool", "--yes", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "tool 1.3.0" {
		t.Fatalf("tool printed %q, want 1.3.0", got)
	}

	if !strings.Contains(out, "oku runs no script of a recipe, so it left out postflight, postflight_steps") {
		t.Fatalf("the manifest does not name the scripts it left out:\n%s", out)
	}
}

func TestB297ASparkleFeedGivesItsNewestMacOSVersionOffABetaChannel(t *testing.T) {
	m := newMachine(t)
	ruby := strings.Replace(caskRuby, "strategy :json do |json|\n      json[\"version\"]\n    end",
		"strategy :sparkle, &:short_version", 1)
	ruby = strings.Replace(ruby, "SERVER/latest.json", "SERVER/appcast.xml", 1)
	server := recipeServer{
		versions: []string{"1.2.0", "1.3.0", "1.4.0"},
		casks:    map[string][2]string{"tool": {caskJSON, ruby}},
	}
	server.appcast = `<rss><channel>
<item><title>1.2.0</title><enclosure url="x" sparkle:shortVersionString="1.2.0" sparkle:version="120"/></item>
<item><title>1.4.0 beta</title><sparkle:channel>beta</sparkle:channel><sparkle:shortVersionString>1.4.0</sparkle:shortVersionString></item>
<item><title>1.3.0</title><sparkle:channel>stable</sparkle:channel><sparkle:version>130</sparkle:version><sparkle:shortVersionString>1.3.0</sparkle:shortVersionString></item>
<item><title>Build 5</title><sparkle:version>5</sparkle:version><sparkle:shortVersionString>663205b5 (2024-12-20)</sparkle:shortVersionString></item>
<item><title>1.4.0 for Windows</title><enclosure url="y" sparkle:os="windows" sparkle:shortVersionString="1.4.0"/></item>
</channel></rss>`
	server.start(t, &m)

	out, err := m.run(t, "", "add", "cask:tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// 1.3.0 is the newest macOS release off the beta channel. Its channel,
	// stable, counts as none, as OrbStack's feed has it. oku ranks the items by
	// build, so an old one whose short version is a commit, as Ghostty's feed
	// has, does not win.
	if got := m.toolOutput(t); got != "tool 1.3.0" {
		t.Fatalf("tool printed %q, want 1.3.0", got)
	}
}

func TestB299TheAtOfACaskNameIsPartOfTheName(t *testing.T) {
	m := newMachine(t)
	json := strings.Replace(caskJSON, `"token": "tool"`, `"token": "tool@2"`, 1)
	json = strings.Replace(json, "Casks/t/tool.rb", "Casks/t/tool@2.rb", 1)
	recipeServer{casks: map[string][2]string{"tool@2": {json, caskRuby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool@2", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, `name = "tool-2"`) {
		t.Fatalf("the manifest is not the cask tool@2:\n%s", out)
	}
}

// scoopJSON is a Scoop manifest of tool for two arches, whose checkver reads
// a key of the vendor's JSON feed.
const scoopJSON = `{
  "version": "1.2.0",
  "description": "A tool",
  "homepage": "https://tool.example",
  "architecture": {
    "64bit": {"url": "SERVER/dl/1.2.0/win-x64/tool.zip#/dl.zip", "hash": "SUM1.2.0", "extract_dir": "tool-1.2.0"},
    "arm64": {"url": "SERVER/dl/1.2.0/win-arm64/tool.zip", "hash": "sha256:SUM1.2.0", "extract_dir": "tool-1.2.0"}
  },
  "bin": [["bin\\tool.exe", "tl"], ["bin\\tool.exe", "tool-quiet", "--quiet \"a b\""], ["bin\\tool.exe", "tool-data", "--data $persist_dir"]],
  "depends": ["7zip", "extras/helper"],
  "pre_install": "if (!(Test-Path $persist_dir)) { New-Item $persist_dir }",
  "shortcuts": [["gui\\ToolGui.exe", "Tools\\Tool"]],
  "post_install": "Remove-Item x",
  "checkver": {"url": "SERVER/latest.json", "jsonpath": "$.version"},
  "autoupdate": {
    "architecture": {
      "64bit": {"url": "SERVER/dl/$version/win-x64/tool.zip#/dl.zip"},
      "arm64": {"url": "SERVER/dl/$version/win-arm64/tool.zip"}
    }
  }
}`

func TestB292AScoopManifestTranslatesItsArchesProgramsAndShortcuts(t *testing.T) {
	m := newMachine(t)
	// A bare name falls back to the extras bucket.
	recipeServer{scoop: map[string]string{"ScoopInstaller/Extras/tool": scoopJSON}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "scoop:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, want := range []string{
		"# Translated from the Scoop manifest extras/tool.",
		"[version]\nfrom = \"page\"\n",
		`latest.json"`,
		`match = { os = "windows", arch = "amd64" }`,
		`match = { os = "windows", arch = "arm64" }`,
		`/dl/{{version}}/win-x64/tool.zip"`,
		"strip = 1",
		`{ name = "tl.exe", path = "bin/tool.exe" }`,
		`{ name = "tool-quiet", run = "{{pkg}}/bin/tool.exe", args = ["--quiet", "a b"] }`,
		`{ name = "tool-data.exe", path = "bin/tool.exe" }`,
		"# The arguments of tool-data.exe name folders of Scoop's, so oku runs it without them.",
		"# oku runs no script of a recipe, so it left out pre_install, post_install.",
		"[runtime]\ndeps = [\"scoop:extras/helper\"]",
		"[[app]]\nname = \"Tool\"\nexec = \"bin/ToolGui.exe\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "Remove-Item") {
		t.Fatalf("the manifest kept a script:\n%s", out)
	}
}

func TestB292AScoopRefNamesItsBucket(t *testing.T) {
	m := newMachine(t)
	recipeServer{scoop: map[string]string{"ScoopInstaller/Extras/tool": scoopJSON}}.start(t, &m)

	if _, err := m.run(t, "", "manifest", "init", "--from", "scoop:main/tool", "-o", "-"); err == nil ||
		!strings.Contains(err.Error(), "no bucket of main has it") {
		t.Fatalf("want main to lack tool, got %v", err)
	}

	if _, err := m.run(t, "", "add", "scoop:nonportable/tool"); err == nil ||
		!strings.Contains(err.Error(), "a bucket that Scoop knows by name") {
		t.Fatalf("want an unknown bucket refused, got %v", err)
	}
}

func TestB292AScoopDirectoryOnPathGivesEveryProgramInIt(t *testing.T) {
	m := newMachine(t)
	recipeServer{
		versions: []string{"1.2.0"},
		files:    map[string]string{"bin/tool.exe": "x", "bin/helper.exe": "x", "bin/readme.txt": "x"},
		scoop: map[string]string{"ScoopInstaller/Main/tool": `{
  "version": "1.2.0", "url": "SERVER/dl/1.2.0/win/tool.tar.gz", "hash": "SUM1.2.0",
  "env_add_path": "bin"
}`},
	}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "scoop:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, `bin = ["bin/helper.exe", "bin/tool.exe"]`) &&
		!strings.Contains(out, `bin = ["bin/tool.exe", "bin/helper.exe"]`) {
		t.Fatalf("the manifest lacks the programs of bin:\n%s", out)
	}
}

func TestB298ACaskProgramInAMovedFolderIsFoundInTheDownload(t *testing.T) {
	m := newMachine(t)
	json := strings.Replace(caskJSON, `{"binary": ["tool"]}, {"uninstall"`,
		`{"artifact": ["sdk"], "target": "$HOMEBREW_PREFIX/share/tool"}, `+
			`{"binary": ["$HOMEBREW_PREFIX/share/tool/bin/tool"]}, {"uninstall"`, 1)
	recipeServer{casks: map[string][2]string{"tool": {json, caskRuby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, `bin = ["sdk/bin/tool"]`) {
		t.Fatalf("the program is not found in the moved folder:\n%s", out)
	}
}

func TestB302AddTranslatesAnAquaEntryAndFollowsTheRepo(t *testing.T) {
	m := newMachine(t)
	recipeServer{
		versions: []string{"1.2.0", "1.3.0"},
		// A download at another URL than the release, as aqua's http type has.
		aqua: `packages:
  - type: http
    repo_owner: owner
    repo_name: tool
    description: A tool
    url: SERVER/dl/{{.SemVer}}/{{.OS}}-{{.Arch}}/tool.tar.gz
    files:
      - name: tool
`,
	}.start(t, &m)

	out, err := m.run(t, "", "add", "aqua:owner/tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "tool 1.3.0" {
		t.Fatalf("tool printed %q, want the newest release 1.3.0", got)
	}

	if !strings.Contains(out, "aqua:owner/tool is a recipe of another package manager") {
		t.Fatalf("add did not say that it translated the entry:\n%s", out)
	}
}

func TestB302AnAquaEntryGivesEachPlatformItsOwnReleaseFile(t *testing.T) {
	m := newMachine(t)
	recipeServer{aqua: `packages:
  - type: github_release
    repo_owner: owner
    repo_name: tool
    version_constraint: "false"
    version_overrides:
      - version_constraint: semver("< 1.0.0")
        asset: old-{{.OS}}.tgz
      - version_constraint: "true"
        asset: tool_{{trimV .Version}}_{{.OS}}_{{.Arch}}.{{.Format}}
        format: tar.gz
        rosetta2: true
        supported_envs: [darwin, linux/amd64, windows/amd64]
        replacements:
          darwin: macOS
          amd64: x86_64
        overrides:
          - goos: linux
            replacements:
              linux: Linux-musl
          - goos: windows
            format: zip
            files:
              - name: tool
                src: bin/tool
        files:
          - name: tool
            src: tool_{{trimV .Version}}_{{.OS}}_{{.Arch}}/bin/tool
        checksum:
          type: github_release
          asset: checksums.txt
          algorithm: sha256
`}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "aqua:owner/tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	release := "https://github.com/owner/tool/releases/download/{{tag}}/"
	for _, want := range []string{
		"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\n",
		// Rosetta 2 runs the Intel build on arm64.
		"match = { os = \"darwin\", arch = \"arm64\" }\nurl = \"" + release + "tool_{{version}}_macOS_x86_64.tar.gz\"",
		"sha256_url = \"" + release + "checksums.txt\"",
		// An override's replacements add to the others.
		"url = \"" + release + "tool_{{version}}_Linux-musl_x86_64.tar.gz\"",
		"url = \"" + release + "tool_{{version}}_windows_x86_64.zip\"",
		// A folder named after the version is stripped, and a Windows program
		// gets ".exe".
		"strip = 1\nbin = [\"bin/tool\"]",
		"bin = [\"bin/tool.exe\"]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, `os = "linux", arch = "arm64"`) {
		t.Fatalf("the manifest has a platform that supported_envs leaves out:\n%s", out)
	}
}

func TestB302AnAquaEntryOkuCannotReadIsRefused(t *testing.T) {
	m := newMachine(t)
	recipeServer{aqua: `packages:
  - type: github_release
    repo_owner: owner
    repo_name: tool
    asset: tool-{{title .OS}}.tar.gz
`}.start(t, &m)

	_, err := m.run(t, "", "add", "aqua:owner/tool", "--yes")
	if err == nil || !strings.Contains(err.Error(), "oku has no match for {{title .OS}}") {
		t.Fatalf("want a refusal that names the template, got %v", err)
	}

	_, err = m.run(t, "", "add", "aqua:owner/none", "--yes")
	if err == nil || !strings.Contains(err.Error(), "the aqua registry has no entry for it") {
		t.Fatalf("want a missing entry named, got %v", err)
	}
}

// wingetZip is the installer manifest of Owner.Tool at version 1.10.0, a zip of
// the program in a folder named after the version, for two arches.
const wingetZip = `PackageIdentifier: Owner.Tool
ManifestVersion: 1.12.0
PackageVersion: 1.10.0
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
- RelativeFilePath: tool-1.10.0\bin\tool.exe
  PortableCommandAlias: tl
Installers:
- Architecture: x64
  InstallerUrl: https://github.com/owner/tool/releases/download/v1.10.0/tool-1.10.0-x64.zip
  InstallerSha256: 71B2FEF860ABE467217A538FF31DE02F5258807C0129F771846F87BD029AAFC5
- Architecture: arm64
  InstallerUrl: https://github.com/owner/tool/releases/download/v1.10.0/tool-1.10.0-arm64.zip
  InstallerSha256: E4ABCA10C3A64EBEA742667DD7009449D49403DB5460DD6873E389FA2945360F
- Architecture: x86
  InstallerType: nullsoft
  InstallerUrl: https://example.com/tool-setup.exe
  InstallerSha256: 9BF73BDB3FDA9AD4B0235E1295B02C717031C986AFA4D7C05DD0AF8B74010A95
`

func TestB303AWingetPackageTranslatesItsNewestVersion(t *testing.T) {
	m := newMachine(t)
	recipeServer{winget: map[string]string{
		"1.9.0":  strings.ReplaceAll(wingetZip, "1.10.0", "1.9.0"),
		"1.10.0": wingetZip,
		// A package whose identifier continues this one's keeps its folder here.
		"Beta": wingetZip,
	}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "winget:Owner.Tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	release := "https://github.com/owner/tool/releases/download/v{{version}}/"
	for _, want := range []string{
		"# Translated from the winget package Owner.Tool 1.10.0.",
		"name = \"tool\"\ndescription = \"A tool\"\nhomepage = \"https://tool.example\"",
		"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\n",
		"match = { os = \"windows\", arch = \"amd64\" }\nurl = \"" + release + "tool-{{version}}-x64.zip\"",
		"match = { os = \"windows\", arch = \"arm64\" }",
		"strip = 1\nbin = [{ name = \"tl.exe\", path = \"bin/tool.exe\" }]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	// The x86 build is a setup program, which oku does not run.
	if strings.Contains(out, "386") || strings.Contains(out, "tool-setup") {
		t.Fatalf("the manifest has the setup program:\n%s", out)
	}
}

func TestB303AWingetPackageOffGitHubPinsItsVersionAndDigest(t *testing.T) {
	m := newMachine(t)
	recipeServer{winget: map[string]string{
		"2.0.0": `PackageIdentifier: Owner.Tool
ManifestVersion: 1.12.0
PackageVersion: 2.0.0
InstallerType: portable
Commands:
- tool
Installers:
- Architecture: x64
  InstallerUrl: https://example.com/2.0.0/tool.exe
  InstallerSha256: 71B2FEF860ABE467217A538FF31DE02F5258807C0129F771846F87BD029AAFC5
`,
	}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "winget:Owner.Tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, want := range []string{
		"[version]\nvalue = \"2.0.0\"",
		"url = \"https://example.com/2.0.0/tool.exe\"",
		"sha256 = \"71b2fef860abe467217a538ff31de02f5258807c0129f771846f87bd029aafc5\"",
		"bin = [\"tool.exe\"]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}
}

func TestB303AWingetPackageThatOnlyHasASetupProgramIsRefused(t *testing.T) {
	m := newMachine(t)
	recipeServer{winget: map[string]string{
		"1.0.0": `PackageIdentifier: Owner.Tool
ManifestVersion: 1.12.0
PackageVersion: 1.0.0
InstallerType: inno
Installers:
- Architecture: x64
  InstallerUrl: https://example.com/tool-setup.exe
  InstallerSha256: 71B2FEF860ABE467217A538FF31DE02F5258807C0129F771846F87BD029AAFC5
`,
	}}.start(t, &m)

	_, err := m.run(t, "", "add", "winget:Owner.Tool", "--yes")
	if err == nil || !strings.Contains(err.Error(), "its inno installer runs when it installs") {
		t.Fatalf("want a refusal that names the installer, got %v", err)
	}

	_, err = m.run(t, "", "add", "winget:Owner.None", "--yes")
	if err == nil || !strings.Contains(err.Error(), "winget has no such package") {
		t.Fatalf("want a missing package named, got %v", err)
	}
}

func TestB305ACaskWithVersionPartsFollowsTheirSource(t *testing.T) {
	json := strings.NewReplacer(
		`"version": "1.2.0"`, `"version": "1.2.0,45"`,
		"/dl/1.2.0/", "/dl/45/",
	).Replace(caskJSON)

	for name, tc := range map[string]struct{ livecheck, want string }{
		// A block that joins the regex's groups with commas, and nothing else.
		"page": {
			livecheck: `  livecheck do
    url "SERVER/latest.html"
    regex(/tool-(\d+(?:\.\d+)+)-(\d+)\.tar/i)
    strategy :page_match do |page, regex|
      page.scan(regex).map { |match| "#{match[0]},#{match[1]}" }
    end
  end`,
			want: "[version]\nfrom = \"page\"\nrepo = \"SERVER/latest.html\"\nregex = \"(?i)tool-(\\\\d+(?:\\\\.\\\\d+)+)-(\\\\d+)\\\\.tar\"\njoin = \"+\"\n",
		},
		// Sparkle's version without a block is the short version and the build.
		"sparkle": {
			livecheck: `  livecheck do
    url "SERVER/appcast.xml"
    strategy :sparkle
  end`,
			want: "[version]\nfrom = \"sparkle\"\nrepo = \"SERVER/appcast.xml\"\njoin = \"+\"\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			ruby := strings.NewReplacer(
				"#{version}/#{os}", "#{version.csv.second}/#{os}",
				`  livecheck do
    url "SERVER/latest.json"
    strategy :json do |json|
      json["version"]
    end
  end`, tc.livecheck,
			).Replace(caskRuby)
			url := recipeServer{casks: map[string][2]string{"tool": {json, ruby}}}.start(t, &m)

			out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
			if err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}

			for _, want := range []string{
				strings.ReplaceAll(tc.want, "SERVER", url),
				`/dl/{{version_part2}}/mac-arm64/tool.tar.gz"`,
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("the manifest lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestB305AScoopURLWithADerivedVersionFollowsTheVersion(t *testing.T) {
	m := newMachine(t)
	recipeServer{scoop: map[string]string{"ScoopInstaller/Main/tool": `{
  "version": "26.03",
  "url": "SERVER/dl/2603/win/tool.zip",
  "bin": "tool.exe",
  "checkver": {"url": "SERVER/latest.json", "jsonpath": "$.version"},
  "autoupdate": {"url": "SERVER/dl/$cleanVersion/win/tool.zip"}
}`}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "scoop:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, `/dl/{{version_nodots}}/win/tool.zip"`) || strings.Contains(out, "It pins") {
		t.Fatalf("the manifest does not follow the version with {{version_nodots}}:\n%s", out)
	}
}

func TestB306AnAnswerThatLacksWhatOkuNeedsFailsAndNamesIt(t *testing.T) {
	m := newMachine(t)
	json := strings.Replace(caskJSON, `"url": "SERVER/dl/1.2.0/mac-arm64/tool.tar.gz",`, "", 1)
	recipeServer{casks: map[string][2]string{"tool": {json, caskRuby}}}.start(t, &m)

	_, err := m.run(t, "", "add", "cask:tool", "--yes")
	if err == nil || !strings.Contains(err.Error(),
		"the Homebrew API's answer for the cask tool lacks url, which oku needs, and its format may have changed") {
		t.Fatalf("want the missing url named, got %v", err)
	}

	if exists(m.profile("bin", "tool")) {
		t.Fatal("oku installed from half an answer")
	}
}

func TestB307ASourceAtAFormatVersionOkuDoesNotReadIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		server recipeServer
		ref    string
		want   string
	}{
		"winget 2": {
			server: recipeServer{winget: map[string]string{
				"1.0.0": strings.Replace(wingetZip, "ManifestVersion: 1.12.0", "ManifestVersion: 2.0.0", 1),
			}},
			ref:  "winget:Owner.Tool",
			want: "lacks ManifestVersion 1",
		},
		"aqua v5": {
			server: recipeServer{aqua: "packages: []\n", aquaTag: "v5.0.0"},
			ref:    "aqua:owner/tool",
			want:   "the aqua registry is at v5.0.0, whose format this oku does not read",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			tc.server.start(t, &m)

			if _, err := m.run(t, "", "add", tc.ref, "--yes"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// tapCask is a cask of a tap, in the Ruby of a Homebrew cask.
const tapCask = `cask "tool" do # generated
  arch arm: "arm64", intel: "x86_64"

  version '1.4.0'
  sha256 arm:   "` + "aaaa" + `",
         intel: "` + "bbbb" + `"

  home = "https://tool.example.com"

  url "SERVER/dl/v#{version}/Tool-#{version}-#{arch}.zip"
  name "Tool"
  desc "A tool from a tap"
  homepage home

  on_intel do
    url "SERVER/dl/v#{version}/Tool-#{version}-intel.zip"
  end

  livecheck do
    url :url
    strategy :github_latest
  end

  postflight do
    system_command "/usr/bin/xattr"
  end

  app "Tool-#{version}/Tool.app"
  binary "Tool-#{version}/bin/tool"
  binary "Tool-#{version}/completions/_tool",
         target: "#{HOMEBREW_PREFIX}/share/zsh/site-functions/_tool"
end
`

func TestB337ACaskOfATapTranslatesFromItsRuby(t *testing.T) {
	m := newMachine(t)

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/raw/someone/homebrew-tools/HEAD/Casks/tool.rb":
			_, _ = fmt.Fprint(w, strings.ReplaceAll(tapCask, "SERVER", server.URL))
		case "/raw/someone/homebrew-tools/HEAD/Casks/logic.rb":
			_, _ = fmt.Fprint(w, "cask \"logic\" do\n  if Hardware::CPU.intel?\n    url \"a\"\n  end\nend\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI, m.opts.GitHubRaw = server.URL+"/api", server.URL+"/raw"

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:someone/tools/tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, want := range []string{
		"# Translated from the Homebrew cask someone/tools/tool.",
		"# oku runs no script of a recipe, so it left out postflight.",
		`description = "A tool from a tap"`,
		`homepage = "https://tool.example.com"`,
		`match = { os = "darwin", arch = "arm64" }`,
		`/dl/v1.4.0/Tool-1.4.0-arm64.zip"`,
		`sha256 = "aaaa"`,
		`match = { os = "darwin", arch = "amd64" }`,
		`/dl/v1.4.0/Tool-1.4.0-intel.zip"`,
		`sha256 = "bbbb"`,
		`app = ["Tool-1.4.0/Tool.app"]`,
		`bin = ["Tool-1.4.0/bin/tool"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "_tool") {
		t.Fatalf("a completion file became a program:\n%s", out)
	}

	if _, err := m.run(t, "", "manifest", "init", "--from", "cask:someone/tools/logic", "-o", "-"); err == nil ||
		!strings.Contains(err.Error(), "Ruby logic") {
		t.Fatalf("want a cask with Ruby logic refused, got %v", err)
	}

	if _, err := m.run(t, "", "manifest", "init", "--from", "cask:someone/tools/other", "-o", "-"); err == nil ||
		!strings.Contains(err.Error(), "the tap someone/homebrew-tools has no cask other") {
		t.Fatalf("want a missing cask named with its tap, got %v", err)
	}
}

func TestB336AScoopRefReadsABucketOnGitHub(t *testing.T) {
	m := newMachine(t)
	recipeServer{scoop: map[string]string{"someone/scoop-tools/tool": scoopJSON}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "scoop:someone/scoop-tools/tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, "# Translated from the Scoop manifest someone/scoop-tools/tool.") ||
		!strings.Contains(out, `/dl/{{version}}/win-x64/tool.zip"`) {
		t.Fatalf("the manifest does not come from the bucket on GitHub:\n%s", out)
	}

	if _, err := m.run(t, "", "manifest", "init", "--from", "scoop:someone/scoop-tools/other", "-o", "-"); err == nil ||
		!strings.Contains(err.Error(), "no bucket of someone/scoop-tools has it") {
		t.Fatalf("want a missing manifest named with its bucket, got %v", err)
	}
}

func TestB338ALivecheckBlockThatReadsFieldsOfAFeedTranslates(t *testing.T) {
	json := strings.NewReplacer(
		`"version": "1.2.0"`, `"version": "1.2.0,45"`,
		"/dl/1.2.0/", "/dl/45/",
	).Replace(caskJSON)

	for name, tc := range map[string]struct{ livecheck, want string }{
		"a list": {
			livecheck: `  livecheck do
    url "SERVER/releases.json"
    strategy :json do |json|
      json["TOOL"]&.map do |release|
        version = release["version"]
        build = release["build"]
        next if version.blank? || build.blank?

        "#{version},#{build}"
      end
    end
  end`,
			want: "[version]\nfrom = \"page\"\nrepo = \"SERVER/releases.json\"\njson = [\"TOOL.*.version\", \"TOOL.*.build\"]\njoin = \"+\"\n",
		},
		"a field and a match": {
			livecheck: `  livecheck do
    url "SERVER/update.json"
    regex(%r{/production/(\h+)/}i)
    strategy :json do |json, regex|
      ver = json["name"] || json["version"]
      next unless ver

      match = json["url"]&.match(regex)
      next if match.blank?

      "#{ver},#{match[1]}"
    end
  end`,
			want: "json = [\"name\", \"url\"]\nregex = \"^([^\\\\n]*)\\\\n[^\\\\n]*?(?i:/production/([0-9a-fA-F]+)/)[^\\\\n]*\"\njoin = \"+\"\n",
		},
		"a property list": {
			livecheck: `  livecheck do
    url "SERVER/update#{version.major}.xml"
    strategy :xml do |xml|
      version = xml.elements["//key[text()='version']"]&.next_element&.text
      build = xml.elements["//key[text()='build']"]&.next_element&.text
      next if version.blank? || build.blank?

      "#{version.strip},#{build.strip}"
    end
  end`,
			want: "repo = \"SERVER/update1.xml\"\njson = [\"version\", \"build\"]\njoin = \"+\"\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			ruby := strings.NewReplacer(
				"#{version}/#{os}", "#{version.csv.second}/#{os}",
				`  livecheck do
    url "SERVER/latest.json"
    strategy :json do |json|
      json["version"]
    end
  end`, tc.livecheck,
			).Replace(caskRuby)
			url := recipeServer{casks: map[string][2]string{"tool": {json, ruby}}}.start(t, &m)

			out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
			if err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}

			if !strings.Contains(out, strings.ReplaceAll(tc.want, "SERVER", url)) {
				t.Fatalf("the manifest lacks %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestB339APageSourceReadsFieldsOfAJSONFeedOrAPropertyList(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho tool\n"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases.json":
			fmt.Fprint(w, `{"TOOL": [{"version": "1.3.0", "build": 46}, {"version": "1.4.0", "build": 47}, {"version": "1.2.0"}]}`)
		case "/update.json":
			fmt.Fprint(w, `{"url": "https://dl.example.com/production/abc123/tool.zip", "name": "2.0.1"}`)
		case "/host.json":
			fmt.Fprint(w, `{"full": {"host_version": [0, 0, 413]}}`)
		case "/update.xml":
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>build</key><integer>2349</integer>
  <key>version</key><string> 5.8.1 </string>
</dict></plist>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	for source, want := range map[string]string{
		`repo = "SERVER/releases.json"` + "\njson = [\"TOOL.*.version\", \"TOOL.*.build\"]\njoin = \"+\"":                            "1.4.0+47",
		`repo = "SERVER/update.json"` + "\njson = [\"name\", \"url\"]\nregex = '^(\\S+)\\n.*/production/([0-9a-f]+)/'\njoin = \"+\"": "2.0.1+abc123",
		`repo = "SERVER/update.xml"` + "\njson = [\"version\", \"build\"]\njoin = \"+\"":                                             "5.8.1+2349",
		`repo = "SERVER/host.json"` + "\njson = [\"full.host_version\"]":                                                             "0.0.413",
	} {
		ref := filepath.Join(m.fixtures, "tool.toml")
		must(t, os.WriteFile(ref, []byte(fmt.Sprintf(`[package]
name = "tool"
[version]
from = "page"
%s
[[artifact]]
url = "file://%s"
bin = ["tool"]
`, strings.ReplaceAll(source, "SERVER", server.URL), archive)), 0o644))

		out, err := m.run(t, "", "add", ref, "--plan")
		if err != nil || !strings.Contains(out, "version    "+want) {
			t.Fatalf("%s: want version %s, got %v\n%s", source, want, err, out)
		}
	}

	// json belongs to the page source.
	ref := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(`[package]
name = "tool"
[version]
from = "github-releases"
repo = "owner/tool"
json = ["version"]
[[artifact]]
url = "file://%s"
bin = ["tool"]
`, archive)), 0o644))

	if out, err := m.run(t, "", "manifest", "lint", ref); err == nil || !strings.Contains(out, `version.json needs version.from = "page"`) {
		t.Fatalf("want json refused beside github-releases, got %v\n%s", err, out)
	}
}

func TestB338AOneLineLivecheckBlockTranslates(t *testing.T) {
	m := newMachine(t)
	ruby := strings.Replace(caskRuby, `      json["version"]`, `      json.dig("full", "host_version")&.join(".")`, 1)
	url := recipeServer{casks: map[string][2]string{"tool": {caskJSON, ruby}}}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "cask:tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if want := "repo = \"" + url + "/latest.json\"\njson = [\"full.host_version\"]\n"; !strings.Contains(out, want) {
		t.Fatalf("the manifest lacks %q:\n%s", want, out)
	}
}
