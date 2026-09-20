# Commands

Every command exits with status 0 on success. On failure it prints
`oku: <reason>` to stderr and exits with status 1.

All commands act on the global list and the global profile. Per-project lists
do not exist yet.

## oku add

```
oku add <ref>[@version] [--from-source] [--yes] [--verbose]
```

Installs the package a [ref](refs.md) points at.

1. Fetches the manifest. For a `github:owner/repo` ref whose repo has none, oku
   [infers one](manifest.md#inferred-manifests) and prints it.
2. Picks the first `[[artifact]]` whose `match` fits this machine. With none, or
   with `--from-source`, it [builds from source](manifest.md#build).
3. Downloads the artifact, verifies the checksum, and unpacks it into the
   store. For a build it runs the steps and installs into the store.
4. Activates a new profile generation that includes the package.
5. Writes the package to `oku.toml` and its resolution to `oku.lock`.

Adding a package that is already installed replaces it.

| Flag | Effect |
|---|---|
| `--from-source` | Builds from source even when a prebuilt download fits. `oku.lock` records the choice, so `oku sync` builds too. |
| `--yes`, `-y` | Approves the manifest's build commands without asking, see [Build commands](trust.md#build-commands). |
| `--verbose`, `-v` | Shows the output of build commands as they run. |

`oku sync` and `oku update` take `--yes` and `--verbose` too.

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
| `<name> has no [build], so it cannot be built from source` | `--from-source` on a manifest with artifacts only. |
| `the build needs "<tool>", which is not on PATH` | Install that tool yourself. oku does not install `needs`. |
| `<name> needs approval to run them, and this is not a terminal` | The manifest runs build commands and stdin is not a terminal. Pass `--yes` after reading them. |
| `build.step[N] (run) failed` | A build step failed. The last 40 lines of its output follow. |
| `checksum mismatch for <url>` | The download differs from the expected sha256. Nothing was installed. |
| `<alias> is not a source and <arg> is not a file` | The argument looks like `alias/name`, but no such source exists. See `oku source list`. |
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

## oku gc

```
oku gc [--keep N] [--dry-run]
```

Deletes store paths that no generation of any profile uses, and nothing else.

Old generations keep their packages in the store, so rollback needs no
download. For that reason a plain `oku gc` usually finds little to delete. To
free space, delete old generations first:

| Flag | Effect |
|---|---|
| `--keep N` | First deletes all generations except the newest N. The active generation is always kept, even when it is older than those. N must be at least 1. |
| `--dry-run` | Prints what would be deleted and deletes nothing. |

```
$ oku gc --keep 2
removed generation 1
removed ripgrep-14.0.3-4c8fe21b8d1d13c4 (4.6 MiB)
freed 4.6 MiB from 1 store path
```

With nothing to delete it prints
`nothing to delete, every store path is used by a generation`.

`gc` does not touch the download cache, `oku.toml`, `oku.lock`, or the
temporary directory of an install that is still running. You cannot roll back
to a deleted generation. You can install a deleted package again, and oku
reuses its cached download when the cache still has it.

## oku source

```
oku source add <alias> <ref>
oku source remove <alias>
oku source list
```

Manages your aliases for manifest collections, see [Sources](refs.md#sources).

`add` takes a ref to a whole collection, so it refuses a `#name` or an
`@version`. An alias is lowercase letters, digits, `_` or `-`. Adding an alias
that exists replaces it. `remove` fails for an alias that is not defined.

## oku search

```
oku search <term>
```

Prints the packages in your sources whose name or description contains the
term. Case does not matter. It searches your sources and nothing else.

```
$ oku search grep
core/ripgrep  Recursively search directories for a regex pattern
```

Each line starts with what to pass to `oku add`. With no match it prints
`nothing in your sources matches "<term>"`. With no sources it fails and says
how to add one.

A source that cannot be listed, such as a URL, is skipped with a notice on
stderr. TOML files in a collection that are not manifests are ignored. For a
`github:` source, search reads the repo's file list and then each manifest, so
a large collection takes one request per manifest.

## oku manifest init

```
oku manifest init --from <owner/repo> [-o file] [--force]
```

Writes the manifest that oku [infers](manifest.md#inferred-manifests) from a
GitHub repo's newest release. It is for people who publish a package. Run it,
check the result, and commit it to the repo as `oku.pkg.toml`.

| Flag | Effect |
|---|---|
| `--from` | The repo, as `owner/repo`. Required. |
| `-o`, `--output` | The file to write. Default `oku.pkg.toml`. `-` prints to stdout. |
| `--force` | Replaces the output file when it exists. |

Inference opens the asset for the machine it runs on, so run it on a platform
the project releases for.

## oku manifest lint

```
oku manifest lint [file...]
```

Checks manifests for mistakes before you publish them. Without a file it checks
`oku.pkg.toml`. It prints one line per problem, then `<file>: ok` for each file
with no errors, and exits with status 1 when any file has an error.

```
$ oku manifest lint
oku.pkg.toml: error: line 4: unknown key package.relocateable
oku.pkg.toml: warning: artifact[0]: no sha256 or sha256_url, so users trust the first download
oku: 1 of 1 manifests have errors
```

`oku add` ignores keys it does not know, so that a manifest may use parts of the
schema that oku does not act on yet. Lint knows the whole schema and is strict.

Errors:

- a key that is not in the schema, with its line
- everything `oku add` rejects, such as a bad name, `value` together with `from`,
  or an artifact with none of `bin`, `man` and `completions`
- a template variable that does not exist
- a build step with no type key, or with more than one
- a `run` build step that can run on Windows and sets no `shell`. A step can run
  on Windows unless its `when` names another `os`.
- a `fetch` build step without `sha256`

Warnings, which do not fail the run:

- an artifact with neither `sha256` nor `sha256_url`
- an empty `description`

## oku manifest bump

```
oku manifest bump [file] [--to version] [--repo owner/repo] [--strip-prefix text]
```

Moves a manifest with a fixed `version.value` to the newest upstream release.
Without a file it bumps `oku.pkg.toml`.

```
$ oku manifest bump
ripgrep 14.1.0 -> 15.2.0, 2 checksums updated in oku.pkg.toml
```

It edits the file as text, so comments and layout stay:

- `version.value` becomes the new version.
- A `url` or `sha256_url` that spells the old version out, without
  `{{version}}`, gets the new one.
- Every inline `sha256` is replaced. oku downloads each artifact, for every
  platform, to compute the new digest.

| Flag | Effect |
|---|---|
| `--to` | The version to move to. Default is the newest release. |
| `--repo` | The GitHub repo to read releases from. Default is the repo in the first `github.com/<owner>/<repo>/releases/download/` URL of the manifest. |
| `--strip-prefix` | Text before the version in a tag. Default is `v` when a URL contains `/releases/download/v`, else nothing. |

At the newest version it prints `<name> is already at <version>` and changes
nothing. A manifest that uses `version.from` needs no bump, and bump says so.

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
