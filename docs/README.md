# oku documentation

These pages describe what oku does today. The design for the whole product is
in [`../prd/`](../prd/).

| Page | For |
|---|---|
| [Getting started](getting-started.md) | Build oku, install a first package, put it on `PATH`, uninstall |
| [Commands](commands.md) | Every command, its flags, and what it prints |
| [Refs](refs.md) | The ways to point oku at a manifest, and sources |
| [Manifest reference](manifest.md) | Every key a package manifest accepts, for people publishing software |
| [Services](services.md) | Running a package's daemon with launchd or systemd |
| [Build caches](caches.md) | Downloading built packages from a signed cache, and filling one |
| [System scope](system-scope.md) | Apps, fonts and services for every user of the machine, with `sudo` |
| [Windows](windows.md) | What works on Windows, and how profiles differ there |
| [Projects](projects.md) | A list, a lock and a profile that belong to one repo |
| [List and lock](list-and-lock.md) | `oku.toml`, `oku.lock`, and moving a setup to another machine |
| [Trust and checksums](trust.md) | What oku verifies, what it pins, and when it stops |
| [Files and directories](files.md) | Where oku keeps things on disk |

## What works today

- Linux and macOS, amd64 and arm64.
- Windows, in part. Prebuilt packages install, run, roll back and uninstall, see
  [Windows](windows.md) for what is missing.
- Packages that ship a prebuilt download: tar in any common compression, zip,
  `.deb`, `.rpm`, a single binary or AppImage, on macOS `.dmg` and `.pkg`, and
  on Windows `.msi`.
  oku unpacks installers and never runs them.
- Packages that ship a macOS app, a Linux desktop launcher, or fonts. oku puts
  them in your per-user folders and takes them away on remove, rollback and
  uninstall.
- Services: a package's daemon runs under launchd or systemd for your user,
  enabled with `service = true` in your list.
- `oku shell <ref>...` opens a shell with packages on `PATH` and installs
  nothing.
- Build caches: `oku cache` and `oku key` share built packages through any
  directory or static web host, signed with minisign.
- System scope: `--system` installs a package's apps, fonts and services for the
  whole machine, after listing the files and asking.
- A shared store root at `/opt/oku` with `oku setup --system`.
- Packages built from source with `[build]` steps, after you approve their
  commands. The build uses tools already on your machine.
- A build sandbox on macOS and Linux: build commands get no network and cannot
  read your home directory.
- `vendor` steps for Go, Cargo, npm and pip packages, with the download pinned
  in `oku.lock`.
- Dependencies between packages, with version constraints. A build finds its
  deps' headers and libraries with no flags in the manifest.
- `oku add github:owner/repo` for a repo that has no oku manifest, by reading
  its newest release.
- `oku manifest init`, `lint`, `test` and `bump` for people who publish a
  manifest.
- Sources: `oku source add core github:someone/recipes`, then
  `oku add core/ripgrep` and `oku search grep`.
- Manifests with a fixed version, or with versions discovered from GitHub
  releases or git tags.
- One `oku.toml` for several machines, with `include` and per-platform `when`.
- Per-project lists: a repo with its own `oku.toml`, `oku.lock` and profile. A
  shell hook for bash, zsh and fish puts an allowed project's programs on `PATH`
  while you are inside it.
- `oku generations`, `oku rollback` and `oku gc`.
- Setting up a new machine from a published list and lock with
  `oku sync <list-ref>`.

## What does not work yet

- `patch` build steps.
- `.msi` packages.
- Installing anything system-wide, including services that run as root.
