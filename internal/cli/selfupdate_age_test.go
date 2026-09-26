package cli_test

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
)

// agedReleases fakes GitHub for owner/tool with a signed oku release per tag,
// published that long ago, and trusts the key that signed them.
func (m *machine) agedReleases(t *testing.T, ages map[string]time.Duration) {
	t.Helper()

	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	name := "oku-" + runtime.GOOS + "-" + runtime.GOARCH
	releases := map[string]string{}

	var list []string

	for tag, age := range ages {
		dir := filepath.Join(m.fixtures, tag)
		must(t, os.MkdirAll(dir, 0o755))

		binary := filepath.Join(dir, name)
		must(t, os.WriteFile(binary, []byte("oku "+tag), 0o755))

		reader := minisign.NewReader(strings.NewReader("oku " + tag))
		_, err := io.Copy(io.Discard, reader)
		must(t, err)
		must(t, os.WriteFile(binary+".minisig", reader.SignWithComments(secret, "oku "+tag, ""), 0o644))

		releases[tag] = fmt.Sprintf(
			`{"tag_name": %q, "published_at": %q, "assets": [`+
				`{"name": %q, "browser_download_url": "file://%s"}, `+
				`{"name": %q, "browser_download_url": "file://%s.minisig"}]}`,
			tag, time.Now().Add(-age).UTC().Format(time.RFC3339), name, binary, name+".minisig", binary,
		)
		list = append(list, releases[tag])
	}

	newest := ""
	for tag := range ages {
		if newest == "" || ages[tag] < ages[newest] {
			newest = tag
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/repos/owner/tool")

		switch {
		case path == "/releases/latest":
			fmt.Fprint(w, releases[newest])
		case path == "/releases":
			fmt.Fprintf(w, "[%s]", strings.Join(list, ","))
		case releases[strings.TrimPrefix(path, "/releases/tags/")] != "":
			fmt.Fprint(w, releases[strings.TrimPrefix(path, "/releases/tags/")])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.ReleaseRepo = "owner/tool"
	m.opts.ReleaseKey = public.String()
}

func TestB374SelfUpdateTakesTheNewestReleaseOlderThanTheMinimumReleaseAge(t *testing.T) {
	m := newMachine(t)
	m.opts.Version = "1.2.0"
	m.agedReleases(t, map[string]time.Duration{"v1.3.0": 10 * 24 * time.Hour, "v1.4.0": 2 * time.Hour})

	binary := func() string {
		data, err := os.ReadFile(m.exe)
		must(t, err)

		return string(data)
	}

	out, err := m.run(t, "", "self", "update", "--check")
	must(t, err)

	if !strings.Contains(out, "1.4.0 came out less than 1d ago") ||
		!strings.Contains(out, "1.3.0 is available") {
		t.Fatalf("--check does not say that 1.4.0 waits and 1.3.0 is available:\n%s", out)
	}

	out, err = m.run(t, "", "self", "update")
	must(t, err)

	if binary() != "oku v1.3.0" {
		t.Fatalf("self update took %q, want v1.3.0:\n%s", binary(), out)
	}

	// On the newest release that is old enough, nothing changes.
	m.opts.Version = "1.3.0"

	out, err = m.run(t, "", "self", "update")
	must(t, err)

	if binary() != "oku v1.3.0" || !strings.Contains(out, "waits until") {
		t.Fatalf("self update on 1.3.0 took %q:\n%s", binary(), out)
	}

	out, err = m.run(t, "", "self", "update", "--min-release-age", "0")
	must(t, err)

	if binary() != "oku v1.4.0" {
		t.Fatalf("--min-release-age 0 took %q:\n%s", binary(), out)
	}
}

func TestB374SelfUpdateToATagSkipsTheMinimumReleaseAge(t *testing.T) {
	m := newMachine(t)
	m.opts.Version = "1.2.0"
	m.agedReleases(t, map[string]time.Duration{"v1.3.0": 10 * 24 * time.Hour, "v1.4.0": 2 * time.Hour})

	_, err := m.run(t, "", "self", "update", "--to", "v1.4.0")
	must(t, err)

	if data, _ := os.ReadFile(m.exe); string(data) != "oku v1.4.0" {
		t.Fatalf("self update --to v1.4.0 took %q", data)
	}
}

func TestB374SelfUpdateFollowsTheListsMinimumReleaseAge(t *testing.T) {
	m := newMachine(t)
	m.opts.Version = "1.2.0"
	m.agedReleases(t, map[string]time.Duration{"v1.3.0": 10 * 24 * time.Hour, "v1.4.0": 2 * time.Hour})
	m.writeFilesList(t, "[lock]\nmin_release_age = \"0\"\n")

	_, err := m.run(t, "", "self", "update")
	must(t, err)

	if data, _ := os.ReadFile(m.exe); string(data) != "oku v1.4.0" {
		t.Fatalf("with min_release_age 0 self update took %q", data)
	}
}
