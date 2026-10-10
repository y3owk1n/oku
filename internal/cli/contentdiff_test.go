package cli_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestB578DryRunDiffShowsHowFilesChangeWithSecretsLeftOut(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/token.age", "s3cr3t", m.ageKey(t, ""))
	m.writeTemplate(t, "files/hosts.tmpl", "user: kyle\ntoken: {{ secret.token }}\n")

	list := "[secrets]\ntoken = { file = \"./secrets/token.age\" }\n[files]\n" +
		"\"{{home}}/notes.txt\" = { text = \"one\\ntwo\\nthree\\nfour\\nfive\\nsix\\n\" }\n" +
		"\"{{home}}/.config/gh/hosts.yml\" = { render = \"./files/hosts.tmpl\" }\n"
	m.writeFilesList(t, list)

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	m.writeTemplate(t, "files/hosts.tmpl", "user: kyle\nhost: github.com\ntoken: {{ secret.token }}\n")
	m.writeFilesList(t, strings.Replace(list, "four", "FOUR", 1)+
		"\"{{home}}/new.txt\" = { text = \"hello\\n\" }\n")

	out, err := m.run(t, "", "sync", "--dry-run", "--diff")
	if err != nil {
		t.Fatalf("sync --dry-run --diff: %v\n%s", err, out)
	}

	// Two lines of context around a change, and "..." for the lines between.
	for _, want := range []string{
		"     two\n     three\n    -four\n    +FOUR\n     five\n     six\n",
		"    +hello\n",
		"     user: kyle\n    +host: github.com\n     token: {{secret.token}}\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the dry run should print %q:\n%s", want, out)
		}
	}

	// A file with secrets that is written again reads as one change.
	if !strings.Contains(out, "would change the secret ") || strings.Contains(out, "would remove the secret") {
		t.Fatalf("the file with secrets should read as one change:\n%s", out)
	}

	if strings.Contains(out, "s3cr3t") || strings.Contains(out, " one\n") {
		t.Fatalf("the diff showed a secret or a line far from the change:\n%s", out)
	}

	out, err = m.run(t, "", "sync", "--dry-run", "--diff", "--json")
	if err != nil {
		t.Fatalf("sync --dry-run --diff --json: %v\n%s", err, out)
	}

	var rows []struct {
		Target string
		Diff   []string
	}
	must(t, json.Unmarshal([]byte(out), &rows))

	diffs := map[string][]string{}
	for _, r := range rows {
		if r.Diff != nil {
			diffs[r.Target[strings.LastIndex(r.Target, string(os.PathSeparator))+1:]] = r.Diff
		}
	}

	if got := strings.Join(diffs["new.txt"], "|"); got != "+hello" || len(diffs) != 3 {
		t.Fatalf("--json should give each changed file its diff, got %v", diffs)
	}

	// Without --diff a dry run prints the would lines alone.
	if out, err := m.run(t, "", "sync", "--dry-run"); err != nil || strings.Contains(out, "+FOUR") {
		t.Fatalf("sync --dry-run printed a diff: %v\n%s", err, out)
	}

	if _, err := m.run(t, "", "sync", "--diff"); err == nil || !strings.Contains(err.Error(), "--diff goes with --dry-run") {
		t.Fatalf("--diff without --dry-run should be refused, got %v", err)
	}
}
