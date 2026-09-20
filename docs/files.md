# Files and directories

oku writes only under three directories. It needs no root.

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
        oku-gen.toml           the packages in this generation
      current -> gen-2         the active generation

<cache>/oku/
  downloads/<sha256>           verified downloads, reused on reinstall
  git/<hash>/                  clones for git+ refs
```

## The store

A store path is named `<name>-<version>-<hash>`. The hash covers the manifest
content, the version, the platform and the artifact's sha256, so a changed
manifest gets a new path and never overwrites an old one.

oku builds a package in a temporary directory inside the store and renames it
into place as the last step. A failed install leaves no files in the store.

`oku remove` leaves store paths in place. Nothing deletes them yet except
`oku self uninstall`.

## Profiles and generations

Put `<data>/oku/profiles/global/current/bin` on `PATH`. Every `add`, `remove`,
`sync` and `update` that changes the package set writes a new `gen-<n>`
directory of symlinks and then moves `current` to it in one rename. A failed
change deletes its half-built generation and leaves `current` unchanged.

Old generations stay on disk. `oku rollback` does not exist yet.

## The cache

Deleting the cache directory is safe. oku downloads again when it needs to.
