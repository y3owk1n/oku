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

// listRepo makes the fixtures a git repo with base.toml as its list, includes
// it in the global list, and returns a function that commits new list text.
func listRepo(t *testing.T, m machine, text string) func(string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	commit := func(text string) {
		must(t, os.WriteFile(filepath.Join(m.fixtures, "base.toml"), []byte(text), 0o644))

		for _, args := range [][]string{
			{"init", "--quiet"},
			{"add", "."},
			{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "list"},
		} {
			cmd := exec.Command("git", append([]string{"-C", m.fixtures}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)

			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}

	commit(text)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"),
		[]byte("include = [\"git+file://"+m.fixtures+"#base.toml\"]\n"), 0o644))

	return commit
}

func TestB475UpdateShowsAChangedIncludeAndAsks(t *testing.T) {
	m := newMachine(t)
	m.namedManifest(t, "extra", "extra", "extra")
	m.namedManifest(t, "other", "other", "other")

	commit := listRepo(t, m, "[packages]\nextra = \"./extra.toml\"\n")

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	commit("[packages]\nextra = \"./extra.toml\"\nother = \"./other.toml\"\n")

	out, err := m.run(t, "", "update")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("want update without a terminal refused, got %v:\n%s", err, out)
	}

	if !strings.Contains(out, "[packages]") || !strings.Contains(out, `+ other = "./other.toml"`) ||
		strings.Contains(out, `extra = "./extra.toml"`) {
		t.Fatalf("want the added line under its table and no unchanged line, got:\n%s", out)
	}

	m.opts.Interactive = yes()

	if out, err := m.run(t, "n\n", "update"); err == nil || exists(m.profile("bin", "other")) {
		t.Fatalf("a no should change nothing, got %v:\n%s", err, out)
	}

	if out, err := m.run(t, "y\n", "update"); err != nil || !exists(m.profile("bin", "other")) {
		t.Fatalf("a yes should take the change: %v\n%s", err, out)
	}
}

func TestB476AListAtAURLShowsItsWholeTextAndYesTakesIt(t *testing.T) {
	m := newMachine(t)

	server := httptest.NewServer(http.FileServer(http.Dir(m.fixtures)))
	t.Cleanup(server.Close)

	m.namedManifest(t, "extra", "extra", "extra")
	m.namedManifest(t, "other", "other", "other")

	base := filepath.Join(m.fixtures, "base.toml")
	must(t, os.WriteFile(base, []byte("[packages]\nextra = \""+server.URL+"/extra.toml\"\n"), 0o644))
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"),
		[]byte("include = [\""+server.URL+"/base.toml\"]\n"), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	must(t, os.WriteFile(base, []byte("[packages]\nother = \""+server.URL+"/other.toml\"\n"), 0o644))

	out, err := m.run(t, "", "update")
	if err == nil || !strings.Contains(out, "this is the whole list") {
		t.Fatalf("want the whole list shown and the update refused, got %v:\n%s", err, out)
	}

	out, err = m.run(t, "", "update", "--yes")
	if err != nil || !strings.Contains(out, "--yes took the changed included list") {
		t.Fatalf("--yes should take the change and say so: %v\n%s", err, out)
	}
}
