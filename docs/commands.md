# Commands

Every command exits with status 0 on success. On failure it prints
`oku: <reason>` to stderr and exits with status 1.

Inside a directory tree that has an `oku.toml`, the commands that read or change
a list act on that [project](projects.md) and print `project <dir>` on stderr.
`--global`, or `-g`, makes them use the global list instead. It works on every
command.

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
| `--service` | Runs the package's [services](services.md) now and at every login, and writes `service = true` to `oku.toml`. |
| `--from-source` | Builds from source even when a prebuilt download fits. `oku.lock` records the choice, so `oku sync` builds too. |
| `--yes`, `-y` | Approves the manifest's build commands without asking, see [Build commands](trust.md#build-commands). |
| `--verbose`, `-v` | Shows the output of build commands as they run. |

`oku sync` and `oku update` take `--yes` and `--verbose` too.

Build commands run in a [sandbox](manifest.md#the-sandbox). When the host
cannot provide one, oku builds anyway and prints a warning on stderr that names
the reason.

Without `@version`, oku installs the newest version the manifest offers.
`@version` picks one, see [Pinning a version](refs.md#pinning-a-version). The
pin is recorded in `oku.toml` as `{ ref = "...", version = "..." }`.

Output:

```
added ripgrep 14.1.1
```

A package that ships [apps or fonts](manifest.md#apps-and-fonts) gets them copied
to your per-user app and font folders, and oku prints one `exposed ...` line on
stderr for each. `oku remove`, `oku rollback` and `oku sync` remove them again
when the package leaves the active generation.

Two more notices go to stderr when they apply. One names the profile `bin`
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
| `dep <ref>: no version satisfies ">=9"` | A dep's version constraint matches nothing upstream. The versions found follow. |
| `dependency cycle: a -> b -> a` | Two manifests depend on each other. |
| `the vendored packages changed: oku.lock pinned ...` | A build's vendor steps downloaded something other than the lock pinned. Nothing was installed. `oku update <name>` accepts it. |
| `build.step[N] (run) failed` | A build step failed. The last 40 lines of its output follow. |
| `checksum mismatch for <url>` | The download differs from the expected sha256. Nothing was installed. |
| `<alias> is not a source and <arg> is not a file` | The argument looks like `alias/name`, but no such source exists. See `oku source list`. |
| `<path> already exists and oku did not put it there` | A package's app or font would overwrite a file of yours. Move it away, or leave the package out. |
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

## oku info

```
oku info <name>
```

Shows what oku knows about an installed package.

```
$ oku info hey
name       hey
version    0.1.5
ref        github:you/recipes#hey
commit     3fce3b5bb0236da2df6d99672afb8a719642eca7
installed  build
store      /home/you/.local/share/oku/store/hey-0.1.5-e7d0aa74a6f65b35
vendored   sha256 9a7aaff0...
programs   hey
```

`installed` is `artifact` or `build`. oku leaves out a line that does not apply.
`manifest` appears for a package whose manifest oku inferred. `impure` appears
for a build that used `network = true`, which means it is not reproducible.
`deps` lists the packages in its closure.

It fails for a name that is not in your list. For a dep, use `oku why`.

## oku why

```
oku why <name>
```

Says why a package is in the store. Deps are not linked into your profile, so
`oku list` does not show them. `why` names the installed packages that depend
on one, directly or through another dep.

```
$ oku why openssl
openssl 3.4.0 is needed by curl 8.11.0
openssl 1.1.1w is needed by legacy-tool 2.0
```

For a package in your own list it prints the ref it came from. It fails for a
name that nothing installed uses.

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

## oku service

```
oku service list
oku service start|stop|restart|status|logs <name>
```

Controls the services of installed packages through launchd or systemd. See
[Services](services.md) for what each one does and for how to enable a service
at login.

## oku hook

```
oku hook <bash|zsh|fish>
```

Prints the shell code that applies a [project's](projects.md) environment. You
load it from your shell's startup file with one line, which `oku hook --help`
and [Projects](projects.md#using-the-projects-programs) show for each shell. oku
never edits that file.

The hook runs `oku env` before each prompt.

## oku env

```
oku env [--shell bash|zsh|fish]
```

Prints the environment changes for the directory you are in: `PATH`, the
`[env]` of installed packages, and the commands that undo what the last run
applied. `--shell` defaults to `bash`. It reads local files only and takes a few
milliseconds.

Outside a project it exports the `[env]` of your global packages. Inside an
allowed project whose profile matches its lock, it also puts the project's `bin`
first on `PATH` and exports its packages' `[env]`. Otherwise it prints a one-line
hint that names `oku allow` or `oku sync`.

## oku allow, oku deny

```
oku allow [dir]
oku deny [dir]
```

`allow` lets the shell hook apply a project's environment. The allow belongs to
the project's `oku.toml` as it is now, so any later edit needs a new `oku allow`.
`deny` removes it. Both default to the project you are in, and fail when
there is no `oku.toml` in the directory or above it.

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

After `oku setup --system`, gc checks the shared store and the old store in the
data directory.

A dep counts as used while any generation holds a package that depends on it.

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

## oku manifest test

```
oku manifest test [file] [--yes] [--verbose] [--keep]
```

Installs a manifest into a throwaway store, to show that it works on this
machine before you publish it. Without a file it tests `oku.pkg.toml`.

- A manifest with a `[build]` is built from source, deps included, in the same
  sandbox a user gets. It prints each step as it finishes.
- A manifest with only artifacts installs the artifact for this machine.
- It ends with the files that would be linked into a profile.

```
$ oku manifest test
[1/3] vendor   ok
[2/3] run      ok
[3/3] install  ok
hey 0.1.5 works on darwin-arm64 (build)
  bin/hey
```

A failing step prints `FAILED`, then the usual build error with the end of the
step's output, and the command exits with status 1.

Your own store, profile, `oku.toml` and `oku.lock` are not touched. Downloads go
to your normal cache. The build commands need approval like any other build, so
pass `--yes` when you test your own manifest repeatedly.

| Flag | Effect |
|---|---|
| `--yes`, `-y` | Approves the build commands without asking. |
| `--verbose`, `-v` | Shows the output of build commands as they run. |
| `--keep` | Keeps the throwaway store and prints the package's path, so you can run what was built. |

It tests one platform, the one you run it on.

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

## oku setup

```
oku setup --system [--yes]
```

Moves the store to a shared root, `/opt/oku`, which is the same path on every
machine. You need this only to share packages built from source between
machines, because a build can write its store path into the files it installs.
Prebuilt downloads work from any store.

`/opt` belongs to root, so this is the one oku command that asks for
administrator rights. It prints the directory it will create and who will own
it, asks, and then runs one command through `sudo`:

```
$ oku setup --system
this creates, with administrator rights:
  /opt/oku  owned by kyle
continue? [y/N] y
the store root is now /opt/oku
run "oku sync" to install your packages there, then "oku gc" to delete the old copies
```

The directory belongs to your user, so nothing after this needs `sudo`. oku
records the root as `store_root` in `config.toml`. When you run oku as root, it
does not call `sudo`.

| Flag | Effect |
|---|---|
| `--system` | Required. Without it `oku setup` fails and does nothing. |
| `--yes`, `-y` | Does not ask. |

Packages you already have stay in the old store until `oku sync` installs them
under `/opt/oku`. Older generations still point at the old store, so rollback
keeps working. `oku gc` covers both stores.

Profiles, the ledger and approvals stay in the data directory.

## oku self uninstall

```
oku self uninstall [--keep-list] [--yes] [--system]
```

Lists what it will delete, asks once, then removes:

- every app, font and service oku set up outside its directories, each listed
  by path. Services are stopped first.
- the data directory (store and profiles)
- the cache directory
- the config directory
- the `oku` binary

| Flag | Effect |
|---|---|
| `--keep-list` | Keeps `oku.toml` and `oku.lock` in the config directory and prints where they are. |
| `--yes`, `-y` | Does not ask. |
| `--system` | With `--yes`, also deletes the shared store root through `sudo`. |

Any answer other than `y` or `yes` cancels and removes nothing.

After [`oku setup --system`](#oku-setup) the list includes the shared store
root, and uninstall asks a second question before it uses `sudo`:

```
  shared store root   /opt/oku (needs administrator rights)
continue? [y/N] y
remove /opt/oku with administrator rights? [y/N] n
oku is uninstalled
left in place, empty:
  /opt/oku
remove it with: sudo rmdir /opt/oku
```

Answering no still deletes everything inside `/opt/oku`, because your user owns
it. Only the empty directory stays. `--yes` alone counts as no.

oku never edits shell config files. Uninstall ends by printing what you should
delete yourself: the oku hook line, with the startup file it found it in, and
the profile `bin` entry when that is on `PATH`. A hook line you leave behind does
nothing once oku is gone.

## oku --version, oku --help

`oku --version` prints the version. `--help` works on every command.
