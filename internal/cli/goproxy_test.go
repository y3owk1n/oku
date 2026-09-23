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
)

// fakeGo is a go command that does what oku asks of it for a module:
// "mod download -json" puts the module in the module cache, "mod download" in
// its directory adds a dependency, and "install" writes a program that prints
// the package, the version and where the go command read modules from.
const fakeGo = `#!/bin/sh
case "$1 $2" in
"mod download")
  if [ "$3" = -json ]; then
    module="${4%@*}"; version="${4#*@}"
    dir="$GOMODCACHE/$module@$version"
    mkdir -p "$dir" "$GOMODCACHE/cache/download/$module/@v"
    echo "module $module" > "$dir/go.mod"
    echo "zip of $module $version" > "$GOMODCACHE/cache/download/$module/@v/$version.zip"
    printf '{\n\t"Path": "%s",\n\t"Dir": "%s",\n}\n' "$module" "$dir"
  else
    mkdir -p "$GOMODCACHE/cache/download/example.com/dep/@v" "$GOMODCACHE/cache/download/sumdb"
    echo "zip of dep" > "$GOMODCACHE/cache/download/example.com/dep/@v/v0.1.0.zip"
    echo $$ > "$GOMODCACHE/cache/download/sumdb/tile"
  fi ;;
"install -trimpath")
  pkg="${3%@*}"; version="${3#*@}"
  mkdir -p "$GOBIN"
  printf '#!/bin/sh\necho %s %s %s\n' "$pkg" "$version" "$GOPROXY" > "$GOBIN/${pkg##*/}"
  chmod +x "$GOBIN/${pkg##*/}" ;;
*) echo "fake go cannot $*" >&2; exit 1 ;;
esac
`

// goMachine is a machine whose list names a fake go, with a module proxy that
// knows the module example.com/tool at versions and nothing else.
func goMachine(t *testing.T, versions ...string) machine {
	t.Helper()

	m := newMachine(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/tool/@v/list" {
			// The proxy answers 410 for a path that is no module.
			http.Error(w, "gone", http.StatusGone)

			return
		}

		for _, version := range versions {
			fmt.Fprintln(w, "v"+version)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GoProxy = server.URL

	goRef := m.manifest(t, "go", map[string]string{"bin/go": fakeGo}, `bin = ["bin/go"]`)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[runtimes]\ngo = %q\n", goRef,
	)), 0o644))

	return m
}

func TestB258AddBuildsAGoProgramFromItsModuleWithTheListsGo(t *testing.T) {
	m := goMachine(t, "1.0.0", "1.2.0", "1.3.0-rc.1")

	out, err := m.run(t, "", "add", "go:example.com/tool/cmd/tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// The program was installed offline from the module cache of the build.
	got, err := exec.Command(m.profile("bin", "tool")).Output()
	must(t, err)

	if fields := strings.Fields(string(got)); len(fields) != 3 ||
		fields[0] != "example.com/tool/cmd/tool" || fields[1] != "v1.2.0" ||
		!strings.HasPrefix(fields[2], "file://") {
		t.Fatalf("tool printed %q, want the package at v1.2.0 from a file proxy", got)
	}

	if exists(m.profile("bin", "go")) {
		t.Fatal("go is in the user's profile")
	}

	lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(lock), "vendor_sha256") || !strings.Contains(string(lock), "go:example.com/tool/cmd/tool") {
		t.Fatalf("the lock does not pin the ref and a digest of the modules:\n%s", lock)
	}
}

func TestB258ARerunOfTheDownloadGivesTheSameDigest(t *testing.T) {
	m := goMachine(t, "1.2.0")

	_, err := m.run(t, "", "add", "go:example.com/tool/cmd/tool", "--yes")
	must(t, err)

	// The fake writes its pid into the checksum database's files, which oku
	// leaves out of the digest, so a rebuild matches what the lock pinned.
	out, err := m.run(t, "", "sync", "--yes", "--rebuild", "tool")
	if err != nil {
		t.Fatalf("a rebuild whose downloads are the same failed: %v\n%s", err, out)
	}
}

func TestB259AGoProgramFollowsTaggedVersionsAndSkipsPrereleases(t *testing.T) {
	m := goMachine(t, "1.0.0", "1.3.0-rc.1")

	_, err := m.run(t, "", "add", "go:example.com/tool", "--yes")
	must(t, err)

	if got := m.toolOutput(t); !strings.Contains(got, " v1.0.0 ") {
		t.Fatalf("add took %q, want v1.0.0, the newest that is no prerelease", got)
	}

	_, err = m.run(t, "", "add", "go:example.com/tool@1.3.0-rc.1", "--yes")
	must(t, err)

	if got := m.toolOutput(t); !strings.Contains(got, " v1.3.0-rc.1 ") {
		t.Fatalf("add @1.3.0-rc.1 took %q", got)
	}
}
