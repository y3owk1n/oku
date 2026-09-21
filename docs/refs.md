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
| `github:host/owner/repo` | The same on a GitHub Enterprise Server at `host`. `#name` and `@version` work as above. |
| `codeberg:owner/repo` | The same on codeberg.org. |
| `gitea:host/owner/repo` | The same on any Gitea or Forgejo server. The host is required. |
| `gitlab:group/project` | The same on gitlab.com. A project may be in subgroups, as in `gitlab:group/sub/project`. |
| `gitlab:host/group/project` | The same on a GitLab server of your own. |
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

## Limits

- A manifest may be at most 1 MiB.
- A `#path` in a `git+` ref must stay inside the repository.
- Inference covers `github:`, `codeberg:`, `gitea:` and `gitlab:` refs with no
  `#name`. A
  ref with a `#name`, and a `git+` ref, needs the manifest file to exist.
