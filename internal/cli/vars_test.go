package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemplate writes a file beside the global list and returns its path.
func (m machine) writeTemplate(t *testing.T, name, body string) string {
	t.Helper()

	path := filepath.Join(m.config, name)
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func TestB143RenderFillsTheVariablesIntoAReadOnlyFile(t *testing.T) {
	m := newMachine(t)

	m.writeTemplate(t, "files/ghostty.tmpl",
		"font-family = {{font}}\nbackground = #{{ theme.base00 }}\nkeep = \\{{.Go}}\n")
	m.writeFilesList(t, "[vars]\nfont = \"JetBrains Mono\"\n"+
		"[vars.theme]\nbase00 = \"0c1410\"\n"+
		"[files]\n\"{{home}}/.config/ghostty/config\" = { render = \"./files/ghostty.tmpl\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	target := home(".config", "ghostty", "config")

	body, err := os.ReadFile(target)
	must(t, err)

	want := "font-family = JetBrains Mono\nbackground = #0c1410\nkeep = {{.Go}}\n"
	if string(body) != want {
		t.Fatalf("the target holds %q, want %q", body, want)
	}

	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("a rendered file should be read-only, its mode is %v", info.Mode().Perm())
	}
}

func TestB144AVariableThatIsNotSetFailsBeforeAnyChange(t *testing.T) {
	m := newMachine(t)

	template := m.writeTemplate(t, "files/tool.tmpl", "one\ntwo\ncolor = {{missing}}\n")
	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.aaa\" = { text = \"first in order\" }\n"+
		"\"{{home}}/.tool\" = { render = \"./files/tool.tmpl\" }\n")

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), template+":3") ||
		!strings.Contains(err.Error(), "missing") {
		t.Fatalf("sync should name the template, line 3 and the variable, got %v", err)
	}

	if exists(home(".aaa")) {
		t.Fatal("sync wrote a file although a template could not be rendered")
	}
}

func TestB146AChangedVariableRendersEveryFileAgainInOneGeneration(t *testing.T) {
	m := newMachine(t)

	list := func(color string) string {
		return "[vars]\ncolor = \"" + color + "\"\n[files]\n" +
			"\"{{home}}/.one\" = { text = \"one {{color}}\" }\n" +
			"\"{{home}}/.two\" = { text = \"two {{color}}\" }\n"
	}

	m.writeFilesList(t, list("green"))
	_, err := m.run(t, "", "sync")
	must(t, err)

	m.writeFilesList(t, list("blue"))
	_, err = m.run(t, "", "sync")
	must(t, err)

	one, _ := os.ReadFile(home(".one"))
	two, _ := os.ReadFile(home(".two"))

	if string(one) != "one blue" || string(two) != "two blue" || m.generation(t) != "gen-2" {
		t.Fatalf("after the change: %q and %q in %s", one, two, m.generation(t))
	}

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if one, _ := os.ReadFile(home(".one")); string(one) != "one green" {
		t.Fatalf("rollback left %q", one)
	}
}

func TestB147TheUsersListOverridesTheVariablesOfAnInclude(t *testing.T) {
	m := newMachine(t)

	m.writeTemplate(t, "first.toml", "[vars]\na = \"first\"\nb = \"first\"\nc = \"first\"\n")
	m.writeTemplate(t, "second.toml", "[vars]\nb = \"second\"\nc = \"second\"\n")
	m.writeFilesList(t, "include = [\"./first.toml\", \"./second.toml\"]\n"+
		"[vars]\nc = \"own\"\n"+
		"[files]\n\"{{home}}/.vars\" = { text = \"{{a}} {{b}} {{c}}\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".vars")); string(body) != "first second own" {
		t.Fatalf("the target holds %q, want %q", body, "first second own")
	}
}

func TestB148RenderKeepsEveryByteOfTheTemplate(t *testing.T) {
	m := newMachine(t)

	m.writeTemplate(t, "files/crlf.tmpl", "a = {{v}}\r\n\tb = {{v}}")
	m.writeFilesList(t, "[vars]\nv = \"1\"\n"+
		"[files]\n\"{{home}}/.crlf\" = { render = \"./files/crlf.tmpl\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".crlf")); string(body) != "a = 1\r\n\tb = 1" {
		t.Fatalf("the target holds %q", body)
	}
}
