# Refs

A ref tells oku where a manifest is. `oku add` takes one, and `oku.toml` stores
one per package.

| Ref | Reads |
|---|---|
| `./ripgrep.toml`, `/abs/ripgrep.toml` | A local file. oku stores a file inside the list's directory as `./path`, and any other file as its absolute path. |
| `https://host/ripgrep.toml` | A URL of a manifest. `http://` works too. |
| `https://host/tool-1.2.3-linux-amd64.tar.gz` | A URL of the download itself. oku [infers a manifest](manifest.md#a-url-of-the-download) for this machine from it. |
| `github:owner/repo` | `oku.pkg.toml` at the root of the repo's default branch. Without one, oku [infers a manifest](manifest.md#inferred-manifests) from the newest release. |
| `github:owner/repo#name` | `name.toml` at the root, else `packages/name.toml`. |
| `github:owner/repo#dir/name.toml` | That file in the repo. A fragment with a `/` or ending in `.toml` is a path. This works on every forge. |
| `github:host/owner/repo` | The same on a GitHub Enterprise Server at `host`. `#name` and `@version` work as above. |
| `codeberg:owner/repo` | The same on codeberg.org. |
| `gitea:host/owner/repo` | The same on any Gitea or Forgejo server. The host is required. |
| `gitlab:group/project` | The same on gitlab.com. A project may be in subgroups, as in `gitlab:group/sub/project`. |
| `gitlab:host/group/project` | The same on a GitLab server of your own. |
| `npm:name`, `npm:@scope/name` | A package in the npm registry. It has no manifest, so oku always [infers one](#npm-packages). |
| `pypi:name` | A package in the Python Package Index, installed with uv. oku always [infers its manifest](#python-packages). |
| `go:host/path` | A Go program, such as `go:golang.org/x/tools/gopls`, built the way `go install` builds it. oku always [infers its manifest](#go-programs). |
| `cargo:name` | A crate on crates.io, built the way `cargo install --locked` builds it. oku always [infers its manifest](#rust-crates). |
| `git+https://host/repo` | `oku.pkg.toml` at the root of any git repo. |
| `git+https://host/repo#name` | `name.toml` at the root, else `packages/name.toml`. A fragment with no `/` and no `.` is a name. |
| `git+https://host/repo#dir/name.toml` | That file in the repo. |
| `core/ripgrep` | The package `ripgrep` in your source `core`, see [Sources](#sources). |

`git+` also accepts `git+ssh://`, `git+http://` and `git+file://`.

The same forms point at a list in an `include`. Only the file names differ, see
[Including other lists](list-and-lock.md#including-other-lists).

In a list or a manifest that oku read from a repo or a URL, a relative path
names a file beside it there. `./packages/fd.toml` in `lists/base.toml` of
`github:you/machines` is `github:you/machines#lists/packages/fd.toml`, read at
the same commit as the list. At a URL it is the URL beside the list. A path
that leaves the repo, and an absolute path, are errors.

## Pinning a version

Add `@version` to any ref:

```
oku add github:owner/repo@1.4.0
```

With a manifest that [discovers versions](manifest.md#version), `@1.4.0` picks
that release. An unknown version fails and names the five newest. With a
manifest that fixes one version, the pin only checks that the manifest provides
it.

`oku.toml` records the pin as `{ ref = "...", version = "1.4.0" }`, and
`oku update` leaves a pinned package on its version. Without a pin, `oku add`
and `oku update` take the newest version.

## How each kind is fetched

**`github:`** needs no git. oku asks the GitHub API for the newest commit of
the default branch, then reads the manifest at that exact commit from
`raw.githubusercontent.com`. The commit goes into `oku.lock`, and `oku sync`
reads that same commit again.

GitHub allows 60 unauthenticated API requests per hour. Set `GITHUB_TOKEN` to
raise the limit, or log in with the `gh` CLI. Without the variable, oku runs
`gh auth token --hostname github.com` once per run and uses that token. oku
sends the token to the GitHub API only.

oku keeps each API answer in its cache, and the next time asks GitHub only
whether it changed. With `GITHUB_TOKEN` set, GitHub does not count an
unchanged answer against the limit. Without a token every request counts. oku
reads a repo's releases 100 at a time, up to 1000. When GitHub says that oku sent
too many requests too fast, oku stops and says how many seconds to wait.

A `github:host/owner/repo` ref reads a GitHub Enterprise Server. A first part
with a dot is the host, because no owner name has one. oku reads everything
from `https://host/api/v3`, the manifest included. Set `GH_ENTERPRISE_TOKEN`
for a server that needs a login. oku never sends `GITHUB_TOKEN` to such a
server, and never sends `GH_ENTERPRISE_TOKEN` to github.com.

**`codeberg:`** and **`gitea:`** need no git either. oku reads the server's API
at `https://host/api/v1`. It asks for the newest commit, then reads the
manifest at that commit. Gitea and Forgejo serve the same API, so `gitea:`
reads both.
Set `CODEBERG_TOKEN` for codeberg.org and `GITEA_TOKEN` for any other server
that needs a login. oku sends each token to its own host only.

**`gitlab:`** needs no git. oku reads the API at `https://gitlab.com/api/v4`, or
at `https://host/api/v4` when the ref starts with a host. As with `github:`, a
first part with a dot is the host. A top-level group on gitlab.com that has a
dot in its name therefore needs the host written out, as in
`gitlab:gitlab.com/my.group/project`. Set `GITLAB_TOKEN` for gitlab.com and
`GITLAB_SERVER_TOKEN` for any other server. oku sends each token to its own
host only, and does not send it when the server redirects to another host.

**`git+`** needs `git` on `PATH`. oku fetches one commit at depth 1 into its
cache directory and reads the file from there. git runs with
`GIT_TERMINAL_PROMPT=0`, so a repo that needs credentials fails instead of
waiting for input. Use an ssh URL with a loaded key for private repos.

**Files and URLs** have no commit. `oku.lock` pins the manifest's sha256
instead, and `oku sync` stops if the content changed.

## Sources

A source is your own short name for a collection of manifests, so you can type
`core/ripgrep` instead of `github:someone/recipes#ripgrep`. oku ships with no
sources.

```
$ oku source add core github:someone/recipes
core is github:someone/recipes
$ oku add core/ripgrep
$ oku search grep
core/ripgrep  Recursively search directories for a regex pattern
```

A collection is a repo or a directory that holds manifests named `<name>.toml`,
at its root or under `packages/`. oku uses the one at the root when both
exist. A source can
be a `github:` repo, a `git+` repo, a local directory, or a URL prefix. A URL
source only finds `<url>/<name>.toml` and cannot be searched.

The alias only shortens what you type. `oku add core/ripgrep` writes the full
ref, `github:someone/recipes#ripgrep`, to `oku.toml`. Your list therefore works
on a machine that has no such alias, and removing a source does not affect
installed packages or `oku sync`.

`core/ripgrep@14.1.1` pins a version like any other ref. If `core/ripgrep` is
also a path that exists where you run oku, the path is used.

Aliases live in `config.toml` in the config directory:

```toml
[sources]
core = 'github:someone/recipes'
```

## npm packages

`oku add npm:@scope/name` installs a command-line tool from the npm registry.
oku asks the registry for the package's versions, its download and the programs
in its `bin`, and writes a manifest that follows the package's versions. It
checks each download against the sha512 the registry publishes. `@version`
picks a version, and `oku manifest init --from npm:@scope/name` writes the
manifest to a file.

The programs need node. Name a package that provides it in `oku.toml`, once:

```toml
[runtimes]
node = 'github:you/recipes#node'
```

Every npm package then gets that package as a runtime dep, and its programs run
through it. They do not need node on `PATH`, and node does not appear there.
`oku update` updates that node package too.

`[runtimes]` works in the lists that `oku.toml` includes too, and a later list
overrides an earlier one, as with `[vars]`. A relative path starts at the list
that names it. In a list from a repo, `node = "./packages/node.toml"` names that
file of the same repo, so a machine that adopts the repo with
`oku sync github:you/machines` gets the same node. `oku.lock` stores a node
inside the list's directory relative to it, so the lock works under another home
directory and in another checkout of a project.

`config.toml` may hold the same `[runtimes]` table. oku uses it when no list
names a node, and a relative path there starts at the directory of
`config.toml`. There the ref may also be a [source alias](#sources), such as
`core/node`. A lock that oku 0.4.0 or older wrote holds the
full path from the machine that wrote it. Run `oku update <name>` for each npm
package to replace it.

Without `runtimes.node` in a list or in `config.toml`, the programs run the
`node` on `PATH`, and `oku add` says so. That does not work on Windows, where
`oku add npm:` then fails and names the key.

A package that lists no dependencies is a plain download, such as prettier. For
a package that lists some, oku installs them too. That covers a tool that ships
its program in a platform package, as typescript 7 does. oku then writes
a [build](manifest.md#vendoring) that runs the `npm` of your node package, so
that package has to list `bin/npm` beside `bin/node`. npm picks each dependency
as it was when the version was published and runs no install scripts, and
`oku.lock` pins a digest of what it installed. A package whose dependency
needs its install script, because that script downloads a native binary, takes
a manifest of your own that names it in
[`scripts`](manifest.md#vendoring). A build asks for
[approval](trust.md#build-commands) once, or takes `--yes`. oku cannot do this
on Windows yet, and says so there.

Without `runtimes.node` there is no npm to run, so oku installs the package's
own download only. That works when the download bundles its code, and the
inferred manifest has a comment that says so.

## Python packages

`oku add pypi:black` installs a command-line tool from the Python Package
Index. oku asks the index for the package's versions and writes a manifest that
follows them. `@version` picks a version. A prerelease, such as `2.0rc1` or
`2.0.dev1`, and a yanked version are never the newest, and `@version` still
takes one. `oku manifest init --from pypi:black` writes the manifest to a file.

The package's programs need python. Name a package that provides `python3` in
`[runtimes]`, as for node:

```toml
[runtimes]
python = "./packages/python.toml"
```

The programs then run through that python. They do not need python on `PATH`,
and python does not appear there. Without `runtimes.python`, the build and the
programs use the `python3` that the build finds on its `PATH`, which on macOS is
`/usr/bin/python3` from the Command Line Tools. That python may be too old for a
package. uv then says which python the package needs.

oku installs the package with [uv](https://github.com/astral-sh/uv), which it
takes from `github:astral-sh/uv` as a build dep, so uv needs no setup and does
not appear on `PATH`. `[runtimes] uv` names another uv package. uv installs the
package and its dependencies as they were when that version was uploaded, and
never downloads a python of its own. `oku.lock` pins a digest of the install,
which is the same on every machine of a platform. oku writes a program for
each console script of the package, and copies any other program the package
ships, such as ruff's binary. The programs of its dependencies are not
exposed. A build asks for [approval](trust.md#build-commands) once, or takes
`--yes`.

On Windows the build runs through PowerShell, and each console script becomes
a shim, like any program in a profile there. A Windows build has no python of
the system, so a pypi package on Windows needs `runtimes.python`, such as a
python-build-standalone download, and the build says so without one.

## Go programs

`oku add go:golang.org/x/tools/gopls` builds a Go program the way
`go install golang.org/x/tools/gopls@latest` does. oku asks the Go module proxy
which module holds the package. That is the longest part of the path that is a
module. The manifest oku writes follows that module's tagged versions. A
version with a `-`, such as `1.3.0-rc.1`, is never the newest, and `@version`
takes one. A module with no tags has one version, the pseudo-version of its
newest commit. The program is named after the last part of the path that is no
major version, so `go:github.com/mikefarah/yq/v4` gives `yq`.
`oku manifest init --from go:<path>` writes the manifest to a file.

The build needs the go command. Name a package that provides it in
`[runtimes]`:

```toml
[runtimes]
go = "./packages/go.toml"
```

Without `runtimes.go` the build uses the `go` on your `PATH`, and reads its
standard library where `go env GOROOT` says it is. A Go program needs nothing at
run time, so go is only a build dep and stays out of `PATH` either way.

The build downloads the module and every module it needs with the network on,
and the go command checks each against the Go checksum database. `oku.lock`
pins a digest of those downloads, which is the same on every platform. Then
`go install` runs offline from them, with cgo off, so the program knows its own
version as a `go install` gives it. A build asks for
[approval](trust.md#build-commands) once, or takes `--yes`. It works the same on
Windows, where the steps run through PowerShell.

## Rust crates

`oku add cargo:just` builds a crate from crates.io the way
`cargo install --locked just` does. oku asks crates.io for the crate's versions
and writes a manifest that follows them. A yanked version and a version with a
`-`, such as `2.0.0-beta.1`, are never the newest, and `@version` takes one. A
crate with no programs, a library, fails with a message that says so.
`oku manifest init --from cargo:<name>` writes the manifest to a file.

The build needs cargo and rustc. Name a package that provides them in
`[runtimes]`:

```toml
[runtimes]
rust = "./packages/rust.toml"
```

Without `runtimes.rust` the build uses the `cargo` on your `PATH`, and a cargo
that rustup manages works there too. rust is only a build dep and stays out of
`PATH`.

The build downloads the crate's `.crate` file from crates.io and checks it
against the sha256 that crates.io publishes for that version, so oku trusts no
download on first use. It vendors the dependencies that the crate's
`Cargo.lock` pins, and `oku.lock` pins a digest of them that is the same on every
platform. Then `cargo install --locked --offline` builds the programs. A crate
published without a `Cargo.lock` fails there, with cargo's own message. A build
asks for [approval](trust.md#build-commands) once, or takes `--yes`. It works the
same on Windows, where the step runs through PowerShell.

## Private repos

Set the token of the host, as listed above. On Codeberg, a Gitea or Forgejo
server, and GitLab, oku sends it with the API requests and with the downloads
of a release, so a private repo installs like a public one. oku sends it only
to that host and only over https, and Go drops it when the server redirects to
another host. `oku sync` on another machine needs the token too.

On GitHub the token reads a private repo's manifest and releases, and the
download of a private release then fails. GitHub serves those files from its API
only, and oku downloads the URL a release lists.

## Limits

- A manifest may be at most 1 MiB.
- A `#path` in a `git+` ref must stay inside the repository.
- Inference covers `github:`, `codeberg:`, `gitea:` and `gitlab:` refs with no
  `#name`, `npm:` refs, and a URL of the download. A
  ref with a `#name`, and a `git+` ref, needs the manifest file to exist.
