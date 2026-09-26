package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitManifest writes a manifest that builds tool at version into the git
// repo at repo, commits it, and returns its ref.
func (m machine) commitManifest(t *testing.T, repo, version string) string {
	t.Helper()

	must(t, os.MkdirAll(repo, 0o755))
	must(t, os.WriteFile(filepath.Join(repo, "tool.toml"), []byte(
		"[package]\nname = \"tool\"\n[version]\nvalue = \""+version+"\"\n[build]\n"+writeTool+installTool,
	), 0o644))

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", version},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	return "git+file://" + repo + "#tool.toml"
}

func TestB377ADeclinedBuildKeepsTheLockedVersionAndTheUpdateGoesOn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	m := newMachine(t)
	repo := filepath.Join(m.fixtures, "repo")
	ref := m.commitManifest(t, repo, "1.0.0")

	m.opts.Interactive = yes()

	_, err := m.run(t, "y\n", "add", ref)
	must(t, err)

	// A new version whose manifest changed asks again.
	m.commitManifest(t, repo, "1.1.0")

	for _, tc := range []struct {
		name  string
		stdin string
		tty   bool
	}{
		{"a no on a terminal", "n\n", true},
		{"without a terminal", "", false},
	} {
		m.opts.Interactive = &tc.tty

		out, err := m.run(t, tc.stdin, "update")
		if err != nil {
			t.Fatalf("%s: update failed: %v\n%s", tc.name, err, out)
		}

		if got := m.toolOutput(t); got != "built 1.0.0" || !strings.Contains(out, "tool stays at 1.0.0") {
			t.Fatalf("%s: update left %q and said:\n%s", tc.name, got, out)
		}
	}

	m.opts.Interactive = yes()

	_, err = m.run(t, "y\n", "update")
	must(t, err)

	if got := m.toolOutput(t); got != "built 1.1.0" {
		t.Fatalf("a yes left %q", got)
	}
}
