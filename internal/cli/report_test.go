package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// managedList writes a list with one text file and one setting.
func managedList(t *testing.T, m machine, text string, tilesize string) {
	t.Helper()
	m.writeFilesList(t, "[files]\n\"{{home}}/.config/tool/config\" = { text = \""+text+"\" }\n"+
		"[defaults.\"com.apple.dock\"]\ntilesize = "+tilesize+"\n")
}

func TestB224EveryChangeToAFileOrASettingIsReported(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["com.apple.dock tilesize"] = "<integer>64</integer>"
	target := home(".config", "tool", "config")

	managedList(t, m, "one", "48")
	out, err := m.run(t, "", "sync")
	must(t, err)

	for _, want := range []string{"wrote " + target, "set com.apple.dock tilesize", "0 packages, 1 file, 1 setting"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the first sync should say %q:\n%s", want, out)
		}
	}

	managedList(t, m, "two", "32")
	out, err = m.run(t, "", "sync")
	must(t, err)

	for _, want := range []string{"changed " + target, "changed com.apple.dock tilesize"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the second sync should say %q:\n%s", want, out)
		}
	}

	m.writeFilesList(t, "")
	out, err = m.run(t, "", "sync")
	must(t, err)

	for _, want := range []string{"removed " + target, "restored com.apple.dock tilesize"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the third sync should say %q:\n%s", want, out)
		}
	}
}

func TestB225GenerationsAndRollbackCountFilesAndSettings(t *testing.T) {
	m, _ := settingsMachine(t)
	target := home(".config", "tool", "config")

	managedList(t, m, "one", "48")
	_, err := m.run(t, "", "sync")
	must(t, err)

	managedList(t, m, "two", "32")
	_, err = m.run(t, "", "sync")
	must(t, err)

	out, err := m.run(t, "", "generations")
	must(t, err)

	for _, want := range []string{
		"1 file, 1 setting", "+ " + target, "+ com.apple.dock tilesize",
		"~ " + target, "~ com.apple.dock tilesize",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generations should say %q:\n%s", want, out)
		}
	}

	out, err = m.run(t, "", "generations", "--json")
	must(t, err)

	var gens []struct {
		Files    []struct{ Target string }      `json:"files"`
		Settings []struct{ Domain, Key string } `json:"settings"`
	}
	must(t, json.Unmarshal([]byte(out), &gens))

	if len(gens) != 2 || gens[1].Files[0].Target != target || gens[1].Settings[0].Key != "tilesize" {
		t.Fatalf("generations --json should list files and settings:\n%s", out)
	}

	out, err = m.run(t, "", "rollback")
	must(t, err)

	if !strings.Contains(out, "1 file, 1 setting: ~ "+target+", ~ com.apple.dock tilesize") {
		t.Fatalf("rollback should say what it changed:\n%s", out)
	}
}

func TestB226ListShowsTheFilesAndTheSettingsOfTheList(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["com.apple.dock tilesize"] = "<integer>64</integer>"

	managedList(t, m, "one", "48")
	_, err := m.run(t, "", "sync")
	must(t, err)

	out, err := m.run(t, "", "list", "--files")
	must(t, err)

	if !strings.Contains(out, "{{home}}/.config/tool/config") || !strings.Contains(out, "text") ||
		!strings.Contains(out, m.config) {
		t.Fatalf("list --files should show the target, the kind and the list:\n%s", out)
	}

	out, err = m.run(t, "", "list", "--settings")
	must(t, err)

	if !strings.Contains(out, "com.apple.dock") || !strings.Contains(out, "tilesize") ||
		!strings.Contains(out, "48") || !strings.Contains(out, "<integer>64</integer>") {
		t.Fatalf("list --settings should show the value and the one before oku:\n%s", out)
	}

	out, err = m.run(t, "", "list", "--settings", "--json")
	must(t, err)

	var rows []struct {
		Key      string `json:"key"`
		HadPrior bool   `json:"had_prior"`
	}
	must(t, json.Unmarshal([]byte(out), &rows))

	if len(rows) != 1 || rows[0].Key != "tilesize" || !rows[0].HadPrior {
		t.Fatalf("list --settings --json: %s", out)
	}
}

func TestB228ATerminalGetsEachPackageRowWithACheckAsItFinishes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	m.writeFilesList(t, "[packages]\ntool = \""+strings.ReplaceAll(ref, `\`, `\\`)+"\"\n")

	out, err := m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "✓\x1b[0m \x1b[1mtool\x1b[0m  1.2.3") {
		t.Fatalf("sync should print a checked row for the package:\n%s", out)
	}

	m.writeFilesList(t, "[packages]\n")

	out, err = m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "-\x1b[0m \x1b[1mtool\x1b[0m  1.2.3  \x1b[2mremoved") {
		t.Fatalf("sync should print a minus row for the removed package:\n%s", out)
	}
}

func TestB229TheApprovalPromptCollapsesToOneLineOnceAnswered(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m := newMachine(t)
	m.opts.Interactive = yes()
	ref := m.buildManifest(t, false, "", writeTool+installTool)

	out, err := m.run(t, "n\n", "add", ref)
	if err == nil || !strings.Contains(out, "\x1b[1A") || !strings.Contains(out, "✗\x1b[0m rejected tool 1.0.0") {
		t.Fatalf("a rejected prompt should be erased and say so: %v\n%q", err, out)
	}

	out, err = m.run(t, "y\n", "add", ref)
	must(t, err)

	if !strings.Contains(out, "\x1b[1A") || !strings.Contains(out, "✓\x1b[0m approved tool 1.0.0") {
		t.Fatalf("an approved prompt should be erased and say so:\n%q", out)
	}
}

func TestB231EveryFinishedItemAndTheClosingLineCarryACheck(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m, store := settingsMachine(t)
	store.values["com.apple.dock tilesize"] = "<integer>64</integer>"
	target := home(".config", "tool", "config")

	managedList(t, m, "one", "48")
	out, err := m.run(t, "", "sync")
	must(t, err)

	for _, want := range []string{
		"✓\x1b[0m wrote " + target, "✓\x1b[0m set com.apple.dock tilesize",
		"✓\x1b[0m done in ", "profile holds 0 packages, 1 file, 1 setting, generation 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sync should print %q:\n%s", want, out)
		}
	}

	m.writeFilesList(t, "")
	out, err = m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "-\x1b[0m removed "+target) ||
		!strings.Contains(out, "✓\x1b[0m restored com.apple.dock tilesize") {
		t.Fatalf("a removal gets a minus and a restore a check:\n%s", out)
	}

	out, err = m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "✓\x1b[0m already in sync") {
		t.Fatalf("a sync with nothing to do closes with a check:\n%s", out)
	}

	// update shares the path of sync, so it closes the same way.
	out, err = m.run(t, "", "update")
	must(t, err)

	if !strings.Contains(out, "✓\x1b[0m already in sync") && !strings.Contains(out, "✓\x1b[0m done in ") {
		t.Fatalf("update should close with a check:\n%s", out)
	}
}

func TestB235ADryRunRowSaysWhatWouldChange(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	m.writeFilesList(t, "[packages]\ntool = \""+strings.ReplaceAll(ref, `\`, `\\`)+"\"\n")

	out, err := m.run(t, "", "sync", "--dry-run")
	must(t, err)

	if !strings.Contains(out, "~\x1b[0m \x1b[1mtool\x1b[0m  1.2.3") || strings.Contains(out, "✓") {
		t.Fatalf("a dry run row should carry a tilde and no check:\n%s", out)
	}
}

func TestB235ACommandNamesItsProjectOnce(t *testing.T) {
	m := newMachine(t)

	project := filepath.Join(m.fixtures, "work", "api")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), nil, 0o644))

	m.opts.WorkDir = project

	out, err := m.run(t, "", "sync")
	must(t, err)

	if n := strings.Count(out, "project "+project); n != 1 {
		t.Fatalf("sync named the project %d times:\n%s", n, out)
	}
}

func TestB234HelpFitsANarrowTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("COLUMNS", "50")

	m := newMachine(t)

	out, err := m.run(t, "", "sync", "--help")
	must(t, err)

	escape := regexp.MustCompile("\x1b\\[[0-9;]*m")

	for _, line := range strings.Split(escape.ReplaceAllString(out, ""), "\n") {
		if len([]rune(line)) > 50 {
			t.Fatalf("a help line is wider than 50 columns: %q\n%s", line, out)
		}
	}
}

func TestB238ATerminalGetsOneNoteForTheManifestsOkuInferred(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m := newMachine(t)
	m.archive(t, "tool-1.2.3", map[string]string{"tool": script})
	m.archive(t, "other-2.0.0", map[string]string{"other": script})

	server := httptest.NewServer(http.FileServer(http.Dir(m.fixtures)))
	t.Cleanup(server.Close)

	m.writeFilesList(t, "[packages]\n"+
		"tool = \""+server.URL+"/tool-1.2.3.tar.gz\"\n"+
		"other = \""+server.URL+"/other-2.0.0.tar.gz\"\n")

	out, err := m.run(t, "", "sync")
	must(t, err)

	// The notes wrap at the width, so the words are compared on one line.
	text := strings.Join(strings.Fields(out), " ")

	if !strings.Contains(text, "2 packages are downloads and no manifests, so oku inferred one from each: other, tool") ||
		strings.Count(text, "inferred") != 1 {
		t.Fatalf("sync should print one note for both inferred manifests:\n%s", out)
	}

	if !regexp.MustCompile(`pinned sha256 [0-9a-f]{12} in`).MatchString(text) {
		t.Fatalf("a terminal should get the start of the pinned checksum:\n%s", out)
	}
}

func TestB236AddTakesSeveralRefsOneGenerationEach(t *testing.T) {
	m := newMachine(t)
	first := m.manifest(t, "first", map[string]string{"first": script}, `bin = ["first"]`)
	second := m.manifest(t, "second", map[string]string{"second": script}, `bin = ["second"]`)

	if _, err := m.run(t, "", "add", "--asset", "*.tar.gz", first, second); err == nil ||
		!strings.Contains(err.Error(), "add that ref on its own") {
		t.Fatalf("--asset with two refs: %v", err)
	}

	out, err := m.run(t, "", "add", first, second)
	must(t, err)

	if !strings.Contains(out, "added first") || !strings.Contains(out, "added second") {
		t.Fatalf("add should add both:\n%s", out)
	}

	if strings.Count(out, "to run") != 1 || !strings.Contains(out, "to run them") {
		t.Fatalf("add should say once how to run both packages' programs:\n%s", out)
	}

	out, err = m.run(t, "", "generations", "--json")
	must(t, err)

	var gens []map[string]any
	must(t, json.Unmarshal([]byte(out), &gens))

	if len(gens) != 2 {
		t.Fatalf("each ref should get its own generation, got %d:\n%s", len(gens), out)
	}
}

func TestB237ACommandToTypeIsInColourWithoutBackticks(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	m := newMachine(t)

	out, err := m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "\x1b[36moku add <ref>\x1b[0m installs one") || strings.Contains(out, "`") {
		t.Fatalf("the hint should show the command in colour:\n%q", out)
	}

	t.Setenv("FORCE_COLOR", "")

	out, err = m.run(t, "", "list")
	must(t, err)

	if !strings.Contains(out, "`oku add <ref>` installs one") {
		t.Fatalf("a pipe should keep the backticks:\n%q", out)
	}
}

func TestB239RollbackSaysWhenSyncWouldRemoveAPackageAgain(t *testing.T) {
	m := newMachine(t)
	first := m.manifest(t, "first", map[string]string{"first": script}, `bin = ["first"]`)

	_, err := m.run(t, "", "add", first)
	must(t, err)

	_, err = m.run(t, "", "remove", "first")
	must(t, err)

	out, err := m.run(t, "", "rollback")
	must(t, err)

	if !strings.Contains(out, "does not list first, so `oku sync` will remove it again") {
		t.Fatalf("rollback should say the next sync removes first:\n%s", out)
	}
}

func TestB240AnEmptyListWritesNoGenerationAndQuickRunsReadInMilliseconds(t *testing.T) {
	m := newMachine(t)
	m.writeFilesList(t, "[packages]\n")

	out, err := m.run(t, "", "sync")
	must(t, err)

	if !strings.Contains(out, "nothing to sync") {
		t.Fatalf("an empty first sync should say there is nothing to do:\n%s", out)
	}

	out, err = m.run(t, "", "generations")
	must(t, err)

	if !strings.Contains(out, "no generations yet") {
		t.Fatalf("an empty sync wrote a generation:\n%s", out)
	}

	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)
	m.writeFilesList(t, "[packages]\ntool = \""+strings.ReplaceAll(ref, `\`, `\\`)+"\"\n")

	out, err = m.run(t, "", "sync")
	must(t, err)

	if strings.Contains(out, ", 0s\n") {
		t.Fatalf("a quick sync should not take 0s:\n%s", out)
	}
}

func TestB241AGenerationShowsWhatChangedFromTheOneItReplaced(t *testing.T) {
	m := newMachine(t)

	for _, name := range []string{"first", "second"} {
		_, err := m.run(t, "", "add", m.manifest(t, name, map[string]string{name: script}, `bin = ["`+name+`"]`))
		must(t, err)
	}

	_, err := m.run(t, "", "rollback")
	must(t, err)

	_, err = m.run(t, "", "add", m.manifest(t, "third", map[string]string{"third": script}, `bin = ["third"]`))
	must(t, err)

	out, err := m.run(t, "", "generations")
	must(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, "from 1, + third 1.2.3") {
		t.Fatalf("generation 3 should compare with generation 1, which it replaced:\n%s", out)
	}

	out, err = m.run(t, "", "generations", "--json")
	must(t, err)

	var gens []struct {
		Number int `json:"number"`
		From   int `json:"from"`
	}
	must(t, json.Unmarshal([]byte(out), &gens))

	if len(gens) != 3 || gens[1].From != 1 || gens[2].From != 1 {
		t.Fatalf("generations --json should name the generation each replaced:\n%s", out)
	}
}
