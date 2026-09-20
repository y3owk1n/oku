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

A relative file ref such as `"./recipes/fd.toml"` starts at the directory of the
list that contains it, not at your working directory.

### Services

`service = true` runs the package's [services](services.md) now and at every
login:

```toml
postgres = { ref = "github:you/recipes#postgres", service = true }
```

### Packages for some platforms only

`when` limits a package to matching machines:

```toml
[packages]
rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
patchelf = { ref = "github:you/recipes#patchelf", when = { os = "linux", libc = "glibc" } }
```

The keys are `os`, `arch` and `libc`, with the values described in the
[manifest reference](manifest.md#match-values). A missing key matches anything.
Any other key is an error.

`oku sync` skips a package whose `when` does not match the machine. If the lock
already has an entry for it from another machine, that entry stays.

### Including other lists

`include` merges other lists under this one:

```toml
include = [
  "github:you/machines#base",
  "./work.toml",
]

[packages]
ripgrep = "./my-ripgrep.toml"
```

Each item is a ref to a list:

| Ref | Reads |
|---|---|
| `./work.toml` | That file, relative to the including list. |
| `https://host/base.toml` | That URL. |
| `github:you/machines` | `oku.toml` at the root of the repo. |
| `github:you/machines#base` | `base.toml` at the root, else `lists/base.toml`. |
| `git+https://host/repo` | `oku.toml` at the root of the repo. |
| `git+https://host/repo#dir/base.toml` | That file in the repo. |

Rules:

- Includes merge in order, so a later include overrides an earlier one.
- A package in your own `[packages]` overrides the same name from any include.
- An included list may include others, up to 8 levels deep. A list that is
  included twice, or that includes itself, is an error.
- A list from a URL or a repo can only point at URLs and repos. A local path
  inside it is an error, because the path refers to the list author's machine.
- oku never edits an included list. `oku remove` refuses a package that only
  an include declares. Remove it there, or take the include out.
- oku uses only the `oku.lock` beside your own `oku.toml`. It ignores a lock
  beside an included list.

One form is not editable by oku. A package written as its own table,
`[packages.fd]`, makes `oku add fd` and `oku remove fd` stop and ask you to
edit it by hand. `oku sync` reads that form.

### System scope

`system = true` puts the package's apps, fonts and services in
[system scope](system-scope.md), for every user of the machine:

```toml
postgres = { ref = "github:you/recipes#postgres", service = true, system = true }
```

A plain `oku sync` skips those files and lists them. `oku sync --system` applies
them with `sudo`.

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

The lock pins each included list too:

```toml
[[include]]
ref = 'github:you/machines#base'
commit = 'dc2478ae14dc9931336430027ff284d4dc8e4d44'
sha256 = 'd60f12df...'
```

`oku sync` reads an included list at its pinned commit and stops if its content
no longer has the pinned sha256:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

`oku update` with no names reads includes fresh. `oku update <name>` keeps them
pinned, so it never adds or drops packages.

Keys of a `[[package]]` entry:

| Key | Meaning |
|---|---|
| `ref` | The ref from `oku.toml`, without `@version`. |
| `commit` | The commit the manifest was read at. Only for `github:` and `git+` refs. |
| `manifest_sha256` | Digest of the manifest file. |
| `signing_key` | The manifest's minisign key, when it has one. oku refuses a manifest with another key until you pass `--accept-key`. |
| `version` | The version installed. |
| `tag` | The upstream tag of that version, when it differs, such as `v10.2.0`. |
| `inferred`, `manifest` | Set for a package whose repo has no manifest. `manifest` holds the full text oku inferred. |
| `platform.<name>` | One entry per platform that has resolved this package. |
| `dep` | The packages this one depends on, pinned the same way. |

A platform entry has `strategy`, which is `artifact` or `build`. An artifact
also has `url` and `sha256`. A build has `vendor_sha256` when it has vendor
steps, and `impure = true` when one of its steps used `network = true`.

Platform names are `os-arch`, plus `-glibc` or `-musl` on Linux.

Deps are nested under the package that needs them, as `[[package.dep]]`, and a
dep's own deps nest under it. Each package pins its own, so two packages can
pin different versions of the same dep. `oku sync` installs every dep at its
pinned version, and `oku update` re-resolves them with their parent.

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

Publish `oku.toml` and `oku.lock` side by side in a repo, then run one command
on the new machine:

```
$ oku sync github:you/machines
adopted github:you/machines with 23 locked packages
profile now holds 23 packages
```

oku reads both files at the same commit. `sync` then reads `github:` and `git+`
manifests at their locked commits and checks every download against the locked
sha256, so the new machine gets the same versions, the same bytes, and the same
store paths.

The new machine's own `oku.toml` holds one line, `include = ["github:you/machines"]`.
The published list decides what every machine installs:

- `oku update` on any machine picks up what the published list gained or lost.
- `oku add` on a machine writes to that machine's own `oku.toml` only. To share
  a package, add it to the published list.
- To publish new versions, run `oku update` on the machine whose config
  directory is the repo, and commit the lock.

`oku sync <list-ref>` reads the published lock once, when it sets the machine
up. Afterwards `oku update` on that machine resolves packages fresh, so it can
install newer versions than the published lock. To keep several machines on exactly the
published lock over time, clone the repo to `~/.config/oku/` on each one, and
run `git pull` and `oku sync` to update them together.

Packages in a published list must use URL or repo refs. A local path in it is
an error on every other machine.

You can also copy the two files into `~/.config/oku/` yourself, or symlink that
directory from a dotfiles repo, and run `oku sync` with no argument.

## Moving versions

Versions stay fixed until you run `oku update`. It reads every ref fresh and
rewrites the lock. `oku update <name>` does it for one package. See
[Commands](commands.md#oku-update).

If an update breaks something, `oku rollback` switches the profile and
`oku.lock` back to the generation before it. Commit the lock again afterwards
if you keep it in version control.
