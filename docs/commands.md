# Commands

Every command exits with status 0 on success. On failure it prints
`oku: <reason>` to stderr and exits with status 1.

All commands act on the global list and the global profile. Per-project lists
do not exist yet.

## oku add

```
oku add <ref>[@version]
```

Installs the package a [ref](refs.md) points at.

1. Fetches the manifest.
2. Picks the first `[[artifact]]` whose `match` fits this machine.
3. Downloads it, verifies the checksum, and unpacks it into the store.
4. Activates a new profile generation that includes the package.
5. Writes the package to `oku.toml` and its resolution to `oku.lock`.

Adding a package that is already installed replaces it.

`@version` must equal the manifest's version. It is recorded in `oku.toml` as
`{ ref = "...", version = "..." }`.

Output:

```
added ripgrep 14.1.1
```

Two notices go to stderr when they apply. One names the profile `bin`
directory when it is missing from `PATH`. The other says that oku trusted a
download because the manifest publishes no checksum, see
[Trust and checksums](trust.md).

Common failures:

| Message | Meaning |
|---|---|
| `<name> has no artifact for darwin-arm64` | No `[[artifact]]` matches this machine. |
| `... building from source is not supported so far` | The manifest only offers a `[build]`. |
| `checksum mismatch for <url>` | The download differs from the expected sha256. Nothing was installed. |
| `<a> and <b> both provide bin/<x>` | Two packages ship a file of the same name. The second install is refused. |
| `<ref> provides version X, not Y` | The `@version` suffix does not match the manifest. |

A failed `add` leaves the previous profile active and the list and lock
unchanged.

## oku remove

```
oku remove <name>
```

Drops the package from the profile, from `oku.toml` and from `oku.lock`. The
package's files stay in the store, so adding it again needs no download.

It also works when only the list or the lock still names the package, which
happens after the data directory was deleted. A name found nowhere fails with
`<name>: not installed`.

## oku list

```
oku list
```

Prints one line per installed package: name, version, ref. With nothing
installed it prints `no packages installed`.

## oku sync

```
oku sync
```

Makes the profile match `oku.toml`, at the versions pinned in `oku.lock`.

- A package in the list but not installed is installed.
- A package installed but not in the list is dropped from the profile and from
  the lock.
- A `github:` or `git+` package is read at the commit in the lock, so a newer
  upstream release is ignored until `oku update`.
- A package in the list with no lock entry is resolved fresh and locked.
- On a platform the lock has not seen, oku resolves the package for that
  platform and adds an entry. Entries for other platforms are not touched.

`sync` stops, before downloading anything, when a manifest no longer has the
hash in the lock:

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

If any package fails, `sync` leaves the profile unchanged.

Output is either `already in sync` or a line such as
`profile now holds 12 packages`.

## oku update

```
oku update [name...]
```

Re-resolves packages from their refs and rewrites `oku.lock`. With no names it
updates every package in `oku.toml`. It reads the newest commit of `github:`
and `git+` refs, accepts changed manifests, and accepts a new checksum when the
manifest states one. Then it syncs.

It prints one line per package that changed:

```
hello 1.0.0 -> 2.0.0
ripgrep 14.1.1, manifest changed
tool 1.0.0, checksum changed
```

A name that is not in `oku.toml` fails.

## oku self uninstall

```
oku self uninstall [--keep-list] [--yes]
```

Lists what it will delete, asks once, then removes:

- the data directory (store and profiles)
- the cache directory
- the config directory
- the `oku` binary

| Flag | Effect |
|---|---|
| `--keep-list` | Keeps `oku.toml` and `oku.lock` in the config directory and prints where they are. |
| `--yes`, `-y` | Does not ask. |

Any answer other than `y` or `yes` cancels and removes nothing.

oku never edits shell config files. If the profile `bin` directory is on
`PATH`, uninstall ends by printing that entry so you can delete the line
yourself.

## oku --version, oku --help

`oku --version` prints the version. `--help` works on every command.
