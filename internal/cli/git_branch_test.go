package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestB177AGitBranchVersionBuildsTheLockedCommitUntilUpdate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}

	m := newMachine(t)
	repo := filepath.Join(m.fixtures, "upstream")

	git := func(args ...string) string {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}

		return strings.TrimSpace(string(out))
	}

	commit := func(rev string) string {
		t.Helper()
		must(t, os.WriteFile(filepath.Join(repo, "rev.txt"), []byte(rev), 0o644))
		git("add", "rev.txt")
		git("commit", "--quiet", "-m", rev)

		return git("rev-parse", "HEAD")[:7]
	}

	must(t, os.MkdirAll(repo, 0o755))
	git("init", "--quiet", "--initial-branch", "main")

	first := commit("first")

	ref := filepath.Join(m.fixtures, "branch.toml")
	must(t, os.WriteFile(ref, []byte(`[package]
name = "tool"

[version]
from = "git-branch"
repo = "file://`+filepath.ToSlash(repo)+`"
branch = "main"

[build]
needs = ["sh", "git"]
source = { git = "file://`+filepath.ToSlash(repo)+`", tag = "{{tag}}" }

[[build.step]]
run = "printf '#!/bin/sh\\necho %s {{version}}\\n' \"$(cat rev.txt)\" > tool"
shell = "sh"

[[build.step]]
install = { bin = ["tool"] }
`), 0o644))

	out, err := m.run(t, "", "add", ref, "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.toolOutput(
		t,
	); !strings.HasPrefix(got, "first ") ||
		!strings.HasSuffix(got, "-"+first) {
		t.Fatalf("add installed %q, want the commit %s of main", got, first)
	}

	second := commit("second")

	// Another machine has the lock and no store. It builds the locked commit,
	// although main has a newer one.
	must(t, os.RemoveAll(filepath.Join(m.data, "store")))
	must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

	out, err = m.run(t, "", "sync", "--yes")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if got := m.toolOutput(t); !strings.HasPrefix(got, "first ") {
		t.Fatalf("sync installed %q, want the locked commit %s", got, first)
	}

	out, err = m.run(t, "", "update", "tool", "--yes")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := m.toolOutput(
		t,
	); !strings.HasPrefix(got, "second ") ||
		!strings.HasSuffix(got, "-"+second) {
		t.Fatalf("update installed %q, want the commit %s of main", got, second)
	}
}

func TestB435AGitFetchThatSshWouldAskAboutFailsWithSshsMessage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}

	m := newMachine(t)
	repo := filepath.Join(m.fixtures, "upstream")

	env := append(
		os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)

	must(t, os.MkdirAll(repo, 0o755))

	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch", "main"},
		{"commit", "--quiet", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = env

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	url := "file://" + filepath.ToSlash(repo)
	ref := filepath.Join(m.fixtures, "branch.toml")
	must(t, os.WriteFile(ref, []byte(`[package]
name = "tool"

[version]
from = "git-branch"
repo = "`+url+`"
branch = "main"

[build]
needs = ["sh", "git"]
source = { git = "`+url+`", tag = "{{tag}}" }

[[build.step]]
run = "printf '#!/bin/sh\\necho ok\\n' > tool"
shell = "sh"

[[build.step]]
install = { bin = ["tool"] }
`), 0o644))

	out, err := m.run(t, "", "add", ref, "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// Real ssh asks on the terminal about an unknown host or a locked key, and
	// waits. With BatchMode it fails at once instead.
	bin := filepath.Join(m.fixtures, "bin")
	must(t, os.MkdirAll(bin, 0o755))
	must(t, os.WriteFile(filepath.Join(bin, "ssh"), []byte(`#!/bin/sh
case " $* " in
*" BatchMode=yes "*) echo "Host key verification failed." >&2 ;;
*) echo "waited on a prompt" >&2 ;;
esac
exit 255
`), 0o755))

	home, _ := os.UserHomeDir()

	for name, config := range map[string]string{
		"a rewrite to ssh": "",
		"the user's core.sshCommand": "[core]\n\tsshCommand = " +
			filepath.Join(bin, "ssh") + " -i key\n",
	} {
		t.Run(name, func(t *testing.T) {
			if config == "" {
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}

			must(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config+
				"[url \"ssh://git@example.invalid/upstream\"]\n\tinsteadOf = "+url+"\n"), 0o644))

			must(t, os.RemoveAll(filepath.Join(m.data, "store")))
			must(t, os.RemoveAll(filepath.Join(m.data, "profiles")))

			out, err := m.run(t, "", "sync", "--locked", "--yes")
			if err == nil || !strings.Contains(out+err.Error(), "Host key verification failed") {
				t.Fatalf("sync = %v, want ssh's own error\n%s", err, out)
			}
		})
	}
}
