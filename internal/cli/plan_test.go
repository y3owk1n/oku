package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/busy"
	"github.com/y3owk1n/oku/internal/cli"
	"github.com/y3owk1n/oku/internal/platform"
)

func TestB289AddPlanSaysWhatAddWouldDoAndChangesNothing(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": script,
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	// A plan only reads, so it runs while another oku process holds the lock.
	release, err := busy.Lock(context.Background(), m.data, func(int) {})
	must(t, err)

	out, err := m.run(t, "", "add", "github:owner/tool", "--plan")
	release()

	if err != nil {
		t.Fatalf("add --plan: %v\n%s", err, out)
	}

	for _, want := range []string{
		"1.4.0", "inferred by oku", hostAssetName(), "download for " + platform.Host().String(),
		"programs", "installed", "--manifest", "plan: nothing was changed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the plan lacks %q:\n%s", want, out)
		}
	}

	for _, path := range []string{filepath.Join(m.config, "oku.toml"), filepath.Join(m.config, "oku.lock")} {
		if exists(path) {
			t.Fatalf("add --plan wrote %s:\n%s", path, out)
		}
	}

	if got := m.storeEntries(t); len(got) != 0 {
		t.Fatalf("add --plan filled the store with %v", got)
	}

	out, err = m.run(t, "", "add", "github:owner/tool", "--plan", "--json")
	must(t, err)

	var plans []struct {
		Name     string   `json:"name"`
		Version  string   `json:"version"`
		Install  string   `json:"install"`
		Programs []string `json:"programs"`
	}

	must(t, json.Unmarshal([]byte(out[strings.Index(out, "["):]), &plans))

	if len(plans) != 1 || plans[0].Install != "download" || plans[0].Version != "1.4.0" ||
		len(plans[0].Programs) != 1 || plans[0].Programs[0] != "tool" {
		t.Fatalf("add --plan --json printed %+v", plans)
	}
}

func TestB289AddPlanFailsWhereAddWouldFail(t *testing.T) {
	m := newMachine(t)

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	gone := filepath.Join(m.fixtures, "gone.toml")
	must(t, os.WriteFile(gone, []byte(`[package]
name = "gone"

[version]
value = "1.0.0"

[[artifact]]
url = "`+server.URL+`/gone.tar.gz"
bin = ["gone"]
`), 0o644))

	out, err := m.run(t, "", "add", gone, "--plan")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want the plan of a missing download to fail with 404, got %v\n%s", err, out)
	}

	_, err = m.run(t, "", "add", "missing", "--plan")
	if err == nil || !strings.Contains(err.Error(), "there is no file named missing here") {
		t.Fatalf("want a bare word to fail as no file, got %v", err)
	}
}

func TestB290AddManifestPrintsAManifestThatAddsTheSamePackage(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	// The manifest covers every platform, not only the host.
	inferServer(t, &m, map[string]string{
		hostAssetName():                           archive,
		"tool-v1.4.0-x86_64-pc-windows-msvc.zip":  archive,
		"tool-v1.4.0-aarch64-pc-windows-msvc.zip": archive,
	})

	var stdout bytes.Buffer

	cmd := cli.NewRootCmd(m.opts)
	cmd.SetArgs([]string{"add", "github:owner/tool", "--manifest"})
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	must(t, cmd.Execute())

	if !strings.Contains(stdout.String(), `match = { os = "windows", arch = "arm64" }`) {
		t.Fatalf("the manifest has no artifact for windows arm64:\n%s", stdout.String())
	}

	saved := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(saved, stdout.Bytes(), 0o644))

	if out, err := m.run(t, "", "add", saved); err != nil {
		t.Fatalf("the printed manifest does not install: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("tool printed %q", got)
	}
}
