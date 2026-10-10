package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestB579SyncMergesALockWithGitConflicts(t *testing.T) {
	m := newMachine(t)

	// tool writes the manifest of name at version, whose program prints it.
	tool := func(name, version string) string {
		archive, sum := m.archive(t, name+"-"+version, map[string]string{name: "#!/bin/sh\necho " + version + "\n"})
		path := filepath.Join(m.fixtures, name+".toml")
		must(t, os.WriteFile(path, []byte(fmt.Sprintf(
			"[package]\nname = %q\n[version]\nvalue = %q\n[[artifact]]\nurl = \"file://%s\"\nsha256 = %q\nbin = [%q]\n",
			name, version, archive, sum, name,
		)), 0o644))

		return path
	}

	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{
			"-C", m.config, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)

		// A merge with conflicts exits with 1.
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run := func(args ...string) string {
		t.Helper()

		out, err := m.run(t, "", args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}

		return out
	}

	a := tool("a", "1.0.0")
	m.writeOwnList(t, fmt.Sprintf("[packages]\na = %q\n", a))
	run("sync")
	git("init", "--quiet", "--initial-branch=main")
	git("add", ".")
	git("commit", "--quiet", "-m", "base")

	// One branch moves a to 2.0.0, the other to 1.5.0 and adds b.
	git("switch", "--quiet", "-c", "feature")
	tool("a", "2.0.0")
	run("update", "a")
	git("commit", "--quiet", "-am", "a 2.0.0")

	git("switch", "--quiet", "main")
	tool("a", "1.5.0")
	m.writeOwnList(t, fmt.Sprintf("[packages]\na = %q\nb = %q\n", a, tool("b", "1.0.0")))
	run("update")
	git("commit", "--quiet", "-am", "a 1.5.0 and b")

	git("merge", "--quiet", "feature")
	tool("a", "2.0.0")

	lockPath := filepath.Join(m.config, "oku.lock")

	conflicted, err := os.ReadFile(lockPath)
	must(t, err)

	if !strings.Contains(string(conflicted), "<<<<<<< HEAD") {
		t.Fatalf("the merge should leave conflicts in oku.lock:\n%s", conflicted)
	}

	// Another command names the conflict and the way out.
	if _, err := m.run(t, "", "outdated"); err == nil || !strings.Contains(err.Error(), "run `oku sync` to merge them") {
		t.Fatalf("outdated on a conflicted lock should point at sync, got %v", err)
	}

	if _, err := m.run(t, "", "sync", "--locked"); err == nil || !strings.Contains(err.Error(), "--locked keeps oku from merging") {
		t.Fatalf("sync --locked should refuse to merge, got %v", err)
	}

	// A dry run puts the conflicts back.
	run("sync", "--dry-run")

	if now, _ := os.ReadFile(lockPath); string(now) != string(conflicted) {
		t.Fatalf("a dry run changed the conflicted lock:\n%s", now)
	}

	out := run("sync")
	if !strings.Contains(out, "a: took 2.0.0 from feature over 1.5.0") {
		t.Fatalf("sync should say which side it took:\n%s", out)
	}

	if merged, _ := os.ReadFile(lockPath); strings.Contains(string(merged), "<<<<<<<") ||
		!strings.Contains(string(merged), "name = 'b'") {
		t.Fatalf("the merged lock should keep b and no conflict:\n%s", merged)
	}

	var installed []struct{ Name, Version string }
	must(t, json.Unmarshal([]byte(run("list", "--json")), &installed))

	if fmt.Sprint(installed) != "[{a 2.0.0} {b 1.0.0}]" {
		t.Fatalf("sync should install a at 2.0.0 and b, the profile holds %v:\n%s", installed, out)
	}

	// Once git no longer holds the sides, oku does not guess them from the
	// markers.
	must(t, os.WriteFile(lockPath, conflicted, 0o644))
	git("add", "oku.lock")

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "git holds no copy") {
		t.Fatalf("sync without the sides in git should refuse, got %v", err)
	}
}
