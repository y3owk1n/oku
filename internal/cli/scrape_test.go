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
		artifact("version = { from = \"npm\", repo = \"x\" }\n"):                                                `"redirect" or "page"`,
		artifact("version = { from = \"page\", repo = \"https://example.com\", regex = '(.)', tag = \"x\" }\n"): "from, repo and regex only",
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
