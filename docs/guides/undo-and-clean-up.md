# Undo a change and free space

This guide puts your machine back the way it was before a change, frees the
disk space that old versions hold, and removes oku itself when you are done
with it.

## See the history

Every `add`, `remove`, `sync` and `update` that changes something writes a
numbered [generation](../how-oku-works.md#generation). A generation holds your
packages, your [home files](dotfiles.md) and your
[OS settings](os-settings.md) together, and a copy of `oku.lock` as it was.

```
$ oku generations
  1  2026-09-23 21:37  1 package   + ripgrep 14.1.1
  2  2026-09-23 21:37  2 packages  + fd 10.5.0
* 3  2026-09-23 21:38  2 packages  ripgrep 14.1.1 -> 15.2.0
```

`*` marks the active generation. Each line has the time, what the generation
holds, and what changed from the one before it:

| Mark | Means |
|---|---|
| `+` | A package, file or setting came. |
| `-` | One went. |
| `->` | A package moved between two versions. |
| `rebuilt` | A new build of the same version. |
| `~` | A file has other bytes, or a setting another value. |

A change with more than six parts ends in `and N more`. After a rollback, the
next generation replaces an older one, and its line starts with that number,
such as `from 1, + hello 1.0.0`.

## Roll back the last change

```
$ oku rollback
generation 2 is active, 2 packages: ripgrep 15.2.0 -> 14.1.1
```

Without a number, `oku rollback` goes to the generation before the active one.
It downloads nothing, because every generation's packages are still in the
store.

A rollback covers everything the generation holds:

- Packages go back to that generation's versions.
- `oku.lock` goes back to that generation's copy, so a later `oku sync` keeps
  the rolled-back versions.
- A `text` or `render` file gets the bytes it had then.
- A setting gets the value it had then, or the value it had before oku first
  wrote it.
- Apps, fonts and services that the generation does not hold go away.

A rollback does not change `oku.toml`. When your list and the generation
disagree, oku says what the next `oku sync` would do:

```
$ oku rollback 1
generation 1 is active, 1 package: - fd
~/.config/oku/oku.toml still lists fd, so `oku sync` will install it again. Run `oku remove fd` to drop it.
```

In the other case, the generation holds a package that your list no longer
names. oku then prints a notice with the `oku add` command that keeps it. oku leaves that notice
out when your list includes other lists.

If you keep `oku.lock` in git, commit it again after a rollback.

## Roll back to a given generation

Pass the number from `oku generations`:

```
$ oku rollback 3
generation 3 is active, 2 packages: + fd 10.5.0, ripgrep 14.1.1 -> 15.2.0
```

A rollback writes no new generation. After `oku rollback 1`, generations 2
and 3 still exist, so `oku rollback 3` goes forward again. The next change
writes the next free number.

A rollback fails, and changes nothing, when:

- the number does not exist, as in
  ``generation 9 does not exist, see `oku generations` ``
- the generation is already active, as in `generation 1 is already active`
- the oldest generation is active and you gave no number
- an app or a font of that generation would overwrite a file that oku did not
  write

## A change that fails undoes itself

`add`, `remove`, `sync`, `update` and `rollback` check everything they can
first: downloads, checksums, builds, templates, secrets and the paths they
write. A failure there leaves the machine as it was. When a later step fails,
such as a service that will not start, oku undoes the steps before it.

If oku is killed halfway, the next of those commands puts the machine back
first and says so:

```
the last change did not finish, so oku put generation 4 back
```

See [Troubleshooting](../troubleshooting.md#the-last-change-did-not-finish)
when it cannot.

## Free disk space

`oku du` shows how much each of oku's directories holds, before you delete anything:

```
$ oku du
store      ~/.local/share/oku/store     1.4 GiB    84 paths, 312.0 MiB unused, 900.0 MiB only in old generations
profiles   ~/.local/share/oku/profiles  2.1 MiB    2 profiles, 14 generations
cache      ~/.cache/oku                 640.0 MiB  downloads 600.0 MiB, git 38.0 MiB, api 2.0 MiB
other      ~/.local/share/oku           1.0 MiB    logs, secrets, trust
total                                   2.0 GiB
`oku gc` frees 312.0 MiB, and deleting ~/.cache/oku is safe
```

`oku du --packages` lists each store path, largest first, with the profile or
package that keeps it. `old:` marks one that only old generations keep, which
`--keep` below frees. See [`oku du`](../reference/commands.md#oku-du).

Old generations keep their packages in the store, so rollback needs no
download. That also means a plain `oku gc` usually finds little to delete. Drop
old generations first with `--keep`:

```
$ oku gc --keep 2
removed generation 1
removed generation 2
removed ripgrep-14.1.1-77da99cdceeb3140 (4.5 MiB)
freed 4.5 MiB from 1 store path
```

- `--keep N` deletes every generation except the newest N, in every profile,
  projects included. The active generation always stays, even when it is older.
  N is at least 1.
- Then gc deletes each store path that no remaining generation uses. A
  [dep](../how-oku-works.md#runtime-dep) counts as used while a package that
  needs it is.
- `--dry-run` prints what it would delete and deletes nothing.

With nothing to delete it says
`nothing to delete, every store path is used by a generation`.

You cannot roll back to a deleted generation. You can add a deleted package
again, and oku reuses its download when the cache still has it.

gc does not touch `oku.toml`, `oku.lock`, the download cache, or an install
that is still running. It refuses to run while an earlier change is
unfinished, until `oku sync` has put the machine back.

The download cache lives in `~/.cache/oku`. Deleting it is safe, and oku
downloads again when it needs a file. [Paths](../reference/paths.md) lists
every directory.

## Remove a package

```
$ oku remove fd
removed fd
```

This writes a new generation without the package and takes it out of
`oku.toml` and `oku.lock`. The package stays in the store until `oku gc`
deletes it, so `oku rollback` brings it back with no download. See
[Remove a package](add-packages.md#remove-a-package).

## Uninstall oku

```
$ oku self uninstall
this removes:
  store and profiles  ~/.local/share/oku
  cache               ~/.cache/oku
  config              oku's own files in ~/.config/oku
  binary              ~/.local/bin/oku
continue? [y/N]
```

It lists what it deletes and asks once. Any answer other than `y` or `yes`
cancels and removes nothing. It then removes:

- every app, font, service and home file oku placed, each listed by path.
  Services stop first.
- every setting oku wrote. Each one gets back the value it had before.
- the data directory, with the store and the profiles
- the cache directory
- `oku.toml`, `oku.lock`, `config.toml` and `signing.key` in the config
  directory
- the `oku` binary

Anything else in the config directory is yours, such as the sources of your
home files, your own manifests or a `.git` directory. Uninstall leaves it and
lists it.

| Flag | Effect |
|---|---|
| `--keep-list` | Keeps `oku.toml` and `oku.lock` and prints where they are. |
| `--yes`, `-y` | Does not ask. |
| `--system` | With `--yes`, also removes what needs `sudo`: the shared store root and everything in [system scope](system-wide.md). |

After [`oku setup --system`](system-wide.md), uninstall asks a second question
before it uses `sudo`. Answering no still empties `/opt/oku`, because your user
owns it, and leaves the empty directory with the `sudo rmdir` command that
removes it. `--yes` alone counts as no.

oku never edits your shell's startup file. Uninstall ends by naming the hook
line and the file it found it in, and the profile `bin` on `PATH` when it is
there, so you can delete them yourself. A hook line left behind does nothing
once oku is gone.

On Windows the binary goes in two steps, see [Windows](windows.md).
