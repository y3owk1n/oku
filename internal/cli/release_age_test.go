package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ageServer fakes GitHub for owner/tool with a release per version, published
// that long ago. A zero age leaves the release without a publish time. It
// returns a manifest that installs the release's tool.
func ageServer(t *testing.T, m *machine, ages map[string]time.Duration) string {
	t.Helper()

	var server *httptest.Server

	archives := map[string]string{}
	for version := range ages {
		archives[version], _ = m.archive(t, "tool-"+version, map[string]string{
			"tool": "#!/bin/sh\necho " + version + "\n",
		})
	}

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/repos/owner/tool/releases" {
			var releases []string

			for version, age := range ages {
				published := ""
				if age > 0 {
					at := time.Now().Add(-age).UTC().Format(time.RFC3339)
					published = fmt.Sprintf(`, "published_at": %q`, at)
				}

				releases = append(releases, fmt.Sprintf(
					`{"tag_name": "v%s"%s, "assets": [{"name": "tool.tar.gz", "browser_download_url": %q}]}`,
					version, published, server.URL+"/download/v"+version+"/tool.tar.gz",
				))
			}

			fmt.Fprintf(w, "[%s]", strings.Join(releases, ","))

			return
		}

		for version, archive := range archives {
			if r.URL.Path == "/download/v"+version+"/tool.tar.gz" {
				http.ServeFile(w, r, archive)

				return
			}
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"%s/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
		server.URL,
	)), 0o644))

	return path
}

func (m machine) toolVersion(t *testing.T) string {
	t.Helper()

	out, err := exec.Command(m.profile("bin", "tool")).Output()
	must(t, err)

	return strings.TrimSpace(string(out))
}

func TestB372AddTakesTheNewestVersionOlderThanTheMinimumReleaseAge(t *testing.T) {
	m := newMachine(t)
	tool := ageServer(t, &m, map[string]time.Duration{
		"1.0.0": 10 * 24 * time.Hour, "1.1.0": 2 * time.Hour,
	})

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	if got := m.toolVersion(t); got != "1.0.0" {
		t.Fatalf("add took %s, want 1.0.0, since 1.1.0 is 2 hours old", got)
	}

	out := m.stdout(t, "outdated")
	if !strings.Contains(out, "waiting") || !strings.Contains(out, "1.1.0 from") {
		t.Fatalf("outdated does not say that 1.1.0 waits:\n%s", out)
	}

	_, err = m.run(t, "", "update", "tool", "--min-release-age", "0")
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" {
		t.Fatalf("update --min-release-age 0 took %s, want 1.1.0", got)
	}

	// The age never takes a package back to an older version.
	out, err = m.run(t, "", "update", "tool")
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" || strings.Contains(out, "gives no release time") {
		t.Fatalf("a plain update took %s and said:\n%s", got, out)
	}

	if out := m.stdout(t, "outdated"); strings.Contains(out, "waiting") {
		t.Fatalf("outdated shows a version as waiting behind the locked one:\n%s", out)
	}
}

func TestB372ListAndEntrySetTheMinimumReleaseAge(t *testing.T) {
	for _, tc := range []struct {
		name, list, want string
	}{
		{"list turns it off", "[lock]\nmin_release_age = \"0\"\n[packages]\ntool = %q\n", "1.1.0"},
		{"list asks for more", "[lock]\nmin_release_age = \"2w\"\n[packages]\ntool = %q\n", ""},
		{"entry turns it off", "[packages]\ntool = { ref = %q, min_release_age = \"0\" }\n", "1.1.0"},
		{"entry beats the list", "[lock]\nmin_release_age = \"0\"\n" +
			"[packages]\ntool = { ref = %q, min_release_age = \"1d\" }\n", "1.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			tool := ageServer(t, &m, map[string]time.Duration{
				"1.0.0": 10 * 24 * time.Hour, "1.1.0": 2 * time.Hour,
			})

			m.writeFilesList(t, fmt.Sprintf(tc.list, tool))

			out, err := m.run(t, "", "sync")
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "--min-release-age 0") {
					t.Fatalf("sync with every version too new: %v\n%s", err, out)
				}

				return
			}

			must(t, err)

			if got := m.toolVersion(t); got != tc.want {
				t.Fatalf("sync took %s, want %s", got, tc.want)
			}
		})
	}
}

func TestB372AnExactVersionSkipsTheMinimumReleaseAge(t *testing.T) {
	m := newMachine(t)
	tool := ageServer(t, &m, map[string]time.Duration{
		"1.0.0": 10 * 24 * time.Hour, "1.1.0": 2 * time.Hour,
	})

	_, err := m.run(t, "", "add", tool+"@1.1.0")
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" {
		t.Fatalf("add @1.1.0 took %s", got)
	}
}

func TestB373AVersionWithoutAReleaseTimeIsTakenWithANote(t *testing.T) {
	m := newMachine(t)
	tool := ageServer(t, &m, map[string]time.Duration{"1.0.0": 0})

	out, err := m.run(t, "", "add", tool)
	must(t, err)

	if got := m.toolVersion(t); got != "1.0.0" || !strings.Contains(out, "gives no release time") {
		t.Fatalf("add took %s and said:\n%s", got, out)
	}
}

// undated serves owner/tool with releases 1.0.0 and 1.1.0 and no release times,
// and asks the real default of [lock] unknown_release_age.
func undated(t *testing.T) (machine, string) {
	t.Helper()

	m := newMachine(t)
	m.opts.UnknownReleaseAge = ""
	tool := ageServer(t, &m, map[string]time.Duration{"1.0.0": 0, "1.1.0": 0})

	return m, tool
}

func TestB375WarnRefusesANewUndatedVersionWithoutATerminal(t *testing.T) {
	m, tool := undated(t)

	_, err := m.run(t, "", "add", tool)
	if err == nil || !strings.Contains(err.Error(), "--accept-unknown-age") {
		t.Fatalf("add without a terminal took an undated version: %v", err)
	}

	out, err := m.run(t, "", "add", tool, "--accept-unknown-age")
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" || !strings.Contains(out, "gives no release time") {
		t.Fatalf("--accept-unknown-age took %s and said:\n%s", got, out)
	}
}

func TestB375WarnAsksOnATerminal(t *testing.T) {
	m, tool := undated(t)
	m.opts.Interactive = yes()

	out, err := m.run(t, "y\n", "add", tool)
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" || !strings.Contains(out, "take it? [y/N]") {
		t.Fatalf("a yes took %s after:\n%s", got, out)
	}
}

func TestB375ANoKeepsTheLockedVersionAndTheUpdateGoesOn(t *testing.T) {
	m, tool := undated(t)

	_, err := m.run(t, "", "add", tool+"@1.0.0")
	must(t, err)

	// The list lets tool move again.
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

	for _, tc := range []struct {
		name  string
		stdin string
		tty   bool
	}{
		{"without a terminal", "", false},
		{"a no on a terminal", "n\n", true},
	} {
		m.opts.Interactive = &tc.tty

		out, err := m.run(t, tc.stdin, "update")
		if err != nil {
			t.Fatalf("%s: update failed: %v\n%s", tc.name, err, out)
		}

		if got := m.toolVersion(t); got != "1.0.0" || !strings.Contains(out, "stays at 1.0.0") {
			t.Fatalf("%s: update left %s and said:\n%s", tc.name, got, out)
		}
	}
}

func TestB375RefuseAndThePackagesOwnAge(t *testing.T) {
	m, tool := undated(t)
	m.opts.Interactive = yes()

	m.writeFilesList(t, fmt.Sprintf(
		"[lock]\nunknown_release_age = \"refuse\"\n[packages]\ntool = %q\n", tool,
	))

	if _, err := m.run(t, "y\n", "sync"); err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("refuse asked or took an undated version: %v", err)
	}

	m.writeFilesList(t, fmt.Sprintf(
		"[lock]\nunknown_release_age = \"refuse\"\n[packages]\ntool = { ref = %q, min_release_age = \"0\" }\n",
		tool,
	))

	_, err := m.run(t, "", "sync")
	must(t, err)

	if got := m.toolVersion(t); got != "1.1.0" {
		t.Fatalf("min_release_age 0 took %s", got)
	}

	m.writeFilesList(t, "[lock]\nunknown_release_age = \"never\"\n")

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "wants \"allow\"") {
		t.Fatalf("an unknown value passed: %v", err)
	}
}

func TestB376ASparkleItemsPubDateIsItsReleaseTime(t *testing.T) {
	m := newMachine(t)
	m.opts.UnknownReleaseAge = ""
	s := m.vendorServer(t, "1.2.0")

	feed := func(age time.Duration) {
		s.setFeed(`<rss><channel><item><sparkle:shortVersionString>1.2.0</sparkle:shortVersionString>` +
			`<pubDate>` + time.Now().Add(-age).UTC().Format(time.RFC1123Z) + `</pubDate></item></channel></rss>`)
	}

	ref := m.partsManifest(t, s, "from = \"sparkle\"\nrepo = \""+s.URL+"/feed\"", "{{version}}")

	feed(2 * time.Hour)

	if _, err := m.run(t, "", "add", ref); err == nil || !strings.Contains(err.Error(), "the first to pass") {
		t.Fatalf("a Sparkle item 2 hours old passed the age: %v", err)
	}

	feed(10 * 24 * time.Hour)

	out, err := m.run(t, "", "add", ref)
	if err != nil || strings.Contains(out, "gives no release time") {
		t.Fatalf("a Sparkle item 10 days old: %v\n%s", err, out)
	}
}
