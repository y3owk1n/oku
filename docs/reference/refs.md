# Refs

A [ref](../how-oku-works.md#ref) tells oku where a manifest or a list is.
`oku add` takes one, `oku.toml` stores one per package, and `include` takes
refs to lists.

## Ref forms

| Ref | Reads |
|---|---|
| `./ripgrep.toml`, `/abs/ripgrep.toml` | A local file. oku stores a file inside the list's directory as `./path`, and any other file as its absolute path. |
| `https://host/ripgrep.toml` | A URL of a manifest. `http://` works too. |
| `https://host/tool-1.2.3-linux-amd64.tar.gz` | A URL of the download itself. oku [infers a manifest](manifest.md) for this machine from it. |
| `github:owner/repo` | `oku.pkg.toml` at the root of the repo's default branch. Without one, oku infers a manifest from the newest release. |
| `github:owner/repo#name` | `name.toml` at the root, else `packages/name.toml`. |
| `github:owner/repo#dir/name.toml` | That file in the repo. A fragment with a `/` or ending in `.toml` is a path. This works on every forge. |
| `github:host/owner/repo` | The same on a GitHub Enterprise Server at `host`. `#name` and `@version` work as above. |
| `codeberg:owner/repo` | The same on codeberg.org. |
| `gitea:host/owner/repo` | The same on any Gitea or Forgejo server. The host is required. |
| `gitlab:group/project` | The same on gitlab.com. A project may be in subgroups, as in `gitlab:group/sub/project`. |
| `gitlab:host/group/project` | The same on a GitLab server of your own. |
| `git+https://host/repo` | `oku.pkg.toml` at the root of any git repo. `git+ssh://`, `git+http://` and `git+file://` work too. |
| `git+https://host/repo#name` | `name.toml` at the root, else `packages/name.toml`. A fragment with no `/` and no `.` is a name. |
| `git+https://host/repo#dir/name.toml` | That file in the repo. |
| `npm:name`, `npm:@scope/name` | A package in the npm registry. |
| `pypi:name` | A package in the Python Package Index. |
| `go:host/path` | A Go program, such as `go:golang.org/x/tools/gopls`. |
| `cargo:name` | A crate on crates.io. |
| `cask:name` | A Homebrew cask, which oku translates into a manifest. An `@` is part of the name, as in `cask:temurin@21`, so a cask ref takes no version. |
| `aqua:owner/repo` | The GitHub repo's entry in the aqua registry, which oku translates into a manifest. |
| `winget:Publisher.Package` | The newest version of a package of winget's community manifests, which oku translates into a manifest. |
| `scoop:name`, `scoop:bucket/name` | A Scoop manifest, which oku translates into a manifest. oku looks up a bare name in the `main` bucket, then `extras`. |
| `core/ripgrep` | The package `ripgrep` in your source `core`, see [Sources and aliases](#sources-and-aliases). |

The registry refs `npm:`, `pypi:`, `go:` and `cargo:` have no manifest, so oku
always infers one. How each installs, and the runtime it needs, is in
[npm, PyPI, Go and cargo](../guides/npm-pypi-go-cargo.md).

`cask:`, `scoop:`, `aqua:` and `winget:` read a recipe of Homebrew, Scoop, the
aqua registry or winget once, and oku writes a manifest of its own from it, see
[Recipes of other package managers](manifest.md#recipes-of-other-package-managers).
None needs brew, scoop, aqua or winget on the machine. `aqua:owner/repo` and
`github:owner/repo` are separate sources, and oku reads only the one the ref
names. `scoop:<bucket>/name` takes any
bucket that Scoop knows by name, such as `extras`, `versions` or `java`, except
`nonportable`.

A ref to a [list](oku-toml.md#include) has the same forms. It reads
`oku.toml` in place of `oku.pkg.toml`, and a `#name` reads `name.toml` at the
root, else `lists/name.toml`.

A forge ref whose first part has a dot names a host, because no owner name has
one. A top-level group on gitlab.com with a dot in its name therefore needs the
host written out, as in `gitlab:gitlab.com/my.group/project`.

## Relative paths in a remote list

In a list or a manifest that oku read from a repo or a URL, a relative path
names a file beside it there:

- `./packages/fd.toml` in `lists/base.toml` of `github:you/machines` is
  `github:you/machines#lists/packages/fd.toml`, read at the same commit as the
  list.
- At a URL it is the URL beside the list.
- A path that leaves the repo is an error, and so is an absolute path, because
  it names a file on the author's machine.

So a repo laid out as `oku.toml`, `lists/` and `packages/` works on every
machine that reads it.

## Pin a version

Add `@version` to any ref:

```sh
oku add github:BurntSushi/ripgrep@14.1.1
```

| Form | Picks |
|---|---|
| `@1.4.0` | That release. An unknown version fails and names the five newest. |
| `@22`, `@1.26` | A version that no release has exactly is a prefix. `@22` picks the newest 22.x, and `@1.26` the newest 1.26.x. |
| `@^1.4` | 1.4 and newer, below 2. `^0.4` allows versions below 0.5. |
| `@~1.4` | 1.4 and newer, below 1.5. |
| `@'>=1.2, <2'` | The newest version where every part holds. A part is `>=`, `>`, `<=`, `<` or `=` and a version. |

- With a manifest that fixes one version, `@version` only checks that the
  manifest provides it.
- Quote a range in the shell, as in `oku add 'npm:prettier@^3'`.
- `oku.toml` records the version as `{ ref = "...", version = "^1.4" }`.
- `oku.lock` pins the version oku picked, and `oku sync` keeps it while the
  list's version allows it.
- `oku update` moves the package to the newest version the list allows, so an
  exact version stays where it is.
- When you change the version in `oku.toml` so that it no longer allows the
  locked one, `oku sync` picks the newest version it allows.
- Without a version, `oku add` and `oku update` take the newest.
- A list ref takes no `@version`.

## Sources and aliases

A source is your own short name for a collection of manifests, so you can type
`core/ripgrep` in place of `github:you/recipes#ripgrep`. oku ships with no
sources.

```
$ oku source add core github:you/recipes
core is github:you/recipes
$ oku add core/ripgrep
$ oku search grep
core/ripgrep  Recursively search directories for a regex pattern
```

- A collection is a repo or a directory that holds manifests named
  `<name>.toml`, at its root or under `packages/`. oku uses the one at the
  root when both exist.
- A source can be a `github:` or other forge repo, a `git+` repo, a local
  directory, or a URL prefix. A URL source only finds `<url>/<name>.toml`, and
  `oku search` skips it.
- The alias only shortens what you type. `oku add core/ripgrep` writes the
  full ref to `oku.toml`, so the list works on a machine that has no such
  alias, and removing a source does not affect installed packages.
- `core/ripgrep@14.1.1` pins a version like any other ref.
- When `core/ripgrep` is also a path that exists where you run oku, oku reads
  the path.
- oku keeps aliases in [`config.toml`](oku-toml.md#configtoml) under `[sources]`.

The commands are in [oku source](commands.md#oku-source) and
[oku search](commands.md#oku-search).

## How each kind is fetched

| Kind | Needs git | How oku reads it |
|---|---|---|
| `github:` | no | Asks the GitHub API for the newest commit of the default branch, then reads the manifest at that commit from `raw.githubusercontent.com`. |
| `github:host/...` | no | Reads everything from `https://host/api/v3`, the manifest included. |
| `codeberg:`, `gitea:` | no | Asks `https://host/api/v1` for the newest commit, then reads the manifest at that commit. Gitea and Forgejo serve the same API. |
| `gitlab:` | no | Reads `https://gitlab.com/api/v4`, or `https://host/api/v4` when the ref starts with a host. |
| `git+` | yes | Fetches one commit at depth 1 into the cache directory and reads the file there. git runs with `GIT_TERMINAL_PROMPT=0`, so a repo that needs credentials fails in place of waiting for input. |
| file, URL | no | Reads the bytes. There is no commit, so `oku.lock` pins the manifest's sha256 and `oku sync` stops when the content changed. |

The commit goes into `oku.lock`, and `oku sync` reads that same commit again.

oku keeps each API answer in its cache and asks next time only whether it
changed. With `GITHUB_TOKEN` set, GitHub does not count an unchanged answer
against the rate limit. oku reads a repo's GitHub releases 100 at a time, up
to 1000. When GitHub says oku sent too many requests too fast, oku stops and
says how many seconds to wait.

## When a source changes its format

oku checks each answer from a registry, a forge or a recipe for the fields it
needs, and fails `add` or `update` with the source's name when one is missing.
Where a source versions its format, oku asks for one version:

| Source | Version oku reads |
|---|---|
| GitHub's REST API | `2026-03-10`, sent as `X-GitHub-Api-Version` to github.com |
| PyPI | The Simple API in JSON, major version 1, for versions and files |
| winget manifests | `ManifestVersion` 1.x |
| aqua registry | The newest release tagged `v4.x` |

A newer format fails with a message that says to update oku. `oku sync`
installs what `oku.lock` pins without reading these sources again, so a
machine can still be set up while you wait for an oku that reads the new
format.

## Tokens per host

oku sends each token to its own host only, over https, and not when the server
redirects to another host.

| Variable | Host |
|---|---|
| `GITHUB_TOKEN` | github.com. Never sent to a GitHub Enterprise Server. |
| `GH_ENTERPRISE_TOKEN` | A GitHub Enterprise Server of a `github:host/...` ref. Never sent to github.com. |
| `CODEBERG_TOKEN` | codeberg.org. |
| `GITEA_TOKEN` | Any other Gitea or Forgejo server. |
| `GITLAB_TOKEN` | gitlab.com. |
| `GITLAB_SERVER_TOKEN` | Any other GitLab server. |

When `GITHUB_TOKEN` or `GH_ENTERPRISE_TOKEN` is not set and the `gh` CLI is on
`PATH`, oku runs `gh auth token --hostname <host>` once per run and sends that
login to the same host only.

Without a token GitHub allows 60 API requests an hour. Set `GITHUB_TOKEN`, or
log in with `gh`, to raise the limit.

## Private repos

Set the token of the host, as listed above. `oku sync` on another machine
needs the token too.

- On Codeberg, a Gitea or Forgejo server, and GitLab, oku sends the token with
  the API requests and with the downloads of a release, so a private repo
  installs like a public one.
- On github.com the token reads a private repo's manifest and releases. The
  download link of a private release answers 404, so oku then finds the file
  through the API and downloads it from there with the token. The manifest and
  `oku.lock` keep the download link. GitHub redirects the API request to a
  signed address, and oku never sends the token there. A GitHub Enterprise
  Server's private release still fails to download.
- For `git+`, use an ssh URL with a loaded key.

## Limits

- A manifest may be at most 1 MiB.
- A `#path` must stay inside the repository.
- Inference covers forge refs with no `#name`, the registry refs and a URL of
  the download. A ref with a `#name`, and a `git+` ref, needs the manifest
  file to exist.
