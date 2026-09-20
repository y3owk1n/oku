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

This page lists the keys oku reads today. `oku add` ignores other keys, so a
manifest may already hold sections from the full design in
[`prd/architecture.md`](../prd/architecture.md). `oku manifest lint` checks a
manifest against that full design.

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

## [version]

A manifest either fixes one version or discovers versions from upstream.
Discovery is the better choice for a published manifest, because a new release
needs no manifest change.

| Key | Meaning |
|---|---|
| `value` | The one version this manifest installs. |
| `from` | `github-releases` or `git-tags`. Not together with `value`. |
| `repo` | `owner/repo` for `github-releases`, a git URL for `git-tags`. Required with `from`. |
| `strip_prefix` | Text cut off the front of a tag to get the version, such as `"v"`. A tag without the prefix is ignored. |

```toml
[version]
from = "github-releases"
repo = "sharkdp/fd"
strip_prefix = "v"
```

How oku turns tags into versions:

- `github-releases` reads the newest 100 releases and skips drafts and
  prereleases. `git-tags` reads every tag with `git ls-remote`, so it needs
  `git` on `PATH`.
- After `strip_prefix`, a tag that does not start with a digit is ignored. That
  drops tags such as `nightly`.
- The newest version is the highest by its dot-separated numbers, so `1.10.0` is
  newer than `1.9.0`. A version with a `-` suffix, such as `2.0.0-rc1`, is older
  than `2.0.0`.

`oku add` installs the newest version, and `oku add <ref>@1.2.0` installs that
one. The user's `oku.lock` records the version and its tag, and `oku sync`
installs the locked version without asking upstream again.

## [[artifact]]

One table per prebuilt download. oku uses the first one whose `match` fits the
machine, so put specific entries before general ones.

| Key | Required | Meaning |
|---|---|---|
| `match` | no | `{ os, arch, libc }`. A missing key matches anything, and a missing `match` matches every machine. |
| `url` | yes | Where the download is. `https://`, `http://` or `file://`. |
| `sha256` | no | The download's digest, 64 lowercase hex characters. |
| `sha256_url` | no | A URL of a checksum file. Not together with `sha256`. |
| `strip` | no | How many leading path components to drop when unpacking. Default 0. |
| `bin` | see below | Paths of executables inside the package. |
| `man` | see below | Paths of man pages. The file name needs a section, such as `rg.1` or `rg.1.gz`. |
| `completions` | see below | Shell name to path, such as `{ fish = "complete/rg.fish" }`. |

Each artifact needs at least one of `bin`, `man` and `completions`.

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

tar, tar.gz, tar.bz2 and zip, recognised by content, not by file name.

A download that is none of these is taken as the executable itself. The
artifact must then list exactly one `bin` and nothing else, and the file is
installed under that name.

### Paths

`bin`, `man` and `completions` paths are relative to the unpacked package after
`strip`. A path that does not exist, is not a regular file, or points outside
the package fails the install. Two outputs with the same file name fail too.

### Checksums

In order, oku uses `sha256`, then the file at `sha256_url`, then the digest the
user's `oku.lock` pinned earlier. With none of them it trusts the first
download and pins it. Publish one of the first two. See
[Trust and checksums](trust.md).

A `sha256_url` file may hold a single digest, or `digest  filename` lines such
as `sha256sum` writes. oku picks the line that names the download's file.

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

## Inferred manifests

`oku add github:owner/repo` on a repo with no `oku.pkg.toml` writes a manifest
from the repo's newest release, prints it, and installs from it. To get that
manifest as a file you can edit and commit:

```
$ oku manifest init --from owner/repo
wrote oku.pkg.toml
```

How inference reads a release:

- It matches asset names to platforms by the words in them, listed in the table
  below.
- On Linux it writes the glibc build first and the musl build second with no
  `libc` in its `match`, so glibc machines with no build of their own use the
  musl build.
- It takes tar, tar.gz, tar.bz2 and zip archives and single binaries. It skips
  packages and formats oku cannot unpack, such as `.deb`, `.rpm`, `.msi`,
  `.dmg`, `.tar.xz` and `.tar.zst`. With several candidates it prefers a tar
  archive over a zip, then the shortest name.
- It uses `<asset>.sha256` as `sha256_url` when that exists, else a release file
  with `checksum` or `sha256sum` in its name. With neither, the package is
  [trusted on first use](trust.md#trust-on-first-use).
- The version starts at the first digit of the tag, and everything before it
  becomes `strip_prefix`. `v1.2.0` gives `"v"` and `jq-1.8.1` gives `"jq-"`.
- It downloads the asset for your machine and looks inside. The program is the
  executable named after the repo, else the only executable. A single top-level
  directory becomes `strip = 1`. Files ending in `.1` become `man`, and with
  more than 8 of them only the program's own page is kept.

| For | Words it looks for in an asset name |
|---|---|
| `linux` | `linux` |
| `darwin` | `darwin`, `macos`, `apple`, `osx`, `mac` |
| `windows` | `windows`, `win64`, `win` |
| `amd64` | `x86_64`, `amd64`, `x64` |
| `arm64` | `aarch64`, `arm64` |
| `glibc` | `gnu`, `glibc` |
| `musl` | `musl` |

Limits:

- The repo has no releases, or no asset for your machine. The error lists the
  asset names it saw.
- The archive holds several executables and none is named after the repo.
- It names the package after the repo, so `github:cli/cli` installs a package
  called `cli` whose program is `gh`. Commit a manifest to choose the name.
- It writes `bin` and `man` only. Completions need a manifest.

An inferred manifest is pinned like any other. `oku.lock` stores its full text,
so `oku sync` on another machine installs from the same text and does not infer
again. `oku update` infers again.

## Checking and updating a manifest

Run `oku manifest lint` before you publish. It is stricter than `oku add`. It
knows the whole schema and rejects a misspelt key. If your manifest fixes a
version with inline `sha256` values, `oku manifest bump` moves it to the newest
release and recomputes them. See [Commands](commands.md#oku-manifest-lint).

## [build]

Building from source is not supported yet. When no artifact matches and the
manifest has a `[build]` table, oku says so instead of reporting that the
platform is unsupported.
