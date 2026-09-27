package cli_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/cli"
)

// appPackage returns the files and the artifact TOML of a package whose apps of
// this OS run bodies, keyed by the app's name. An app of plain gets a macOS
// bundle whose Info.plist says nothing, so that oku has to find its program.
func appPackage(bodies map[string]string, plain string) (map[string]string, string) {
	files := map[string]string{}

	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}

	slices.Sort(names)

	var bins, apps []string

	for _, name := range names {
		if runtime.GOOS == "darwin" {
			bundle := name + ".app"
			plist := "<plist/>"

			if name != plain {
				plist = fmt.Sprintf(
					"<plist><dict><key>CFBundleExecutable</key><string>%s</string></dict></plist>",
					name,
				)
			}

			files[bundle+"/Contents/MacOS/"+name] = bodies[name]
			files[bundle+"/Contents/Info.plist"] = plist
			bins = append(bins, bundle+"/Contents/MacOS/"+name)
			apps = append(apps, bundle)

			continue
		}

		entry := "share/applications/" + name + ".desktop"
		files["bin/"+name] = bodies[name]
		files[entry] = "[Desktop Entry]\nType=Application\nName=" + name + "\nExec=" + name + "\n"
		bins = append(bins, "bin/"+name)
		apps = append(apps, entry)
	}

	return files, "bin = " + tomlList(bins) + "\napp = " + tomlList(apps) + "\n"
}

// tomlList writes values as a TOML array of strings.
func tomlList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

// appManifest writes a package whose apps of this OS run bodies, and whose
// [env] sets APP_HOME.
func (m machine) appManifest(
	t *testing.T,
	name string,
	bodies map[string]string,
	plain string,
) string {
	t.Helper()

	files, artifact := appPackage(bodies, plain)
	path := m.manifest(t, name, files, artifact)

	body, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, append(
		body,
		[]byte("\n[env]\nAPP_HOME = \"{{prefix}}/share/"+name+"\"\n")...,
	), 0o644))

	return path
}

func TestB388RunStartsTheAppAndChangesNothing(t *testing.T) {
	m := newMachine(t)
	kept := m.manifest(t, "kept", map[string]string{"kept": script}, `bin = ["kept"]`)

	_, err := m.run(t, "", "add", kept)
	must(t, err)

	before := m.snapshot(t)
	ref := m.desktopManifest(t)
	app, font := m.exposedPaths()

	out, err := m.run(t, "", "run", ref)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	if !strings.Contains(out, "hello from tool") {
		t.Fatalf("run did not start the app:\n%s", out)
	}

	if exists(app) || exists(font) {
		t.Fatalf("run placed %s or %s", app, font)
	}

	if exists(m.profile("bin", "foo")) {
		t.Fatal("run linked the package into the profile")
	}

	if after := m.snapshot(t); after != before {
		t.Fatalf("run changed the list, the lock or the profile:\n%s\nwas\n%s", after, before)
	}

	exiting := m.appManifest(
		t, "exiting", map[string]string{"exiting": "#!/bin/sh\nexit 7\n"}, "",
	)

	_, err = m.run(t, "", "run", exiting)

	var exit cli.ExitError
	if !errors.As(err, &exit) || exit.Code != 7 {
		t.Fatalf("want the app's exit code 7, got %v", err)
	}
}

func TestB390RunTakesTheOneProgramOfAPackageWithNoApp(t *testing.T) {
	m := newMachine(t)
	one := m.manifest(t, "one", map[string]string{"one": script}, `bin = ["one"]`)

	out, err := m.run(t, "", "run", one)
	if err != nil || !strings.Contains(out, "hello from tool") {
		t.Fatalf("run did not start the package's one program: %v\n%s", err, out)
	}

	two := m.manifest(
		t,
		"two",
		map[string]string{"first": script, "second": script},
		`bin = ["first", "second"]`,
	)

	out, err = m.run(t, "", "run", two)
	if err == nil {
		t.Fatalf("run started something from a package with two programs:\n%s", out)
	}

	for _, want := range []string{"first", "second", "oku shell " + two + " -- <program>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %s: %v", want, err)
		}
	}

	none := m.manifest(t, "none", map[string]string{"share/none/data": "text"}, `data = true`)

	_, err = m.run(t, "", "run", none)
	if err == nil || !strings.Contains(err.Error(), "no app and no program") {
		t.Fatalf("want an error for a package that runs nothing, got %v", err)
	}
}

// The apps are two, and on macOS one bundle's Info.plist names its program
// while the other's does not.
func TestB391RunNamesSeveralAppsAndAppPicksOne(t *testing.T) {
	m := newMachine(t)
	ref := m.appManifest(t, "suite", map[string]string{
		"alpha": "#!/bin/sh\necho started alpha\n",
		"beta":  "#!/bin/sh\necho started beta\n",
	}, "beta")

	out, err := m.run(t, "", "run", ref)
	if err == nil {
		t.Fatalf("run started something from a package with two apps:\n%s", out)
	}

	for _, want := range []string{"alpha", "beta", "--app"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %s: %v", want, err)
		}
	}

	// The name ignores case, and the suffix a macOS bundle carries.
	out, err = m.run(t, "", "run", ref, "--app", "ALPHA")
	if err != nil || !strings.Contains(out, "started alpha") {
		t.Fatalf("--app ALPHA did not start alpha: %v\n%s", err, out)
	}

	out, err = m.run(t, "", "run", ref, "--app", "beta.app")
	if err != nil || !strings.Contains(out, "started beta") {
		t.Fatalf("--app beta.app did not start beta: %v\n%s", err, out)
	}

	_, err = m.run(t, "", "run", ref, "--app", "gamma")
	if err == nil || !strings.Contains(err.Error(), "no app gamma") {
		t.Fatalf("want an error naming the app that is missing, got %v", err)
	}
}

func TestB392RunPassesTheArgumentsAndTheEnvironment(t *testing.T) {
	m := newMachine(t)
	ref := m.appManifest(t, "gui", map[string]string{
		"gui": "#!/bin/sh\necho \"args=$*\"\necho \"home=$APP_HOME\"\ncommand -v gui\n",
	}, "")

	out, err := m.run(t, "", "run", ref, "--", "one", "two")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	if !strings.Contains(out, "args=one two") {
		t.Fatalf("the app did not get the arguments:\n%s", out)
	}

	if !strings.Contains(out, "home="+filepath.Join(m.data, "store")) {
		t.Fatalf("the app did not get the package's [env]:\n%s", out)
	}

	if !strings.Contains(out, filepath.Join(m.data, "store")) {
		t.Fatalf("the app did not find the package's programs on PATH:\n%s", out)
	}
}

func TestB394RunTakesOneRef(t *testing.T) {
	m := newMachine(t)
	one := m.manifest(t, "one", map[string]string{"one": script}, `bin = ["one"]`)
	two := m.manifest(t, "two", map[string]string{"two": script}, `bin = ["two"]`)

	_, err := m.run(t, "", "run", one, two)
	if err == nil || !strings.Contains(err.Error(), "oku shell "+one+" "+two) {
		t.Fatalf("want an error pointing at oku shell, got %v", err)
	}
}

func TestB395RunWarnsForADownloadWithNoChecksum(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "tool", map[string]string{"tool": script})
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = \"file://%s\"\nbin = [\"tool\"]\n", archive,
	))

	out, err := m.run(t, "", "run", ref)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	if !strings.Contains(out, "publishes no checksum, so oku trusted this download") {
		t.Fatalf("run did not report the download it trusted:\n%s", out)
	}
}

func TestB396GcDeletesWhatRunPutInTheStore(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	out, err := m.run(t, "", "run", ref)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	if len(m.storeEntries(t)) != 1 {
		t.Fatalf("the store holds %v", m.storeEntries(t))
	}

	out, err = m.run(t, "", "gc")
	must(t, err)

	if left := m.storeEntries(t); len(left) != 0 {
		t.Fatalf("gc kept %v after:\n%s", left, out)
	}
}

func TestB397RunAndShellTakeTheFlagsThatSteerInference(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "odd", map[string]string{"main": "#!/bin/sh\necho steered\n"})

	// The asset's name fits no platform, so inference cannot choose it alone.
	inferServer(t, &m, map[string]string{"tool-v1.4.0-odd.tar.gz": archive})

	_, err := m.run(t, "", "run", "github:owner/tool")
	if err == nil || !strings.Contains(err.Error(), "--asset") {
		t.Fatalf("want a failure that names --asset, got %v", err)
	}

	out, err := m.run(t, "", "run", "github:owner/tool", "--asset", "*-odd.*", "--bin", "main")
	if err != nil || !strings.Contains(out, "steered") {
		t.Fatalf("run with --asset and --bin: %v\n%s", err, out)
	}

	out, err = m.run(
		t, "", "shell", "github:owner/tool", "--asset", "*-odd.*", "--bin", "main", "--", "main",
	)
	if err != nil || !strings.Contains(out, "steered") {
		t.Fatalf("shell with --asset and --bin: %v\n%s", err, out)
	}

	_, err = m.run(
		t, "", "shell", "github:owner/tool", "github:owner/other", "--asset", "*-odd.*",
	)
	if err == nil || !strings.Contains(err.Error(), "one download") {
		t.Fatalf("want --asset to refuse two refs, got %v", err)
	}
}
