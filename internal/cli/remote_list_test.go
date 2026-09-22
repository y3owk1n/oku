package cli_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// machineRepo serves github:me/machines at one commit, with files at the paths
// of files, and records every path it was asked for.
func machineRepo(t *testing.T, m *machine, commit string, files map[string]string) *[]string {
	t.Helper()

	var asked []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)

		if r.URL.Path == "/api/repos/me/machines/commits/HEAD" {
			_, _ = w.Write([]byte(commit))

			return
		}

		at, ok := strings.CutPrefix(r.URL.Path, "/raw/me/machines/"+commit+"/")
		if body, found := files[at]; ok && found {
			_, _ = w.Write([]byte(body))

			return
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	return &asked
}

func TestB252ARelativePathInARemoteListNamesAFileOfTheSameRepo(t *testing.T) {
	const commit = "5555555555555555555555555555555555555555"

	m := newMachine(t)

	read := func(path string) string {
		data, err := os.ReadFile(path)
		must(t, err)

		return string(data)
	}

	lib := read(m.namedManifest(t, "lib", "lib", "lib"))
	tool := read(m.namedManifest(t, "tool", "tool", "tool")) +
		"[runtime]\ndeps = [\"./lib.toml\"]\n"

	asked := machineRepo(t, &m, commit, map[string]string{
		"oku.toml":           "include = [\"./lists/base.toml\"]\n",
		"lists/base.toml":    "[packages]\ntool = \"../packages/tool.toml\"\n",
		"packages/tool.toml": tool,
		"packages/lib.toml":  lib,
	})

	out, err := m.run(t, "", "sync", "github:me/machines")
	if err != nil {
		t.Fatalf("sync of a repo laid out with relative paths: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "tool")) {
		t.Fatalf("tool is not in the profile:\n%s", out)
	}

	lock := read(filepath.Join(m.config, "oku.lock"))
	for _, want := range []string{
		"github:me/machines#lists/base.toml", "github:me/machines#packages/tool.toml",
		"github:me/machines#packages/lib.toml",
	} {
		if !strings.Contains(lock, want) {
			t.Fatalf("oku.lock lacks %s:\n%s", want, lock)
		}
	}

	// Every file came from the one commit of the list.
	for _, path := range *asked {
		if strings.HasPrefix(path, "/raw/") && !strings.Contains(path, commit) {
			t.Fatalf("oku read %s, which is not at the list's commit", path)
		}
	}
}

func TestB252ARelativePathCannotLeaveTheRepo(t *testing.T) {
	m := newMachine(t)

	machineRepo(t, &m, "6666666666666666666666666666666666666666", map[string]string{
		"oku.toml": "[packages]\ntool = \"../elsewhere/tool.toml\"\n",
	})

	_, err := m.run(t, "", "sync", "github:me/machines")
	if err == nil || !strings.Contains(err.Error(), "is outside the repository") {
		t.Fatalf("want a path that leaves the repo refused, got %v", err)
	}
}

func TestB252ARelativePathInAListAtAURLNamesTheURLBesideIt(t *testing.T) {
	m := newMachine(t)

	tool, err := os.ReadFile(m.namedManifest(t, "tool", "tool", "tool"))
	must(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/machines/oku.toml":
			_, _ = w.Write([]byte("[packages]\ntool = \"./packages/tool.toml\"\n"))
		case "/machines/packages/tool.toml":
			_, _ = w.Write(tool)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	out, err := m.run(t, "", "sync", server.URL+"/machines/oku.toml")
	if err != nil {
		t.Fatalf("sync of a list at a URL with a relative path: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "tool")) {
		t.Fatalf("tool is not in the profile:\n%s", out)
	}
}
