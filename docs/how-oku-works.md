# How oku works

This page explains the ideas behind oku and defines the words the rest of the
docs use. You do not need it to get started. Read it when something in a guide
does not make sense, or when you want to know why oku behaves as it does.

## The short version

- You describe what you want in a file, `oku.toml`. oku calls it your list.
- oku writes down exactly what it installed in a second file, `oku.lock`.
- oku unpacks every package into its own folder in a private store. The folder
  never changes after that.
- A profile is a folder of links into the store. Its `bin` folder is on your
  `PATH`, so the programs run by name.
- Every change makes a new generation of the profile. `oku rollback` switches
  back to an older one without downloading anything.

```
oku.toml --+
           +--> oku sync --> store --------> profile generation --> your PATH
oku.lock --+                 (one folder     (links into            and your
                             per package)    the store)             home files
```

## From a list to a machine

When you run `oku sync`, or a command that changes the list such as `oku add`,
oku does this:

1. It reads `oku.toml`, any lists it includes, and the parts that apply to
   this OS.
2. For each package it finds a manifest, which says where the downloads are.
   It uses the version that `oku.lock` pins, or picks one when there is no pin
   yet.
3. It downloads, checks each download against its sha256, and unpacks it into
   the store. It skips a package it already has.
4. It checks everything else it will touch, such as the files it will write in
   your home directory.
5. It builds a new generation of the profile next to the active one, and
   switches to it in one step.
6. It applies the rest: apps, fonts, services, your home files, secrets and OS
   settings.

If any step fails, oku undoes the steps before it and the machine stays as it
was. The [paths reference](reference/paths.md) describes this in detail.

## Why two files

`oku.toml` says what you want. It can be loose, for example "ripgrep, any
14.x". You edit it, or `oku add` and `oku remove` edit it for you.

`oku.lock` says what you got. It records the exact version, the git commit of
the manifest, and the sha256 of every download. oku writes it, and you never
edit it by hand.

Commit both files. Another machine with the same two files installs the same
bytes. Versions move only when you run `oku update`, or when you change
`oku.toml` so that the pinned version no longer fits.

## Why a store

Each package lives in its own store folder, named after the package, its
version, and a hash of everything that went into it. A new version or a
changed manifest gets a new folder, and oku never overwrites an old folder.

The old generation still points at the old folders, so `oku rollback` needs
no download and finishes at once. `oku gc` deletes store
folders that no generation uses any more.

Old versions cost less than a full copy each. When two store folders hold a
file with the same bytes, the disk keeps it once, so a new version only adds
the files that changed.

## Glossary

### list

A TOML file that names what you want: packages, home files, secrets and OS
settings. Your main list is `~/.config/oku/oku.toml`, on Windows
`%APPDATA%\oku\oku.toml`. A list can `include` other lists, and a repo can
have its own list, see [project](#project). Every key is in the
[oku.toml reference](reference/oku-toml.md).

### lock

`oku.lock`, next to the list. It pins the exact version, commit and download
sha256 of every package, per platform. oku writes it. See the
[lock reference](reference/lock.md).

### ref

The address of a package. `github:BurntSushi/ripgrep`, `npm:prettier`,
`./fd.toml` and `https://example.com/tool.toml` are all refs. oku has no
central package registry, so a ref always says where to look. Every form is in
the [refs reference](reference/refs.md).

### manifest

A small TOML file that says where a package's downloads are, which programs it
has, and how to build it from source if needed. A project can publish one as
`oku.pkg.toml` in its repo. See the
[manifest reference](reference/manifest.md).

### inferred manifest

A manifest oku writes for you when a repo has none. oku reads the newest
release, matches the files to operating systems and CPUs, finds the published
checksums, and looks inside the download for the programs. For a `cask:` or
`scoop:` ref, oku translates the recipe of Homebrew or Scoop instead.
`--verbose` prints the manifest it wrote.

### store

The folder where oku unpacks packages, one folder per exact build, under
`~/.local/share/oku/store` by default. Nothing in it changes after it is
written. See the [paths reference](reference/paths.md).

### profile

A folder of links into the store. The global profile's `bin` folder is the one
the shell hook puts on your `PATH`. Each [project](#project) gets a profile of
its own.

### generation

One numbered version of a profile. Every change that installs, removes or
changes a package, a home file or a setting writes a new generation.
`oku generations` lists them and `oku rollback` switches between them. See
[Undo a change](guides/undo-and-clean-up.md).

### sync

Making the machine match the list and the lock. `oku sync` installs what is
missing, removes what the list no longer names, and applies home files and
settings. `oku sync --dry-run` shows what would change.

### source

A short name you give to a repo of manifests, so `core/ripgrep` can stand for
`github:someone/recipes#ripgrep`. oku ships with no sources. See
[Add packages](guides/add-packages.md).

### runtime

A toolchain that registry packages need, such as node for `npm:` packages or
rust for `cargo:` packages. You name it once under `[runtimes]` in your list.
oku installs it into the store and uses it, but does not put it on your
`PATH`. See [npm, PyPI, Go and cargo packages](guides/npm-pypi-go-cargo.md).

### runtime dep

A package another package needs while it runs, such as the interpreter of a
script. oku installs it into the store, and it stays out of your profile.

### build dep

A package that another package needs only to build from source, such as a
compiler. It is never on the `PATH` of the program it built.

### artifact

A prebuilt download listed in a manifest, such as a `.tar.gz` for Linux on
arm64. oku picks the artifact that fits the machine.

### build

The recipe in a manifest for building a package from source, used when no
artifact fits. oku shows you the commands and asks once before it runs them,
then runs them in a sandbox with no network. See
[Security](reference/security.md).

### vendor step

A build step that downloads a language's dependencies, such as `go mod vendor`
or `cargo vendor`, with the network on. oku pins a digest of what it
downloaded in the lock, so a later build must download the same thing.

### ledger

oku's record of every file it wrote outside its own folders, such as an app,
a font, a service definition or a file in your home directory. oku never
overwrites a file that is not in its ledger, and `oku self uninstall` removes
everything in it.

### project

A repo with its own `oku.toml`. When you `cd` into it, the shell hook puts the
project's tools on `PATH`, after you have run `oku allow` there once. See
[Projects](guides/projects.md).

### system scope

Installing apps, fonts and services for every user of the machine instead of
only for you. It is the one part of oku that needs root. See
[Install for every user](guides/system-wide.md).

### build cache

A folder or web host of packages that someone already built, signed with their
key. oku uses a build from a cache only when you trust the key that signed it.
See [Build caches](guides/build-caches.md).

### shim

On Windows, a small program in the profile that starts the real program in the
store. Windows cannot run a program through a link the way macOS and Linux
can. See [Windows](guides/windows.md).
