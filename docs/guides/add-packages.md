# Add, update and remove packages

This guide gets you from a repo, a URL or a file to a program on your `PATH`,
pinned in `oku.lock`, and shows how to move it forward or take it out again.

Every command here edits two files in `~/.config/oku/`. `oku.toml` is your
[list](../how-oku-works.md#list), and `oku.lock` is the [lock](../how-oku-works.md#lock)
that pins what oku installed. Inside a directory that has its own
`oku.toml`, the same commands act on that [project](../how-oku-works.md#project)
instead, and `--global` (`-g`) sends them to your own list.

## Add a package from GitHub

Point `oku add` at the repo:

```
$ oku add github:sharkdp/fd
github:sharkdp/fd has no manifest, so oku inferred one from its newest release, --verbose prints it
added fd 10.5.0
```

The fd repo has no oku [manifest](../how-oku-works.md#manifest), the small
TOML file that says where a package's downloads are. So oku read the newest
release, matched the release files to your OS and CPU, found the published
checksums, and opened the download to find the program inside. That is an
[inferred manifest](../how-oku-works.md#inferred-manifest). `--verbose` prints
it.

`oku add` then wrote one line to `oku.toml`:

```toml
[packages]
fd = "github:sharkdp/fd"
```

and pinned the commit, the version and the download's sha256 in `oku.lock`.
The lock also keeps the text of the inferred manifest, so another machine
installs from that same text, and only `oku update` infers again.

When the programs do not run by name yet, `oku add` prints the line your shell
needs:

```
to run it, add this line to ~/.zshrc, then open a new terminal:
  [ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"
```

See [Set up your shell](../getting-started.md#2-set-up-your-shell) for bash,
fish and PowerShell.

Several refs in one command install one after another, one
[generation](../how-oku-works.md#generation) each. A failure stops the command,
and the packages added before it stay:

```sh
oku add github:BurntSushi/ripgrep github:sharkdp/fd
```

Adding a package that is already installed replaces it.

Three flags change how a package installs. `--service` runs its daemon now and
at every login, see [Services](services.md). `--system` puts its apps, fonts
and services where every user sees them, see [System-wide](system-wide.md).
`--from-source` builds it even when a download fits. The
[command reference](../reference/commands.md#oku-add) lists them all.

## Look before you add

`--plan` shows what `oku add` would install and changes nothing:

```
$ oku add github:sharkdp/fd --plan
name       fd
version    10.5.0
manifest   inferred by oku, because the ref has none
asset      fd-v10.5.0-aarch64-apple-darwin.tar.gz
install    download for darwin-arm64
url        https://github.com/sharkdp/fd/releases/download/v10.5.0/fd-v10.5.0-aarch64-apple-darwin.tar.gz
verify     sha256 the release publishes
programs   fd
installed  no
...
plan: nothing was changed
```

It names the version, the download or the build, how oku checks it, the
programs, and whether the manifest runs commands. It takes the same ref,
`@version`, `--asset` and `--bin` as `add`, so you can check a version or an
asset before you install it. [A plan](../reference/commands.md#a-plan) lists
every row.

`--manifest` prints the manifest instead. For a repo with no manifest, it
prints the manifest oku infers, with an artifact for every platform. Save it,
edit it, and add the file:

```sh
oku add github:sharkdp/fd --manifest > fd.toml
oku add ./fd.toml
```

## Add a package from another forge

The same short form works on GitLab, Codeberg, and any Gitea or Forgejo
server:

```sh
oku add gitlab:gitlab-org/cli              # the glab CLI
oku add codeberg:mergiraf/mergiraf
oku add gitea:gitea.com/gitea/tea          # a gitea: ref always names its host
```

A GitLab project can sit in subgroups, as in `gitlab:group/sub/project`. A
self-hosted server goes first in the ref, as in `gitlab:git.example.com/group/project`
or `github:ghe.example.com/owner/repo` for GitHub Enterprise Server. oku reads
a first part with a dot as a host, so a gitlab.com group with a dot in its name
needs the host written out, as in `gitlab:gitlab.com/my.group/project`.

None of these need git installed. oku reads each forge's API.

## Add a manifest from a repo

Some repos hold manifests, their own or a collection of them. A `#` after the
repo names one:

```sh
oku add github:you/recipes#bat                  # bat.toml at the root, else packages/bat.toml
oku add github:you/recipes#packages/zoxide.toml # that file in the repo
```

A fragment with a `/` or ending in `.toml` is a path, and anything else is a
name. This works on every forge.

For any other git repo, put `git+` in front of its URL. This form needs `git`
on your `PATH`:

```sh
oku add git+https://git.example.com/you/recipes#lazygit
oku add git+ssh://git@git.example.com/you/recipes#lazygit
```

`git+` also takes `http://` and `file://`. git runs without a password prompt,
so a private repo needs an ssh URL with a loaded key.

## Add from a URL or a local file

A URL can name a manifest or the download itself:

```sh
oku add https://example.com/recipes/starship.toml
oku add https://github.com/sharkdp/hyperfine/releases/download/v1.19.0/hyperfine-v1.19.0-aarch64-apple-darwin.tar.gz
```

A URL that ends in `.toml` is always a manifest. For any other URL oku tries a
manifest first, and treats the file as the download when it is not one. A
download gets a manifest for this machine's OS and CPU only, so another kind of
machine cannot install it from your list. Its version comes from the file name,
and `oku update` never changes it. To move on, add the URL of the newer
version, or write a manifest that
[follows a download URL](../reference/manifest.md#follow-a-download-url). A URL on its own has no checksum, so oku trusts the first download
and pins its sha256 in `oku.lock`.

A manifest on disk works too:

```sh
oku add ./ripgrep.toml
```

`oku add` writes a file inside the list's directory as `./path`, relative to
the list, and any other file as its absolute path. A local path only works on the
machine that has the file, so use repo or URL refs in a list that other
machines read.

[Refs](../reference/refs.md) lists every form and how oku fetches each one.

## Use a short name for a collection

A [source](../how-oku-works.md#source) is your own alias for a repo or a
directory of manifests, so `recipes/bat` stands for
`github:you/recipes#bat`. oku ships with no sources.

```
$ oku source add recipes github:you/recipes
recipes is github:you/recipes
$ oku add recipes/bat
added bat 0.26.1
$ oku search lazy
recipes/lazygit  Terminal UI for git
```

The alias only shortens what you type. `oku add` writes the full ref to
`oku.toml`, so your list works on a machine that has no such alias, and
`oku source remove recipes` changes no installed package. `oku search` looks
in your sources and nowhere else.

A source can be a `github:` repo, a `git+` repo, a local directory, or a URL
prefix. A URL source finds `<url>/<name>.toml` only and cannot be searched.
Aliases live in `config.toml`, see [Refs](../reference/refs.md).

## Add npm, PyPI, Go and crates.io packages

```sh
oku add npm:prettier
oku add pypi:ruff
oku add go:mvdan.cc/gofumpt
oku add cargo:just
```

These need a toolchain named in `[runtimes]`. See
[npm, PyPI, Go and Cargo packages](npm-pypi-go-cargo.md).

## Pick a version

Without a version, `oku add` takes the newest. Add `@` and a version to pick
one:

```
$ oku add github:BurntSushi/ripgrep@14.1.1
added ripgrep 14.1.1
```

`oku.toml` records it beside the ref:

```toml
[packages]
ripgrep = { ref = "github:BurntSushi/ripgrep", version = "14.1.1" }
```

A version that no release has exactly is a prefix. `@14` picks the newest
14.x, and `@1.26` the newest 1.26.x. A range picks the newest version inside
it:

| Range | Allows |
|---|---|
| `^1.4` | 1.4 and newer, below 2. `^0.4` allows versions below 0.5. |
| `~1.4` | 1.4 and newer, below 1.5. |
| `>=1.2, <2` | Every part must hold. A part is `>=`, `>`, `<=`, `<` or `=` and a version. |

Quote a range in the shell:

```sh
oku add 'npm:prettier@^3'
```

A version that upstream does not have fails and names the five newest:

```
$ oku add github:sharkdp/fd@99.0
oku: github-releases sharkdp/fd has no version 99.0, the newest are 10.5.0, 10.4.2, 10.4.1, 10.4.0, 10.3.0
```

The version in `oku.toml` says how far `oku update` may move. The lock pins
the exact build. See [Update packages](#update-packages).

## Fix a wrong pick with --asset and --bin

Inference can pick the wrong release file, or fail to tell which file in it is
the program. It also exposes only the program and the executables next to it
whose names start with the program's name and a `-`, such as `age-keygen`
next to `age`. Other programs in the download need `--bin`. `--asset` names
the release file with a glob, and `--bin` names the program inside it:

```sh
oku add github:owner/repo --asset 'tool-*-macos.zip' --bin tool-cli
```

- `--asset` must match exactly one file of the release. A glob that matches
  none fails and lists every file the release has.
- `--bin` is the file name of a program inside the download. Give it once per
  program, as in `--bin node --bin npm`. It also works for a URL of a
  download.
- oku records both in `oku.lock`, so `oku update` infers the next version the
  same way. Pass them again to change them.
- Both describe one download, so pass one ref with them.
- Both apply to an inferred manifest only. A ref with a manifest fails with
  `--asset and --bin apply when oku infers a manifest`.

When an install from an inferred manifest fails, the error names the file oku
chose, lists the other files that fit your machine, and gives the
`oku add --asset` command that picks one. [Troubleshooting](../troubleshooting.md#oku-picked-the-wrong-file-of-a-release)
has the messages.

When neither flag fixes it, write a manifest yourself. `oku manifest init
--from owner/repo` writes the inferred one to `oku.pkg.toml` as a start, see
[Publish a manifest](publish-a-manifest.md). The
[manifest reference](../reference/manifest.md) lists the inference rules.

Inference also names the package after the repo, so `github:cli/cli` installs
a package called `cli` whose program is `gh`. It writes programs and man pages
only, and completions need a manifest.

## See what is outdated

`oku outdated` asks upstream for versions and changes nothing:

```
$ oku outdated
ripgrep  14.1.1  14.1.1  15.2.0  github:BurntSushi/ripgrep
`oku update` takes the newest versions. To take a latest beyond them, change its version in oku.toml
```

The columns are the name, the locked version, the newest version that the
`version` in `oku.toml` allows, the latest release, and the ref. On a terminal
they have headers. Here `version = "14.1.1"` holds ripgrep at 14.1.1, so the
newest allowed is 14.1.1 while the latest is 15.2.0. A package shows up when
either one is newer than the lock. `--json` prints the same rows for a script,
see [CI](ci.md).

## Update packages

Versions stay where the lock pins them until you run `oku update`:

```
$ oku update
ripgrep 14.1.1 -> 15.2.0
profile now holds 2 packages, generation 3, 4s
```

With no names it updates every package in `oku.toml` and reads included lists
fresh. `oku update ripgrep` updates one package and keeps included lists
pinned.

A package moves to the newest version its `version` in `oku.toml` allows. An
exact version stays where it is, `^1.4` moves to the newest 1.x, and no
version moves to the newest release. To take a release beyond the constraint,
change the `version` in `oku.toml` first. When you change it so that it no
longer allows the locked version, `oku sync` picks the newest version it
allows without an update.

`oku update` also accepts a manifest that changed and a checksum that upstream
changed, and prints a line for each:

```
ripgrep 14.1.1, manifest changed
tool 1.0.0, checksum changed
```

Read those lines, and the diff of `oku.lock`, before you commit it. If an
update breaks something, `oku rollback` puts the previous generation and its
lock back, see [Undo a change](undo-and-clean-up.md).

## Remove a package

```
$ oku remove fd
removed fd
```

`oku remove` takes the package out of the profile, `oku.toml` and `oku.lock` in
one generation. Its files stay in the store, so `oku rollback` or a new
`oku add` needs no download. `oku gc` deletes them later, see
[Undo a change and free space](undo-and-clean-up.md#free-disk-space).

`oku remove` refuses a package that only an included list declares, because oku
never edits an included list. Remove it there, or take the include out.

## Give oku a GitHub token

GitHub allows 60 API requests an hour without a login, and a first list of a
dozen packages can use them up. oku uses a token when it finds one:

1. `GITHUB_TOKEN`, when it is set.
2. Otherwise the login of the `gh` CLI. oku runs
   `gh auth token --hostname github.com` once per run.

```sh
export GITHUB_TOKEN=$(gh auth token)
```

oku keeps each API answer in its cache and next time asks GitHub only whether
it changed. With `GITHUB_TOKEN` set, GitHub does not count an unchanged answer
against the limit.

Other hosts read their own variable, and oku sends each token to its own host
only:

| Host | Variable |
|---|---|
| github.com | `GITHUB_TOKEN` |
| GitHub Enterprise Server | `GH_ENTERPRISE_TOKEN` |
| gitlab.com | `GITLAB_TOKEN` |
| another GitLab server | `GITLAB_SERVER_TOKEN` |
| codeberg.org | `CODEBERG_TOKEN` |
| another Gitea or Forgejo server | `GITEA_TOKEN` |

The same tokens install from a private repo on Codeberg, Gitea, Forgejo and
GitLab. On GitHub a token reads a private repo's manifest and releases, but the
download of a private release fails, because GitHub serves those files through
its API only. `oku sync` on another machine needs the token too.

## Next

- [Keep a second machine in step](new-machine.md)
- [npm, PyPI, Go and Cargo packages](npm-pypi-go-cargo.md)
- [Every flag of add, update, outdated and remove](../reference/commands.md)
