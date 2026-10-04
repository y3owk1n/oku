package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB533ASyncKeepsAFileOrSecretTheUserEdited(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows places copies, which TestB257 covers")
	}

	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))

	list := "[vars]\nn = \"1\"\n[files]\n" +
		"\"{{home}}/.signers\" = { text = \"one {{n}}\\n\", mode = \"0644\" }\n" +
		"\"{{home}}/.token\" = { secret = \"./secrets/id.age\" }\n"

	// Generation 2 keeps .signers as it was in generation 1.
	for _, extra := range []string{"", "\"{{home}}/.other\" = { text = \"x\" }\n"} {
		m.writeFilesList(t, list+extra)

		_, err := m.run(t, "", "sync")
		must(t, err)
	}

	// An edit through the link of a writable file.
	f, err := os.OpenFile(home(".signers"), os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	_, err = f.WriteString("mine\n")
	must(t, err)
	must(t, f.Close())

	copies, _ := filepath.Glob(filepath.Join(m.genDir(1), "files", "*-.signers"))
	if len(copies) != 1 {
		t.Fatalf("generation 1 holds %d copies of .signers", len(copies))
	}

	if old, _ := os.ReadFile(copies[0]); strings.Contains(string(old), "mine") {
		t.Fatal("the edit reached generation 1")
	}

	m.writeFilesList(t, "[vars]\nn = \"2\"\n[files]\n"+
		"\"{{home}}/.signers\" = { text = \"one {{n}}\\n\", mode = \"0644\" }\n"+
		"\"{{home}}/.token\" = { secret = \"./secrets/id.age\" }\n")

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), ".signers changed since oku wrote it") {
		t.Fatalf("want a refusal that names the edited file, got %v", err)
	}

	if body, _ := os.ReadFile(home(".signers")); !strings.Contains(string(body), "mine") {
		t.Fatalf("the sync overwrote the edit:\n%s", body)
	}

	// The user moves the edit into the list, and replaces the secret's link.
	must(t, os.Remove(home(".signers")))
	must(t, os.Remove(home(".token")))
	must(t, os.WriteFile(home(".token"), []byte("my own"), 0o600))

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), ".token changed since oku wrote it") ||
		strings.Contains(err.Error(), ".signers") {
		t.Fatalf("want a refusal that names the replaced secret, got %v", err)
	}
}

func TestB533ADeletedSecretLeavesNoDecryptedCopy(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))
	m.writeFilesList(t, "[files]\n\"{{home}}/.token\" = { secret = \"./secrets/id.age\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	must(t, os.Remove(home(".token")))
	m.writeFilesList(t, "[files]\n")

	_, err = m.run(t, "", "sync")
	must(t, err)

	if entries, _ := os.ReadDir(filepath.Join(m.data, "secrets")); len(entries) != 0 {
		t.Fatalf("a decrypted secret stayed after its entry went: %v", entries)
	}
}
