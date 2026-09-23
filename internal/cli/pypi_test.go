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

// fakeUV is a uv that installs the package tool at the version it is asked
// for, the way uv lays out a --target install. Its program prints the version
// and the time uv was told to resolve as of.
const fakeUV = `#!/bin/sh
shift 2
while [ $# -gt 0 ]; do
  case "$1" in
    --target) target="$2"; shift ;;
    --python) python="$2"; shift ;;
    --exclude-newer) before="$2"; shift ;;
    --*) ;;
    *) spec="$1" ;;
  esac
  shift
done
version="${spec#*==}"
mkdir -p "$target/tool" "$target/tool-$version.dist-info" "$target/bin"
printf 'def main():\n    print("tool %s %s")\n' "$version" "$before" > "$target/tool/__init__.py"
printf 'Metadata-Version: 2.1\nName: tool\nVersion: %s\n' "$version" > "$target/tool-$version.dist-info/METADATA"
printf '[console_scripts]\ntool = tool:main\n' > "$target/tool-$version.dist-info/entry_points.txt"
printf '#!%s\nimport tool\ntool.main()\n' "$python" > "$target/bin/tool"
printf 'tool/__init__.py,,\ntool-%s.dist-info/METADATA,,\nbin/tool,sha256=%s,1\n' "$version" "$python" \
  > "$target/tool-$version.dist-info/RECORD"
`

// pypiMachine is a machine whose list names a python and a fake uv, with an
// index that lists the Python package tool. versions maps each version to its
// upload time, and "yanked" in the time yanks it.
func pypiMachine(t *testing.T, versions map[string]string) machine {
	t.Helper()

	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("needs python3")
	}

	m := newMachine(t)

	var releases []string

	for version, uploaded := range versions {
		releases = append(releases, fmt.Sprintf(
			`%q: [{"upload_time_iso_8601": %q, "yanked": %t}]`,
			version, strings.TrimSuffix(uploaded, " yanked"), strings.HasSuffix(uploaded, "yanked"),
		))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/tool/json" {
			http.NotFound(w, r)

			return
		}

		_, _ = fmt.Fprintf(w, `{"info": {"name": "tool", "summary": "A tool", "version": "1.0.0"},
"releases": {%s}}`, strings.Join(releases, ","))
	}))
	t.Cleanup(server.Close)

	m.opts.PyPIIndex = server.URL

	pythonRef := m.manifest(t, "python", map[string]string{
		"bin/python3": "#!/bin/sh\nexec " + python + " \"$@\"\n",
	}, `bin = ["bin/python3"]`)
	uvRef := m.manifest(t, "uv", map[string]string{"uv": fakeUV}, `bin = ["uv"]`)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(fmt.Sprintf(
		"[runtimes]\npython = %q\nuv = %q\n", pythonRef, uvRef,
	)), 0o644))

	return m
}

func TestB256AddInstallsAPythonPackageAsOfItsUploadThroughTheListsPython(t *testing.T) {
	m := pypiMachine(t, map[string]string{"1.0.0": "2026-01-02T03:04:05Z"})

	out, err := m.run(t, "", "add", "pypi:tool", "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// uv resolved as of one second after the version's upload.
	cmd := exec.Command(m.profile("bin", "tool"))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	got, err := cmd.Output()
	must(t, err)

	if strings.TrimSpace(string(got)) != "tool 1.0.0 2026-01-02T03:04:06Z" {
		t.Fatalf("tool printed %q", got)
	}

	for _, name := range []string{"python3", "uv"} {
		if exists(m.profile("bin", name)) {
			t.Fatalf("%s is in the user's profile", name)
		}
	}

	lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(lock), "vendor_sha256") {
		t.Fatalf("the lock pins no digest of the install:\n%s", lock)
	}
}

func TestB256TheInstallDigestIsTheSameUnderAnotherDirectory(t *testing.T) {
	// A machine sets the directories of the whole test, so each gets its own.
	var digests []string

	for _, name := range []string{"one", "another"} {
		t.Run(name, func(t *testing.T) {
			digests = append(digests, pypiDigest(t))
		})
	}

	// A machine skips where these tests cannot run, and a failed one failed t.
	if len(digests) != 2 {
		t.Skip("the machines did not run here")
	}

	// The fake uv writes the python's path into bin/tool and RECORD, as uv does.
	if digests[0] != digests[1] {
		t.Fatalf("the two machines pinned %q", digests)
	}
}

// pypiDigest adds pypi:tool on a new machine and returns the digest line of
// its install in oku.lock.
func pypiDigest(t *testing.T) string {
	t.Helper()

	m := pypiMachine(t, map[string]string{"1.0.0": "2026-01-02T03:04:05Z"})

	_, err := m.run(t, "", "add", "pypi:tool", "--yes")
	must(t, err)

	lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	for _, line := range strings.Split(string(lock), "\n") {
		if strings.HasPrefix(line, "vendor_sha256") {
			return line
		}
	}

	t.Fatalf("no digest in the lock:\n%s", lock)

	return ""
}

func TestB257ThePythonPackageFollowsReleasesAndSkipsPrereleasesAndYanked(t *testing.T) {
	m := pypiMachine(t, map[string]string{
		"1.0.0":    "2026-01-02T03:04:05Z",
		"1.1.0":    "2026-02-02T03:04:05Z yanked",
		"1.2.0rc1": "2026-03-02T03:04:05Z",
		"2.0.dev1": "2026-04-02T03:04:05Z",
	})

	_, err := m.run(t, "", "add", "pypi:tool", "--yes")
	must(t, err)

	if got := m.toolOutput(t); !strings.HasPrefix(got, "tool 1.0.0 ") {
		t.Fatalf("add took %q, want 1.0.0, the newest release that is no prerelease and not yanked", got)
	}

	_, err = m.run(t, "", "add", "pypi:tool@1.2.0rc1", "--yes")
	must(t, err)

	if got := m.toolOutput(t); !strings.HasPrefix(got, "tool 1.2.0rc1 ") {
		t.Fatalf("add @1.2.0rc1 took %q", got)
	}
}
