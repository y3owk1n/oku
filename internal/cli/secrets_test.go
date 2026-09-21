package cli_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// ageKey writes a new age identity where oku looks for one, or at path when it
// is given, and returns the identity.
func (m machine) ageKey(t *testing.T, path string) *age.X25519Identity {
	t.Helper()

	identity, err := age.GenerateX25519Identity()
	must(t, err)

	if path == "" {
		path = filepath.Join(filepath.Dir(m.config), "sops", "age", "keys.txt")
	}

	must(t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(t, os.WriteFile(path, []byte(identity.String()+"\n"), 0o600))

	return identity
}

// encrypt writes plaintext as an age file beside the global list.
func (m machine) encrypt(t *testing.T, name, plaintext string, to *age.X25519Identity) {
	t.Helper()

	var out bytes.Buffer

	w, err := age.Encrypt(&out, to.Recipient())
	must(t, err)

	_, err = w.Write([]byte(plaintext))
	must(t, err)
	must(t, w.Close())

	m.writeTemplate(t, name, out.String())
}

// fakeSops writes a package whose sops prints "sops-value:" and the path it was
// asked to extract, notes the identity file it was given, and fails for a file
// that holds the word FAIL.
func (m machine) fakeSops(t *testing.T) string {
	t.Helper()

	return m.manifest(t, "sops", map[string]string{"sops": "#!/bin/sh\n" +
		"for last; do :; done\n" +
		"echo \"$SOPS_AGE_KEY_FILE\" > \"$HOME/.sops-key-file\"\n" +
		"if grep -q FAIL \"$last\"; then echo 'MAC mismatch' >&2; exit 1; fi\n" +
		"printf 'sops-value:%s' \"$3\"\n"}, "bin = [\"sops\"]")
}

func TestB154ASecretEntryWritesTheDecryptedValue(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "PRIVATE KEY\n", m.ageKey(t, ""))

	m.writeFilesList(
		t,
		"[files]\n\"{{home}}/.ssh/id_ed25519\" = { secret = \"./secrets/id.age\" }\n",
	)

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".ssh", "id_ed25519")); string(body) != "PRIVATE KEY\n" {
		t.Fatalf("the target holds %q", body)
	}
}

func TestB155AnAgeFileNeedsNoProgramAndASopsFileGoesThroughSops(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "from age", m.ageKey(t, ""))
	m.writeTemplate(
		t,
		"secrets/secrets.yaml",
		"ssh:\n    id: ENC[AES256_GCM,data:xx]\nsops:\n    version: 3\n",
	)

	t.Setenv("PATH", "/usr/bin:/bin")
	m.writeFilesList(t, "[packages]\nsops = \""+m.fakeSops(t)+"\"\n[files]\n"+
		"\"{{home}}/.from-age\" = { secret = \"./secrets/id.age\" }\n"+
		"\"{{home}}/.from-sops\" = { secret = \"./secrets/secrets.yaml\", key = \"ssh/id\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".from-age")); string(body) != "from age" {
		t.Fatalf("the age secret is %q", body)
	}

	if body, _ := os.ReadFile(home(".from-sops")); string(body) != `sops-value:["ssh"]["id"]` {
		t.Fatalf("the sops secret is %q, want what sops printed for the key", body)
	}
}

func TestB156ATemplateMayUseANamedSecret(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/token.age", "s3cr3t", m.ageKey(t, ""))
	m.writeTemplate(t, "files/hosts.tmpl", "user: {{user}}\ntoken: {{ secret.token }}\n")

	list := "[vars]\nuser = \"kyle\"\n[secrets]\ntoken = { file = \"./secrets/token.age\" }\n" +
		"[files]\n\"{{home}}/.config/gh/hosts.yml\" = { render = \"./files/hosts.tmpl\" }\n"
	m.writeFilesList(t, list)

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(
		home(".config", "gh", "hosts.yml"),
	); string(
		body,
	) != "user: kyle\ntoken: s3cr3t\n" {
		t.Fatalf("the target holds %q", body)
	}

	m.writeFilesList(t, strings.Replace(list, "token = {", "other = {", 1))

	if _, err := m.run(
		t,
		"",
		"sync",
	); err == nil ||
		!strings.Contains(err.Error(), "token is not in [secrets]") {
		t.Fatalf("a secret that [secrets] lacks should be an error, got %v", err)
	}
}

func TestB157OnlyTheUserCanReadAFileThatHoldsASecret(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))

	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.default\" = { secret = \"./secrets/id.age\" }\n"+
		"\"{{home}}/.readonly\" = { secret = \"./secrets/id.age\", mode = \"0400\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	for target, want := range map[string]fs.FileMode{".default": 0o600, ".readonly": 0o400} {
		info, err := os.Stat(home(target))
		must(t, err)

		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %o, want %o", target, info.Mode().Perm(), want)
		}
	}

	if info, err := os.Stat(
		filepath.Join(m.data, "secrets"),
	); err != nil ||
		info.Mode().Perm() != 0o700 {
		t.Fatalf(
			"the directory of decrypted secrets should have mode 700, got %v",
			info.Mode().Perm(),
		)
	}
}

func TestB158OnlyTheEncryptedFileIsKept(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "VERY-SECRET-BYTES", m.ageKey(t, ""))

	m.writeFilesList(t, "[files]\n\"{{home}}/.key\" = { secret = \"./secrets/id.age\" }\n")

	out, err := m.run(t, "", "sync")
	must(t, err)

	if strings.Contains(out, "VERY-SECRET-BYTES") {
		t.Fatalf("the output holds the secret:\n%s", out)
	}

	var holders []string

	must(t, filepath.WalkDir(m.data, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		if body, _ := os.ReadFile(path); bytes.Contains(body, []byte("VERY-SECRET-BYTES")) {
			holders = append(holders, path)
		}

		return nil
	}))

	if len(holders) != 1 || filepath.Dir(holders[0]) != filepath.Join(m.data, "secrets") {
		t.Fatalf("the decrypted bytes should be in one file under secrets, they are in %v", holders)
	}
}

func TestB159ASecretThatCannotBeDecryptedFailsBeforeAnyChange(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))
	m.ageKey(t, "") // Another identity replaces the one the file was encrypted to.

	m.writeFilesList(t, "[files]\n"+
		"\"{{home}}/.aaa\" = { text = \"first in order\" }\n"+
		"\"{{home}}/.key\" = { secret = \"./secrets/id.age\" }\n")

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "./secrets/id.age") {
		t.Fatalf("sync should fail and name the encrypted file, got %v", err)
	}

	if exists(home(".aaa")) || exists(home(".key")) {
		t.Fatal("sync wrote a file although a secret could not be decrypted")
	}

	m.writeTemplate(t, "secrets/bad.yaml", "FAIL\n")
	m.writeFilesList(t, "[packages]\nsops = \""+m.fakeSops(t)+"\"\n[files]\n"+
		"\"{{home}}/.key\" = { secret = \"./secrets/bad.yaml\", key = \"ssh/id\" }\n")

	_, err = m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "./secrets/bad.yaml, key ssh/id") ||
		!strings.Contains(err.Error(), "MAC mismatch") {
		t.Fatalf("sync should name the sops file, the key and what sops said, got %v", err)
	}
}

func TestB160AChangedSecretIsWrittenAgainAndRollbackBringsBackTheOld(t *testing.T) {
	m := newMachine(t)
	identity := m.ageKey(t, "")

	m.encrypt(t, "secrets/id.age", "old", identity)
	m.writeFilesList(t, "[files]\n\"{{home}}/.key\" = { secret = \"./secrets/id.age\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	m.encrypt(t, "secrets/id.age", "new", identity)

	_, err = m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".key")); string(body) != "new" {
		t.Fatalf("after the encrypted file changed the target holds %q", body)
	}

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if body, _ := os.ReadFile(home(".key")); string(body) != "old" {
		t.Fatalf("after rollback the target holds %q", body)
	}
}

func TestB161ARemovedSecretIsDeletedAndUninstallDeletesAll(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))

	list := "[files]\n\"{{home}}/.one\" = { secret = \"./secrets/id.age\" }\n" +
		"\"{{home}}/.two\" = { secret = \"./secrets/id.age\" }\n"
	m.writeFilesList(t, list)

	_, err := m.run(t, "", "sync")
	must(t, err)

	decrypted := func() int {
		entries, _ := os.ReadDir(filepath.Join(m.data, "secrets"))

		return len(entries)
	}

	m.writeFilesList(t, strings.Split(list, "\"{{home}}/.two\"")[0])

	_, err = m.run(t, "", "sync")
	must(t, err)

	if _, err := os.Lstat(home(".two")); err == nil || decrypted() != 1 {
		t.Fatalf("the removed secret is still there, %d decrypted files", decrypted())
	}

	_, err = m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if _, err := os.Lstat(home(".one")); err == nil || exists(filepath.Join(m.data, "secrets")) {
		t.Fatal("uninstall left a decrypted secret or its target")
	}
}

func TestB162SopsFromTheListComesBeforePath(t *testing.T) {
	m := newMachine(t)
	m.ageKey(t, "")
	m.writeTemplate(t, "secrets/secrets.yaml", "a: ENC[x]\nsops:\n    version: 3\n")

	// A sops on PATH that fails, so only the one from the list can succeed.
	bin := filepath.Join(m.fixtures, "pathbin")
	must(t, os.MkdirAll(bin, 0o755))
	must(t, os.WriteFile(filepath.Join(bin, "sops"), []byte("#!/bin/sh\nexit 9\n"), 0o755))
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	// The same sync installs sops and decrypts with it.
	m.writeFilesList(t, "[packages]\nsops = \""+m.fakeSops(t)+"\"\n[files]\n"+
		"\"{{home}}/.key\" = { secret = \"./secrets/secrets.yaml\", key = \"a\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".key")); string(body) != `sops-value:["a"]` {
		t.Fatalf("the target holds %q, want the value from the list's sops", body)
	}
}

func TestB163TheIdentitiesComeFromTheEnvironmentOrTheConfigDirectory(t *testing.T) {
	m := newMachine(t)

	elsewhere := filepath.Join(m.fixtures, "my-keys.txt")
	m.encrypt(t, "secrets/id.age", "from env", m.ageKey(t, elsewhere))
	m.writeTemplate(t, "secrets/secrets.yaml", "a: ENC[x]\nsops:\n    version: 3\n")
	t.Setenv("SOPS_AGE_KEY_FILE", elsewhere)

	m.writeFilesList(t, "[packages]\nsops = \""+m.fakeSops(t)+"\"\n[files]\n"+
		"\"{{home}}/.age\" = { secret = \"./secrets/id.age\" }\n"+
		"\"{{home}}/.sops\" = { secret = \"./secrets/secrets.yaml\", key = \"a\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if body, _ := os.ReadFile(home(".age")); string(body) != "from env" {
		t.Fatalf(
			"oku did not read the identities from SOPS_AGE_KEY_FILE, the target holds %q",
			body,
		)
	}

	if saw, _ := os.ReadFile(home(".sops-key-file")); strings.TrimSpace(string(saw)) != elsewhere {
		t.Fatalf("sops got the identity file %q, want %q", saw, elsewhere)
	}
}

func TestB164DoctorReportsSecretsThatThisMachineCannotDecrypt(t *testing.T) {
	m := newMachine(t)
	m.encrypt(t, "secrets/id.age", "key", m.ageKey(t, ""))
	m.writeFilesList(t, "[files]\n\"{{home}}/.key\" = { secret = \"./secrets/id.age\" }\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if out, _ := m.run(
		t,
		"",
		"doctor",
	); !strings.Contains(
		out,
		"the age identities for 1 secrets are at",
	) {
		t.Fatalf("doctor should confirm the identities:\n%s", out)
	}

	must(t, os.Remove(filepath.Join(filepath.Dir(m.config), "sops", "age", "keys.txt")))

	if out, _ := m.run(t, "", "doctor"); !strings.Contains(out, "the age identities are not at") {
		t.Fatalf("doctor should report the missing identities:\n%s", out)
	}
}

func TestB165AProjectListWithSecretsIsAnError(t *testing.T) {
	m := newMachine(t)

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"),
		[]byte("[secrets]\ntoken = { file = \"./t.age\" }\n"), 0o644))

	m.opts.WorkDir = project

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "[secrets]") {
		t.Fatalf("a project list with [secrets] should fail, got %v", err)
	}
}
