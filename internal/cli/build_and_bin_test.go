package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestB206ManPagesUnderThePrefixManDirectoryReachTheProfile(t *testing.T) {
	m := newMachine(t)
	ref := m.buildManifest(t, false, "",
		"[[build.step]]\nrun = \"mkdir -p {{prefix}}/man/man1 && printf '.TH' > {{prefix}}/man/man1/tool.1\"\n"+
			"shell = \"sh\"\n"+writeTool+installTool)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !exists(m.profile("share", "man", "man1", "tool.1")) {
		t.Fatal("the man page under {{prefix}}/man is not in the profile's share/man")
	}
}

func TestB207BuildPathHoldsTheSbinDirectoriesLast(t *testing.T) {
	m := newMachine(t)
	ref := m.buildManifest(t, false, "",
		"[[build.step]]\nrun = \"printf '#!/bin/sh\\\\necho %s\\\\n' \\\"$PATH\\\" > tool\"\nshell = \"sh\"\n"+
			installTool)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	got := filepath.SplitList(m.output(t, "tool"))

	want := []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("the build PATH ends in %v, want %v", got, want)
	}
}

func TestB208ANeedsToolIsOnPathWithoutItsSiblings(t *testing.T) {
	m := newMachine(t)

	tools := filepath.Join(m.fixtures, "tools")
	must(t, os.Mkdir(tools, 0o755))
	must(t, os.WriteFile(
		filepath.Join(tools, "mytool"), []byte("#!/bin/sh\necho '#!/bin/sh'\necho 'echo from mytool'\n"), 0o755,
	))
	must(t, os.WriteFile(filepath.Join(tools, "sibling"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))

	ref := m.buildManifest(t, false, `needs = ["mytool"]`,
		"[[build.step]]\nrun = \"mytool > tool; if command -v sibling; then echo 'echo sibling-seen' >> tool; fi\"\n"+
			"shell = \"sh\"\n"+installTool)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.output(t, "tool"); got != "from mytool" {
		t.Fatalf("the build saw more than the needs tool: %q", got)
	}
}

func TestB209ABinTableWithAPathExposesAFileUnderAnotherName(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "ff", map[string]string{
		"bin/ffmpeg":  "#!/bin/sh\necho ffmpeg\n",
		"bin/ffprobe": "#!/bin/sh\necho ffprobe\n",
	}, `bin = ["bin/ffmpeg", { name = "probe", path = "bin/ffprobe" }]`)

	if out, err := m.run(t, "", "manifest", "lint", ref); err != nil {
		t.Fatalf("lint: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.output(t, "probe"); got != "ffprobe" {
		t.Fatalf("probe printed %q", got)
	}

	if got := m.output(t, "ffmpeg"); got != "ffmpeg" {
		t.Fatalf("ffmpeg printed %q", got)
	}

	// An install step takes the same table.
	built := m.buildManifest(t, false, "",
		"[[build.step]]\nrun = \"printf '#!/bin/sh\\\\necho built probe\\\\n' > ffprobe\"\nshell = \"sh\"\n"+
			"[[build.step]]\ninstall = { bin = [{ name = \"probe2\", path = \"ffprobe\" }] }\n")

	if out, err := m.run(t, "", "add", built, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.output(t, "probe2"); got != "built probe" {
		t.Fatalf("probe2 printed %q", got)
	}

	both := m.manifest(t, "both", map[string]string{"bin/x": "#!/bin/sh\n"},
		`bin = [{ name = "x", path = "bin/x", run = "/bin/sh" }]`)

	out, err := m.run(t, "", "manifest", "lint", both)
	if err == nil || !strings.Contains(out, "path") {
		t.Fatalf("lint accepted path together with run: %v\n%s", err, out)
	}

	if out, err := m.run(t, "", "add", both); err == nil {
		t.Fatalf("add accepted path together with run:\n%s", out)
	}
}

func TestB551ABuildRunsTheShellOnItsOwnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell that oku's PATH holds is a shell script")
	}

	m := newMachine(t)
	source := filepath.Join(m.fixtures, "completer")
	must(t, os.WriteFile(source, []byte(completer), 0o755))

	// oku's own PATH holds only a sh that fails. A host that cannot sandbox, such
	// as a Linux container without user namespaces, then shows which sh ran.
	fake := t.TempDir()
	must(t, os.WriteFile(filepath.Join(fake, "sh"), []byte("#!/bin/sh\necho the sh on the PATH of oku ran\nexit 1\n"), 0o755))
	t.Setenv("PATH", fake)

	ref := m.buildManifest(
		t, false, "",
		fmt.Sprintf("[[build.step]]\nrun = \"cp %s tool\"\nshell = \"sh\"\n", source)+
			"[[build.step]]\ninstall = { bin = [\"tool\"], "+
			"completions = { generate = \"tool completions {{shell}}\" } }\n",
	)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	body, err := os.ReadFile(m.profile("share", "completions", "zsh", "_tool"))
	if err != nil || string(body) != "complete zsh\n" {
		t.Fatalf("zsh completions: %v %q", err, body)
	}
}
