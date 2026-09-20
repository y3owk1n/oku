# oku documentation

These pages describe what oku does today. The design for the whole product is
in [`../prd/`](../prd/).

| Page | For |
|---|---|
| [Getting started](getting-started.md) | Build oku, install a first package, put it on `PATH`, uninstall |
| [Commands](commands.md) | Every command, its flags, and what it prints |
| [Refs](refs.md) | The ways to point oku at a manifest, and sources |
| [Manifest reference](manifest.md) | Every key a package manifest accepts, for people publishing software |
| [List and lock](list-and-lock.md) | `oku.toml`, `oku.lock`, and moving a setup to another machine |
| [Trust and checksums](trust.md) | What oku verifies, what it pins, and when it stops |
| [Files and directories](files.md) | Where oku keeps things on disk |

## What works today

- Linux and macOS, amd64 and arm64. Windows builds, but profiles need symlinks,
  so treat it as not supported yet.
- Packages that ship a prebuilt download: tar, tar.gz, tar.bz2, zip, or a
  single binary.
- `oku add github:owner/repo` for a repo that has no oku manifest, by reading
  its newest release.
- Sources: `oku source add core github:someone/recipes`, then
  `oku add core/ripgrep` and `oku search grep`.
- Manifests with a fixed version, or with versions discovered from GitHub
  releases or git tags.
- One `oku.toml` for several machines, with `include` and per-platform `when`.
- `oku generations`, `oku rollback` and `oku gc`.
- Setting up a new machine from a published list and lock with
  `oku sync <list-ref>`.

## What does not work yet

- Building from source. A `[build]` table in a manifest is recognised and
  refused.
- `oku manifest lint` and `oku manifest bump`.
- Per-project lists and the shell hook.
- `.tar.xz` and `.tar.zst` archives.
