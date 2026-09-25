# Install npm, PyPI, Go and Cargo packages

This guide installs command-line tools from npm, the Python Package Index, the
Go module proxy and crates.io. oku pins the toolchain that runs or builds
each one, and puts none of those toolchains on your `PATH`.

```sh
oku add npm:prettier
oku add pypi:ruff
oku add go:mvdan.cc/gofumpt
oku add cargo:just
```

None of these packages has an oku manifest. oku always writes one from the
registry, an [inferred manifest](../how-oku-works.md#inferred-manifest), and
pins it in `oku.lock` like any other.

## Name the toolchains in [runtimes]

Each kind of package needs a toolchain. An npm program runs on node, a PyPI
program runs on python, and Go and Rust programs are built with go and cargo.
`[runtimes]` in `oku.toml` names a package that provides each one:

```toml
[runtimes]
node = { ref = "./node.toml", version = "^22" }
python = "./python.toml"
go = "./go.toml"
rust = "./rust.toml"

[packages]
prettier = "npm:prettier"
ruff = "pypi:ruff"
gofumpt = "go:mvdan.cc/gofumpt"
just = "cargo:just"
```

You do not have to write those four manifests.
[`examples/runtimes/`](../../examples/runtimes) in the oku repo has one for
each:

| File | Used by | Platforms |
|---|---|---|
| [`node.toml`](../../examples/runtimes/node.toml) | `npm:` programs run on it, and their builds run its npm | macOS, Linux with glibc, Windows |
| [`python.toml`](../../examples/runtimes/python.toml) | `pypi:` programs run on it, and uv installs them for it | macOS, Linux, Windows |
| [`go.toml`](../../examples/runtimes/go.toml) | `go:` packages build with it | macOS, Linux, Windows |
| [`rust.toml`](../../examples/runtimes/rust.toml) | `cargo:` packages build with its cargo and rustc | macOS, Linux |

Copy the files you need next to your `oku.toml`, name them in `[runtimes]` as
above, and run `oku add` or `oku sync`.

Each toolchain becomes a [runtime dep](../how-oku-works.md#runtime-dep) or a
[build dep](../how-oku-works.md#build-dep) of the packages that use it:

- node and python are runtime deps. A program runs through them, and oku puts
  that node or python first on the program's own `PATH`, so a program that
  starts `node` by name finds the right one.
- go and rust are build deps. The programs need nothing of them at run time.

None of them goes on your `PATH`. To run `node` or `python3` yourself, add the
same manifest under `[packages]` too. A runtime needs a `[packages]` entry for
that reason only.

### Pin a toolchain's version

A runtime can take a `version` constraint, written like the one for a package
in [Pick a version](add-packages.md#pick-a-version):

```toml
[runtimes]
node = { ref = "./node.toml", version = "^22" }
go = { ref = "./go.toml", version = "1.26.4" }
```

oku picks the newest version of that package that the constraint allows, when
it adds a package and at `oku update`. `oku.lock` pins what it picked, and
`oku sync` keeps it. The lock copies the constraint into the manifest of each
package that uses the runtime, so after you change it, run `oku update <name>`
for those packages.

In the examples only node has a constraint. node ships several release lines
at once, and the newest is often not the line your tools target. `^22`
keeps node on one line and still takes that line's patches. A newer go or rust
still builds older code, and many crates need a recent rustc, so those two take
the newest release. `oku update` moves node, go and rust. `python.toml` fixes
its version, because each python-build-standalone download is named after the
python version and its release date. Change both in that file to move to a
newer python.

### Where [runtimes] can live

- In `oku.toml` and in the lists it includes. A later list overrides an earlier
  one, and your own list overrides them all.
- A relative path starts at the list that names it. In a list from a repo,
  `node = "./packages/node.toml"` names that file of the same repo, so a
  machine that adopts the repo gets the same node. `oku.lock` stores a path
  inside the list's directory relative to it, so the lock works under another
  home directory and in another checkout.
- In `config.toml`, for a machine whose lists name none. A relative path there
  starts at the directory of `config.toml`, and the ref may be a
  [source](../how-oku-works.md#source) alias such as `recipes/node`.

A lock that oku 0.4.0 or older wrote holds the full path of the node from the
machine that wrote it. Run `oku update <name>` for each npm package to replace
it.

## npm packages

`npm:name` and `npm:@scope/name` install a tool from the npm registry. oku
reads the package's versions, its download and the programs in its `bin`, and
checks each download against the sha512 the registry publishes.

A package with no dependencies, such as prettier, is one download. For a
package that lists dependencies, oku writes a [build](../how-oku-works.md#build)
that runs the `npm` of your node package. So your node package lists
`bin/npm` beside `bin/node`, as `node.toml` does. npm then installs each dependency as
it was when that version was published, and runs no install scripts.
`oku.lock` pins a digest of what it installed. This covers a tool that ships
its program in a platform package, as typescript 7 does.

Some packages need an install script, which downloads or builds a native
binary, such as esbuild's and opencode-ai's own. After npm installs the tree,
oku looks for packages with an install script or a `binding.gyp`. For an
`npm:` ref it names them in the manifest's `scripts` and asks again, and the
approval lists them before any runs:

```
vendor npm, which installs esbuild and its dependencies and runs the install scripts of esbuild
```

`--yes` approves them too. A script that puts a native program where the
package's script was makes the program run directly, not through node. A
manifest of your own names its scripts itself, and oku warns about the
packages with install scripts that it leaves out. See
[Vendoring](../reference/manifest.md).

### Without runtimes.node

A package with no dependencies, such as prettier, runs the `node` on your
`PATH`, and `oku add` says so:

```
its programs run the node on PATH. To pin one, set runtimes.node in config.toml to the ref of a package that provides node
example: https://github.com/y3owk1n/oku/blob/main/examples/runtimes/node.toml
guide: https://github.com/y3owk1n/oku/blob/main/docs/guides/npm-pypi-go-cargo.md#name-the-toolchains-in-runtimes
```

`[runtimes]` in `oku.toml` works as well as `config.toml`.

A package that lists dependencies needs the npm of a node package to install
them, so `oku add` stops without one:

```
oku: the npm package repomix lists dependencies, and only the npm of a node package installs them
set runtimes.node in ~/.config/oku/oku.toml to the ref of a package that provides node and npm
example: https://github.com/y3owk1n/oku/blob/main/examples/runtimes/node.toml
guide: https://github.com/y3owk1n/oku/blob/main/docs/guides/npm-pypi-go-cargo.md#name-the-toolchains-in-runtimes
```

## PyPI packages

`pypi:name` installs a tool from the Python Package Index with
[uv](https://github.com/astral-sh/uv). oku takes uv from `github:astral-sh/uv`
as a build dep, so you set nothing up and uv does not land on your `PATH`.
`[runtimes] uv` names another uv package.

uv installs the package and its dependencies as they were when that version
was uploaded, and never downloads a python of its own. `oku.lock` pins a
digest of the install, the same on every machine of a platform.

oku asks uv for the wheels of a fixed platform, on every machine, so the
digest does not depend on the machine's glibc or macOS version:

| Platform | Wheels for |
|---|---|
| Linux with glibc | manylinux 2.28, which runs on glibc 2.28 and newer, such as Debian 10 and RHEL 8 |
| Linux with musl | musllinux |
| macOS | macOS 13 and newer |
| Windows | the MSVC target |

A lock written on a Mac therefore pins the wheels that a Linux machine
installs. oku writes a
program for each console script of the package, and copies any other program
it ships, such as ruff's binary. oku does not expose the programs of its
dependencies.

A prerelease, such as `2.0rc1` or `2.0.dev1`, and a yanked version are never
the newest. `@version` still takes one.

### Without runtimes.python

`oku add` stops. Without a python package, oku would write the path of the
`python3` on your `PATH` into each program, and an upgrade of that python would
break them:

```
oku: pypi:ruff needs python
set runtimes.python in ~/.config/oku/oku.toml to the ref of a package that provides python3
example: https://github.com/y3owk1n/oku/blob/main/examples/runtimes/python.toml
guide: https://github.com/y3owk1n/oku/blob/main/docs/guides/npm-pypi-go-cargo.md#name-the-toolchains-in-runtimes
```

## Go programs

`go:host/path` builds a Go program the way `go install host/path@latest` does.
oku asks the Go module proxy which module holds the package and follows that
module's tagged versions. A module with no tags has one version, the
pseudo-version of its newest commit. A version with a `-`, such as
`1.3.0-rc.1`, is never the newest, and `@version` takes one.

The program is named after the last part of the path that is not a major
version, so `go:github.com/mikefarah/yq/v4` gives `yq`.

The build downloads the module and everything it needs with the network on,
and go checks each against the Go checksum database. `oku.lock` pins a digest
of those downloads, the same on every platform. Then `go install` runs offline
from them, with cgo off, so the program reports its version as a
`go install` build does.

### Without runtimes.go

The build uses the `go` on your `PATH`, with the standard library at the place
`go env GOROOT` names.

## Cargo packages

`cargo:name` builds a crate from crates.io the way `cargo install --locked name`
does. A yanked version and a version with a `-`, such as `2.0.0-beta.1`, are
never the newest, and `@version` takes one. A crate with no programs is a
library, and `oku add` fails and says so.

The build downloads the `.crate` file and checks it against the sha256 that
crates.io publishes for that version. It vendors the dependencies the crate's
`Cargo.lock` pins, and `oku.lock` pins a digest of them, the same on every
platform. Then `cargo install --locked --offline` builds the programs.

### Without runtimes.rust

The build uses the `cargo` on your `PATH`. A cargo that rustup manages works
too.

## Approve the build once

oku builds a PyPI, Go or Cargo package, and an npm package with
dependencies, on your machine. The first build asks before it runs anything:

```
just 1.58.0 builds from source and runs these commands on your machine:

  step 1  (downloads packages, checked against oku.lock)
    vendor cargo, which installs just and its dependencies and runs none of their scripts

run them? [y/N]
```

oku remembers the answer for that exact manifest. When stdin is not a
terminal, pass `--yes` after you have read the commands. Builds run in a
[sandbox](../reference/security.md) on macOS and Linux.

## Windows

- Every build here runs through PowerShell, and needs no PowerShell 7.
- An npm build runs npm's own `npm-cli.js` with your node package's
  `node.exe`, so the node package needs only `node.exe` in its `bin`.
- An `npm:` package fails without `runtimes.node`, because Windows cannot run
  a script through `PATH`. The error names the key to set.
- Each console script of a `pypi:` package becomes a
  [shim](../how-oku-works.md#shim).
- `rust.toml` does not build on Windows, because it runs the installer's
  `install.sh`. Leave `rust` out of `[runtimes]` there, and a `cargo:` package
  uses the cargo on `PATH`, such as the one rustup installs.

See [Windows](windows.md) for the rest.

## What does not work

- A crate published without a `Cargo.lock` fails at the build, with cargo's own
  message.
- A library crate has no programs to install.
- `--asset` and `--bin` do not apply to these refs, because the registry names
  the download and the programs.
- A `pypi:` package built with an older oku pinned the wheels of the machine
  that built it. A new build can then stop with "the vendored packages
  changed". Run `oku update <name>` once to pin the fixed platform's wheels.

To edit the manifest oku writes, save it to a file with
`oku manifest init --from npm:prettier`, or with a `pypi:`, `go:` or `cargo:`
ref. [Refs](../reference/refs.md) has the details of each registry.
