package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
)

// vendorServer serves a download of tool for each version at
// /dl/<version>/tool.tar.gz, and feed at /feed. /latest/<channel> redirects to
// /hop/<channel>, which has no version, and that redirects to the download of
// the channel's current version.
type vendorServer struct {
	*httptest.Server

	mu      sync.Mutex
	current map[string]string
	feed    string
	asked   int
}

func (m machine) vendorServer(t *testing.T, versions ...string) *vendorServer {
	t.Helper()

	for _, version := range versions {
		m.archive(t, "tool-"+version, map[string]string{"tool": "#!/bin/sh\necho " + version + "\n"})
	}

	s := &vendorServer{current: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		hop, isHop := strings.CutPrefix(r.URL.Path, "/hop/")

		switch version, ok := strings.CutPrefix(r.URL.Path, "/dl/"); {
		case strings.HasPrefix(r.URL.Path, "/latest/"):
			s.asked++
			http.Redirect(w, r, "/hop/"+strings.TrimPrefix(r.URL.Path, "/latest/"), http.StatusFound)
		case isHop:
			http.Redirect(w, r, "/dl/"+s.current[hop]+"/tool.tar.gz", http.StatusFound)
		case r.URL.Path == "/feed":
			s.asked++
			_, _ = w.Write([]byte(s.feed))
		case ok:
			http.ServeFile(w, r, filepath.Join(m.fixtures, "tool-"+strings.TrimSuffix(version, "/tool.tar.gz")+".tar.gz"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)

	return s
}

// set moves channel to version.
func (s *vendorServer) set(channel, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.current[channel] = version
}

func (s *vendorServer) setFeed(feed string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.feed = feed
}

func (s *vendorServer) timesAsked() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.asked
}

// scrapedManifest writes a manifest that finds its version with version, and
// downloads tool from s.
func (m machine) scrapedManifest(t *testing.T, s *vendorServer, version string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\n[version]\n"+version+"\n"+
			"[[artifact]]\nurl = \""+s.URL+"/dl/{{version}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
	), 0o644))

	return path
}

func TestB280RedirectVersionFollowsTheLocationUntilUpdate(t *testing.T) {
	m := newMachine(t)
	s := m.vendorServer(t, "1.0.0", "1.1.0")
	s.set("tool", "1.0.0")
	ref := m.scrapedManifest(t, s,
		"from = \"redirect\"\nrepo = \""+s.URL+"/latest/tool\"\nregex = '/dl/([\\d.]+)/'")

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("add installed %q, want the 1.0.0 the redirect names", got)
	}

	s.set("tool", "1.1.0")
	asked := s.timesAsked()

	// Another machine has the lock and no store. It installs the locked version,
	// and does not ask where the redirect points now.
	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("sync installed %q, want the locked 1.0.0", got)
	}

	if s.timesAsked() != asked {
		t.Fatal("sync asked upstream for the version")
	}

	out, err := m.run(t, "", "add", ref+"@1.0.0", "--yes")
	if err == nil || !strings.Contains(err.Error(), "1.1.0") {
		t.Fatalf("add @1.0.0 did not fail naming the 1.1.0 upstream is at: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "update", "tool", "--yes"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("update installed %q, want the 1.1.0 the redirect names now", got)
	}
}

func TestB281PageVersionJoinsTheGroupsAndFailsWithoutAVersion(t *testing.T) {
	m := newMachine(t)
	s := m.vendorServer(t, "0.0.413")
	s.setFeed(`{"full":{"host_version":[0,0,413]}}`)
	ref := m.scrapedManifest(t, s,
		"from = \"page\"\nrepo = \""+s.URL+"/feed\"\nregex = '\"host_version\":\\[(\\w+),(\\w+),(\\w+)\\]'")

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.lockedVersion(t, "tool"); got != "0.0.413" {
		t.Fatalf("locked %q, want 0.0.413 from the groups joined with dots", got)
	}

	for feed, want := range map[string]string{
		`{"full":{}}`:                       "matches nothing",
		`{"full":{"host_version":[a,b,c]}}`: "not a version",
	} {
		s.setFeed(feed)

		_, err := m.run(t, "", "update", "tool", "--yes")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("update with the page %s did not fail saying %q: %v", feed, want, err)
		}

		if got := m.lockedVersion(t, "tool"); got != "0.0.413" {
			t.Fatalf("a failed update locked %q", got)
		}
	}
}

func TestB282LintChecksARedirectOrPageSource(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")

	write := func(version string) {
		must(t, os.WriteFile(path, []byte(
			"[package]\nname = \"tool\"\ndescription = \"a tool\"\n[version]\n"+version+
				"\n[[artifact]]\nurl = \"https://example.com/{{version}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
		), 0o644))
	}

	for version, want := range map[string]string{
		"from = \"redirect\"\nrepo = \"https://example.com/latest\"":                               "version.regex is required",
		"from = \"page\"\nrepo = \"example.com/feed\"\nregex = '([0-9.]+)'":                        "http(s) URL",
		"from = \"page\"\nrepo = \"https://example.com/feed\"\nregex = '[0-9.]+'":                  "needs a group",
		"from = \"page\"\nrepo = \"https://example.com/feed\"\nregex = '(['":                       "version.regex",
		"from = \"redirect\"\nrepo = \"https://example.com\"\nregex = '(.)'\nstrip_prefix = \"v\"": "strip_prefix",
		"from = \"redirect\"\nrepo = \"https://example.com\"\nregex = '(.)'\ntag = \"nightly\"":    "version.tag",
		"from = \"git-tags\"\nrepo = \"https://example.com/tool\"\nregex = '(.)'":                  "version.regex needs",
	} {
		write(version)

		out, err := m.run(t, "", "manifest", "lint", path)
		if err == nil || !strings.Contains(out, want) {
			t.Errorf("lint accepted %q, or did not say %q: %v\n%s", version, want, err, out)
		}
	}

	write("from = \"redirect\"\nrepo = \"https://example.com/latest\"\nregex = '/([0-9.]+)/'")

	out, err := m.run(t, "", "manifest", "lint", path)
	if err != nil || !strings.Contains(out, "trust the first download") {
		t.Errorf("lint did not warn that users trust the first download: %v\n%s", err, out)
	}
}

// perPlatformManifest writes a manifest whose host artifact follows the channel
// "host" of s and whose artifact for other follows "other".
func (m machine) perPlatformManifest(t *testing.T, s *vendorServer, other platform.Platform) string {
	t.Helper()

	host := platform.Host()
	artifact := func(match, channel string) string {
		return fmt.Sprintf(
			"[[artifact]]\nmatch = %s\nversion = { from = \"redirect\", repo = \"%s/latest/%s\", regex = '/dl/([0-9.]+)/' }\n"+
				"url = \"%s/dl/{{version}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
			match, s.URL, channel, s.URL,
		)
	}

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\n"+
			artifact(fmt.Sprintf("{ os = %q, arch = %q, libc = %q }", host.OS, host.Arch, host.Libc), "host")+
			artifact(fmt.Sprintf("{ os = %q, arch = %q }", other.OS, other.Arch), "other"),
	), 0o644))

	return path
}

func TestB283EachArtifactFollowsItsOwnVersion(t *testing.T) {
	m := newMachine(t)

	// A platform that sorts before the host names the package's version, which
	// shows that the host does not.
	other := platform.All()[0]
	if other == platform.Host() {
		other = platform.All()[1]
	}

	s := m.vendorServer(t, "1.0.0", "1.1.0", "2.0.0", "2.1.0")
	s.set("host", "1.0.0")
	s.set("other", "2.0.0")
	ref := m.perPlatformManifest(t, s, other)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref,
	)), 0o644))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	lockPath := filepath.Join(m.config, "oku.lock")
	platformVersion := func(key string) (lock.Platform, string) {
		t.Helper()

		locked, err := lock.Read(lockPath)
		must(t, err)

		pkg, _ := locked.Find("tool")

		return pkg.Platforms[key], pkg.Version
	}

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("installed %q, want the host's 1.0.0", got)
	}

	at, _ := platformVersion(other.String())
	if at.Version != "2.0.0" || !strings.Contains(at.URL, "/dl/2.0.0/") {
		t.Fatalf("oku.lock pins %s at %+v, want its own 2.0.0", other, at)
	}

	// Only the other platform moves. update pins its new download and leaves the
	// host where it is.
	s.set("other", "2.1.0")

	out, err := m.run(t, "", "update", "tool", "--yes")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if !strings.Contains(out, other.String()+" 2.0.0") || !strings.Contains(out, "2.1.0") {
		t.Fatalf("update did not report that %s moved to 2.1.0:\n%s", other, out)
	}

	at, pkgVersion := platformVersion(other.String())
	if at.Version != "2.1.0" || !strings.Contains(at.URL, "/dl/2.1.0/") {
		t.Fatalf("update left %s at %+v, want 2.1.0", other, at)
	}

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("update moved the host to %q", got)
	}

	// The package's version is the first platform's, whatever the host is.
	first := min(platform.Host().String(), other.String())
	if want, _ := platformVersion(first); pkgVersion != want.Version {
		t.Fatalf("oku.lock gives the package %s, want %s of %s", pkgVersion, want.Version, first)
	}

	// Another machine installs the host's locked version and writes the same
	// lock, although upstream moved.
	s.set("host", "1.1.0")

	before, err := os.ReadFile(lockPath)
	must(t, err)
	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	if out, err := m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("sync installed %q, want the locked 1.0.0", got)
	}

	after, err := os.ReadFile(lockPath)
	must(t, err)

	if string(before) != string(after) {
		t.Fatalf("sync rewrote oku.lock:\n%s\nto\n%s", before, after)
	}

	if out := m.stdout(t, "outdated"); !strings.Contains(out, "1.1.0") {
		t.Fatalf("outdated did not list the host's 1.1.0:\n%s", out)
	}

	out, err = m.run(t, "", "add", ref+"@1.1.0", "--yes")
	if err == nil || !strings.Contains(err.Error(), "finds its own version") {
		t.Fatalf("add @1.1.0 did not refuse a version: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "update", "tool", "--yes"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("update installed %q, want the host's 1.1.0", got)
	}
}

func TestB284LintChecksAVersionInEachArtifact(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")

	const good = "version = { from = \"redirect\", repo = \"https://example.com/latest\", regex = '/([0-9.]+)/' }\n"

	artifact := func(version string) string {
		return "[[artifact]]\n" + version + "url = \"https://example.com/{{version}}/tool.tar.gz\"\nbin = [\"tool\"]\n"
	}

	for body, want := range map[string]string{
		"[version]\nvalue = \"1.0.0\"\n" + artifact(good):                                                       "not both",
		artifact(good) + artifact(""):                                                                           "artifact[1]: version is required",
		artifact(good) + "[build]\n[[build.step]]\nrun = \"true\"\n":                                            "[build] needs [version]",
		artifact("version = { from = \"npm\", repo = \"x\" }\n"):                                                `"git-tags", "redirect", "page" or "sparkle"`,
		artifact("version = { from = \"page\", repo = \"https://example.com\", regex = '(.)', tag = \"x\" }\n"): "from, repo, regex, join and strip_prefix only",
		artifact("version = { from = \"github-releases\", repo = \"not a repo\" }\n"):                           `artifact[0].version.repo must be "owner/repo"`,
		artifact("version = { from = \"github-releases\", repo = \"o/r\", regex = '(.)' }\n"):                   "artifact[0].version.regex and artifact[0].version.join need",
		artifact("version = { from = \"page\", repo = \"https://example.com\" }\n"):                             "artifact[0].version.regex is required",
	} {
		must(t, os.WriteFile(path, []byte("[package]\nname = \"tool\"\ndescription = \"a tool\"\n"+body), 0o644))

		out, err := m.run(t, "", "manifest", "lint", path)
		if err == nil || !strings.Contains(out, want) {
			t.Errorf("lint accepted\n%s\nor did not say %q: %v\n%s", body, want, err, out)
		}
	}

	must(t, os.WriteFile(path, []byte("[package]\nname = \"tool\"\ndescription = \"a tool\"\n"+artifact(good)), 0o644))

	out, err := m.run(t, "", "manifest", "lint", path)
	if err != nil || !strings.Contains(out, "trust the first download") {
		t.Errorf("lint did not accept the manifest and warn: %v\n%s", err, out)
	}

	if _, err := m.run(t, "", "manifest", "bump", path); err == nil {
		t.Fatal("bump accepted a manifest whose artifacts find their own versions")
	}

	// A file of a GitHub release has the digest GitHub reports, so lint warns
	// about the page's download only.
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\ndescription = \"a tool\"\n"+
			"[[artifact]]\nmatch = { os = \"linux\" }\n"+
			"version = { from = \"github-releases\", repo = \"owner/tool\", strip_prefix = \"v\" }\n"+
			"url = \"https://github.com/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n"+
			artifact("match = { os = \"darwin\" }\n"+good),
	), 0o644))

	out, err = m.run(t, "", "manifest", "lint", path)
	if err != nil || !strings.Contains(out, "artifact[1]: no sha256") || strings.Contains(out, "artifact[0]") {
		t.Errorf("lint did not warn about the page's download alone: %v\n%s", err, out)
	}
}

func TestB285AnArtifactVersionGivenAsAStringIsRefusedByName(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")

	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\ndescription = \"a tool\"\n[version]\nvalue = \"1.0.0\"\n"+
			"[[artifact]]\nversion = \"1.0.0\"\nurl = \"https://example.com/tool.tar.gz\"\nbin = [\"tool\"]\n",
	), 0o644))

	for _, args := range [][]string{{"manifest", "lint", path}, {"add", path, "--yes"}} {
		out, err := m.run(t, "", args...)
		if err == nil || !strings.Contains(out+err.Error(), "version must be a table") {
			t.Errorf("%s did not refuse the string and name the table: %v\n%s", args[0], err, out)
		}
	}
}

func TestB300AnArtifactFollowsReleasesWhileAnotherFollowsARedirect(t *testing.T) {
	m := newMachine(t)

	other := platform.All()[0]
	if other == platform.Host() {
		other = platform.All()[1]
	}

	vendor := m.vendorServer(t, "1.5.0")
	vendor.set("other", "1.5.0")

	archive, sum := m.archive(t, "release", map[string]string{"tool": "#!/bin/sh\necho 2.0.0\n"})

	var github *httptest.Server

	github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/releases":
			fmt.Fprintf(w, `[{"tag_name": "v2.0.0", "assets": [{"name": "tool.tar.gz", `+
				`"browser_download_url": %q, "digest": "sha256:%s"}]}]`,
				github.URL+"/owner/tool/releases/download/v2.0.0/tool.tar.gz", sum)
		case "/owner/tool/releases/download/v2.0.0/tool.tar.gz":
			http.ServeFile(w, r, archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(github.Close)

	host := platform.Host()
	ref := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[[artifact]]\nmatch = { os = %q, arch = %q, libc = %q }\n"+
			"version = { from = \"github-releases\", repo = \"owner/tool\", strip_prefix = \"v\" }\n"+
			"url = \"%s/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n"+
			"[[artifact]]\nmatch = { os = %q, arch = %q }\n"+
			"version = { from = \"redirect\", repo = \"%s/latest/other\", regex = '/dl/([0-9.]+)/' }\n"+
			"url = \"%s/dl/{{version}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
		host.OS, host.Arch, host.Libc, github.URL, other.OS, other.Arch, vendor.URL, vendor.URL,
	)), 0o644))

	list := fmt.Sprintf("[lock]\nplatforms = [%q]\n\n[packages]\ntool = %q\n", other.String(), ref)
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(list), 0o644))

	m.opts.GitHubAPI = github.URL + "/api"

	out, err := m.run(t, "", "sync", "--yes")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "2.0.0" {
		t.Fatalf("installed %q, want the release 2.0.0", got)
	}

	// GitHub reports the digest of the host's file, so oku trusts only the
	// redirect's download on first use.
	if strings.Contains(out, "trusted this download") {
		t.Fatalf("oku trusted the release file whose digest GitHub reports:\n%s", out)
	}

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	pkg, _ := locked.Find("tool")
	if at := pkg.Platforms[host.String()]; at.Version != "2.0.0" || at.Tag != "v2.0.0" || at.SHA256 != sum {
		t.Fatalf("oku.lock pins the host at %+v, want 2.0.0 from the tag v2.0.0", at)
	}

	if at := pkg.Platforms[other.String()]; at.Version != "1.5.0" || at.Tag != "" {
		t.Fatalf("oku.lock pins %s at %+v, want the redirect's 1.5.0", other, at)
	}

	// Another machine installs from the lock, and downloads from the locked tag.
	fresh := newMachine(t)
	fresh.opts.GitHubAPI = m.opts.GitHubAPI

	must(t, os.MkdirAll(fresh.config, 0o755))
	must(t, os.WriteFile(filepath.Join(fresh.config, "oku.toml"), []byte(list), 0o644))

	data, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(fresh.config, "oku.lock"), data, 0o644))

	if out, err := fresh.run(t, "", "sync", "--locked", "--yes"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, out)
	}

	if got := fresh.toolOutput(t); got != "2.0.0" {
		t.Fatalf("the lock installed %q on another machine, want 2.0.0", got)
	}
}

// partsManifest writes a manifest whose version source is version and whose
// download path is dir, a template of the version's parts.
func (m machine) partsManifest(t *testing.T, s *vendorServer, version, dir string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\ndescription = \"a tool\"\n[version]\n"+version+"\n"+
			"[[artifact]]\nurl = \""+s.URL+"/dl/"+dir+"/tool.tar.gz\"\nbin = [\"tool\"]\n",
	), 0o644))

	return path
}

func TestB304AURLUsesThePartsOfAVersion(t *testing.T) {
	m := newMachine(t)
	s := m.vendorServer(t, "1.2-45")
	s.setFeed("Tool 1.2.3 build 45")

	ref := m.partsManifest(t, s,
		"from = \"page\"\nrepo = \""+s.URL+"/feed\"\nregex = 'Tool ([\\d.]+) build (\\d+)'\njoin = \"+\"",
		"{{version_major}}.{{version_minor}}-{{version_part2}}")

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.2-45" {
		t.Fatalf("installed %q, want the download of 1.2-45", got)
	}

	if got := m.lockedVersion(t, "tool"); got != "1.2.3+45" {
		t.Fatalf("locked %q, want the groups joined with +", got)
	}

	// A part the version does not have fails the download, and names it.
	// The manifest at the same path now wants a patch number.
	s.setFeed("Tool 1.2 build 45")
	m.partsManifest(t, s,
		"from = \"page\"\nrepo = \""+s.URL+"/feed\"\nregex = 'Tool ([\\d.]+) build (\\d+)'\njoin = \"+\"",
		"{{version_patch}}")

	_, err := m.run(t, "", "update", "tool", "--yes")
	if err == nil || !strings.Contains(err.Error(), "{{version_patch}}: the version 1.2+45 has no patch number") {
		t.Fatalf("want the missing patch number named, got %v", err)
	}
}

func TestB304ASparkleFeedJoinsTheShortVersionAndTheBuild(t *testing.T) {
	m := newMachine(t)
	s := m.vendorServer(t, "1.2.0-87")
	s.setFeed(`<rss><channel>
<item><sparkle:shortVersionString>1.1.0 (80)</sparkle:shortVersionString><sparkle:version>80</sparkle:version></item>
<item><enclosure url="x" sparkle:shortVersionString="1.2.0 (87)" sparkle:version="87"/></item>
</channel></rss>`)

	ref := m.partsManifest(t, s, "from = \"sparkle\"\nrepo = \""+s.URL+"/feed\"\njoin = \"+\"",
		"{{version_part1}}-{{version_part2}}")

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.lockedVersion(t, "tool"); got != "1.2.0+87" {
		t.Fatalf("locked %q, want the short version and the build of the newest item", got)
	}
}

func TestB304LintKnowsTheVersionPartsAndChecksJoin(t *testing.T) {
	m := newMachine(t)
	path := filepath.Join(m.fixtures, "tool.toml")

	lint := func(version, url string) (string, error) {
		must(t, os.WriteFile(path, []byte("[package]\nname = \"tool\"\ndescription = \"a tool\"\n[version]\n"+version+
			"\n[[artifact]]\nurl = \""+url+"\"\nsha256 = \""+strings.Repeat("a", 64)+"\"\nbin = [\"tool\"]\n"), 0o644))

		return m.run(t, "", "manifest", "lint", path)
	}

	page := "from = \"page\"\nrepo = \"https://example.com\"\nregex = '(\\d+)-(\\d+)'"

	out, err := lint(page+"\njoin = \"+\"",
		"https://example.com/{{version_nodots}}/{{version_underscores}}/{{version_dashes}}/{{version_part2}}.zip")
	if err != nil {
		t.Fatalf("lint refused the version parts: %v\n%s", err, out)
	}

	for version, want := range map[string]string{
		page + "\njoin = \"x\"": `version.join must be ".", "+", "-" or "_"`,
		"from = \"github-releases\"\nrepo = \"owner/tool\"\njoin = \"+\"": "version.join needs version.regex",
	} {
		if out, err := lint(version, "https://example.com/{{version}}.zip"); err == nil || !strings.Contains(out, want) {
			t.Errorf("lint accepted\n%s\nor did not say %q: %v\n%s", version, want, err, out)
		}
	}

	if out, err := lint(page, "https://example.com/{{version_bogus}}.zip"); err == nil ||
		!strings.Contains(out, "unknown template variable {{version_bogus}}") {
		t.Errorf("lint accepted an unknown variable: %v\n%s", err, out)
	}
}
