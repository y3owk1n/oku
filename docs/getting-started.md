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
It ends by printing the one line your shell needs, see
[Set up your shell](#set-up-your-shell).

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

## Set up your shell

The installer puts `oku` in `~/.local/bin`, and oku links every program it
installs into one directory. Neither is on `PATH` on a new machine. One line in
your shell's startup file puts both there. The installer prints that line for
your shell, with a command that appends it.

| Shell | File | Line |
|---|---|---|
| bash | `~/.bashrc` | `[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook bash)"` |
| zsh | `~/.zshrc` | `[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"` |
| fish | `~/.config/fish/config.fish` | `test -x "$HOME/.local/bin/oku"; and "$HOME/.local/bin/oku" hook fish \| source` |
| PowerShell | the file `$PROFILE` names | `if (Test-Path "$HOME\AppData\Local\oku\bin\oku.exe") { Invoke-Expression ((& "$HOME\AppData\Local\oku\bin\oku.exe" hook pwsh) -join [Environment]::NewLine) }` |

For zsh on a new Mac, which has no `~/.zshrc` yet:

```sh
echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc
exec zsh
```

The line does three things:

- It puts the directory of `oku` on `PATH`.
- It puts `<data>/oku/profiles/global/current/bin` on `PATH`, where the programs
  you install are.
- It applies a [project's](projects.md) tools and variables while you are inside
  an allowed project.

It names `oku` by its full path, because `oku` is not on `PATH` before the line
has run. It does nothing when that file is gone, so it is safe in a dotfiles
repo that other machines share. Loading it twice changes nothing. oku never
edits a startup file itself.

If you chose another directory with `OKU_INSTALL_DIR`, the line has that path.
`oku hook --help` prints the four lines for the oku you are running, and
`oku doctor` says whether the line is in place.

```
$ oku add github:BurntSushi/ripgrep
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
