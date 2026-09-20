# Refs

A ref tells oku where a manifest is. `oku add` takes one, and `oku.toml` stores
one per package.

| Ref | Reads |
|---|---|
| `./ripgrep.toml`, `/abs/ripgrep.toml` | A local file. oku stores the absolute path. |
| `https://host/ripgrep.toml` | A URL. `http://` works too. |
| `github:owner/repo` | `oku.pkg.toml` at the root of the repo's default branch. Without one, oku [infers a manifest](manifest.md#inferred-manifests) from the newest release. |
| `github:owner/repo#name` | `name.toml` at the root, else `packages/name.toml`. |
| `git+https://host/repo` | `oku.pkg.toml` at the root of any git repo. |
| `git+https://host/repo#name` | `name.toml` at the root, else `packages/name.toml`. A fragment with no `/` and no `.` is a name. |
| `git+https://host/repo#dir/name.toml` | That file in the repo. |
| `core/ripgrep` | The package `ripgrep` in your source `core`, see [Sources](#sources). |

`git+` also accepts `git+ssh://`, `git+http://` and `git+file://`.

The same forms point at a list in an `include`. Only the file names differ, see
[Including other lists](list-and-lock.md#including-other-lists).

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
raise the limit. oku sends the token to the GitHub API only.

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

## Limits

- A manifest may be at most 1 MiB.
- A `#path` in a `git+` ref must stay inside the repository.
- Inference only covers `github:owner/repo`. A `github:owner/repo#name` ref or
  a `git+` ref needs the manifest file to exist.
