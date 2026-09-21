# Manifest reference

A manifest is one TOML file that describes one package. Put it in your repo as
`oku.pkg.toml` and anyone can run `oku add github:you/repo`. A repo that holds
manifests for many packages names them `<name>.toml` or
`packages/<name>.toml`, and users add `github:you/repo#<name>`.

A repo or directory that holds many manifests is a collection. Users can give
it a short name with `oku source add` and search it, see
[Sources](refs.md#sources). Add a `description` to each manifest, because
`oku search` matches it.

You may not need one. A GitHub repo whose releases follow common naming is
installable with no manifest, see [Inferred manifests](#inferred-manifests).

This page lists every key. `oku add` ignores a key it does not know, so an older
oku still installs a manifest that was written for a newer one.
`oku manifest lint` is strict and reports unknown keys.

## Example

```toml
[package]
name = "ripgrep"
description = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT"

[version]
from = "github-releases"
repo = "BurntSushi/ripgrep"

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256_url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz.sha256"
strip = 1
bin = ["rg"]
man = ["doc/rg.1"]
completions = { fish = "complete/rg.fish", zsh = "complete/_rg" }
```

## [package]

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | Lowercase letters, digits, `.`, `_`, `-`. Starts with a letter or digit. |
| `description` | no | One line. |
| `homepage` | no | URL. |
| `license` | no | SPDX identifier. |
| `signing_key` | no | Your minisign public key. oku then checks every artifact against its signature, see [Signatures](#signatures). |
| `relocatable` | no | `true` when the built files contain no store path. A [cache](caches.md) then offers the package to machines with any store root. Default `false`. Only matters for `[build]`. |

## [version]

A manifest either fixes one version or discovers versions from upstream.
Discovery is the better choice for a published manifest, because a new release
needs no manifest change.

| Key | Meaning |
|---|---|
| `value` | The one version this manifest installs. |
| `from` | `github-releases`, `gitea-releases`, `gitlab-releases`, `git-tags` or `npm`. Not together with `value`. |
| `repo` | `owner/repo` for `github-releases`, or `host/owner/repo` on a GitHub Enterprise Server. `host/owner/repo` for `gitea-releases`, such as `codeberg.org/owner/repo`. `group/project` for `gitlab-releases`, or `host/group/project` on a GitLab server of your own. A git URL for `git-tags`. A package name for `npm`, such as `@scope/name`. Required with `from`. |
| `strip_prefix` | Text cut off the front of a tag to get the version, such as `"v"`. A tag without the prefix is ignored. |
| `tag` | One tag that upstream moves, such as `"nightly"`. Only with `github-releases`, `gitea-releases` or `gitlab-releases`, and not together with `strip_prefix`. See [A moving tag](#a-moving-tag). |

```toml
[version]
from = "github-releases"
repo = "sharkdp/fd"
strip_prefix = "v"
```

How oku turns tags into versions:

- `npm` reads every version of the package from `registry.npmjs.org` and skips
  a prerelease, which has a `-` in its version. An npm version has no tag, so
  `{{tag}}` equals `{{version}}` and `strip_prefix` does not apply. The
  registry publishes a sha512 for each version's download. When the artifact's
  `url` is that download and the manifest has no checksum of its own, oku
  checks the download against it.
- `github-releases` reads the newest 100 releases and skips drafts and
  prereleases. `gitea-releases` does the same for the newest 50 on a Gitea or
  Forgejo server. `gitlab-releases` reads the newest 100 and skips a release
  dated in the future, which GitLab calls upcoming. `git-tags` reads every tag with `git ls-remote`, so it needs
  `git` on `PATH`.
- After `strip_prefix`, a tag that does not start with a digit is ignored. That
  drops tags such as `nightly`.
- The newest version is the highest by its dot-separated numbers, so `1.10.0` is
  newer than `1.9.0`. A version with a `-` suffix, such as `2.0.0-rc1`, is older
  than `2.0.0`.

`oku add` installs the newest version, and `oku add <ref>@1.2.0` installs that
one. The user's `oku.lock` records the version and its tag, and `oku sync`
installs the locked version without asking upstream again.

### A moving tag

Some projects publish every nightly build under one tag and replace its files
each time. Name that tag, and oku follows it:

```toml
[version]
from = "github-releases"
repo = "neovim/neovim"
tag = "nightly"

[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://github.com/neovim/neovim/releases/download/{{tag}}/nvim-macos-arm64.tar.gz"
strip = 1
bin = ["bin/nvim"]
```

- oku reads that one release and accepts it when it is a prerelease. A draft
  fails.
- Each build is its own version, `<date>-<commit>`, such as
  `2026.09.20-a73243f`. Both parts come from the commit the tag points at, its
  day in UTC and the start of its hash, so one commit always has one version.
  `{{tag}}` stays `nightly`.
- `oku update` moves the package when the tag points at another commit. Every
  build has its own store path, so `oku rollback` returns to the earlier build
  without a download.
- When the artifact has no `sha256` and no `sha256_url`, oku checks the download
  against the sha256 that the GitHub API reports for that file. `url` must be
  the file's GitHub download URL for that, and `oku manifest lint` warns when
  it is not. Gitea, Forgejo and GitLab report no sha256 for a file, so with
  `gitea-releases` or `gitlab-releases` the user trusts the first download, and
  `lint` warns about that too.
- A `[build]` whose `source` clones `{{tag}}` fails when the clone is not at the
  commit of the version.

Upstream deletes the old build when it moves the tag. `oku sync` on a machine
that lacks the locked build can therefore only download it until then.
Afterwards it stops, and installs no newer build under the locked version:

```
oku: nvim: upstream moved the tag nightly to 2026.09.21-0c1f2aa since oku.lock was written, and the locked build 2026.09.20-a73243f is gone
run `oku update nvim` to take the new build
```

`oku add <ref>@2026.09.20-a73243f` works only while upstream is at that build.
A [cache](caches.md) does not keep old builds, because it holds no plain
downloads.

## [[artifact]]

One table per prebuilt download. oku uses the first one whose `match` fits the
machine, so put specific entries before general ones.

| Key | Required | Meaning |
|---|---|---|
| `match` | no | `{ os, arch, libc }`. A missing key matches anything, and a missing `match` matches every machine. |
| `url` | yes | Where the download is. `https://`, `http://` or `file://`. |
| `sha256` | no | The download's digest, 64 lowercase hex characters. |
| `sha256_url` | no | A URL of a checksum file. Not together with `sha256`. |
| `integrity` | no | A sha512 digest the way npm publishes it, `sha512-` and the digest in base64. oku checks the download against it. |
| `strip` | no | How many leading path components to drop when unpacking. Default 0. |
| `bin` | see below | Paths of executables inside the package. An entry may also be a table that makes oku write the program, see [A program that needs an interpreter](#a-program-that-needs-an-interpreter). |
| `man` | see below | Paths of man pages. The file name needs a section, such as `rg.1` or `rg.1.gz`. |
| `completions` | see below | Shell name to path, such as `{ fish = "complete/rg.fish" }`. |
| `app` | see below | macOS app bundles, such as `["Foo.app"]`. See [Apps and fonts](#apps-and-fonts). |
| `font` | see below | Font files, such as `["fonts/ttf/Foo-Regular.ttf"]`. |

Each artifact needs at least one of `bin`, `man`, `completions`, `app` and
`font`.

### A program that needs an interpreter

Some packages ship a script and no executable, such as a Node or Python tool or
a `.jar`. A `bin` entry that is a table makes oku write the program. It runs `run` with
`args` in front of the user's own arguments.

```toml
[runtime]
deps = ["github:someone/recipes#node"]

[[artifact]]
url = "https://registry.npmjs.org/@actions/languageserver/-/languageserver-{{version}}.tgz"
sha256 = "d152725064c64f862da5158cd630d4c67973edfb58bdabaa44054ffef03b9d03"
strip = 1
bin = [{ name = "gh-actions-language-server", run = "{{dep.node.prefix}}/bin/node", args = ["{{pkg}}/bin/actions-languageserver"] }]
```

| Key | Meaning |
|---|---|
| `name` | The program's name in the user's profile. It need not match any file in the download. |
| `run` | The program to run, as an absolute path. |
| `args` | Arguments that go before the user's. Optional. |

`run` and `args` expand the [template variables](#template-variables) and:

| Variable | Value |
|---|---|
| `{{pkg}}` | The directory that holds the unpacked download. |
| `{{prefix}}` | The package's directory in the store. `{{pkg}}` is `{{prefix}}/pkg`. |
| `{{dep.<name>.prefix}}` | The store directory of a [runtime dep](#dependencies), by its package name. A dep's programs are in its `bin`. |

The interpreter is a runtime dep, so the user does not need it on `PATH`, and it
does not appear there either. The package stays a plain download. oku runs no
build and asks for no approval.

On macOS and Linux the program is a shell script that ends in `exec`. On Windows
it is a [shim](windows.md#shims) that holds the arguments, so `run` names an
`.exe` there. Use one `[[artifact]]` per OS when the paths differ. A `bin` list
may mix paths and tables.

### match values

`os` and `arch` use Go's names: `linux`, `darwin`, `windows` and `amd64`,
`arm64`. `libc` is `glibc` or `musl` and only applies to Linux. oku reports
`musl` when `/lib/ld-musl-*.so.1` exists.

### Template variables

`url` and `sha256_url` expand these variables. An unknown variable is an error.

| Variable | Value |
|---|---|
| `{{version}}` | The version being installed, such as `10.2.0`. |
| `{{tag}}` | The upstream tag of that version, such as `v10.2.0`. With a fixed version it equals `{{version}}`. |
| `{{os}}`, `{{arch}}`, `{{libc}}` | The machine, with the same values `match` uses. |

Release URLs often hold the tag in one place and the version in another:

```toml
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-aarch64-apple-darwin.tar.gz"
```

### Downloads oku can unpack

oku recognises a download by its content, not by its file name.

| Format | Notes |
|---|---|
| tar, tar.gz, tar.bz2, tar.xz, tar.zst | |
| zip | |
| 7z | An archive made on Windows has no unix file modes. A program from it still runs, because oku marks every `bin` as executable. |
| `.deb` | oku unpacks only the data archive. Its files are at `usr/bin/...`. |
| `.rpm` | oku unpacks only the file payload. Its files are at `usr/bin/...`. |
| `.dmg` | macOS only. oku mounts the image read-only, copies it, and unmounts it. |
| `.pkg` | macOS only. Its files are at `<component>.pkg/Payload/...`. |
| `.msi` | Windows only. oku runs `msiexec /a`, the administrative install. It copies the files out and skips the install sequence, so it writes no registry entries, services or shortcuts. Its files are at paths such as `Program Files/<product>/...`. |
| anything else | oku treats it as the executable itself. This covers a plain binary and an AppImage. |

oku unpacks installers and never runs them. It never executes a `.deb`'s
maintainer scripts, an `.rpm`'s scriptlets, a `.pkg`'s install scripts or an
`.msi`'s install sequence, so a package that depends on its post-install script
will not work from oku. There is one exception. An `.msi` can define actions
for the administrative install itself, and `msiexec /a` runs those. Few packages
define any.

For a download that is the executable itself, the artifact must list exactly one
`bin` and nothing else, and the file is installed under that name.

`strip` applies to tar, zip, 7z, deb and rpm. oku copies a `.dmg`, a `.pkg` and an
`.msi` whole. From a `.dmg` it leaves out the hidden Finder files and any link that
points out of the image, such as the shortcut to `/Applications`.

To find the paths inside an installer, run `oku manifest test --keep` and look
in the `pkg/` directory of the store path it prints.

### Paths

`bin`, `man` and `completions` paths are relative to the unpacked package after
`strip`. A path that does not exist, is not a regular file, or points outside
the package fails the install. Two outputs with the same file name fail too.

### Checksums

In order, oku uses `sha256`, then the file at `sha256_url`, then the digest the
user's `oku.lock` pinned earlier. It also checks `integrity` when the artifact
has one, or when the npm registry publishes one for the download. With none of
them it trusts the first download and pins it. Publish `sha256`, `sha256_url` or
`integrity`. See [Trust and checksums](trust.md).

`oku manifest hash <url>` prints the `sha256` and `integrity` lines for a
download.

A `sha256_url` file may hold a single digest, or `digest  filename` lines such
as `sha256sum` writes. oku picks the line that names the download's file.

### Signatures

A checksum in the manifest protects the download, but not the manifest. A
signing key lets users notice when someone else publishes under your name:

```toml
[package]
name = "foo"
signing_key = "RWTr8koGkq7wBGTVdGedU8b7CkkiIu+LBp6uQLJ7ltH/7iGXMKNF1b27"
```

Create the key and sign each release file with
[minisign](https://jedisct1.github.io/minisign/):

```sh
minisign -G                           # once, writes minisign.pub and the secret key
minisign -S -m foo-1.2.0-linux.tar.gz # writes foo-1.2.0-linux.tar.gz.minisig
```

Upload each `.minisig` beside its file. oku downloads the artifact's URL with
`.minisig` appended and refuses the artifact when the signature is missing or
not from `signing_key`. It accepts the current and the legacy (`-l`) kind of
signature. With a signing key, an artifact without `sha256` is no longer trust
on first use, and `oku manifest lint` does not warn about it.

The user's `oku.lock` pins the key at the first install. After that, oku refuses
a manifest with another key, or with none, until the user passes `--accept-key`.
Tell your users before you change the key.

`signing_key` covers artifacts. It does not sign the sources of a `[build]`.

## What a package looks like once installed

oku keeps the whole unpacked download, so an executable that needs files next
to it keeps working. `bin`, `man` and `completions` only choose what gets
linked into the user's profile:

| Key | Linked at |
|---|---|
| `bin = ["dir/tool"]` | `bin/tool` |
| `man = ["doc/tool.1"]` | `share/man/man1/tool.1` |
| `completions = { fish = "c/tool.fish" }` | `share/completions/fish/tool.fish` |

## Archive safety

oku refuses an archive entry that is absolute or contains `..`, and a symlink
whose target is absolute or resolves outside the package.

## Apps and fonts

A package can ship a desktop app and fonts. oku copies them to where the user's
OS looks for them, and removes them again when the package is removed.

```toml
[[artifact]]
match = { os = "darwin" }
url = "https://github.com/rxhanson/Rectangle/releases/download/{{tag}}/Rectangle{{version}}.dmg"
app = ["Rectangle.app"]
```

```toml
[[artifact]]
url = "https://github.com/JetBrains/JetBrainsMono/releases/download/{{tag}}/JetBrainsMono-{{version}}.zip"
font = ["fonts/ttf/JetBrainsMono-Regular.ttf", "fonts/ttf/JetBrainsMono-Bold.ttf"]
```

| Key | macOS | Linux | Windows |
|---|---|---|---|
| `app = ["Foo.app"]` | oku copies the bundle to `~/Applications/Foo.app`. | Not used. | Not used. |
| `[[app]]` | Not used. | A desktop entry at `<data home>/applications/oku-<name>.desktop`. | A Start Menu shortcut, `oku-<name>.lnk`. |
| `font = [...]` | oku copies them to `~/Library/Fonts/`. | oku copies them to `<data home>/fonts/oku/`. | oku copies them to the user's font folder and names them in the registry. |

`<data home>` is `$XDG_DATA_HOME`, or `~/.local/share`. The Windows paths are in
[Windows](windows.md#apps-and-fonts).

On Linux and Windows an app is a program plus a launcher. Ship the program with `bin` and
describe the launcher in a `[[app]]` table at the top level of the manifest:

```toml
[[app]]
name = "Foo"
exec = "bin/foo"
icon = "share/icons/foo.png"
```

`name` and `exec` are required. `exec` and `icon` are paths inside the installed
package, so `exec` is normally `bin/<program>`. Windows ignores `icon`, because a
shortcut shows the icon of its program.

oku copies apps and fonts, because Finder, Spotlight and font services do not
treat a symlink as installed. A macOS bundle keeps its code signature. oku
refuses to overwrite a file that it did not place, and the install fails with a
message that names the file.

Apps and fonts come from the user's global list only. A package in a
[project](projects.md) installs its programs, and oku says that its apps and
fonts were skipped.

A `[build]` can install them too, with `install = { app = [...], font = [...] }`.

## [[service]]

A long-running program that the user's OS service manager can run, see
[Services](services.md). A package may have several.

```toml
[[service]]
name = "postgres"
command = "bin/postgres"
args = ["-D", "{{prefix}}/share/postgres/data"]
env = { PGPORT = "5432" }
restart = "on-failure"
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | What the user types in `oku service start <name>`. Same characters as a package name. Two installed packages cannot ship a service of the same name. |
| `command` | yes | A path inside the installed package, normally `bin/<program>`. |
| `args` | no | Arguments. They expand `{{prefix}}` and `{{version}}`. |
| `env` | no | Variables for the service. Values expand the same variables. |
| `restart` | no | `never`, `on-failure` or `always`. Default `never`. |

The program must stay in the foreground and must not fork into the background.
The service manager starts it, watches it, and restarts it according to
`restart`. What it prints goes to a log file on macOS and to the user journal on
Linux.

Installing a package never starts its service. The user turns it on with
`service = true` in their list.

## [env]

Variables the package needs in the user's shell. The user's
[shell hook](getting-started.md#set-up-your-shell) exports them while the
package is installed: in every shell for a globally installed package, and
inside the project for a project package.

```toml
[env]
JAVA_HOME = "{{pkg}}/lib/jvm"
```

Values expand `{{version}}`, `{{tag}}` and:

| Variable | Value |
|---|---|
| `{{pkg}}` | The directory that holds the package's files. For a prebuilt artifact that is the unpacked download, and for a build it is what the build installed. |
| `{{prefix}}` | The package's directory in the store. A build's files are in it. An artifact's files are in `{{prefix}}/pkg`, so use `{{pkg}}` for a path that works either way. |

A package may not set a variable that changes how other programs load or run.
oku rejects `PATH`, `HOME`, `SHELL`, `USER`, `IFS`, `ENV`, `BASH_ENV`, `PS1`,
`PROMPT_COMMAND`, and any name that starts with `LD_`, `DYLD_` or `OKU_`. When
two packages set the same variable, the one whose name sorts last is used.

## Inferred manifests

`oku add github:owner/repo` on a repo with no `oku.pkg.toml` writes a manifest
from the repo's newest release, prints it, and installs from it. A `codeberg:`
or `gitea:` ref works the same way, and its manifest gets
`from = "gitea-releases"`. A `gitlab:` ref gets `from = "gitlab-releases"`, and
its assets are the links of the release, not the source archives GitLab adds.

With `@version` it reads that version's release. It tries the tag `version`,
then `v<version>`. With any other tag prefix it reads the newest release.

To get that
manifest as a file you can edit and commit:

```
$ oku manifest init --from owner/repo
wrote oku.pkg.toml
```

How inference reads a release:

- It matches asset names to platforms by the words in them, listed in the table
  below.
- On macOS an asset for your arch comes before a universal one. `gnu` names a
  libc on Linux only, so `x86_64-pc-windows-gnu` is a Windows asset.
- On Linux it writes the glibc build first and the musl build second with no
  `libc` in its `match`, so glibc machines with no build of their own use the
  musl build.
- It takes tar archives in any compression oku knows, zip and 7z archives, and
  single binaries, compressed or not. It skips editor extensions (`.vsix`) and
  installers such as `.deb`, `.rpm`, `.msi`, `.dmg` and
  `.pkg`, because the paths inside them cannot be guessed. With several
  candidates it prefers a tar archive over a zip, then the shortest name. When
  other assets fit your machine as well, a comment in the manifest lists them.
- It uses `<asset>.sha256` as `sha256_url` when that exists, else a release file
  with `checksum` or `sha256sum` in its name that is not a signature. With neither, the package is
  [trusted on first use](trust.md#trust-on-first-use).
- The version starts at the first digit of the tag, and everything before it
  becomes `strip_prefix`. `v1.2.0` gives `"v"` and `jq-1.8.1` gives `"jq-"`.
- It downloads the asset for your machine and looks inside. It opens one asset
  per archive ending, so a Windows zip gets its own layout and the tar archives
  share one. It leaves out a platform whose asset it cannot read. The program is
  the executable named after the repo, else the only executable. In an archive
  with no executable files, which is what a zip made on Windows is, it is the
  file named after the repo. A single top-level
  directory becomes `strip = 1`. Files ending in `.1` become `man`, and with
  more than 8 of them only the program's own page is kept.

| For | Words it looks for in an asset name |
|---|---|
| `linux` | `linux` |
| `darwin` | `darwin`, `macos`, `macosx`, `apple`, `osx`, `mac` |
| `windows` | `windows`, `win64`, `win` |
| `amd64` | `x86_64`, `x86-64`, `amd64`, `x64` |
| `arm64` | `aarch64`, `arm64` |
| `386` | `i386`, `i686`, `386` |
| `arm` | `armv7`, `armv7l`, `armhf`, `arm` |
| `riscv64` | `riscv64` |
| any arch, on `darwin` | `universal`, `universal2`, `all` |
| `glibc` | `gnu`, `glibc` |
| `musl` | `musl` |

When inference picks the wrong asset or fails, name the asset and the program:

```
$ oku add github:owner/repo --asset 'tool-*-macos.zip' --bin tool-cli
```

`--asset` is a glob that must name exactly one asset of the release, and oku
uses that asset for your machine. `--bin` is the file name of the program inside
the assets. Both apply to an inferred manifest only. `oku add` fails when you
pass them for a ref that has a manifest.

### An npm package

`oku add npm:@scope/name` infers a manifest from the npm registry. It has
`version.from = "npm"`, the registry's download as its artifact, and one
[`bin` table](#a-program-that-needs-an-interpreter) for each program in the
package's `bin`. See [npm packages](refs.md#npm-packages) for where node comes
from and which packages work.

### A URL of the download

`oku add https://host/tool-1.2.3-linux-amd64.tar.gz` installs from a URL that is
the package itself. A URL whose path ends in `.toml` is always a manifest. oku
reads any other URL as a manifest first, and takes it as the download when it
does not parse as one or is larger than 1 MiB.

The inferred manifest has one artifact, for the OS and arch of the machine that
ran `oku add`. `oku sync` on another kind of machine fails with "no artifact".
The package name is the file name up to its version, and the version is the
first `1.2.3` in the file name. A file name with no version gives the version
`0`. The version is fixed, so `oku update` never changes it. To get a newer
version, add that version's URL.

oku finds the program the way it does in a release asset, and `--bin` names it
when that fails. `--asset` does not apply. A URL on its own has no checksum, so
oku [trusts the download on first use](trust.md#trust-on-first-use) and pins
its digest in `oku.lock`.

Limits:

- The repo has no releases, or no asset for your machine. The error lists the
  asset names it saw. Pass one to `--asset`.
- The archive holds several executables and none is named after the repo. Pass
  the right one to `--bin`.
- It names the package after the repo, so `github:cli/cli` installs a package
  called `cli` whose program is `gh`. Commit a manifest to choose the name.
- It writes `bin` and `man` only. Completions need a manifest.

An inferred manifest is pinned like any other. `oku.lock` stores its full text,
so `oku sync` on another machine installs from the same text and does not infer
again. `oku update` infers again.

## Checking and updating a manifest

Run `oku manifest lint` and `oku manifest test` before you publish.

`lint` is stricter than `oku add`. It knows the whole schema and rejects a
misspelt key. `test` installs the manifest into a throwaway store on your
machine, and builds it from source when it has a `[build]`.

If your manifest fixes a version with inline `sha256` values,
`oku manifest bump` moves it to the newest release and recomputes them. See
[Commands](commands.md#oku-manifest-lint).

## [build]

`[build]` says how to produce the package from source. oku builds when no
`[[artifact]]` fits the machine, or when the user passes `--from-source`. A
manifest may have both. oku then uses an artifact when one fits, because that
needs no build.

```toml
[build]
needs = ["cc", "make"]
source = { git = "https://github.com/Old-Man-Programmer/tree", tag = "{{tag}}" }

[[build.step]]
run = "make -j{{jobs}}"
shell = "sh"

[[build.step]]
install = { bin = ["tree"], man = ["doc/tree.1"] }
```

| Key | Meaning |
|---|---|
| `needs` | Tools that must be on the user's `PATH`, such as `cc` or `cargo`. oku checks them before any step runs and never installs them. |
| `source` | `{ git, tag }` clones that tag at depth 1 and needs `git`. `{ url, sha256, strip }` downloads and unpacks an archive, and `sha256` is required. Without `source` the build starts in an empty directory. |
| `deps` | Other oku packages the build uses, see [Dependencies](#dependencies). |

### Steps

Steps run in order, in the source directory. Each `[[build.step]]` sets exactly
one of these keys:

| Key | What it does |
|---|---|
| `run = "..."` | Runs a command string in a shell. |
| `install = { bin, lib, include, man, share, completions, app, font }` | Copies files from the source directory into the package. `bin` files become executable. `man` and `completions` go where an artifact's would. |
| `copy = { from, to }` | Copies one file. `from` is relative to the source directory and `to` to the package. |
| `patch = { file, strip }` | Applies a unified diff to the source, see below. |
| `fetch = { url, sha256, to }` | Downloads a file into the source directory. `sha256` is required. |
| `extract = { file, to, strip }` | Unpacks an archive that is in the source directory. |
| `vendor = "go"` | Downloads the language's packages with the network on, see [Vendoring](#vendoring). |

A `patch` step applies a unified diff to the source:

```toml
[[build.step]]
fetch = { url = "https://example.com/fix-build.patch", sha256 = "...", to = "fix.patch" }
[[build.step]]
patch = { file = "fix.patch" }
```

`file` is a path in the source directory, so a `fetch` step or the source itself
provides it. `strip` removes that many leading directories from the file names
in the diff, as `-p` does for the `patch` program. It defaults to 0:

| The diff comes from | `strip` |
|---|---|
| `git diff` or `git format-patch` | 0. oku leaves out the `a/` and `b/` prefixes of a diff with a `diff --git` line. |
| `diff -ur old new` | 1, which removes `old/` and `new/`. |
| a diff with `a/` and `b/` prefixes but no `diff --git` line | 1 |

A patch can change, create, delete and rename files, and change a file's mode.
oku applies it in Go and never calls the `patch` program, so the step works the
same on Windows. Every hunk has to fit exactly. oku does not apply a hunk at an
offset or with fuzz, and a hunk that does not fit fails the build and names the
file. Line endings count. A patch with Windows line endings fits a source that
has them too, and not one with unix line endings.

Any step may also set:

| Key | Meaning |
|---|---|
| `when = { os, arch, libc }` | The step only runs on a matching machine. |
| `shell` | For `run`: `sh`, `bash`, `pwsh` or `cmd`. Default `sh`, except on Windows, which has no default. `oku manifest lint` requires `shell` on every `run` step that can reach Windows. |
| `env = { KEY = "value" }` | Extra variables for `run`. Values expand template variables. |

`sh` and `bash` run with `-e`, so the step fails at the first failing command.

### Template variables in a build

`run`, `env` values, `source.tag`, `source.url` and `fetch.url` expand
`{{version}}`, `{{tag}}`, `{{os}}`, `{{arch}}`, `{{libc}}` and:

| Variable | Value |
|---|---|
| `{{prefix}}` | The package's final directory in the store. Pass it to `make install PREFIX={{prefix}}` or `./configure --prefix={{prefix}}`. |
| `{{src}}` | The source directory. |
| `{{jobs}}` | The number of CPUs. |

A `run` step may write into `{{prefix}}` itself, so a project with a working
`make install` needs no `install` step. A build that leaves `{{prefix}}` empty
fails.

### Dependencies

A dep is another oku package, given as a [ref](refs.md). `build.deps` are
installed before the build and are visible to it. `runtime.deps` are installed
with the package, for prebuilt artifacts too.

```toml
[build]
deps = [
  "github:someone/recipes#zlib",
  { ref = "github:someone/recipes#openssl", version = ">=3, <4" },
]

[runtime]
deps = ["github:someone/recipes#ca-certificates"]
```

A dep is a ref string, or a table with `ref` and an optional `version`
constraint. A constraint is one or more comma-separated parts, and all must
hold. A part is `>=`, `>`, `<=`, `<` or `=` followed by a version, and a bare
version means `=`. oku picks the newest version that satisfies it. When none
does, the error names the constraint and the versions it found.

A relative file ref starts at the directory of the manifest that names it. A
manifest that came from a URL or a repo cannot depend on a local path. A
dependency cycle is an error.

Deps are not linked into the user's profile, so their programs are not added
to the user's `PATH`. Each package gets its own deps, and two packages may use
different versions of the same one. `oku why <name>` shows what uses a dep.

For every build dep, a `run` step gets these variables, so compilers, linkers,
pkg-config and cmake find it with no flags in your manifest:

| Variable | Holds, for each dep |
|---|---|
| `PATH` | `<dep>/bin`, ahead of the `needs` tools |
| `CPATH` | `<dep>/include` |
| `LIBRARY_PATH` | `<dep>/lib` |
| `LD_RUN_PATH` | `<dep>/lib`. The GNU linker records it, so the result finds the dep's shared libraries at runtime. |
| `PKG_CONFIG_PATH` | `<dep>/lib/pkgconfig` and `<dep>/share/pkgconfig` |
| `CMAKE_PREFIX_PATH` | `<dep>` |

`{{dep.<name>.prefix}}` expands to a dep's directory when you need the path
itself.

On macOS the linker has no `LD_RUN_PATH`. A shared library must record its own
absolute install name, which cmake and autotools do when they are given
`{{prefix}}`. With a bare compiler call it is
`cc -dynamiclib -install_name {{prefix}}/lib/libfoo.dylib ...`.

A package's store path depends on the deps it was built against, so a new dep
version leads to a new build instead of changing an installed package.

### Vendoring

`run` steps have no network, so `go build` or `cargo build` cannot download
packages. A `vendor` step does that download for them. It runs the language's
own tool with the network on, and oku hashes everything it downloaded.

```toml
[build]
needs = ["go", "git"]
source = { git = "https://github.com/rakyll/hey", tag = "{{tag}}" }

[[build.step]]
vendor = "go"

[[build.step]]
run = "go build -mod=vendor -o hey ."
shell = "sh"
env = { GOTOOLCHAIN = "local", GOFLAGS = "-buildvcs=false" }

[[build.step]]
install = { bin = ["hey"] }
```

| `vendor` | Runs | Fills | Then build with |
|---|---|---|---|
| `"go"` | `go mod vendor` | `vendor/` | `go build -mod=vendor` |
| `"cargo"` | `cargo vendor --locked vendor`, and points `.cargo/config.toml` at it | `vendor/` | `cargo build --offline --locked` |
| `"npm"` | `npm ci --ignore-scripts` | `node_modules/` | the project as it is |
| `"pip"` | `pip download -r requirements.txt -d vendor/pip` | `vendor/pip/` | `pip install --no-index --find-links vendor/pip` |

The tool must be in `needs`, or come from a dep. `cargo` and `npm` need the
project's lockfile, and `pip` needs `requirements.txt`. `pip` uses `pip3` when
there is no `pip`.

The user's `oku.lock` pins one digest for what the vendor steps downloaded.
`oku sync` runs them again and fails if the digest differs, so a locked build
cannot get different packages. `oku update` accepts the new digest. What `pip`
and `npm` download depends on the platform, and the lock keeps one digest per
platform.

A vendor step runs a program, so it appears in the approval prompt. It does not
make the package impure, because its output is checked.

A `cargo` that rustup manages works in the sandbox. oku passes `RUSTUP_HOME`
through and lets the build read that directory.

### The build environment

A `run` step does not see the user's environment. It gets:

- `PATH` with each dep's `bin`, the directories of the `needs` tools, then
  `/usr/bin` and `/bin`
- the [dependency variables](#dependencies) above
- `HOME` and `TMPDIR` pointing at empty temporary directories
- `OKU_PREFIX`, `OKU_SRC`, `OKU_JOBS`, and the step's `env`

A tool the build uses must therefore be in `needs`.

### The sandbox

On macOS and Linux a `run` step runs in a sandbox:

- It has no network.
- It cannot read the user's home directory. The store and the directories of
  the `needs` tools stay readable, even when they are inside it.
- On macOS it can only write to the source directory, its temporary `HOME` and
  `TMPDIR`, and `{{prefix}}`.

So a build must get everything it downloads through `source`, a `fetch` step, or
a [`vendor` step](#vendoring). oku checks all three against a digest.

`network = true` on a `run` step gives that step the network and nothing else.
oku shows it in the approval prompt as `(wants network)`, and marks the package
`impure` in the store, in `oku.lock` and in `oku info`. Use it only when a checksummed `fetch`
is not enough.

```toml
[[build.step]]
run = "npm ci"
shell = "sh"
network = true
```

A `needs` tool that is installed in the user's home directory and loads files
from elsewhere in it cannot read them in the sandbox. rustup is the one case oku
handles. For anything else, depend on a toolchain package or on a system-wide
install.

oku uses `sandbox-exec` on macOS and user, mount and network namespaces on
Linux. On a Linux host that forbids unprivileged user namespaces, which
includes a default Docker container and a default Ubuntu 24.04, and on Windows,
the step runs with the
scrubbed environment only. oku then prints a warning that the build could use
the network and read the user's files.

### When a build fails

oku reports the step number, its kind, and the last 40 lines of its output,
deletes the half-built package, and leaves the user's profile as it was.
`oku add -v` shows the output while the build runs.

### Approval

Before the first build of a manifest that has `run` steps, oku shows the user
those commands and asks, see [Trust and checksums](trust.md#build-commands).
