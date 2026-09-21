# oku documentation

Start with [Getting started](getting-started.md). The [README](../README.md)
has the overview.

| Using oku | |
|---|---|
| [Getting started](getting-started.md) | Install, a first package, `PATH`, uninstall |
| [Commands](commands.md) | Every command, its flags, what it prints, its JSON |
| [Refs](refs.md) | The ways to point oku at a manifest, a repo on GitHub, GitLab, Codeberg or Gitea, or a download, and sources |
| [List and lock](list-and-lock.md) | `oku.toml`, `oku.lock`, one list for several machines |
| [Projects](projects.md) | A list, a lock and a profile that belong to one repo, and the shell hook |
| [Secrets](secrets.md) | SSH keys and tokens from sops and age files, decrypted on sync |
| [Services](services.md) | Running a package's daemon with launchd, systemd or Task Scheduler |
| [System scope](system-scope.md) | Apps, fonts and services for every user of the machine |
| [Windows](windows.md) | Shims, junctions, and what is not verified there |
| [Trust and checksums](trust.md) | What oku verifies, what it pins, and when it stops |
| [Files and directories](files.md) | Where oku keeps things on disk |

| Publishing with oku | |
|---|---|
| [Manifest reference](manifest.md) | Every key a manifest accepts, every build step, the sandbox |
| [Build caches](caches.md) | Serving built packages from a directory or a web host, signed |

| Working on oku | |
|---|---|
| [Releasing](releasing.md) | release-please, the signing key, rotating it |
| [Product spec](../prd/product.md) | Vision, promises, boundaries |
| [Decisions](../prd/decisions.md) | Every design decision and why |
| [Behaviours](../prd/behaviours.md) | Every promise oku tests |
| [Architecture](../prd/architecture.md) | Manifest schema, lock format, paths, packages |

## Platforms

| | Linux | macOS | Windows |
|---|---|---|---|
| CPU | amd64, arm64 | amd64, arm64 | amd64, arm64 |
| Build sandbox | yes, where the host allows user namespaces | yes | no |
| Services | systemd | launchd | Task Scheduler |
| Installers oku unpacks | `.deb`, `.rpm`, AppImage | `.dmg`, `.pkg` | `.msi` |
| Needs administrator rights | only for [system scope](system-scope.md) | only for system scope | only for system scope |

tar, zip and 7z archives, `.deb` and `.rpm` unpack on every OS. What is built
and not verified on a real machine of its kind is listed in
[Windows](windows.md#what-is-not-verified) and
[System scope](system-scope.md#what-was-tested).
