# List and lock

Two files in the config directory describe a machine:

- `oku.toml` is what you want installed. You edit it.
- `oku.lock` is what oku resolved. oku writes it.

Both live in `~/.config/oku/`, or `$XDG_CONFIG_HOME/oku/`. Keep both in version
control.

## oku.toml

```toml
# search tools
[packages]
ripgrep = "github:you/recipes#ripgrep"
fd = { ref = "github:you/recipes#fd", version = "10.2.0" }
"node.js" = "https://example.com/node.toml"
```

Each key under `[packages]` is a package name, and it must equal the `name` in
the manifest the [ref](refs.md) points at. The value is a ref, or a table with
`ref` and an optional `version`. A name that contains `.` needs quotes.

`oku add` and `oku remove` edit this file as text. Your comments, the order of
entries and any other tables stay as you wrote them. A new package goes at the
end of `[packages]`.

You can also edit the file by hand and run `oku sync`.

One form is not editable by oku. A package written as its own table,
`[packages.fd]`, makes `oku add fd` and `oku remove fd` stop and ask you to
edit it by hand. `oku sync` reads that form.

## oku.lock

```toml
# Written by oku. Commit this file, and change it with oku commands only.

[[package]]
name = 'ripgrep'
ref = 'github:you/recipes#ripgrep'
commit = '3fce3b5bb0236da2df6d99672afb8a719642eca7'
manifest_sha256 = '17b39b37...'
version = '14.1.1'

[package.platform]
[package.platform.darwin-arm64]
strategy = 'artifact'
url = 'https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-apple-darwin.tar.gz'
sha256 = '24ad7677...'

[package.platform.linux-amd64-glibc]
strategy = 'artifact'
url = '...'
sha256 = '...'
```

| Key | Meaning |
|---|---|
| `ref` | The ref from `oku.toml`, without `@version`. |
| `commit` | The commit the manifest was read at. Only for `github:` and `git+` refs. |
| `manifest_sha256` | Digest of the manifest file. |
| `version` | The version installed. |
| `platform.<name>` | One entry per platform that has resolved this package. |

Platform names are `os-arch`, plus `-glibc` or `-musl` on Linux.

Packages are sorted by name, so the same state always writes the same bytes
and diffs stay small.

## One lock for several machines

Each machine adds its own platform entry the first time it runs `oku sync`, and
does not change the other entries. Commit the lock back after syncing on a new
kind of machine.

Platform entries for a package are cleared when `oku update` accepts a changed
manifest, because they described the old one. Each machine adds its entry
again on its next sync.

## A new machine

Put `oku.toml` and `oku.lock` into `~/.config/oku/`, by copying them or by
symlinking that directory from your dotfiles repo. Then:

```
oku sync
```

`sync` reads `github:` and `git+` manifests at the locked commit and checks
every download against the locked sha256, so the new machine gets the same
versions and the same bytes.

## Moving versions

Versions stay fixed until you run `oku update`. It reads every ref fresh and
rewrites the lock. `oku update <name>` does it for one package. See
[Commands](commands.md#oku-update).
