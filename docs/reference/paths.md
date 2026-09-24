# Paths and environment

Where oku keeps its files, what it writes outside them, and the environment
variables it reads.

## The three directories

oku writes under three directories of its own, and needs no root.

| Directory | macOS and Linux | With the variable set | Windows |
|---|---|---|---|
| Config | `~/.config/oku` | `$XDG_CONFIG_HOME/oku` | `%APPDATA%\oku` |
| Data | `~/.local/share/oku` | `$XDG_DATA_HOME/oku` | `%LOCALAPPDATA%\oku` |
| Cache | `~/.cache/oku` | `$XDG_CACHE_HOME/oku` | `%LOCALAPPDATA%\oku\cache` |

The XDG variables win on every OS, Windows included. Point them at a scratch
directory to try oku without touching your real setup:

```sh
export XDG_CONFIG_HOME=/tmp/oku-try/config
export XDG_DATA_HOME=/tmp/oku-try/data
export XDG_CACHE_HOME=/tmp/oku-try/cache
```

## Layout

```
<config>/oku/
  oku.toml                     your list, see oku-toml.md
  oku.lock                     what oku resolved, see lock.md
  config.toml                  sources, caches, trusted keys, runtimes, store_root
  signing.key                  the secret key of "oku cache push", after "oku key generate"

<data>/oku/
  store/
    ripgrep-14.1.1-77da99cdceeb3140/
      pkg/                     the whole unpacked download
      bin/rg -> ../pkg/rg
      share/man/man1/rg.1 -> ../../../pkg/doc/rg.1
      oku-meta.toml            name, version, platform, url, sha256
    machines-5f3a9c1e2b7d-<hash>/  the files of a repo whose list holds [files], at one commit
  profiles/
    global/
      gen-1/                   one directory per profile change
      gen-2/
        bin/rg -> <store path>/bin/rg
        share/...
        oku-gen.toml           when it was written, its packages, and the store paths of their deps
        oku.lock               a copy of oku.lock as it was then
        files/                 the content of the text entries of [files]
      current -> gen-2         the active generation
    project-2d27013d8c67/      one per project, same layout
  exposed.toml                 the ledger: every file and setting oku wrote outside these directories
  pending.toml                 exists only while oku applies a change
  busy                         the pid of the oku that changes the machine now
  secrets/                     decrypted secrets, which only you can read
  services/                    service definitions that are not enabled (macOS), every definition (Windows)
  logs/                        output of services (macOS, Windows)
  shims/                       the copy of oku.exe that shims link to (Windows)
  trust/
    approvals.toml             manifests you allowed to run build commands
    allow.toml                 projects the shell hook may apply

<cache>/oku/
  downloads/<sha256>           verified downloads, reused on reinstall
  downloads/by-url/<hash>      the digest each url gave in the last day, so a run that stopped early does not download again
  git/<hash>/                  clones for git+ refs
  api/<hash>                   answers of forge and registry APIs, asked again with their ETag
```

## The store

A store path is named `<name>-<version>-<hash>`. The hash covers the manifest
content, the version, the platform and the artifact's sha256, so a changed
manifest gets a new path and never overwrites an old one. For a build it also
covers the deps, and the store root unless the manifest says
`relocatable = true`.

- oku unpacks a download in a temporary directory inside the store and renames
  it into place last. A failed install leaves no files in the store.
- A build installs straight into its final store path, because build systems
  write that path into their files. oku writes `oku-meta.toml` last. A store
  path without it is a crashed build, and oku deletes it before it builds
  again. A build has no `pkg/`.
- `oku remove` leaves store paths in place, because older generations use
  them. [`oku gc`](commands.md#oku-gc) deletes the ones no generation uses.
- After [`oku setup --system`](commands.md#oku-setup) the store is
  `/opt/oku/store`, and `store_root` in `config.toml` says so. Everything else
  stays where it was.

## Profiles and generations

Each list has a profile: `profiles/global` for the global list, and
`profiles/project-<hash>` for a [project](../how-oku-works.md#project), named
after a hash of the project's path. Moving a project directory gives it a new
profile on the next `oku sync`.

Put `<data>/oku/profiles/global/current/bin` on `PATH`, which the shell hook
does. A change writes a new `gen-<n>` directory of links and then moves
`current` to it in one rename. Old generations stay until `oku gc --keep N`
deletes them.

On Windows a generation holds shims and hard links in place of symlinks, and
`current` is a directory junction, see [Windows](../guides/windows.md).

## Outside oku's directories

An app, a font or a service only works where the OS reads it, so oku copies or
writes those out of the store:

| | macOS | Linux | Windows |
|---|---|---|---|
| Apps | `~/Applications/` | `<data home>/applications/oku-<name>.desktop` | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk` |
| Fonts | `~/Library/Fonts/` | `<data home>/fonts/oku/` | `%LOCALAPPDATA%\Microsoft\Windows\Fonts\`, and a value under `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |
| Enabled services | `~/Library/LaunchAgents/dev.oku.<name>.plist` | `<config home>/systemd/user/oku-<name>.service` | a scheduled task `oku-<name>` |

`<data home>` is `$XDG_DATA_HOME`, else `~/.local/share`. `<config home>` is
`$XDG_CONFIG_HOME`, else `~/.config`.

With `system = true` they go to the machine-wide places:

| | macOS | Linux | Windows |
|---|---|---|---|
| Apps | `/Applications/` | `/usr/local/share/applications/oku-<name>.desktop` | `%ProgramData%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk` |
| Fonts | `/Library/Fonts/` | `/usr/local/share/fonts/oku/` | `%SystemRoot%\Fonts\`, and a value under `HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |
| Enabled services | `/Library/LaunchDaemons/dev.oku.<name>.plist` | `/etc/systemd/system/oku-<name>.service` | a scheduled task `oku-<name>` as `SYSTEM` |
| Other service files | `/Library/Application Support/oku/services/`, `/Library/Logs/oku/<name>.log` | the system journal | `%ProgramData%\oku\services\`, `%ProgramData%\oku\logs\` |
| Shared store root | `/opt/oku` | `/opt/oku` | `%ProgramData%\oku` |

oku also writes:

- the paths of [`[files]`](oku-toml.md#files). A `link` points at your source.
  A `text` points at `<data>/oku/profiles/global/current/files/`, so moving
  `current` changes every such file in the same step. A `secret` points into
  `<data>/oku/secrets/`.
- the keys of the [settings tables](oku-toml.md#settings-tables)

Before oku creates one of these, it records it in the ledger `exposed.toml`
with the package or list it came from. For a setting it also records the value
it had before.
`oku remove`, `oku rollback` and `oku sync` remove what the active generation
no longer has, and `oku self uninstall` removes everything in the ledger. oku
never overwrites a file that is not in its ledger.

oku does not edit shell startup files, the system `PATH`, or anything else
outside these places.

## Environment variables

| Variable | Effect |
|---|---|
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME` | Move the three directories, see above. `XDG_CONFIG_HOME` also moves the age key file and `{{config}}`, and `XDG_DATA_HOME` moves `{{data}}`. |
| `APPDATA`, `LOCALAPPDATA` | The Windows defaults of the three directories, when the XDG variables are not set. |
| `HOME` | Your home directory, for the default directories and `{{home}}`. |
| `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `CODEBERG_TOKEN`, `GITEA_TOKEN`, `GITLAB_TOKEN`, `GITLAB_SERVER_TOKEN` | Tokens for forge APIs and downloads, each sent to its own host only. See [tokens per host](refs.md#tokens-per-host). |
| `SOPS_AGE_KEY_FILE` | The age key file for [secrets](oku-toml.md#secrets). Without it oku reads `sops/age/keys.txt` in your config directory: `~/.config/sops/age/keys.txt`, or `%APPDATA%\sops\age\keys.txt` on Windows. oku passes the same path to `sops`. |
| `OKU_PARALLEL` | How many packages `sync` and `update` install at once. Default `8`. A number from 1 up, so `OKU_PARALLEL=1` installs one after another. |
| `OKU_ENV` | Names the project's `oku.<env>.toml`, whose [`[env]`](oku-toml.md#okuenvtoml-and-okulocaltoml) the hook, `oku exec` and `oku env` apply over the `oku.toml`. |
| `NO_COLOR` | Any value turns colour off on a terminal. |
| `FORCE_COLOR` | Any value turns colour on for a pipe, such as a pager. |
| `COLUMNS` | The width under `FORCE_COLOR`. |
| `TERM` | `dumb` prints plain text, as in a pipe. |
| `SHELL` | The shell `oku shell` starts. |
| `PATH` | Where oku looks for `git`, `gh`, `sops` and build `needs`, and what `oku which` and `oku doctor` check. |
| `RUSTUP_HOME`, `RUSTUP_TOOLCHAIN` | Passed to a build that needs `cargo` or `rustc` from rustup. `RUSTUP_HOME` defaults to `~/.rustup`. |

oku sets these for others:

| Variable | Set in |
|---|---|
| `OKU_SHELL` | The shell of `oku shell`, to the refs it holds. |
| `OKU_PREFIX`, `OKU_SRC`, `OKU_JOBS` | A build's steps, see the [manifest reference](manifest.md). |
| `GIT_TERMINAL_PROMPT=0` | Every `git` oku runs. |
| `OKU_HOOK_SAVED`, `OKU_HOOK_ADDED`, `OKU_HOOK_HINT` | Your shell, by the hook, to undo what it applied. The hook removes `OKU_HOOK_PATH` and `OKU_HOOK_KEYS`, the state of an older oku. |

The install scripts read `OKU_INSTALL_DIR` and `OKU_VERSION`, see
[Getting started](../getting-started.md).

## How a change applies

`add`, `remove`, `sync`, `update` and `rollback` work in two parts.

1. oku checks everything it can without changing the machine. It downloads,
   verifies and builds into the store, builds the new generation beside the
   active one, decrypts secrets in memory, and checks that no app, font or
   file would overwrite one it did not write. A failure here leaves the
   machine as it was.
2. oku writes `pending.toml`, moves `current`, sets up apps, fonts, services,
   files and settings, writes `oku.toml` and `oku.lock`, and deletes
   `pending.toml`. When a step fails, oku undoes the steps before it, deletes
   the new generation and prints the error of the step that failed.

`pending.toml` holds the two generation numbers and the text of `oku.toml` and
`oku.lock` from before. When an oku process is killed halfway, the next of
these commands finds the file, puts the machine back first, and prints:

```
the last change did not finish, so oku put generation 4 back
```

`oku gc` refuses to run until that has happened. When oku cannot undo a step,
for example because the service manager refuses, it says which one and keeps
`pending.toml`, and `oku doctor` reports it until a later command succeeds.

No OS offers one atomic step that covers files, services and the list
together, so oku does not promise one. It checks first and undoes on failure.

## One change at a time

Only one oku process changes the machine at a time. Before it changes
anything, each of these commands takes a lock on `busy`:

- `add`, `remove`, `sync`, `update`, `rollback` and `gc`
- `allow` and `deny`
- `source add`, `source remove`, `cache add`, `cache remove`, `cache push`,
  `key generate`, `key trust` and `key revoke`
- `self uninstall`, and `shell` while it installs

A second command from that list waits for the first, and says which process it
waits for:

```
waiting for oku process 4312 to finish
```

Commands that only read, such as `list`, `generations` and `doctor`, never
wait. The OS releases the lock when the process ends, so a killed oku leaves no
stale lock.

## The cache

Deleting the cache directory is safe. oku downloads again when it needs to.
`oku gc` does not touch the cache.
