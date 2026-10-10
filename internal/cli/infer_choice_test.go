package cli_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
)

// hostWords returns the OS and arch words of hostAssetName, and the words of
// another arch.
func hostWords() (os, arch, otherArch string) {
	host := platform.Host()
	arch = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[host.Arch]
	otherArch = map[string]string{"amd64": "aarch64", "arm64": "x86_64"}[host.Arch]

	return host.OS, arch, otherArch
}

// hostArtifact returns the artifact of the machine that runs the test from an
// inferred manifest, up to its bin line.
func hostArtifact(t *testing.T, out string) string {
	t.Helper()

	host := platform.Host()

	at := strings.Index(out, `os = "`+host.OS+`", arch = "`+host.Arch+`"`)
	if at < 0 {
		t.Fatalf("the manifest has no artifact for %s:\n%s", host, out)
	}

	artifact := out[at:]

	return artifact[:strings.Index(artifact, "bin =")]
}

func TestB197InferencePrefersTheChecksumsOfTheAssetsPlatformOrAGenericFile(t *testing.T) {
	hostOS, arch, otherArch := hostWords()

	// sums writes one checksum file per name, so the manifest's URL names it.
	sums := func(t *testing.T, m machine, names ...string) map[string]string {
		t.Helper()

		files := map[string]string{}

		for _, name := range names {
			files[name] = filepath.Join(m.fixtures, name)
			must(t, os.WriteFile(files[name], []byte("x"), 0o644))
		}

		return files
	}

	t.Run("os and arch", func(t *testing.T) {
		m := newMachine(t)
		archive, _ := m.archive(t, "release", map[string]string{"tool": script})
		own := "tool-" + hostOS + "-" + arch + "-checksums.txt"

		assets := sums(t, m, "tool-"+hostOS+"-"+otherArch+"-checksums.txt", "checksums.txt", own)
		assets[hostAssetName()] = archive

		inferServer(t, &m, assets)

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if got := hostArtifact(t, out); !strings.Contains(got, "/"+own+`"`) {
			t.Fatalf("the artifact should read the checksums of its own platform:\n%s", out)
		}
	})

	t.Run("generic over another platform", func(t *testing.T) {
		m := newMachine(t)
		archive, _ := m.archive(t, "release", map[string]string{"tool": script})
		otherOS := map[string]string{"darwin": "linux", "linux": "darwin"}[hostOS]

		assets := sums(t, m, "tool-"+otherOS+"-checksums.txt", "SHA256SUMS")
		assets[hostAssetName()] = archive

		inferServer(t, &m, assets)

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if got := hostArtifact(t, out); !strings.Contains(got, `/SHA256SUMS"`) {
			t.Fatalf("the artifact should read the generic checksum file:\n%s", out)
		}
	})

	t.Run("not a bundle or a SHA-1 file of the checksums", func(t *testing.T) {
		m := newMachine(t)
		archive, _ := m.archive(t, "release", map[string]string{"tool": script})

		assets := sums(t, m, "checksums.txt.bundle", "checksums.shasum")
		assets[hostAssetName()] = archive

		inferServer(t, &m, assets)

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if got := hostArtifact(t, out); strings.Contains(got, "sha256_url") {
			t.Fatalf("the artifact should read no bundle or SHA-1 file as its checksums:\n%s", out)
		}
	})

	t.Run("not the checksum file of another asset", func(t *testing.T) {
		m := newMachine(t)
		archive, _ := m.archive(t, "release", map[string]string{"tool": script})
		deb := strings.TrimSuffix(hostAssetName(), ".tar.gz") + ".deb"

		assets := sums(t, m, deb+".sha256sum")
		assets[hostAssetName()], assets[deb] = archive, archive

		inferServer(t, &m, assets)

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if got := hostArtifact(t, out); strings.Contains(got, "sha256_url") {
			t.Fatalf("the artifact should not read the checksum file of the .deb:\n%s", out)
		}
	})
}

func TestB198InferencePrefersTheSmallerAssetAndNoneNamedAsAnApp(t *testing.T) {
	m := newMachine(t)
	_, arch, _ := hostWords()

	// Every build adds one word to the name. Size alone puts the command line
	// build before the bundle. The app is the smallest, so only the word app
	// puts it last.
	cli, _ := m.archive(t, "cli-slim", map[string]string{"tool": script, "notes": noise(1 << 10)})
	bundle, _ := m.archive(t, "bundle", map[string]string{"tool": script, "resources.bin": noise(1 << 12)})
	app, _ := m.archive(t, "app", map[string]string{"tool": script})

	base := strings.TrimSuffix(hostAssetName(), ".tar.gz")
	name := base + "-full.tar.gz"
	slim := base + "-slim.tar.gz"
	appName := base + "-app.tar.gz"
	// The same build in another format is an alternative too.
	sevenZip := base + "-slim.7z"

	inferServer(t, &m, map[string]string{name: bundle, slim: cli, appName: app, sevenZip: cli})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	got := hostArtifact(t, out)
	if !strings.Contains(got, "/cli-slim.tar.gz") {
		t.Fatalf("the %s artifact should be the smaller asset:\n%s", arch, out)
	}

	if !strings.Contains(out, "fit "+infer.Machine(platform.Host())+" too: "+name+", "+sevenZip+", "+appName) {
		t.Fatalf("init should list the larger asset, the 7z and the app:\n%s", out)
	}
}

func TestB349InferencePrefersTheAssetNamedAfterTheRepoOverAnotherProgram(t *testing.T) {
	m := newMachine(t)

	// The other program is smaller, so only its name tells it apart.
	tool, _ := m.archive(t, "tool", map[string]string{
		"tool": script, "resources.bin": strings.Repeat("x", 1<<16),
	})
	server, _ := m.archive(t, "server", map[string]string{"tool-server": script})

	name := hostAssetName()
	serverName := strings.Replace(name, "tool-", "tool-server-", 1)

	inferServer(t, &m, map[string]string{name: tool, serverName: server})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if got := hostArtifact(t, out); !strings.Contains(got, "/tool.tar.gz") {
		t.Fatalf("the artifact should be the asset named after the repo:\n%s", out)
	}

	if !strings.Contains(out, "fit "+infer.Machine(platform.Host())+" too: "+serverName) {
		t.Fatalf("init should list the other program's asset:\n%s", out)
	}
}

func TestB466AnInferredManifestIsTheSameTextOnEveryHost(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})

	// A second asset fits the host. Only inference on the host finds it.
	other := strings.TrimSuffix(hostAssetName(), ".tar.gz") + ".7z"

	inferServer(t, &m, map[string]string{hostAssetName(): archive, other: archive})

	_, err := m.run(t, "", "add", "github:owner/tool")
	must(t, err)

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	pkg, ok := locked.Find("tool")
	if !ok {
		t.Fatal("the lock has no tool")
	}

	if strings.Contains(pkg.Manifest, other) || strings.Contains(pkg.Manifest, platform.Host().String()) {
		t.Fatalf("the locked manifest should not depend on the host that inferred it:\n%s", pkg.Manifest)
	}
}

func TestB199AFailedInstallFromAnInferredManifestSaysWhatToTypeInstead(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})
	sums := filepath.Join(m.fixtures, "checksums.txt")
	must(t, os.WriteFile(sums, []byte(strings.Repeat("1", 64)+"  "+hostAssetName()+"\n"), 0o644))

	// The same build in another format is the alternative --asset may pick.
	other := strings.TrimSuffix(hostAssetName(), ".tar.gz") + ".7z"

	inferServer(t, &m, map[string]string{hostAssetName(): archive, other: archive, "checksums.txt": sums})

	_, err := m.run(t, "", "add", "github:owner/tool")
	if err == nil {
		t.Fatal("add installed a download that does not match the published checksum")
	}

	for _, want := range []string{
		"oku inferred a manifest for github:owner/tool", "--verbose prints it",
		"it chose the asset " + hostAssetName(), "these fit too: " + other,
		"pick one with: oku add github:owner/tool --asset " + other,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error should say %q:\n%v", want, err)
		}
	}

	if strings.Contains(err.Error(), "[[artifact]]") {
		t.Fatalf("without --verbose the error should not print the manifest:\n%v", err)
	}

	_, err = m.run(t, "", "add", "github:owner/tool", "--verbose")
	if err == nil || !strings.Contains(err.Error(), "[[artifact]]") ||
		!strings.Contains(err.Error(), "sha256_url") {
		t.Fatalf("with --verbose the error should end with the manifest:\n%v", err)
	}
}

func TestB271AnInferredManifestForOneOSLimitsItsEntryWithWhen(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})
	other := otherPlatform()

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(
		filepath.Join(m.config, "oku.toml"),
		[]byte("[lock]\nplatforms = [\""+other.String()+"\"]\n\n[packages]\n"), 0o644,
	))

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add should limit the entry instead of failing: %v\n%s", err, out)
	}

	if !strings.Contains(out, "has no artifact or build for "+other.String()) {
		t.Fatalf("add should say why the entry got a when:\n%s", out)
	}

	own, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	hostOS := platform.Host().OS
	if !strings.Contains(string(own), `when = { os = "`+hostOS+`"`) {
		t.Fatalf("oku.toml should limit the package to %s:\n%s", hostOS, own)
	}

	if _, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("a sync after the add should pass: %v", err)
	}
}

func TestB174InferencePrefersTheCommandLineBuildAndTheChecksumsOfItsOwnOS(t *testing.T) {
	m := newMachine(t)
	file := func(name string) string {
		path, _ := m.archive(t, name, map[string]string{"tool": script})

		return path
	}

	// The desktop app is the smaller file and both names add one word, so only
	// the word desktop puts the app last.
	cli, _ := m.archive(t, "cli", map[string]string{"tool": script, "notes": noise(1 << 10)})

	inferServer(t, &m, map[string]string{
		"tool-darwin-arm64-desktop.zip": file("desktop"),
		"tool-darwin-arm64-cli.zip":     cli,
		"tool-linux-arm64.tar.gz":       file("linux"),
		// manifest init wants an asset for the machine it runs on.
		"tool-linux-x86_64.tar.gz": file("linux-amd64"),
		"tool-darwin-x86_64.zip":   file("cli-amd64"),
		// One checksum file for each OS, as stripe/stripe-cli ships them.
		"tool-linux-checksums.txt": file("sums-linux"),
		"tool-mac-checksums.txt":   file("sums-mac"),
	})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	darwin := out[strings.Index(out, `os = "darwin", arch = "arm64"`):]
	darwin = darwin[:strings.Index(darwin, "bin =")]

	if !strings.Contains(darwin, "/cli.tar.gz") || strings.Contains(darwin, "desktop") {
		t.Fatalf("the macOS artifact should be the command line build:\n%s", out)
	}

	if !strings.Contains(darwin, "/sums-mac.tar.gz") {
		t.Fatalf("the macOS artifact should read the checksums of macOS:\n%s", out)
	}

	linux := out[strings.Index(out, `os = "linux", arch = "arm64"`):]
	linux = linux[:strings.Index(linux, "bin =")]

	if !strings.Contains(linux, "/sums-linux.tar.gz") {
		t.Fatalf("the Linux artifact should read the checksums of Linux:\n%s", out)
	}
}

func TestB222InferenceTakesAnAppBundleAsAnAppAndItsCommandAsAProgram(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "app", map[string]string{
		"Tool.app/Contents/MacOS/tool":        script,
		"Tool.app/Contents/MacOS/toolctl":     script,
		"Tool.app/Contents/MacOS/tool_server": script,
		"Tool.app/Contents/MacOS/helper":      script,
		"Tool.app/Contents/Info.plist": "<plist><dict><key>CFBundleExecutable</key>" +
			"<string>tool_server</string></dict></plist>",
		"Tool.app/Contents/Frameworks/x": "helper",
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if !strings.Contains(out, `app = ["Tool.app"]`) || strings.Contains(out, "strip =") {
		t.Fatalf("the bundle should be an app at the top of the package:\n%s", out)
	}

	// A program whose name starts with the package's is a command. The one
	// that opens the app, and the others, are not.
	bins := out[strings.Index(out, "bin ="):]
	bins = bins[:strings.Index(bins, "\n")]

	if !strings.Contains(bins, "MacOS/tool\"") || !strings.Contains(bins, "MacOS/toolctl\"") ||
		strings.Contains(bins, "tool_server") || strings.Contains(bins, "helper") {
		t.Fatalf("the bundle's commands should be tool and toolctl:\n%s", out)
	}
}

// thirdPlatform returns a platform of an OS that is neither the host's nor
// otherPlatform's.
func thirdPlatform() platform.Platform {
	for _, p := range platform.All() {
		if p.OS != platform.Host().OS && p.OS != otherPlatform().OS {
			return p
		}
	}

	panic("unreachable")
}

// assetFor names a release asset for p with the given ending.
func assetFor(p platform.Platform, ending string) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[p.Arch]

	return "tool-v1.4.0-" + arch + "-" + p.OS + ending
}

// matchOf is the start of the match line of p's artifact in an inferred manifest.
func matchOf(p platform.Platform) string {
	return fmt.Sprintf("match = { os = %q, arch = %q", p.OS, p.Arch)
}

func TestB223InferenceOpensAssetsForTheHostAndTheLockPlatformsOnly(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})
	other, third := otherPlatform(), thirdPlatform()

	// The other platform shares the host's ending, and the third has its own.
	// The store reads the format from the bytes, so one file serves both names.
	inferServer(t, &m, map[string]string{
		hostAssetName():            archive,
		assetFor(other, ".tar.gz"): archive,
		assetFor(third, ".tar"):    archive,
	})

	out, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
	must(t, err)

	if !strings.Contains(out, matchOf(other)) || strings.Contains(out, matchOf(third)) {
		t.Fatalf("without [lock] the manifest should cover %s and not %s:\n%s", other, third, out)
	}

	// The lock names the third platform, and update infers again for it.
	listPath := filepath.Join(m.config, "oku.toml")
	own, err := os.ReadFile(listPath)
	must(t, err)
	must(t, os.WriteFile(
		listPath, append([]byte("[lock]\nplatforms = [\""+third.String()+"\"]\n\n"), own...), 0o644,
	))

	out, err = m.run(t, "", "update", "--verbose")
	must(t, err)

	if !strings.Contains(out, matchOf(third)) {
		t.Fatalf("with [lock] naming %s the manifest should cover it:\n%s", third, out)
	}
}

// twoProgramRelease serves a release of owner/tool that holds the programs tool
// and tool-server, each for the host and for otherPlatform.
func twoProgramRelease(t *testing.T, m *machine) {
	t.Helper()

	tool, _ := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho tool\n"})
	server, _ := m.archive(
		t,
		"server",
		map[string]string{"tool-server": "#!/bin/sh\necho server\n"},
	)
	other := assetFor(otherPlatform(), ".tar.gz")

	inferServer(t, m, map[string]string{
		hostAssetName(): tool, other: tool,
		serverAsset(hostAssetName()): server, serverAsset(other): server,
	})
}

func serverAsset(name string) string {
	return strings.Replace(name, "tool-", "tool-server-", 1)
}

func TestB350AnAssetGlobPicksAnotherProgramOfTheReleaseOnEveryPlatform(t *testing.T) {
	m := newMachine(t)
	twoProgramRelease(t, &m)

	for _, args := range [][]string{
		{"add", "github:owner/tool"},
		{"add", "github:owner/tool", "--asset", "tool-server-*"},
	} {
		if out, err := m.run(t, "", args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	if tool, server := m.output(
		t,
		"tool",
	), m.output(
		t,
		"tool-server",
	); tool != "tool" ||
		server != "server" {
		t.Fatalf("tool printed %q and tool-server printed %q", tool, server)
	}

	listed, err := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	must(t, err)

	for _, want := range []string{
		`tool = "github:owner/tool"`,
		`tool-server = { ref = "github:owner/tool", asset = "tool-server-*" }`,
	} {
		if !strings.Contains(string(listed), want) {
			t.Fatalf("oku.toml should hold %s:\n%s", want, listed)
		}
	}

	// The other platform downloads the server's own archive too.
	locked, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	// The lock holds the manifest as a TOML string, with its quotes escaped.
	_, server, _ := strings.Cut(string(locked), "name = 'tool-server'")
	match := strings.ReplaceAll(matchOf(otherPlatform()), `"`, `\"`)

	if !strings.Contains(server, match) || strings.Contains(server, "/tool.tar.gz") {
		t.Fatalf("tool-server should download its own archive on %s:\n%s", otherPlatform(), locked)
	}
}

func TestB351SyncInfersWithTheAssetOfTheListEntry(t *testing.T) {
	m := newMachine(t)
	twoProgramRelease(t, &m)

	// The glob fits both programs, and inference takes the one named after the repo.
	listPath := filepath.Join(m.config, "oku.toml")
	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(listPath, []byte(
		"[packages]\nserver = { ref = \"github:owner/tool\", asset = \"tool-*\" }\n",
	), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if got := m.output(t, "tool"); got != "tool" {
		t.Fatalf("tool printed %q", got)
	}

	// sync infers again when the list names another asset, and keeps the list's name.
	must(t, os.WriteFile(listPath, []byte(
		"[packages]\nserver = { ref = \"github:owner/tool\", asset = \"tool-server-*\" }\n",
	), 0o644))

	if out, err := m.run(t, "", "sync"); err != nil {
		t.Fatalf("sync after the asset changed: %v\n%s", err, out)
	}

	if got := m.output(t, "tool-server"); got != "server" {
		t.Fatalf("tool-server printed %q", got)
	}

	// An entry without an asset keeps the one the lock recorded.
	must(t, os.WriteFile(listPath, []byte("[packages]\nserver = \"github:owner/tool\"\n"), 0o644))

	if out, err := m.run(t, "", "update", "server"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	if got := m.output(t, "tool-server"); got != "server" {
		t.Fatalf("after update tool-server printed %q", got)
	}
}

func TestB433AnAssetGlobForAnotherBuildKeepsTheRepoName(t *testing.T) {
	m := newMachine(t)

	// tool-portable is a build of tool, and holds the program tool.
	tool, _ := m.archive(t, "tool", map[string]string{"tool": script})
	portable, _ := m.archive(t, "portable", map[string]string{"tool": script})
	portableName := strings.Replace(hostAssetName(), "tool-", "tool-portable-", 1)

	inferServer(t, &m, map[string]string{hostAssetName(): tool, portableName: portable})

	out, err := m.run(t, "", "add", "github:owner/tool", "--asset", "tool-portable-*", "--manifest")
	if err != nil {
		t.Fatalf("add --manifest: %v\n%s", err, out)
	}

	if !strings.Contains(out, `name = "tool"`) || !strings.Contains(hostArtifact(t, out), "/portable.tar.gz") {
		t.Fatalf("the package should keep the repo's name and download the portable build:\n%s", out)
	}
}

// artifactURL returns the url of the first artifact of an inferred manifest
// whose match starts with selector, such as `os = "linux", arch = "amd64"`,
// with the tag in place of its variable, or "" when there is none.
func artifactURL(out, selector string) string {
	at := strings.Index(out, "match = { "+selector)
	if at < 0 {
		return ""
	}

	rest := out[at:]
	rest = rest[strings.Index(rest, "url = ")+len("url = "):]

	return strings.ReplaceAll(rest[:strings.Index(rest, "\n")], "{{tag}}", "v1.4.0")
}

// inferAssets serves the host's asset and one archive per other name, each
// holding the program, and returns the manifest oku infers.
func inferAssets(t *testing.T, names ...string) string {
	t.Helper()

	m := newMachine(t)
	assets := map[string]string{}

	for _, name := range append(names, hostAssetName()) {
		assets[name], _ = m.archive(t, strings.TrimSuffix(name, ".tar.gz"), map[string]string{"tool": script})
	}

	inferServer(t, &m, assets)

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	if err != nil {
		t.Fatalf("manifest init: %v\n%s", err, out)
	}

	return out
}

func TestB478InferencePrefersThePlainBuildOverAVariant(t *testing.T) {
	m := newMachine(t)

	// The plain build is the largest, so only its name favours it.
	plain, _ := m.archive(t, "tool-v1.4.0-linux-amd64", map[string]string{
		"tool": script, "resources.bin": strings.Repeat("x", 1<<16),
	})
	baseline, _ := m.archive(t, "tool-v1.4.0-linux-amd64-baseline", map[string]string{"tool": script})
	pivkey, _ := m.archive(t, "tool-v1.4.0-linux-pivkey-amd64", map[string]string{"tool": script})
	host, _ := m.archive(t, "host", map[string]string{"tool": script})

	inferServer(t, &m, map[string]string{
		"tool-v1.4.0-linux-amd64.tar.gz":          plain,
		"tool-v1.4.0-linux-amd64-baseline.tar.gz": baseline,
		"tool-v1.4.0-linux-pivkey-amd64.tar.gz":   pivkey,
		hostAssetName():                           host,
	})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if got := artifactURL(out, `os = "linux", arch = "amd64"`); !strings.Contains(got, "/tool-v1.4.0-linux-amd64.tar.gz") {
		t.Fatalf("linux amd64 should take the plain build, got %s:\n%s", got, out)
	}
}

func TestB479InferenceTakesTheBuildWithoutALibcAsGlibcBesideAMuslBuild(t *testing.T) {
	out := inferAssets(t, "tool-v1.4.0-linux-arm64.tar.gz", "tool-v1.4.0-linux-arm64-musl.tar.gz")

	if got := artifactURL(out, `os = "linux", arch = "arm64", libc = "glibc" }`); !strings.Contains(got, "/tool-v1.4.0-linux-arm64.tar.gz") {
		t.Fatalf("glibc hosts should take the build without a libc, got %s:\n%s", got, out)
	}

	if got := artifactURL(out, `os = "linux", arch = "arm64" }`); !strings.Contains(got, "/tool-v1.4.0-linux-arm64-musl.tar.gz") {
		t.Fatalf("other hosts should take the musl build, got %s:\n%s", got, out)
	}

	// A .deb names no libc either, but it is no glibc build of its own.
	out = inferAssets(t, "tool_1.4.0_arm64.deb", "tool-v1.4.0-linux-arm64-musl.tar.gz")

	if got := artifactURL(out, `os = "linux", arch = "arm64", libc = "glibc" }`); got != "" {
		t.Fatalf("glibc hosts should take the musl build, not %s:\n%s", got, out)
	}
}

func TestB480InferenceSkipsAssetsOfUnknownArchesAndAndroid(t *testing.T) {
	out := inferAssets(t,
		"tool-v1.4.0-linux-ppc64le.rpm", "tool-v1.4.0-linux-armv6.tar.gz", "tool-v1.4.0-linux-arm64-android.tar.gz",
		"tool-v1.4.0-armv7-linux-androideabi.tar.gz",
	)

	for _, name := range []string{"ppc64le", "armv6", "android"} {
		if strings.Contains(out, name) {
			t.Fatalf("no artifact should take the %s asset:\n%s", name, out)
		}
	}
}

func TestB481InferenceReadsPlatformsFromMoreNames(t *testing.T) {
	cases := []struct{ asset, selector string }{
		{"tool-v1.4.0-macos.tar.gz", `os = "darwin", arch = "amd64"`},
		{"tool-v1.4.0-win32-x64.tar.gz", `os = "windows", arch = "amd64"`},
		{"tool-v1.4.0-Linux-64bit.tar.gz", `os = "linux", arch = "amd64"`},
		{"tool-v1.4.0-linux-32bit.tar.gz", `os = "linux", arch = "386"`},
		{"tool-v1.4.0-linux-32-bit.tar.gz", `os = "linux", arch = "386"`},
		{"tool-v1.4.0-macOS_64-bit.tar.gz", `os = "darwin", arch = "amd64"`},
	}

	for _, c := range cases {
		t.Run(c.asset, func(t *testing.T) {
			out := inferAssets(t, c.asset)

			if got := artifactURL(out, c.selector); !strings.Contains(got, "/"+strings.TrimSuffix(c.asset, ".tar.gz")) {
				t.Fatalf("%s should take %s, got %s:\n%s", c.selector, c.asset, got, out)
			}

			amd64 := filepath.Base(artifactURL(out, `os = "linux", arch = "amd64"`))
			if strings.Contains(amd64, "32bit") || strings.Contains(amd64, "32-bit") {
				t.Fatalf("a 32-bit build is no amd64 build:\n%s", out)
			}
		})
	}

	t.Run("exe", func(t *testing.T) {
		m := newMachine(t)
		host, _ := m.archive(t, "host", map[string]string{"tool": script})
		exe := filepath.Join(m.fixtures, "tool.exe")
		must(t, os.WriteFile(exe, []byte(script), 0o755))

		inferServer(t, &m, map[string]string{"tool.exe": exe, hostAssetName(): host})

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if got := artifactURL(out, `os = "windows", arch = "amd64"`); !strings.Contains(got, "/tool.exe") {
			t.Fatalf("windows amd64 should take tool.exe, got %s:\n%s", got, out)
		}

		if artifactURL(out, `os = "windows", arch = "arm64"`) != "" {
			t.Fatalf("an exe without an arch is no arm64 build:\n%s", out)
		}
	})

	// A name that says setup is an installer, whatever the file holds.
	t.Run("setup exe", func(t *testing.T) {
		m := newMachine(t)
		host, _ := m.archive(t, "host", map[string]string{"tool": script})
		setup := filepath.Join(m.fixtures, "tool-1.4.0-setup.exe")
		must(t, os.WriteFile(setup, []byte("MZ"), 0o644))

		inferServer(t, &m, map[string]string{"tool-1.4.0-setup.exe": setup, hostAssetName(): host})

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if strings.Contains(out, `os = "windows"`) {
			t.Fatalf("windows should get no artifact from a setup program:\n%s", out)
		}
	})
}

// noise returns n characters that do not compress, so an archive that holds
// them is larger by about n.
func noise(n int) string {
	data := make([]byte, n/2)
	_, _ = rand.Read(data)

	return hex.EncodeToString(data)
}

func TestB482InferenceRefusesAWindowsSetupProgram(t *testing.T) {
	for _, mark := range []string{"Inno Setup Setup Data (6.4.3)", "Nullsoft.NSIS.exehead"} {
		t.Run(mark, func(t *testing.T) {
			m := newMachine(t)
			host, _ := m.archive(t, "host", map[string]string{"tool": script})
			setup := filepath.Join(m.fixtures, "tool-x86_64.exe")
			must(t, os.WriteFile(setup, []byte("MZ\x00\x00"+mark), 0o644))

			inferServer(t, &m, map[string]string{"tool-x86_64.exe": setup, hostAssetName(): host})

			out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
			must(t, err)

			if strings.Contains(out, `os = "windows"`) {
				t.Fatalf("windows should get no artifact from a setup program:\n%s", out)
			}
		})
	}
}

func TestB482AddLeavesOutAnExeItDidNotOpen(t *testing.T) {
	m := newMachine(t)

	// add opens the host's asset only. The host's is a single binary, so an
	// unopened exe would otherwise take its layout.
	host := filepath.Join(m.fixtures, "tool-host")
	must(t, os.WriteFile(host, []byte(script), 0o755))
	setup := filepath.Join(m.fixtures, "tool-x86_64.exe")
	must(t, os.WriteFile(setup, []byte("MZ\x00\x00Nullsoft.NSIS.exehead"), 0o644))

	inferServer(t, &m, map[string]string{
		strings.TrimSuffix(hostAssetName(), ".tar.gz"): host,
		"tool-x86_64.exe": setup,
	})

	out, err := m.run(t, "", "add", "github:owner/tool")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	pkg, _ := locked.Find("tool")
	if strings.Contains(pkg.Manifest, `os = "windows"`) {
		t.Fatalf("windows should get no artifact from an exe add did not open:\n%s", pkg.Manifest)
	}
}

// plainArchive writes a tar.gz whose files have no mode bits for running,
// as a tar made without modes has, and returns its path.
func plainArchive(t *testing.T, m machine, name string, files map[string]string) string {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for path, body := range files {
		must(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(body))}))

		_, err := tw.Write([]byte(body))
		must(t, err)
	}

	must(t, tw.Close())
	must(t, gz.Close())

	path := filepath.Join(m.fixtures, name+".tar.gz")
	must(t, os.WriteFile(path, buf.Bytes(), 0o644))

	return path
}

func TestB483InferenceTakesTheProgramEveryAssetIsNamedAfter(t *testing.T) {
	stemmed := strings.Replace(hostAssetName(), "tool-", "toolfmt-", 1)

	t.Run("single binary", func(t *testing.T) {
		m := newMachine(t)
		bin := filepath.Join(m.fixtures, "toolfmt")
		must(t, os.WriteFile(bin, []byte(script), 0o755))

		inferServer(t, &m, map[string]string{strings.TrimSuffix(stemmed, ".tar.gz"): bin})

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		must(t, err)

		if !strings.Contains(out, `name = "tool"`) || !strings.Contains(out, `bin = ["toolfmt"]`) {
			t.Fatalf("the package should keep the repo's name and run toolfmt:\n%s", out)
		}
	})

	t.Run("archive with several programs", func(t *testing.T) {
		m := newMachine(t)
		archive, _ := m.archive(t, "toolfmt", map[string]string{"toolfmt": script, "toolfmt_plugin": script})

		inferServer(t, &m, map[string]string{stemmed: archive})

		out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
		if err != nil {
			t.Fatalf("manifest init: %v\n%s", err, out)
		}

		if !strings.Contains(out, `bin = ["toolfmt"]`) {
			t.Fatalf("the program should be toolfmt:\n%s", out)
		}
	})
}

func TestB484AProgramNamedAfterItsAssetRunsUnderThePackagesName(t *testing.T) {
	hostOS, arch, otherArch := hostWords()
	own := map[string]string{"darwin": "apple-darwin", "linux": "unknown-linux-musl"}[hostOS]

	m := newMachine(t)
	assets := map[string]string{}

	// A tar made without modes leaves the program plain, and a script beside
	// it is executable.
	for _, a := range []string{arch, otherArch} {
		file := "tool-" + a + "-" + own
		assets[file+".tar.gz"] = plainArchive(t, m, file, map[string]string{file: script, "install-man.sh": script})
	}

	inferServer(t, &m, assets)

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	if err != nil {
		t.Fatalf("manifest init: %v\n%s", err, out)
	}

	for _, a := range []string{arch, otherArch} {
		want := `bin = [{ name = "tool", path = "tool-` + a + "-" + own + `" }]`
		if !strings.Contains(out, want) {
			t.Fatalf("the %s artifact should run its own file as tool:\n%s", a, out)
		}
	}
}

func TestB485LibrariesAndScriptsAreNoPrograms(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{
		"bin/runner": script, "lib/parser.so": "x", "lib/libtool.so.1": "x", "share/less.sh": script,
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	if err != nil {
		t.Fatalf("manifest init: %v\n%s", err, out)
	}

	if !strings.Contains(out, `bin = ["bin/runner"]`) {
		t.Fatalf("the only program should be bin/runner:\n%s", out)
	}
}

func TestB486ADesktopEntryIsAnAppOnLinuxOnly(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{
		"tool":                           script,
		"etc/linux-desktop/tool.desktop": "[Desktop Entry]\nType=Application\nName=Tool\nExec=tool %U\n",
	})

	assets := map[string]string{hostAssetName(): archive}
	for _, name := range []string{"tool-v1.4.0-x86_64-unknown-linux-gnu.tar.gz", "tool-v1.4.0-x86_64-apple-darwin.tar.gz"} {
		assets[name] = archive
	}

	inferServer(t, &m, assets)

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	for _, artifact := range strings.Split(out, "[[artifact]]")[1:] {
		linux := strings.Contains(artifact, `os = "linux"`)
		if hasApp := strings.Contains(artifact, "tool.desktop"); hasApp != linux {
			t.Fatalf("only a linux artifact should list the desktop entry:\n%s", out)
		}
	}
}

func TestB487InferenceReadsSHASUMS256AndSha256Txt(t *testing.T) {
	for _, sums := range []string{"SHASUMS256.txt", "sha256.txt"} {
		t.Run(sums, func(t *testing.T) {
			m := newMachine(t)
			archive, _ := m.archive(t, "release", map[string]string{"tool": script})
			file := filepath.Join(m.fixtures, sums)
			must(t, os.WriteFile(file, []byte("x"), 0o644))

			inferServer(t, &m, map[string]string{hostAssetName(): archive, sums: file})

			out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
			must(t, err)

			if !strings.Contains(hostArtifact(t, out), "/"+sums+`"`) {
				t.Fatalf("the artifact should read %s:\n%s", sums, out)
			}
		})
	}
}

func TestB488InferenceSkipsSHA1AndOtherHashFiles(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script})
	name := strings.TrimSuffix(hostAssetName(), ".tar.gz")

	assets := map[string]string{hostAssetName(): archive}
	for _, ending := range []string{".shasum", ".sha1", ".md5sum", ".sha512sum"} {
		assets[name+ending] = archive
	}

	inferServer(t, &m, assets)

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if strings.Contains(out, "fit "+infer.Machine(platform.Host())+" too") {
		t.Fatalf("no hash file should fit as an asset:\n%s", out)
	}
}

func TestB489AReleaseWithNoAssetsSaysSo(t *testing.T) {
	m := newMachine(t)
	inferServer(t, &m, map[string]string{})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	if err == nil || !strings.Contains(out+err.Error(), "release v1.4.0 of owner/tool has no assets\nname an older release") {
		t.Fatalf("init should say the release has no assets and what to do: %v\n%s", err, out)
	}
}

func TestB490OkuNamesTheProgramsItLeftOutAndHowToAddThem(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "release", map[string]string{"tool": script, "toolx": script})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if !strings.Contains(out, `bin = ["tool"]`) || !strings.Contains(out, "the asset also holds toolx") {
		t.Fatalf("init should leave toolx out and say so:\n%s", out)
	}

	out, err = m.run(t, "", "add", "github:owner/tool", "--dry-run")
	must(t, err)

	if !strings.Contains(out, "also holds") || !strings.Contains(out, "oku add github:owner/tool --bin tool --bin toolx") {
		t.Fatalf("the plan should list toolx and the command:\n%s", out)
	}

	out, err = m.run(t, "", "add", "github:owner/tool", "--dry-run", "--json")
	must(t, err)

	if !regexp.MustCompile(`"other_programs":\s*\[\s*"toolx"\s*\]`).MatchString(out) {
		t.Fatalf("the JSON plan should list toolx in other_programs:\n%s", out)
	}

	out, err = m.run(t, "", "add", "github:owner/tool")
	must(t, err)

	want := "tool also holds toolx, which oku left out\nadd them with: oku add github:owner/tool --bin tool --bin toolx"
	if !strings.Contains(out, want) {
		t.Fatalf("add should name toolx and the command:\n%s", out)
	}

	out, err = m.run(t, "", "add", "github:owner/tool", "--bin", "tool", "--bin", "toolx")
	if err != nil {
		t.Fatalf("the suggested command: %v\n%s", err, out)
	}

	locked, err := lock.Read(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	pkg, _ := locked.Find("tool")
	if !strings.Contains(pkg.Manifest, `bin = ["tool", "toolx"]`) {
		t.Fatalf("the suggested command should expose toolx:\n%s", pkg.Manifest)
	}
}

func TestB491InferencePrefersAnMSVCBuildOnWindows(t *testing.T) {
	m := newMachine(t)
	host, _ := m.archive(t, "host", map[string]string{"tool": script})
	// The gnu build is smaller, so only its toolchain puts it second.
	msvc, _ := m.archive(t, "tool-v1.4.0-x86_64-pc-windows-msvc", map[string]string{
		"tool.exe": script, "resources.bin": strings.Repeat("x", 1<<16),
	})
	gnu, _ := m.archive(t, "tool-v1.4.0-x86_64-pc-windows-gnu", map[string]string{"tool.exe": script})

	inferServer(t, &m, map[string]string{
		hostAssetName(): host,
		"tool-v1.4.0-x86_64-pc-windows-msvc.tar.gz": msvc,
		"tool-v1.4.0-x86_64-pc-windows-gnu.tar.gz":  gnu,
	})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if got := artifactURL(out, `os = "windows", arch = "amd64"`); !strings.Contains(got, "windows-msvc") {
		t.Fatalf("windows should take the msvc build, got %s:\n%s", got, out)
	}
}

func TestB536AnInferredURLTurnsTheTagIntoAVariableAndLeavesTheRepoName(t *testing.T) {
	_, arch, _ := hostWords()
	platformWords := strings.TrimPrefix(strings.TrimSuffix(hostAssetName(), ".tar.gz"), "tool-v1.4.0-")

	for _, c := range []struct{ repo, tag, folder, asset, want, wrong string }{
		// GitHub writes the + of the tag as %2B in its download URLs.
		{"owner/tool", "v1.30.0+k3s1", "v1.30.0+k3s1", "tool-" + platformWords + ".tar.gz", "/{{tag}}/tool-", "k3s1"},
		// The 2 of tool2 is the repo's name, and the version 2 only where it stands alone.
		{"owner/tool2", "v2", "tool2/v2", "tool2-v2-" + platformWords + ".tar.gz", "/tool2/{{tag}}/tool2-{{tag}}-", "tool{{version}}"},
	} {
		m := newMachine(t)
		archive, _ := m.archive(t, "release", map[string]string{"tool": script})

		folder := filepath.Join(m.fixtures, filepath.FromSlash(c.folder))
		must(t, os.MkdirAll(folder, 0o755))
		must(t, os.Rename(archive, filepath.Join(folder, c.asset)))

		info, err := os.Stat(filepath.Join(folder, c.asset))
		must(t, err)

		link := "file://" + filepath.ToSlash(m.fixtures) + "/" +
			strings.ReplaceAll(c.folder, "+", "%2B") + "/" + c.asset
		release := fmt.Sprintf(`{"tag_name": %q, "assets": [{"name": %q, "browser_download_url": %q, "size": %d}]}`,
			c.tag, c.asset, link, info.Size())

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch strings.TrimPrefix(r.URL.Path, "/api/repos/"+c.repo) {
			case "/commits/HEAD":
				_, _ = w.Write([]byte("5555555555555555555555555555555555555555"))
			case "/releases/latest", "/releases/tags/" + c.tag:
				_, _ = w.Write([]byte(release))
			case "/releases":
				_, _ = w.Write([]byte("[" + release + "]"))
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		m.opts.GitHubAPI = server.URL + "/api"
		m.opts.GitHubRaw = server.URL + "/raw"

		out, err := m.run(t, "", "manifest", "init", "--from", c.repo, "-o", "-")
		if err != nil {
			t.Fatalf("%s: init: %v\n%s", c.tag, err, out)
		}

		if got := hostArtifact(t, out); !strings.Contains(got, c.want) || strings.Contains(got, c.wrong) {
			t.Fatalf("%s: the %s url should hold %q and not %q:\n%s", c.tag, arch, c.want, c.wrong, out)
		}

		if _, err := m.run(t, "", "add", "github:"+c.repo, "--yes"); err != nil {
			t.Fatalf("%s: add: %v", c.tag, err)
		}
	}
}
