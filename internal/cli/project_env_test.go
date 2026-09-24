package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// envProject makes an allowed project whose oku.toml ends with env, and returns
// its directory.
func (m *machine) envProject(t *testing.T, env string) string {
	t.Helper()

	project := m.hookProject(t)
	list := filepath.Join(project, "oku.toml")

	body, err := os.ReadFile(list)
	must(t, err)
	must(t, os.WriteFile(list, append(body, "\n"+env+"\n"...), 0o644))

	_, err = m.run(t, "", "allow")
	must(t, err)

	return project
}

func TestB308ProjectEnvAppliesAndLeavingRestoresTheOldValues(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("STAGE", "mine")
	t.Setenv("PAGER", "less")
	t.Setenv("REGION", "")
	must(t, os.Unsetenv("REGION"))

	project := m.envProject(t, `[env]
STAGE = "dev"
REGION = "${AWS_REGION:-eu-west-1}"
URL = "https://${REGION}/${STAGE}"
PAGER = false`)

	m.apply(t)

	for name, want := range map[string]string{
		"STAGE": "dev", "REGION": "eu-west-1", "URL": "https://eu-west-1/dev",
	} {
		if got := os.Getenv(name); got != want {
			t.Fatalf("%s inside the project is %q, want %q", name, got, want)
		}
	}

	if _, set := os.LookupEnv("PAGER"); set {
		t.Fatal("PAGER = false left PAGER set")
	}

	if out := m.apply(t); strings.TrimSpace(out) != "" {
		t.Fatalf("a second prompt in the same directory changed something:\n%s", out)
	}

	m.opts.WorkDir = filepath.Dir(project)
	m.apply(t)

	if os.Getenv("STAGE") != "mine" || os.Getenv("PAGER") != "less" {
		t.Fatalf("leaving left STAGE=%q PAGER=%q, want mine and less", os.Getenv("STAGE"), os.Getenv("PAGER"))
	}

	for _, name := range []string{"REGION", "URL"} {
		if _, set := os.LookupEnv(name); set {
			t.Fatalf("%s survived leaving the project", name)
		}
	}
}

func TestB309PrependPutsEntriesFromTheListDirectoryFirst(t *testing.T) {
	m := newMachine(t)

	project := m.envProject(t, `[env]
PATH = { prepend = ["scripts", "/opt/tools"] }
MANPATH = { prepend = ["man"] }`)

	// The user lists /opt/tools too, and keeps it after leaving.
	t.Setenv("PATH", "/usr/bin:/opt/tools")
	t.Setenv("MANPATH", "")
	must(t, os.Unsetenv("MANPATH"))

	m.apply(t)

	bin := filepath.Dir(m.projectBin(t, "ptool"))

	want := filepath.Join(project, "scripts") + ":/opt/tools:" + bin + ":/usr/bin:/opt/tools"
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("PATH inside the project is\n%s, want\n%s", got, want)
	}

	if got := os.Getenv("MANPATH"); got != filepath.Join(project, "man") {
		t.Fatalf("MANPATH inside the project is %q", got)
	}

	m.opts.WorkDir = filepath.Dir(project)
	m.apply(t)

	if got := os.Getenv("PATH"); got != "/usr/bin:/opt/tools" {
		t.Fatalf("PATH after leaving is %s", got)
	}

	if got, set := os.LookupEnv("MANPATH"); set && got != "" {
		t.Fatalf("MANPATH after leaving is %q", got)
	}
}

func TestB310RequiredVariableHintsOnceAndStopsExec(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("DEPLOY_TOKEN", "")

	m.envProject(t, `[env]
DEPLOY_TOKEN = { required = "ask ops for one" }`)

	out := m.apply(t)
	if !strings.Contains(out, "DEPLOY_TOKEN is not set, ask ops for one") {
		t.Fatalf("no hint for the required variable:\n%s", out)
	}

	if !strings.Contains(os.Getenv("PATH"), "project-") {
		t.Fatal("a missing variable kept the project from applying")
	}

	if out := m.apply(t); strings.Contains(out, "DEPLOY_TOKEN") {
		t.Fatalf("the hint is repeated before every prompt:\n%s", out)
	}

	_, err := m.run(t, "", "exec", "ptool")
	if err == nil || !strings.Contains(err.Error(), "DEPLOY_TOKEN is not set, ask ops for one") {
		t.Fatalf("want exec refused without DEPLOY_TOKEN, got %v", err)
	}

	t.Setenv("DEPLOY_TOKEN", "t0ken")

	if out := m.apply(t); strings.Contains(out, "DEPLOY_TOKEN") {
		t.Fatalf("a set variable still hints:\n%s", out)
	}
}

func TestB311GlobalEnvAppliesEverywhereAndTheProjectWins(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "oku.toml"), []byte(`[env]
EDITOR = "vi"
PTOOL_HOME = "global"
GREETING = "hello"
`), 0o644))

	outside := m.fixtures
	project := m.envProject(t, `[env]
GREETING = "${GREETING} from the project"`)

	m.opts.WorkDir = outside
	m.apply(t)

	if os.Getenv("EDITOR") != "vi" || os.Getenv("GREETING") != "hello" {
		t.Fatalf("outside a project EDITOR=%q GREETING=%q", os.Getenv("EDITOR"), os.Getenv("GREETING"))
	}

	m.opts.WorkDir = project
	m.apply(t)

	// The project's package sets PTOOL_HOME over the global list.
	if !strings.HasSuffix(os.Getenv("PTOOL_HOME"), "/share/ptool") {
		t.Fatalf("PTOOL_HOME inside the project is %q", os.Getenv("PTOOL_HOME"))
	}

	if got := os.Getenv("GREETING"); got != "hello from the project" {
		t.Fatalf("GREETING inside the project is %q", got)
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", `echo "$EDITOR,$GREETING"`)); got != "vi,hello from the project" {
		t.Fatalf("exec saw %q", got)
	}

	m.opts.WorkDir = outside
	m.apply(t)

	if os.Getenv("GREETING") != "hello" || os.Getenv("PTOOL_HOME") != "global" {
		t.Fatalf("leaving gave GREETING=%q PTOOL_HOME=%q", os.Getenv("GREETING"), os.Getenv("PTOOL_HOME"))
	}
}

func TestB312ListEnvThatControlsTheShellIsRejected(t *testing.T) {
	m := newMachine(t)

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))

	m.opts.WorkDir = project

	for _, env := range []string{`PATH = "/evil"`, `LD_PRELOAD = "x.so"`, `OKU_HOOK_SAVED = "{}"`, `BASH_ENV = "x"`} {
		must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("[env]\n"+env+"\n"), 0o644))

		if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "controls the shell") {
			t.Fatalf("%s: want the list rejected, got %v", env, err)
		}
	}

	shared := filepath.Join(m.fixtures, "shared.toml")
	must(t, os.WriteFile(shared, []byte("[env]\nSTAGE = \"dev\"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("include = [\""+shared+"\"]\n"), 0o644))

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "has [env]") {
		t.Fatalf("want an included list with [env] refused, got %v", err)
	}
}

func TestB313EnvPrintsTheVariablesAsJSONOrDotenv(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("PAGER", "less")

	m.envProject(t, `[env]
QUOTED = "it's \"here\""
PLAIN = "a b"
PAGER = false`)

	var got map[string]*string
	must(t, json.Unmarshal([]byte(m.stdout(t, "env", "--json")), &got))

	if got["PAGER"] != nil || got["PLAIN"] == nil || *got["PLAIN"] != "a b" || *got["QUOTED"] != `it's "here"` {
		t.Fatalf("env --json gave %v", got)
	}

	if !strings.Contains(*got["PATH"], "project-") {
		t.Fatalf("env --json PATH is %s", *got["PATH"])
	}

	dotenv := m.stdout(t, "env", "--dotenv")
	for _, line := range []string{`PLAIN='a b'`, `QUOTED="it's \"here\""`} {
		if !strings.Contains(dotenv, line+"\n") {
			t.Fatalf("env --dotenv has no line %s:\n%s", line, dotenv)
		}
	}

	if strings.Contains(dotenv, "PAGER") {
		t.Fatalf("env --dotenv names an unset variable:\n%s", dotenv)
	}
}

func TestB314ShellFromAnOlderHookMovesToTheNewState(t *testing.T) {
	m := newMachine(t)

	project := m.hookProject(t)

	_, err := m.run(t, "", "allow")
	must(t, err)

	bin := filepath.Dir(m.projectBin(t, "ptool"))

	// An older oku put the project bin on PATH and kept these two.
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("OKU_HOOK_PATH", bin)
	t.Setenv("OKU_HOOK_KEYS", "PTOOL_HOME")
	t.Setenv("PTOOL_HOME", "old")

	m.opts.WorkDir = filepath.Dir(project)
	m.apply(t)

	if got := os.Getenv("PATH"); got != "/usr/bin:/bin" {
		t.Fatalf("PATH after leaving is %s", got)
	}

	for _, name := range []string{"PTOOL_HOME", "OKU_HOOK_PATH", "OKU_HOOK_KEYS"} {
		if _, set := os.LookupEnv(name); set {
			t.Fatalf("%s survived the move to the new state", name)
		}
	}
}

func TestB315ProjectWithOnlyEnvAppliesWithoutALock(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("[env]\nSTAGE = \"dev\"\n"), 0o644))

	m.opts.WorkDir = project

	_, err := m.run(t, "", "allow")
	must(t, err)

	if out := m.apply(t); os.Getenv("STAGE") != "dev" {
		t.Fatalf("a project without packages did not apply its [env]:\n%s", out)
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo $STAGE")); got != "dev" {
		t.Fatalf("exec saw STAGE=%q", got)
	}
}

// envFileProject makes an allowed project without packages whose oku.toml is
// list and whose other files are files, and returns its directory.
func (m *machine) envFileProject(t *testing.T, list string, files map[string]string) string {
	t.Helper()

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte(list), 0o644))

	for name, body := range files {
		must(t, os.WriteFile(filepath.Join(project, name), []byte(body), 0o644))
	}

	m.opts.WorkDir = project

	_, err := m.run(t, "", "allow")
	must(t, err)

	return project
}

// dirEnv returns what oku env --json gives for the current directory.
func (m machine) dirEnv(t *testing.T) map[string]*string {
	t.Helper()

	var got map[string]*string
	must(t, json.Unmarshal([]byte(m.stdout(t, "env", "--json")), &got))

	return got
}

func TestB316EnvFilesLoadInOrderUnderTheInlineValues(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("USER", "kyle")

	m.envFileProject(t, `[[env.file]]
path = ".env"

[[env.file]]
path = ".env.local"

[env]
STAGE = "inline"
URL = "https://${HOST}/${STAGE}"
`, map[string]string{
		".env": `# shared settings
export HOST=example.com
STAGE=file
PLAIN=a b # a comment
SINGLE='keeps $HOST and \n'
DOUBLE="line one
line \"two\"\tend, ${HOST} $USER \$HOST"
DEFAULT=${MISSING:-fallback}
LAYER=shared
`,
		".env.local": "LAYER=local\n",
	})

	got := m.dirEnv(t)

	for name, want := range map[string]string{
		"HOST":    "example.com",
		"STAGE":   "inline",
		"URL":     "https://example.com/inline",
		"PLAIN":   "a b",
		"SINGLE":  `keeps $HOST and \n`,
		"DOUBLE":  "line one\nline \"two\"\tend, example.com kyle $HOST",
		"DEFAULT": "fallback",
		"LAYER":   "local",
	} {
		if got[name] == nil || *got[name] != want {
			t.Errorf("%s is %v, want %q", name, got[name], want)
		}
	}
}

func TestB317MissingEnvFileHintsUnlessOptionalAndUnlessSkipsIt(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("CLAUDECODE", "")

	project := m.envFileProject(t, `[[env.file]]
path = ".env"

[[env.file]]
path = ".env.deploy"
optional = true
unless = ["CLAUDECODE"]
`, nil)

	out := m.apply(t)
	if !strings.Contains(out, filepath.Join(project, ".env")+" does not exist") {
		t.Fatalf("no hint for the missing .env:\n%s", out)
	}

	if strings.Contains(out, ".env.deploy") {
		t.Fatalf("a missing optional file hints:\n%s", out)
	}

	if _, err := m.run(t, "", "exec", "true"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want exec refused without .env, got %v", err)
	}

	must(t, os.WriteFile(filepath.Join(project, ".env"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(project, ".env.deploy"), []byte("DEPLOY_TOKEN=t0ken\n"), 0o644))

	if got := m.dirEnv(t)["DEPLOY_TOKEN"]; got == nil || *got != "t0ken" {
		t.Fatalf("DEPLOY_TOKEN is %v, want the value of .env.deploy", got)
	}

	// An agent starts in a shell where the hook already set the token.
	m.apply(t)
	t.Setenv("CLAUDECODE", "1")

	if got := m.dirEnv(t)["DEPLOY_TOKEN"]; got != nil {
		t.Fatalf("CLAUDECODE=1 still loaded .env.deploy, DEPLOY_TOKEN=%s", *got)
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo ${DEPLOY_TOKEN-unset}")); got != "unset" {
		t.Fatalf("exec under CLAUDECODE=1 passed on DEPLOY_TOKEN=%s", got)
	}
}

func TestB318AllowCoversTheEnvFilesThatGitTracks(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte(`[[env.file]]
path = ".env"

[[env.file]]
path = ".env.deploy"
optional = true
`), 0o644))
	must(t, os.WriteFile(filepath.Join(project, ".env"), []byte("STAGE=dev\n"), 0o644))

	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", project}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git("init", "-q")
	git("add", "oku.toml", ".env")

	m.opts.WorkDir = project

	out, err := m.run(t, "", "allow")
	must(t, err)

	if !strings.Contains(out, "git tracks "+filepath.Join(project, ".env")) ||
		!strings.Contains(out, "git does not track "+filepath.Join(project, ".env.deploy")) {
		t.Fatalf("allow does not say which files it covers:\n%s", out)
	}

	// The user's own file changes freely, and the next prompt reads it.
	must(t, os.WriteFile(filepath.Join(project, ".env.deploy"), []byte("TOKEN=one\n"), 0o644))
	m.apply(t)

	if os.Getenv("TOKEN") != "one" || os.Getenv("STAGE") != "dev" {
		t.Fatalf("after writing .env.deploy TOKEN=%q STAGE=%q", os.Getenv("TOKEN"), os.Getenv("STAGE"))
	}

	// A pull that changes a tracked file stops the project until a new allow.
	must(t, os.WriteFile(filepath.Join(project, ".env"), []byte("STAGE=evil\n"), 0o644))

	if out := m.apply(t); !strings.Contains(out, "oku allow") || os.Getenv("STAGE") == "evil" {
		t.Fatalf("a changed tracked file still applied, STAGE=%q:\n%s", os.Getenv("STAGE"), out)
	}

	_, err = m.run(t, "", "allow")
	must(t, err)
	m.apply(t)

	if os.Getenv("STAGE") != "evil" {
		t.Fatalf("a new allow did not apply the tracked file, STAGE=%q", os.Getenv("STAGE"))
	}

	// A file that git starts to track needs a new allow too.
	must(t, os.WriteFile(filepath.Join(project, ".env.deploy"), []byte("TOKEN=two\n"), 0o644))
	git("add", ".env.deploy")

	if out := m.apply(t); !strings.Contains(out, "oku allow") {
		t.Fatalf("a file that git started to track still applied:\n%s", out)
	}
}

func TestB319EnvFileThatSetsAShellVariableIsRefused(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	m.envFileProject(t, "[[env.file]]\npath = \".env\"\n", map[string]string{
		".env": "STAGE=dev\nLD_PRELOAD=/tmp/x.so\n",
	})

	out := m.apply(t)
	if !strings.Contains(out, "LD_PRELOAD") || os.Getenv("LD_PRELOAD") != "" || os.Getenv("STAGE") != "" {
		t.Fatalf("a file with LD_PRELOAD applied, STAGE=%q:\n%s", os.Getenv("STAGE"), out)
	}
}

func TestB320SecretEnvFilesAreDecryptedWithAgeOrSops(t *testing.T) {
	m := newMachine(t)
	identity := m.ageKey(t, "")

	// A sops on PATH that prints a .env file, or fails for a file with FAIL.
	bin := filepath.Join(m.fixtures, "bin")
	must(t, os.MkdirAll(bin, 0o755))
	must(t, os.WriteFile(filepath.Join(bin, "sops"), []byte("#!/bin/sh\n"+
		"for last; do :; done\n"+
		"if grep -q FAIL \"$last\"; then echo 'MAC mismatch' >&2; exit 1; fi\n"+
		"printf 'FROM_SOPS=\"sops value\"\\n'\n"), 0o755))
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	var sealed bytes.Buffer

	w, err := age.Encrypt(&sealed, identity.Recipient())
	must(t, err)
	_, err = w.Write([]byte("FROM_AGE=age value\n"))
	must(t, err)
	must(t, w.Close())

	project := m.envFileProject(t, `[[env.file]]
path = "secrets.env.age"
secret = true

[[env.file]]
path = "secrets.sops.env"
secret = true
`, map[string]string{
		"secrets.env.age":  sealed.String(),
		"secrets.sops.env": "FROM_SOPS=ENC[AES256_GCM,data:xx]\nsops_version=3\n",
	})

	m.apply(t)

	if os.Getenv("FROM_AGE") != "age value" || os.Getenv("FROM_SOPS") != "sops value" {
		t.Fatalf("FROM_AGE=%q FROM_SOPS=%q", os.Getenv("FROM_AGE"), os.Getenv("FROM_SOPS"))
	}

	must(t, os.WriteFile(filepath.Join(project, "secrets.sops.env"), []byte("FAIL\n"), 0o644))

	if out := m.apply(t); !strings.Contains(out, "MAC mismatch") {
		t.Fatalf("no hint for a file that does not decrypt:\n%s", out)
	}

	if _, err := m.run(t, "", "exec", "true"); err == nil || !strings.Contains(err.Error(), "MAC mismatch") {
		t.Fatalf("want exec refused while a secret does not decrypt, got %v", err)
	}
}

func TestB321ExecScopeFilesLoadOnlyForOkuExec(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	m.envFileProject(t, `[[env.file]]
path = ".env.deploy"
scope = "exec"
`, map[string]string{".env.deploy": "DEPLOY_TOKEN=t0ken\n"})

	m.apply(t)

	if _, set := os.LookupEnv("DEPLOY_TOKEN"); set {
		t.Fatal("the hook set a variable of a file with scope = \"exec\"")
	}

	if got := m.dirEnv(t)["DEPLOY_TOKEN"]; got != nil {
		t.Fatalf("oku env --json printed DEPLOY_TOKEN=%s", *got)
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo $DEPLOY_TOKEN")); got != "t0ken" {
		t.Fatalf("exec saw DEPLOY_TOKEN=%q", got)
	}
}

func TestB322OkuEnvAndOkuLocalSetVariablesOverTheList(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("OKU_ENV", "")

	project := m.envFileProject(t, `[env]
STAGE = "dev"
API = "https://dev.example.com"
TOKEN = { required = "set it in oku.local.toml" }
`, map[string]string{
		"oku.staging.toml": "[env]\nSTAGE = \"staging\"\nAPI = \"https://staging.example.com\"\n",
		"oku.local.toml":   "[env]\nAPI = \"http://localhost:8080\"\nTOKEN = \"mine\"\n",
	})

	got := m.dirEnv(t)
	if *got["STAGE"] != "dev" || *got["API"] != "http://localhost:8080" || *got["TOKEN"] != "mine" {
		t.Fatalf("without OKU_ENV STAGE=%s API=%s TOKEN=%s", *got["STAGE"], *got["API"], *got["TOKEN"])
	}

	t.Setenv("OKU_ENV", "staging")

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo $STAGE $API")); got != "staging http://localhost:8080" {
		t.Fatalf("exec with OKU_ENV=staging saw %q", got)
	}

	for _, name := range []string{"prod", "local"} {
		t.Setenv("OKU_ENV", name)

		if _, err := m.run(t, "", "exec", "true"); err == nil || !strings.Contains(err.Error(), "OKU_ENV="+name) {
			t.Fatalf("want exec refused with OKU_ENV=%s, got %v", name, err)
		}
	}

	t.Setenv("OKU_ENV", "")
	must(t, os.WriteFile(filepath.Join(project, "oku.local.toml"), []byte("[packages]\nrg = \"github:BurntSushi/ripgrep\"\n"), 0o644))

	if out := m.apply(t); !strings.Contains(out, "holds packages") {
		t.Fatalf("an oku.local.toml with [packages] was not refused:\n%s", out)
	}
}

func TestB323AllowCoversTheOverlaysThatGitTracks(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("OKU_ENV", "prod")

	project := filepath.Join(m.fixtures, "proj")
	must(t, os.MkdirAll(project, 0o755))
	must(t, os.WriteFile(filepath.Join(project, "oku.toml"), []byte("[env]\nSTAGE = \"dev\"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(project, "oku.prod.toml"), []byte("[env]\nSTAGE = \"prod\"\n"), 0o644))

	cmd := exec.Command("git", "-C", project, "init", "-q")
	must(t, cmd.Run())
	must(t, exec.Command("git", "-C", project, "add", "oku.toml", "oku.prod.toml").Run())

	m.opts.WorkDir = project

	_, err := m.run(t, "", "allow")
	must(t, err)
	m.apply(t)

	if os.Getenv("STAGE") != "prod" {
		t.Fatalf("STAGE=%q, want prod", os.Getenv("STAGE"))
	}

	// The user's own overlay, and a file it loads, change without a new allow.
	must(t, os.WriteFile(filepath.Join(project, ".env.mine"), []byte("MINE=1\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(project, "oku.local.toml"), []byte("[[env.file]]\npath = \".env.mine\"\n"), 0o644))
	m.apply(t)

	if os.Getenv("MINE") != "1" {
		t.Fatalf("an untracked oku.local.toml did not apply, MINE=%q", os.Getenv("MINE"))
	}

	// A pull that changes a tracked overlay stops the project, whatever OKU_ENV says.
	t.Setenv("OKU_ENV", "")
	must(t, os.WriteFile(filepath.Join(project, "oku.prod.toml"), []byte("[env]\nSTAGE = \"evil\"\n"), 0o644))

	if out := m.apply(t); !strings.Contains(out, "oku allow") {
		t.Fatalf("a changed tracked overlay still applied:\n%s", out)
	}
}

func TestB324OkuProjectNamesTheProjectThatApplies(t *testing.T) {
	m := newMachine(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	project := m.hookProject(t)

	// A project that is not allowed does not apply.
	m.apply(t)

	if _, set := os.LookupEnv("OKU_PROJECT"); set {
		t.Fatal("OKU_PROJECT is set for a project that is not allowed")
	}

	_, err := m.run(t, "", "allow")
	must(t, err)
	m.apply(t)

	if got := os.Getenv("OKU_PROJECT"); got != project {
		t.Fatalf("OKU_PROJECT inside the project is %q, want %s", got, project)
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo $OKU_PROJECT")); got != project {
		t.Fatalf("exec saw OKU_PROJECT=%q", got)
	}

	m.opts.WorkDir = filepath.Dir(project)
	m.apply(t)

	if _, set := os.LookupEnv("OKU_PROJECT"); set {
		t.Fatal("OKU_PROJECT survived leaving the project")
	}

	if got := strings.TrimSpace(m.stdout(t, "exec", "sh", "-c", "echo ${OKU_PROJECT-unset}")); got != "unset" {
		t.Fatalf("exec outside a project saw OKU_PROJECT=%q", got)
	}
}
