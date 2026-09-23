package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
}

func TestB198InferencePrefersTheSmallerAssetAndNoneNamedAsAnApp(t *testing.T) {
	m := newMachine(t)
	_, arch, _ := hostWords()

	// The command line build has the longer name, so only its size favours it.
	cli, _ := m.archive(t, "cli-slim", map[string]string{"tool": script})
	bundle, _ := m.archive(t, "bundle", map[string]string{
		"tool": script, "resources.bin": strings.Repeat("x", 1<<16),
	})
	app, _ := m.archive(t, "app", map[string]string{"tool": script})

	name := hostAssetName()
	slim := strings.TrimSuffix(name, ".tar.gz") + "-slim.tar.gz"
	appName := strings.Replace(name, "tool-", "tool-app-", 1)
	// The same build in another format is an alternative too.
	sevenZip := strings.TrimSuffix(name, ".tar.gz") + ".7z"

	inferServer(t, &m, map[string]string{name: bundle, slim: cli, appName: app, sevenZip: cli})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	got := hostArtifact(t, out)
	if !strings.Contains(got, "/cli-slim.tar.gz") {
		t.Fatalf("the %s artifact should be the smaller asset:\n%s", arch, out)
	}

	if !strings.Contains(out, "fit this machine too: "+name+", "+sevenZip+", "+appName) {
		t.Fatalf("the manifest should list the larger asset, the 7z and the app:\n%s", out)
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

	inferServer(t, &m, map[string]string{
		// A desktop app beside the command line build, as sst/opencode ships them.
		"tool-desktop-mac-arm64.app.tar.gz": file("desktop"),
		"tool-darwin-arm64.zip":             file("cli"),
		"tool-linux-arm64.tar.gz":           file("linux"),
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

func TestB222InferenceTakesAnAppBundleAsAnAppAndNotAsAProgram(t *testing.T) {
	m := newMachine(t)
	archive, _ := m.archive(t, "app", map[string]string{
		"Tool.app/Contents/MacOS/tool":   script,
		"Tool.app/Contents/Info.plist":   "<plist/>",
		"Tool.app/Contents/Frameworks/x": "helper",
	})

	inferServer(t, &m, map[string]string{hostAssetName(): archive})

	out, err := m.run(t, "", "manifest", "init", "--from", "owner/tool", "-o", "-")
	must(t, err)

	if !strings.Contains(out, `app = ["Tool.app"]`) || strings.Contains(out, "bin =") ||
		strings.Contains(out, "strip =") {
		t.Fatalf("the bundle should be an app at the top of the package:\n%s", out)
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
