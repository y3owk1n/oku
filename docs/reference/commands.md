# Commands

Every oku command, its flags, what it prints and how it exits. For the tasks
behind the commands, see the [guides](../README.md).

| Group | Commands |
|---|---|
| [Packages](#packages) | [`add`](#oku-add), [`remove`](#oku-remove), [`update`](#oku-update), [`outdated`](#oku-outdated), [`list`](#oku-list), [`info`](#oku-info), [`why`](#oku-why), [`which`](#oku-which), [`shell`](#oku-shell) |
| [The machine](#the-machine) | [`sync`](#oku-sync), [`service`](#oku-service), [`setup`](#oku-setup) |
| [History](#history) | [`generations`](#oku-generations), [`rollback`](#oku-rollback), [`gc`](#oku-gc), [`du`](#oku-du) |
| [Projects](#projects) | [`allow`, `deny`](#oku-allow-oku-deny), [`hook`](#oku-hook), [`env`](#oku-env), [`exec`](#oku-exec) |
| [Manifests](#manifests) | [`manifest init`](#oku-manifest-init), [`lint`](#oku-manifest-lint), [`test`](#oku-manifest-test), [`bump`](#oku-manifest-bump), [`hash`](#oku-manifest-hash) |
| [Sources, caches and keys](#sources-caches-and-keys) | [`source`](#oku-source), [`search`](#oku-search), [`cache`](#oku-cache), [`key`](#oku-key) |
| [Self and diagnostics](#self-and-diagnostics) | [`doctor`](#oku-doctor), [`self update`](#oku-self-update), [`self uninstall`](#oku-self-uninstall), [`--version`, `--help`, `completion`](#oku---version-oku---help-oku-completion) |
| Reading the output | [Global flags](#global-flags), [Exit codes](#exit-codes), [JSON output](#json-output), [Error messages](#error-messages), [Output](#output) |

## Global flags

These work on every command.

| Flag | Effect |
|---|---|
| `--global`, `-g` | Uses the global list even inside a [project](../how-oku-works.md#project). |
| `--json` | Prints data as JSON on stdout, on the commands that print data. See [JSON output](#json-output). |
| `--help`, `-h` | Prints the help of the command. |

Inside a directory tree that has an `oku.toml`, the commands that read or
change a list act on that project and print `project <dir>` on stderr, once
per command. These are `add`, `remove`, `list`, `sync`, `update`, `outdated`,
`info`, `why`, `which`, `generations` and `rollback`. `gc`, `source`, `search`,
`manifest` and `self` use no list. See [Projects](../guides/projects.md).

## Exit codes

| Code | When |
|---|---|
| `0` | The command succeeded. |
| `1` | The command failed. oku prints `oku: <reason>` on stderr, and the lines after it say what to do when there is something to do. |
| the command's code | `oku exec` and `oku shell -- <command>` exit with the code of the command they ran, and add no message. |

`oku doctor` and `oku manifest lint` exit with `1` when they find a problem.
`--json` does not change an exit code. A command that gets the wrong number of
arguments prints its usage line and exits with `1`.

`add`, `remove`, `sync`, `update` and `rollback` change the machine all the way
or not at all. See [the transaction model](paths.md#how-a-change-applies).

## Packages

### oku add

```
oku add <ref>[@version]... [flags]
```

Installs the package that a [ref](refs.md) points at, and writes it to
`oku.toml` and `oku.lock`.

| Flag | Effect |
|---|---|
| `--system` | Puts the package's apps, fonts and services in [system scope](../how-oku-works.md#system-scope), and writes `system = true` to `oku.toml`. oku lists the files, asks, and uses `sudo`. |
| `--service` | Runs the package's services now and at every login, and writes `service = true` to `oku.toml`. |
| `--from-source` | Builds from source even when a prebuilt download fits. `oku.lock` records the choice, so `oku sync` builds too. |
| `--asset <glob>` | For a repo with no manifest, the release asset to use. A glob that names one asset takes it for this machine. One that names more takes the best of them on every platform. Other platforms take the asset of the same program. |
| `--bin <name>` | For a repo with no manifest, or a URL of the download itself, the file name of a program inside it. Give it once per program. oku records `--asset` and `--bin` in `oku.toml` and `oku.lock`, and `oku update` infers the next version with them. |
| `--yes`, `-y` | Approves the manifest's build commands, or the command that generates an artifact's completions, without asking. See [approvals](security.md#approve-build-commands). |
| `--accept-key` | Accepts a manifest whose `signing_key` differs from the one in `oku.lock`. See [signing keys](security.md#signing-keys-of-a-manifest). |
| `--verbose`, `-v` | Shows the output of build commands as they run, and prints a manifest that oku inferred. |
| `--when <key=value,...>` | Limits the package to matching platforms and writes `when` to `oku.toml`, such as `--when os=linux,libc=glibc`. Give it once per table of an array. When it leaves out this machine, `add` pins the package for the `[lock]` platforms it matches and installs nothing. |
| `--plan` | Prints what oku found for the ref and what `add` would do, and changes nothing. See [A plan](#a-plan). |
| `--manifest` | Prints the manifest `add` would use and changes nothing. See [A plan](#a-plan). |

What it does, in order:

1. Fetches the manifest. For a forge ref whose repo has none, oku
   [infers one](manifest.md) and says so.
2. Picks the first `[[artifact]]` whose `match` fits this machine. With none,
   or with `--from-source`, it builds from source.
3. Downloads the artifact, verifies the checksum and unpacks it into the
   store. A build runs its steps in the [sandbox](security.md#the-build-sandbox)
   and installs into the store.
4. Activates a new [generation](../how-oku-works.md#generation) that includes
   the package.
5. Writes the package to `oku.toml` and its resolution to `oku.lock`.

Rules:

- Several refs install one after another, one generation each. A failure
  stops the command, and the packages added before it stay.
- `--asset` and `--bin` describe one download, so they take one ref.
- Adding a package that is already installed replaces it.
- Without `@version`, oku installs the newest version. `@version`, a range or
  a prefix picks one, see [Pin a version](refs.md#pin-a-version). oku writes
  it to `oku.toml` as `{ ref = "...", version = "..." }`.
- A bare word that is no file here fails with `there is no file named <word>
  here`, and says how to write a ref.
- When the host cannot give a build a sandbox, oku builds anyway and prints a
  warning that names the reason.

```
$ oku add github:BurntSushi/ripgrep@14.1.1
github:BurntSushi/ripgrep has no manifest, so oku inferred one from its newest release, --verbose prints it
added ripgrep 14.1.1
```

On stderr oku also prints, when they apply:

- one `exposed ...` line for each app or font it copies to your per-user app
  and font folders
- a line that names the profile `bin` directory, with the hook line to add,
  when that directory is not on `PATH`
- a line that says oku trusted a download because the manifest publishes no
  checksum, see [trust on first use](security.md#trust-on-first-use)

A failed `add` leaves the previous generation active, and the list, the lock
and every app, font and service unchanged. The messages it can stop with are
in [Error messages](#error-messages).

#### A plan

`oku add <ref> --plan` fetches the manifest, or infers one, picks the version
and the download or build for this machine, and prints what it found. It
takes the other flags of `add`, such as `@version`, `--from-source`, `--asset`
and `--bin`, and plans what they would do.

```
$ oku add github:sharkdp/fd --plan
name       fd
version    10.5.0
homepage   https://github.com/sharkdp/fd
ref        github:sharkdp/fd
manifest   inferred by oku, because the ref has none
asset      fd-v10.5.0-aarch64-apple-darwin.tar.gz
install    download for darwin-arm64
url        https://github.com/sharkdp/fd/releases/download/v10.5.0/fd-v10.5.0-aarch64-apple-darwin.tar.gz
verify     sha256 the release publishes
programs   fd
platforms  darwin-amd64, darwin-arm64, linux-amd64-glibc, linux-amd64-musl, linux-arm64-glibc, linux-arm64-musl
installed  no
list       ~/.config/oku/oku.toml
`oku add github:sharkdp/fd --manifest` prints the inferred manifest
plan: nothing was changed
```

- `install` is `download`, `build from source`, or `pin in oku.lock only`
  when nothing fits this machine but a `[lock]` platform does.
- `verify` says how oku checks the download. `none` means oku
  [trusts the first download](security.md#trust-on-first-use).
- `commands` appears when the manifest runs commands, in a build or to
  generate completions. oku [asks you to approve](security.md#approve-build-commands)
  them before it runs them.
- `also fits` lists the other assets that fit this machine, and a line names
  the `--asset` flag that picks one.
- `installed` names the version that `oku.lock` holds now.
- Other rows, such as `deps`, `build deps`, `needs`, `apps`, `services` and
  `env`, appear when the manifest has them. `--json` prints the same fields.

`oku add <ref> --manifest` prints the manifest `add` would use, ready to save
as a file. A manifest that oku infers covers every platform, as
[`oku manifest init`](#oku-manifest-init) does, and a published one prints as
fetched.

A plan fails where `add` would fail. That covers a ref, a version or an asset
that does not exist, and a download, checksum file or signature that the
server does not have. oku checks those files without downloading them.

Neither writes to the list, the lock, the store or a generation, and neither
waits for another oku process. Inference still opens a release asset to find
the programs inside it, so a plan of a repo with no manifest downloads that
asset. Neither runs a build step or a command from the manifest.

### oku remove

```
oku remove <name>...
```

Drops packages from the profile, from `oku.toml` and from `oku.lock`, in one
generation.

The packages' files stay in the store, so adding one again needs no download.
`oku gc` deletes them once no generation uses them.

- It also works when only the list or the lock still names the package, as
  after the data directory was deleted.
- A name found nowhere stops the command before anything changes, with
  `<name> is not installed, so nothing was removed`.
- It refuses a package that only an included list declares, because oku does
  not edit included lists. Remove it there, or take the include out.
- It refuses a package written as its own `[packages.<name>]` table. Edit that
  by hand.

### oku update

```
oku update [name...] [flags]
```

Resolves packages from their refs again, rewrites `oku.lock`, then syncs.
With no names it updates every package of the list.

| Flag | Effect |
|---|---|
| `--dry-run` | Checks everything and prints what would change. It changes only the store and the cache. See [A dry run](#a-dry-run). |
| `--system` | Also applies system-scope apps, fonts and services, which needs administrator rights. |
| `--yes`, `-y` | Approves build commands without asking. |
| `--accept-key` | Accepts a changed `signing_key`. |
| `--verbose`, `-v` | Shows build output, and a manifest that oku inferred. |

It reads the newest commit of forge and `git+` refs, moves each package to the
newest version it may take, accepts changed manifests, and accepts a new
checksum when the manifest states one.

- A package with a `version` in `oku.toml` moves to the newest version that
  it allows. An exact version stays where it is, and `^1.4` moves to the
  newest 1.x.
- A package that follows a moving tag moves when the tag points at another
  commit.
- With no names it also reads included lists fresh, so packages they gained
  are installed and packages they lost are dropped. With names, includes stay
  pinned, so it never adds or drops packages.
- A name that is in neither `oku.toml` nor its includes fails.
- `oku update` is the only command that accepts a changed manifest or
  checksum. See [what the lock pins](lock.md#what-each-pin-does).

It prints one line per package that changed:

```
hello 1.0.0 -> 2.0.0
ripgrep 14.1.1, manifest changed
tool 1.0.0, checksum changed
```

When nothing changed it prints `already in sync`.

### oku outdated

```
oku outdated
```

Lists the packages that have a newer version than `oku.lock` pins. It
downloads no package and changes nothing.

```
$ oku outdated
name     locked  newest  latest  ref
ripgrep  14.1.1  14.1.1  15.2.0  github:BurntSushi/ripgrep
`oku update` takes the newest versions. To take a latest beyond them, change its version in oku.toml
```

| Column | Meaning |
|---|---|
| `locked` | The version `oku.lock` pins. |
| `newest` | The newest version that the package's `version` in `oku.toml` allows. `oku update` takes it. |
| `latest` | The newest release. It differs from `newest` when `version` leaves it out, as `^2` leaves out 3.0.0. |
| `ref` | The ref from the list. |

- A package shows when either `newest` or `latest` is newer than the lock.
- oku reads an inferred manifest from `oku.lock`, and any other manifest from
  its ref.
- When every package is at its newest version, oku says so.
- When a version source cannot answer, oku lists the rest and ends with an
  error that names the package.
- oku looks up 16 packages at once, or the number in
  [`OKU_PARALLEL`](paths.md#environment-variables).

`--json` prints only the packages that have a newer version, which suits a bot
that opens a pull request. See [CI](../guides/ci.md).

### oku list

```
oku list [--files | --settings]
```

Prints one line per installed package: name, version and ref.

| Flag | Effect |
|---|---|
| `--files` | Lists the [`[files]`](oku-toml.md#files) entries that apply to this machine instead: the target as the list writes it, the kind (`link`, `text`, `render` or `secret`), the source, and the directory of the list that declares it. |
| `--settings` | Lists the [settings](oku-toml.md#settings-tables) of this OS instead: domain, key, the value the list wants, and the value the key had before oku wrote it, or `not set`. |

```
$ oku list
fd       10.5.0  github:sharkdp/fd
ripgrep  14.1.1  github:BurntSushi/ripgrep
```

On a terminal a last column says `service` or `system` for a package that has
one, and a footer counts the packages and names the list. With nothing
installed it says so and points at `oku add`.

### oku info

```
oku info <name>
```

Shows what oku knows about an installed package.

```
$ oku info ripgrep
name       ripgrep
version    14.1.1
ref        github:BurntSushi/ripgrep
commit     3fce3b5bb0236da2df6d99672afb8a719642eca7
installed  artifact
store      /home/you/.local/share/oku/store/ripgrep-14.1.1-77da99cdceeb3140
manifest   inferred by oku from the repo's releases
programs   rg
```

| Line | Meaning |
|---|---|
| `installed` | `artifact` or `build`. On a terminal it reads `from a release download` or `built from source`. |
| `commit` | The commit oku read the manifest at. A terminal shows its first 12 characters. |
| `manifest` | Present when oku inferred the manifest. |
| `vendored` | The digest of what a build's vendor steps downloaded. |
| `impure` | Present for a build that used `network = true`, which is not reproducible. |
| `deps` | The packages in its closure. |
| `programs` | The programs it puts in the profile. |

oku leaves out a line that does not apply. It fails for a name that is not in
your list. For a dep, use `oku why`.

### oku why

```
oku why <name>
```

Says why a package is in the store. oku does not link deps into your
profile, so `oku list` does not show them.

```
$ oku why openssl
openssl 3.4.0 is needed by curl 8.11.0
openssl 1.1.1w is needed by legacy-tool 2.0
$ oku why ripgrep
ripgrep 14.1.1 is in your list, from github:BurntSushi/ripgrep
```

It names the installed packages that depend on it, directly or through
another dep. It fails for a name that nothing installed uses.

### oku which

```
oku which <program>
```

Says which package provides a program and which file in the store it runs.
Inside a project it looks in the project's profile first, then in the global
one.

```
$ oku which rg
program  rg
package  ripgrep 14.1.1
path     /home/you/.local/share/oku/store/ripgrep-14.1.1-77da99cdceeb3140/pkg/rg
```

When a program of the same name earlier on `PATH` runs in place of oku's, it
says so on stderr and points at `oku doctor`. For a program oku did not
install it fails and names what `PATH` runs.

### oku shell

```
oku shell <ref>... [-- command [args...]]
```

Puts packages in the store and starts `$SHELL` with their programs first on
`PATH` and their `[env]` set. It changes no `oku.toml`, no `oku.lock` and no
profile.

| Flag | Effect |
|---|---|
| `--yes`, `-y` | As in `oku add`. |
| `--accept-key` | As in `oku add`. |
| `--verbose`, `-v` | As in `oku add`. |

```
$ oku shell github:BurntSushi/ripgrep github:sharkdp/fd
oku shell with github:BurntSushi/ripgrep, github:sharkdp/fd, leave it with exit
$ rg --version
ripgrep 15.2.0
$ exit
```

- After `--` oku runs that command in place of a shell, finds it on the new
  `PATH`, and exits with its exit code.
- A ref takes `@version` as in `oku add`.
- Inside, `OKU_SHELL` holds the refs. Your shell's startup files still run,
  so one that resets `PATH` removes the packages from it.
- The packages stay in the store and no generation uses them, so `oku gc`
  deletes them. With a published checksum the next `oku shell` reuses them.
  Without one oku downloads the file again and trusts it again, because
  `shell` writes no lock.

## The machine

### oku sync

```
oku sync [list-ref] [flags]
```

Makes the profile match `oku.toml` and the lists it includes, at the versions
pinned in `oku.lock`. It also applies `[files]`, `[secrets]` and the settings
tables.

| Flag | Effect |
|---|---|
| `--dry-run` | Checks everything and prints what would change. It changes only the store and the cache. See [A dry run](#a-dry-run). |
| `--locked` | Fails when `oku.lock` would change. For CI. |
| `--rebuild <name>` | Builds the package again even though the store holds its build. Repeat the flag, or separate names with commas. |
| `--system` | Also applies system-scope apps, fonts and services, which needs administrator rights. |
| `--yes`, `-y` | Approves build commands without asking. |
| `--accept-key` | Accepts a changed `signing_key`. |
| `--verbose`, `-v` | Shows build output, and a manifest that oku inferred. |

What it does:

- Installs a package that is in the list and not installed, and drops one that
  is installed and not in the list, from the profile and from the lock.
- Installs every package at the version, commit and checksum in the lock. It
  asks upstream for nothing, so a newer release waits for `oku update`.
- Resolves a package with no lock entry fresh and locks it.
- On a platform the lock has not seen, resolves the package for that platform
  and adds an entry. It does not touch the entries of other platforms.
- Skips a package whose `when` does not match this machine, and pins it for
  the platforms of [`[lock]`](oku-toml.md#lock) that the lock lacks.
- Reads an included list at the commit in the lock.
- Changes files in system scope only with `--system`, after it lists them and
  you agree. Without the flag it lists them and leaves them.

It stops before it downloads anything when a manifest or an included list no
longer has the hash in the lock:

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

When several packages drifted, the error lists each and ends with one
`oku update` that names them all. An `oku update` of some names that finds
another package drifted writes nothing, and its error names the packages you
gave too. After a terminal showed a checked row for a package, a failed `sync`
or `update` ends with `nothing was installed, and the next run reuses the
downloads above`.

`--locked` checks before any download, so a locked sync never trusts a
download on first use:

```
oku: ./oku.lock does not pin ripgrep for linux-amd64-glibc
run `oku sync` without --locked, and commit oku.lock
```

It also fails with `oku.lock is out of date` when the lock holds a package
that left the list, or lacks a platform that `[lock]` names. Line endings do
not count, so a lock and a local manifest that git checked out with CRLF pass.

`--rebuild` rules:

- The new build takes the place of the old one under the same store path, so
  every generation gets it. oku moves the old build aside first, and puts it
  back when the new build fails. When oku is killed during the build, the
  next `sync` puts it back. `oku gc` keeps the old build until then.
- While the build runs, the package's programs do not work.
- The lock still applies. A build that vendors other packages than `oku.lock`
  pins stops, and the old build stays.
- It does not rebuild deps, and it refuses a package that is a download on
  this machine.
- A service of the package keeps the old program until
  `oku service restart <name>`.
- oku prints `eza 0.23.5, built again` for each one.

Use it for a build from an older oku that has no `vendor_sha256` in the lock,
or after a change on the machine that a build depends on, such as a new
compiler.

`sync` and `update` install 16 packages at once.
[`OKU_PARALLEL`](paths.md#environment-variables) sets another number. Builds from source run one at a time. A package that the
store holds at the locked version needs no request to its server, so a sync
with nothing to do works offline.

When any package fails, `sync` leaves the profile unchanged, and oku takes back
any app, font or service it had changed.

Output, after one line per file, secret, setting, app, font or service it
placed, changed or removed:

```
wrote ~/.config/nvim
changed ~/.ssh/config
set com.apple.dock tilesize
restored com.apple.dock autohide
removed the app ~/Applications/Foo.app
profile now holds 12 packages, 8 files, 20 settings, generation 7, 12s
```

- `already in sync` when nothing changed.
- One line per package that changed: a fresh install, `ripgrep 14.1.1 ->
  15.2.0` for a new version, or a note for a rebuild, a manifest or checksum
  change, a pin for another platform, or a package that left the list.
- Packages whose manifests oku inferred share one note, such as `4 packages
  have no manifest, so oku inferred one for each from its newest release: fzf,
  gofumpt, jq, just`.
- A first sync of an empty list writes no generation and says `nothing to
  sync`.

#### A dry run

`oku sync --dry-run` and `oku update --dry-run` run every check of the real
command and then stop. They resolve, download, verify and build, render every
template, decrypt every secret in memory, and look for files that oku did not
write.

```
$ oku sync --dry-run
would remove the package fd
would install ripgrep 15.2.0
would change the content of /home/you/.config/ghostty/config
would write the file /home/you/.config/git/config
would write the setting com.apple.dock tilesize
dry run: nothing was changed
```

- It fails with the error the real command would give.
- It writes no generation, no `oku.lock`, no file, no service and no setting.
- Downloads and builds stay in the store and the cache, where the real command
  finds them. `oku gc` deletes the unused ones.
- When an earlier change did not finish, it stops and says to run `oku sync`
  first.
- With nothing to change it prints `dry run: already in sync`.

#### Adopt a published list

```
oku sync github:you/machines
```

With a ref to a list, `sync` sets the machine up from that list, then syncs.
See [New machine](../guides/new-machine.md) for when to use it.

1. Reads the list, and the lock beside it at the same commit. The lock's name
   is the list's name with `.lock`, so `oku.toml` pairs with `oku.lock` and
   `base.toml` with `base.lock`.
2. Writes a global `oku.toml` that holds only `include = ["<ref>"]`.
3. Writes a global `oku.lock` that starts from the published lock and pins the
   list itself. A relative path in the published lock, such as
   `./packages/node.toml`, becomes that file of the repo at the list's commit.

```
$ oku sync github:you/machines
adopted github:you/machines with 23 locked packages
profile now holds 23 packages
```

- Without a lock beside the list, oku prints a notice and resolves every
  package fresh.
- It works only when the global `oku.toml` is missing or empty. Otherwise oku
  refuses and tells you to add the ref to your `include` array. Inside a
  project it refuses too.
- A list ref takes no `@version`.
- When the install fails, oku removes the two files again.

### oku service

```
oku service list
oku service start|stop|restart|logs <name> [--system]
oku service status <name>
```

Controls the services of installed packages through launchd, systemd or Task
Scheduler. See [Services](../guides/services.md).

| Subcommand | Effect |
|---|---|
| `list` | Every service of your installed packages, the package it belongs to, and its state. |
| `start <name>` | Starts it. For a service that is not enabled, this lasts until you log out. |
| `stop <name>` | Stops it. An enabled service starts again at the next login. |
| `restart <name>` | Stops it and starts it again. |
| `status <name>` | Whether it is running and whether it starts at login. It never fails for a stopped service. |
| `logs <name>` | The last 50 lines it printed. |

| Flag | Effect |
|---|---|
| `--system` | On `start`, `stop`, `restart` and `logs`: acts on a service in system scope, with administrator rights. `start`, `stop` and `restart` of such a service fail without it. On Linux, `logs --system` reads the system journal through `sudo`. |

```
$ oku service list
postgres  postgres  running, starts at login, pid 9449
$ oku service stop postgres
postgres: stopped
```

- An unknown name fails and lists the services you have.
- `start` and `restart` look again one second later. When the program has
  exited by then, the command fails and says where to look, such as
  `oku: atuin started and then exited, look at ~/.local/share/oku/logs/atuin.log`.
  On Linux the hint is a `journalctl` command. Windows has no log to point at.
- `list` and `status` mark a system service with `system scope`.

### oku setup

```
oku setup --system [--yes]
```

Moves the store to a shared root that is the same path on every machine, so
builds from source can be shared. See [Build caches](../guides/build-caches.md).

| Flag | Effect |
|---|---|
| `--system` | Required. Without it `oku setup` fails and does nothing. |
| `--yes`, `-y` | Does not ask. |

```
$ oku setup --system
this creates, with administrator rights:
  /opt/oku  owned by you
continue? [y/N] y
the store root is now /opt/oku
run "oku sync" to install your packages there, then "oku gc" to delete the old copies
```

- The root is `/opt/oku`, and `%ProgramData%\oku` on Windows.
- It is the one command that asks for administrator rights for itself. It
  runs one command through `sudo`, or through the consent prompt on Windows.
  Run as root, it does not call `sudo`.
- The directory belongs to your user, so nothing after this needs `sudo`.
- oku writes `store_root` to [`config.toml`](oku-toml.md#configtoml).
- Packages stay in the old store until `oku sync` installs them under the new
  root. Older generations still point at the old store, so rollback works, and
  `oku gc` covers both stores.
- Profiles, the ledger and approvals stay in the data directory.

## History

### oku generations

```
oku generations
```

Lists the profile's [generations](../how-oku-works.md#generation), oldest
first, with `*` on the active one.

```
  1  2026-09-20 14:02  1 package                        + ripgrep 14.1.1
  2  2026-09-20 14:10  1 package                        ripgrep 14.1.1 -> 15.2.0
  3  2026-09-20 14:31  2 packages                       + hello 1.0.0
* 4  2026-09-20 14:40  2 packages, 3 files, 1 setting   + ~/.config/nvim, + ~/.gitconfig, + ~/.ssh/config, + com.apple.dock tilesize
```

Each line has the time, what the generation holds, and what changed from the
generation it replaced.

| Mark | Meaning |
|---|---|
| `+` | A package, file or setting that came. |
| `-` | One that went. |
| `->` | Between two versions. |
| `rebuilt` | A new build of the same version. |
| `~` | A file with other bytes, or a setting with another value. |
| `and N more` | The rest of a change with more than six parts. |
| `empty` | A first generation that holds nothing. |
| `from N, ...` | The generation after a rollback, which replaced generation N. |
| `replaced N, which is deleted` | A generation whose replaced generation `oku gc --keep` deleted. |

Generations from an older oku do not record what they replaced, so they
compare with the one numbered before.

### oku rollback

```
oku rollback [generation]
```

Switches the profile and `oku.lock` back to an earlier generation. Without a
number it goes to the generation before the active one.

```
$ oku rollback
generation 1 is active, 1 package: - fd
~/.config/oku/oku.toml still lists fd, so `oku sync` will install it again. Run `oku remove fd` to drop it.
```

- It downloads nothing, because every generation's packages are still in the
  store.
- It points `current` at that generation, and replaces `oku.lock` with the
  copy saved in it, so a later `oku sync` keeps those versions.
- It puts files, settings, apps, fonts and services back as that generation
  had them, and names each one it changed.
- It does not change `oku.toml`. A package the list names and the generation
  lacks gets the notice above. A package the generation holds and the list no
  longer names gets the other notice, unless the list includes other lists:
  ``~/.config/oku/oku.toml does not list fzf, so `oku sync` will remove it
  again. Run `oku add github:junegunn/fzf` to keep it.``
- It writes no new generation. After `oku rollback 1`, `oku rollback 3` goes
  forward again. The next command that changes something builds on
  generation 1 and writes the number after the highest, and 2 and 3 stay.
- It lasts until the next `oku sync`, which applies the list again. To undo a
  change for good, undo it in the list, see
  [Rollback or git](../guides/undo-and-clean-up.md#rollback-or-git).

It fails for a number that does not exist, for the active generation, and
with no number when the oldest is active. It also fails, before it changes
anything, when an app or a font of that generation would overwrite a file
that oku did not write.

### oku gc

```
oku gc [--keep N] [--cache] [--dry-run]
```

Deletes store paths that no generation of any profile uses, and what a killed
oku process left in the system's temporary directory. It also shares the
identical files of store paths that an older oku installed, see
[the store](paths.md#the-store).

| Flag | Effect |
|---|---|
| `--keep N` | First deletes all generations of each profile except the newest N. The active generation always stays. N is at least 1. |
| `--cache` | Also deletes the downloads in the cache that no kept store path was made from, see below. |
| `--dry-run` | Prints what would be deleted and deletes nothing. |

```
$ oku gc --keep 2
removed generation 1
removed ripgrep-14.0.3-4c8fe21b8d1d13c4 (4.6 MiB)
freed 4.6 MiB from 1 store path
```

- Old generations keep their packages, so a plain `oku gc` usually finds
  little. With nothing to delete it prints `nothing to delete, every store
  path is used by a generation`. When `--keep` deleted generations and no
  store path became unused, it prints `every store path is still used by a
  generation`.
- The size of a store path is what deleting it frees. A file that a kept
  store path shares counts as nothing, and a file that several deleted store
  paths share counts once.
- The first `oku gc` after an upgrade shares the files of older store paths,
  which reads each file once. It says how many store paths and what that saved,
  and `--dry-run` says how many it would share:

  ```
  $ oku gc
  shared the identical files of 109 store paths (728.4 MiB)
  freed 728.4 MiB from identical files
  ```
- Windows refuses to delete a program that runs. When a program of an old
  generation or of an unused store path still runs, gc moves its files into a
  `trash` folder beside the profiles or the store, and a later gc deletes them
  once the program has ended.
- The kept generations keep their numbers, and no number is used twice.
- A dep counts as used while any generation holds a package that depends on
  it.
- After `oku setup --system` it checks the shared store and the old one.
- In the system's temporary directory it deletes the `oku-*` entries whose
  oku process has ended, such as a half-done build. On macOS it first
  detaches a disk image that such a process left mounted. It skips the
  entries of a process that still runs and those of another user.
- Without `--cache` it does not touch the cache directory. It never touches
  `oku.toml` or `oku.lock`.
- With `--cache` it keeps each download that a kept store path was made from,
  as its `oku-meta.toml` records. So oku can install any generation you can
  roll back to again offline. It deletes the other downloads, such as old
  versions and the downloads of other lock platforms, and the index of
  downloads by url. It skips a file less than a day old, which a run that has
  not written its lock yet may need. It leaves `git/` and `api/` alone.

```
$ oku gc --keep 1 --cache
removed generation 1
removed ripgrep-14.0.3-4c8fe21b8d1d13c4 (4.6 MiB)
removed 38 files from the download cache (1.9 GiB)
freed 1.9 GiB from 1 store path and 38 cached files
```
- It refuses to run while an unfinished change waits to be put back.
- You cannot roll back to a deleted generation.

### oku du

```
oku du [--packages]
```

Shows how much disk oku uses and where. It reads and changes nothing.

```
$ oku du
store      ~/.local/share/oku/store     6.2 GiB    142 paths, 1.2 GiB unused, 2.9 GiB only in old generations
profiles   ~/.local/share/oku/profiles  8.1 MiB    3 profiles, 41 generations
cache      ~/.cache/oku                 3.4 GiB    downloads 3.3 GiB, api 52.0 MiB
apps       ~/Applications               1.9 GiB    4 apps
fonts      ~/Library/Fonts              31.5 MiB   12 fonts
other      ~/.local/share/oku           1.2 MiB    logs, secrets, trust
total                                   11.5 GiB
`oku gc` frees 1.2 GiB, and `oku gc --cache` frees 2.8 GiB
```

The last line says what `oku gc` frees, and what `oku gc --cache` frees, which
includes it.

| Area | What it holds |
|---|---|
| `store` | Every store path, with a file that store paths share counted once. `unused` is what `oku gc` frees. `only in old generations` is what `oku gc --keep` can free once those generations go. After `oku setup --system` there is a row for the shared store and one for the old store. |
| `profiles` | The generations of every profile. |
| `cache` | Downloads, `git+` clones and API answers. |
| `apps`, `fonts` | The copies oku placed outside the store, from the ledger. Absent when there are none. |
| `other` | The rest of the data directory, such as secrets, services and logs. |
| `temporary` | What killed oku processes left in the system's temporary directory, which `oku gc` also deletes. Absent when there is none. |

A file with more than one hard link counts once, so the links of a Windows
generation count in the store.

| Flag | Effect |
|---|---|
| `--packages` | Lists each store path instead, largest first, with its size and what keeps it. |

```
$ oku du --packages
firefox-156.0-724bce82a5308f1f        506.6 MiB  global
firefox-155.0.2-f70cc181a8c66932      498.3 MiB  old: global
rust-1.98.1-53fb62887997e999          434.9 MiB  dep of eza, pngquant
gopls-0.23.0-8a1c3e2f40b9d7a6         39.1 MiB   ~/code/api, ~/code/web
fd-10.4.0-4c8fe21b8d1d13c4            3.1 MiB    unused
```

The last column names the profiles whose generations hold the path, and the
packages that depend on it. A project shows as its directory once you have
[allowed](#oku-allow-oku-deny) it, and as `project-<hash>` before. `old:`
marks a path that only generations other than the active ones hold, and
`unused` one that no generation holds.

## Projects

### oku allow, oku deny

```
oku allow [dir]
oku deny [dir]
```

`allow` lets the shell hook apply a project's environment. `deny` takes that
back.

- An allow belongs to the project's `oku.toml` as it is now, and to each
  [`.env` file](oku-toml.md#envfile) and
  [`oku.<env>.toml` or `oku.local.toml`](oku-toml.md#okuenvtoml-and-okulocaltoml)
  that git tracks. Any later edit, such as a `git pull` that changes one of
  them, needs a new `oku allow`.
- A file of these that git does not track is yours, and you change it without a
  new allow. `oku allow` names each file and says which kind it is.
- Both default to the project you are in, and fail when there is no
  `oku.toml` in the directory or above it.

```
$ oku allow
allowed /home/you/work/api
```

### oku hook

```
oku hook <bash|zsh|fish|pwsh>
```

Prints the shell code that sets oku up in a shell. You load it with one line
in your shell's startup file, which `oku hook --help` prints for each shell.
oku never edits that file.

The code:

- puts the global profile's `bin` and the directory of `oku` first on `PATH`,
  and moves them to the front when they are there already
- loads the completions of your installed programs and of `oku`
- runs `oku env` before each prompt, which applies an allowed project's
  environment
- in bash and zsh, clears the paths the shell remembers for programs before
  each prompt. In a shell that was open already, a program you add runs from
  the profile, and one you remove runs from the next directory on `PATH`
- in PowerShell, wraps your `prompt` function and keeps `$LASTEXITCODE`

Completions come from the profile's `share/completions`.

| Shell | How it loads them |
|---|---|
| bash | Sources every file. |
| zsh | Puts the directory on `fpath` and registers each file once `compinit` has run, so the hook line may come before or after `compinit`. |
| fish | Adds the directory to `fish_complete_path`, and reads a file when you first complete that program. |
| PowerShell | Loads the completions of `oku` only. |

### oku env

```
oku env [--shell bash|zsh|fish|pwsh] [--json | --dotenv]
```

Prints the environment changes for the current directory. This is what the
hook runs before each prompt, and what an `.envrc` of direnv can `eval`.

| Flag | Effect |
|---|---|
| `--shell` | The shell to write for. Default `bash`. |
| `--json` | Print every variable the directory sets as JSON, with `null` for one it unsets. |
| `--dotenv` | Print every variable the directory sets as a `.env` file. A variable it unsets is left out. |

- Outside a project it exports the `[env]` of your global packages and of your
  global `oku.toml`.
- Inside an allowed project whose profile matches its lock, it also puts the
  project's `bin` first and exports the `[env]` of its packages and of its
  `oku.toml`. See [\[env\]](oku-toml.md#env).
- Otherwise it prints a one-line hint that names `oku allow` or `oku sync`.
- Inside a project that applies, it sets `OKU_PROJECT` to the project's
  directory. Everywhere else it removes `OKU_PROJECT`.
- When a variable that an `[env]` requires is not set, it prints a one-line
  hint that names the variable.
- It also prints the commands that undo what the last run applied, and gives
  each variable back the value it had before.
- `--json` and `--dotenv` print the variables after the hook's changes, with
  `PATH` in full, and the hints on stderr.
- It reads local files only, uses no network, runs nothing from a manifest,
  and takes a few milliseconds.

### oku exec

```
oku exec <command> [args...]
```

Runs a command with the programs of the current directory on `PATH`, for an
editor or a script that does not run the shell hook.

```
$ oku exec gopls version
golang.org/x/tools/gopls v0.22.0
```

- It puts the global `bin` on `PATH` and sets the `[env]` of the global
  packages and the global `oku.toml`. Inside a project it puts the project's
  `bin` first and sets the `[env]` of its packages and its `oku.toml` too.
- It refuses to run while a variable that an `[env]` requires is not set, and
  names it.
- Flags after the command go to the command.
- It exits with the command's exit code.
- A project needs no `oku allow` here, because you name the command yourself.
  Its profile must match its `oku.lock`, or `exec` fails and names `oku sync`.
- `--global` leaves the project out.

## Manifests

These commands are for people who publish a manifest. See
[Publish a manifest](../guides/publish-a-manifest.md) and the
[manifest reference](manifest.md).

### oku manifest init

```
oku manifest init --from <owner/repo | ref> [-o file] [--force]
```

Writes the manifest that oku infers from a repo's newest release, or from a
registry package.

| Flag | Effect |
|---|---|
| `--from` | Required. `owner/repo` on GitHub, or a ref such as `codeberg:owner/repo`, `gitlab:group/project`, `npm:@scope/name`, `pypi:name`, `go:host/path` or `cargo:name`. It takes no `#name` and no `@version`. |
| `-o`, `--output` | The file to write. Default `oku.pkg.toml`. `-` prints to stdout. |
| `--force` | Replaces the output file when it exists. |

It prints `wrote oku.pkg.toml`. An existing file fails with `oku.pkg.toml
already exists, pass --force to replace it`. Inference opens the asset for the
machine it runs on, so run it on a platform the project releases for.

### oku manifest lint

```
oku manifest lint [file...]
```

Checks manifests before you publish them. Without a file it checks
`oku.pkg.toml`.

```
$ oku manifest lint
oku.pkg.toml: error: line 4: unknown key package.relocateable
oku.pkg.toml: warning: artifact[0]: no sha256 or sha256_url, so users trust the first download
oku: 1 of 1 manifests have errors
```

It prints one line per problem, then `<file>: ok` for each file with no
errors. It exits with `1` when any file has an error. `oku add` ignores keys
it does not know, so an older oku still installs a newer manifest. Lint knows
the whole schema.

| Level | Finding |
|---|---|
| error | A key that is not in the schema, with its line. |
| error | Everything `oku add` rejects, such as a bad name, `value` together with `from`, or an artifact with none of `bin`, `man` and `completions`. |
| error | A template variable that does not exist. |
| error | A build step with no type key, or with more than one. |
| error | A `run` step that can run on Windows and sets no `shell`. A step can run on Windows unless its `when` names another `os`. |
| error | A `fetch` step without `sha256`. |
| error | A `bin` table with both `path` and `run`. |
| warning | An artifact with neither `sha256` nor `sha256_url`. |
| warning | An empty `description`. |

### oku manifest test

```
oku manifest test [file] [flags]
```

Installs a manifest into a throwaway store, to show that it works on this
machine. Without a file it tests `oku.pkg.toml`.

| Flag | Effect |
|---|---|
| `--yes`, `-y` | Approves the build commands without asking. |
| `--verbose`, `-v` | Shows the output of build commands as they run. |
| `--keep` | Keeps the throwaway store and prints the package's path. |
| `--accept-key` | Accepts a signing key that differs from the one in `oku.lock`. |

```
$ oku manifest test
[1/3] vendor   ok
[2/3] run      ok
[3/3] install  ok
hey 0.1.5 works on darwin-arm64 (build)
  bin/hey
```

- A manifest with a `[build]` builds from source, deps included, in the same
  sandbox a user gets. One with only artifacts installs the one for this
  machine.
- It ends with the files a profile would link.
- A failing step prints `FAILED`, the build error with the end of the step's
  output, and exits with `1`.
- It does not touch your store, profile, `oku.toml` or `oku.lock`. Downloads
  go to your normal cache.
- It tests the platform you run it on and no other.

### oku manifest bump

```
oku manifest bump [file] [--to version] [--repo ref] [--strip-prefix text]
```

Moves a manifest with a fixed `version.value` to the newest upstream release.
Without a file it bumps `oku.pkg.toml`.

| Flag | Effect |
|---|---|
| `--to` | The version to move to. Default the newest release. |
| `--repo` | The repo to read releases from: `owner/repo` on GitHub, or a ref such as `gitlab:group/project`, `codeberg:owner/repo`, `gitea:host/owner/repo` or `github:host/owner/repo`. `npm:@scope/name` reads the npm registry. Default the repo in the first release URL on github.com, codeberg.org, gitlab.com or registry.npmjs.org. A URL on any other host needs `--repo`. |
| `--strip-prefix` | Text before the version in a tag. Default `v` when a URL holds `/releases/download/v`, else nothing. |

```
$ oku manifest bump
ripgrep 14.1.0 -> 15.2.0, 2 checksums updated in oku.pkg.toml
```

It edits the file as text, so comments and layout stay:

- `version.value` becomes the new version.
- A `url` or `sha256_url` that spells out the old version, without
  `{{version}}`, gets the new one.
- Every inline `sha256` is replaced. oku downloads each artifact, for every
  platform, to compute it.

At the newest version it prints `<name> is already at <version>`. A manifest
that uses `version.from` needs no bump, and bump says so.

### oku manifest hash

```
oku manifest hash <url | file>
```

Downloads a file and prints its checksums as the two lines a manifest can
hold.

```
$ oku manifest hash https://registry.npmjs.org/@actions/languageserver/-/languageserver-0.3.61.tgz
sha256 = "d152725064c64f862da5158cd630d4c67973edfb58bdabaa44054ffef03b9d03"
integrity = "sha512-L5Vf3zc3yD11xUSM8zMxrNwYLZpiUNG6U8kFK2BsxVyRKhU8gQEt02ydR+FZgidkrHSQO/2P7eU9vHPcOwwsOQ=="
```

It trusts the download it gets, as a first `oku add` would. Compare the
output with a checksum the project publishes. The download stays in the
cache, so a following `oku add` does not fetch it again.

## Sources, caches and keys

### oku source

```
oku source add <alias> <ref>
oku source remove <alias>
oku source list
```

Names the manifest collections you install from, so `core/ripgrep` stands for
a full ref. See [Sources](refs.md#sources-and-aliases).

```
$ oku source add core github:you/recipes
core is github:you/recipes
$ oku source list
core  github:you/recipes
```

- `add` takes a ref to a whole collection, so it refuses a `#name` or an
  `@version`.
- An alias is lowercase letters, digits, `_` or `-`. Adding an alias that
  exists replaces it.
- `remove` prints `removed <alias>`, and fails for an alias that is not
  defined.
- `list` with no sources says ``no sources, add one with `oku source add <alias> <ref>` ``.

### oku search

```
oku search <term>
```

Prints the packages in your sources whose name or description contains the
term. Case does not matter.

```
$ oku search grep
core/ripgrep  Recursively search directories for a regex pattern
```

- Each line starts with what to pass to `oku add`.
- With no match it prints `nothing in your sources matches "<term>"`. With no
  sources it fails and says how to add one.
- oku skips a source it cannot list, such as a URL, and prints a notice.
- oku ignores TOML files in a collection that are not manifests.
- A `github:` source costs one request for the file list, then one per
  manifest.

### oku cache

```
oku cache add <directory-or-url>
oku cache remove <directory-or-url>
oku cache list
oku cache push <directory> [name...]
```

Uses and fills caches of built packages. See
[Build caches](../guides/build-caches.md).

| Subcommand | Effect |
|---|---|
| `add` | Looks in this cache before building. The location is an http(s) URL or a directory. Prints `added cache <location>`. |
| `remove` | Stops looking in it. |
| `list` | Lists your caches, in the order oku tries them. |
| `push` | Writes `<store path>.tar.zst` and `<store path>.tar.zst.minisig` into the directory, for each named package of your global profile and each of its deps. Without names it takes every package. |

`push` rules:

- It skips packages that are plain downloads.
- It refuses a package whose build had network access (`network = true` on a
  `run` step).
- It signs with `signing.key`, so it needs `oku key generate` first.
- It uploads nothing. Copy the directory to any static web host.

```
$ oku cache push ./oku-cache jq
pushed jq-1.7.1-0c1d5a3f9e2b7a41
pushed oniguruma-6.9.9-5e8b1f60a2c4d913
```

### oku key

```
oku key generate
oku key trust <public-key>
oku key revoke <public-key>
oku key list
```

Manages the minisign keys that sign and verify cache entries. See
[signed caches](security.md#signed-caches).

| Subcommand | Effect |
|---|---|
| `generate` | Creates `signing.key` in the config directory, with no password, and prints the `oku key trust` line for your users. |
| `trust` | Accepts cache entries that this key signed. |
| `revoke` | Stops accepting them. Packages already installed stay. |
| `list` | Lists the trusted keys, and your own public key when you have one. |

```
$ oku key generate
wrote the secret key to ~/.config/oku/signing.key, it has no password
people who use your cache run:
  oku key trust RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
```

## Self and diagnostics

### oku doctor

```
oku doctor
```

Checks this machine's setup and says what to fix. It reads local files only,
prints one line per check, and exits with `1` when it found a problem. With
none it ends with `no problems found`.

```
$ oku doctor
ok       the store is /home/you/.local/share/oku/store, in your data directory
ok       builds from source run in a sandbox
ok       the shell hook is loaded from /home/you/.zshrc: [ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"
ok       /home/you/.local/share/oku/profiles/global/current/bin is on PATH
problem  /usr/bin/rg runs in place of oku's rg, because /usr/bin is earlier on PATH. Put /home/you/.local/share/oku/profiles/global/current/bin before it, or remove the other copy
ok       every link in 3 profiles points at a file in the store
oku: doctor found 1 problem
```

| Check | `problem` when |
|---|---|
| Store | oku cannot write to the store root. The line says whether the root is the shared one. |
| Sandbox | Never. A host without a sandbox gets a `note` with the reason. |
| Shell hook | Never. Without a hook line in a startup file you get a `note` with the line to add. |
| `PATH` | The global profile's `bin` is not on `PATH`, or a program earlier on `PATH` has the name of an oku program. Several hidden programs are one line, because the fix is one change to `PATH`. |
| Profiles | A package's store path is missing, or a link in a profile's `bin` points at nothing. `oku sync` installs a missing package again. |
| Secrets | The list has secrets and the age key file is missing, or a secret is a sops file and `sops` is neither in the list nor on `PATH`. |
| Unfinished change | A change stopped halfway and oku has not put the machine back. `oku sync` does that first. |

### oku self update

```
oku self update [--check] [--nightly | --release | --to <tag>]
```

Replaces the `oku` binary with the newest release from
`github.com/y3owk1n/oku`, after it checks the signature. See
[self update signatures](security.md#self-update-signatures).

| Flag | Effect |
|---|---|
| `--check` | Says whether a newer release exists, and changes nothing. |
| `--nightly` | Takes the build of the newest commit on `main`, the prerelease `nightly`. |
| `--release` | Goes from a nightly build back to the newest release. |
| `--to <tag>` | Takes the release with that tag, such as `v0.4.0`, older or newer. |

```
$ oku self update
updated oku from 0.4.0 to 0.5.0
what changed: https://github.com/y3owk1n/oku/releases/tag/v0.5.0
```

- `--check` prints `oku <newest> is available, this is <running>`.
- At the newest release it prints `oku <version> is the newest release`.
- On a nightly build, `oku self update` without a flag refuses, because the
  newest release is older than the build. `--release` goes back.
- When the running binary is the newest nightly, `--nightly` prints
  `oku nightly <commit> is the newest nightly build`.
- A binary you built from source has the release key too, so `self update`
  replaces it with the newest release.
- On Windows oku first renames the running `oku.exe` aside, because Windows
  does not let anything replace a program while it runs.

### oku self uninstall

```
oku self uninstall [--keep-list] [--yes] [--system]
```

Lists what it will delete, asks once, then removes oku and everything it
installed.

| Flag | Effect |
|---|---|
| `--keep-list` | Keeps the global `oku.toml` and `oku.lock`, and prints where they are. |
| `--yes`, `-y` | Does not ask. |
| `--system` | With `--yes`, also removes what needs administrator rights: the shared store root and everything in system scope. `--yes` alone counts as no for that part. |

It removes:

- every app, font, service and file that oku set up outside its directories,
  each listed by path. Services stop first.
- every setting oku wrote, which gets back the value it had before
- the data directory, with the store and every profile, project profiles too
- the cache directory
- `oku.toml`, `oku.lock`, `config.toml` and `signing.key` in the config
  directory
- the `oku` binary

Anything else in the config directory is yours, such as the sources of
`[files]`, your own manifests or a `.git` directory. It stays, and uninstall
lists it. A project's `oku.toml` and `oku.lock` stay too.

Any answer other than `y` or `yes` cancels and removes nothing. When some items
need administrator rights, a second question asks about them:

```
  shared store root   /opt/oku (needs administrator rights)
continue? [y/N] y
remove what needs administrator rights? [y/N] n
oku is uninstalled
left in place, empty:
  /opt/oku
remove it with: sudo rmdir /opt/oku
```

Answering no still empties `/opt/oku`, because your user owns it, and prints
the commands for what stays. uninstall ends by printing what you delete
yourself: the hook line, with the startup file it found it in, and the
profile `bin` entry when that is on `PATH`. On Windows the binary goes in two
steps, see [Windows](../guides/windows.md).

### oku --version, oku --help, oku completion

```
oku --version
oku <command> --help
oku completion <bash|zsh|fish|powershell>
```

`oku --version`, or `-v`, prints `oku version <version>`, such as
`oku version 0.6.1` or `oku version nightly-20260921101500-a1b2c3d`. `--help`
works on every command and sorts the commands into sections. `completion`
prints a completion script. The hook loads the completions of `oku` already.

## JSON output

The commands that print data take `--json` and then print JSON on stdout in
place of text. Messages and errors still go to stderr as text. On a command
that prints no data, `--json` changes nothing.

| Command | JSON |
|---|---|
| `oku list` | A list of `name`, `version`, `ref`, `store_path`, `service`, `system`. |
| `oku list --files` | A list of `target`, `kind`, `source`, `list`. |
| `oku list --settings` | A list of `domain`, `key`, `value`, `prior`, `had_prior`. |
| `oku info <name>` | One object: `name`, `version`, `ref`, `commit`, `installed`, `store_path`, `inferred`, `impure`, `vendor_sha256`, `signing_key`. |
| `oku why <name>` | `name`, `version`, `in_list` (the listed ref, or empty for a package that is only a dep), and `needed_by`, a list of `name`, `version`, `dep_versions`. |
| `oku which <program>` | `program`, `package`, `version`, `path`, `shadowed_by`. |
| `oku generations` | A list of `number`, `from`, `current`, `created`, `packages` (`name`, `version`), `files` (`target`, `link`) and `settings` (`domain`, `key`, `value`). |
| `oku outdated` | A list of `name`, `version`, `newest`, `latest`, `ref`, for each package with a newer version. |
| `oku search <term>` | A list of `ref`, `description`. |
| `oku source list` | A list of `alias`, `ref`. |
| `oku cache list` | A list of locations. |
| `oku key list` | `yours` and `trusted`. |
| `oku service list` | A list of `name`, `package`, `installed`, `enabled`, `running`, `system`, `detail`. |
| `oku service status <name>` | One such object. |
| `oku doctor` | `problems`, and `checks`, a list of `status` and `message`. |
| `oku du` | `areas`, a list of `area`, `paths`, `bytes`, then `total`, `gc_frees` and `gc_cache_frees`, all in bytes. |
| `oku du --packages` | A list of `name`, `version`, `path`, `bytes`, `profiles`, `dep_of`, `old`, `unused`. |

- A result with nothing in it prints `[]`, never `null`.
- `created` is an RFC 3339 time in UTC.
- `commit`, `vendor_sha256`, `signing_key`, `shadowed_by`, `from`, `link`,
  `source`, `prior`, `detail` and the `version` of `du --packages` are absent
  when empty.
- `--json` always has the full values, never a cut column.

```
$ oku list --json | jq -r '.[] | "\(.name) \(.version)"'
fd 10.5.0
ripgrep 14.1.1
```

## Error messages

Common messages of `add`, `sync` and `update`. For what to do about each stop
message, see [Troubleshooting](../troubleshooting.md).

| Message | Meaning |
|---|---|
| `<name> has no artifact for darwin-arm64` | No `[[artifact]]` matches this machine, and there is no `[build]`. |
| `no release asset fits this machine` | The repo has no manifest, and no release asset names this OS and arch. The asset names follow. Pass one to `--asset`. For a `[lock]` platform the message names the platform in place of this machine. |
| `cannot tell which file is the program` | The inferred asset holds several executables and none is named after the repo. Pass one to `--bin`. |
| `it chose the asset <name> for this machine` | An install from an inferred manifest failed. The lines after it list the other assets that fit and the `--asset` command that picks one. `--verbose` adds the manifest. |
| `--asset and --bin apply when oku infers a manifest` | The ref has a manifest. |
| `<name> has no [build], so it cannot be built from source` | `--from-source` on a manifest with artifacts only. |
| `the build needs "<tool>", which is not on PATH` | Install that tool yourself. oku does not install `needs`. |
| `<name> needs approval to run them, and this is not a terminal` | The manifest runs build commands and stdin is not a terminal. Pass `--yes` after reading them. |
| `dep <ref>: no version satisfies ">=9"` | A dep's version constraint matches nothing upstream. The versions found follow. |
| `dependency cycle: a -> b -> a` | Two manifests depend on each other. |
| `the vendored packages changed: oku.lock pinned ...` | A build's vendor steps downloaded something other than the lock pinned. oku installed nothing. `oku update <name>` accepts it. |
| `build.step[N] (run) failed` | A build step failed. The last 40 lines of its output follow. |
| `checksum mismatch for <url>` | The download differs from the expected sha256. oku installed nothing. |
| `<alias> is not a source and <arg> is not a file` | The argument looks like `alias/name`, but no such source exists. |
| `<path> already exists and oku did not put it there` | A package's app or font, or a `[files]` entry, would overwrite a file of yours. Move it away. |
| `<a> and <b> both provide bin/<x>` | Two packages ship a file of the same name. oku refuses the second install. |
| `the manifest provides version X, not Y` | The `@version` does not match a manifest with a fixed version. |
| `... has no version X, the newest are ...` | The `@version` names a release that upstream does not have. |
| `<name>: the manifest changed since oku.lock was written` | A file or URL manifest has other bytes than the lock pinned. `oku update <name>` accepts it. |
| `<name>: checksum changed: upstream publishes sha256 <new>, oku.lock pinned <old>` | The checksum file now holds another digest. `oku update <name>` accepts it. |
| `<name>: oku.lock pinned the signing key ..., and the manifest now has the signing key ...` | The manifest's `signing_key` changed. `--accept-key` accepts it. |
| `<name> has no artifact or build for <platform>, so sync did not install it` | A package of the list has nothing for that platform. The next lines give the `when` to write. |
| `waiting for oku process <pid> to finish` | Not an error. Another oku changes the machine, see [one change at a time](paths.md#one-change-at-a-time). |

## Output

These rules apply to every command.

- **Waits.** While oku waits, stderr says what for, with the package's name in
  front: reading a manifest, looking up versions, downloading, unpacking,
  cloning, asking a cache, or running a build step. On a terminal that is one
  line per package that is installing. A download shows how far it got, such
  as `1.4 MiB of 2.0 MiB, 70%`, with the time so far. After eight lines the
  last one counts the rest, and the lines go away when the waits end.
- **Terminal.** On a terminal oku uses colour, glyphs, column headers and `~`
  for your home directory. A finished line starts with a green `✓`, a removal
  with a red `-`, a dry-run change with a yellow `~`, and a pin for another
  platform with a dim `·`. The last line of a change says what it did, such
  as `✓ done in 3s` or `✓ freed 1.2 GiB from 14 store paths`. A command to type
  in a hint shows in colour, without its backticks.
- **Width.** A table fits the terminal. Its last column wraps under itself.
  oku cuts a column that must give room and ends it with `…`. Under 60
  columns, a table that does not fit prints each row as a block of label and
  value lines. A long note or error wraps under its text.
- **Pipes.** In a pipe, a CI log or with `TERM=dumb`, oku prints plain text
  with no header, no glyph and no cut, and each wait is one plain line. A
  script reads the same text wherever it runs.
- **Variables.** `NO_COLOR=1` turns colour off on a terminal. `FORCE_COLOR=1`
  turns it on for a pipe, such as a pager, and `COLUMNS` then sets the width.
