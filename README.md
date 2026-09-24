<div align="center">

# oku

**Your machine, from one file.**

One `oku.toml` names the tools, dotfiles, secrets and OS settings of your account. A lock pins the sha256 of every download. `oku sync` builds that machine on Linux, macOS or Windows, and `oku rollback` puts the previous one back. oku needs no registry, no language to learn and no root.

[![Latest Release](https://img.shields.io/github/v/release/y3owk1n/oku?style=flat-square)](https://github.com/y3owk1n/oku/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/y3owk1n/oku/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/y3owk1n/oku/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/y3owk1n/oku?style=flat-square)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-%E2%9D%A4-30363D?style=flat-square)](https://github.com/sponsors/y3owk1n)

|   Linux   |   macOS   |           Windows           |      Status       |
| :-------: | :-------: | :-------------------------: | :---------------: |
| Supported | Supported | Supported, no build sandbox | Early development |

<sub>Manifest keys, CLI flags and behaviour may still change between releases. See the [CHANGELOG](CHANGELOG.md).</sub>

[Install](#install) · [What it does](#what-it-does) · [Publish a package](#publish-a-package) · [Compare](#how-oku-compares) · [Docs](docs/README.md)

</div>

---

```toml
# oku.toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"                        # a repo with no manifest at all
obsidian = { ref = "cask:obsidian", when = { os = "darwin" } }   # a Homebrew cask, without brew
jq = "aqua:jqlang/jq"                                        # the aqua registry, Scoop and winget too
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
git clone https://github.com/you/machines ~/.config/oku   # a new machine
oku sync                       # install and apply everything the list and lock name
oku sync --dry-run             # what would change, and nothing changes
oku rollback                   # the machine as it was before the last change
```

## Why oku

Tools like Nix with home-manager and nix-darwin can describe a whole machine in a file and roll it back. They ask you to learn a language, take over `/nix`, and draw from one central package set. Dotfile managers cover the home directory and nothing else. oku keeps the store, the lock and rollback, and leaves out the language, the package set and root.

- **One list, one lock, one rollback.** Packages, home files, secrets and settings change together. Every change is a generation. `oku rollback` reverts all of it, with no download.
- **Same input, same machine.** `oku.lock` pins the commit, the manifest hash and every download's sha256. Two machines with the same two files get the same store paths.
- **One list for every OS.** `include` and per-platform `when` let one list describe a Mac laptop, a Linux server and a Windows desktop. oku skips the tables of another OS.
- **No registry.** A package is a TOML manifest in the author's repo, a URL, a local file, a repo with no manifest at all, or a package of npm, PyPI, Go or crates.io. oku also reads the recipes of Homebrew casks, Scoop, winget and the aqua registry. It translates each into a manifest of its own, which downloads from the vendor and follows the vendor's versions. oku never runs brew, scoop, winget or aqua, and ships no package list of its own.
- **Checked downloads.** oku checks each download against a sha256 from the manifest, from upstream's checksum file, or from GitHub, npm or crates.io, and pins it in the lock.
- **Stops when a source changes.** oku checks each answer from a forge, a registry or a recipe for what it needs, and asks sources that version their format for one version. A source that changed makes `add` and `update` fail with its name, and `oku sync` still installs what the lock pins.
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

Add the line the installer printed to your shell's startup file. For zsh:

```bash
echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc && exec zsh

oku add github:sharkdp/fd      # install something
fd --version
oku doctor                     # checks PATH, the shell line, the sandbox and the profiles
```

[Getting started](docs/getting-started.md) walks through a first package, a version change and a rollback in about ten minutes.

---

## What it does

| You want to | oku does it with | Guide |
| :-- | :-- | :-- |
| Install a tool from GitHub, GitLab, Codeberg, Gitea, a URL or a file | `oku add github:BurntSushi/ripgrep` | [Add packages](docs/guides/add-packages.md) |
| Install from npm, PyPI, Go or crates.io without their toolchains on `PATH` | `oku add npm:prettier` and `[runtimes]` | [Registry packages](docs/guides/npm-pypi-go-cargo.md) |
| Install an app from a Homebrew cask, a Scoop or winget manifest, or the aqua registry, without their tools | `oku add cask:obsidian`, `scoop:`, `winget:`, `aqua:` | [Add packages](docs/guides/add-packages.md#add-a-package-from-homebrew-scoop-winget-or-aqua) |
| Follow a vendor's own update feed, download page or redirect | `from = "sparkle"`, `"page"` or `"redirect"` | [Manifest reference](docs/reference/manifest.md#version) |
| Set up every machine from one repo | a git clone at `~/.config/oku` and `oku sync` | [New machine](docs/guides/new-machine.md) |
| Place dotfiles and templates | `[files]` and `[vars]` | [Dotfiles](docs/guides/dotfiles.md) |
| Keep SSH keys and tokens in the repo, encrypted | `[secrets]` with sops and age | [Secrets](docs/guides/secrets.md) |
| Set macOS defaults, Windows registry values or GNOME settings | `[defaults]`, `[registry]`, `[dconf]` | [OS settings](docs/guides/os-settings.md) |
| Run a daemon at login | `service = true` | [Services](docs/guides/services.md) |
| Give a repo its own tools and environment variables, restored when you leave | an `oku.toml` with `[env]` in the repo and `oku allow` | [Projects](docs/guides/projects.md) |
| Use the same tools in CI | `uses: y3owk1n/oku@main` | [CI](docs/guides/ci.md) |
| Undo a change | `oku rollback` | [Undo and clean up](docs/guides/undo-and-clean-up.md) |

Every change is a generation that covers packages, files and settings together. `oku generations` lists them, and `oku rollback` switches back without downloading anything.

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

[Publish a manifest](docs/guides/publish-a-manifest.md) · [Manifest reference](docs/reference/manifest.md) · [Security](docs/reference/security.md) · [Build caches](docs/guides/build-caches.md)

---

## How oku compares

oku covers what a package manager, a dotfile manager and a settings script do separately. The rows come from each tool's own documentation, checked in September 2026.

| Setup | Tools from | Home files | Secrets | OS settings | Lock with hashes | Rollback | Root | You write |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| **oku** | Any repo, URL or file, npm, PyPI, Go and crates.io packages, and Homebrew cask, Scoop, winget and aqua recipes, translated. No registry of its own | Links, text, templates | sops and age files | macOS defaults, Windows registry, dconf | Always, every download | Packages, files and settings, in one generation | Only to [install for every user](docs/guides/system-wide.md) | TOML, or nothing |
| [Nix](https://nixos.org) + [home-manager](https://github.com/nix-community/home-manager) + [nix-darwin](https://github.com/nix-darwin/nix-darwin) | nixpkgs, plus flakes | Yes | Separate projects, sops-nix or agenix | macOS defaults, dconf | `flake.lock` pins inputs, nixpkgs pins each source | Per tool, each with its own generations | To create `/nix`, and for every nix-darwin switch | The Nix language |
| [Homebrew](https://brew.sh) + [chezmoi](https://www.chezmoi.io) | homebrew-core, plus taps | Files, links, templates | Password managers, age, gpg | Scripts you write | No. A Brewfile pins nothing | No. Revert the source in git and apply again | Homebrew, to install on macOS | Ruby, Go templates, shell |
| [mise](https://mise.jdx.dev) | A registry, backends such as `github:owner/repo`, and OS packages through apt, brew, winget and others | Links, copies, templates, tracked files, line edits | Environment variables from fnox, sops or age. Templates can write them to files | macOS defaults | Optional, `mise.lock`. It pins checksums for downloads, only versions for some backends, and nothing for OS packages | Files only, from a git history. Tools and packages stay as they are | For OS packages, system files and system services | TOML |

oku fits if you want a machine you can rebuild and roll back without learning Nix, or you ship software and do not want to maintain it in several registries. It does not fit if you need what nixpkgs or Homebrew's formulae build from source. oku translates Homebrew casks, Scoop, winget and the aqua registry, and refuses a recipe that runs an installer or a script to make its files. It covers the per-user part of what home-manager and nix-darwin do, and nothing that needs root. mise covers dotfiles, macOS defaults and OS packages too, and it has tasks, and env vars from files and scripts, that oku does not. oku differs from it in six ways. One generation covers the whole machine. A change that fails partway undoes itself. The lock pins every download, where mise's lock is optional. Per-user settings work on Windows and Linux too. oku unpacks installers and never runs them. Source builds run in a sandbox.

---

## Documentation

[Getting started](docs/getting-started.md) · [How oku works](docs/how-oku-works.md) · [All guides](docs/README.md#guides) · [Commands](docs/reference/commands.md) · [Troubleshooting](docs/troubleshooting.md) · [Coming from another tool](docs/guides/coming-from.md)

## Contributing

oku is written in Go and installs its own toolchain with `oku sync && oku allow`. See [CONTRIBUTING.md](CONTRIBUTING.md) for the build, the tests and releases. Report bugs through the [issues](https://github.com/y3owk1n/oku/issues).

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

Made by <a href="https://github.com/y3owk1n">y3owk1n</a>

</div>
