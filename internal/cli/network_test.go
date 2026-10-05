package cli_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const anySHA = "0000000000000000000000000000000000000000000000000000000000000000"

func TestB467ADownloadFromAPrivateAddressIsRefused(t *testing.T) {
	m := newMachine(t)

	ref := m.rawManifest(t, "tool", "[[artifact]]\nurl = \"https://169.254.169.254/tool.tar.gz\"\n"+
		"sha256 = \""+anySHA+"\"\nbin = [\"tool\"]\n")

	if out, err := m.run(t, "", "add", ref); err == nil || !strings.Contains(err.Error(), "a private address") {
		t.Fatalf("want the metadata address refused, got %v:\n%s", err, out)
	}
}

func TestB468AllowListsTheOnlyHostsOkuConnectsTo(t *testing.T) {
	m := newMachine(t)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte("[network]\nallow = [\"*.example.com\"]\n"), 0o644))

	ref := m.rawManifest(t, "tool", "[[artifact]]\nurl = \"https://downloads.example.org/tool.tar.gz\"\n"+
		"sha256 = \""+anySHA+"\"\nbin = [\"tool\"]\n")

	out, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "downloads.example.org is not in [network] allow") {
		t.Fatalf("want the unlisted host refused, got %v:\n%s", err, out)
	}

	// A URL of this machine is one the user named, so it works with any list.
	if out, err := m.run(
		t,
		"",
		"add",
		m.manifest(t, "local", map[string]string{"local": script}, `bin = ["local"]`),
	); err != nil {
		t.Fatalf("a local manifest should install: %v\n%s", err, out)
	}
}

func TestB469GitIsNotRunForAHostThePolicyRefuses(t *testing.T) {
	m := newMachine(t)

	out, err := m.run(t, "", "add", "git+https://10.1.2.3/owner/repo")
	if err == nil || !strings.Contains(err.Error(), "10.1.2.3 resolves to 10.1.2.3, a private address") {
		t.Fatalf("want git refused before it runs, got %v:\n%s", err, out)
	}
}

func TestB470RedirectVersionFollowsNoRedirectToAFile(t *testing.T) {
	m := newMachine(t)

	// Only the version page redirects, so a refusal names the version lookup
	// and not the download.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)

			return
		}

		http.Redirect(w, r, "file:///dl/1.2.3/tool.tar.gz", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	ref := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.MkdirAll(m.fixtures, 0o755))
	must(t, os.WriteFile(ref, []byte(
		"[package]\nname = \"tool\"\n[version]\nfrom = \"redirect\"\nrepo = \""+server.URL+"/latest\"\n"+
			"regex = '/dl/([\\d.]+)/'\n[[artifact]]\nurl = \""+server.URL+"/dl/{{version}}/tool.tar.gz\"\n"+
			"bin = [\"tool\"]\n",
	), 0o644))

	out, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "read the version from") ||
		!strings.Contains(err.Error(), "redirects to file:///dl/1.2.3/tool.tar.gz, which oku does not follow") {
		t.Fatalf("want the redirect to a file refused, got %v:\n%s", err, out)
	}
}
