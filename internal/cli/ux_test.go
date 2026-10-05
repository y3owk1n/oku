package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/cli"
)

func TestB541AWrongArgumentCountShowsTheCommandsUsage(t *testing.T) {
	m := newMachine(t)

	for _, args := range [][]string{
		{"list", "extra"}, {"rollback", "1", "2"}, {"shell"}, {"run"}, {"exec"},
		{"cache", "push"}, {"sync", "a", "b"}, {"manifest", "init"},
	} {
		_, err := m.run(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "usage: oku "+args[0]) ||
			!strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "too many") {
			t.Fatalf("%v: want the problem and the usage line, got %v", args, err)
		}
	}
}

func TestB542CacheAddTakesAURLOrADirectoryThatExists(t *testing.T) {
	m := newMachine(t)

	for _, location := range []string{"ftp://cache.example.com", "https://", filepath.Join(m.fixtures, "missing")} {
		if out, err := m.run(t, "", "cache", "add", location); err == nil {
			t.Fatalf("cache add %s should fail:\n%s", location, out)
		}
	}

	must(t, os.MkdirAll(m.fixtures, 0o755))

	if out, err := m.run(t, "", "cache", "add", m.fixtures); err != nil {
		t.Fatalf("cache add of a directory: %v\n%s", err, out)
	}
}

func TestB196WhichRefusesANameThatIsNoProgram(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"", ".", "..", "bin/tool"} {
		if _, err := m.run(t, "", "which", name); err == nil || !strings.Contains(err.Error(), "is no program name") {
			t.Fatalf("which %q: want it refused as no program name, got %v", name, err)
		}
	}
}

func TestB195RemoveCountsANameOnceAndRefusesAnEmptyOne(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`))
	must(t, err)

	if _, err := m.run(t, "", "remove", ""); err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("remove of an empty name: %v", err)
	}

	out, err := m.run(t, "", "remove", "tool", "tool")
	if err != nil || strings.Contains(out, "tool tool") || strings.Contains(out, "tool, tool") {
		t.Fatalf("remove of a name given twice: %v\n%s", err, out)
	}
}

func TestB74AnUnknownServiceWithNoneInstalledSaysThereIsNone(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "service", "start", "nosuch")
	if err == nil || !strings.Contains(err.Error(), "or any service") || strings.HasSuffix(err.Error(), ": ") {
		t.Fatalf("want a refusal that says no package ships a service, got %v", err)
	}
}

func TestB395AMinimumReleaseAgeThatDoesNotParseNamesTheFlag(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	if _, err := m.run(t, "", "add", ref, "--min-release-age", "banana"); err == nil ||
		!strings.Contains(err.Error(), "--min-release-age") {
		t.Fatalf("want the error to name --min-release-age, got %v", err)
	}
}

func TestB125AMissingFileIsNamedPlainly(t *testing.T) {
	m := newMachine(t)
	missing := filepath.Join(m.fixtures, "nope.toml")

	for command, want := range map[string]string{
		"hash": "there is no file at", "lint": "there is no manifest at", "bump": "there is no manifest at",
	} {
		if _, err := m.run(t, "", "manifest", command, missing); err == nil || !strings.Contains(err.Error(), want) ||
			strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "no such file") {
			t.Fatalf("manifest %s: want %q, got %v", command, want, err)
		}
	}
}

func TestB543AListThatDoesNotParseNamesItsLineEvenInList(t *testing.T) {
	m := newMachine(t)

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"),
		[]byte("[packages]\ntool = { ref = \"x\"\nother = \"y\"\n"), 0o644))

	m.opts.WorkDir = project

	out, err := m.run(t, "", "list")
	if err != nil || !strings.Contains(out, "line 3, column 1") {
		t.Fatalf("list should show where oku.toml does not parse: %v\n%s", err, out)
	}

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "line 3, column 1") {
		t.Fatalf("sync should name the line: %v", err)
	}
}

func TestB18SyncOfAListFileThatIsMissingSaysSo(t *testing.T) {
	m := newMachine(t)
	m.writeFilesList(t, "[vars]\nname = \"x\"\n")

	missing := filepath.Join(m.fixtures, "team.toml")
	if _, err := m.run(t, "", "sync", missing); err == nil || !strings.Contains(err.Error(), "there is no list at") {
		t.Fatalf("want sync of a missing file to say so, got %v", err)
	}
}

func TestB167ADryRunPrintsOnlyWhatWouldChange(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0", "1.1.0"))
	must(t, err)

	server.tags = []string{"v1.1.0", "v1.0.0"}

	for line := range strings.SplitSeq(strings.TrimSpace(m.stdout(t, "update", "--dry-run")), "\n") {
		if !strings.Contains(line, "would") && !strings.Contains(line, "dry run") {
			t.Fatalf("a dry run printed %q, which is no would line", line)
		}
	}
}

func TestB544NotesAndQuestionsGoToStderr(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, strings.TrimSuffix(hostAssetName(), ".tar.gz"), map[string]string{
		"tool-1.4.0/tool": "#!/bin/sh\necho inferred\n",
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	stdout, stderr := m.split(t, "", "add", "github:owner/tool")
	if strings.Contains(stdout, "inferred") || !strings.Contains(stderr, "inferred") {
		t.Fatalf("the note about the inferred manifest should go to stderr only:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	m.opts.Interactive = yes()

	stdout, stderr = m.split(t, "y\n", "add", m.buildManifest(t, false, "", writeTool+installTool))
	if strings.Contains(stdout, "y/N") || !strings.Contains(stderr, "y/N") {
		t.Fatalf("the build approval should go to stderr only:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

// split runs oku with stdin and returns what it wrote to stdout and to stderr.
func (m machine) split(t *testing.T, stdin string, args ...string) (stdout, stderr string) {
	t.Helper()

	var out, errOut bytes.Buffer

	cmd := cli.NewRootCmd(m.opts)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	must(t, cmd.Execute())

	return out.String(), errOut.String()
}

func TestB546AnEmptyGenerationStateStopsGCAndDeletesNothing(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`))
	must(t, err)

	state := filepath.Join(m.data, "profiles", "global", "gen-1", "oku-gen.toml")
	must(t, os.Chmod(state, 0o644))
	must(t, os.WriteFile(state, nil, 0o644))

	for _, args := range [][]string{{"gc"}, {"generations"}} {
		if _, err := m.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("%v should stop at an empty state file, got %v", args, err)
		}
	}

	if got := m.toolOutput(t); got != "hello from tool" {
		t.Fatalf("gc deleted what the generation holds, tool printed %q", got)
	}

	// The error says to delete the file and sync.
	must(t, os.Remove(state))

	if out, err := m.run(t, "", "sync"); err != nil || m.toolOutput(t) != "hello from tool" {
		t.Fatalf("sync after deleting the empty state: %v\n%s", err, out)
	}

	_, err = m.run(t, "", "gc")
	must(t, err)
}
