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

// fakeCargo is a cargo that vendors one dependency and installs the program
// tool, which prints the version the crate's VERSION file holds.
const fakeCargo = `#!/bin/sh
case "$1" in
vendor) mkdir -p vendor/dep && echo dep > vendor/dep/lib.rs ;;
install)
  while [ $# -gt 0 ]; do [ "$1" = --root ] && root="$2"; shift; done
  mkdir -p "$root/bin"
  printf '#!/bin/sh\necho tool %s\n' "$(cat VERSION)" > "$root/bin/tool"
  chmod +x "$root/bin/tool" ;;
*) echo "fake cargo cannot $*" >&2; exit 1 ;;
esac
`

// crate is one version that the fake crates.io serves.
type crate struct {
	version string
	yanked  bool
	// library leaves out the programs, as crates.io lists a library.
	library bool
	// tampered serves other bytes than the checksum names.
	tampered bool
}

// cratesMachine is a machine whose list names a fake cargo, with a crates.io
// that serves the crate tool at versions.
func cratesMachine(t *testing.T, versions ...crate) machine {
	t.Helper()

	m := newMachine(t)

	files := map[string]string{}

	var listed []string

	for _, v := range versions {
		archive, sum := m.archive(t, "tool-"+v.version, map[string]string{
			"tool-" + v.version + "/VERSION":    v.version,
			"tool-" + v.version + "/Cargo.toml": "[package]\nname = \"tool\"\n",
			"tool-" + v.version + "/Cargo.lock": "# locked\n",
		})

		files["/crates/tool/tool-"+v.version+".crate"] = archive

		if v.tampered {
			sum = strings.Repeat("0", 64)
		}

		bins := `["tool"]`
		if v.library {
			bins = `[]`
		}

		listed = append(listed, fmt.Sprintf(
			`{"num": %q, "checksum": %q, "yanked": %t, "bin_names": %s}`, v.version, sum, v.yanked, bins,
		))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/crates/tool" {
			_, _ = fmt.Fprintf(w, `{"crate": {"name": "tool", "description": "A tool"}, "versions": [%s]}`,
				strings.Join(listed, ","))

			return
		}

		if file, ok := files[r.URL.Path]; ok {
			http.ServeFile(w, r, file)

			return
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	m.opts.CratesAPI = server.URL + "/api"
	m.opts.CrateDownloads = server.URL + "/crates"

	rust := m.manifest(t, "rust", map[string]string{"bin/cargo": fakeCargo}, `bin = ["bin/cargo"]`)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[runtimes]\nrust = %q\n", rust,
	)), 0o644))

	return m
}

func TestB260AddBuildsACrateFromItsCheckedDownloadWithTheListsRust(t *testing.T) {
	m := cratesMachine(t, crate{version: "1.2.0"}, crate{version: "1.1.0"})

	out, err := m.run(t, "", "add", "cargo:tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "tool 1.2.0" {
		t.Fatalf("tool printed %q, want the newest release 1.2.0", got)
	}

	// The digest came from crates.io, so oku trusted nothing on first use.
	if strings.Contains(out, "trusted this download") {
		t.Fatalf("oku trusted the .crate file on first use:\n%s", out)
	}

	if exists(m.profile("bin", "cargo")) {
		t.Fatal("cargo is in the user's profile")
	}
}

func TestB260ACrateWhoseDownloadDoesNotMatchItsChecksumIsRefused(t *testing.T) {
	m := cratesMachine(t, crate{version: "1.2.0", tampered: true})

	_, err := m.run(t, "", "add", "cargo:tool", "--yes")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want a refusal of the download, got %v", err)
	}

	if exists(m.profile("bin", "tool")) {
		t.Fatal("a crate that failed its checksum was installed")
	}
}

func TestB261ACrateFollowsReleasesSkipsYankedAndRefusesALibrary(t *testing.T) {
	m := cratesMachine(
		t,
		crate{version: "2.0.0-beta.1"}, crate{version: "1.3.0", yanked: true}, crate{version: "1.2.0"},
	)

	_, err := m.run(t, "", "add", "cargo:tool", "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "tool 1.2.0" {
		t.Fatalf("add took %q, want 1.2.0, the newest that is not yanked and no prerelease", got)
	}

	_, err = m.run(t, "", "add", "cargo:tool@2.0.0-beta.1", "--yes")
	must(t, err)

	if got := m.toolOutput(t); got != "tool 2.0.0-beta.1" {
		t.Fatalf("add @2.0.0-beta.1 took %q", got)
	}

	library := cratesMachine(t, crate{version: "1.0.0", library: true})

	if _, err := library.run(t, "", "add", "cargo:tool", "--yes"); err == nil ||
		!strings.Contains(err.Error(), "has no programs") {
		t.Fatalf("want a library crate refused, got %v", err)
	}
}
