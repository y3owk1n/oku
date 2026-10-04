package cli_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestB419ADownloadDoesNotFollowARedirectToALocalFile(t *testing.T) {
	m := newMachine(t)

	secret := filepath.Join(m.fixtures, "secret.txt")
	must(t, os.MkdirAll(m.fixtures, 0o755))
	must(t, os.WriteFile(secret, []byte("hunter2"), 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file://"+secret, http.StatusFound)
	}))
	t.Cleanup(server.Close)

	ref := m.rawManifest(t, "tool", "[[artifact]]\nurl = \""+server.URL+"/tool\"\nbin = [\"tool\"]\n")

	if out, err := m.run(t, "", "add", ref); err == nil || !strings.Contains(err.Error(), "which oku does not follow") {
		t.Fatalf("want the redirect to a file refused, got %v:\n%s", err, out)
	}
}

func TestB420OnlyAManifestOnThisMachineMayNameAFileURL(t *testing.T) {
	m := newMachine(t)
	m.opts.FileDownloads = false

	local := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	body, err := os.ReadFile(local)
	must(t, err)

	// A build that clones a repo of this machine, such as a password store.
	cloning := "[package]\nname = \"tool\"\n[version]\nvalue = \"1.0.0\"\n[build]\n" +
		"source = { git = \"" + m.fixtures + "\" }\n[[build.step]]\ninstall = { bin = [\"tool\"] }\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/clone.toml") {
			_, _ = w.Write([]byte(cloning))

			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	if out, err := m.run(t, "", "add", server.URL+"/tool.toml"); err == nil ||
		!strings.Contains(err.Error(), "only a manifest on this machine may read a local file") {
		t.Fatalf("want a served manifest with a file:// url refused, got %v:\n%s", err, out)
	}

	if out, err := m.run(t, "", "add", server.URL+"/clone.toml"); err == nil ||
		!strings.Contains(err.Error(), "only a manifest on this machine may name a local repo") {
		t.Fatalf("want a served manifest that clones a local repo refused, got %v:\n%s", err, out)
	}

	if out, err := m.run(t, "", "add", local); err != nil {
		t.Fatalf("a manifest file on this machine should read its file:// url: %v\n%s", err, out)
	}
}

func TestB538OnlyAManifestOnThisMachineMayNameAURLOfThisMachine(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"

	m := newMachine(t)
	m.opts.FileDownloads = false

	archive, _ := m.archive(t, "tool", map[string]string{"tool": script})
	data, err := os.ReadFile(archive)
	must(t, err)

	var server *httptest.Server

	manifest := func(version string) string {
		return "[package]\nname = \"tool\"\n" + version +
			"[[artifact]]\nurl = \"" + server.URL + "/tool.tar.gz\"\nbin = [\"tool\"]\n"
	}

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/commits/HEAD", "/api/repos/owner/page/commits/HEAD":
			_, _ = w.Write([]byte(commit))
		case "/raw/owner/tool/" + commit + "/oku.pkg.toml", "/tool.toml":
			_, _ = w.Write([]byte(manifest("[version]\nvalue = \"1.0.0\"\n")))
		case "/raw/owner/page/" + commit + "/oku.pkg.toml":
			// A version read from a local service could carry its answer away.
			_, _ = w.Write([]byte(manifest(
				"[version]\nfrom = \"page\"\nrepo = \"" + server.URL + "/admin\"\nregex = '([0-9.]+)'\n",
			)))
		case "/tool.tar.gz":
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	for _, ref := range []string{"github:owner/tool", "github:owner/page"} {
		if out, err := m.run(t, "", "add", ref); err == nil ||
			!strings.Contains(err.Error(), "only a manifest on this machine may name a URL of this machine") {
			t.Fatalf("want %s refused, got %v:\n%s", ref, err, out)
		}
	}

	// A manifest that this machine serves may name its own URLs.
	if out, err := m.run(t, "", "add", server.URL+"/tool.toml"); err != nil {
		t.Fatalf("a manifest served from this machine should download from it: %v\n%s", err, out)
	}
}

func TestB421APlainHTTPRefToAnotherMachineIsRefused(t *testing.T) {
	m := newMachine(t)

	for _, r := range []string{"http://example.com/tool.toml", "git+http://example.com/repo"} {
		if _, err := m.run(t, "", "add", r); err == nil || !strings.Contains(err.Error(), "http:// is not encrypted") {
			t.Fatalf("want %s refused, got %v", r, err)
		}
	}
}

func TestB422LintRefusesAnHTTPDownloadWithoutASHA256(t *testing.T) {
	m := newMachine(t)
	path := m.rawManifest(t, "tool", "[[artifact]]\nurl = \"http://example.com/tool.tar.gz\"\nbin = [\"tool\"]\n")

	out, err := m.run(t, "", "manifest", "lint", path)
	if err == nil || !strings.Contains(out+err.Error(), "an http:// url needs sha256") {
		t.Fatalf("want lint to refuse the http:// download, got %v:\n%s", err, out)
	}
}

func TestB423AGitCollectionCannotReadAFileOutsideTheRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}

	m := newMachine(t)

	outside := filepath.Join(m.fixtures, "outside.toml")
	must(t, os.MkdirAll(m.fixtures, 0o755))
	must(t, os.WriteFile(outside, []byte("[package]\nname = \"leaked\"\ndescription = \"SECRET\"\n"+
		"[version]\nvalue = \"1.0.0\"\n[[artifact]]\nurl = \"https://example.com/leaked\"\nbin = [\"leaked\"]\n"), 0o644))

	repo := filepath.Join(m.fixtures, "recipes")
	must(t, os.MkdirAll(filepath.Join(repo, "packages"), 0o755))
	must(t, os.Symlink(outside, filepath.Join(repo, "packages", "leaked.toml")))

	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "x"},
	} {
		must(t, exec.Command("git", append([]string{"-C", repo}, args...)...).Run())
	}

	_, err := m.run(t, "", "source", "add", "core", "git+file://"+repo)
	must(t, err)

	if out, _ := m.run(t, "", "search", "leaked"); strings.Contains(out, "SECRET") {
		t.Fatalf("search read a file outside the repo through a link:\n%s", out)
	}
}
