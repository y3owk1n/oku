# Getting started

## Build oku

There are no releases yet, so build from source. You need Go 1.26.4 or newer.

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

oku installs from a manifest, a small TOML file that says where a package's
downloads are. Save this as `ripgrep.toml`:

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
add /home/you/.local/share/oku/profiles/global/current/bin to PATH to run it
```

oku installed the newest ripgrep release. To pick one, add `@version`:

```
$ oku add ./ripgrep.toml@14.1.1
```

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
ripgrep  15.2.0  /home/you/ripgrep.toml

$ oku remove ripgrep
removed ripgrep
```

## Move to another machine

`oku add` records every package in `~/.config/oku/oku.toml` and pins what it
resolved in `~/.config/oku/oku.lock`. Commit both files to a repo. On a new
machine, one command restores the same versions:

```
$ oku sync github:you/machines
adopted github:you/machines with 1 locked package
profile now holds 1 package
```

Use URL or repo refs in a list you publish. The `./ripgrep.toml` ref above only
works on the machine that has that file.

See [List and lock](list-and-lock.md#a-new-machine).

## Uninstall oku

```
$ oku self uninstall
```

It lists what it will delete, asks once, and removes every package, its own
directories and the `oku` binary. `--keep-list` keeps `oku.toml` and `oku.lock`.
See [Commands](commands.md#oku-self-uninstall).
