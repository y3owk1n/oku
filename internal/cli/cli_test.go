package cli_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"aead.dev/minisign"
	"github.com/y3owk1n/oku/internal/cli"
	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/service"
	"github.com/y3owk1n/oku/internal/shellhook"
)

const script = "#!/bin/sh\necho hello from tool\n"

// machine is a throwaway set of oku directories plus a stand-in oku binary.
type machine struct {
	config, data, cache, exe, fixtures string
	opts                               cli.Options
	services                           *fakeServices
}

// fakeServices stands in for launchd or systemd and remembers what oku asked.
type fakeServices struct {
	state map[string]*fakeService
	// failInstall names the services whose Install fails.
	failInstall map[string]bool
	// dieInInstall makes Install panic, which stands in for a killed process.
	dieInInstall bool
}

type fakeService struct {
	def              service.Definition
	enabled, running bool
}

func (f *fakeServices) Install(_ context.Context, d service.Definition, enabled bool) error {
	if f.dieInInstall {
		panic("killed")
	}

	if f.failInstall[d.Name] {
		return fmt.Errorf("the service manager refused %s", d.Name)
	}

	f.state[d.Name] = &fakeService{def: d, enabled: enabled, running: enabled}

	return nil
}

func (f *fakeServices) Remove(_ context.Context, d service.Definition) error {
	delete(f.state, d.Name)

	return nil
}

func (f *fakeServices) Start(_ context.Context, d service.Definition) error {
	f.state[d.Name].running = true

	return nil
}

func (f *fakeServices) Stop(_ context.Context, d service.Definition) error {
	f.state[d.Name].running = false

	return nil
}

func (f *fakeServices) Status(_ context.Context, d service.Definition) (service.Status, error) {
	s, ok := f.state[d.Name]
	if !ok {
		return service.Status{}, nil
	}

	return service.Status{Installed: true, Enabled: s.enabled, Running: s.running}, nil
}

func (f *fakeServices) Logs(context.Context, service.Definition, int) (string, error) {
	return "listening on 8080", nil
}

func (f *fakeServices) File(d service.Definition) string {
	return "/fake/services/" + d.Name
}

// TestMain makes the test binary run as oku when the build sandbox starts it. On
// Linux the sandbox runs the current binary again as "oku sandbox-init", and in a
// test that binary is this one.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.InitCommand {
		cmd := cli.NewRootCmd(cli.Options{})
		cmd.SetArgs(os.Args[1:])

		if err := cmd.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, "oku:", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
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

	// WorkDir keeps a stray oku.toml above the repo from turning tests into
	// project runs.
	m.opts = cli.Options{Version: "test", Executable: m.exe, WorkDir: m.fixtures}

	// A real service manager would load agents into the login session of whoever
	// runs the tests.
	m.services = &fakeServices{state: map[string]*fakeService{}}
	m.opts.Services = m.services

	// oku places apps and fonts under HOME, so tests get their own.
	t.Setenv("HOME", filepath.Join(root, "home"))
	must(t, os.MkdirAll(filepath.Join(root, "home"), 0o755))
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

// archive writes a tar.gz holding files and returns its path and sha256.
func (m machine) archive(t *testing.T, name string, files map[string]string) (string, string) {
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

	path := filepath.Join(m.fixtures, name+".tar.gz")
	must(t, os.WriteFile(path, buf.Bytes(), 0o644))

	sum := sha256.Sum256(buf.Bytes())

	return path, hex.EncodeToString(sum[:])
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

	archive, sum := m.archive(t, name, files)

	return m.rawManifest(t, name, fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256 = \"%s\"\n%s\n", archive, sum, artifact,
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

func TestB11AddWritesListAndLock(t *testing.T) {
	m := newMachine(t)
	listPath := filepath.Join(m.config, "oku.toml")
	lockPath := filepath.Join(m.config, "oku.lock")

	must(t, os.MkdirAll(m.config, 0o755))
	must(
		t,
		os.WriteFile(
			listPath,
			[]byte("# my tools\n[packages]\nother = \"./other.toml\" # keep\n"),
			0o644,
		),
	)

	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	listed, err := os.ReadFile(listPath)
	must(t, err)

	for _, want := range []string{"# my tools", `other = "./other.toml" # keep`, fmt.Sprintf("tool = %q", ref)} {
		if !strings.Contains(string(listed), want) {
			t.Fatalf("oku.toml lacks %q:\n%s", want, listed)
		}
	}

	locked, err := os.ReadFile(lockPath)
	must(t, err)

	for _, want := range []string{"name = 'tool'", "version = '1.2.3'", "manifest_sha256", platform.Host().String(), "sha256 = '"} {
		if !strings.Contains(string(locked), want) {
			t.Fatalf("oku.lock lacks %q:\n%s", want, locked)
		}
	}

	_, err = m.run(t, "", "remove", "tool")
	must(t, err)

	listed, err = os.ReadFile(listPath)
	must(t, err)

	locked, err = os.ReadFile(lockPath)
	must(t, err)

	if strings.Contains(string(listed), "tool =") || strings.Contains(string(locked), "'tool'") {
		t.Fatalf("remove left tool behind:\n%s\n%s", listed, locked)
	}

	if !strings.Contains(string(listed), "# my tools") {
		t.Fatalf("remove dropped the user's comment:\n%s", listed)
	}
}

func TestB14FirstUseChecksumIsPinnedAndEnforced(t *testing.T) {
	m := newMachine(t)
	archive, sum := m.archive(t, "tool", map[string]string{"tool": script})
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nbin = [\"tool\"]\n", archive,
	))

	out, err := m.run(t, "", "add", ref)
	must(t, err)

	if !strings.Contains(out, sum) {
		t.Fatalf("add did not report the pinned checksum:\n%s", out)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), sum) {
		t.Fatalf("oku.lock lacks the checksum:\n%s", locked)
	}

	// The same URL now serves different bytes, on a machine with an empty store.
	m.archive(t, "tool", map[string]string{"tool": script + "# tampered\n"})
	must(t, os.RemoveAll(m.data))
	must(t, os.RemoveAll(m.cache))

	_, err = m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
}

func TestArtifactChecksumComesFromSHA256URL(t *testing.T) {
	m := newMachine(t)
	archive, sum := m.archive(t, "tool", map[string]string{"tool": script})
	sums := filepath.Join(m.fixtures, "checksums.txt")

	must(t, os.WriteFile(sums, []byte(
		strings.Repeat("1", 64)+"  other.tar.gz\n"+sum+"  tool.tar.gz\n",
	), 0o644))

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256_url = \"file://%s\"\nbin = [\"tool\"]\n",
		archive, sums,
	))

	out, err := m.run(t, "", "add", ref)
	must(t, err)

	if strings.Contains(out, "trusted") {
		t.Fatalf("add fell back to first-use trust:\n%s", out)
	}

	must(t, os.WriteFile(sums, []byte(strings.Repeat("2", 64)+"  tool.tar.gz\n"), 0o644))
	must(t, os.RemoveAll(m.data))
	must(t, os.RemoveAll(m.cache))
	must(t, os.RemoveAll(m.config))

	if _, err := m.run(t, "", "add", ref); err == nil {
		t.Fatal("a download that differs from the published checksum was accepted")
	}
}

func TestB178SyncOfAnInstalledPackageReadsNoChecksums(t *testing.T) {
	m := newMachine(t)
	archive, sum := m.archive(t, "tool", map[string]string{"tool": script})
	sums := filepath.Join(m.fixtures, "checksums.txt")

	must(t, os.WriteFile(sums, []byte(sum+"  tool.tar.gz\n"), 0o644))

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nsha256_url = \"file://%s\"\nbin = [\"tool\"]\n",
		archive, sums,
	))

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	must(t, os.Remove(sums))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync read the checksums of a package the store holds: %v\n%s", err, out)
	}
}

func TestB178ParallelEnvLimitsHowManyPackagesInstallAtOnce(t *testing.T) {
	m := newMachine(t)
	one := m.manifest(t, "one", map[string]string{"one": script}, `bin = ["one"]`)
	two := m.manifest(t, "two", map[string]string{"two": script}, `bin = ["two"]`)

	t.Setenv("OKU_PARALLEL", "1")

	for _, ref := range []string{one, two} {
		_, err := m.run(t, "", "add", ref)
		must(t, err)
	}

	must(t, os.RemoveAll(m.data))

	out, err := m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "profile now holds 2 packages") {
		t.Fatalf("sync with one package at a time installed something else:\n%s", out)
	}

	t.Setenv("OKU_PARALLEL", "many")

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "OKU_PARALLEL") {
		t.Fatalf("want an error that names OKU_PARALLEL, got %v", err)
	}
}

func TestB12SyncMakesProfileMatchListAtLockedVersions(t *testing.T) {
	const first, second = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"

	m := newMachine(t)
	keep := m.manifest(t, "keep", map[string]string{"keep": script}, `bin = ["keep"]`)
	drop := m.manifest(t, "drop", map[string]string{"drop": script}, `bin = ["drop"]`)

	v1, err := os.ReadFile(
		m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`),
	)
	must(t, err)

	v2 := bytes.ReplaceAll(v1, []byte("1.2.3"), []byte("2.0.0"))
	head := first

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/repo/commits/HEAD":
			_, _ = w.Write([]byte(head))
		case "/raw/owner/repo/" + first + "/oku.pkg.toml":
			_, _ = w.Write(v1)
		case "/raw/owner/repo/" + second + "/oku.pkg.toml":
			_, _ = w.Write(v2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	for _, ref := range []string{keep, drop, "github:owner/repo"} {
		_, err := m.run(t, "", "add", ref)
		must(t, err)
	}

	// Upstream moves on, the user drops a package by hand, and the machine is new.
	head = second

	listPath := filepath.Join(m.config, "oku.toml")
	listed, err := os.ReadFile(listPath)
	must(t, err)

	var kept []string

	for _, line := range strings.Split(string(listed), "\n") {
		if !strings.HasPrefix(line, "drop =") {
			kept = append(kept, line)
		}
	}

	must(t, os.WriteFile(listPath, []byte(strings.Join(kept, "\n")), 0o644))
	must(t, os.RemoveAll(m.data))

	out, err := m.run(t, "", "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	out, err = m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "keep") || strings.Contains(out, "drop") {
		t.Fatalf("list after sync:\n%s", out)
	}

	if !strings.Contains(out, "1.2.3") || strings.Contains(out, "2.0.0") {
		t.Fatalf("sync did not use the locked version:\n%s", out)
	}

	if !exists(m.profile("bin", "tool")) || exists(m.profile("bin", "drop")) {
		t.Fatal("profile bin does not match the list")
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if strings.Contains(string(locked), "'drop'") {
		t.Fatalf("oku.lock still pins drop:\n%s", locked)
	}

	out, err = m.run(t, "", "update")
	must(t, err)

	if !strings.Contains(out, "1.2.3 -> 2.0.0") {
		t.Fatalf("update did not move tool to 2.0.0:\n%s", out)
	}
}

func TestB13SyncStopsOnChangedManifestUntilUpdate(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	body, err := os.ReadFile(ref)
	must(t, err)
	must(t, os.WriteFile(ref, append(body, []byte("\n# changed upstream\n")...), 0o644))

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "oku update tool") {
		t.Fatalf("want sync to stop and name oku update, got %v", err)
	}

	if _, err := m.run(t, "", "update", "tool"); err != nil {
		t.Fatalf("update: %v", err)
	}

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync after update: %v\n%s", err, out)
	}
}

func TestB15SyncAppendsMissingPlatformAndKeepsOthers(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	lockPath := filepath.Join(m.config, "oku.lock")
	host := platform.Host().String()

	locked, err := os.ReadFile(lockPath)
	must(t, err)

	// The lock now looks as if another machine wrote it.
	foreign := strings.ReplaceAll(string(locked), host, "plan9-mips")
	must(t, os.WriteFile(lockPath, []byte(foreign), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	locked, err = os.ReadFile(lockPath)
	must(t, err)

	for _, want := range []string{"platform.plan9-mips]", "platform." + host + "]"} {
		if !strings.Contains(string(locked), want) {
			t.Fatalf("oku.lock lacks %s:\n%s", want, locked)
		}
	}

	section := func(text, name string) string {
		_, after, _ := strings.Cut(text, "platform."+name+"]")
		before, _, _ := strings.Cut(after, "[")

		return before
	}

	if section(foreign, "plan9-mips") != section(string(locked), "plan9-mips") {
		t.Fatalf("sync rewrote the plan9-mips entry:\n%s", locked)
	}
}

// namedManifest writes a manifest for package name into file, shipping one
// executable called bin.
func (m machine) namedManifest(t *testing.T, file, name, bin string) string {
	t.Helper()

	archive, sum := m.archive(t, file, map[string]string{bin: script})
	path := filepath.Join(m.fixtures, file+".toml")

	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = %q\n[version]\nvalue = \"1.2.3\"\n"+
			"[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nbin = [%q]\n",
		name, archive, sum, bin,
	)), 0o644))

	return path
}

func TestB16IncludeMergesListsAndLocalEntryWins(t *testing.T) {
	m := newMachine(t)
	m.namedManifest(t, "extra", "extra", "extra")
	m.namedManifest(t, "shared-base", "shared", "shared-base")
	local := m.namedManifest(t, "shared-local", "shared", "shared-local")

	base := filepath.Join(m.fixtures, "base.toml")
	must(t, os.WriteFile(base, []byte(
		"[packages]\nextra = \"./extra.toml\"\nshared = \"./shared-base.toml\"\n",
	), 0o644))

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"include = [%q]\n\n[packages]\nshared = %q\n", base, local,
	)), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "extra")) || !exists(m.profile("bin", "shared-local")) ||
		exists(m.profile("bin", "shared-base")) {
		t.Fatal("profile does not hold extra plus the local shared")
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), "[[include]]") || !strings.Contains(string(locked), base) {
		t.Fatalf("oku.lock does not pin the include:\n%s", locked)
	}

	_, err = m.run(t, "", "remove", "extra")
	if err == nil || !strings.Contains(err.Error(), "include") {
		t.Fatalf("want remove to refuse an included package, got %v", err)
	}

	// A list on this machine is the user's own file, so sync reads it as it is.
	must(t, os.WriteFile(base, []byte("[packages]\nshared = \"./shared-base.toml\"\n"), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync after an edit of a local include: %v\n%s", err, out)
	}

	if exists(m.profile("bin", "extra")) {
		t.Fatal("extra left the include but stayed in the profile")
	}
}

func TestB16SyncStopsWhenAListFromAURLChanged(t *testing.T) {
	m := newMachine(t)

	server := httptest.NewServer(http.FileServer(http.Dir(m.fixtures)))
	t.Cleanup(server.Close)

	m.namedManifest(t, "extra", "extra", "extra")
	m.namedManifest(t, "other", "other", "other")

	base := filepath.Join(m.fixtures, "base.toml")
	must(
		t,
		os.WriteFile(base, []byte("[packages]\nextra = \""+server.URL+"/extra.toml\"\n"), 0o644),
	)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"),
		[]byte("include = [\""+server.URL+"/base.toml\"]\n"), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	must(
		t,
		os.WriteFile(base, []byte("[packages]\nother = \""+server.URL+"/other.toml\"\n"), 0o644),
	)

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "oku update") {
		t.Fatalf("want sync to stop on a changed list from a URL, got %v", err)
	}

	if !exists(m.profile("bin", "extra")) || exists(m.profile("bin", "other")) {
		t.Fatal("the refused sync changed the profile")
	}

	if out, err := m.run(t, "", "update"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if exists(m.profile("bin", "extra")) || !exists(m.profile("bin", "other")) {
		t.Fatal("update did not accept the changed list")
	}
}

func TestB17WhenSkipsOtherPlatformsAndKeepsTheirLockEntry(t *testing.T) {
	m := newMachine(t)
	here := m.namedManifest(t, "here", "here", "here")
	elsewhere := m.namedManifest(t, "elsewhere", "elsewhere", "elsewhere")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[packages]\nhere = { ref = %q, when = { os = %q } }\n"+
			"elsewhere = { ref = %q, when = { os = \"plan9\" } }\n",
		here, platform.Host().OS, elsewhere,
	)), 0o644))

	lockPath := filepath.Join(m.config, "oku.lock")
	must(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(
		"[[package]]\nname = 'elsewhere'\nref = '%s'\nmanifest_sha256 = 'abc'\nversion = '1.2.3'\n"+
			"[package.platform.plan9-mips]\nstrategy = 'artifact'\nurl = 'file:///x'\nsha256 = 'def'\n",
		elsewhere,
	)), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "here")) || exists(m.profile("bin", "elsewhere")) {
		t.Fatal("profile does not hold exactly the matching package")
	}

	locked, err := os.ReadFile(lockPath)
	must(t, err)

	for _, want := range []string{"'elsewhere'", "plan9-mips", "sha256 = 'def'", "'here'"} {
		if !strings.Contains(string(locked), want) {
			t.Fatalf("oku.lock lacks %s:\n%s", want, locked)
		}
	}
}

func TestB18SyncRefAdoptsListAndLockWithIdenticalStoreHashes(t *testing.T) {
	const commit = "3333333333333333333333333333333333333333"

	files := map[string][]byte{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/repos/me/machines/commits/HEAD" {
			_, _ = w.Write([]byte(commit))

			return
		}

		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(body)
	}))
	defer server.Close()

	setUp := func() machine {
		m := newMachine(t)
		m.opts.GitHubAPI = server.URL + "/api"
		m.opts.GitHubRaw = server.URL + "/raw"

		return m
	}

	// The publisher installs a package, then publishes its list and lock.
	publisher := setUp()

	manifest, err := os.ReadFile(publisher.namedManifest(t, "tool", "tool", "tool"))
	must(t, err)

	files["/plain/tool.toml"] = manifest

	_, err = publisher.run(t, "", "add", server.URL+"/plain/tool.toml")
	must(t, err)

	for _, name := range []string{"oku.toml", "oku.lock"} {
		body, err := os.ReadFile(filepath.Join(publisher.config, name))
		must(t, err)

		files["/raw/me/machines/"+commit+"/"+name] = body
	}

	want := publisher.storeEntries(t)

	for range 2 {
		m := setUp()

		out, err := m.run(t, "", "sync", "github:me/machines")
		if err != nil {
			t.Fatalf("sync github:me/machines: %v\n%s", err, out)
		}

		if !strings.Contains(out, "1 locked package") {
			t.Fatalf("sync did not adopt the lock:\n%s", out)
		}

		if got := m.storeEntries(t); !slices.Equal(got, want) {
			t.Fatalf("store holds %v, the publisher has %v", got, want)
		}

		if !exists(m.profile("bin", "tool")) {
			t.Fatal("tool is not in the profile")
		}

		_, err = m.run(t, "", "sync", "github:me/machines")
		if err == nil || !strings.Contains(err.Error(), "already has packages or includes") {
			t.Fatalf("want a second adoption to be refused, got %v", err)
		}
	}
}

func TestB19RelativeRefsStartAtTheListAndRemoteListsRejectLocalPaths(t *testing.T) {
	const commit = "4444444444444444444444444444444444444444"

	m := newMachine(t)
	m.namedManifest(t, "tool", "tool", "tool")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/me/lists/commits/HEAD":
			_, _ = w.Write([]byte(commit))
		case "/raw/me/lists/" + commit + "/oku.toml":
			_, _ = w.Write([]byte("[packages]\ntool = \"./tool.toml\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	listPath := filepath.Join(m.config, "oku.toml")
	must(t, os.MkdirAll(m.config, 0o755))

	// The list is in the config directory and the test runs elsewhere, so
	// "../../fixtures" only resolves when it starts at the list.
	must(
		t,
		os.WriteFile(listPath, []byte("[packages]\ntool = \"../../fixtures/tool.toml\"\n"), 0o644),
	)

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync with a relative ref: %v\n%s", err, out)
	}

	must(t, os.WriteFile(listPath, []byte("include = [\"github:me/lists\"]\n"), 0o644))

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "local path") {
		t.Fatalf("want a remote list naming a local path to fail, got %v", err)
	}
}

func TestB19AddStoresAFileInsideTheProjectRelativeToIt(t *testing.T) {
	m := newMachine(t)

	manifest, err := os.ReadFile(m.namedManifest(t, "tool", "tool", "tool"))
	must(t, err)

	project := filepath.Join(m.fixtures, "work", "api")
	must(t, os.MkdirAll(filepath.Join(project, "recipes"), 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(project, "recipes", "tool.toml"), manifest, 0o644))

	m.opts.WorkDir = project

	_, err = m.run(t, "", "add", filepath.Join(project, "recipes", "tool.toml"))
	must(t, err)

	for _, name := range []string{"oku.toml", "oku.lock"} {
		data, err := os.ReadFile(filepath.Join(project, name))
		must(t, err)

		if !strings.Contains(string(data), "./recipes/tool.toml") ||
			strings.Contains(string(data), project) {
			t.Fatalf("%s does not hold the ref relative to the project:\n%s", name, data)
		}
	}

	// The hook compares the lock with the copy in the profile.
	_, err = m.run(t, "", "allow")
	must(t, err)

	t.Setenv("PATH", "/usr/bin:/bin")

	if out := m.apply(
		t,
	); !strings.Contains(
		os.Getenv("PATH"),
		filepath.Dir(m.projectBin(t, "tool")),
	) {
		t.Fatalf("the hook did not apply the project right after add:\n%s", out)
	}

	// Another checkout is the same project at another path.
	moved := filepath.Join(m.fixtures, "elsewhere")
	must(t, os.Rename(project, moved))
	must(t, os.RemoveAll(m.data))

	locked, err := os.ReadFile(filepath.Join(moved, "oku.lock"))
	must(t, err)

	m.opts.WorkDir = moved

	out, err := m.run(t, "", "sync")
	if err != nil || !strings.Contains(out, "profile now holds 1 package") {
		t.Fatalf("sync in the moved project: %v\n%s", err, out)
	}

	after, err := os.ReadFile(filepath.Join(moved, "oku.lock"))
	must(t, err)

	if string(after) != string(locked) {
		t.Fatalf("sync in the moved project changed the lock:\n%s", after)
	}
}

// releaseServer fakes the GitHub releases API for owner/tool. The test changes
// tags between calls, and hits counts the requests.
type releaseServer struct {
	*httptest.Server

	tags []string
	hits int
}

func newReleaseServer(t *testing.T, tags ...string) *releaseServer {
	t.Helper()

	rs := &releaseServer{tags: tags}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/owner/tool/releases" {
			http.NotFound(w, r)

			return
		}

		rs.hits++

		// The server gives one page of the tags, the way GitHub does.
		tags := rs.tags

		if size, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			from := min(max(page-1, 0)*size, len(tags))
			tags = tags[from:min(from+size, len(tags))]
		}

		var items []string

		for _, tag := range tags {
			items = append(items, fmt.Sprintf(
				`{"tag_name": %q, "draft": %t, "prerelease": %t}`,
				tag, strings.HasSuffix(tag, "-draft"), strings.Contains(tag, "-rc"),
			))
		}

		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(rs.Close)

	return rs
}

// discoveredManifest writes a manifest whose versions come from the release
// server, plus one archive per version.
func (m machine) discoveredManifest(t *testing.T, versions ...string) string {
	t.Helper()

	for _, version := range versions {
		m.archive(
			t,
			"tool-v"+version,
			map[string]string{"tool": "#!/bin/sh\necho " + version + "\n"},
		)
	}

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{tag}}.tar.gz\"\nbin = [\"tool\"]\n",
		m.fixtures,
	)), 0o644))

	return path
}

func (m machine) toolOutput(t *testing.T) string {
	t.Helper()

	out, err := exec.Command(m.profile("bin", "tool")).Output()
	must(t, err)

	return strings.TrimSpace(string(out))
}

func TestB118AddFindsAVersionPastTheFirstPageOfReleases(t *testing.T) {
	m := newMachine(t)

	var tags []string
	for i := 60; i > 0; i-- {
		tags = append(tags, fmt.Sprintf("v2.0.%d", i))
	}

	server := newReleaseServer(t, tags...)
	m.opts.GitHubAPI = server.URL + "/api"

	ref := m.discoveredManifest(t, "2.0.3")

	_, err := m.run(t, "", "add", ref+"@2.0.3")
	must(t, err)

	if got := m.toolOutput(t); got != "2.0.3" {
		t.Fatalf("add installed %s, want 2.0.3 from the third page", got)
	}
}

func TestB20AddPicksNewestDiscoveredVersionOrThePinnedOne(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.2.0", "v1.10.0", "v2.0.0-rc1", "v3.0.0-draft", "nightly")
	m.opts.GitHubAPI = server.URL + "/api"

	ref := m.discoveredManifest(t, "1.2.0", "1.10.0")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if got := m.toolOutput(t); got != "1.10.0" {
		t.Fatalf("add installed %s, want the newest release 1.10.0", got)
	}

	_, err = m.run(t, "", "add", ref+"@1.2.0")
	must(t, err)

	if got := m.toolOutput(t); got != "1.2.0" {
		t.Fatalf("add @1.2.0 installed %s", got)
	}

	_, err = m.run(t, "", "add", ref+"@9.9.9")
	if err == nil || !strings.Contains(err.Error(), "1.10.0") {
		t.Fatalf("want an unknown version to fail and name the newest, got %v", err)
	}
}

func TestB21VersionsOnlyMoveOnUpdate(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	ref := m.discoveredManifest(t, "1.0.0", "1.1.0")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	server.tags = append(server.tags, "v1.1.0")
	server.hits = 0

	must(t, os.RemoveAll(m.data))

	_, err = m.run(t, "", "sync")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("sync moved tool to %s", got)
	}

	if server.hits != 0 {
		t.Fatalf("sync listed releases %d times, want the locked version used as is", server.hits)
	}

	out, err := m.run(t, "", "update")
	must(t, err)

	if got := m.toolOutput(t); got != "1.1.0" || !strings.Contains(out, "1.0.0 -> 1.1.0") {
		t.Fatalf("update left tool at %s:\n%s", got, out)
	}

	_, err = m.run(t, "", "add", ref+"@1.0.0")
	must(t, err)

	_, err = m.run(t, "", "update")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("update moved a package pinned to 1.0.0 to %s", got)
	}
}

// nightlyServer fakes GitHub for owner/tool's moving tag "nightly" and serves
// its one asset. publish moves the tag, and hits counts the API requests.
type nightlyServer struct {
	*httptest.Server

	commit string
	body   []byte
	// digest is what the API reports for the asset.
	digest string
	hits   int
}

func newNightlyServer(t *testing.T) *nightlyServer {
	t.Helper()

	ns := &nightlyServer{}
	ns.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/releases/tags/nightly":
			ns.hits++

			fmt.Fprintf(
				w,
				`{"draft": false, "prerelease": true, "published_at": "2026-01-01T00:00:00Z",`+
					`"assets": [{"browser_download_url": %q, "digest": "sha256:%s"}]}`,
				ns.URL+"/dl/tool.tar.gz", ns.digest,
			)
		case "/api/repos/owner/tool/commits/nightly":
			ns.hits++

			fmt.Fprintf(
				w,
				`{"sha": %q, "commit": {"committer": {"date": "2026-09-20T05:23:16Z"}}}`,
				ns.commit,
			)
		case "/dl/tool.tar.gz":
			_, _ = w.Write(ns.body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ns.Close)

	return ns
}

// publish moves the tag to commit with a tool that prints say.
func (ns *nightlyServer) publish(t *testing.T, m machine, commit, say string) {
	t.Helper()

	path, sum := m.archive(t, "nightly", map[string]string{"tool": "#!/bin/sh\necho " + say + "\n"})

	body, err := os.ReadFile(path)
	must(t, err)

	ns.commit, ns.body, ns.digest = commit, body, sum
}

// manifest writes a manifest that follows the tag and states no checksum.
func (ns *nightlyServer) manifest(t *testing.T, m machine) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\ntag = \"nightly\"\n"+
			"[[artifact]]\nurl = \"%s/dl/tool.tar.gz\"\nbin = [\"tool\"]\n",
		ns.URL,
	)), 0o644))

	return path
}

const (
	nightlyCommitA = "aaaaaaa1111111111111111111111111111111111"
	nightlyCommitB = "bbbbbbb2222222222222222222222222222222222"
)

func TestB106AMovingTagInstallsUpdatesAndRollsBack(t *testing.T) {
	m := newMachine(t)
	server := newNightlyServer(t)
	m.opts.GitHubAPI = server.URL + "/api"
	server.publish(t, m, nightlyCommitA, "monday")

	_, err := m.run(t, "", "add", server.manifest(t, m))
	must(t, err)

	out, err := m.run(t, "", "list")
	must(t, err)

	if got := m.toolOutput(t); got != "monday" || !strings.Contains(out, "2026.09.20-aaaaaaa") {
		t.Fatalf("add installed %s:\n%s", got, out)
	}

	out, err = m.run(t, "", "update")
	must(t, err)

	if strings.HasPrefix(out, "tool ") || strings.Contains(out, "\ntool ") {
		t.Fatalf("update changed a tag that did not move:\n%s", out)
	}

	server.publish(t, m, nightlyCommitB, "tuesday")

	out, err = m.run(t, "", "update")
	must(t, err)

	if got := m.toolOutput(t); got != "tuesday" ||
		!strings.Contains(out, "2026.09.20-aaaaaaa -> 2026.09.20-bbbbbbb") {
		t.Fatalf("update left tool at %s:\n%s", got, out)
	}

	server.body = nil

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if got := m.toolOutput(t); got != "monday" {
		t.Fatalf("rollback left tool at %s", got)
	}
}

func TestB107AMovingTagDownloadMustMatchTheAPIDigest(t *testing.T) {
	m := newMachine(t)
	server := newNightlyServer(t)
	m.opts.GitHubAPI = server.URL + "/api"
	server.publish(t, m, nightlyCommitA, "monday")
	server.digest = strings.Repeat("0", 64)

	_, err := m.run(t, "", "add", server.manifest(t, m))
	if err == nil {
		t.Fatal("add accepted a download that does not match the API digest")
	}

	if entries, _ := os.ReadDir(filepath.Join(m.data, "oku", "store")); len(entries) != 0 {
		t.Fatalf("the rejected download left %d store entries", len(entries))
	}

	if _, err := os.Lstat(m.profile("bin", "tool")); err == nil {
		t.Fatal("the rejected download reached the profile")
	}
}

func TestB108SyncFailsOnceTheLockedMovingTagMoved(t *testing.T) {
	m := newMachine(t)
	server := newNightlyServer(t)
	m.opts.GitHubAPI = server.URL + "/api"
	server.publish(t, m, nightlyCommitA, "monday")

	_, err := m.run(t, "", "add", server.manifest(t, m))
	must(t, err)

	server.publish(t, m, nightlyCommitB, "tuesday")
	server.hits = 0

	_, err = m.run(t, "", "sync")
	must(t, err)

	if server.hits != 0 {
		t.Fatalf("sync asked upstream %d times with the store path present", server.hits)
	}

	must(t, os.RemoveAll(m.data))

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "oku update tool") {
		t.Fatalf("want sync to fail and name oku update tool, got %v", err)
	}

	if _, err := os.Lstat(m.profile("bin", "tool")); err == nil {
		t.Fatal("sync installed a newer build under the locked version")
	}
}

func TestB109AMovingTagPinFailsWhenUpstreamIsElsewhere(t *testing.T) {
	m := newMachine(t)
	server := newNightlyServer(t)
	m.opts.GitHubAPI = server.URL + "/api"
	server.publish(t, m, nightlyCommitB, "tuesday")

	_, err := m.run(t, "", "add", server.manifest(t, m)+"@2026.09.20-aaaaaaa")
	if err == nil || !strings.Contains(err.Error(), "2026.09.20-bbbbbbb") {
		t.Fatalf("want the pin to fail and name the version upstream is at, got %v", err)
	}
}

func TestB110LintAndBumpRefuseAMisplacedTag(t *testing.T) {
	m := newMachine(t)

	cases := map[string]string{
		"from = \"git-tags\"\nrepo = \"https://example.com/tool\"\ntag = \"nightly\"":                "github-releases",
		"value = \"1.0.0\"\ntag = \"nightly\"":                                                       "github-releases",
		"from = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\ntag = \"nightly\"": "strip_prefix",
	}

	path := filepath.Join(m.fixtures, "tool.toml")

	url := "https://example.com/tool.tar.gz"

	write := func(version string) {
		must(t, os.WriteFile(path, []byte(
			"[package]\nname = \"tool\"\ndescription = \"a tool\"\n[version]\n"+version+
				"\n[[artifact]]\nurl = \""+url+"\"\nbin = [\"tool\"]\n",
		), 0o644))
	}

	for version, want := range cases {
		write(version)

		out, err := m.run(t, "", "manifest", "lint", path)
		if err == nil || !strings.Contains(out, want) {
			t.Errorf("lint accepted %q, or did not say %q: %v\n%s", version, want, err, out)
		}
	}

	moving := "from = \"github-releases\"\nrepo = \"owner/tool\"\ntag = \"nightly\""
	write(moving)

	out, err := m.run(t, "", "manifest", "lint", path)
	if err != nil || !strings.Contains(out, "trust the first download") {
		t.Errorf("lint did not warn about a url GitHub reports no sha256 for: %v\n%s", err, out)
	}

	url = "https://github.com/owner/tool/releases/download/{{tag}}/tool.tar.gz"
	write(moving)

	out, err = m.run(t, "", "manifest", "lint", path)
	if err != nil || strings.Contains(out, "warning") {
		t.Errorf("lint warned about a download GitHub reports a sha256 for: %v\n%s", err, out)
	}

	if _, err := m.run(t, "", "manifest", "bump", path); err == nil {
		t.Fatal("bump accepted a manifest that follows a moving tag")
	}
}

func TestB22EveryChangeIsAGenerationAndRollbackRestoresOne(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	ref := m.discoveredManifest(t, "1.0.0", "1.1.0")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	server.tags = append(server.tags, "v1.1.0")

	_, err = m.run(t, "", "update")
	must(t, err)

	out, err := m.run(t, "", "generations")
	must(t, err)

	if !strings.Contains(out, "  1") || !strings.Contains(out, "* 2") ||
		!strings.Contains(out, "tool 1.0.0") || !strings.Contains(out, "tool 1.1.0") {
		t.Fatalf("generations after add and update:\n%s", out)
	}

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("rollback left tool at %s", got)
	}

	// The lock went back too, so sync does not undo the rollback.
	out, err = m.run(t, "", "sync")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" || !strings.Contains(out, "already in sync") {
		t.Fatalf("sync after rollback moved tool to %s:\n%s", got, out)
	}

	_, err = m.run(t, "", "rollback", "2")
	must(t, err)

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("rollback 2 left tool at %s", got)
	}

	if _, err := m.run(t, "", "rollback", "99"); err == nil {
		t.Fatal("rolling back to a generation that does not exist succeeded")
	}

	_, err = m.run(t, "", "rollback", "1")
	must(t, err)

	if _, err := m.run(t, "", "rollback"); err == nil {
		t.Fatal("rolling back from the oldest generation succeeded")
	}
}

func TestB23GCDeletesOnlyStorePathsNoGenerationUses(t *testing.T) {
	m := newMachine(t)
	keep := m.namedManifest(t, "keep", "keep", "keep")
	gone := m.namedManifest(t, "gone", "gone", "gone")

	for _, args := range [][]string{{"add", keep}, {"add", gone}, {"remove", "gone"}} {
		_, err := m.run(t, "", args...)
		must(t, err)
	}

	before := m.storeEntries(t)

	out, err := m.run(t, "", "gc")
	must(t, err)

	if got := m.storeEntries(t); !slices.Equal(got, before) {
		t.Fatalf("gc deleted a path that generation 2 still uses: %v\n%s", got, out)
	}

	out, err = m.run(t, "", "gc", "--keep", "1", "--dry-run")
	must(t, err)

	if got := m.storeEntries(
		t,
	); !slices.Equal(got, before) ||
		!strings.Contains(out, "would remove gone-") {
		t.Fatalf("dry run changed the store or did not name gone: %v\n%s", got, out)
	}

	_, err = m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	got := m.storeEntries(t)
	if len(got) != 1 || !strings.HasPrefix(got[0], "keep-") {
		t.Fatalf("store after gc --keep 1 holds %v, want only keep", got)
	}

	if _, err := exec.Command(m.profile("bin", "keep")).Output(); err != nil {
		t.Fatalf("keep no longer runs: %v", err)
	}

	for _, path := range []string{
		filepath.Join(m.config, "oku.toml"),
		filepath.Join(m.config, "oku.lock"),
		filepath.Join(m.cache, "downloads"),
	} {
		if !exists(path) {
			t.Fatalf("gc deleted %s", path)
		}
	}
}

// inferServer fakes a GitHub repo owner/tool that has releases and no manifest.
// assets maps an asset name to the local file that holds it.
func inferServer(t *testing.T, m *machine, assets map[string]string) {
	t.Helper()

	var items []string
	for name, file := range assets {
		items = append(
			items,
			fmt.Sprintf(`{"name": %q, "browser_download_url": "file://%s"}`, name, file),
		)
	}

	latest := `{"tag_name": "v1.4.0", "assets": [` + strings.Join(items, ",") + `]}`
	nightly := `{"tag_name": "nightly", "target_commitish": "7777777aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		` "assets": [` + strings.Join(
		items,
		",",
	) + `]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/commits/HEAD":
			_, _ = w.Write([]byte("5555555555555555555555555555555555555555"))
		case "/api/repos/owner/tool/releases/latest":
			_, _ = w.Write([]byte(latest))
		case "/api/repos/owner/tool/releases/tags/nightly":
			_, _ = w.Write([]byte(nightly))
		case "/api/repos/owner/tool/releases":
			_, _ = w.Write([]byte(`[{"tag_name": "v1.4.0"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"
}

// hostAssetName names a release asset the way upstream projects do for the
// machine that runs the test.
func hostAssetName() string {
	host := platform.Host()
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[host.Arch]
	os := map[string]string{"darwin": "apple-darwin", "linux": "unknown-linux-musl"}[host.OS]

	return "tool-v1.4.0-" + arch + "-" + os + ".tar.gz"
}

func TestB25AddInfersAManifestForARepoWithoutOne(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool":       "#!/bin/sh\necho inferred\n",
		"tool-1.4.0/doc/tool.1": ".TH",
	})

	inferServer(t, &m, map[string]string{
		hostAssetName():                 archive,
		"tool-v1.4.0-riscv64-plan9.zip": archive,
		"tool-v1.4.0.deb":               archive,
	})

	out, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		"has no manifest", `from = "github-releases"`, `strip_prefix = "v"`,
		"tool-{{tag}}-", "strip = 1", `bin = ["tool"]`, `man = ["doc/tool.1"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("add did not print the inferred manifest, missing %q:\n%s", want, out)
		}
	}

	if got := m.toolOutput(t); got != "inferred" {
		t.Fatalf("tool printed %q", got)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), "inferred = true") {
		t.Fatalf("oku.lock does not mark the package inferred:\n%s", locked)
	}

	// A new machine installs from the manifest text in the lock.
	must(t, os.RemoveAll(m.data))

	out, err = m.run(t, "", "sync")
	if err != nil || strings.Contains(out, "has no manifest") {
		t.Fatalf("sync inferred again or failed: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "inferred" {
		t.Fatalf("tool printed %q after sync", got)
	}
}

func TestB26InferenceWithoutAHostAssetListsWhatItSaw(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	inferServer(t, &m, map[string]string{
		"tool-v1.4.0-riscv64-plan9.tar.gz": archive,
		"tool-v1.4.0.deb":                  archive,
	})

	_, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
	if err == nil {
		t.Fatal("add succeeded without an asset for this machine")
	}

	for _, want := range []string{platform.Host().String(), "tool-v1.4.0-riscv64-plan9.tar.gz", "tool-v1.4.0.deb"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error lacks %q: %v", want, err)
		}
	}
}

func TestB25AddKeepsTheInferredManifestForVerbose(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho inferred\n",
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "has no manifest") || strings.Contains(out, "from = ") {
		t.Fatalf("add should say it inferred a manifest and not print it:\n%s", out)
	}
}

func TestB27ManifestInitWritesTheInferredManifest(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	// A manifest covers every platform, so the test checks the x86_64 and aarch64
	// spellings whatever the host is.
	inferServer(t, &m, map[string]string{
		hostAssetName():                           archive,
		"tool-v1.4.0-x86_64-pc-windows-msvc.zip":  archive,
		"tool-v1.4.0-aarch64-pc-windows-msvc.zip": archive,
	})

	target := filepath.Join(m.fixtures, "oku.pkg.toml")

	_, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", target)
	must(t, err)

	written, err := os.ReadFile(target)
	must(t, err)

	for _, arch := range []string{"amd64", "arm64"} {
		match := fmt.Sprintf(`match = { os = "windows", arch = %q }`, arch)
		if !strings.Contains(string(written), match) {
			t.Fatalf("the manifest has no artifact for windows %s:\n%s", arch, written)
		}
	}

	if _, err := m.run(t, "", "add", target); err != nil {
		t.Fatalf("the written manifest does not install: %v", err)
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("tool printed %q", got)
	}

	if _, err := m.run(
		t,
		"",
		"manifest",
		"init",
		"--from",
		"owner/tool",
		"-o",
		target,
	); err == nil {
		t.Fatal("manifest init replaced an existing file without --force")
	}
}

// rewriteHost sends requests for one host to a test server.
type rewriteHost struct {
	host   string
	server *url.URL
}

func (rw rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == rw.host {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = rw.server.Scheme, rw.server.Host
	}

	return http.DefaultTransport.RoundTrip(req)
}

func TestB114AddReadsAGitHubEnterpriseHostFromTheRef(t *testing.T) {
	m := newMachine(t)
	manifest, err := os.ReadFile(
		m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`),
	)
	must(t, err)

	t.Setenv("GITHUB_TOKEN", "for-github-com")
	t.Setenv("GH_ENTERPRISE_TOKEN", "for-the-server")

	var tokens []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))

		switch r.URL.Path {
		case "/api/v3/repos/owner/tool/commits/HEAD":
			_, _ = w.Write([]byte("5555555555555555555555555555555555555555"))
		case "/api/v3/repos/owner/tool/contents/oku.pkg.toml":
			_, _ = w.Write(manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	must(t, err)

	client := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteHost{host: "ghe.example.com", server: target}

	t.Cleanup(func() { http.DefaultClient.Transport = client })

	out, err := m.run(t, "", "add", "github:ghe.example.com/owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("tool printed %q", got)
	}

	for _, token := range tokens {
		if token != "Bearer for-the-server" {
			t.Fatalf("the server got the token %q", token)
		}
	}

	list, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(list), "github:ghe.example.com/owner/tool") {
		t.Fatalf("oku.toml lacks the ref with its host:\n%s", list)
	}
}

func TestB115AddInfersFromACodebergRepoAndUpdateListsItsReleases(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	t.Setenv("GITHUB_TOKEN", "for-github-com")
	t.Setenv("CODEBERG_TOKEN", "for-codeberg")

	release := fmt.Sprintf(
		`{"tag_name": "v1.4.0", "assets": [{"name": %q, "browser_download_url": "file://%s"}]}`,
		hostAssetName(), archive,
	)

	var tokens []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))

		switch r.URL.Path {
		case "/api/v1/repos/owner/tool/commits":
			_, _ = w.Write([]byte(`[{"sha": "5555555555555555555555555555555555555555"}]`))
		case "/api/v1/repos/owner/tool/releases/latest":
			_, _ = w.Write([]byte(release))
		case "/api/v1/repos/owner/tool/releases":
			_, _ = w.Write([]byte("[" + release + "]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	must(t, err)

	client := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteHost{host: "codeberg.org", server: target}

	t.Cleanup(func() { http.DefaultClient.Transport = client })

	out, err := m.run(t, "", "add", "codeberg:owner/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		`from = "gitea-releases"`, `repo = "codeberg.org/owner/tool"`,
		`homepage = "https://codeberg.org/owner/tool"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the inferred manifest lacks %q:\n%s", want, out)
		}
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("tool printed %q", got)
	}

	if out, err = m.run(t, "", "update"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	for _, token := range tokens {
		if token != "token for-codeberg" {
			t.Fatalf("codeberg.org got the token %q", token)
		}
	}
}

func TestB116AddInfersFromAGitLabProjectInASubgroup(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	t.Setenv("GITLAB_TOKEN", "for-gitlab-com")

	release := fmt.Sprintf(
		`{"tag_name": "v1.4.0", "commit": {"id": "5555555555555555555555555555555555555555"},`+
			` "assets": {"links": [{"name": %q, "url": "file:///encoded", "direct_asset_url": "file://%s"}]}}`,
		hostAssetName(), archive,
	)

	var tokens []string

	const project = "/api/v4/projects/group%2Fsub%2Ftool"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))

		switch r.URL.EscapedPath() {
		case project + "/repository/commits":
			_, _ = w.Write([]byte(`[{"id": "5555555555555555555555555555555555555555"}]`))
		case project + "/releases/permalink/latest":
			_, _ = w.Write([]byte(release))
		case project + "/releases":
			_, _ = w.Write([]byte("[" + release + "]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	must(t, err)

	client := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteHost{host: "gitlab.com", server: target}

	t.Cleanup(func() { http.DefaultClient.Transport = client })

	out, err := m.run(t, "", "add", "gitlab:group/sub/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		`from = "gitlab-releases"`, `repo = "group/sub/tool"`,
		`homepage = "https://gitlab.com/group/sub/tool"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the inferred manifest lacks %q:\n%s", want, out)
		}
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("tool printed %q", got)
	}

	if out, err = m.run(t, "", "update"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	for _, token := range tokens {
		if token != "Bearer for-gitlab-com" {
			t.Fatalf("gitlab.com got the token %q", token)
		}
	}
}

func TestSyncAfterAStoppedRunReusesItsDownloads(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho inferred\n",
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// A run that stops early has written no lock and may have no store path.
	must(t, os.Remove(filepath.Join(m.config, "oku.lock")))
	must(t, os.RemoveAll(m.data))

	out, err = m.run(t, "", "sync")
	if err != nil || strings.Contains(out, "downloading") {
		t.Fatalf("sync downloaded again or failed: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "inferred" {
		t.Fatalf("tool printed %q", got)
	}
}

func TestB117AddTakesAURLThatIsTheDownloadItself(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho from a url\n",
	})
	manifest := m.manifest(t, "other", map[string]string{"other": script}, `bin = ["other"]`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/tool-v1.4.0-linux-amd64.tar.gz":
			http.ServeFile(w, r, archive)
		case "/manifest-without-an-ending":
			http.ServeFile(w, r, manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	out, err := m.run(t, "", "add", server.URL+"/dl/tool-v1.4.0-linux-amd64.tar.gz", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		"is a download", `name = "tool"`, `value = "1.4.0"`, "strip = 1", `bin = ["tool"]`,
		"added tool 1.4.0", "trusted this download",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("add output lacks %q:\n%s", want, out)
		}
	}

	if got := m.toolOutput(t); got != "from a url" {
		t.Fatalf("tool printed %q", got)
	}

	// A URL that holds a manifest stays a manifest, whatever its name.
	out, err = m.run(t, "", "add", server.URL+"/manifest-without-an-ending")
	if err != nil || !strings.Contains(out, "added other 1.2.3") {
		t.Fatalf("add of a manifest URL: %v\n%s", err, out)
	}

	_, err = m.run(t, "", "add", server.URL+"/dl/missing.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want a missing URL to fail as not found, got %v", err)
	}
}

func TestB113AddAtAVersionInfersFromThatVersionsRelease(t *testing.T) {
	m := newMachine(t)
	host := platform.Host()

	// The older release names its files another way than the newest one does.
	older, _ := m.archive(t, "older", map[string]string{"tool": "#!/bin/sh\necho 1.3.0\n"})
	newest, _ := m.archive(t, "newest", map[string]string{"tool": "#!/bin/sh\necho 1.4.0\n"})
	olderName := "tool_1.3.0_" + host.OS + "_" + host.Arch + ".tar.gz"

	release := func(tag, name, file string) string {
		return fmt.Sprintf(
			`{"tag_name": %q, "assets": [{"name": %q, "browser_download_url": "file://%s"}]}`,
			tag, name, file,
		)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/owner/tool/commits/HEAD":
			_, _ = w.Write([]byte("5555555555555555555555555555555555555555"))
		case "/api/repos/owner/tool/releases/latest":
			_, _ = w.Write([]byte(release("v1.4.0", hostAssetName(), newest)))
		case "/api/repos/owner/tool/releases/tags/v1.3.0":
			_, _ = w.Write([]byte(release("v1.3.0", olderName, older)))
		case "/api/repos/owner/tool/releases":
			_, _ = w.Write([]byte(`[{"tag_name": "v1.4.0"}, {"tag_name": "v1.3.0"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"

	out, err := m.run(t, "", "add", "github:owner/tool@1.3.0")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.3.0" {
		t.Fatalf("add @1.3.0 installed %q, want the file of the 1.3.0 release", got)
	}
}

func TestB122ABinTableWritesAProgramThatRunsADepWithArguments(t *testing.T) {
	m := newMachine(t)

	// The dep stands in for an interpreter such as node.
	m.manifest(t, "interp", map[string]string{
		"interp": "#!/bin/sh\necho \"interp ran $(basename \"$1\") $2\"\n",
	}, `bin = ["interp"]`)

	archive, sum := m.archive(t, "script", map[string]string{"lib/main.js": "// the script"})
	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(`[package]
name = "tool"
[version]
value = "1.0.0"
[runtime]
deps = ["./interp.toml"]
[[artifact]]
url = "file://%s"
sha256 = %q
bin = [{ name = "tool", run = "{{dep.interp.prefix}}/bin/interp", args = ["{{pkg}}/lib/main.js"] }]
`, archive, sum)), 0o644))

	out, err := m.run(t, "", "manifest", "lint", path)
	if err != nil {
		t.Fatalf("lint: %v\n%s", err, out)
	}

	out, err = m.run(t, "", "add", path)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got, err := exec.Command(m.profile("bin", "tool"), "--stdio").Output()
	must(t, err)

	if strings.TrimSpace(string(got)) != "interp ran main.js --stdio" {
		t.Fatalf("tool printed %q", got)
	}

	if _, err := os.Stat(m.profile("bin", "interp")); err == nil {
		t.Fatal("the dep's program is in the user's profile")
	}

	bad := strings.Replace(path, "tool.toml", "bad.toml", 1)
	must(t, os.WriteFile(bad, []byte(fmt.Sprintf(
		"[package]\nname = \"bad\"\n[version]\nvalue = \"1.0.0\"\n[[artifact]]\nurl = \"file://%s\"\n"+
			"bin = [{ name = \"bad\", run = \"{{nope}}/x\" }]\n",
		archive,
	)), 0o644))

	if out, err = m.run(
		t,
		"",
		"manifest",
		"lint",
		bad,
	); err == nil ||
		!strings.Contains(out, "nope") {
		t.Fatalf("lint accepted an unknown variable in a bin table: %v\n%s", err, out)
	}
}

// npmServer fakes the npm registry for the package @scope/tool. Each version's
// download is a tar archive whose program prints the version. A version in
// tampered gets an integrity that does not fit its download.
func npmServer(t *testing.T, m *machine, tampered string, versions ...string) string {
	t.Helper()

	return npmServerWith(t, m, tampered, false, versions...)
}

// npmServerWith is npmServer for a package that lists a dependency when deps is
// true.
func npmServerWith(
	t *testing.T,
	m *machine,
	tampered string,
	deps bool,
	versions ...string,
) string {
	t.Helper()

	files := map[string]string{}

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if file, ok := files[r.URL.Path]; ok {
			http.ServeFile(w, r, file)

			return
		}

		if r.URL.Path != "/@scope/tool" {
			http.NotFound(w, r)

			return
		}

		var items, times []string

		for _, version := range versions {
			at := "/@scope/tool/-/tool-" + version + ".tgz"

			data, err := os.ReadFile(files[at])
			if err != nil {
				t.Error(err)
			}

			if version == tampered {
				data = append(data, 'x')
			}

			sum := sha512.Sum512(data)
			needs := ""
			if deps {
				needs = `"dependencies": {"left-pad": "^1.0.0"}, `
			}

			items = append(items, fmt.Sprintf(
				`%q: {%s"bin": {"tool": "./tool"}, "dist": {"tarball": %q, "integrity": "sha512-%s"}}`,
				version,
				needs,
				server.URL+at,
				base64.StdEncoding.EncodeToString(sum[:]),
			))
			times = append(times, fmt.Sprintf(`%q: "2026-01-02T03:04:05.000Z"`, version))
		}

		// The newest version that is no prerelease has the "latest" tag.
		latest := ""

		for _, version := range versions {
			if !strings.Contains(version, "-") {
				latest = version
			}
		}

		_, _ = fmt.Fprintf(
			w, `{"dist-tags": {"latest": %q}, "time": {%s}, "versions": {%s}}`,
			latest, strings.Join(times, ","), strings.Join(items, ","),
		)
	}))
	t.Cleanup(server.Close)

	for _, version := range versions {
		archive, _ := m.archive(t, "tool-"+version, map[string]string{
			"package/tool": "#!/bin/sh\necho " + version + "\n",
		})
		files["/@scope/tool/-/tool-"+version+".tgz"] = archive
	}

	m.opts.NPMRegistry = server.URL

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(`[package]
name = "tool"
[version]
from = "npm"
repo = "@scope/tool"
[[artifact]]
url = "%s/@scope/tool/-/tool-{{version}}.tgz"
strip = 1
bin = ["tool"]
`, server.URL)), 0o644))

	return path
}

func TestB123VersionsComeFromTheNPMRegistry(t *testing.T) {
	m := newMachine(t)
	ref := npmServer(t, &m, "", "1.0.0", "1.1.0", "2.0.0-beta.1")

	out, err := m.run(t, "", "add", ref)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("add installed %s, want the newest version that is no prerelease", got)
	}

	// The registry's sha512 checked the download, so oku did not trust it blindly.
	if strings.Contains(out, "trusted this download") {
		t.Fatalf("add trusted a download that the registry has a digest for:\n%s", out)
	}

	_, err = m.run(t, "", "add", ref+"@1.0.0")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("add @1.0.0 installed %s", got)
	}
}

func TestB124ADownloadThatDoesNotFitItsIntegrityIsRejected(t *testing.T) {
	m := newMachine(t)
	ref := npmServer(t, &m, "1.1.0", "1.0.0", "1.1.0")

	_, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "integrity mismatch") {
		t.Fatalf("want an integrity mismatch, got %v", err)
	}

	if entries := m.storeEntries(t); len(entries) != 0 {
		t.Fatalf("the store holds %v after a rejected download", entries)
	}
}

func TestB125ManifestHashPrintsTheChecksumsOfADownload(t *testing.T) {
	m := newMachine(t)
	archive, sum := m.archive(t, "release", map[string]string{"tool": script})

	data, err := os.ReadFile(archive)
	must(t, err)

	sum512 := sha512.Sum512(data)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum512[:])

	for _, at := range []string{archive, "file://" + archive} {
		out, err := m.run(t, "", "manifest", "hash", at)
		if err != nil {
			t.Fatalf("hash %s: %v\n%s", at, err, out)
		}

		if !strings.Contains(out, fmt.Sprintf("sha256 = %q", sum)) ||
			!strings.Contains(out, fmt.Sprintf("integrity = %q", integrity)) {
			t.Fatalf("hash %s printed:\n%s", at, out)
		}
	}

	// The printed line is one a manifest accepts.
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nintegrity = %q\nbin = [\"tool\"]\n", archive, integrity,
	))

	out, err := m.run(t, "", "add", ref)
	if err != nil || strings.Contains(out, "trusted this download") {
		t.Fatalf("add with the printed integrity: %v\n%s", err, out)
	}
}

// fakeNode writes a package called "interp" and returns its manifest. Its
// program "node" runs a script with sh. Its program "npm" stands in for "npm
// install": it writes the package as a script that prints what npm was asked
// for, so a test can see the version and the date.
func (m machine) fakeNode(t *testing.T) string {
	t.Helper()

	npm := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) prefix="$2"; shift ;;
    --before=*) before="${1#--before=}" ;;
    --ignore-scripts) safe=yes ;;
    -*|install) ;;
    *) spec="$1" ;;
  esac
  shift
done
dir="$prefix/node_modules/@scope/tool"
mkdir -p "$dir" "$prefix/node_modules/left-pad"
echo "echo $spec before $before scripts-off=$safe" > "$dir/tool"
echo "module.exports = 1" > "$prefix/node_modules/left-pad/index.js"
`

	return m.manifest(t, "interp", map[string]string{
		"node": "#!/bin/sh\nexec sh \"$@\"\n", "npm": npm,
	}, `bin = ["node", "npm"]`)
}

func TestB126AddInfersAnNPMPackageThatRunsThroughTheConfiguredNode(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.0.0", "1.1.0")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(
		filepath.Join(m.config, "config.toml"),
		[]byte(fmt.Sprintf("[runtimes]\nnode = %q\n", m.fakeNode(t))), 0o644,
	))

	out, err := m.run(t, "", "add", "npm:@scope/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		"is an npm package", `from = "npm"`, `repo = "@scope/tool"`, "[runtime]",
		`run = "{{dep.interp.prefix}}/bin/node"`, `args = ["{{pkg}}/tool"]`, "added tool 1.1.0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("add output lacks %q:\n%s", want, out)
		}
	}

	// PATH has no node, so only the configured one can run the script.
	cmd := exec.Command(m.profile("bin", "tool"))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	got, err := cmd.Output()
	must(t, err)

	if strings.TrimSpace(string(got)) != "1.1.0" {
		t.Fatalf("tool printed %q", got)
	}

	if _, err := os.Stat(m.profile("bin", "node")); err == nil {
		t.Fatal("node is in the user's profile")
	}

	_, err = m.run(t, "", "add", "npm:@scope/tool@1.0.0")
	must(t, err)

	if got := m.toolOutput(t); got != "1.0.0" {
		t.Fatalf("add @1.0.0 installed %s", got)
	}
}

func TestB129AnNPMPackageThatListsDependenciesIsInstalledWithThem(t *testing.T) {
	m := newMachine(t)
	npmServerWith(t, &m, "", true, "1.0.0", "1.1.0")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(
		filepath.Join(m.config, "config.toml"),
		[]byte(fmt.Sprintf("[runtimes]\nnode = %q\n", m.fakeNode(t))), 0o644,
	))

	out, err := m.run(t, "", "add", "npm:@scope/tool", "--yes", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, want := range []string{
		"[build]", `vendor = "npm"`, `package = "@scope/tool"`,
		`args = ["{{prefix}}/lib/node_modules/@scope/tool/tool"]`, "added tool 1.1.0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("add output lacks %q:\n%s", want, out)
		}
	}

	// npm got the version, the day it was published, and ran no scripts.
	cmd := exec.Command(m.profile("bin", "tool"))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	got, err := cmd.Output()
	must(t, err)

	want := "@scope/tool@1.1.0 before 2026-01-02T03:04:05Z scripts-off=yes"
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("tool printed %q, want %q", got, want)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), "vendor_sha256") {
		t.Fatalf("oku.lock does not pin what npm installed:\n%s", locked)
	}

	// A new machine installs the same tree from the lock.
	must(t, os.RemoveAll(m.data))

	if out, err = m.run(t, "", "sync", "--yes"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
}

func TestB127AnNPMPackageRunsTheNodeOnPathWhenNoneIsConfigured(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	out, err := m.run(t, "", "add", "npm:@scope/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if strings.Contains(out, "[runtime]") || !strings.Contains(out, "runtimes.node") {
		t.Fatalf("add did not say how to pin a node:\n%s", out)
	}

	dir := filepath.Join(m.fixtures, "path")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "node"), []byte("#!/bin/sh\nexec sh \"$@\"\n"), 0o755))

	cmd := exec.Command(m.profile("bin", "tool"))
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}

	got, err := cmd.Output()
	must(t, err)

	if strings.TrimSpace(string(got)) != "1.1.0" {
		t.Fatalf("tool printed %q", got)
	}

	_, err = m.run(t, "", "add", "npm:@scope/missing")
	if err == nil || !strings.Contains(err.Error(), "no such package") {
		t.Fatalf("want a missing package to fail, got %v", err)
	}
}

func TestB120ManifestInitReadsUniversalAndWindowsGnuAssets(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	inferServer(t, &m, map[string]string{
		hostAssetName():                         archive,
		"tool-v1.4.0-darwin-all.tar.gz":         archive,
		"tool-v1.4.0-x86_64-pc-windows-gnu.zip": archive,
		"checksums.txt.sig":                     archive,
	})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	for _, want := range []string{
		`os = "darwin", arch = "amd64"`, `os = "darwin", arch = "arm64"`,
		`os = "windows", arch = "amd64"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "sha256_url") {
		t.Fatalf("the manifest reads checksums from a signature file:\n%s", out)
	}
}

func TestB112AddAssetAndBinChooseWhatInferenceUses(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "odd", map[string]string{
		"main":   "#!/bin/sh\necho steered\n",
		"helper": script,
	})

	inferServer(t, &m, map[string]string{"tool-v1.4.0-odd.tar.gz": archive})

	_, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
	if err == nil || !strings.Contains(err.Error(), "--asset") {
		t.Fatalf("want a failure that names --asset, got %v", err)
	}

	_, err = m.run(t, "", "add", "github:owner/tool", "--asset", "*-odd.*")
	if err == nil || !strings.Contains(err.Error(), "--bin") {
		t.Fatalf("want a failure that names --bin, got %v", err)
	}

	out, err := m.run(t, "", "add", "github:owner/tool", "--asset", "*-odd.*", "--bin", "main")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got, err := exec.Command(m.profile("bin", "main")).Output()
	must(t, err)

	if strings.TrimSpace(string(got)) != "steered" {
		t.Fatalf("main printed %q", got)
	}
}

func TestB121AddInfersFromACompressedSingleBinary(t *testing.T) {
	m := newMachine(t)

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte("#!/bin/sh\necho unzipped\n"))
	must(t, err)
	must(t, gz.Close())

	binary := filepath.Join(m.fixtures, "tool.gz")
	must(t, os.WriteFile(binary, buf.Bytes(), 0o644))

	inferServer(t, &m, map[string]string{
		strings.TrimSuffix(hostAssetName(), ".tar.gz") + ".gz": binary,
	})

	out, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "unzipped" {
		t.Fatalf("tool printed %q", got)
	}
}

// collectionServer fakes the GitHub repo someone/recipes, a collection that
// holds the given manifest files by path.
func collectionServer(t *testing.T, m *machine, files map[string][]byte) {
	t.Helper()

	const commit = "6666666666666666666666666666666666666666"

	var items []string
	for path := range files {
		items = append(items, fmt.Sprintf(`{"path": %q, "type": "blob"}`, path))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, isFile := files[strings.TrimPrefix(r.URL.Path, "/raw/someone/recipes/"+commit+"/")]

		switch {
		case r.URL.Path == "/api/repos/someone/recipes/commits/HEAD":
			_, _ = w.Write([]byte(commit))
		case r.URL.Path == "/api/repos/someone/recipes/git/trees/"+commit:
			_, _ = w.Write([]byte(`{"tree": [` + strings.Join(items, ",") + `]}`))
		case isFile:
			_, _ = w.Write(file)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	m.opts.GitHubRaw = server.URL + "/raw"
}

func (m machine) describedManifest(t *testing.T, name, description string) []byte {
	t.Helper()

	body, err := os.ReadFile(m.namedManifest(t, name, name, name))
	must(t, err)

	return bytes.Replace(
		body,
		[]byte("[version]"),
		[]byte(fmt.Sprintf("description = %q\n[version]", description)),
		1,
	)
}

func TestB30SourceAliasResolvesToAManifestInTheCollection(t *testing.T) {
	m := newMachine(t)
	collectionServer(t, &m, map[string][]byte{
		"packages/tool.toml": m.describedManifest(t, "tool", "a tool"),
	})

	if _, err := m.run(t, "", "add", "core/tool"); err == nil {
		t.Fatal("an alias that is not defined resolved")
	}

	_, err := m.run(t, "", "source", "add", "core", "github:someone/recipes")
	must(t, err)

	out, err := m.run(t, "", "source", "list")
	must(t, err)

	if !strings.Contains(out, "core") || !strings.Contains(out, "github:someone/recipes") {
		t.Fatalf("source list:\n%s", out)
	}

	_, err = m.run(t, "", "add", "core/tool")
	must(t, err)

	if !exists(m.profile("bin", "tool")) {
		t.Fatal("tool is not in the profile")
	}

	// The list holds the full ref, so it works without the alias.
	listed, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(listed), `tool = "github:someone/recipes#tool"`) {
		t.Fatalf("oku.toml does not hold the expanded ref:\n%s", listed)
	}

	_, err = m.run(t, "", "source", "remove", "core")
	must(t, err)

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync after the alias was removed: %v\n%s", err, out)
	}
}

func TestB31SearchMatchesNamesAndDescriptionsInSourcesOnly(t *testing.T) {
	m := newMachine(t)
	collectionServer(t, &m, map[string][]byte{
		"packages/grepper.toml": m.describedManifest(t, "grepper", "searches text"),
		"finder.toml":           m.describedManifest(t, "finder", "a GREP for file names"),
		"packages/other.toml":   m.describedManifest(t, "other", "unrelated"),
		"oku.toml":              []byte("[packages]\ngrep-list = \"github:x/y\"\n"),
		"deep/nested/grep.toml": m.describedManifest(t, "deepgrep", "too deep to be a member"),
	})

	if _, err := m.run(t, "", "search", "grep"); err == nil {
		t.Fatal("search without sources succeeded")
	}

	_, err := m.run(t, "", "source", "add", "core", "github:someone/recipes")
	must(t, err)

	out, err := m.run(t, "", "search", "grep")
	must(t, err)

	for _, want := range []string{"core/grepper", "searches text", "core/finder"} {
		if !strings.Contains(out, want) {
			t.Fatalf("search lacks %q:\n%s", want, out)
		}
	}

	for _, reject := range []string{"other", "grep-list", "deepgrep"} {
		if strings.Contains(out, reject) {
			t.Fatalf("search shows %q:\n%s", reject, out)
		}
	}

	out, err = m.run(t, "", "search", "zzz")
	must(t, err)

	if !strings.Contains(out, "nothing in your sources") {
		t.Fatalf("search for a term with no match:\n%s", out)
	}

	out, err = m.run(t, "", "search", "grep", "--json")
	must(t, err)

	var hits []map[string]string
	must(t, json.Unmarshal([]byte(out), &hits))

	if len(hits) != 2 || hits[0]["ref"] != "core/finder" || hits[1]["description"] == "" {
		t.Fatalf("search --json: %v", hits)
	}
}

func TestB28ManifestLintRejectsSchemaMistakes(t *testing.T) {
	m := newMachine(t)
	digest := strings.Repeat("a", 64)

	valid := fmt.Sprintf(`[package]
name = "tool"
description = "a tool"
relocatable = true

[version]
value = "1.0.0"

[[artifact]]
match = { os = "linux" }
url = "https://example.com/tool-{{version}}-{{arch}}.tar.gz"
sha256 = %q
bin = ["tool"]

[build]
needs = ["cc"]
source = { git = "https://example.com/tool", tag = "{{version}}" }

[[build.step]]
run = "make PREFIX={{prefix}} -j{{jobs}} DEP={{dep.z-lib.prefix}}"
when = { os = "linux" }

[[build.step]]
run = "nmake"
when = { os = "windows" }
shell = "cmd"

[[build.step]]
install = { bin = ["tool"] }
`, digest)

	cases := map[string]struct{ from, to, want string }{
		"an unknown key": {
			`relocatable = true`,
			`relocateable = true`,
			"unknown key package.relocateable",
		},
		"a step with no type key": {
			`install = { bin = ["tool"] }`,
			`env = { A = "b" }`,
			"needs one of run",
		},
		"a step with two type keys": {
			`shell = "cmd"`,
			"shell = \"cmd\"\nvendor = \"go\"",
			"run and vendor",
		},
		"a run step reachable on windows": {
			"when = { os = \"linux\" }\n\n[[build.step]]\nrun = \"nmake\"",
			"\n[[build.step]]\nrun = \"nmake\"",
			"can run on Windows",
		},
		"an unknown template variable": {
			`{{arch}}`,
			`{{cpu}}`,
			"unknown template variable {{cpu}}",
		},
		"an artifact with no output keys": {
			`bin = ["tool"]` + "\n\n[build]",
			"\n[build]",
			"at least one of bin",
		},
	}

	write := func(name, body string) string {
		path := filepath.Join(m.fixtures, name+".toml")
		must(t, os.WriteFile(path, []byte(body), 0o644))

		return path
	}

	if out, err := m.run(t, "", "manifest", "lint", write("valid", valid)); err != nil {
		t.Fatalf("lint rejected a valid manifest: %v\n%s", err, out)
	}

	for name, c := range cases {
		if !strings.Contains(valid, c.from) {
			t.Fatalf("%s: the valid manifest lacks %q", name, c.from)
		}

		out, err := m.run(
			t,
			"",
			"manifest",
			"lint",
			write("broken", strings.Replace(valid, c.from, c.to, 1)),
		)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("lint accepted %s, or did not say %q: %v\n%s", name, c.want, err, out)
		}
	}
}

func TestB29ManifestBumpMovesVersionAndChecksums(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0", "v1.1.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, oldSum := m.archive(t, "tool-1.0.0", map[string]string{"tool": "#!/bin/sh\necho 1.0.0\n"})
	_, newSum := m.archive(t, "tool-1.1.0", map[string]string{"tool": "#!/bin/sh\necho 1.1.0\n"})

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"# my tool\n[package]\nname = \"tool\"\n[version]\nvalue = \"1.0.0\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{version}}.tar.gz\"\nsha256 = %q\nbin = [\"tool\"]\n",
		m.fixtures, oldSum,
	)), 0o644))

	out, err := m.run(
		t,
		"",
		"manifest",
		"bump",
		path,
		"--repo",
		"owner/tool",
		"--strip-prefix",
		"v",
	)
	if err != nil {
		t.Fatalf("bump: %v\n%s", err, out)
	}

	bumped, err := os.ReadFile(path)
	must(t, err)

	for _, want := range []string{"# my tool", `value = "1.1.0"`, newSum} {
		if !strings.Contains(string(bumped), want) {
			t.Fatalf("the bumped manifest lacks %q:\n%s", want, bumped)
		}
	}

	_, err = m.run(t, "", "add", path)
	must(t, err)

	if got := m.toolOutput(t); got != "1.1.0" {
		t.Fatalf("the bumped manifest installs %s", got)
	}

	out, err = m.run(t, "", "manifest", "bump", path, "--repo", "owner/tool", "--strip-prefix", "v")
	must(t, err)

	if !strings.Contains(out, "already at 1.1.0") {
		t.Fatalf("a second bump:\n%s", out)
	}
}

func TestB119ManifestBumpReadsReleasesFromAGitLabRef(t *testing.T) {
	m := newMachine(t)

	_, oldSum := m.archive(t, "tool-1.0.0", map[string]string{"tool": "#!/bin/sh\necho 1.0.0\n"})
	_, newSum := m.archive(t, "tool-1.1.0", map[string]string{"tool": "#!/bin/sh\necho 1.1.0\n"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/projects/group%2Fsub%2Ftool/releases" {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write([]byte(`[{"tag_name": "v1.1.0"}, {"tag_name": "v1.0.0"}]`))
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	must(t, err)

	client := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteHost{host: "gitlab.com", server: target}

	t.Cleanup(func() { http.DefaultClient.Transport = client })

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n[version]\nvalue = \"1.0.0\"\n"+
			"[[artifact]]\nurl = \"file://%s/tool-{{version}}.tar.gz\"\nsha256 = %q\nbin = [\"tool\"]\n",
		m.fixtures, oldSum,
	)), 0o644))

	out, err := m.run(
		t, "", "manifest", "bump", path, "--repo", "gitlab:group/sub/tool", "--strip-prefix", "v",
	)
	if err != nil {
		t.Fatalf("bump: %v\n%s", err, out)
	}

	bumped, err := os.ReadFile(path)
	must(t, err)

	for _, want := range []string{`value = "1.1.0"`, newSum} {
		if !strings.Contains(string(bumped), want) {
			t.Fatalf("the bumped manifest lacks %q:\n%s", want, bumped)
		}
	}
}

// buildManifest writes a manifest for "tool" that has a prebuilt artifact
// printing "prebuilt" and a [build] made of steps. extra goes into [build].
func (m machine) buildManifest(t *testing.T, withArtifact bool, extra, steps string) string {
	t.Helper()

	artifact := ""

	if withArtifact {
		archive, sum := m.archive(
			t,
			"prebuilt",
			map[string]string{"tool": "#!/bin/sh\necho prebuilt\n"},
		)
		artifact = fmt.Sprintf(
			"[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nbin = [\"tool\"]\n",
			archive,
			sum,
		)
	}

	path := filepath.Join(m.fixtures, "built.toml")
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"tool\"\n[version]\nvalue = \"1.0.0\"\n"+artifact+
			"[build]\n"+extra+"\n"+steps,
	), 0o644))

	return path
}

const (
	writeTool   = "[[build.step]]\nrun = \"printf '#!/bin/sh\\\\necho built {{version}}\\\\n' > tool\"\nshell = \"sh\"\n"
	installTool = "[[build.step]]\ninstall = { bin = [\"tool\"] }\n"
)

func yes() *bool {
	v := true

	return &v
}

func TestB35BuildRunsStepsInOrderAndFromSourceForcesIt(t *testing.T) {
	m := newMachine(t)
	ref := m.buildManifest(t, true, `needs = ["sh"]`, writeTool+installTool)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if got := m.toolOutput(t); got != "prebuilt" {
		t.Fatalf("add used %q, want the prebuilt artifact", got)
	}

	out, err := m.run(t, "", "add", ref, "--from-source", "--yes")
	if err != nil {
		t.Fatalf("add --from-source: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "built 1.0.0" {
		t.Fatalf("add --from-source installed %q", got)
	}

	// The lock remembers the strategy, so a new machine builds too.
	must(t, os.RemoveAll(m.data))

	_, err = m.run(t, "", "sync", "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "built 1.0.0" {
		t.Fatalf("sync installed %q, want the build", got)
	}

	noArtifact := m.buildManifest(t, false, "", writeTool+installTool)

	_, err = m.run(t, "", "add", noArtifact, "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "built 1.0.0" {
		t.Fatalf("a manifest with no artifact installed %q", got)
	}
}

func TestB36MissingNeedsToolFailsBeforeAnyStep(t *testing.T) {
	m := newMachine(t)
	marker := filepath.Join(m.fixtures, "ran")
	ref := m.buildManifest(t, false, `needs = ["sh", "no-such-tool-xyz"]`,
		fmt.Sprintf("[[build.step]]\nrun = \"touch %s\"\nshell = \"sh\"\n", marker)+installTool)

	_, err := m.run(t, "", "add", ref, "--yes")
	if err == nil || !strings.Contains(err.Error(), "no-such-tool-xyz") {
		t.Fatalf("want an error naming the missing tool, got %v", err)
	}

	if exists(marker) {
		t.Fatal("a step ran before the needs check")
	}
}

func TestB41RunStepsNeedApprovalOncePerManifestHash(t *testing.T) {
	m := newMachine(t)
	m.opts.Interactive = yes()

	ref := m.buildManifest(t, false, "", writeTool+installTool)

	out, err := m.run(t, "n\n", "add", ref)
	if err == nil || !strings.Contains(out, "printf") {
		t.Fatalf("want the commands shown and the build refused, got %v\n%s", err, out)
	}

	if len(m.storeEntries(t)) != 0 {
		t.Fatal("something was built without approval")
	}

	out, err = m.run(t, "y\n", "add", ref)
	if err != nil {
		t.Fatalf("add after approving: %v\n%s", err, out)
	}

	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	out, err = m.run(t, "", "add", ref)
	if err != nil || strings.Contains(out, "[y/N]") {
		t.Fatalf("the same manifest hash was asked twice: %v\n%s", err, out)
	}

	body, err := os.ReadFile(ref)
	must(t, err)
	must(t, os.WriteFile(ref, append(body, []byte("\n# changed\n")...), 0o644))

	out, _ = m.run(t, "n\n", "add", ref)
	if !strings.Contains(out, "[y/N]") {
		t.Fatalf("a changed manifest was not asked again:\n%s", out)
	}
}

func TestB42NonInteractiveRunsRefuseUnapprovedStepsUnlessYes(t *testing.T) {
	m := newMachine(t)
	ref := m.buildManifest(t, false, "", writeTool+installTool)

	_, err := m.run(t, "y\n", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a refusal that names --yes, got %v", err)
	}

	if _, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add --yes: %v", err)
	}
}

func TestB43StepWithNonMatchingWhenIsSkipped(t *testing.T) {
	m := newMachine(t)
	ref := m.buildManifest(
		t,
		false,
		"",
		"[[build.step]]\nrun = \"exit 7\"\nshell = \"sh\"\nwhen = { os = \"plan9\" }\n"+writeTool+installTool,
	)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("a step for another OS ran: %v\n%s", err, out)
	}
}

func TestB44FailingStepAbortsAndLeavesStoreAndProfileUnchanged(t *testing.T) {
	m := newMachine(t)
	good := m.namedManifest(t, "good", "good", "good")

	_, err := m.run(t, "", "add", good)
	must(t, err)

	before := m.storeEntries(t)
	generation, err := os.Readlink(m.profile())
	must(t, err)

	ref := m.buildManifest(
		t,
		false,
		"",
		writeTool+"[[build.step]]\nrun = \"echo compiler exploded; exit 3\"\nshell = \"sh\"\n"+installTool,
	)

	_, err = m.run(t, "", "add", ref, "--yes")
	if err == nil || !strings.Contains(err.Error(), "build.step[1]") ||
		!strings.Contains(err.Error(), "compiler exploded") {
		t.Fatalf("want the step index and its output, got %v", err)
	}

	after, err := os.Readlink(m.profile())
	must(t, err)

	if got := m.storeEntries(t); !slices.Equal(got, before) || after != generation {
		t.Fatalf("store %v or generation %s changed", got, after)
	}
}

// dataDep writes a discovered-version package "data" whose build installs
// share/data.txt holding its version.
func (m machine) dataDep(t *testing.T) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "data.toml")
	must(t, os.WriteFile(path, []byte(
		"[package]\nname = \"data\"\n"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\nstrip_prefix = \"v\"\n"+
			"[build]\n[[build.step]]\nrun = \"echo {{version}} > data.txt\"\nshell = \"sh\"\n"+
			"[[build.step]]\ninstall = { share = [\"data.txt\"], bin = [\"data.txt\"] }\n",
	), 0o644))

	return path
}

// dataUser writes a package whose program prints the data dep's file, read at
// build time from {{dep.data.prefix}}.
func (m machine) dataUser(t *testing.T, name, constraint string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, name+".toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = %q\n[version]\nvalue = \"1.0.0\"\n"+
			"[build]\ndeps = [{ ref = \"./data.toml\", version = %q }]\n"+
			"[[build.step]]\nrun = \"printf '#!/bin/sh\\\\ncat %%s\\\\n' {{dep.data.prefix}}/share/data.txt > %s\"\nshell = \"sh\"\n"+
			"[[build.step]]\ninstall = { bin = [%q] }\n",
		name, constraint, name, name,
	)), 0o644))

	return path
}

func (m machine) output(t *testing.T, bin string) string {
	t.Helper()

	out, err := exec.Command(m.profile("bin", bin)).Output()
	must(t, err)

	return strings.TrimSpace(string(out))
}

func TestB37BuildFindsDepHeadersAndLibrariesAndTheResultRunsAnywhere(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("needs a C compiler")
	}

	m := newMachine(t)

	shared := "cc -shared -fPIC -o libgreet.so greet.c"
	if runtime.GOOS == "darwin" {
		shared = "cc -dynamiclib -o libgreet.dylib -install_name {{prefix}}/lib/libgreet.dylib greet.c"
	}

	libFile := map[string]string{"darwin": "libgreet.dylib"}[runtime.GOOS]
	if libFile == "" {
		libFile = "libgreet.so"
	}

	must(t, os.WriteFile(filepath.Join(m.fixtures, "greet.toml"), []byte(fmt.Sprintf(`[package]
name = "greet"
[version]
value = "1.0.0"
[build]
needs = ["cc"]
[[build.step]]
run = """
printf 'const char *greet(void);\\n' > greet.h
printf 'const char *greet(void) { return "hello from libgreet"; }\\n' > greet.c
printf 'Name: greet\\nVersion: 1.0.0\\nDescription: d\\nLibs: -lgreet\\n' > greet.pc
%s
"""
shell = "sh"
[[build.step]]
install = { lib = [%q], include = ["greet.h"] }
[[build.step]]
copy = { from = "greet.pc", to = "lib/pkgconfig/greet.pc" }
`, shared, libFile)), 0o644))

	app := filepath.Join(m.fixtures, "app.toml")
	must(t, os.WriteFile(app, []byte(`[package]
name = "app"
[version]
value = "1.0.0"
[build]
needs = ["cc"]
deps = ["./greet.toml"]
[[build.step]]
run = """
printf '#include <stdio.h>\\n#include <greet.h>\\nint main(void) { puts(greet()); return 0; }\\n' > main.c
test -f "${PKG_CONFIG_PATH%%:*}/greet.pc"
cc -o app main.c -lgreet
"""
shell = "sh"
[[build.step]]
install = { bin = ["app"] }
`), 0o644))

	out, err := m.run(t, "", "add", app, "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	cmd := exec.Command(m.profile("bin", "app"))
	cmd.Dir = t.TempDir()
	cmd.Env = []string{}

	got, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(got)) != "hello from libgreet" {
		t.Fatalf("app from another directory: %v\n%s", err, got)
	}
}

func TestUpdateLooksUpADepThatPackagesShareOnce(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	m.dataDep(t)

	for _, name := range []string{"first", "second"} {
		_, err := m.run(t, "", "add", m.dataUser(t, name, ""), "--yes")
		must(t, err)
	}

	out, err := m.run(t, "", "update", "--yes")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := strings.Count(out, "looking up the versions of owner/tool"); got != 1 {
		t.Fatalf("update looked up the shared dep %d times:\n%s", got, out)
	}
}

func TestB38DepsStayOutOfTheProfileAndWhyNamesTheirUsers(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	m.dataDep(t)

	_, err := m.run(t, "", "add", m.dataUser(t, "reader", ""), "--yes")
	must(t, err)

	if exists(m.profile("bin", "data.txt")) {
		t.Fatal("the dep's bin is linked into the profile")
	}

	out, err := m.run(t, "", "list")
	must(t, err)

	if strings.Contains(out, "data ") {
		t.Fatalf("list shows the dep:\n%s", out)
	}

	out, err = m.run(t, "", "why", "data")
	must(t, err)

	if !strings.Contains(out, "data 1.0.0 is needed by reader") {
		t.Fatalf("why data:\n%s", out)
	}

	if _, err := m.run(t, "", "why", "nothing"); err == nil {
		t.Fatal("why succeeded for a package nothing uses")
	}

	// gc keeps a dep that a generation's package still uses.
	_, err = m.run(t, "", "gc")
	must(t, err)

	if got := m.output(t, "reader"); got != "1.0.0" {
		t.Fatalf("reader after gc printed %q", got)
	}
}

func TestB39TwoPackagesUseDifferentVersionsOfOneDep(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0", "v2.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	m.dataDep(t)

	for _, ref := range []string{m.dataUser(t, "old", "<2"), m.dataUser(t, "new", ">=2")} {
		if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
			t.Fatalf("add %s: %v\n%s", ref, err, out)
		}
	}

	if old, latest := m.output(t, "old"), m.output(t, "new"); old != "1.0.0" || latest != "2.0.0" {
		t.Fatalf("old uses %s and new uses %s", old, latest)
	}

	// A new machine gets the same two dep versions from the lock.
	server.tags = append(server.tags, "v1.5.0", "v3.0.0")

	must(t, os.RemoveAll(m.data))

	_, err := m.run(t, "", "sync", "--yes")
	must(t, err)

	if old, latest := m.output(t, "old"), m.output(t, "new"); old != "1.0.0" || latest != "2.0.0" {
		t.Fatalf("after sync old uses %s and new uses %s", old, latest)
	}
}

func TestB40UnsatisfiableDepConstraintNamesItAndTheVersionsFound(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0", "v2.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	m.dataDep(t)

	_, err := m.run(t, "", "add", m.dataUser(t, "picky", ">=9"), "--yes")
	if err == nil {
		t.Fatal("add succeeded with a constraint nothing satisfies")
	}

	for _, want := range []string{">=9", "2.0.0", "1.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error lacks %q: %v", want, err)
		}
	}
}

// probeManifest writes a package whose build tries to fetch url and to read
// secret, and installs what it learned as share/probe.txt.
func (m machine) probeManifest(t *testing.T, url, secret string, network bool) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "probe.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(`[package]
name = "probe"
[version]
value = "1.0.0"
[build]
needs = ["curl"]
[[build.step]]
run = """
curl -sf -m 5 -o /dev/null %s && echo network=open > probe.txt || echo network=blocked > probe.txt
cat %s >/dev/null 2>&1 && echo home=readable >> probe.txt || echo home=hidden >> probe.txt
env | cut -d= -f1 | sort | tr '\\n' ' ' >> probe.txt
"""
shell = "sh"
network = %t
env = { EXTRA = "1" }
[[build.step]]
install = { share = ["probe.txt"] }
`, url, secret, network)), 0o644))

	return path
}

func (m machine) probeResult(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(m.profile("share", "probe.txt"))
	must(t, err)

	return string(body)
}

func sandboxedMachine(t *testing.T) (machine, string, string) {
	t.Helper()

	if ok, why := sandbox.Available(); !ok {
		t.Skip("no sandbox on this host: " + why)
	}

	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("needs curl")
	}

	m := newMachine(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	secret := filepath.Join(home, "secret.txt")
	must(t, os.WriteFile(secret, []byte("hunter2"), 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	return m, server.URL, secret
}

func TestB50RunStepCannotReachTheNetworkOrReadHome(t *testing.T) {
	m, url, secret := sandboxedMachine(t)

	out, err := m.run(t, "", "add", m.probeManifest(t, url, secret, false), "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got := m.probeResult(t)
	if !strings.Contains(got, "network=blocked") || !strings.Contains(got, "home=hidden") {
		t.Fatalf("the run step saw:\n%s", got)
	}

	if strings.Contains(out, "without the sandbox") {
		t.Fatalf("oku reported an unsandboxed build:\n%s", out)
	}
}

func TestB53NetworkStepIsShownInThePromptAndMarksThePackageImpure(t *testing.T) {
	m, url, secret := sandboxedMachine(t)
	m.opts.Interactive = yes()

	out, err := m.run(t, "y\n", "add", m.probeManifest(t, url, secret, true))
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "wants network") {
		t.Fatalf("the prompt does not say the step wants network:\n%s", out)
	}

	if got := m.probeResult(
		t,
	); !strings.Contains(got, "network=open") ||
		!strings.Contains(got, "home=hidden") {
		t.Fatalf("the run step saw:\n%s", got)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), "impure = true") {
		t.Fatalf("oku.lock does not mark the build impure:\n%s", locked)
	}

	out, err = m.run(t, "", "info", "probe")
	must(t, err)

	if !strings.Contains(out, "impure") {
		t.Fatalf("info does not mark the package impure:\n%s", out)
	}
}

func TestB54BuildEnvironmentHoldsOnlyOkuVariables(t *testing.T) {
	m, url, secret := sandboxedMachine(t)

	t.Setenv("MY_TOKEN", "leak-me")

	_, err := m.run(t, "", "add", m.probeManifest(t, url, secret, false), "--yes")
	must(t, err)

	lines := strings.Split(strings.TrimSpace(m.probeResult(t)), "\n")
	names := strings.Fields(lines[len(lines)-1])

	allowed := []string{
		"EXTRA", "HOME", "OKU_JOBS", "OKU_PREFIX", "OKU_SRC", "PATH", "TMPDIR",
		"PWD", "OLDPWD", "SHLVL", "_", "__CF_USER_TEXT_ENCODING",
		// The link environment. On macOS it holds the pkg-config files of the OS
		// even for a build with no deps.
		"PKG_CONFIG_PATH",
	}

	for _, name := range names {
		if !slices.Contains(allowed, name) {
			t.Errorf("the build saw %s", name)
		}
	}

	if !slices.Contains(names, "EXTRA") || !slices.Contains(names, "OKU_PREFIX") {
		t.Fatalf("the build environment is %v", names)
	}
}

func TestB52VendorOutputIsPinnedAndAMismatchFailsTheBuild(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs go")
	}

	m := newMachine(t)

	// Go marks its module cache read-only, and the first run step below does the
	// same in the build's HOME. A build directory that oku fails to delete would
	// stay in here.
	tmp := filepath.Join(m.fixtures, "tmp")
	must(t, os.Mkdir(tmp, 0o755))
	t.Setenv("TMPDIR", tmp)

	// The vendored module comes from a local directory, so the test needs no
	// network. Changing it later stands in for upstream changing a package.
	lib := filepath.Join(m.fixtures, "greet")
	must(t, os.MkdirAll(lib, 0o755))
	must(
		t,
		os.WriteFile(
			filepath.Join(lib, "go.mod"),
			[]byte("module example.com/greet\n\ngo 1.21\n"),
			0o644,
		),
	)

	writeLib := func(text string) {
		must(t, os.WriteFile(filepath.Join(lib, "greet.go"), []byte(fmt.Sprintf(
			"package greet\n\nfunc Text() string { return %q }\n", text,
		)), 0o644))
	}
	writeLib("vendored hello")

	ref := filepath.Join(m.fixtures, "gotool.toml")
	must(t, os.WriteFile(ref, []byte(fmt.Sprintf(`[package]
name = "gotool"
[version]
value = "1.0.0"
[build]
needs = ["go"]
[[build.step]]
run = """
mkdir -p "$HOME/cache/mod" && touch "$HOME/cache/mod/file" && chmod -R a-w "$HOME/cache"
printf 'module example.com/gotool\\n\\ngo 1.21\\n\\nrequire example.com/greet v0.0.0\\n\\nreplace example.com/greet => %s\\n' > go.mod
printf 'package main\\n\\nimport (\\n\\t"fmt"\\n\\n\\t"example.com/greet"\\n)\\n\\nfunc main() { fmt.Println(greet.Text()) }\\n' > main.go
"""
shell = "sh"
[[build.step]]
vendor = "go"
[[build.step]]
run = "go build -mod=vendor -o gotool ."
shell = "sh"
env = { GOTOOLCHAIN = "local", GOFLAGS = "-buildvcs=false" }
[[build.step]]
install = { bin = ["gotool"] }
`, lib)), 0o644))

	out, err := m.run(t, "", "add", ref, "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.output(t, "gotool"); got != "vendored hello" {
		t.Fatalf("gotool printed %q", got)
	}

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(locked), "vendor_sha256 = '") {
		t.Fatalf("oku.lock does not pin the vendor output:\n%s", locked)
	}

	// The same manifest now downloads different code, on a machine with an empty
	// store.
	writeLib("something else")
	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	_, err = m.run(t, "", "sync", "--yes")
	if err == nil || !strings.Contains(err.Error(), "vendored packages changed") {
		t.Fatalf("want sync to fail on changed vendor output, got %v", err)
	}

	if got := m.storeEntries(t); len(got) != 0 {
		t.Fatalf("the mismatched build was kept: %v", got)
	}

	out, err = m.run(t, "", "update", "--yes")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := m.output(t, "gotool"); got != "something else" {
		t.Fatalf("gotool after update printed %q", got)
	}

	if left, _ := filepath.Glob(filepath.Join(tmp, "oku-build-*")); len(left) != 0 {
		t.Fatalf("oku left build directories behind: %v", left)
	}
}

func TestB55ManifestTestBuildsInAThrowawayStoreAndReportsTheFailingStep(t *testing.T) {
	m := newMachine(t)
	good := m.buildManifest(t, true, "", writeTool+installTool)

	out, err := m.run(t, "", "manifest", "test", good, "--yes")
	if err != nil {
		t.Fatalf("manifest test: %v\n%s", err, out)
	}

	for _, want := range []string{"[1/2] run", "[2/2] install", "ok", "tool 1.0.0 works", "(build)", "bin/tool"} {
		if !strings.Contains(out, want) {
			t.Fatalf("manifest test output lacks %q:\n%s", want, out)
		}
	}

	bad := filepath.Join(m.fixtures, "bad.toml")

	body, err := os.ReadFile(good)
	must(t, err)
	must(t, os.WriteFile(bad, bytes.Replace(
		body,
		[]byte(installTool),
		[]byte("[[build.step]]\nrun = \"exit 4\"\nshell = \"sh\"\n"+installTool),
		1,
	), 0o644))

	out, err = m.run(t, "", "manifest", "test", bad, "--yes")
	if err == nil || !strings.Contains(out, "[2/3] run") || !strings.Contains(out, "FAILED") ||
		!strings.Contains(err.Error(), "build.step[1]") {
		t.Fatalf("want the failing step reported, got %v\n%s", err, out)
	}

	// The user's own store, profile, list and lock were never touched.
	for _, path := range []string{m.data, filepath.Join(m.config, "oku.toml"), filepath.Join(m.config, "oku.lock")} {
		if exists(path) {
			t.Fatalf("manifest test wrote %s", path)
		}
	}
}

func TestB60CommandsActOnTheProjectListUnlessGlobal(t *testing.T) {
	m := newMachine(t)
	first := m.namedManifest(t, "first", "first", "first")
	second := m.namedManifest(t, "second", "second", "second")

	project := filepath.Join(m.fixtures, "work", "api")
	deep := filepath.Join(project, "src", "handlers")
	must(t, os.MkdirAll(deep, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("# api tools\n"), 0o644))

	m.opts.WorkDir = deep

	_, err := m.run(t, "", "add", first)
	must(t, err)

	_, err = m.run(t, "", "add", second, "--global")
	must(t, err)

	listed, err := os.ReadFile(filepath.Join(project, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(listed), "first =") ||
		strings.Contains(string(listed), "second =") ||
		!strings.Contains(string(listed), "# api tools") {
		t.Fatalf("the project list holds:\n%s", listed)
	}

	if !exists(filepath.Join(project, "oku.lock")) {
		t.Fatal("the project has no oku.lock beside its list")
	}

	global, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if strings.Contains(string(global), "first =") ||
		!strings.Contains(string(global), "second =") {
		t.Fatalf("the global list holds:\n%s", global)
	}

	out, err := m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "first") || strings.Contains(out, "second ") {
		t.Fatalf("list inside the project:\n%s", out)
	}

	out, err = m.run(t, "", "list", "--global")
	must(t, err)

	if strings.Contains(out, "first ") || !strings.Contains(out, "second") {
		t.Fatalf("list --global inside the project:\n%s", out)
	}

	if exists(m.profile("bin", "first")) || !exists(m.profile("bin", "second")) {
		t.Fatal("the global profile does not hold exactly the global package")
	}

	// A new checkout of the project gets its packages back from list and lock.
	must(t, os.RemoveAll(m.data))

	out, err = m.run(t, "", "sync")
	if err != nil || !strings.Contains(out, "profile now holds 1 package") {
		t.Fatalf("sync inside the project: %v\n%s", err, out)
	}

	_, err = m.run(t, "", "remove", "first")
	must(t, err)

	listed, err = os.ReadFile(filepath.Join(project, "oku.toml"))
	must(t, err)

	if strings.Contains(string(listed), "first =") {
		t.Fatalf("remove left first in the project list:\n%s", listed)
	}

	// Outside the project the same commands act on the global list.
	m.opts.WorkDir = m.fixtures

	_, err = m.run(t, "", "sync")
	must(t, err)

	out, err = m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "second") || strings.Contains(out, "first ") {
		t.Fatalf("list outside the project:\n%s", out)
	}
}

// envManifest writes a package that ships one program and sets one variable.
func (m machine) envManifest(t *testing.T, name, variable string) string {
	t.Helper()

	path := m.namedManifest(t, name, name, name)

	body, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, append(body, []byte(fmt.Sprintf(
		"\n[env]\n%s = \"{{prefix}}/share/%s\"\n", variable, name,
	))...), 0o644))

	return path
}

// hookProject makes a project that holds one package and returns its directory.
func (m *machine) hookProject(t *testing.T) string {
	t.Helper()

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))

	m.opts.WorkDir = project

	_, err := m.run(t, "", "add", m.envManifest(t, "ptool", "PTOOL_HOME"))
	must(t, err)

	return project
}

// apply feeds the exports of an "oku env" run back into the test's environment,
// the way a shell would, and returns the text oku printed.
func (m machine) apply(t *testing.T) string {
	t.Helper()

	out, err := m.run(t, "", "env", "--shell", "bash")
	must(t, err)

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "export "):
			name, value, _ := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			t.Setenv(name, strings.Trim(value, "'"))
		case strings.HasPrefix(line, "unset "):
			must(t, os.Unsetenv(strings.TrimPrefix(line, "unset ")))
		}
	}

	return out
}

func TestB61EnteringAnAllowedSyncedProjectAppliesItAndLeavingRestores(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	m.apply(t)

	bin := filepath.Dir(m.projectBin(t, "ptool"))
	if got := os.Getenv("PATH"); got != bin+":/usr/bin:/bin" {
		t.Fatalf("PATH inside the project is %s", got)
	}

	if !strings.HasSuffix(os.Getenv("PTOOL_HOME"), "/share/ptool") {
		t.Fatalf("PTOOL_HOME inside the project is %q", os.Getenv("PTOOL_HOME"))
	}

	if out := m.apply(t); strings.TrimSpace(out) != "" {
		t.Fatalf("a second prompt in the same directory changed something:\n%s", out)
	}

	m.opts.WorkDir = filepath.Dir(project)
	m.apply(t)

	if got := os.Getenv("PATH"); got != "/usr/bin:/bin" {
		t.Fatalf("PATH after leaving is %s", got)
	}

	if _, set := os.LookupEnv("PTOOL_HOME"); set {
		t.Fatal("PTOOL_HOME survived leaving the project")
	}
}

// projectBin returns the path of a program in the project profile.
func (m machine) projectBin(t *testing.T, name string) string {
	t.Helper()

	found, err := filepath.Glob(
		filepath.Join(m.data, "profiles", "project-*", "current", "bin", name),
	)
	must(t, err)

	if len(found) != 1 {
		t.Fatalf("want one project profile with %s, found %v", name, found)
	}

	return found[0]
}

func TestB62ProjectThatIsNotAllowedChangesNothingAndHints(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	m.hookProject(t)

	out := m.apply(t)
	if !strings.Contains(out, "oku allow") {
		t.Fatalf("no hint to run oku allow:\n%s", out)
	}

	if os.Getenv("PATH") != "/usr/bin:/bin" || os.Getenv("PTOOL_HOME") != "" {
		t.Fatal("a project that is not allowed changed the environment")
	}

	if out := m.apply(t); strings.Contains(out, "oku allow") {
		t.Fatalf("the hint is repeated before every prompt:\n%s", out)
	}
}

func TestB63EditingAnAllowedListRevokesTheAllow(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	m.apply(t)

	list := filepath.Join(project, "oku.toml")
	body, err := os.ReadFile(list)
	must(t, err)
	must(t, os.WriteFile(list, append(body, []byte("# edited\n")...), 0o644))

	out := m.apply(t)
	if !strings.Contains(out, "oku allow") || os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatalf("an edited list stayed active, PATH=%s:\n%s", os.Getenv("PATH"), out)
	}

	_, err = m.run(t, "", "allow")
	must(t, err)

	m.apply(t)

	if !strings.Contains(os.Getenv("PATH"), "project-") {
		t.Fatalf("allowing again did not activate the project, PATH=%s", os.Getenv("PATH"))
	}

	_, err = m.run(t, "", "deny")
	must(t, err)

	m.apply(t)

	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatalf("deny left the project active, PATH=%s", os.Getenv("PATH"))
	}
}

func TestB64ProfileBehindItsLockChangesNothingAndHints(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	// A teammate's commit changed the lock, and nobody ran sync here yet.
	lockPath := filepath.Join(project, "oku.lock")
	locked, err := os.ReadFile(lockPath)
	must(t, err)
	must(t, os.WriteFile(lockPath, append(locked, []byte("\n# newer\n")...), 0o644))

	out := m.apply(t)
	if !strings.Contains(out, "oku sync") || os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatalf("a profile behind its lock was applied, PATH=%s:\n%s", os.Getenv("PATH"), out)
	}
}

func TestB65HookUsesNoNetworkAndRunsNoManifestCode(t *testing.T) {
	m := newMachine(t)
	hits := 0

	server := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }),
	)
	defer server.Close()

	m.opts.GitHubAPI, m.opts.GitHubRaw = server.URL, server.URL

	project := m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	// The manifest the project came from is gone, and so is the cache.
	must(t, os.RemoveAll(m.cache))
	must(t, os.Remove(filepath.Join(m.fixtures, "ptool.toml")))

	m.opts.WorkDir = project

	if out := m.apply(t); !strings.Contains(out, "PTOOL_HOME") {
		t.Fatalf("the hook needed more than local state:\n%s", out)
	}

	if hits != 0 {
		t.Fatalf("the hook made %d network requests", hits)
	}
}

func TestB66EnvPrintsTheExportsForEachShell(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	for shell, want := range map[string]string{"bash": "export PTOOL_HOME='", "zsh": "export PTOOL_HOME='", "fish": "set -gx PTOOL_HOME '", "pwsh": "$env:PTOOL_HOME = '"} {
		out, err := m.run(t, "", "env", "--shell", shell)
		must(t, err)

		if !strings.Contains(out, want) || !strings.Contains(out, "PATH") {
			t.Fatalf("env --shell %s:\n%s", shell, out)
		}
	}

	if _, err := m.run(t, "", "env", "--shell", "tcsh"); err == nil {
		t.Fatal("env accepted a shell it cannot write for")
	}
}

func TestB67ProjectProgramsShadowGlobalOnes(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.namedManifest(t, "gtool", "gtool", "gtool"))
	must(t, err)

	t.Setenv("PATH", m.profile("bin")+":/usr/bin:/bin")
	m.hookProject(t)

	_, err = m.run(t, "", "allow")
	must(t, err)

	m.apply(t)

	entries := filepath.SplitList(os.Getenv("PATH"))
	if !strings.Contains(entries[0], "project-") || entries[1] != m.profile("bin") {
		t.Fatalf("the project is not ahead of the global profile: %v", entries)
	}
}

func TestB68GlobalPackageEnvIsExportedInEveryShell(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	_, err := m.run(t, "", "add", m.envManifest(t, "gtool", "GTOOL_HOME"))
	must(t, err)

	m.apply(t)

	if !strings.HasSuffix(os.Getenv("GTOOL_HOME"), "/share/gtool") {
		t.Fatalf("GTOOL_HOME is %q outside any project", os.Getenv("GTOOL_HOME"))
	}

	_, err = m.run(t, "", "remove", "gtool")
	must(t, err)

	m.apply(t)

	if _, set := os.LookupEnv("GTOOL_HOME"); set {
		t.Fatal("GTOOL_HOME stayed after the package was removed")
	}
}

func TestB128EnvPkgNamesTheFilesOfAnArtifact(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	path := m.manifest(t, "jdk", map[string]string{
		"jdk": script, "lib/jvm/release": "a jvm",
	}, `bin = ["jdk"]`)

	body, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(
		path, append(body, []byte("\n[env]\nJDK_HOME = \"{{pkg}}/lib/jvm\"\n")...), 0o644,
	))

	_, err = m.run(t, "", "add", path)
	must(t, err)

	m.apply(t)

	if _, err := os.Stat(filepath.Join(os.Getenv("JDK_HOME"), "release")); err != nil {
		t.Fatalf(
			"JDK_HOME is %q, which does not hold the download's files: %v",
			os.Getenv("JDK_HOME"),
			err,
		)
	}
}

func TestB99UninstallPrintsTheHookLineToDelete(t *testing.T) {
	m := newMachine(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	must(
		t,
		os.WriteFile(
			filepath.Join(home, ".zshrc"),
			[]byte("# mine\n"+shellhook.Line("zsh", "$HOME/.local/bin/oku")+"\n"),
			0o644,
		),
	)

	out, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if !strings.Contains(out, ".zshrc") || !strings.Contains(out, "hook zsh") {
		t.Fatalf("uninstall did not name the hook line:\n%s", out)
	}
}

func TestB100StaleHookLineWithoutOkuStartsCleanly(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		line := shellhook.Line(shell, "$HOME/.local/bin/oku")

		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}

		cmd := exec.Command(path, "-c", line+"; echo started")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}

		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "started" {
			t.Fatalf("%s with a stale hook line: %v\n%s", shell, err, out)
		}
	}
}

func TestB69EnvThatControlsOtherProgramsIsRejected(t *testing.T) {
	m := newMachine(t)
	path := m.namedManifest(t, "tool", "tool", "tool")

	body, err := os.ReadFile(path)
	must(t, err)

	for _, name := range []string{"PATH", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "BASH_ENV", "OKU_HOOK_PATH"} {
		must(
			t,
			os.WriteFile(
				path,
				append(body, []byte(fmt.Sprintf("\n[env]\n%s = \"/evil\"\n", name))...),
				0o644,
			),
		)

		_, err := m.run(t, "", "add", path)
		if err == nil || !strings.Contains(err.Error(), "env."+name) {
			t.Fatalf("a manifest that sets %s was accepted: %v", name, err)
		}

		if out, err := m.run(t, "", "manifest", "lint", path); err == nil {
			t.Fatalf("lint accepted a manifest that sets %s:\n%s", name, out)
		}
	}

	if len(m.storeEntries(t)) != 0 {
		t.Fatal("a rejected manifest left something in the store")
	}
}

// desktopManifest writes a package that ships a program, a macOS app bundle, a
// Linux launcher and a font.
func (m machine) desktopManifest(t *testing.T) string {
	t.Helper()

	return m.manifest(
		t,
		"foo",
		map[string]string{
			"Foo.app/Contents/MacOS/foo":  script,
			"Foo.app/Contents/Info.plist": "<plist/>",
			"fonts/Test.ttf":              "not really a font",
		},
		"bin = [\"Foo.app/Contents/MacOS/foo\"]\napp = [\"Foo.app\"]\nfont = [\"fonts/Test.ttf\"]\n"+
			"[[app]]\nname = \"Foo\"\nexec = \"bin/foo\"\n",
	)
}

// exposedPaths returns where this OS shows the test package's app and font.
func (m machine) exposedPaths() (app, font string) {
	home := os.Getenv("HOME")
	dataHome := filepath.Dir(m.data)

	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Applications", "Foo.app", "Contents", "MacOS", "foo"),
			filepath.Join(home, "Library", "Fonts", "Test.ttf")
	}

	return filepath.Join(dataHome, "applications", "oku-foo.desktop"),
		filepath.Join(dataHome, "fonts", "oku", "Test.ttf")
}

func TestB70AppAppearsForTheUserAndRemoveAndRollbackTakeItAway(t *testing.T) {
	m := newMachine(t)
	ref := m.desktopManifest(t)
	app, _ := m.exposedPaths()

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	info, err := os.Lstat(app)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the app is not a real file at %s: %v", app, err)
	}

	if runtime.GOOS != "darwin" {
		entry, err := os.ReadFile(app)
		must(t, err)

		if !strings.Contains(string(entry), "Name=Foo") ||
			!strings.Contains(string(entry), "/bin/foo") {
			t.Fatalf("the desktop entry is:\n%s", entry)
		}
	}

	_, err = m.run(t, "", "remove", "foo")
	must(t, err)

	if exists(app) {
		t.Fatal("remove left the app behind")
	}

	_, err = m.run(t, "", "add", ref)
	must(t, err)

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if exists(app) {
		t.Fatal("rollback to a generation without the package left the app behind")
	}

	// oku does not overwrite what it did not place.
	must(t, os.MkdirAll(filepath.Dir(app), 0o755))
	must(t, os.WriteFile(app, []byte("mine"), 0o644))

	_, err = m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "did not put it there") {
		t.Fatalf("want a refusal to overwrite, got %v", err)
	}

	if body, _ := os.ReadFile(app); string(body) != "mine" {
		t.Fatal("oku overwrote a file it did not place")
	}
}

func TestB71FontIsInstalledForTheUserAndRemoveTakesItAway(t *testing.T) {
	m := newMachine(t)
	_, font := m.exposedPaths()

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	if body, _ := os.ReadFile(font); string(body) != "not really a font" {
		t.Fatalf("the font is not at %s", font)
	}

	_, err = m.run(t, "", "remove", "foo")
	must(t, err)

	if exists(font) {
		t.Fatal("remove left the font behind")
	}
}

func TestB95UninstallRemovesExposedAppsAndFonts(t *testing.T) {
	m := newMachine(t)
	app, font := m.exposedPaths()

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	out, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if exists(app) || exists(font) {
		t.Fatal("uninstall left the app or the font behind")
	}

	if !strings.Contains(out, font) {
		t.Fatalf("uninstall did not list what it removes outside its directories:\n%s", out)
	}
}

// tarOf returns an uncompressed tar holding files, each executable.
func tarOf(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)
	for path, body := range files {
		must(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0o755, Size: int64(len(body))}))

		_, err := tw.Write([]byte(body))
		must(t, err)
	}

	must(t, tw.Close())

	return buf.Bytes()
}

// fileManifest writes data as a download and a manifest for "tool" that points
// at it. artifact is the TOML after the url.
func (m machine) fileManifest(t *testing.T, fileName string, data []byte, artifact string) string {
	t.Helper()

	download := filepath.Join(m.fixtures, fileName)
	must(t, os.WriteFile(download, data, 0o644))

	return m.rawManifest(
		t,
		"tool",
		fmt.Sprintf("[[artifact]]\nurl = \"file://%s\"\n%s\n", download, artifact),
	)
}

func (m machine) installAndRun(t *testing.T, ref, want string) {
	t.Helper()

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add %s: %v\n%s", ref, err, out)
	}

	if got := m.toolOutput(t); got != want {
		t.Fatalf("tool printed %q, want %q", got, want)
	}

	_, err := m.run(t, "", "remove", "tool")
	must(t, err)
}

func TestB72UnpacksPackageFormatsWithoutRunningAnythingInside(t *testing.T) {
	m := newMachine(t)
	plain := tarOf(t, map[string]string{"tool": "#!/bin/sh\necho from tar\n"})

	var xzBuf, zstBuf bytes.Buffer

	xw, err := xz.NewWriter(&xzBuf)
	must(t, err)
	_, err = xw.Write(plain)
	must(t, err)
	must(t, xw.Close())

	zw, err := zstd.NewWriter(&zstBuf)
	must(t, err)
	_, err = zw.Write(plain)
	must(t, err)
	must(t, zw.Close())

	m.installAndRun(
		t,
		m.fileManifest(t, "tool.tar.xz", xzBuf.Bytes(), `bin = ["tool"]`),
		"from tar",
	)
	m.installAndRun(
		t,
		m.fileManifest(t, "tool.tar.zst", zstBuf.Bytes(), `bin = ["tool"]`),
		"from tar",
	)

	// A .deb is an ar archive. Its control archive holds a script that would
	// leave a marker if oku ran it.
	marker := filepath.Join(m.fixtures, "maintainer-script-ran")

	var gz bytes.Buffer

	zipper := gzip.NewWriter(&gz)
	_, err = zipper.Write(
		tarOf(t, map[string]string{"./usr/bin/tool": "#!/bin/sh\necho from deb\n"}),
	)
	must(t, err)
	must(t, zipper.Close())

	var deb bytes.Buffer

	deb.WriteString("!<arch>\n")

	for _, member := range []struct {
		name string
		data []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar", tarOf(t, map[string]string{"./postinst": "#!/bin/sh\ntouch " + marker + "\n"})},
		{"data.tar.gz", gz.Bytes()},
	} {
		fmt.Fprintf(
			&deb,
			"%-16s%-12s%-6s%-6s%-8s%-10d`\n",
			member.name,
			"0",
			"0",
			"0",
			"100644",
			len(member.data),
		)
		deb.Write(member.data)

		if len(member.data)%2 == 1 {
			deb.WriteByte('\n')
		}
	}

	m.installAndRun(
		t,
		m.fileManifest(t, "tool.deb", deb.Bytes(), `bin = ["usr/bin/tool"]`),
		"from deb",
	)

	rpmData, err := os.ReadFile(filepath.Join("testdata", "tool.rpm"))
	must(t, err)

	m.installAndRun(
		t,
		m.fileManifest(
			t,
			"tool.rpm",
			rpmData,
			"bin = [\"usr/bin/tool\"]\nman = [\"usr/share/man/man1/tool.1\"]",
		),
		"hello from rpm",
	)

	// The archive was made with 7-Zip and holds tool-1.0/bin/tool, a data file and
	// a symlink.
	sevenZip, err := os.ReadFile(filepath.Join("testdata", "tool.7z"))
	must(t, err)

	m.installAndRun(
		t,
		m.fileManifest(t, "tool.7z", sevenZip, "strip = 1\nbin = [\"bin/tool\"]"),
		"hello from 7z",
	)

	for _, path := range []string{marker, "/tmp/oku-rpm-scriptlet-ran"} {
		if exists(path) {
			t.Fatalf("oku ran a package script, %s exists", path)
		}
	}
}

func TestB72UnpacksMacOSDiskImagesAndInstallerPackages(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("dmg and pkg are unpacked with macOS tools")
	}

	m := newMachine(t)
	marker := filepath.Join(m.fixtures, "pkg-script-ran")

	payload := filepath.Join(m.fixtures, "payload")
	must(t, os.MkdirAll(filepath.Join(payload, "Tool.app", "Contents", "MacOS"), 0o755))
	must(
		t,
		os.WriteFile(
			filepath.Join(payload, "Tool.app", "Contents", "MacOS", "tool"),
			[]byte("#!/bin/sh\necho from image\n"),
			0o755,
		),
	)
	must(t, os.Symlink("/Applications", filepath.Join(payload, "Applications")))

	dmg := filepath.Join(m.fixtures, "tool.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-volname", "Tool", "-srcfolder", payload, "-format", "UDZO", dmg).
		CombinedOutput(); err != nil {
		t.Skipf("cannot create a disk image here: %v\n%s", err, out)
	}

	image, err := os.ReadFile(dmg)
	must(t, err)

	m.installAndRun(
		t,
		m.fileManifest(
			t,
			"image.dmg",
			image,
			"bin = [\"Tool.app/Contents/MacOS/tool\"]\napp = [\"Tool.app\"]",
		),
		"from image",
	)

	scripts := filepath.Join(m.fixtures, "scripts")
	must(t, os.MkdirAll(scripts, 0o755))
	must(
		t,
		os.WriteFile(
			filepath.Join(scripts, "postinstall"),
			[]byte("#!/bin/sh\ntouch "+marker+"\n"),
			0o755,
		),
	)
	must(t, os.Remove(filepath.Join(payload, "Applications")))

	pkg := filepath.Join(m.fixtures, "tool.pkg")
	if out, err := exec.Command("/usr/bin/pkgbuild", "--quiet", "--root", payload, "--scripts", scripts,
		"--identifier", "test.oku.tool", "--version", "1", "--install-location", "/Applications", pkg).
		CombinedOutput(); err != nil {
		t.Skipf("cannot build an installer package here: %v\n%s", err, out)
	}

	installer, err := os.ReadFile(pkg)
	must(t, err)

	m.installAndRun(
		t,
		m.fileManifest(
			t,
			"installer.pkg",
			installer,
			"bin = [\"Payload/Tool.app/Contents/MacOS/tool\"]",
		),
		"from image",
	)

	if exists(marker) {
		t.Fatal("oku ran the installer package's postinstall script")
	}

	if left, _ := filepath.Glob("/Volumes/Tool*"); len(left) != 0 {
		t.Fatalf("a disk image is still mounted: %v", left)
	}
}

// serviceManifest writes a package that ships a program and a service running it.
func (m machine) serviceManifest(t *testing.T) string {
	t.Helper()

	return m.manifest(t, "food", map[string]string{"food": script},
		"bin = [\"food\"]\n[[service]]\nname = \"food\"\ncommand = \"bin/food\"\n"+
			"args = [\"--data\", \"{{prefix}}/share\"]\nenv = { PORT = \"8080\" }\nrestart = \"on-failure\"\n")
}

func TestB73ServiceRunsWhenTheListEnablesItAndIsStoppedOtherwise(t *testing.T) {
	m := newMachine(t)
	ref := m.serviceManifest(t)

	out, err := m.run(t, "", "add", ref)
	must(t, err)

	got, ok := m.services.state["food"]
	if !ok || got.enabled || got.running {
		t.Fatalf(
			"without service = true the service should be installed and stopped, got %+v\n%s",
			got,
			out,
		)
	}

	if !strings.HasSuffix(got.def.Program, "/bin/food") || got.def.Env["PORT"] != "8080" ||
		got.def.Restart != "on-failure" || !strings.HasSuffix(got.def.Args[1], "/share") {
		t.Fatalf("the definition oku passed on is %+v", got.def)
	}

	_, err = m.run(t, "", "add", ref, "--service")
	must(t, err)

	if got := m.services.state["food"]; !got.enabled || !got.running {
		t.Fatalf("add --service left the service at %+v", got)
	}

	listed, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(listed), "service = true") {
		t.Fatalf("oku.toml does not record service = true:\n%s", listed)
	}

	// A new machine gets the service running from the list alone.
	must(t, os.RemoveAll(m.data))
	m.services.state = map[string]*fakeService{}

	_, err = m.run(t, "", "sync")
	must(t, err)

	if got := m.services.state["food"]; got == nil || !got.enabled {
		t.Fatalf("sync did not enable the service: %+v", got)
	}

	_, err = m.run(t, "", "remove", "food")
	must(t, err)

	if _, still := m.services.state["food"]; still {
		t.Fatal("remove left the service installed")
	}
}

func TestB74ServiceCommandsControlTheService(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.serviceManifest(t))
	must(t, err)

	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"service", "list"}, "stopped"},
		{[]string{"service", "start", "food"}, "food: running"},
		{[]string{"service", "status", "food"}, "food: running"},
		{[]string{"service", "restart", "food"}, "food: running"},
		{[]string{"service", "stop", "food"}, "food: stopped"},
		{[]string{"service", "logs", "food"}, "listening on 8080"},
	} {
		out, err := m.run(t, "", step.args...)
		if err != nil || !strings.Contains(out, step.want) {
			t.Fatalf("oku %v: %v\n%s", step.args, err, out)
		}
	}

	_, err = m.run(t, "", "service", "start", "nope")
	if err == nil || !strings.Contains(err.Error(), "food") {
		t.Fatalf("want an unknown service to fail and name the known ones, got %v", err)
	}
}

func TestB76RollbackRestoresWhichServicesAreEnabled(t *testing.T) {
	m := newMachine(t)
	ref := m.serviceManifest(t)

	_, err := m.run(t, "", "add", ref, "--service")
	must(t, err)

	_, err = m.run(t, "", "add", ref)
	must(t, err)

	if m.services.state["food"].enabled {
		t.Fatal("the second generation should have the service disabled")
	}

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if !m.services.state["food"].enabled {
		t.Fatal("rollback did not enable the service again")
	}

	_, err = m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if len(m.services.state) != 0 {
		t.Fatalf("uninstall left services behind: %v", m.services.state)
	}
}

// sharedRoot gives the machine a shared store root and an Elevate that records
// what oku ran with administrator rights instead of calling sudo.
func (m *machine) sharedRoot(t *testing.T) (string, *[][]string) {
	t.Helper()

	root := filepath.Join(filepath.Dir(m.fixtures), "opt", "oku")
	elevated := &[][]string{}

	m.opts.SystemRoot = root
	m.opts.Elevate = func(_ context.Context, argv []string) error {
		*elevated = append(*elevated, argv)

		if argv[0] == "rmdir" {
			return os.Remove(root)
		}

		return os.MkdirAll(root, 0o755)
	}

	return root, elevated
}

func TestB75SetupSystemNamesTheRootAndAsksBeforeElevating(t *testing.T) {
	m := newMachine(t)
	root, elevated := m.sharedRoot(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	if len(*elevated) != 0 {
		t.Fatalf("add elevated without --system: %v", *elevated)
	}

	if _, err := m.run(t, "", "setup"); err == nil {
		t.Fatal("setup ran without --system")
	}

	out, err := m.run(t, "n\n", "setup", "--system")
	if err == nil || len(*elevated) != 0 || exists(root) {
		t.Fatalf("answering no still elevated: %v\n%s", *elevated, out)
	}

	if !strings.Contains(out, root) || !strings.Contains(out, "administrator rights") {
		t.Fatalf("setup did not name what it creates:\n%s", out)
	}

	_, err = m.run(t, "y\n", "setup", "--system")
	must(t, err)

	if len(*elevated) != 1 {
		t.Fatalf("want one elevated command, got %v", *elevated)
	}

	_, err = m.run(t, "", "sync")
	must(t, err)

	entries, err := os.ReadDir(filepath.Join(root, "store"))
	must(t, err)

	if len(entries) != 1 {
		t.Fatalf("sync did not install into the shared root: %v", entries)
	}

	target, err := filepath.EvalSymlinks(m.profile("bin", "tool"))
	must(t, err)

	resolved, err := filepath.EvalSymlinks(root)
	must(t, err)

	if !strings.HasPrefix(target, resolved) {
		t.Fatalf("the profile points at %s, outside the shared root", target)
	}

	_, err = m.run(t, "", "gc", "--keep", "1")
	must(t, err)

	if left := m.storeEntries(t); len(left) != 0 {
		t.Fatalf("gc kept the old copies in the data directory: %v", left)
	}
}

func TestB98UninstallNamesTheSharedRootAndAsksBeforeElevating(t *testing.T) {
	m := newMachine(t)
	root, elevated := m.sharedRoot(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "setup", "--system", "--yes")
	must(t, err)
	_, err = m.run(t, "", "add", ref)
	must(t, err)

	*elevated = nil

	out, err := m.run(t, "y\nn\n", "self", "uninstall")
	must(t, err)

	if len(*elevated) != 0 {
		t.Fatalf("declining still elevated: %v", *elevated)
	}

	if !strings.Contains(out, "left in place") || !strings.Contains(out, root) {
		t.Fatalf("uninstall did not print what is left:\n%s", out)
	}

	if entries, _ := os.ReadDir(root); len(entries) != 0 || exists(m.data) {
		t.Fatalf("declining left user-scope files behind: %v", entries)
	}

	m2 := newMachine(t)
	root, elevated = m2.sharedRoot(t)

	_, err = m2.run(t, "", "setup", "--system", "--yes")
	must(t, err)
	_, err = m2.run(t, "y\ny\n", "self", "uninstall")
	must(t, err)

	if exists(root) || len(*elevated) != 2 {
		t.Fatalf("accepting did not remove %s: %v", root, *elevated)
	}
}

// systemScope gives the machine throwaway system directories and an Elevate that
// runs "oku system-apply" in this process, as sudo would run it as root.
func (m *machine) systemScope(t *testing.T) (expose.Dirs, *int) {
	t.Helper()

	base := filepath.Join(filepath.Dir(m.fixtures), "system")
	dirs := expose.Dirs{Apps: filepath.Join(base, "apps"), Fonts: filepath.Join(base, "fonts")}
	elevations := new(int)

	m.opts.SystemDirs = &dirs
	m.opts.Elevate = func(_ context.Context, argv []string) error {
		*elevations++

		_, err := m.run(t, "", argv[1:]...)

		return err
	}

	return dirs, elevations
}

func TestB75SystemScopeItemsChangeOnlyWithTheFlagAndAfterAQuestion(t *testing.T) {
	m := newMachine(t)
	dirs, elevations := m.systemScope(t)
	font := filepath.Join(dirs.Fonts, "Test.ttf")
	userFont := func() string { _, path := m.exposedPaths(); return path }()
	ref := m.desktopManifest(t)

	out, err := m.run(t, "y\n", "add", ref, "--system")
	must(t, err)

	if !strings.Contains(out, "administrator rights") || !strings.Contains(out, font) {
		t.Fatalf("add --system did not name what it writes:\n%s", out)
	}

	if !exists(font) || exists(userFont) {
		t.Fatalf("the font should be in system scope only:\n%s", out)
	}

	list, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	if !strings.Contains(string(list), "system = true") {
		t.Fatalf("the list does not record system scope:\n%s", list)
	}

	before := *elevations

	out, err = m.run(t, "", "remove", "foo")
	must(t, err)

	if *elevations != before || !exists(font) {
		t.Fatalf("remove elevated without --system:\n%s", out)
	}

	if !strings.Contains(out, "oku sync --system") || !strings.Contains(out, font) {
		t.Fatalf("remove did not say what it left unchanged:\n%s", out)
	}

	out, err = m.run(t, "n\n", "sync", "--system")
	must(t, err)

	if *elevations != before || !exists(font) {
		t.Fatalf("answering no still elevated:\n%s", out)
	}

	_, err = m.run(t, "y\n", "sync", "--system")
	must(t, err)

	if exists(font) {
		t.Fatal("sync --system left the font behind")
	}
}

func TestB75SystemServiceNeedsTheFlagToStart(t *testing.T) {
	m := newMachine(t)
	_, elevations := m.systemScope(t)

	_, err := m.run(t, "y\n", "add", m.serviceManifest(t), "--system", "--service")
	must(t, err)

	if got := m.services.state["food"]; got == nil || !got.enabled {
		t.Fatalf("the system service is not enabled: %+v", got)
	}

	before := *elevations

	if _, err := m.run(t, "", "service", "stop", "food"); err == nil || *elevations != before {
		t.Fatal("service stop elevated without --system")
	}

	out, err := m.run(t, "", "service", "stop", "food", "--system")
	must(t, err)

	if m.services.state["food"].running || !strings.Contains(out, "system scope") {
		t.Fatalf("service stop --system did not stop it:\n%s", out)
	}
}

func TestB98UninstallLeavesSystemItemsWhenElevationIsDeclined(t *testing.T) {
	m := newMachine(t)
	dirs, _ := m.systemScope(t)
	font := filepath.Join(dirs.Fonts, "Test.ttf")
	ref := m.desktopManifest(t)

	_, err := m.run(t, "y\n", "add", ref, "--system")
	must(t, err)

	out, err := m.run(t, "y\nn\n", "self", "uninstall")
	must(t, err)

	if !exists(font) || exists(m.data) {
		t.Fatalf("declining should remove user scope only:\n%s", out)
	}

	if !strings.Contains(out, "left in place") || !strings.Contains(out, font) {
		t.Fatalf("uninstall did not print what is left:\n%s", out)
	}

	m2 := newMachine(t)
	dirs, _ = m2.systemScope(t)

	_, err = m2.run(t, "y\n", "add", m2.desktopManifest(t), "--system")
	must(t, err)
	_, err = m2.run(t, "y\ny\n", "self", "uninstall")
	must(t, err)

	if exists(filepath.Join(dirs.Fonts, "Test.ttf")) {
		t.Fatal("accepting left the system font behind")
	}
}

// cachedManifest writes a manifest that only builds from source. Its text is the
// same on every machine, so its store hash is too.
func (m machine) cachedManifest(t *testing.T, relocatable bool, step string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "cached.toml")
	must(t, os.WriteFile(path, fmt.Appendf(
		nil,
		"[package]\nname = \"tool\"\nrelocatable = %t\n[version]\nvalue = \"1.0.0\"\n[build]\n%s%s",
		relocatable, step, installTool,
	), 0o644))

	return path
}

// publisher builds the manifest on its own machine and pushes it to a cache
// directory. It returns the directory and the public key.
func publisher(t *testing.T, relocatable bool) (dir, key string) {
	t.Helper()

	m := newMachine(t)
	dir = filepath.Join(filepath.Dir(m.fixtures), "served")

	out, err := m.run(t, "", "key", "generate")
	must(t, err)

	key = strings.TrimSpace(out[strings.LastIndex(out, "oku key trust ")+len("oku key trust "):])

	_, err = m.run(t, "", "add", m.cachedManifest(t, relocatable, writeTool), "--yes")
	must(t, err)

	if listed, _ := m.run(t, "", "key", "list"); !strings.Contains(listed, key) {
		t.Fatalf("key list does not show the generated key:\n%s", listed)
	}

	out, err = m.run(t, "", "cache", "push", dir)
	must(t, err)

	if !strings.Contains(out, "pushed tool-1.0.0-") {
		t.Fatalf("push did not report the entry:\n%s", out)
	}

	return dir, key
}

func TestB85TrustedCacheEntryIsUsedAndNothingIsBuilt(t *testing.T) {
	dir, key := publisher(t, true)
	m := newMachine(t)

	_, err := m.run(t, "", "cache", "add", dir)
	must(t, err)
	_, err = m.run(t, "", "key", "trust", key)
	must(t, err)

	// Without --yes a non-interactive add refuses to build, so success means
	// that no build step ran.
	out, err := m.run(t, "", "add", m.cachedManifest(t, true, writeTool))
	if err != nil {
		t.Fatalf("add from the cache: %v\n%s", err, out)
	}

	if !strings.Contains(out, "came from a cache") || m.toolOutput(t) != "built 1.0.0" {
		t.Fatalf("the package did not come from the cache:\n%s", out)
	}
}

func TestB86UnsignedOrUntrustedCacheEntryIsIgnoredAndThePackageBuilds(t *testing.T) {
	dir, key := publisher(t, true)

	untrusting := newMachine(t)
	_, err := untrusting.run(t, "", "cache", "add", dir)
	must(t, err)

	out, err := untrusting.run(t, "", "add", untrusting.cachedManifest(t, true, writeTool), "--yes")
	must(t, err)

	if !strings.Contains(out, "no trusted key signed it") || strings.Contains(out, "came from") {
		t.Fatalf("an entry from an untrusted key was not ignored:\n%s", out)
	}

	signatures, err := filepath.Glob(filepath.Join(dir, "*.minisig"))
	must(t, err)
	must(t, os.Remove(signatures[0]))

	unsigned := newMachine(t)
	_, err = unsigned.run(t, "", "cache", "add", dir)
	must(t, err)
	_, err = unsigned.run(t, "", "key", "trust", key)
	must(t, err)

	out, err = unsigned.run(t, "", "add", unsigned.cachedManifest(t, true, writeTool), "--yes")
	must(t, err)

	if !strings.Contains(out, "has no signature") || unsigned.toolOutput(t) != "built 1.0.0" {
		t.Fatalf("an unsigned entry was not ignored:\n%s", out)
	}
}

func TestB87PushRefusesAnImpurePackage(t *testing.T) {
	m := newMachine(t)
	m.opts.Interactive = yes()

	_, err := m.run(t, "", "key", "generate")
	must(t, err)

	impure := strings.Replace(writeTool, "shell = ", "network = true\nshell = ", 1)
	_, err = m.run(t, "y\n", "add", m.cachedManifest(t, true, impure))
	must(t, err)

	_, err = m.run(t, "", "cache", "push", filepath.Join(m.fixtures, "served"))
	if err == nil || !strings.Contains(err.Error(), "network access") {
		t.Fatalf("want a refusal for the impure package, got %v", err)
	}
}

func TestB88NonRelocatableEntryFromAnotherStoreRootIsNotUsed(t *testing.T) {
	dir, key := publisher(t, false)
	m := newMachine(t)

	_, err := m.run(t, "", "cache", "add", dir)
	must(t, err)
	_, err = m.run(t, "", "key", "trust", key)
	must(t, err)

	out, err := m.run(t, "", "add", m.cachedManifest(t, false, writeTool), "--yes")
	must(t, err)

	if strings.Contains(out, "came from a cache") {
		t.Fatalf("a package built under another store root was substituted:\n%s", out)
	}
}

// signedManifest writes a manifest with the signing key public, or with none when
// public is empty. It writes the artifact and the artifact's signature by secret.
func (m machine) signedManifest(
	t *testing.T,
	public string,
	secret minisign.PrivateKey,
	legacy bool,
) string {
	t.Helper()

	archive, _ := m.archive(t, "tool", map[string]string{"tool": script})
	data, err := os.ReadFile(archive)
	must(t, err)
	// Sign writes the legacy kind of signature, and a Reader the current kind.
	signature := minisign.Sign(secret, data)
	if !legacy {
		reader := minisign.NewReader(bytes.NewReader(data))
		_, err = io.Copy(io.Discard, reader)
		must(t, err)

		signature = reader.Sign(secret)
	}

	must(t, os.WriteFile(archive+".minisig", signature, 0o644))

	key := ""
	if public != "" {
		key = fmt.Sprintf("signing_key = %q\n", public)
	}

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, fmt.Appendf(
		nil,
		"[package]\nname = \"tool\"\n%s[version]\nvalue = \"1.2.3\"\n"+
			"[[artifact]]\nurl = \"file://%s\"\nbin = [\"tool\"]\n", key, archive,
	), 0o644))

	return path
}

func TestB89SigningKeyVerifiesArtifactsAndAChangedKeyStopsUntilAccepted(t *testing.T) {
	m := newMachine(t)

	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)
	otherPublic, otherSecret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	if _, err := m.run(
		t,
		"",
		"add",
		m.signedManifest(t, public.String(), otherSecret, false),
	); err == nil ||
		!strings.Contains(err.Error(), "is not signed by") {
		t.Fatalf("want a refusal for an artifact the key did not sign, got %v", err)
	}

	if len(m.storeEntries(t)) != 0 {
		t.Fatal("a package with a bad signature reached the store")
	}

	ref := m.signedManifest(t, public.String(), secret, false)

	out, err := m.run(t, "", "add", ref)
	must(t, err)

	if strings.Contains(out, "first download") {
		t.Fatalf("a signed artifact should not be trust on first use:\n%s", out)
	}

	lockText, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(lockText), public.String()) {
		t.Fatalf("the lock does not pin the signing key:\n%s", lockText)
	}

	m.signedManifest(t, otherPublic.String(), otherSecret, false)

	for _, command := range []string{"sync", "update"} {
		if _, err := m.run(t, "", command); err == nil {
			t.Fatalf("%s accepted a changed signing key", command)
		}
	}

	m.signedManifest(t, "", secret, false)

	if _, err := m.run(
		t,
		"",
		"update",
	); err == nil ||
		!strings.Contains(err.Error(), "--accept-key") {
		t.Fatalf("want a refusal that names --accept-key for a dropped key, got %v", err)
	}

	m.signedManifest(t, otherPublic.String(), otherSecret, true)

	_, err = m.run(t, "", "update", "--accept-key")
	must(t, err)

	lockText, err = os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(lockText), otherPublic.String()) {
		t.Fatalf("the lock does not pin the accepted key:\n%s", lockText)
	}
}

func TestB90ShellRunsWithThePackagesOnPathAndChangesNothing(t *testing.T) {
	m := newMachine(t)
	kept := m.manifest(t, "kept", map[string]string{"kept": script}, `bin = ["kept"]`)

	_, err := m.run(t, "", "add", kept)
	must(t, err)

	snapshot := func() string {
		var state []string

		for _, path := range []string{
			filepath.Join(m.config, "oku.toml"), filepath.Join(m.config, "oku.lock"),
		} {
			data, err := os.ReadFile(path)
			must(t, err)

			state = append(state, string(data))
		}

		target, err := os.Readlink(filepath.Join(m.data, "profiles", "global", "current"))
		must(t, err)

		return strings.Join(append(state, target), "\n")
	}

	before := snapshot()
	ref := m.envManifest(t, "tool", "TOOL_HOME")

	out, err := m.run(
		t,
		"",
		"shell",
		ref,
		"--",
		"sh",
		"-c",
		`command -v tool; echo "home=$TOOL_HOME"`,
	)
	if err != nil {
		t.Fatalf("shell: %v\n%s", err, out)
	}

	if !strings.Contains(out, filepath.Join(m.data, "store")) || strings.Contains(out, "home=\n") {
		t.Fatalf("the command did not see the package on PATH with its env:\n%s", out)
	}

	if after := snapshot(); after != before {
		t.Fatalf("shell changed the list, the lock or the profile:\n%s\nwas\n%s", after, before)
	}

	if exists(m.profile("bin", "tool")) {
		t.Fatal("shell linked the package into the profile")
	}

	_, err = m.run(t, "", "shell", ref, "--", "sh", "-c", "exit 7")

	var exit cli.ExitError
	if !errors.As(err, &exit) || exit.Code != 7 {
		t.Fatalf("want the command's exit code 7, got %v", err)
	}
}

func TestB72MsiIsRefusedWithAReasonOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows unpacks an msi, and the live test on the Windows runner covers it")
	}

	m := newMachine(t)
	msi := filepath.Join(m.fixtures, "tool.msi")
	must(
		t,
		os.WriteFile(
			msi,
			append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, make([]byte, 600)...),
			0o644,
		),
	)

	ref := m.rawManifest(
		t,
		"tool",
		fmt.Sprintf("[[artifact]]\nurl = \"file://%s\"\nbin = [\"tool.exe\"]\n", msi),
	)

	_, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "on Windows only") {
		t.Fatalf("want a refusal that says msi needs Windows, got %v", err)
	}

	if len(m.storeEntries(t)) != 0 {
		t.Fatal("a refused msi left something in the store")
	}
}

func TestB91DoctorReportsTheSetupAndItsProblems(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	bin := filepath.Dir(m.profile("bin", "tool"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")

	out, err := m.run(t, "", "doctor")
	if err != nil {
		t.Fatalf("doctor found a problem on a healthy machine: %v\n%s", err, out)
	}

	for _, want := range []string{filepath.Join(m.data, "store"), "sandbox", "shell hook", "is on PATH", "points at a file in the store"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor does not report %q:\n%s", want, out)
		}
	}

	// A program of the same name earlier on PATH, and a store path that is gone.
	shadow := filepath.Join(filepath.Dir(m.fixtures), "shadow")
	must(t, os.MkdirAll(shadow, 0o755))
	must(t, os.WriteFile(filepath.Join(shadow, "tool"), []byte(script), 0o755))
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+bin)

	for _, entry := range m.storeEntries(t) {
		must(t, os.RemoveAll(filepath.Join(m.data, "store", entry)))
	}

	out, err = m.run(t, "", "doctor")
	if err == nil {
		t.Fatalf("doctor found nothing wrong:\n%s", out)
	}

	for _, want := range []string{"runs in place of oku's tool", "is missing", "does not exist"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor does not report %q:\n%s", want, out)
		}
	}

	t.Setenv("PATH", "/usr/bin:/bin")

	out, _ = m.run(t, "", "doctor")
	if !strings.Contains(out, "is not on PATH") {
		t.Fatalf("doctor does not say that the profile is missing from PATH:\n%s", out)
	}
}

func TestB103DataCommandsPrintJSON(t *testing.T) {
	m := newMachine(t)

	decode := func(args ...string) any {
		t.Helper()

		out, err := m.run(t, "", append(args, "--json")...)
		if err != nil && args[0] != "doctor" {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}

		// doctor ends with its error line after the JSON when it found a problem.
		var value any
		if err := json.NewDecoder(strings.NewReader(out)).Decode(&value); err != nil {
			t.Fatalf("%v --json is not JSON: %v\n%s", args, err, out)
		}

		return value
	}

	for _, args := range [][]string{
		{"list"}, {"generations"}, {"source", "list"}, {"cache", "list"}, {"service", "list"},
	} {
		if list, ok := decode(args...).([]any); !ok || len(list) != 0 {
			t.Fatalf("%v --json on an empty machine should be [], got %v", args, list)
		}
	}

	_, err := m.run(t, "", "add", m.serviceManifest(t), "--service")
	must(t, err)

	listed := decode("list").([]any)[0].(map[string]any)
	if listed["name"] != "food" || listed["version"] != "1.2.3" || listed["service"] != true {
		t.Fatalf("list --json: %v", listed)
	}

	if info := decode("info", "food").(map[string]any); info["installed"] != "artifact" ||
		!strings.Contains(info["store_path"].(string), "food-1.2.3-") {
		t.Fatalf("info --json: %v", info)
	}

	if why := decode("why", "food").(map[string]any); why["in_list"] == "" {
		t.Fatalf("why --json: %v", why)
	}

	gens := decode("generations").([]any)
	if gen := gens[len(gens)-1].(map[string]any); gen["current"] != true ||
		gen["packages"].([]any)[0].(map[string]any)["name"] != "food" {
		t.Fatalf("generations --json: %v", gens)
	}

	services := decode("service", "list").([]any)[0].(map[string]any)
	if services["name"] != "food" || services["enabled"] != true || services["running"] != true {
		t.Fatalf("service list --json: %v", services)
	}

	if status := decode("service", "status", "food").(map[string]any); status["running"] != true {
		t.Fatalf("service status --json: %v", status)
	}

	if keys := decode("key", "list").(map[string]any); keys["trusted"] == nil {
		t.Fatalf("key list --json: %v", keys)
	}

	doctor := decode("doctor").(map[string]any)
	if len(doctor["checks"].([]any)) < 5 {
		t.Fatalf("doctor --json: %v", doctor)
	}
}

// patchedManifest builds a program from a source file that a patch step changes
// first. diff is the text of the patch file.
func (m machine) patchedManifest(t *testing.T, diff string) string {
	t.Helper()

	must(
		t,
		os.WriteFile(
			filepath.Join(m.fixtures, "tool.sh"),
			[]byte("#!/bin/sh\necho original\n"),
			0o644,
		),
	)
	must(t, os.WriteFile(filepath.Join(m.fixtures, "fix.patch"), []byte(diff), 0o644))

	sum := func(name string) string {
		data, err := os.ReadFile(filepath.Join(m.fixtures, name))
		must(t, err)

		digest := sha256.Sum256(data)

		return hex.EncodeToString(digest[:])
	}

	fetch := func(name string) string {
		return fmt.Sprintf(
			"[[build.step]]\nfetch = { url = \"file://%s\", sha256 = %q, to = %q }\n",
			filepath.Join(m.fixtures, name), sum(name), name,
		)
	}

	return m.buildManifest(t, false, "", fetch("tool.sh")+fetch("fix.patch")+
		"[[build.step]]\npatch = { file = \"fix.patch\" }\n"+
		"[[build.step]]\nrun = \"cp tool.sh tool && chmod +x tool\"\nshell = \"sh\"\n"+installTool)
}

func TestB104PatchStepChangesTheSourceAndAHunkThatDoesNotFitFailsTheBuild(t *testing.T) {
	m := newMachine(t)

	ref := m.patchedManifest(t, "diff --git a/tool.sh b/tool.sh\n--- a/tool.sh\n+++ b/tool.sh\n"+
		"@@ -1,2 +1,2 @@\n #!/bin/sh\n-echo original\n+echo patched\n")

	_, err := m.run(t, "", "add", ref, "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "patched" {
		t.Fatalf("the built program printed %q, so the patch was not applied", got)
	}

	other := newMachine(t)

	ref = other.patchedManifest(t, "diff --git a/tool.sh b/tool.sh\n--- a/tool.sh\n+++ b/tool.sh\n"+
		"@@ -1,2 +1,2 @@\n #!/bin/sh\n-echo something else\n+echo patched\n")

	_, err = other.run(t, "", "add", ref, "--yes")
	if err == nil || !strings.Contains(err.Error(), "does not fit tool.sh") {
		t.Fatalf("want a failure that names the file the patch does not fit, got %v", err)
	}

	if len(other.storeEntries(t)) != 0 {
		t.Fatal("a build with a failed patch left something in the store")
	}

	// A plain diff keeps its a/ and b/ prefixes, as it does for "patch -p1".
	plain := newMachine(t)
	ref = plain.patchedManifest(t, "--- a/tool.sh\n+++ b/tool.sh\n"+
		"@@ -1,2 +1,2 @@\n #!/bin/sh\n-echo original\n+echo patched\n")

	_, err = plain.run(t, "", "add", ref, "--yes")
	if err == nil || !strings.Contains(err.Error(), "strip removes them") {
		t.Fatalf("want a failure that suggests strip, got %v", err)
	}

	body, err := os.ReadFile(ref)
	must(t, err)
	must(t, os.WriteFile(ref, bytes.Replace(
		body,
		[]byte(`patch = { file = "fix.patch" }`),
		[]byte(`patch = { file = "fix.patch", strip = 1 }`),
		1,
	), 0o644))

	_, err = plain.run(t, "", "add", ref, "--yes")
	must(t, err)

	if got := plain.toolOutput(t); got != "patched" {
		t.Fatalf("with strip = 1 the built program printed %q", got)
	}

	report, err := other.run(t, "", "manifest", "lint", other.buildManifest(t, false, "",
		"[[build.step]]\npatch = { strip = 1 }\n"+installTool))
	if err == nil || !strings.Contains(report, "a patch step needs file") {
		t.Fatalf("lint accepted a patch step without a file:\n%s", report)
	}
}

func TestB51FetchStepNeedsASha256AndDownloadsWithOne(t *testing.T) {
	m := newMachine(t)

	source := filepath.Join(m.fixtures, "tool.sh")
	must(t, os.WriteFile(source, []byte(script), 0o644))

	digest := sha256.Sum256([]byte(script))
	build := "[[build.step]]\nrun = \"cp tool.sh tool && chmod +x tool\"\nshell = \"sh\"\n" + installTool

	unpinned := m.buildManifest(t, false, "", fmt.Sprintf(
		"[[build.step]]\nfetch = { url = \"file://%s\", to = \"tool.sh\" }\n", source,
	)+build)

	report, err := m.run(t, "", "manifest", "lint", unpinned)
	if err == nil || !strings.Contains(report, "a fetch step needs sha256") {
		t.Fatalf("lint accepted a fetch step without sha256:\n%s", report)
	}

	pinned := m.buildManifest(t, false, "", fmt.Sprintf(
		"[[build.step]]\nfetch = { url = \"file://%s\", sha256 = %q, to = \"tool.sh\" }\n",
		source, hex.EncodeToString(digest[:]),
	)+build)

	_, err = m.run(t, "", "manifest", "lint", pinned)
	must(t, err)

	_, err = m.run(t, "", "add", pinned, "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("the program built from the fetched file printed %q", got)
	}

	wrong := newMachine(t)
	must(
		t,
		os.WriteFile(filepath.Join(wrong.fixtures, "tool.sh"), []byte(script+"# changed\n"), 0o644),
	)

	changed := wrong.buildManifest(t, false, "", fmt.Sprintf(
		"[[build.step]]\nfetch = { url = \"file://%s\", sha256 = %q, to = \"tool.sh\" }\n",
		filepath.Join(wrong.fixtures, "tool.sh"), hex.EncodeToString(digest[:]),
	)+build)

	if _, err := wrong.run(t, "", "add", changed, "--yes"); err == nil {
		t.Fatal("a fetched file that does not match its sha256 was accepted")
	}

	if len(wrong.storeEntries(t)) != 0 {
		t.Fatal("a build whose fetch failed left something in the store")
	}
}

func TestB97UninstallLeavesAProjectsListAndLockAlone(t *testing.T) {
	m := newMachine(t)
	project := m.hookProject(t)

	read := func() string {
		var both []string

		for _, name := range []string{"oku.toml", "oku.lock"} {
			data, err := os.ReadFile(filepath.Join(project, name))
			must(t, err)

			both = append(both, string(data))
		}

		return strings.Join(both, "\n---\n")
	}

	before := read()
	if !strings.Contains(before, "ptool") {
		t.Fatalf("the project has no list and lock to protect:\n%s", before)
	}

	_, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if exists(m.data) {
		t.Fatal("uninstall left the data directory, so this test proves nothing")
	}

	if after := read(); after != before {
		t.Fatalf("uninstall changed the project's files:\n%s\nwas\n%s", after, before)
	}
}

// releaseWith serves release v1.4.0 of owner/tool with an oku binary for this
// platform and its signature by secret.
func (m *machine) releaseWith(t *testing.T, body string, secret minisign.PrivateKey) {
	t.Helper()

	name := "oku-" + runtime.GOOS + "-" + runtime.GOARCH
	binary := filepath.Join(m.fixtures, name)
	must(t, os.WriteFile(binary, []byte(body), 0o755))

	reader := minisign.NewReader(strings.NewReader(body))
	_, err := io.Copy(io.Discard, reader)
	must(t, err)
	must(t, os.WriteFile(binary+".minisig", reader.Sign(secret), 0o644))

	inferServer(t, m, map[string]string{name: binary, name + ".minisig": binary + ".minisig"})
	m.opts.ReleaseRepo = "owner/tool"
}

func TestB92SelfUpdateReplacesTheBinaryOnlyAfterItsSignatureChecksOut(t *testing.T) {
	m := newMachine(t)

	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)
	_, stranger, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	current := func() string {
		data, err := os.ReadFile(m.exe)
		must(t, err)

		return string(data)
	}

	m.releaseWith(t, "the new oku", secret)

	// Without an override oku trusts only its built-in release key, which did not
	// sign this test release.
	if _, err := m.run(t, "", "self", "update"); err == nil ||
		!strings.Contains(err.Error(), "is not signed by") || current() != "binary" {
		t.Fatalf("a release that the built-in key did not sign should be refused, got %v", err)
	}

	m.opts.ReleaseKey = public.String()

	out, err := m.run(t, "", "self", "update", "--check")
	must(t, err)

	if !strings.Contains(out, "1.4.0 is available") || current() != "binary" {
		t.Fatalf("--check should report and change nothing:\n%s", out)
	}

	forged := newMachine(t)
	forged.opts.ReleaseKey = public.String()
	forged.releaseWith(t, "a forged oku", stranger)

	if _, err := forged.run(t, "", "self", "update"); err == nil ||
		!strings.Contains(err.Error(), "is not signed by") {
		t.Fatalf("want a refusal for a release the key did not sign, got %v", err)
	}

	if data, _ := os.ReadFile(forged.exe); string(data) != "binary" {
		t.Fatalf("a forged release replaced the binary with %q", data)
	}

	out, err = m.run(t, "", "self", "update")
	must(t, err)

	if current() != "the new oku" || !strings.Contains(out, "to 1.4.0") {
		t.Fatalf("self update did not replace the binary:\n%s", out)
	}

	if info, err := os.Stat(m.exe); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatal("the new binary is not executable")
	}

	m.opts.Version = "1.4.0"

	out, err = m.run(t, "", "self", "update")
	must(t, err)

	if !strings.Contains(out, "is the newest release") {
		t.Fatalf("an up to date oku should say so:\n%s", out)
	}
}

func TestB111SelfUpdateNightlyTakesTheNightlyBuildAfterTheSameCheck(t *testing.T) {
	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)
	_, stranger, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	forged := newMachine(t)
	forged.opts.ReleaseKey = public.String()
	forged.releaseWith(t, "a forged oku", stranger)

	if _, err := forged.run(t, "", "self", "update", "--nightly"); err == nil ||
		!strings.Contains(err.Error(), "is not signed by") {
		t.Fatalf("want a refusal for a nightly the key did not sign, got %v", err)
	}

	if data, _ := os.ReadFile(forged.exe); string(data) != "binary" {
		t.Fatalf("a forged nightly replaced the binary with %q", data)
	}

	m := newMachine(t)
	m.opts.ReleaseKey = public.String()
	m.releaseWith(t, "the nightly oku", secret)

	out, err := m.run(t, "", "self", "update", "--nightly")
	must(t, err)

	if data, _ := os.ReadFile(m.exe); string(data) != "the nightly oku" ||
		!strings.Contains(out, "nightly 7777777") {
		t.Fatalf("self update --nightly did not replace the binary:\n%s", out)
	}

	// The nightly tag moves, so the same url serves another build the next day.
	m.releaseWith(t, "the next nightly oku", secret)

	_, err = m.run(t, "", "self", "update", "--nightly")
	must(t, err)

	if data, _ := os.ReadFile(m.exe); string(data) != "the next nightly oku" {
		t.Fatalf("self update --nightly installed %q from an earlier download", data)
	}

	m.opts.Version = "nightly-20260921010203-7777777"

	must(t, os.WriteFile(m.exe, []byte("binary"), 0o755))

	out, err = m.run(t, "", "self", "update", "--nightly")
	must(t, err)

	if data, _ := os.ReadFile(m.exe); string(data) != "binary" ||
		!strings.Contains(out, "is the newest nightly build") {
		t.Fatalf("self update --nightly replaced the build it already is:\n%s", out)
	}
}

func TestB93InstallScriptPutsOneBinaryInPlaceAndEditsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.ps1 is covered by the live test on the Windows runner")
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	download := filepath.Join(root, "release", "latest", "download")
	must(t, os.MkdirAll(download, 0o755))
	must(t, os.MkdirAll(home, 0o755))
	must(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# mine\n"), 0o644))

	name := "oku-" + runtime.GOOS + "-" + runtime.GOARCH
	body := []byte("#!/bin/sh\necho oku\n")
	must(t, os.WriteFile(filepath.Join(download, name), body, 0o644))

	digest := sha256.Sum256(body)
	must(t, os.WriteFile(filepath.Join(download, "checksums.txt"),
		fmt.Appendf(nil, "%s  %s\n", hex.EncodeToString(digest[:]), name), 0o644))

	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(root, "release"))))
	t.Cleanup(server.Close)

	install := func(homeDir string) (string, error) {
		cmd := exec.Command("sh", filepath.Join("..", "..", "install.sh"))
		cmd.Env = []string{
			"HOME=" + homeDir, "SHELL=/bin/zsh", "PATH=" + os.Getenv("PATH"),
			"OKU_RELEASE_URL=" + server.URL,
		}

		out, err := cmd.CombinedOutput()

		return string(out), err
	}

	out, err := install(home)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}

	installed := filepath.Join(home, ".local", "bin", "oku")
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("no executable at %s:\n%s", installed, out)
	}

	if !strings.Contains(out, shellhook.Line("zsh", "$HOME/.local/bin/oku")) ||
		!strings.Contains(out, "~/.zshrc") {
		t.Fatalf("install.sh did not print the hook line and the file it goes into:\n%s", out)
	}

	if rc, _ := os.ReadFile(filepath.Join(home, ".zshrc")); string(rc) != "# mine\n" {
		t.Fatalf("install.sh edited the shell startup file: %q", rc)
	}

	must(t, os.WriteFile(filepath.Join(download, name), append(body, 'x'), 0o644))

	other := filepath.Join(root, "other")
	must(t, os.MkdirAll(other, 0o755))

	if out, err := install(other); err == nil || !strings.Contains(out, "sha256") {
		t.Fatalf("install.sh accepted a binary that does not match checksums.txt:\n%s", out)
	}

	if exists(filepath.Join(other, ".local", "bin", "oku")) {
		t.Fatal("a refused install left a binary behind")
	}
}

func TestB105OneHookLineSetsUpPathForOkuAndItsPrograms(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(
		t,
		"",
		"add",
		m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`),
	)
	must(t, err)

	bin := filepath.Dir(m.profile("bin", "tool"))
	okuDir := filepath.Dir(m.exe)

	for shell, show := range map[string]string{
		"bash": `printf '%s' "$PATH"`, "zsh": `printf '%s' "$PATH"`, "fish": `string join : $PATH`,
	} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}

		code, err := m.run(t, "", "hook", shell)
		must(t, err)

		// The hook is loaded twice, as it is when a startup file is sourced again.
		cmd := exec.Command(path, "-c", code+"\n"+code+"\n"+show+"; echo; tool")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", shell, err, out)
		}

		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		// A system startup file may add directories of its own after these two.
		if front := bin + ":" + okuDir + ":"; !strings.HasPrefix(lines[0], front) ||
			strings.Count(lines[0], bin) != 1 {
			t.Fatalf("%s: PATH should start with %q once, got %q", shell, front, lines[0])
		}

		if lines[len(lines)-1] != "hello from tool" {
			t.Fatalf("%s: the installed program does not run by name:\n%s", shell, out)
		}
	}

	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin:/bin")

	out, err := m.run(
		t,
		"",
		"add",
		m.manifest(t, "other", map[string]string{"other": script}, `bin = ["other"]`),
	)
	must(t, err)

	if !strings.Contains(out, "~/.zshrc") || !strings.Contains(out, "hook zsh") {
		t.Fatalf("add did not say which line goes into which file:\n%s", out)
	}
}

func TestB176WaitsSayWhatTheyWaitFor(t *testing.T) {
	m := newMachine(t)

	out, err := m.run(
		t,
		"",
		"add",
		"--yes",
		m.buildManifest(t, false, `needs = ["sh"]`, writeTool+installTool),
	)
	must(t, err)

	if !strings.Contains(out, "tool 1.0.0: building, step 1 of 2 (run)\n") {
		t.Fatalf("add did not say which build step it waited for:\n%s", out)
	}

	script := "#!/bin/sh\necho hi\n"

	out, err = m.run(
		t,
		"",
		"add",
		m.manifest(t, "other", map[string]string{"other": script}, `bin = ["other"]`),
	)
	must(t, err)

	for _, want := range []string{"other 1.2.3: downloading file://", "other 1.2.3: unpacking other"} {
		if !strings.Contains(out, want) {
			t.Fatalf("add did not print %q:\n%s", want, out)
		}
	}
}
