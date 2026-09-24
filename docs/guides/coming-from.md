# Move to oku from another tool

You end up knowing which oku command or key replaces what you use today, and
what oku leaves to other tools.

oku covers what a package manager, a dotfile manager and a settings script do
separately. One `oku.toml` names packages, home files, secrets and per-user OS
settings. `oku.lock` pins every download by sha256, and one
[generation](../how-oku-works.md#generation) covers all of it, so
`oku rollback` undoes packages, files and settings together.

oku has no package catalogue of its own. A package is a manifest in the
author's repo, a URL, a local file, a repo with no manifest at all, or a
package of npm, PyPI, Go or crates.io. See [Add packages](add-packages.md).

## Homebrew and a Brewfile

A Brewfile such as:

```ruby
brew "ripgrep"
brew "fd"
cask "rectangle"
```

becomes:

```toml
# ~/.config/oku/oku.toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"
fd = "github:sharkdp/fd"
rectangle = { ref = "github:rxhanson/Rectangle", when = { os = "darwin" } }
```

| Homebrew | oku |
|---|---|
| `brew install <formula>` | `oku add github:<owner>/<repo>` |
| `brew uninstall` | `oku remove <name>` |
| `brew upgrade` | `oku update` |
| `brew outdated` | `oku outdated` |
| `brew list`, `brew info` | `oku list`, `oku info <name>` |
| `brew bundle` with a Brewfile | `oku sync` with `oku.toml` |
| A cask | `oku add cask:<token>`, which translates the cask into a manifest. oku copies the app to `~/Applications`. |
| A tap | A [source](../how-oku-works.md#source): `oku source add <alias> <ref>` |
| `brew search` | `oku search <term>`, in your sources only |
| `brew services start` | `service = true` on the package, or `oku service start <name>`. See [Services](services.md). |
| `brew pin` | A `version` on the package in `oku.toml` |
| `brew doctor` | `oku doctor` |

What oku does not do:

- It has no homebrew-core. Until you point it at a repo, it knows no package.
- A Brewfile pins nothing. `oku.lock` pins the version, the commit and the
  sha256 of every download, so two machines get the same bytes.
- oku needs no root. It installs into a store under your home. Only
  [system scope](system-wide.md) needs root, for apps, fonts and services of
  every user.
- oku unpacks `.pkg` files and never runs their install scripts.

## chezmoi or GNU Stow

| chezmoi or Stow | oku |
|---|---|
| `stow <dir>`, a symlink into your dotfiles repo | A `link` entry in `[files]` |
| A file that chezmoi copies | A `text` entry |
| A chezmoi template | A `render` entry, with values from `[vars]` |
| `chezmoi apply` | `oku sync` |
| `chezmoi diff` | `oku sync --dry-run`, which lists the paths it would write, not a diff |
| `chezmoi add <file>` | Move the file into your repo and write its `[files]` entry by hand |
| Secrets from age or gpg | `secret` entries and `[secrets]`, from sops or age files |
| A per-OS file | `when` on the entry |
| Your dotfiles repo | Your machines repo at `~/.config/oku`, see [Set up a new machine](new-machine.md) |

See [Manage your dotfiles](dotfiles.md) for every entry kind.

What oku does not do:

- Templates hold variables and nothing else. There are no conditionals and no
  loops. Write two entries with `when` for a difference between platforms.
- oku reads secrets from sops and age files only, not from a password manager.
- `oku.toml` has no key that runs a script of yours.
- oku refuses a path it did not write. Move an existing file away before the
  first sync.
- chezmoi has no rollback beyond reverting your repo. oku keeps each
  generation, so `oku rollback` puts back the exact bytes.

## nix-darwin and home-manager

| Nix | oku |
|---|---|
| `home.packages`, `environment.systemPackages` | `[packages]` |
| `home.file`, `xdg.configFile` | `[files]` |
| `system.defaults` | `[defaults]`, see [OS settings](os-settings.md) |
| `dconf.settings` | `[dconf]` |
| sops-nix or agenix | `[secrets]` and `secret` entries, see [Secrets](secrets.md) |
| `launchd.agents`, a user service | `service = true` on the package |
| `imports` | `include` |
| `lib.mkIf pkgs.stdenv.isDarwin` | `when = { os = "darwin" }` |
| `flake.lock` | `oku.lock` |
| `darwin-rebuild switch`, `home-manager switch` | `oku sync` |
| `nix flake update` | `oku update` |
| Generations and `--rollback` | `oku generations` and `oku rollback` |
| `nix-collect-garbage` | `oku gc` |
| `nix shell nixpkgs#<pkg>` | `oku shell <ref>` |
| A dev shell with direnv | A [project](projects.md) `oku.toml` with the shell hook, `[env]` for the `.envrc` exports, and `[[env.file]]` for `dotenv` and `dotenv_if_exists` |

What oku does not do:

- There is no language. `oku.toml` and manifests are TOML with a fixed set of
  keys, and `oku manifest lint` checks them.
- There is no nixpkgs. Each package comes from its own repo or a registry.
- oku covers the per-user part of what home-manager and nix-darwin do, and
  nothing that needs root. It does not set the host name, the firewall or
  Touch ID for sudo, and it never writes `/Library/Preferences`. Keep those in
  a script you run once.
- oku needs no `/nix` and no root to switch. One generation covers packages,
  files and settings, where Nix keeps generations per tool.

## mise

| mise | oku |
|---|---|
| `mise use -g <tool>` | `oku add <ref>` |
| A project's `mise.toml` | A [project](projects.md) `oku.toml` |
| `[env]` and `_.path` in `mise.toml` | `[env]`, with `PATH = { prepend = [...] }` |
| `_.file` in `mise.toml` | `[[env.file]]` |
| `MISE_ENV` with `mise.<env>.toml`, and `mise.local.toml` | `OKU_ENV` with `oku.<env>.toml`, and `oku.local.toml`, for `[env]` alone |
| `mise install` | `oku sync` |
| `mise upgrade`, `mise outdated` | `oku update`, `oku outdated` |
| `mise.lock` | `oku.lock`, which is always there and pins every download |
| The `github:` backend | A `github:` ref |
| The npm, pipx, go and cargo backends | `npm:`, `pypi:`, `go:` and `cargo:` refs, see [npm, PyPI, Go and Cargo](npm-pypi-go-cargo.md) |
| A language version, such as node 22 | `[runtimes]` |
| `mise activate` | `oku hook <shell>` |
| `mise exec` | `oku exec`, or `oku shell <ref> -- <command>` for a tool you have not added |
| Dotfiles and macOS defaults | `[files]` and `[defaults]` |

What oku does not do:

- oku has no tasks. A project's `[env]` sets variables, see
  [Set the project's variables](projects.md#set-the-projects-variables).
- oku installs no OS packages through apt, brew or winget. It unpacks `.deb`,
  `.rpm`, `.pkg` and `.msi` files itself and never runs their scripts.

Where oku differs:

- One generation covers the whole machine, and `oku rollback` reverts tools,
  files and settings together.
- A change that fails partway undoes itself.
- Per-user settings work on Windows and Linux too, through `[registry]` and
  `[dconf]`.
- A build from source runs in a sandbox with no network and no access to your
  home directory, on macOS and Linux.
