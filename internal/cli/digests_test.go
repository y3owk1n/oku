package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// digestServer fakes GitHub for owner/tool, whose one release v1.0.0 lists its
// file with the digest that the API reports for it.
func digestServer(t *testing.T, m *machine, digest string) string {
	t.Helper()

	archive, sum := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho 1.0.0\n"})
	if digest == "" {
		digest = sum
	}

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/releases":
			fmt.Fprintf(w, `[{"tag_name": "v1.0.0", "assets": [{"name": "tool.tar.gz", `+
				`"browser_download_url": %q, "digest": "sha256:%s"}]}]`,
				server.URL+"/owner/tool/releases/download/v1.0.0/tool.tar.gz", digest)
		case "/owner/tool/releases/download/v1.0.0/tool.tar.gz":
			http.ServeFile(w, r, archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"%s/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n",
		server.URL,
	)), 0o644))

	return path
}

func TestB268AReleaseFileIsCheckedAgainstTheDigestGitHubReports(t *testing.T) {
	m := newMachine(t)
	tool := digestServer(t, &m, "")

	out, err := m.run(t, "", "add", tool)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if strings.Contains(out, "trusted this download") {
		t.Fatalf("oku trusted a file whose digest GitHub reports:\n%s", out)
	}

	tampered := newMachine(t)

	_, err = tampered.run(t, "", "add", digestServer(t, &tampered, strings.Repeat("0", 64)))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want a file that differs from GitHub's digest refused, got %v", err)
	}
}

func TestB268LintDoesNotWarnWhenGitHubOrCratesIOReportTheDigest(t *testing.T) {
	m := newMachine(t)

	lint := func(name, text string) string {
		t.Helper()

		path := filepath.Join(m.fixtures, name+".toml")
		must(t, os.WriteFile(path, []byte("[package]\nname = \""+name+"\"\n"+text), 0o644))

		out, err := m.run(t, "", "manifest", "lint", path)
		must(t, err)

		return out
	}

	release := "[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\n"
	crate := "[version]\nfrom = \"crates\"\nrepo = \"tool\"\n[build]\n" +
		"[[build.step]]\ninstall = { bin = [\"tool\"] }\n"

	for name, text := range map[string]string{
		"own": release + "[[artifact]]\nurl = \"https://github.com/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\n" +
			"bin = [\"tool\"]\n",
		"crate": strings.Replace(crate, "[build]\n",
			"[build]\nsource = { url = \"https://static.crates.io/crates/tool/tool-{{version}}.crate\", strip = 1 }\n", 1),
	} {
		if out := lint(name, text); strings.Contains(out, "trust the first download") {
			t.Fatalf("lint warned about %s, whose host reports the digest:\n%s", name, out)
		}
	}

	out := lint("elsewhere", release+"[[artifact]]\nurl = \"https://example.com/tool.tar.gz\"\nbin = [\"tool\"]\n")
	if !strings.Contains(out, "users trust the first download") {
		t.Fatalf("lint did not warn about a file that no host reports a digest for:\n%s", out)
	}
}
