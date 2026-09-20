# Manifest reference

A manifest is one TOML file that describes one package. Put it in your repo as
`oku.pkg.toml` and anyone can run `oku add github:you/repo`. A repo that holds
manifests for many packages names them `<name>.toml` or
`packages/<name>.toml`, and users add `github:you/repo#<name>`.

This page lists the keys oku reads today. Unknown keys are ignored, so a
manifest may already carry sections from the full design in
[`prd/architecture.md`](../prd/architecture.md).

## Example

```toml
[package]
name = "ripgrep"
description = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT"

[version]
value = "14.1.1"

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{version}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256_url = "https://github.com/BurntSushi/ripgrep/releases/download/{{version}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz.sha256"
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

| Key | Required | Meaning |
|---|---|---|
| `value` | yes | The version this manifest installs. |

`from`, for discovering versions from releases or tags, is not supported yet.
A manifest that sets it is rejected.

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

`url` and `sha256_url` expand `{{version}}`, `{{os}}`, `{{arch}}` and
`{{libc}}`. An unknown variable is an error.

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

## [build]

Building from source is not supported yet. When no artifact matches and the
manifest has a `[build]` table, oku says so instead of reporting that the
platform is unsupported.
