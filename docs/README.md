# oku documentation

New to oku? Start with [Getting started](getting-started.md). It takes about
ten minutes.

## Learn

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install oku, install a package, change its version, undo it |
| [How oku works](how-oku-works.md) | The list, the lock, the store and generations, and a glossary |

## Guides

Pick the task you have.

**Install software**

| Guide | What you end up with |
|---|---|
| [Add packages](guides/add-packages.md) | Tools from GitHub, GitLab, Codeberg, Gitea, any git repo, a URL or a file, at the version you want |
| [npm, PyPI, Go and cargo packages](guides/npm-pypi-go-cargo.md) | Tools from package registries, without their toolchain on your `PATH` |
| [Run a service](guides/services.md) | A package's daemon that starts at login |
| [Install for every user](guides/system-wide.md) | Apps, fonts and services for the whole machine |

**Set up your machine**

| Guide | What you end up with |
|---|---|
| [Set up a machine from a repo](guides/new-machine.md) | One repo that sets up every machine you use, kept in step |
| [Manage dotfiles](guides/dotfiles.md) | Your config files and templates placed by the same list |
| [Keep secrets](guides/secrets.md) | SSH keys and tokens from encrypted files |
| [Change OS settings](guides/os-settings.md) | macOS defaults, Windows registry values and GNOME settings |
| [Undo and clean up](guides/undo-and-clean-up.md) | Rollback, freeing disk space, and uninstalling oku |

**Work in projects**

| Guide | What you end up with |
|---|---|
| [Tools per project](guides/projects.md) | A repo whose tools appear on `PATH` when you `cd` into it |
| [Use oku in CI](guides/ci.md) | GitHub Actions jobs with the tools your lock pins |

**Publish and share**

| Guide | What you end up with |
|---|---|
| [Publish a manifest](guides/publish-a-manifest.md) | `oku add github:you/tool` installs your tool on every OS |
| [Share builds](guides/build-caches.md) | A signed cache so machines skip building from source |

**Other**

| Guide | What you end up with |
|---|---|
| [Coming from another tool](guides/coming-from.md) | Your Homebrew, chezmoi, nix-darwin or mise setup in oku terms |
| [Windows](guides/windows.md) | What works differently on Windows |
| [Troubleshooting](troubleshooting.md) | What to do when oku stops or a program does not run |

## Reference

| Page | What it lists |
|---|---|
| [Commands](reference/commands.md) | Every command and flag, what it prints, its JSON and exit codes |
| [oku.toml](reference/oku-toml.md) | Every key of your list and of `config.toml` |
| [Manifest](reference/manifest.md) | Every key of a package manifest, build steps and the sandbox |
| [Refs](reference/refs.md) | Every way to point oku at a package, sources and tokens |
| [oku.lock](reference/lock.md) | The lock file format |
| [Paths](reference/paths.md) | Where oku keeps things, environment variables, what it writes elsewhere |
| [Security](reference/security.md) | What oku checks, what it pins, and when it stops to ask |

## Platforms

| | Linux | macOS | Windows |
|---|---|---|---|
| CPU | amd64, arm64 | amd64, arm64 | amd64, arm64 |
| Build sandbox | yes, where the host allows user namespaces | yes | no |
| Services | systemd | launchd | Task Scheduler |
| Installers oku unpacks | `.deb`, `.rpm`, AppImage | `.dmg`, `.pkg` | `.msi` |
| Needs administrator rights | only to [install for every user](guides/system-wide.md) | same | same |

tar, zip and 7z archives, `.deb` and `.rpm` unpack on every OS.

Working on oku itself? See [CONTRIBUTING.md](../CONTRIBUTING.md).
