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
| `git+https://host/repo#dir/name.toml` | That file in the repo. |

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

## Limits

- A manifest may be at most 1 MiB.
- A `#path` in a `git+` ref must stay inside the repository.
- `alias/name` refs are not supported yet.
- Inference only covers `github:owner/repo`. A `github:owner/repo#name` ref or
  a `git+` ref needs the manifest file to exist.
