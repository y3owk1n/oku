# Install your tools in CI

You end up with a GitHub Actions job that installs the exact tools your
`oku.lock` pins and puts them on `PATH` for the steps after it.

## Pin the runner's platform first

CI cannot commit a lock back, so the lock must already pin the runner's
platform. Name it in `[lock]` in your `oku.toml`:

```toml
[packages]
golangci-lint = "github:golangci/golangci-lint"
just = "github:casey/just"

[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc"]
```

Run `oku sync` or `oku update` on your machine, and commit `oku.toml` and
`oku.lock`. See
[Pin every platform in one lock](new-machine.md#pin-every-platform-in-one-lock).

## Add the action

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: y3owk1n/oku@v0.6.1
        env:
          GITHUB_TOKEN: ${{ github.token }}
      - run: golangci-lint run
      - run: just test
```

The action does this:

1. It installs oku, unless an `oku` is already on `PATH`.
2. It restores the store and the downloads from the Actions cache.
3. It runs `oku sync --yes --locked` in the directory of your `oku.toml`.
4. It puts the programs of the global profile and of the
   [project](../how-oku-works.md#project) on `PATH` for the later steps. It
   runs `oku allow` for the project and exports the `[env]` of its packages,
   as the shell hook would.

It works on Linux, macOS and Windows runners. Pass `GITHUB_TOKEN`, because
GitHub rate limits anonymous API calls from a shared runner.

## Set the inputs

| Input | Default | What it does |
|---|---|---|
| `version` | `""` | The oku release to install, such as `v0.6.0` or `nightly`. Empty installs the release of the action's own tag, as in `y3owk1n/oku@v0.6.1`, and the newest release otherwise. The action uses an oku already on `PATH` as it is. |
| `path` | `.` | The directory that holds the `oku.toml` to sync. Its lock must pin the runner's platform. |
| `args` | `--locked` | What `oku sync` gets besides `--yes`. |
| `cache` | `"true"` | Keep the store and the downloads between runs, keyed by `oku.lock`. `"false"` turns it off. |

```yaml
- uses: y3owk1n/oku@main
  with:
    path: tools
    version: nightly
    cache: "false"
```

## Pin the oku version

Use the action by a release tag, such as `y3owk1n/oku@v0.6.1`. With no
`version`, the action then installs that same release, so the tag pins oku
too, and a bot that bumps the tag moves both. `@main` installs the newest
release.

## Know what the cache holds

With `cache` on, the action keeps these paths in the Actions cache:

- `~/.local/share/oku/store` and `~/.cache/oku/downloads`
- `~/AppData/Local/oku/store` and `~/AppData/Local/oku/cache/downloads` on
  Windows

The key is `oku-<os>-<arch>-<hash of oku.lock>`. When the lock changed, the job
starts from the newest cache of the same OS and CPU. The store only grows, and
`oku sync` checks what it takes from it, so an older store is a safe start for
a newer lock.

## Know why a sync is locked

`--locked` makes `oku sync` fail when it would change `oku.lock`. It checks
that before it downloads anything, so a locked sync never trusts a download on
first use:

```
oku: ./oku.lock does not pin ripgrep for linux-amd64-glibc
run `oku sync` without --locked, and commit oku.lock
```

It also fails with `oku.lock is out of date` when the lock holds a package that
left the list, or lacks a platform that `[lock]` names. Line endings do not
count, so a lock and a local manifest that git checked out with CRLF on Windows
pass.

A package that builds with a `pip` vendor step cannot be pinned from another
platform. `oku sync --locked` fails on that platform until a machine of that
platform has built it and you have committed the lock.

`--yes` approves the build steps of packages that build from source. Without a
terminal oku cannot ask, and stops with
`<name> needs approval to run them, and this is not a terminal`.

## Run oku in another CI

Outside GitHub Actions, do what the action does:

```sh
curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
export PATH="$HOME/.local/bin:$HOME/.local/share/oku/profiles/global/current/bin:$PATH"
oku sync --yes --locked
oku allow
eval "$(oku env --shell bash)"
```

`oku env` prints the `PATH` and the `[env]` of the project you are in. See
[Projects](projects.md). Cache `~/.local/share/oku/store` and
`~/.cache/oku/downloads` keyed by `oku.lock` for the same effect as the
action's cache.

## Open pull requests for new versions

`oku outdated --json` lists each package with a newer version than the lock
pins, for a bot that opens pull requests:

```
$ oku outdated --json
[
  {
    "name": "fd",
    "version": "10.1.0",
    "newest": "10.1.0",
    "latest": "10.5.0",
    "ref": "github:sharkdp/fd"
  }
]
```

| Field | Meaning |
|---|---|
| `version` | The version `oku.lock` pins. |
| `newest` | The newest version the package's `version` in `oku.toml` allows. `oku update <name>` takes it. |
| `latest` | The newest release. It differs from `newest` when `oku.toml` pins a version or a range that leaves it out, as above. |

A package shows up when either is newer than the lock. With nothing to update
it prints `[]`. oku downloads no package and changes nothing. When a version
source cannot answer, oku lists the rest and exits with status 1.

A bot then runs `oku update <name>` for each package that `oku update` can
move, and commits what changed:

```sh
for name in $(oku outdated --json | jq -r '.[] | select(.newest != .version) | .name'); do
  oku update "$name"
done
```

Call `oku update` with names only. With no names it moves every package.

## See a real workflow

oku's own [ci.yml](../../.github/workflows/ci.yml) uses the action on Linux,
macOS and Windows runners, with `version: nightly`, and then runs `go test` and
`golangci-lint` from the lock.
