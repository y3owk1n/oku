# Files and directories

oku writes under three directories of its own, and needs no root. The only files
it writes anywhere else are the apps, fonts and service definitions of packages
you installed, see
[Outside oku's directories](#outside-okus-directories), and the shared store
root when you ask for one, see [A shared store root](#a-shared-store-root).

| Directory | Default | With the variable set |
|---|---|---|
| Config | `~/.config/oku` | `$XDG_CONFIG_HOME/oku` |
| Data | `~/.local/share/oku` | `$XDG_DATA_HOME/oku` |
| Cache | `~/.cache/oku` | `$XDG_CACHE_HOME/oku` |

The defaults are the same on macOS and Linux. On Windows the defaults are
`%APPDATA%\oku`, `%LOCALAPPDATA%\oku` and `%LOCALAPPDATA%\oku\cache`.

Point the three variables at a scratch directory to try oku without touching
your real setup:

```sh
export XDG_CONFIG_HOME=/tmp/oku-try/config
export XDG_DATA_HOME=/tmp/oku-try/data
export XDG_CACHE_HOME=/tmp/oku-try/cache
```

## Layout

```
<config>/oku/
  oku.toml                     your package list
  oku.lock                     what oku resolved
  config.toml                  your source aliases, caches, trusted keys and the store root
  signing.key                  the secret key of "oku cache push", after "oku key generate"

<data>/oku/
  store/
    ripgrep-14.1.1-9d34c5164d550c60/
      pkg/                     the whole unpacked download
      bin/rg -> ../pkg/rg
      share/man/man1/rg.1 -> ../../../pkg/doc/rg.1
      oku-meta.toml            name, version, platform, url, sha256
  profiles/
    global/
      gen-1/                   one directory per profile change
      gen-2/
        bin/rg -> <store path>/bin/rg
        share/...
        oku-gen.toml           when it was written, the packages in it, and
                               the store paths of their deps
        oku.lock               a copy of oku.lock as it was at that time
      current -> gen-2         the active generation
    project-2d27013d8c67/      one per project, same layout
  exposed.toml                 every file oku wrote outside these directories
  services/                    definitions of services that are not enabled (macOS)
  logs/                        output of services (macOS)
  trust/
    approvals.toml             manifests you allowed to run build commands
    allow.toml                 projects the shell hook may apply

<cache>/oku/
  downloads/<sha256>           verified downloads, reused on reinstall
  git/<hash>/                  clones for git+ refs
```

## The store

A store path is named `<name>-<version>-<hash>`. The hash covers the manifest
content, the version, the platform and the artifact's sha256, so a changed
manifest gets a new path and never overwrites an old one.

oku unpacks a download in a temporary directory inside the store and renames
it into place as the last step. A failed install leaves no files in the store.

A package built from source is different, because build systems write the
final path into the files they install. oku builds the source in a temporary
directory outside the store, installs straight into the final store path, and
writes `oku-meta.toml` last. A store path without that file is a crashed build,
and oku deletes it before it builds again. Such a package has no `pkg/`
directory.

`oku remove` leaves store paths in place, because older generations still use
them. `oku gc` deletes the store paths that no generation uses, see
[Commands](commands.md#oku-gc).

### A shared store root

[`oku setup --system`](commands.md#oku-setup) moves the store to
`/opt/oku/store`. It sets `store_root = "/opt/oku"` in `<config>/oku/config.toml`.
Everything else stays where it was. Delete that line to go back to the store in
the data directory, then run `oku sync`.

## Profiles and generations

A [project](projects.md) keeps `oku.toml` and `oku.lock` in its own directory
and gets its own profile here.

Put `<data>/oku/profiles/global/current/bin` on `PATH`. Every `add`, `remove`,
`sync` and `update` that changes the package set writes a new `gen-<n>`
directory of symlinks and then moves `current` to it in one rename. A failed
change deletes its half-built generation and leaves `current` unchanged.

Old generations stay on disk until `oku gc --keep N` deletes them.
`oku generations` lists them and `oku rollback` switches back to one, see
[Commands](commands.md#oku-rollback).

## Outside oku's directories

An app or a font only works from the place the OS reads it, so oku copies those
out of the store:

| | macOS | Linux |
|---|---|---|
| Apps | `~/Applications/` | `<data home>/applications/oku-<name>.desktop` |
| Fonts | `~/Library/Fonts/` | `<data home>/fonts/oku/` |
| Enabled services | `~/Library/LaunchAgents/dev.oku.<name>.plist` | `<config home>/systemd/user/oku-<name>.service` |

With `system = true` on the package they go to the machine-wide directories
instead, see [System scope](system-scope.md).

Every such file is written to the ledger `exposed.toml` before oku creates it,
with the package and the store path it came from. `oku remove`, `oku rollback`
and `oku sync` remove what the active generation no longer has, and
`oku self uninstall` removes everything in the ledger. oku never overwrites
a file that is not in its ledger.

oku does not edit shell startup files, the system `PATH`, or anything else
outside these places.

## The cache

Deleting the cache directory is safe. oku downloads again when it needs to.
`oku gc` does not delete the cache.
