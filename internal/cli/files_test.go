package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFilesList replaces the global oku.toml with body, which holds [files].
func (m machine) writeFilesList(t *testing.T, body string) {
	t.Helper()

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(body), 0o644))
}

func home(elem ...string) string {
	return filepath.Join(append([]string{os.Getenv("HOME")}, elem...)...)
}

func TestB135ALinkShowsAnEditOfItsSourceWithoutASync(t *testing.T) {
	m := newMachine(t)

	source := filepath.Join(m.config, "files", "nvim", "init.lua")
	must(t, os.MkdirAll(filepath.Dir(source), 0o755))
	must(t, os.WriteFile(source, []byte("first"), 0o644))

	m.writeFilesList(t, "[files]\n\"{{home}}/.config/nvim\" = { link = \"./files/nvim\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".config", "nvim", "init.lua")); string(body) != "first" {
		t.Fatalf("the target holds %q", body)
	}

	must(t, os.WriteFile(source, []byte("second"), 0o644))

	if body, _ := os.ReadFile(home(".config", "nvim", "init.lua")); string(body) != "second" {
		t.Fatalf("after an edit of the source the target holds %q", body)
	}
}

func TestB136ATextEntryWritesThatTextWithItsMode(t *testing.T) {
	m := newMachine(t)

	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.secret\" = { text = \"key\\n\", mode = \"0600\" }\n"+
		"\"{{home}}/.plain\" = { text = \"\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	body, err := os.ReadFile(home(".secret"))
	must(t, err)

	info, err := os.Stat(home(".secret"))
	must(t, err)

	if string(body) != "key\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("the target holds %q with mode %o", body, info.Mode().Perm())
	}

	if info, err := os.Stat(home(".plain")); err != nil || info.Size() != 0 {
		t.Fatalf("an empty text should give an empty file, got %v", err)
	}
}

func TestB137ATargetThatOkuDidNotWriteIsRefusedAndKept(t *testing.T) {
	m := newMachine(t)

	must(t, os.WriteFile(home(".gitconfig"), []byte("mine"), 0o644))
	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.aaa\" = { text = \"first in order\" }\n"+
		"\"{{home}}/.gitconfig\" = { text = \"from the list\" }\n")

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), home(".gitconfig")) {
		t.Fatalf("sync should refuse and name the target, got %v", err)
	}

	if body, _ := os.ReadFile(home(".gitconfig")); string(body) != "mine" {
		t.Fatalf("the user's file now holds %q", body)
	}

	if exists(home(".aaa")) {
		t.Fatal("sync wrote another target before it refused")
	}
}

func TestB138ARemovedEntryIsGoneAndRollbackBringsBackItsBytes(t *testing.T) {
	m := newMachine(t)
	target := home(".config", "tool", "config")

	m.writeFilesList(t, "[files]\n\"{{home}}/.config/tool/config\" = { text = \"one\" }\n")
	_, err := m.run(t, "", "sync")
	must(t, err)

	m.writeFilesList(t, "[files]\n\"{{home}}/.config/tool/config\" = { text = \"two\" }\n")
	_, err = m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(target); string(body) != "two" {
		t.Fatalf("the second sync left %q", body)
	}

	m.writeFilesList(t, "")
	_, err = m.run(t, "", "sync")
	must(t, err)

	if _, err := os.Lstat(target); err == nil {
		t.Fatal("the target is still there after its entry left the list")
	}

	_, err = m.run(t, "", "rollback", "1")
	must(t, err)

	if body, _ := os.ReadFile(target); string(body) != "one" {
		t.Fatalf("rollback to generation 1 gave %q, want its bytes", body)
	}
}

func TestB139ATargetStartsWithALocationOfThisOS(t *testing.T) {
	m := newMachine(t)

	for _, tc := range []struct{ target, want string }{
		{"/etc/hosts", "starts with a location"},
		{"{{nowhere}}/x", "is not a location"},
		{"{{localappdata}}/x", "Windows only"},
	} {
		m.writeFilesList(t, "[files]\n\""+tc.target+"\" = { text = \"x\" }\n")

		_, err := m.run(t, "", "sync")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want an error with %q, got %v", tc.target, tc.want, err)
		}
	}

	m.writeFilesList(t, "[files]\n"+
		"\"{{localappdata}}/x\" = { text = \"x\", when = { os = \"windows\" } }\n"+
		"\"{{config}}/tool.conf\" = { text = \"here\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(
		filepath.Join(filepath.Dir(m.config), "tool.conf"),
	); string(
		body,
	) != "here" {
		t.Fatalf("{{config}} is not the config home, the target holds %q", body)
	}
}

func TestB140ALinkIntoAPackageFollowsItsVersion(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0", "1.1.0"))
	must(t, err)

	listed, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	m.writeFilesList(t, string(listed)+
		"\n[files]\n\"{{home}}/.tool-script\" = { link = \"{{pkg.tool}}/tool\" }\n")

	_, err = m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".tool-script")); !strings.Contains(string(body), "1.0.0") {
		t.Fatalf("the target should be the file of tool 1.0.0, got %q", body)
	}

	server.tags = append(server.tags, "v1.1.0")

	_, err = m.run(t, "", "update")
	must(t, err)

	if body, _ := os.ReadFile(home(".tool-script")); !strings.Contains(string(body), "1.1.0") {
		t.Fatalf("after update the target should be the file of tool 1.1.0, got %q", body)
	}
}

func TestB142AProjectListWithFilesIsAnError(t *testing.T) {
	m := newMachine(t)

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(
		filepath.Join(project, "oku.toml"),
		[]byte("[files]\n\"{{home}}/.ssh/authorized_keys\" = { text = \"attacker\" }\n"),
		0o644,
	))

	m.opts.WorkDir = project

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "only the global list") {
		t.Fatalf("sync of a project with [files] should fail, got %v", err)
	}

	if exists(home(".ssh", "authorized_keys")) {
		t.Fatal("a project list wrote into the home directory")
	}

	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("[vars]\na = \"b\"\n"), 0o644))

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "[vars]") {
		t.Fatalf("sync of a project with [vars] should fail, got %v", err)
	}
}

func TestB95UninstallRemovesFilesAndKeepsTheirSources(t *testing.T) {
	m := newMachine(t)

	source := filepath.Join(m.config, "files", "nvim", "init.lua")
	must(t, os.MkdirAll(filepath.Dir(source), 0o755))
	must(t, os.WriteFile(source, []byte("mine"), 0o644))

	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.config/nvim\" = { link = \"./files/nvim\" }\n"+
		"\"{{home}}/.hushlogin\" = { text = \"\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	out, err := m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	for _, target := range []string{home(".config", "nvim"), home(".hushlogin")} {
		if _, err := os.Lstat(target); err == nil {
			t.Fatalf("uninstall left %s", target)
		}
	}

	if body, _ := os.ReadFile(source); string(body) != "mine" {
		t.Fatalf("uninstall deleted the source of a link, which the user wrote:\n%s", out)
	}

	if exists(filepath.Join(m.config, "oku.toml")) || !strings.Contains(out, "oku did not write") {
		t.Fatalf("uninstall should delete oku.toml and say what it left:\n%s", out)
	}
}

func TestB166ADataPackageHoldsFilesThatTheListLinks(t *testing.T) {
	m := newMachine(t)

	skills := m.manifest(t, "skills", map[string]string{
		"skills/deslop/SKILL.md": "remove slop",
	}, "data = true")

	m.writeFilesList(t, "[packages]\nskills = \""+skills+"\"\n"+
		"[files]\n\"{{home}}/.skills/deslop\" = { link = \"{{pkg.skills}}/skills/deslop\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(
		home(".skills", "deslop", "SKILL.md"),
	); string(
		body,
	) != "remove slop" {
		t.Fatalf("the link does not reach the file of the data package, got %q", body)
	}

	entries, _ := os.ReadDir(m.profile("bin"))
	if len(entries) != 0 {
		t.Fatalf("a data package should put nothing on PATH, the profile bin holds %v", entries)
	}

	empty := m.manifest(t, "empty", map[string]string{"x": "y"}, "")

	if _, err := m.run(
		t,
		"",
		"add",
		empty,
	); err == nil ||
		!strings.Contains(err.Error(), "data = true") {
		t.Fatalf("an artifact with no output and no data key should name data = true, got %v", err)
	}
}

func TestB126ARelativeNodeRuntimeStartsAtTheConfigDirectory(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	must(t, os.MkdirAll(filepath.Join(m.config, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "packages", "node.toml"), node, 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte("[runtimes]\nnode = \"./packages/node.toml\"\n"), 0o644))

	// The working directory is the fixtures directory, which has no packages/.
	if out, err := m.run(t, "", "add", "npm:@scope/tool"); err != nil {
		t.Fatalf("a relative runtimes.node should start at the config directory: %v\n%s", err, out)
	}
}

func TestB171APrebuiltLibraryServesABuildThatDependsOnIt(t *testing.T) {
	m := newMachine(t)

	// A prebuilt download with a header in a directory and a file under lib.
	m.manifest(t, "libgreet", map[string]string{
		"include/greet/greet.h": "#define GREETING \"hello from the header\"\n",
		"lib/greet.txt":         "from lib\n",
	}, "include = [\"include/greet\"]\nlib = [\"lib/greet.txt\"]")

	app := filepath.Join(m.fixtures, "app.toml")
	must(t, os.WriteFile(app, []byte("[package]\nname = \"app\"\n[version]\nvalue = \"1.0.0\"\n"+
		"[build]\nneeds = [\"sh\"]\ndeps = [\"./libgreet.toml\"]\n"+
		"[[build.step]]\nshell = \"sh\"\n"+
		"run = \"\"\"\nfor dir in $(echo $CPATH | tr ':' ' '); do test -f $dir/greet/greet.h && header=$dir/greet/greet.h; done\n"+
		"for dir in $(echo $LIBRARY_PATH | tr ':' ' '); do test -f $dir/greet.txt && lib=$dir/greet.txt; done\n"+
		"printf '#!/bin/sh\\\\ncat %s %s\\\\n' $header $lib > app\n\"\"\"\n"+
		"[[build.step]]\ninstall = { bin = [\"app\"] }\n"), 0o644))

	m.opts.Interactive = yes()

	out, err := m.run(t, "y\n", "add", app)
	if err != nil {
		t.Fatalf("the build should find the header and the library of its dep: %v\n%s", err, out)
	}

	got, err := exec.Command(m.profile("bin", "app")).Output()
	must(t, err)

	if !strings.Contains(string(got), "hello from the header") ||
		!strings.Contains(string(got), "from lib") {
		t.Fatalf(
			"the built program should hold what CPATH and LIBRARY_PATH led to, it printed %q",
			got,
		)
	}
}

func TestB175AFontEntryMayBeAPattern(t *testing.T) {
	m := newMachine(t)
	_, font := m.exposedPaths()
	fonts := filepath.Dir(font)

	ref := m.manifest(t, "family", map[string]string{
		"fonts/ttf/Family-Bold.ttf":    "bold",
		"fonts/ttf/Family-Regular.ttf": "regular",
		"fonts/otf/Family-Italic.otf":  "italic",
		"fonts/OFL.txt":                "a licence, not a font",
		"doc/tool.1":                   "a man page",
		"tool":                         script,
	}, "bin = [\"tool\"]\nman = [\"doc/*.1\"]\nfont = [\"fonts/ttf/*.ttf\", \"**/Family-Itali?.otf\"]")

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	for _, name := range []string{"Family-Bold.ttf", "Family-Regular.ttf", "Family-Italic.otf"} {
		if !exists(filepath.Join(fonts, name)) {
			t.Fatalf("the pattern should install %s", name)
		}
	}

	if exists(filepath.Join(fonts, "OFL.txt")) {
		t.Fatal("the pattern installed a file it does not match")
	}

	if !exists(m.profile("share", "man", "man1", "tool.1")) {
		t.Fatal("a man pattern should install the page it matches")
	}

	none := m.manifest(t, "nofont", map[string]string{"tool": script},
		"bin = [\"tool\"]\nfont = [\"fonts/*.otf\"]")

	_, err = m.run(t, "", "add", none)
	if err == nil || !strings.Contains(err.Error(), `font "fonts/*.otf" matches no file`) {
		t.Fatalf("a pattern that matches nothing should fail and name the pattern, got %v", err)
	}
}

func TestB183TheLockOfAnNPMPackageWorksUnderAnotherConfigDirectory(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")

	node, err := os.ReadFile(m.fakeNode(t))
	must(t, err)

	must(t, os.MkdirAll(filepath.Join(m.config, "packages"), 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "packages", "node.toml"), node, 0o644))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte("[runtimes]\nnode = \"./packages/node.toml\"\n"), 0o644))

	_, err = m.run(t, "", "add", "npm:@scope/tool")
	must(t, err)

	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if strings.Contains(string(locked), m.config) {
		t.Fatalf("oku.lock names the config directory of this machine:\n%s", locked)
	}

	// Another machine has the same files under another home directory.
	elsewhere := filepath.Join(t.TempDir(), "config")
	must(t, os.MkdirAll(elsewhere, 0o755))
	must(t, os.Rename(m.config, filepath.Join(elsewhere, "oku")))
	must(t, os.RemoveAll(m.data))
	t.Setenv("XDG_CONFIG_HOME", elsewhere)

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync under another config directory: %v\n%s", err, out)
	}

	if !exists(m.profile("bin", "tool")) {
		t.Fatal("sync did not install the npm package")
	}
}

func TestB186ADownloadThatIsTheProgramItselfMayHaveAWrapper(t *testing.T) {
	m := newMachine(t)

	program := filepath.Join(m.fixtures, "tool-prog")
	must(t, os.WriteFile(program, []byte("#!/bin/sh\necho \"$GREETING $1\"\n"), 0o755))

	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\n"+
			"bin = [{ name = \"tool\", run = \"/usr/bin/env\", "+
			"args = [\"GREETING=hello\", \"{{pkg}}/tool-prog\"] }]\n",
		program,
	))

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	out, err := exec.Command(m.profile("bin", "tool"), "you").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "hello you" {
		t.Fatalf("the wrapper did not run the download with the variable: %v\n%s", err, out)
	}
}

func TestB192AnEntryOverridesVarsForItsOwnTemplate(t *testing.T) {
	m := newMachine(t)

	m.writeTemplate(t, "files/tool.tmpl", "size = {{size}}\nfont = {{font}}\n")

	// TOML allows a key once per table, so the second entry for the target
	// comes from an included list.
	other := m.writeTemplate(t, "other.toml", "[files]\n"+
		"\"{{home}}/.tool\" = { render = \"./files/tool.tmpl\", "+
		"when = { os = \"nowhere\" }, vars = { size = \"9\" } }\n")
	m.writeFilesList(t, "include = [\""+filepath.ToSlash(other)+"\"]\n"+
		"[vars]\nfont = \"Mono\"\nsize = \"11\"\n"+
		"[files]\n"+
		"\"{{home}}/.tool\" = { render = \"./files/tool.tmpl\", "+
		"when = { os = \""+runtime.GOOS+"\" }, vars = { size = \"13\" } }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".tool")); string(body) != "size = 13\nfont = Mono\n" {
		t.Fatalf("the target holds %q", body)
	}

	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.tool\" = { link = \"./files/tool.tmpl\", vars = { size = \"13\" } }\n")

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "vars applies to text and render") {
		t.Fatalf("want an error for vars on a link, got %v", err)
	}
}
