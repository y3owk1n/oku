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

Without `@version`, oku installs the newest version the manifest offers.
`@version` picks one, see [Pinning a version](refs.md#pinning-a-version). The
pin is recorded in `oku.toml` as `{ ref = "...", version = "..." }`.

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
| `the manifest provides version X, not Y` | The `@version` suffix does not match a manifest with a fixed version. |
| `... has no version X, the newest are ...` | The `@version` suffix names a release that upstream does not have. |

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

`remove` refuses a package that only an
[included list](list-and-lock.md#including-other-lists) declares, because oku
does not edit included lists.

## oku list

```
oku list
```

Prints one line per installed package: name, version, ref. With nothing
installed it prints `no packages installed`.

## oku sync

```
oku sync [list-ref]
```

Makes the profile match `oku.toml` and the lists it includes, at the versions
pinned in `oku.lock`.

- A package in the list but not installed is installed.
- A package whose `when` does not match this machine is skipped.
- An included list is read at the commit in the lock.
- A package installed but not in the list is dropped from the profile and from
  the lock.
- A `github:` or `git+` package is read at the commit in the lock.
- Every package is installed at the version in the lock. oku does not ask
  upstream for versions, so a newer release is ignored until `oku update`.
- A package in the list with no lock entry is resolved fresh and locked.
- On a platform the lock has not seen, oku resolves the package for that
  platform and adds an entry. Entries for other platforms are not touched.

`sync` stops, before downloading anything, when a manifest or an included list
no longer has the hash in the lock:

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

If any package fails, `sync` leaves the profile unchanged.

Output is either `already in sync` or a line such as
`profile now holds 12 packages`.

### Setting up a machine from a published list

```
oku sync github:you/machines
```

With a [ref to a list](list-and-lock.md#including-other-lists), `sync` first
sets the machine up from it, then syncs:

1. Reads the list, and the lock beside it at the same commit. The lock's name
   is the list's name with `.lock`, so `oku.toml` pairs with `oku.lock` and
   `base.toml` with `base.lock`.
2. Writes a global `oku.toml` that holds only `include = ["<ref>"]`.
3. Writes a global `oku.lock` that starts from the published lock and pins the
   list itself.

The machine then installs what the published lock pinned, so it ends with the
same store paths as the machine that published it.

```
adopted github:you/machines with 23 locked packages
profile now holds 23 packages
```

Without a lock beside the list, oku prints a notice and resolves every package fresh.

This only works on a machine whose global `oku.toml` is missing or empty. On
any other machine oku refuses and tells you to add the ref to your `include`
array instead. A list ref takes no `@version`.

If the install fails after the two files were written, fix the cause and run
`oku sync` with no argument.

## oku update

```
oku update [name...]
```

Re-resolves packages from their refs and rewrites `oku.lock`. With no names it
updates every package in `oku.toml`. It reads the newest commit of `github:`
and `git+` refs, moves each package to the newest version its manifest offers,
accepts changed manifests, and accepts a new checksum when the manifest states
one. Then it syncs.

A package pinned with `version` in `oku.toml` stays on that version. A version
changes only when you run `update`.

It prints one line per package that changed:

```
hello 1.0.0 -> 2.0.0
ripgrep 14.1.1, manifest changed
tool 1.0.0, checksum changed
```

With no names, `update` also reads included lists fresh, so packages they
gained are installed and packages they lost are dropped. With names, includes
stay pinned.

A name that is in neither `oku.toml` nor its includes fails.

## oku generations

```
oku generations
```

Every command that changes the installed packages writes a new generation.
`generations` lists them, oldest first, with `*` on the active one:

```
  1  2026-09-20 14:02  ripgrep 14.1.1
  2  2026-09-20 14:10  ripgrep 15.2.0
* 3  2026-09-20 14:31  hello 1.0.0, ripgrep 15.2.0
```

## oku rollback

```
oku rollback [generation]
```

Switches the profile and `oku.lock` back to an earlier generation. Without a
number it goes to the generation before the active one.

```
$ oku rollback
generation 2 is active: ripgrep 15.2.0
```

Rollback downloads nothing, because the packages of every generation are still
in the store. It changes two things:

- The `current` link points at the chosen generation.
- oku replaces `oku.lock` with the copy saved in that generation, so a later
  `oku sync` keeps the rolled-back versions.

It does not change `oku.toml`. If your list names a package that the generation
does not hold, rollback prints a notice:

```
~/.config/oku/oku.toml still lists hello, so `oku sync` will install it again. Run `oku remove hello` to drop it.
```

Rollback does not write a new generation. After `oku rollback 1`, generation 1
is active and generations 2 and 3 still exist, so `oku rollback 3` goes forward
again. The next `add`, `remove`, `sync` or `update` that changes something
writes the next number.

Rollback fails for a number that does not exist, for the generation that is
already active, and with no number when the oldest generation is active.

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
