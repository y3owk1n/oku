package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/cli"
)

func TestB267ExecRunsACommandWithTheProgramsOfTheDirectory(t *testing.T) {
	m := newMachine(t)

	global := m.manifest(t, "gtool", map[string]string{
		"tool": "#!/bin/sh\necho global \"$@\"\n",
	}, `bin = ["tool"]`)

	_, err := m.run(t, "", "add", global)
	must(t, err)

	// Flags after the command belong to it.
	if out := strings.TrimSpace(m.stdout(t, "exec", "tool", "--flag", "x")); out != "global --flag x" {
		t.Fatalf("exec outside a project printed %q", out)
	}

	project := filepath.Join(m.fixtures, "work", "api")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))

	local := m.manifest(t, "ptool", map[string]string{
		"tool": "#!/bin/sh\necho project \"$GREETING\" \"$@\"\n",
		"fail": "#!/bin/sh\nexit 3\n",
	}, "bin = [\"tool\", \"fail\"]\n[env]\nGREETING = \"hi\"")

	m.opts.WorkDir = project

	_, err = m.run(t, "", "add", local, "--yes")
	must(t, err)

	// A project's [env] could put its own programs first on PATH, so exec
	// applies it only once the user allowed it, as the hook does.
	if _, err := m.run(t, "", "exec", "tool", "a"); err == nil || !strings.Contains(err.Error(), "oku allow") {
		t.Fatalf("want exec refused in a project that is not allowed, got %v", err)
	}

	_, err = m.run(t, "", "allow")
	must(t, err)

	if out := strings.TrimSpace(m.stdout(t, "exec", "tool", "a")); out != "project hi a" {
		t.Fatalf("exec in a project printed %q", out)
	}

	_, err = m.run(t, "", "exec", "fail")

	var exit cli.ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("want the command's exit code 3, got %v", err)
	}

	lockPath := filepath.Join(project, "oku.lock")
	locked, err := os.ReadFile(lockPath)
	must(t, err)
	must(t, os.WriteFile(lockPath, append(locked, "# edited\n"...), 0o644))

	if _, err := m.run(t, "", "exec", "tool"); err == nil || !strings.Contains(err.Error(), "run `oku sync`") {
		t.Fatalf("want exec refused while the profile is behind the lock, got %v", err)
	}
}
