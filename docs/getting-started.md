# Getting started

## Install oku

One line installs oku for your user, with no root:

```sh
curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
```

The script downloads the binary for your OS and CPU from the newest GitHub
release, checks its sha256 against the release's `checksums.txt`, and puts it in
`~/.local/bin`, or `%LOCALAPPDATA%\oku\bin` on Windows. On unix it also checks
the minisign signature when `minisign` is installed. It edits no file of yours.
It ends by printing the directory to add to `PATH` and the hook line for your
shell.

| Variable | Effect |
|---|---|
| `OKU_INSTALL_DIR` | Where the binary goes. |
| `OKU_VERSION` | A release tag such as `v0.1.0`. Default is the newest release. |

`oku self update` replaces the binary later, see
[Commands](commands.md#oku-self-update). `oku doctor` checks the whole setup
and says what to fix, see [Commands](commands.md#oku-doctor).

## Build oku

You only need this to work on oku, or for a platform without a release.

You need Go 1.26.4 or newer.

```
git clone https://github.com/y3owk1n/oku
cd oku
CGO_ENABLED=0 go build -o oku ./cmd/oku
```

`CGO_ENABLED=0` makes one static binary with no C library dependency.

Move the `oku` binary somewhere on your `PATH`, for example `~/.local/bin`.

The repo also has a `justfile`. `just build` writes `bin/oku` with the git
version stamped in.

## Install a package

Point oku at a GitHub repo:

```
$ oku add github:BurntSushi/ripgrep
github:BurntSushi/ripgrep has no manifest, so oku inferred this one from its newest release:

[package]
name = "ripgrep"
...
added ripgrep 15.2.0
add /home/you/.local/share/oku/profiles/global/current/bin to PATH to run it
```

The ripgrep repo has no oku manifest. oku read its newest release, matched the
release files to operating systems and CPU types, found the published
checksums, and opened the download to find the program inside. It printed the
manifest it wrote before it installed anything. See
[Inferred manifests](manifest.md#inferred-manifests) for when this works.

To pick a version, add `@version`:

```
$ oku add github:BurntSushi/ripgrep@14.1.1
```

## Write a manifest yourself

A manifest is a small TOML file that says where a package's downloads are.
Write one when inference gets a package wrong, or to install something that is
not on GitHub. Save this as `ripgrep.toml`:

```toml
[package]
name = "ripgrep"

[version]
from = "github-releases"
repo = "BurntSushi/ripgrep"

[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-aarch64-apple-darwin.tar.gz"
sha256_url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-aarch64-apple-darwin.tar.gz.sha256"
strip = 1
bin = ["rg"]

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256_url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz.sha256"
strip = 1
bin = ["rg"]
```

Then:

```
$ oku add ./ripgrep.toml
added ripgrep 15.2.0
```

The [manifest reference](manifest.md) lists every key.

## Put the profile on PATH

oku links every installed binary into one directory. Add it to `PATH` once, in
your shell's config file. oku never edits that file for you.

```sh
# bash, zsh
export PATH="$HOME/.local/share/oku/profiles/global/current/bin:$PATH"
```

```fish
# fish
fish_add_path ~/.local/share/oku/profiles/global/current/bin
```

```powershell
# PowerShell on Windows, once
$bin = "$env:LOCALAPPDATA\oku\profiles\global\current\bin"
[Environment]::SetEnvironmentVariable('Path', "$bin;" + [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')
```

To use [projects](projects.md), also load the oku hook there.
[Projects](projects.md#using-the-projects-programs) has the line for bash, zsh,
fish and PowerShell.

If you set `XDG_DATA_HOME`, the directory is
`$XDG_DATA_HOME/oku/profiles/global/current/bin`. `oku add` prints the exact
path whenever it is missing from `PATH`.

```
$ rg --version
ripgrep 15.2.0
```

## See and remove what is installed

```
$ oku list
ripgrep  15.2.0  github:BurntSushi/ripgrep

$ oku remove ripgrep
removed ripgrep
```

## Undo a change

Every change to the installed packages is a numbered generation.

```
$ oku update
ripgrep 14.1.1 -> 15.2.0
$ oku rollback
generation 1 is active: ripgrep 14.1.1
```

See [Commands](commands.md#oku-rollback).

## Move to another machine

`oku add` records every package in `~/.config/oku/oku.toml` and pins what it
resolved in `~/.config/oku/oku.lock`. Commit both files to a repo. On a new
machine, one command restores the same versions:

```
$ oku sync github:you/machines
adopted github:you/machines with 1 locked package
profile now holds 1 package
```

Use URL or repo refs in a list you publish. A ref such as `./ripgrep.toml` only
works on the machine that has that file.

See [List and lock](list-and-lock.md#a-new-machine).

## Uninstall oku

```
$ oku self uninstall
```

It lists what it will delete, asks once, and removes every package, its own
directories and the `oku` binary. `--keep-list` keeps `oku.toml` and `oku.lock`.
See [Commands](commands.md#oku-self-uninstall).
