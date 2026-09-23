package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
)

// vendorKind is how one language's packages are downloaded into the source
// directory. The build steps after it then work offline.
type vendorKind struct {
	// tool must be on the build's PATH, through needs or a dep.
	tools []string
	// script runs in the source directory with the network on. "$tool" is the
	// tool that was found.
	script string
	// output is the directory the script fills. oku hashes it. It is in the
	// prefix with inPrefix, else in the source directory.
	output   string
	inPrefix bool
	// after runs in the same way once output is hashed, so what it adds is not
	// in the digest. Empty for most kinds.
	after string
	// pwsh and pwshAfter are script and after for Windows, where "$tool" is the
	// variable $tool. Without them Windows runs the sh scripts.
	pwsh, pwshAfter string
	// env is added to the step's environment.
	env []string
	// portable reports that the script fills output with the same files on every
	// platform, so one digest serves them all. npm and pip pick packages by
	// platform.
	portable bool
}

// cargoVendor vendors what Cargo.lock pins and points cargo at the vendor
// directory, so the build after it works offline. cargoVendorPwsh is the same
// for Windows. The TOML strings take single quotes, which neither shell has to
// escape.
const (
	cargoVendor = `"$tool" vendor --locked vendor >/dev/null
mkdir -p .cargo
printf "\n[source.crates-io]\nreplace-with = 'vendored-sources'\n\n[source.vendored-sources]\ndirectory = 'vendor'\n" >> .cargo/config.toml`
	cargoVendorPwsh = `& $tool vendor --locked vendor | Out-Null
if ($LASTEXITCODE -ne 0) { exit 1 }
New-Item -ItemType Directory -Force .cargo | Out-Null
Add-Content .cargo/config.toml "` + "`n[source.crates-io]`nreplace-with = 'vendored-sources'`n`n[source.vendored-sources]`ndirectory = 'vendor'" + `"`
)

const (
	npmPackageKind   = "npm package"
	pipPackageKind   = "pip package"
	goPackageKind    = "go package"
	cargoPackageKind = "cargo package"
)

var vendorKinds = map[string]vendorKind{
	"go": {
		tools:  []string{"go"},
		script: `"$tool" mod vendor`,
		output: "vendor",
		// A toolchain download would be a second, unhashed download.
		env:      []string{"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod"},
		portable: true,
	},
	"cargo": {
		tools:    []string{"cargo"},
		script:   cargoVendor,
		pwsh:     cargoVendorPwsh,
		output:   "vendor",
		portable: true,
	},
	// cargoPackageKind is a cargo step with "package", in the source of that
	// crate. It vendors what the crate's Cargo.lock pins, and once that is
	// hashed it installs the crate's programs from it alone.
	cargoPackageKind: {
		tools:     []string{"cargo"},
		script:    cargoVendor,
		pwsh:      cargoVendorPwsh,
		after:     `"$tool" install --path . --locked --offline --no-track --root "$OKU_PREFIX"`,
		pwshAfter: "& $tool install --path . --locked --offline --no-track --root $env:OKU_PREFIX\nif ($LASTEXITCODE -ne 0) { exit 1 }",
		output:    "vendor",
		portable:  true,
	},
	"npm": {
		tools:  []string{"npm"},
		script: `"$tool" ci --ignore-scripts --no-audit --no-fund`,
		output: "node_modules",
	},
	// npmPackageKind is an npm step with "package". It installs one package with
	// its dependencies and runs none of their scripts. Once the install is
	// hashed it runs the install scripts of the packages OKU_NPM_SCRIPTS names,
	// if any. npm rebuild runs the lifecycle scripts of the named packages alone.
	npmPackageKind: {
		tools: []string{"npm"},
		script: `"$tool" install --ignore-scripts --omit=dev --no-audit --no-fund --no-package-lock \
  --before="$OKU_NPM_BEFORE" --prefix "$OKU_PREFIX/lib" "$OKU_NPM_PACKAGE"`,
		output:   "lib/node_modules",
		inPrefix: true,
		after: `if [ -n "$OKU_NPM_SCRIPTS" ]; then
  "$tool" rebuild --no-audit --no-fund --prefix "$OKU_PREFIX/lib" $OKU_NPM_SCRIPTS
fi`,
		env: []string{"npm_config_update_notifier=false"},
	},
	"pip": {
		tools:  []string{"pip", "pip3"},
		script: `"$tool" download --disable-pip-version-check -q -r requirements.txt -d vendor/pip`,
		output: "vendor/pip",
	},
	// goPackageKind is a go step with "package", the path of the package to
	// build. The go command downloads the module and every module it needs into
	// a module cache in the source directory, and checks each against the
	// checksum database. oku hashes the downloads, which are the same files on
	// every platform, and leaves out the checksum database's own files, which
	// change as it grows. Then go install builds the package from that cache
	// alone, which records the module's version in the program.
	goPackageKind: {
		tools: []string{"go"},
		script: `export GOMODCACHE="$PWD/modcache"
"$tool" mod download -json "$OKU_GO_MODULE@v$OKU_GO_VERSION" > "$TMPDIR/oku-go-module.json" ||
  { cat "$TMPDIR/oku-go-module.json" >&2; exit 1; }
dir=$(sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p' "$TMPDIR/oku-go-module.json")
[ -n "$dir" ] || { echo "go mod download named no directory for $OKU_GO_MODULE" >&2; exit 1; }
(cd "$dir" && "$tool" mod download)
rm -rf "$GOMODCACHE/cache/download/sumdb"`,
		after: `GOMODCACHE="$PWD/modcache" GOPROXY="file://$PWD/modcache/cache/download" GOSUMDB=off \
  GOBIN="$OKU_PREFIX/bin" CGO_ENABLED=0 "$tool" install -trimpath "$OKU_GO_PACKAGE@v$OKU_GO_VERSION"`,
		pwsh: `$env:GOMODCACHE = Join-Path (Get-Location) 'modcache'
$json = & $tool mod download -json "$($env:OKU_GO_MODULE)@v$($env:OKU_GO_VERSION)" | Out-String
if ($LASTEXITCODE -ne 0) { [Console]::Error.WriteLine($json); exit 1 }
Push-Location ($json | ConvertFrom-Json).Dir
& $tool mod download
if ($LASTEXITCODE -ne 0) { exit 1 }
Pop-Location
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $env:GOMODCACHE 'cache\download\sumdb')`,
		pwshAfter: `$cache = Join-Path (Get-Location) 'modcache'
$env:GOMODCACHE = $cache
$env:GOPROXY = 'file:///' + ((Join-Path $cache 'cache\download') -replace '\\', '/')
$env:GOSUMDB = 'off'
$env:GOBIN = Join-Path $env:OKU_PREFIX 'bin'
$env:CGO_ENABLED = '0'
& $tool install -trimpath "$($env:OKU_GO_PACKAGE)@v$($env:OKU_GO_VERSION)"
if ($LASTEXITCODE -ne 0) { exit 1 }`,
		output: "modcache/cache/download",
		// A toolchain download would be a second, unhashed download.
		env:      []string{"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -modcacherw"},
		portable: true,
	},
	// pipPackageKind is a pip step with "package". uv installs one package with
	// its dependencies as they were when that version was uploaded, into a
	// directory and not a venv, whose files would hold this machine's paths.
	// The programs that uv writes into bin name the python by its path too, so
	// they move out of what oku hashes. Once the install is hashed, oku writes
	// its own program for each console script of the package, and copies the
	// package's other programs, such as a binary in the wheel's data scripts.
	pipPackageKind: {
		tools: []string{"uv"},
		script: `python=$(command -v python3) || { echo "a pypi package needs python3" >&2; exit 1; }
"$tool" pip install --no-config --quiet --target "$OKU_PREFIX/lib/python" --python "$python" \
  --exclude-newer "$OKU_PIP_BEFORE" ${OKU_PIP_PLATFORM:+--python-platform "$OKU_PIP_PLATFORM"} \
  "$OKU_PIP_PACKAGE==$OKU_PIP_VERSION"
rm -rf "$OKU_PREFIX/lib/python-bin"
if [ -d "$OKU_PREFIX/lib/python/bin" ]; then mv "$OKU_PREFIX/lib/python/bin" "$OKU_PREFIX/lib/python-bin"; fi
"$python" - "$OKU_PREFIX/lib/python" "$OKU_PREFIX/lib/python-bin" <<'EOF'
` + pipRecords + `EOF`,
		output:   "lib/python",
		inPrefix: true,
		after: `python=$(command -v python3)
"$python" - "$OKU_PREFIX" "$OKU_PIP_PACKAGE" "$python" <<'EOF'
` + pipWrappers + `EOF`,
		// uv must not download a python of its own, which the lock would not pin.
		env: []string{"UV_PYTHON_DOWNLOADS=never", "UV_NO_PROGRESS=1"},
	},
}

// pipRecords moves the lines for bin out of each RECORD of the install, next
// to the programs they name. Those programs hold the python's path, so their
// hashes in RECORD would differ from one machine to the next.
const pipRecords = `import glob, os, sys

lib, moved = sys.argv[1:]
for record in glob.glob(os.path.join(lib, "*.dist-info", "RECORD")):
    with open(record) as f:
        lines = f.read().splitlines(True)
    programs = [line for line in lines if line.startswith("bin/")]
    if not programs:
        continue
    os.makedirs(moved, exist_ok=True)
    with open(os.path.join(moved, os.path.basename(os.path.dirname(record)) + ".programs"), "w") as f:
        f.writelines(programs)
    with open(record, "w") as f:
        f.writelines(line for line in lines if not line.startswith("bin/"))
`

// pipWrappers puts the package's programs into bin. For a console script it
// writes a program the way pip would, but one that names the installed files
// and the python directly. Any other program of the package, such as a binary
// in its data scripts, is copied from where uv put it. The programs of the
// package's dependencies are not the package's.
const pipWrappers = `import importlib.metadata, os, re, shutil, sys

prefix, name, python = sys.argv[1:]
lib = os.path.join(prefix, "lib", "python")
moved = os.path.join(prefix, "lib", "python-bin")
want = re.sub(r"[-_.]+", "-", name).lower()

found = [d for d in importlib.metadata.distributions(path=[lib])
         if re.sub(r"[-_.]+", "-", d.metadata["Name"]).lower() == want]
if not found:
    sys.exit("uv installed no package called " + name)

dist = found[0]
scripts = [e for e in dist.entry_points if e.group == "console_scripts"]
named = {e.name for e in scripts}
info = [p.parent.name for p in (dist.files or []) if p.name == "METADATA"][0]
listed = os.path.join(moved, info + ".programs")
others = []
if os.path.exists(listed):
    with open(listed) as f:
        others = [line.split(",")[0][len("bin/"):] for line in f]
others = [p for p in others if p and "/" not in p and p not in named]
if not scripts and not others:
    sys.exit("the Python package " + name + " has no programs")

bin = os.path.join(prefix, "bin")
os.makedirs(bin, exist_ok=True)

def quote(s):
    return "'" + s.replace("'", "'\\''") + "'"

for e in scripts:
    module, _, attr = e.value.partition(":")
    attr = attr.split("[")[0].strip()
    code = ("import importlib, sys\n"
            "sys.dont_write_bytecode = True\n"
            "sys.path.insert(0, %r)\n"
            "sys.argv[0] = %r\n"
            "f = importlib.import_module(%r)\n"
            "for part in %r.split('.') if %r else []:\n"
            "    f = getattr(f, part)\n"
            "sys.exit(f())\n") % (lib, e.name, module.strip(), attr, attr)
    path = os.path.join(bin, e.name)
    with open(path, "w") as out:
        out.write("#!/bin/sh\nexec " + quote(python) + " -c " + quote(code) + ' "$@"\n')
    os.chmod(path, 0o755)

for program in others:
    shutil.copy2(os.path.join(moved, program), os.path.join(bin, program))

shutil.rmtree(moved, ignore_errors=True)
`

// VendorPortable reports whether the digest of what b vendors is the same on
// every platform. That needs a vendor step, and each one must run on every
// platform and be of a portable kind.
func VendorPortable(b *manifest.Build) bool {
	found := false

	for _, step := range b.Steps {
		if step.Vendor == nil {
			continue
		}

		if step.When != (platform.Selector{}) || !vendorKinds[*step.Vendor].portable {
			return false
		}

		found = true
	}

	return found
}

// CanCrossVendor reports whether oku can download what b vendors for platform
// p on a machine of another platform. That needs npm vendor steps and pip
// steps with a package alone, since npm and uv install for the platform they
// are told, and no command of the manifest before them, because a command for p
// may not run here.
func CanCrossVendor(b *manifest.Build, p platform.Platform) bool {
	last := -1

	for i, step := range b.Steps {
		if step.Vendor != nil && step.When.Matches(p) {
			if *step.Vendor != "npm" && (*step.Vendor != "pip" || step.Package == "") {
				return false
			}

			last = i
		}
	}

	for _, step := range b.Steps[:last+1] {
		if step.Run != nil && step.When.Matches(p) {
			return false
		}
	}

	return last >= 0
}

// vendorTarget returns the variables that make npm and uv install the packages
// of platform p and not those of the host.
func vendorTarget(p platform.Platform) []string {
	cpu := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[p.Arch]

	triple := map[string]string{
		"darwin": cpu + "-apple-darwin", "windows": cpu + "-pc-windows-msvc",
		"linux": cpu + "-unknown-linux-gnu",
	}[p.OS]
	if p.Libc == "musl" {
		triple = cpu + "-unknown-linux-musl"
	}

	return append(npmTarget(p), "OKU_PIP_PLATFORM="+triple)
}

// npmTarget returns the variables that make npm install the optional packages
// of platform p and not those of the host.
func npmTarget(p platform.Platform) []string {
	os, cpu := p.OS, p.Arch
	if os == "windows" {
		os = "win32"
	}

	if cpu == "amd64" {
		cpu = "x64"
	}

	env := []string{"npm_config_os=" + os, "npm_config_cpu=" + cpu}
	if p.Libc != "" {
		env = append(env, "npm_config_libc="+p.Libc)
	}

	return env
}

// BuildPin is what oku.lock holds for a build before a machine of its platform
// ran it.
type BuildPin struct {
	Impure bool
	// SourceURL and SHA256 are the source archive and its digest, or empty for a
	// git source. FirstUse reports that oku trusted the download.
	SourceURL string
	SHA256    string
	FirstUse  bool
}

// PinBuild returns the pin of m's build for platform p. It builds nothing. It
// takes the digest of a source archive from the manifest, else from its
// checksum file, else from pinned, which is the entry oku.lock holds, else from
// a download.
func (s *Store) PinBuild(
	ctx context.Context,
	m *manifest.Manifest,
	p platform.Platform,
	pinned BuildPin,
) (BuildPin, error) {
	var pin BuildPin

	for _, step := range m.Build.Steps {
		pin.Impure = pin.Impure || step.Impure() && step.When.Matches(p)
	}

	source := m.Build.Source
	if source.URL == "" {
		return pin, nil
	}

	vars := map[string]string{
		"version": m.Version.Value, "tag": m.Tag, "os": p.OS, "arch": p.Arch, "libc": p.Libc,
	}

	var err error
	if pin.SourceURL, err = manifest.Expand(source.URL, vars); err != nil {
		return pin, err
	}

	switch {
	case source.SHA256 != "":
		pin.SHA256 = source.SHA256
	case source.SHA256URL != "":
		var checksums string
		if checksums, err = manifest.Expand(source.SHA256URL, vars); err != nil {
			return pin, err
		}

		pin.SHA256, err = s.publishedSHA256(ctx, checksums, path.Base(pin.SourceURL))
	case pinned.SHA256 != "" && pinned.SourceURL == pin.SourceURL:
		pin.SHA256 = pinned.SHA256
	default:
		pin.FirstUse = true
		_, pin.SHA256, err = s.fetch(ctx, pin.SourceURL, "")
	}

	return pin, err
}

// hashTree returns one digest for every file under dir: its path, whether it is
// executable, and its content. A symlink counts by its target.
func hashTree(dir string) (string, error) {
	sum := sha256.New()

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			fmt.Fprintf(sum, "link %s -> %s\n", filepath.ToSlash(rel), target)

			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		content := sha256.New()
		if _, err := io.Copy(content, file); err != nil {
			return err
		}

		fmt.Fprintf(
			sum, "file %s %t %s\n",
			filepath.ToSlash(rel), info.Mode()&0o111 != 0, hex.EncodeToString(content.Sum(nil)),
		)

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", dir, err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// findTool returns the first of tools that is in one of the PATH directories of
// env.
func findTool(tools, env []string) (string, error) {
	var dirs []string

	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok {
			dirs = filepath.SplitList(value)
		}
	}

	// A Windows program has an extension and no mode that says it runs.
	names := func(tool string) []string { return []string{tool} }
	if runtime.GOOS == "windows" {
		names = func(tool string) []string { return []string{tool + ".exe", tool + ".cmd", tool + ".bat"} }
	}

	for _, tool := range tools {
		for _, dir := range dirs {
			for _, name := range names(tool) {
				candidate := filepath.Join(dir, name)

				info, err := os.Stat(candidate)
				if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf(
		"needs %s, which is not among the build's tools, add it to needs",
		strings.Join(tools, " or "),
	)
}
