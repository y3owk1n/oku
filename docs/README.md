# oku documentation

These pages describe what oku does today. The design for the whole product is
in [`../prd/`](../prd/).

| Page | For |
|---|---|
| [Getting started](getting-started.md) | Build oku, install a first package, put it on `PATH`, uninstall |
| [Commands](commands.md) | Every command, its flags, and what it prints |
| [Refs](refs.md) | The ways to point oku at a manifest |
| [Manifest reference](manifest.md) | Every key a package manifest accepts, for people publishing software |
| [List and lock](list-and-lock.md) | `oku.toml`, `oku.lock`, and moving a setup to another machine |
| [Trust and checksums](trust.md) | What oku verifies, what it pins, and when it stops |
| [Files and directories](files.md) | Where oku keeps things on disk |

## What works today

- Linux and macOS, amd64 and arm64. Windows builds, but profiles need symlinks,
  so treat it as not supported yet.
- Packages that ship a prebuilt download: tar, tar.gz, tar.bz2, zip, or a
  single binary.
- Manifests with a fixed version.
- One `oku.toml` for several machines, with `include` and per-platform `when`.

## What does not work yet

- Building from source. A `[build]` table in a manifest is recognised and
  refused.
- Version discovery (`[version] from = ...`), `oku rollback`, `oku gc`.
- Installing from a repo that has no manifest.
- Source aliases (`alias/name` refs) and `oku search`.
- Per-project lists and the shell hook.
- `.tar.xz` and `.tar.zst` archives.
