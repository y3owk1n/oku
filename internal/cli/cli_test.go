package cli_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/cli"
	"github.com/y3owk1n/oku/internal/platform"
)

const script = "#!/bin/sh\necho hello from tool\n"

// machine is a throwaway set of oku directories plus a stand-in oku binary.
type machine struct {
	config, data, cache, exe, fixtures string
	opts                               cli.Options
}

func newMachine(t *testing.T) machine {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("step 1 profiles use symlinks, Windows shims come in step 9")
	}

	root := t.TempDir()
	m := machine{
		config:   filepath.Join(root, "config", "oku"),
		data:     filepath.Join(root, "data", "oku"),
		cache:    filepath.Join(root, "cache", "oku"),
		exe:      filepath.Join(root, "oku-binary"),
		fixtures: filepath.Join(root, "fixtures"),
	}

	m.opts = cli.Options{Version: "test", Executable: m.exe}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	must(t, os.MkdirAll(m.fixtures, 0o755))
	must(t, os.WriteFile(m.exe, []byte("binary"), 0o755))

	return m
}

func (m machine) run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	cmd := cli.NewRootCmd(m.opts)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()

	return out.String(), err
}

// manifest writes an archive holding files and a manifest that points at it.
// artifact is the TOML after url and sha256, such as `bin = ["tool"]`.
func (m machine) manifest(
	t *testing.T,
	name string,
	files map[string]string,
	artifact string,
) string {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for path, body := range files {
		must(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0o755, Size: int64(len(body))}))

		_, err := tw.Write([]byte(body))
		must(t, err)
	}

	must(t, tw.Close())
	must(t, gz.Close())

	archive := filepath.Join(m.fixtures, name+".tar.gz")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	sum := sha256.Sum256(buf.Bytes())

	return m.rawManifest(t, name, fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256 = \"%s\"\n%s\n",
		archive, hex.EncodeToString(sum[:]), artifact,
	))
}

func (m machine) rawManifest(t *testing.T, name, artifacts string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, name+".toml")
	body := fmt.Sprintf("[package]\nname = %q\n[version]\nvalue = \"1.2.3\"\n%s", name, artifacts)
	must(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func (m machine) profile(elem ...string) string {
	return filepath.Join(append([]string{m.data, "profiles", "global", "current"}, elem...)...)
}

func (m machine) storeEntries(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(m.data, "store"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func TestB1AddedBinRunsFromProfile(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	out, err := exec.Command(m.profile("bin", "tool")).Output()
	must(t, err)

	if string(out) != "hello from tool\n" {
		t.Fatalf("tool printed %q", out)
	}
}

func TestB2ChecksumMismatchIsRejected(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	body, err := os.ReadFile(ref)
	must(t, err)

	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "sha256") {
			lines[i] = fmt.Sprintf("sha256 = %q", strings.Repeat("0", 64))
		}
	}

	must(t, os.WriteFile(ref, []byte(strings.Join(lines, "\n")), 0o644))

	out, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v\n%s", err, out)
	}

	if got := m.storeEntries(t); len(got) != 0 {
		t.Fatalf("store holds %v", got)
	}

	if exists(m.profile()) {
		t.Fatal("a profile generation was activated")
	}
}

func TestB3NoMatchingArtifactNamesHostPlatform(t *testing.T) {
	m := newMachine(t)
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nmatch = { os = \"plan9\" }\nurl = \"file:///nowhere\"\nsha256 = %q\nbin = [\"tool\"]\n",
		strings.Repeat("0", 64),
	))

	_, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), platform.Host().String()) {
		t.Fatalf("want an error naming %s, got %v", platform.Host(), err)
	}
}

func TestB4RemoveDropsPackageAndKeepsStorePath(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	_, err = m.run(t, "", "remove", "tool")
	must(t, err)

	if exists(m.profile("bin", "tool")) {
		t.Fatal("tool is still in the profile")
	}

	if got := m.storeEntries(t); len(got) != 1 {
		t.Fatalf("store holds %v, want the one removed package", got)
	}

	if _, err := m.run(t, "", "remove", "tool"); err == nil {
		t.Fatal("removing a package twice succeeded")
	}
}

func TestB5ListShowsNameVersionRef(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	out, err := m.run(t, "", "list")
	must(t, err)

	for _, want := range []string{"tool", "1.2.3", ref} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output lacks %q:\n%s", want, out)
		}
	}
}

func TestB6FailedInstallLeavesProfileUnchanged(t *testing.T) {
	m := newMachine(t)
	good := m.manifest(t, "good", map[string]string{"good": script}, `bin = ["good"]`)
	broken := m.manifest(t, "broken", map[string]string{"other": script}, `bin = ["missing"]`)

	_, err := m.run(t, "", "add", good)
	must(t, err)

	before, err := os.Readlink(m.profile())
	must(t, err)

	if _, err := m.run(t, "", "add", broken); err == nil {
		t.Fatal("adding a package with a missing bin succeeded")
	}

	after, err := os.Readlink(m.profile())
	must(t, err)

	if before != after {
		t.Fatalf("active generation moved from %s to %s", before, after)
	}

	out, err := m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "good") || strings.Contains(out, "broken") {
		t.Fatalf("list after the failed install:\n%s", out)
	}
}

func TestB7ManAndCompletionsAppearUnderProfileShare(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(
		t, "tool",
		map[string]string{"tool": script, "doc/tool.1": ".TH", "comp/tool.fish": "complete"},
		"bin = [\"tool\"]\nman = [\"doc/tool.1\"]\ncompletions = { fish = \"comp/tool.fish\" }",
	)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	for _, path := range []string{
		m.profile("share", "man", "man1", "tool.1"),
		m.profile("share", "completions", "fish", "tool.fish"),
	} {
		if !exists(path) {
			t.Fatalf("%s is missing", path)
		}
	}
}

func TestB8WritesOnlyUnderOkuDirectories(t *testing.T) {
	m := newMachine(t)
	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	allowed := []string{
		filepath.Join(home, ".config", "oku"),
		filepath.Join(home, ".local", "share", "oku"),
		filepath.Join(home, ".cache", "oku"),
	}

	must(t, filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		for _, dir := range allowed {
			if strings.HasPrefix(path, dir+string(filepath.Separator)) {
				return nil
			}
		}

		t.Errorf("oku wrote %s", path)

		return nil
	}))
}

func TestB9SameBinNameFailsNamingBothPackages(t *testing.T) {
	m := newMachine(t)
	first := m.manifest(t, "first", map[string]string{"tool": script}, `bin = ["tool"]`)
	second := m.manifest(t, "second", map[string]string{"tool": script + "#2\n"}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", first)
	must(t, err)

	_, err = m.run(t, "", "add", second)
	if err == nil || !strings.Contains(err.Error(), "first") ||
		!strings.Contains(err.Error(), "second") {
		t.Fatalf("want an error naming first and second, got %v", err)
	}
}

func TestB94SelfUninstallRemovesEverythingAfterOneQuestion(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if _, err := m.run(t, "n\n", "self", "uninstall"); err == nil {
		t.Fatal("answering no did not stop the uninstall")
	}

	if !exists(m.data) || !exists(m.exe) {
		t.Fatal("answering no removed something")
	}

	out, err := m.run(t, "y\n", "self", "uninstall")
	must(t, err)

	if strings.Count(out, "[y/N]") != 1 {
		t.Fatalf("want one question:\n%s", out)
	}

	for _, path := range []string{m.data, m.cache, m.config, m.exe} {
		if exists(path) {
			t.Fatalf("%s still exists after the uninstall", path)
		}
	}
}

func TestB96KeepListKeepsGlobalList(t *testing.T) {
	m := newMachine(t)
	list := filepath.Join(m.config, "oku.toml")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(list, []byte("[packages]\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"), nil, 0o644))

	out, err := m.run(t, "", "self", "uninstall", "--keep-list", "--yes")
	must(t, err)

	if !exists(list) {
		t.Fatal("oku.toml was removed")
	}

	if exists(filepath.Join(m.config, "config.toml")) {
		t.Fatal("config.toml was kept")
	}

	if !strings.Contains(out, list) {
		t.Fatalf("output does not say where the list is:\n%s", out)
	}
}

func TestB102UninstallPrintsPathEntryToRemove(t *testing.T) {
	m := newMachine(t)
	bin := m.profile("bin")

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if !strings.Contains(out, "remove "+bin+" from PATH") {
		t.Fatalf("output does not name the PATH entry:\n%s", out)
	}
}

func TestB10AddAcceptsEveryRefKind(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"

	m := newMachine(t)
	manifestPath := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	body, err := os.ReadFile(manifestPath)
	must(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain/tool.toml",
			"/raw/owner/repo/" + commit + "/oku.pkg.toml",
			"/raw/owner/recipes/" + commit + "/packages/tool.toml":
			_, _ = w.Write(body)
		case "/api/repos/owner/repo/commits/HEAD", "/api/repos/owner/recipes/commits/HEAD":
			_, _ = w.Write([]byte(commit))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	refs := []string{
		manifestPath,
		server.URL + "/plain/tool.toml",
		"github:owner/repo",
		"github:owner/recipes#tool",
	}

	if _, err := exec.LookPath("git"); err == nil {
		repo := filepath.Join(m.fixtures, "repo")
		must(t, os.MkdirAll(filepath.Join(repo, "recipes"), 0o755))
		must(t, os.WriteFile(filepath.Join(repo, "recipes", "tool.toml"), body, 0o644))

		for _, args := range [][]string{
			{"init", "--quiet"},
			{"add", "."},
			{
				"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
				"commit", "--quiet", "-m", "add",
			},
		} {
			cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)

			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}

		refs = append(refs, "git+file://"+repo+"#recipes/tool.toml")
	}

	for _, ref := range refs {
		if out, err := m.run(t, "", "add", ref); err != nil {
			t.Fatalf("add %s: %v\n%s", ref, err, out)
		}

		out, err := m.run(t, "", "list")
		must(t, err)

		if !strings.Contains(out, strings.TrimPrefix(ref, m.fixtures)) {
			t.Fatalf("list does not show %s:\n%s", ref, out)
		}

		_, err = m.run(t, "", "remove", "tool")
		must(t, err)
	}

	if _, err := m.run(t, "", "add", "github:owner/missing"); err == nil {
		t.Fatal("adding a repo that does not exist succeeded")
	}
}
