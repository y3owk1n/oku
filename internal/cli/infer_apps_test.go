package cli_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/platform"
)

// guiExe returns the start of a Windows program for the GUI subsystem, which is
// as much of it as inference reads.
func guiExe() string {
	head := make([]byte, 0x40+24+70)
	copy(head, "MZ")
	binary.LittleEndian.PutUint32(head[0x3c:], 0x40)
	copy(head[0x40:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(head[0x40+24+68:], 2)

	return string(head)
}

// artifactOf returns the artifact for p in an inferred manifest, up to the next
// artifact.
func artifactOf(t *testing.T, out string, p platform.Platform) string {
	t.Helper()

	at := strings.Index(out, matchOf(p))
	if at < 0 {
		t.Fatalf("the manifest has no artifact for %s:\n%s", p, out)
	}

	artifact := out[at:]
	if end := strings.Index(artifact, "[[artifact]]"); end >= 0 {
		artifact = artifact[:end]
	}

	return artifact
}

var (
	darwinArm = platform.Platform{OS: "darwin", Arch: "arm64"}
	linuxArm  = platform.Platform{OS: "linux", Arch: "arm64"}
	windowsX  = platform.Platform{OS: "windows", Arch: "amd64"}
)

func TestB352InferenceFindsTheAppsOfEachOS(t *testing.T) {
	m := newMachine(t)

	mac, _ := m.archive(t, "mac", map[string]string{
		"bin/tool":                     script,
		"Tool.app/Contents/MacOS/tool": script,
		"Tool.app/Contents/Info.plist": "<plist/>",
	})
	linux, _ := m.archive(t, "linux", map[string]string{
		"bin/tool": script,
		"share/applications/tool.desktop": "[Desktop Entry]\nType=Application\nName=Tool\n" +
			"Exec=tool %U\nIcon=tool\n",
		"share/applications/tool-shell.desktop": "[Desktop Entry]\nType=Application\n" +
			"Name=Tool Shell\nExec=tool\nTerminal=true\n",
	})
	windows, _ := m.archive(t, "windows", map[string]string{
		"bin/tool.exe":     guiExe(),
		"bin/toolctl.exe":  "MZ",
		"share/readme.txt": "hi",
	})

	inferServer(t, &m, map[string]string{
		assetFor(darwinArm, ".tar.gz"): mac,
		assetFor(linuxArm, ".tar.gz"):  linux,
		assetFor(windowsX, ".tar.gz"):  windows,
	})

	out, err := m.run(t, "", "add", "github:owner/tool", "--manifest")
	must(t, err)

	// The bundle's program is the command, so the command and the app are one
	// install.
	if got := artifactOf(t, out, darwinArm); !strings.Contains(got, `bin = ["Tool.app/Contents/MacOS/tool"]`) ||
		!strings.Contains(got, `app = ["Tool.app"]`) {
		t.Fatalf("the macOS artifact should take the bundle and its command:\n%s", out)
	}

	// A desktop entry that runs the program is the Linux app, and one for the
	// terminal is not.
	if got := artifactOf(t, out, linuxArm); !strings.Contains(got, `app = ["share/applications/tool.desktop"]`) ||
		strings.Contains(got, "Tool.app") {
		t.Fatalf("the Linux artifact should take its desktop entry and no bundle:\n%s", out)
	}

	// A program for the GUI is the Windows app.
	if got := artifactOf(t, out, windowsX); !strings.Contains(got, `app = ["bin/tool.exe"]`) {
		t.Fatalf("the Windows artifact should take its GUI program as the app:\n%s", out)
	}
}

func TestB353AReleaseWithOnlyAnotherProgramForAPlatformLeavesItOut(t *testing.T) {
	m := newMachine(t)
	tool, _ := m.archive(t, "tool", map[string]string{"tool": script})
	helper, _ := m.archive(t, "helper", map[string]string{"toolkit": script})

	// The other program is a single binary for the host's arch, and the
	// package's own build is universal. A .deb carries the distro's name.
	inferServer(t, &m, map[string]string{
		"tool-v1.4.0-universal-apple-darwin.tar.gz":                             tool,
		strings.Replace(assetFor(darwinArm, ".tar.gz"), "tool-", "toolkit-", 1): helper,
		strings.Replace(assetFor(linuxArm, ".tar.gz"), "tool-", "toolkit-", 1):  helper,
		"sometool_1.4.0_amd64.deb":                                              tool,
	})

	out, err := m.run(t, "", "add", "github:owner/tool", "--manifest")
	must(t, err)

	if !strings.Contains(artifactOf(t, out, darwinArm), "/tool.tar.gz") {
		t.Fatalf("macOS should take the package's own build:\n%s", out)
	}

	if strings.Contains(out, matchOf(linuxArm)) {
		t.Fatalf("Linux arm64 has only another program, so it should have no artifact:\n%s", out)
	}

	if !strings.Contains(out, matchOf(platform.Platform{OS: "linux", Arch: "amd64"})) {
		t.Fatalf("Linux amd64 has a .deb, whose name says nothing, so it should keep it:\n%s", out)
	}
}

func TestB354AnArtifactsAppNamesAProgramForALauncher(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "foo", map[string]string{"bin/foo": script, "foo.png": "icon"},
		`bin = ["bin/foo"]`+"\n"+`app = [{ path = "bin/foo", name = "Foo Tool", icon = "foo.png" }]`)

	if out, err := m.run(t, "", "add", ref); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// macOS shows a bundle only, so a launcher is for Linux and Windows.
	if runtime.GOOS != "linux" {
		return
	}

	entry, err := os.ReadFile(filepath.Join(filepath.Dir(m.data), "applications", "oku-foo-tool.desktop"))
	must(t, err)

	for _, want := range []string{"Name=Foo Tool", "/pkg/bin/foo", "/pkg/foo.png"} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("the desktop entry lacks %s:\n%s", want, entry)
		}
	}
}

func TestB355AManifestWithATopLevelAppIsRefused(t *testing.T) {
	m := newMachine(t)
	ref := m.manifest(t, "foo", map[string]string{"bin/foo": script},
		`bin = ["bin/foo"]`+"\n[[app]]\nname = \"Foo\"\nexec = \"bin/foo\"")

	_, err := m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "[[app]] is gone") {
		t.Fatalf("want a refusal that names the artifact's app, got %v", err)
	}
}

func TestB356ARepoNameWithASuffixNamesItsProgramWithout(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "tool", map[string]string{"tool": script, "tool.zig-notes": "text"})

	inferServerFor(t, &m, "owner/tool.zig", map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "add", "github:owner/tool.zig", "--manifest")
	must(t, err)

	if !strings.Contains(out, `name = "tool"`) || !strings.Contains(out, `bin = ["tool"]`) {
		t.Fatalf("the package and its program should be tool:\n%s", out)
	}
}

func TestB357ACommandOfAnAppBundleRunsTheAppsCopy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS copies a bundle to an Applications folder")
	}

	m := newMachine(t)
	ref := m.manifest(t, "foo", map[string]string{
		"Foo.app/Contents/MacOS/foo":  "#!/bin/sh\necho \"$0\"\n",
		"Foo.app/Contents/Info.plist": "<plist>foo 1</plist>",
	}, `bin = ["Foo.app/Contents/MacOS/foo"]`+"\n"+`app = ["Foo.app"]`)

	_, err := m.run(t, "", "add", ref)
	must(t, err)

	app, _ := m.exposedPaths()
	if got := m.output(t, "foo"); got != app {
		t.Fatalf("foo ran %s, want the app's copy at %s", got, app)
	}

	// Without the app's copy, the command runs the store's.
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(app)))
	must(t, os.RemoveAll(bundle))

	if got := m.output(t, "foo"); !strings.Contains(got, "/store/") {
		t.Fatalf("foo ran %s, want the store's copy", got)
	}

	// Another build of the app is not this one, so it never runs.
	must(t, os.MkdirAll(filepath.Dir(app), 0o755))
	must(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte("<plist>foo 2</plist>"), 0o644))
	must(t, os.WriteFile(app, []byte("#!/bin/sh\necho other\n"), 0o755))

	if got := m.output(t, "foo"); got == "other" {
		t.Fatal("foo ran another build of the app")
	}
}

func TestB357ACommandOfABuiltAppBundleRunsTheAppsCopy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS copies a bundle to an Applications folder")
	}

	m := newMachine(t)
	ref := m.buildManifest(t, false, `needs = ["sh"]`, `[[build.step]]
run = '''
mkdir -p Tool.app/Contents/MacOS
printf '#!/bin/sh\necho "$0"\n' > Tool.app/Contents/MacOS/tool
chmod +x Tool.app/Contents/MacOS/tool
echo plist > Tool.app/Contents/Info.plist
'''
shell = "sh"
[[build.step]]
install = { bin = ["Tool.app/Contents/MacOS/tool"], app = ["Tool.app"] }
`)

	if out, err := m.run(t, "", "add", ref, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	want := filepath.Join(os.Getenv("HOME"), "Applications", "Tool.app", "Contents", "MacOS", "tool")
	if got := m.output(t, "tool"); got != want {
		t.Fatalf("tool ran %s, want the app's copy at %s", got, want)
	}
}
