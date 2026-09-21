<div align="center">

# oku

A package manager with no central registry. Point it at any repo, and a lock file pins what it installs. The same list sets up your dotfiles, secrets and OS settings.

[![Latest Release](https://img.shields.io/github/v/release/y3owk1n/oku?style=flat-square)](https://github.com/y3owk1n/oku/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/y3owk1n/oku/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/y3owk1n/oku/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/y3owk1n/oku?style=flat-square)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-%E2%9D%A4-30363D?style=flat-square)](https://github.com/sponsors/y3owk1n)

|   Linux   |   macOS   |         Windows          |      Status       |
| :-------: | :-------: | :----------------------: | :---------------: |
| Supported | Supported | Supported, no build sandbox | Early development |

<sub>Manifest keys, CLI flags and behaviour may still change between releases. See the [CHANGELOG](CHANGELOG.md).</sub>

[Install](#install) · [What oku does](#what-oku-does) · [Publish a package](#publish-a-package) · [Compare](#how-oku-compares) · [Docs](#documentation)

</div>

---

Installing software with oku is one line that the author controls and the user can check.

```bash
oku add github:BurntSushi/ripgrep        # a repo with no oku manifest at all
oku add github:you/tool@1.4.0            # a manifest next to the code, at a version
oku add gitlab:gitlab-org/cli            # or codeberg:, gitea:host/..., github:host/...
oku add npm:prettier                     # a command-line tool from the npm registry
oku add https://example.com/tool.toml    # a manifest at a URL, or ./tool.toml
oku add https://example.com/tool-1.2.0-linux-amd64.tar.gz   # or the download itself
oku sync github:you/machines             # rebuild your whole setup on a new machine
```

## Why oku

- **No registry.** A package is a TOML manifest in the author's own repo, a URL or a local file. Nobody submits anything anywhere, and oku ships no package list of its own.
- **Often no manifest either.** For a repo without one on GitHub, GitLab, Codeberg, or any Gitea or Forgejo server, oku reads the newest release, matches the files to your OS and CPU, finds the published checksums, and shows you the manifest it wrote before it installs. A URL of the download itself works the same way, and so does a command-line tool in the npm registry, which runs through a node that you pin once. When oku picks the wrong file, `--asset` and `--bin` name the right one.
- **Same input, same machine.** `oku.toml` lists what you want. `oku.lock` pins the commit, the manifest hash and every download's sha256. `oku sync` on a new machine gives the same store paths.
- **TOML, not a language.** A manifest has a fixed set of keys and seven build step types. `oku manifest lint` checks all of it.
- **More than packages.** The same `oku.toml` places files in your home directory, as links, as text or from templates with your own variables. It decrypts secrets from sops and age files. It sets macOS `defaults`, Windows registry values and dconf keys. A generation holds all of it, so `oku rollback` puts it back.
- **All or nothing.** `add`, `remove`, `sync`, `update` and `rollback` check everything before the first step and undo a change that fails partway. `oku sync --dry-run` runs every check and changes nothing.
- **No root.** Everything lives in a private store under your home. Each change is a new generation, `oku rollback` activates the previous one, and `oku self uninstall` removes every file oku wrote.
- **Nothing runs unasked.** oku unpacks `.deb`, `.rpm`, `.pkg` and `.msi` files and never runs their scripts. A build from source shows you its commands first, and then runs them in a sandbox that has no network and cannot read your home directory.

---

## Install

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
```

The script puts one static binary in `~/.local/bin`, or `%LOCALAPPDATA%\oku\bin`, after checking its sha256. It needs no root and edits none of your files, so it prints the one line your shell needs. `oku self update` replaces the binary later, after checking its [minisign](https://jedisct1.github.io/minisign/) signature.

<details>
<summary>From source</summary>

```bash
git clone https://github.com/y3owk1n/oku && cd oku
CGO_ENABLED=0 go build -o oku ./cmd/oku
```

You need Go 1.26.4 or newer.

</details>

### First run

The installer ends with one line for your shell's startup file and a command that
appends it. That line puts oku and the programs it installs on `PATH`. On a new
Mac:

```bash
echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc && exec zsh

oku add github:sharkdp/fd      # install something
fd --version
oku doctor                     # checks PATH, the hook, the sandbox and the profiles
```

[Getting started](docs/getting-started.md)

---

## What oku does

**Packages.** Prebuilt downloads in tar, zip, 7z, `.deb`, `.rpm`, AppImage, `.dmg`, `.pkg` and `.msi`, or a build from source with dependencies between packages. A package can also ship a desktop app, fonts and a service. It can be a prebuilt library that other builds link against, a script that oku wraps with its interpreter, such as node, or only files, such as agent skills or a colour scheme. A version can follow releases, an npm package, or a moving tag such as `nightly`.

```bash
oku add github:you/recipes#postgres --service   # runs now and at every login
oku service logs postgres
oku list --json | jq -r '.[].name'              # every command that prints data takes --json
```

**Your setup as two files.** One `oku.toml` can describe a Mac laptop, a Linux server and a Windows desktop, with `include` and per-platform `when`. `oku.lock` pins the versions, so every machine gets the same ones.

```toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"
rectangle = { ref = "mine/rectangle", when = { os = "darwin" } }
postgres = { ref = "mine/postgres", service = true }
```

**Your home directory and settings.** The global list places files, renders templates, decrypts secrets and sets per-user OS settings. oku refuses to overwrite a file it did not write. Before it first writes a setting it records the old value, and it puts that value back when the entry leaves the list. It skips the tables of another OS, so one list serves every machine.

```toml
[vars]
font = "JetBrainsMono Nerd Font Propo"

[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
"{{home}}/.claude/skills/deslop" = { link = "{{pkg.cursor-plugins}}/skills/deslop" }

[defaults."com.apple.dock"]
autohide = true

[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"

[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

```bash
oku sync --dry-run    # resolve, build, render and decrypt, then print what would change
```

**Projects.** A repo can carry its own `oku.toml`, lock and profile. With the shell hook for bash, zsh, fish or PowerShell, entering the directory puts the project's tools on `PATH`, after you allowed it once.

```bash
cd ~/work/api && oku allow     # once per repo, then its tools are on PATH while you are inside
oku shell github:cli/cli -- gh --version   # or try a package without installing it
```

**History.** Every change is a generation, and it covers packages, files and settings. oku overwrites nothing, so going back needs no download.

```bash
oku generations
oku rollback
oku gc --keep 3
```

**Sharing builds.** A signed cache is any directory or static web host. oku takes a built package from it only when a key you trust signed it, and builds it itself otherwise.

[Commands](docs/commands.md) · [List and lock](docs/list-and-lock.md) · [Secrets](docs/secrets.md) · [Projects](docs/projects.md) · [Build caches](docs/caches.md)

---

## Publish a package

Many repos need no manifest. When release files follow the usual naming, `oku add github:you/tool` already works, as do `gitlab:`, `codeberg:` and `gitea:` refs, and `oku manifest init --from you/tool` prints the manifest oku inferred so you can commit it.

A manifest is `oku.pkg.toml` next to your code:

```toml
[package]
name = "tool"
description = "Does one thing"

[version]
from = "github-releases"
repo = "you/tool"

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/you/tool/releases/download/{{tag}}/tool-{{version}}-linux-amd64.tar.gz"
sha256_url = "https://github.com/you/tool/releases/download/{{tag}}/checksums.txt"
bin = ["tool"]

[build]                     # used when no artifact fits the machine
needs = ["go"]
[[build.step]]
run = "go build -o tool ."
shell = "sh"
[[build.step]]
install = { bin = ["tool"] }
```

```bash
oku manifest lint    # check it
oku manifest test    # install it into a throwaway store
oku manifest bump    # move it to the newest release, checksums included
oku manifest hash <url>   # print the checksums of a download
```

[Manifest reference](docs/manifest.md) · [Trust and checksums](docs/trust.md)

---

## How oku compares

| Tool                                  | Where packages come from               | Written in          | Pins hashes in a lock | Runs on               |
| :------------------------------------ | :------------------------------------- | :------------------ | :-------------------: | :-------------------- |
| **oku**                               | Any repo, URL, file or npm package. No registry of its own | TOML, or nothing    | Yes                   | Linux, macOS, Windows |
| [Homebrew](https://brew.sh)           | A central tap, plus third-party taps   | Ruby                | No                    | macOS, Linux          |
| [Nix](https://nixos.org)              | nixpkgs, plus flakes                   | The Nix language    | Yes                   | Linux, macOS          |
| [mise](https://mise.jdx.dev)          | A registry of tools and backends       | TOML config         | Optional              | Linux, macOS, Windows |
| [Scoop](https://scoop.sh)             | Buckets                                | JSON                | No                    | Windows               |

oku fits if you want the reproducibility of a lock file without learning a language for it, or you ship software and do not want to maintain a package in several registries. It does not fit if you need the large catalogues that Homebrew and nixpkgs already have. oku has none until someone points it at a repo. For dotfiles and OS settings it covers the per-user part of what home-manager and nix-darwin do, and nothing that needs root.

---

## How it works

```
oku add <ref>
  -> fetch the manifest        a file, a URL, a repo on GitHub, GitLab, Codeberg or Gitea, any git repo
                               or infer one from a release, a URL of the download, or the npm registry
  -> pick a version            pinned, locked, or the newest release
  -> pick a strategy           the first artifact that fits this machine, else [build]
  -> realize in the store      <data>/oku/store/<name>-<version>-<hash>/
  -> new profile generation    a directory of links, swapped in with one rename
  -> expose                    apps, fonts and services, each recorded in a ledger
  -> apply the list            files, templates, secrets and OS settings, on sync and update
```

A store path's hash covers the manifest, the version, the platform and the download's sha256, so a changed input never overwrites an old package. On Windows a profile uses shims, hard links and a junction where unix uses symlinks, so it needs no administrator rights either. [Files and directories](docs/files.md)

---

## Documentation

| Using oku                                      |                                                            |
| :--------------------------------------------- | :--------------------------------------------------------- |
| [Getting started](docs/getting-started.md)     | Install, a first package, `PATH`, uninstall                |
| [Commands](docs/commands.md)                   | Every command, its flags, what it prints, its JSON         |
| [Refs](docs/refs.md)                           | The ways to point oku at a manifest, and sources           |
| [List and lock](docs/list-and-lock.md)         | `oku.toml`, `oku.lock`, home files, templates, OS settings |
| [Projects](docs/projects.md)                   | A list and a profile that belong to one repo, the hook     |
| [Secrets](docs/secrets.md)                     | SSH keys and tokens from sops and age files                |
| [Services](docs/services.md)                   | Running a package's daemon                                 |
| [System scope](docs/system-scope.md)           | Apps, fonts and services for the whole machine             |
| [Windows](docs/windows.md)                     | Shims, junctions, and what is not verified there           |
| [Trust and checksums](docs/trust.md)           | What oku verifies, what it pins, and when it stops         |
| [Files and directories](docs/files.md)         | Where oku keeps things on disk                             |

| Publishing with oku                            |                                                            |
| :--------------------------------------------- | :--------------------------------------------------------- |
| [Manifest reference](docs/manifest.md)         | Every key, every build step, the sandbox                   |
| [Build caches](docs/caches.md)                 | Serving built packages, signed                             |

| Working on oku                                 |                                                            |
| :--------------------------------------------- | :--------------------------------------------------------- |
| [Releasing](docs/releasing.md)                 | release-please, the signing key, rotating it               |
| [Product spec](prd/product.md)                 | Vision, promises, boundaries                               |
| [Decisions](prd/decisions.md)                  | Every design decision and why                              |
| [Behaviours](prd/behaviours.md)                | Every promise oku tests                                    |
| [Architecture](prd/architecture.md)            | Schema, lock format, paths, packages                       |

---

## Contributing

oku is written in Go. oku installs its own toolchain. `oku sync && oku allow` sets it up from `oku.toml`.

```bash
just fmt && just lint && just test && just build   # the pre-commit gate
```

Every behaviour in [`prd/behaviours.md`](prd/behaviours.md) has a test named after it. CI runs the suite on Linux, macOS and Windows, and on Windows it also runs the real `oku.exe` against real releases. Report bugs through the [issues](https://github.com/y3owk1n/oku/issues).

---

## Support the project

One person builds oku in their spare time. If you use it, you can [sponsor it](https://github.com/sponsors/y3owk1n).

## License

MIT. See [LICENSE](LICENSE).

<div align="center">
<br/>

**Install and add your first package:**

```bash
curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh && ~/.local/bin/oku add github:sharkdp/fd
```

Made with ❤️ by <a href="https://github.com/y3owk1n">y3owk1n</a>

</div>
