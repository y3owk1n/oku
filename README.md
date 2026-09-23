<div align="center">

# oku

**Your machine, from one file.**

One `oku.toml` names the tools, dotfiles, secrets and OS settings of your account. A lock pins every byte. `oku sync` builds that machine on Linux, macOS or Windows, and `oku rollback` puts the previous one back. No registry, no language, no root.

[![Latest Release](https://img.shields.io/github/v/release/y3owk1n/oku?style=flat-square)](https://github.com/y3owk1n/oku/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/y3owk1n/oku/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/y3owk1n/oku/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/y3owk1n/oku?style=flat-square)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-%E2%9D%A4-30363D?style=flat-square)](https://github.com/sponsors/y3owk1n)

|   Linux   |   macOS   |           Windows           |      Status       |
| :-------: | :-------: | :-------------------------: | :---------------: |
| Supported | Supported | Supported, no build sandbox | Early development |

<sub>Manifest keys, CLI flags and behaviour may still change between releases. See the [CHANGELOG](CHANGELOG.md).</sub>

[Install](#install) · [The list](#the-list) · [Publish a package](#publish-a-package) · [Compare](#how-oku-compares) · [Docs](#documentation)

</div>

---

```toml
# oku.toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"                        # a repo with no manifest at all
rectangle = { ref = "github:rxhanson/Rectangle", when = { os = "darwin" } }   # a macOS app
prettier = { ref = "npm:prettier", version = "^3" }          # npm, PyPI, Go and crates.io too
atuin = { ref = "github:y3owk1n/oku-config#atuin", service = true }           # a daemon

[runtimes]
node = { ref = "github:y3owk1n/oku-config#node", version = "22" }             # what npm tools run on

[vars]
font = "JetBrainsMono Nerd Font Propo"

[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }

[defaults."com.apple.dock"]
autohide = true

[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"

[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

```bash
oku sync github:you/machines   # a new machine, from that list and its lock
oku sync --dry-run             # what would change, and nothing changes
oku rollback                   # the machine as it was before the last change
```

## Why oku

Tools like Nix with home-manager and nix-darwin can describe a whole machine in a file and roll it back. They ask you to learn a language, take over `/nix`, and draw from one central package set. Dotfile managers cover the home directory and nothing else. oku keeps the store, the lock and rollback, and leaves out the language, the package set and root.

- **One list, one lock, one rollback.** Packages, home files, secrets and settings change together. Every change is a generation. `oku rollback` reverts all of it, with no download.
- **Same input, same machine.** `oku.lock` pins the commit, the manifest hash and every download's sha256. Two machines with the same two files get the same store paths.
- **One list for every OS.** `include` and per-platform `when` let one list describe a Mac laptop, a Linux server and a Windows desktop. oku skips the tables of another OS.
- **No registry.** A package is a TOML manifest in the author's repo, a URL, a local file, a repo with no manifest at all, or a package of npm, PyPI, Go or crates.io. oku ships no package list of its own.
- **Checked downloads.** oku checks each download against a sha256 from the manifest, from upstream's checksum file, or from GitHub, npm or crates.io, and pins it in the lock.
- **TOML, not a language.** A manifest has a fixed set of keys and seven build step types. `oku manifest lint` checks all of it.
- **No root.** Everything lives in a private store under your home. `oku self uninstall` removes every file oku wrote.
- **All or nothing.** `add`, `remove`, `sync`, `update` and `rollback` check everything before the first step and undo a change that fails partway.
- **Nothing runs unasked.** oku unpacks `.deb`, `.rpm`, `.pkg` and `.msi` files and never runs their scripts. A build from source shows you its commands first, then runs them in a sandbox with no network and no access to your home directory.

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

The script puts one static binary in `~/.local/bin`, or `%LOCALAPPDATA%\oku\bin`, after checking its sha256. It edits none of your files and ends by printing the one line your shell needs. `oku self update` replaces the binary later, after checking its [minisign](https://jedisct1.github.io/minisign/) signature.

<details>
<summary>From source</summary>

```bash
git clone https://github.com/y3owk1n/oku && cd oku
CGO_ENABLED=0 go build -o oku ./cmd/oku
```

You need Go 1.26.4 or newer.

</details>

### First run

```bash
echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc && exec zsh

oku add github:sharkdp/fd      # install something
fd --version
oku doctor                     # checks PATH, the hook, the sandbox and the profiles
```

`oku add` writes to `oku.toml` and `oku.lock` for you, so a list grows one command at a time. For the GitHub API oku uses `GITHUB_TOKEN`, or the login of `gh` when it is installed. Without either, GitHub allows 60 requests an hour, which a first list of a dozen packages can use up. [Getting started](docs/getting-started.md) · [A real list that replaced nix-darwin](https://github.com/y3owk1n/oku-config)

---

## The list

**Packages.** A ref is enough. For a repo on GitHub, GitLab, Codeberg, or any Gitea or Forgejo server, oku reads the newest release, matches the files to your OS and CPU, finds the checksums, and shows you the manifest it wrote before it installs. Every line below installs a real package:

```bash
# A repo with no manifest. oku infers one from the release.
oku add github:BurntSushi/ripgrep
oku add gitlab:gitlab-org/cli                     # the glab CLI
oku add codeberg:mergiraf/mergiraf
oku add gitea:gitea.com/gitea/tea                 # any Gitea or Forgejo server
oku add github:rxhanson/Rectangle                 # a macOS app, copied to ~/Applications

# A manifest in a repo, by name or by path, or in any git repo.
oku add github:y3owk1n/oku-config#bat             # bat.toml or packages/bat.toml
oku add github:y3owk1n/oku-config#packages/zoxide.toml
oku add git+https://github.com/y3owk1n/oku-config#lazygit

# A manifest at a URL or on disk, or a download itself, here the one for macOS arm64.
oku add https://raw.githubusercontent.com/y3owk1n/oku-config/main/packages/starship.toml
oku add ./gh.toml
oku add https://github.com/sharkdp/hyperfine/releases/download/v1.19.0/hyperfine-v1.19.0-aarch64-apple-darwin.tar.gz

# A registry package, built or run with the toolchain that [runtimes] names.
oku add npm:prettier                              # runs on node
oku add pypi:ruff                                 # installed with uv
oku add go:mvdan.cc/gofumpt                       # built as go install does
oku add cargo:hexyl                               # built as cargo install --locked does

# A version: exact, the newest of a line, or a range.
oku add github:BurntSushi/ripgrep@14.1.1
oku add github:BurntSushi/ripgrep@14
oku add 'npm:prettier@^3'

# A short name for a repo of manifests.
oku source add kyle github:y3owk1n/oku-config
oku add kyle/sesh
```

When oku picks the wrong file of a release, `--asset` and `--bin` name the right one. `--service` starts a package's daemon now and at every login. A version in `oku.toml` names what `oku update` may move to, and `oku.lock` pins the exact build. `oku outdated` lists what is behind, with the newest version the list allows and the latest release.

A package can be a prebuilt download in tar, zip, 7z, `.deb`, `.rpm`, AppImage, `.dmg`, `.pkg` or `.msi`, or a build from source with dependencies between packages. It can ship a desktop app, fonts, a service, a library that other builds link against, a script oku wraps with its interpreter, or only files, such as agent skills or a colour scheme. A version can follow releases, an npm package, a branch, or a moving tag such as `nightly`.

**Home files.** `[files]` places links, text, rendered templates and decrypted secrets. `[vars]` feeds the templates, so a colour scheme is a table of variables and changing it re-renders every file in one generation. Secrets come from [sops](https://github.com/getsops/sops) and [age](https://age-encryption.org) files and are decrypted at apply time. oku refuses to overwrite a file it did not write.

**Settings.** `[defaults]` on macOS, `[registry]` on Windows and `[dconf]` on Linux set per-user OS settings. Before oku first writes a setting it records the old value, and it puts that value back when the entry leaves the list.

**Projects.** A repo can carry its own `oku.toml`, lock and profile. With the shell hook for bash, zsh, fish or PowerShell, entering the directory puts the project's tools on `PATH`, after you allowed it once.

```bash
cd ~/work/api && oku allow                 # once per repo
oku shell github:cli/cli -- gh --version   # try a package without installing it
oku exec gopls                             # run a project's tool where no hook runs, as in an editor
```

**CI.** The action in this repo installs oku and the tools that a project's lock pins, on Linux, macOS and Windows, and puts them on `PATH` for the later steps:

```yaml
- uses: y3owk1n/oku@main   # oku sync --yes --locked, with the store cached by oku.lock
- run: golangci-lint run
```

`oku outdated --json` lists each package with a newer version, for a bot that opens pull requests.

**History.** Every change is a generation, and it covers packages, files and settings.

```bash
oku generations
oku rollback
oku gc --keep 3
oku list --json | jq -r '.[].name'   # every command that prints data takes --json
```

**Sharing builds.** A signed cache is any directory or static web host. oku takes a built package from it only when a key you trust signed it, and builds it itself otherwise.

[List and lock](docs/list-and-lock.md) · [Refs](docs/refs.md) · [Secrets](docs/secrets.md) · [Projects](docs/projects.md) · [Commands](docs/commands.md) · [The action](docs/list-and-lock.md#one-lock-for-several-machines)

---

## Publish a package

The install instructions for your software become one line that you control: `oku add github:you/tool`. Nobody submits anything to any registry.

Many repos need no manifest. When release files follow the usual naming, the line above already works, and `oku manifest init --from you/tool` prints the manifest oku inferred so you can commit it. Otherwise a manifest is `oku.pkg.toml` next to your code:

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
oku manifest lint         # check it
oku manifest test         # install it into a throwaway store
oku manifest bump         # move it to the newest release, checksums included
oku manifest hash <url>   # print the checksums of a download
```

[Manifest reference](docs/manifest.md) · [Trust and checksums](docs/trust.md) · [Build caches](docs/caches.md)

---

## How oku compares

oku covers what a package manager, a dotfile manager and a settings script do separately. The rows come from each tool's own documentation, checked in September 2026.

| Setup | Tools from | Home files | Secrets | OS settings | Lock with hashes | Rollback | Root | You write |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| **oku** | Any repo, URL or file, and npm, PyPI, Go and crates.io packages. No registry of its own | Links, text, templates | sops and age files | macOS defaults, Windows registry, dconf | Always, every download | Packages, files and settings, in one generation | Only for [system scope](docs/system-scope.md) | TOML, or nothing |
| [Nix](https://nixos.org) + [home-manager](https://github.com/nix-community/home-manager) + [nix-darwin](https://github.com/nix-darwin/nix-darwin) | nixpkgs, plus flakes | Yes | Separate projects, sops-nix or agenix | macOS defaults, dconf | `flake.lock` pins inputs, nixpkgs pins each source | Per tool, each with its own generations | To create `/nix`, and for every nix-darwin switch | The Nix language |
| [Homebrew](https://brew.sh) + [chezmoi](https://www.chezmoi.io) | homebrew-core, plus taps | Files, links, templates | Password managers, age, gpg | Scripts you write | No. A Brewfile pins nothing | No. Revert the source in git and apply again | Homebrew, to install on macOS | Ruby, Go templates, shell |
| [mise](https://mise.jdx.dev) | A registry, backends such as `github:owner/repo`, and OS packages through apt, brew, winget and others | Links, copies, templates, tracked files, line edits | Environment variables from fnox, sops or age. Templates can write them to files | macOS defaults | Optional, `mise.lock`. It pins checksums for downloads, only versions for some backends, and nothing for OS packages | Files only, from a git history. Tools and packages stay as they are | For OS packages, system files and system services | TOML |

oku fits if you want a machine you can rebuild and roll back without learning Nix, or you ship software and do not want to maintain it in several registries. It does not fit if you need the catalogues that Homebrew and nixpkgs already have. oku has none until someone points it at a repo. It covers the per-user part of what home-manager and nix-darwin do, and nothing that needs root. mise covers dotfiles, macOS defaults and OS packages too, and it has env vars and tasks that oku does not. oku differs from it in six ways. One generation covers the whole machine. A change that fails partway undoes itself. The lock pins every download, where mise's lock is optional. Per-user settings work on Windows and Linux too. oku unpacks installers and never runs them. Source builds run in a sandbox.

---

## How it works

```
oku sync
  -> read the list             oku.toml, its includes, and the tables for this OS
  -> resolve each package      a manifest from a file, a URL or a forge, or one inferred
                               from a release or from npm, PyPI, Go or crates.io, at the
                               version the lock pins
  -> realize in the store      <data>/oku/store/<name>-<version>-<hash>/
  -> check everything          downloads, checksums, targets, secrets, before the first change
  -> new profile generation    a directory of links, swapped in with one rename
  -> apply the rest            apps, fonts, services, files, templates, secrets, settings,
                               each recorded in a ledger so rollback can undo it
```

A store path's hash covers the manifest, the version, the platform and the download's sha256, so a changed input never overwrites an old package. On Windows a profile uses shims, hard links and a junction where unix uses symlinks, so it needs no administrator rights either. [Files and directories](docs/files.md)

---

## Documentation

| Using oku                                  |                                                            |
| :----------------------------------------- | :--------------------------------------------------------- |
| [Getting started](docs/getting-started.md) | Install, a first package, `PATH`, uninstall                |
| [Commands](docs/commands.md)               | Every command, its flags, what it prints, its JSON         |
| [Refs](docs/refs.md)                       | The ways to point oku at a manifest, and sources           |
| [List and lock](docs/list-and-lock.md)     | `oku.toml`, `oku.lock`, home files, templates, OS settings |
| [Projects](docs/projects.md)               | A list and a profile that belong to one repo, the hook     |
| [Secrets](docs/secrets.md)                 | SSH keys and tokens from sops and age files                |
| [Services](docs/services.md)               | Running a package's daemon                                 |
| [System scope](docs/system-scope.md)       | Apps, fonts and services for the whole machine             |
| [Windows](docs/windows.md)                 | Shims, junctions, and what is not verified there           |
| [Trust and checksums](docs/trust.md)       | What oku verifies, what it pins, and when it stops         |
| [Files and directories](docs/files.md)     | Where oku keeps things on disk                             |

| Publishing with oku                    |                                              |
| :------------------------------------- | :------------------------------------------- |
| [Manifest reference](docs/manifest.md) | Every key, every build step, the sandbox     |
| [Build caches](docs/caches.md)         | Serving built packages, signed               |

| Working on oku                       |                                            |
| :----------------------------------- | :----------------------------------------- |
| [Releasing](docs/releasing.md)       | release-please, the signing key, rotating it |
| [Product spec](prd/product.md)       | Vision, promises, boundaries               |
| [Decisions](prd/decisions.md)        | Every design decision and why              |
| [Behaviours](prd/behaviours.md)      | Every promise oku tests                    |
| [Architecture](prd/architecture.md)  | Schema, lock format, paths, packages       |

---

## Contributing

oku is written in Go, and it installs its own toolchain. `oku sync && oku allow` sets it up from the repo's `oku.toml`. CI takes the same tools from the same lock with the [action](action.yml), so [`ci.yml`](.github/workflows/ci.yml) shows how to use it in a workflow of your own.

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
