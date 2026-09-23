# Troubleshooting

Find the symptom or the message you see, then follow the fix.

When a command fails, oku prints `oku: <reason>` on stderr and exits with
status 1. The lines after the first say what to do. `add`, `remove`, `sync`,
`update` and `rollback` change the machine all the way or not at all, so after
a failure your machine is as it was before the command.

Start with `oku doctor`. It checks the store, `PATH`, the shell hook, the
build sandbox, the profiles, your secrets setup and any unfinished change, and
names the fix for each problem:

```sh
oku doctor
```

## A program does not run after oku add

The shell cannot find the programs oku installed until the profile's `bin` is
on `PATH`. `oku add` says so when it is missing:

```
to run it, add this line to ~/.zshrc, then open a new terminal:
  [ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"
```

Add that line, then open a new terminal. [Set up your shell](getting-started.md#2-set-up-your-shell)
has the line for bash, fish and PowerShell. When oku does not know your shell,
it prints `add <dir> to PATH to run it` instead, with the directory to add.

`oku doctor` reports the same problem as:

```
problem  ~/.local/share/oku/profiles/global/current/bin is not on PATH, so installed programs do not run by name. The hook line puts it there
```

Inside a [project](how-oku-works.md#project), a package goes to the project's
profile. oku prints `this project's programs are in <dir>`, and the hook puts
them on `PATH` once you run `oku allow`, see [Projects](guides/projects.md).

## Another program runs in place of oku's

A program of the same name earlier on `PATH`, such as `/usr/bin/rg`, runs
instead of oku's. `oku which` shows which one wins:

```
$ oku which rg
program  rg
package  ripgrep 15.2.0
path     ~/.local/share/oku/store/ripgrep-15.2.0-82921c938f26f8ff/pkg/rg
PATH runs /usr/bin/rg instead, because its directory comes first. `oku doctor` says how to fix that
```

Put the profile's `bin` before that directory, or remove the other copy. `oku doctor` names every hidden program in one line, because one
`PATH` change fixes them all:

```
problem  /usr/bin/rg runs in place of oku's rg, because /usr/bin is earlier on PATH. Put ~/.local/share/oku/profiles/global/current/bin before it, or remove the other copy
```

The shell hook moves oku's directories to the front of `PATH` even when they
are there already. A login shell that tmux starts puts the system's
directories first, and the hook puts oku's back in front.

For a program oku did not install, `oku which` says so:

```
$ oku which ls
oku: ls is not from oku, PATH runs /bin/ls
`oku search ls` looks for a package that provides it
```

## GitHub rate limit reached

```
oku: GitHub rate limit reached, set GITHUB_TOKEN to raise it
```

GitHub allows 60 API requests an hour without a login. Set `GITHUB_TOKEN`, or
log in with the `gh` CLI, which oku uses when the variable is not set:

```sh
export GITHUB_TOKEN=$(gh auth token)
```

A different message means too many requests in a short time:

```
oku: GitHub asked oku to slow down, try again in 60 seconds
```

Wait that long and run the command again. See
[Give oku a GitHub token](guides/add-packages.md#give-oku-a-github-token).

## oku picked the wrong file of a release

For a repo with no manifest, oku picks a release file for your machine. When
it picks wrong or cannot pick, name the file with `--asset` and the program
with `--bin`. See [Fix a wrong pick](guides/add-packages.md#fix-a-wrong-pick-with---asset-and---bin).

| Message | Fix |
|---|---|
| `no release asset fits this machine (darwin-arm64)`, then the files of the release | No file name says your OS and CPU. Pass one of the listed files to `--asset`. |
| `cannot tell which file is the program, executables found: ...` | The download holds several programs and none has the repo's name. Pass one to `--bin`. |
| `no file in it is executable` | Write a manifest, see [Publish a manifest](guides/publish-a-manifest.md). |
| `it chose the asset <file> for this machine` | The install failed. The next lines list the other files that fit, and the `oku add --asset` command that picks one. `--verbose` adds the manifest oku inferred. |
| `--asset "<glob>" names 0 assets, want one of: ...` | The glob matches no file. Pick a name from the list. It must match exactly one. |
| `--asset and --bin apply when oku infers a manifest, and <ref> has one` | The ref has a manifest, so these flags do nothing. Drop them. |
| `--asset and --bin do not apply, <ref> names its programs` | An `npm:`, `pypi:`, `go:` or `cargo:` ref. The registry names the download. |
| `<ref> has no manifest and no release to infer one from` | The repo has neither. Write a manifest, or point at one elsewhere. |

## oku cannot find the package or the version

| Message | Fix |
|---|---|
| `there is no file named ripgrep here` | A bare word is a local file. Write a full ref, such as `github:BurntSushi/ripgrep`, or `alias/name` for a [source](guides/add-packages.md#use-a-short-name-for-a-collection). `oku add --help` lists every form. |
| `<alias> is not a source and <arg> is not a file, see oku source list` | No source has that alias. `oku source list` shows yours. |
| `... has no version 99.0, the newest are 10.5.0, ...` | Pick a version from the list, or drop `@version`. |
| `the manifest provides version X, not Y` | The manifest fixes one version, and `@version` asked for another. |
| `<name> has no artifact for darwin-arm64` | The manifest has no download for this machine and no build. |
| `<name> has no [build], so it cannot be built from source` | Drop `--from-source`. |

## The manifest changed since oku.lock was written

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

`oku sync` checks each manifest against the hash in `oku.lock` before it
downloads anything. A local file or a URL has no commit to pin, so an edit
shows up here. Read what changed, then run the `oku update` it names. When
several packages drifted, one error lists them all and ends with one
`oku update` that names each.

An included list works the same way:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

## Checksum mismatch

```
oku: checksum mismatch for <url>: expected <digest>, download is <digest>
```

The download is not the file the manifest or the lock pinned. oku deleted it
and installed nothing. Run the command again in case the download broke on the
way. If it fails again, the file on the server changed. Ask the project before
you trust it.

A checksum file upstream can also change:

```
oku: ripgrep: checksum changed: upstream publishes sha256 <new>, oku.lock pinned <old>
run `oku update ripgrep` to accept the new checksum
```

oku cannot tell an upstream that replaced a release file from an attack. Check
with the author before you run the `oku update`.

## The vendored packages changed

```
oku: the vendored packages changed: oku.lock pinned <digest>, this build downloaded <digest>
run `oku update tool` to accept what it downloads now
```

A build downloads its dependencies in a vendor step, and the lock pins a digest
of them. This time it got different ones, and oku installed nothing. `oku update`
accepts the new set.

## The signing key changed

```
oku: foo: oku.lock pinned the signing key RWTr8ko..., and the manifest now has the signing key RWSwtYz...
if the developer announced this change, run the command again with --accept-key
```

A new key is what someone who took over the repo would publish. Check with the
developer first. See [Security](reference/security.md).

## oku sync --locked fails

`--locked` never changes `oku.lock`, for CI that cannot commit it back.

```
oku: ./oku.lock does not pin ripgrep for linux-amd64-glibc
run `oku sync` without --locked, and commit oku.lock
```

```
oku: ./oku.lock is out of date, and --locked does not change it
run `oku sync` without --locked, and commit oku.lock
```

Run `oku sync` on a machine, commit the lock, and push. To pin other platforms
from your own machine, name them in `[lock] platforms`, see
[A new machine](guides/new-machine.md). A `pypi:` package cannot be pinned
from another kind of machine, so a machine of that platform has to build it
once.

## A package has nothing for this platform

```
rectangle has no artifact or build for linux-amd64-glibc, so sync did not install it
change its line in ~/.config/oku/oku.toml to
  rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
```

`oku sync` installed the rest of the list and never edits `oku.toml`. Paste the
line it gives. See [One list for several OSes](guides/new-machine.md).

## A build needs approval

```
oku: just needs approval to run them, and this is not a terminal
pass --yes to approve
```

A build runs commands on your machine, so oku asks first. In a script there is
nobody to ask. Read the commands it printed above the error, then run it again
with `--yes`. Answering no to the question gives
`not approved, nothing was built`.

## A build fails

| Message | Fix |
|---|---|
| `the build needs "<tool>", which is not on PATH` | Install that tool. oku does not install what a manifest `needs`. |
| `build.step[N] (run) failed`, then the end of its output | A build step failed. `--verbose` shows its whole output. |
| `no version satisfies ">=9", the versions found are ...` | A dep's version constraint matches nothing upstream. |
| `dependency cycle: ...` | Two manifests depend on each other. Fix one of them. |

## A build ran without the sandbox

```
tree was built without the sandbox, because this host does not let an unprivileged user set up namespaces (...)
its build commands could use the network and read your files
```

On Linux the sandbox needs unprivileged user namespaces. Ubuntu 24.04 and
later forbid them by default, through AppArmor, and so does a default Docker
container. To allow the sandbox on Ubuntu:

```sh
sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0
```

Put the same setting in a file under `/etc/sysctl.d/` to keep it after a
reboot. Windows has no sandbox at all. `oku doctor` shows the state as a
`note`.

## A file already exists

```
oku: ~/.config/nvim already exists and oku did not put it there
move it away, or take it out of [files]
```

oku never overwrites a file it did not write. Move the file away, then run the
command again. For an app or a font the message ends with
`so <package> cannot expose its app` instead. See [Dotfiles](guides/dotfiles.md).

A template that uses a name `[vars]` does not set stops with
`<name> is not set in [vars]`, before anything changes.

## Two packages provide the same program

```
oku: <a> and <b> both provide bin/<program>
```

Two packages ship a file of the same name, so oku refuses the second one. Keep
one of them.

## oku remove or oku add refuses

| Message | Fix |
|---|---|
| `fd is not installed, so nothing was removed` | `oku list` shows the names. |
| `fd is not in ~/.config/oku/oku.toml, so it comes from an include` | oku never edits an included list. Remove it there, or take the include out. |
| `... defines fd as a [packages.fd] table, edit it by hand` | oku edits one-line entries only. Edit that table yourself. |
| `fd is not in ~/.config/oku/oku.toml or its includes` | `oku update` got a name the list does not have. |

## oku sync with a list ref refuses

```
oku: ~/.config/oku/oku.toml already has packages or includes, so oku will not replace it
add "github:you/machines" to its include array and run `oku sync`
```

`oku sync github:you/machines` sets up a machine whose list is empty. On any
other machine, add the ref to `include` as it says. Inside a project it refuses
too, and `--global` sends it to your own list. A list ref takes no `@version`.
See [A new machine](guides/new-machine.md).

## The last change did not finish

When an oku process is killed halfway through a change, the next `add`,
`remove`, `sync`, `update` or `rollback` puts the machine back first:

```
the last change did not finish, so oku put generation 4 back
```

Until then, `oku gc` and any `--dry-run` refuse to run and tell you to run
`oku sync` first. When oku cannot undo a step, for example because the service
manager refuses, it names the step. `oku doctor` then reports
`a change from generation 4 to 5 did not finish` until a later command
succeeds. Fix what it names, then run `oku sync`.

## oku waits for another oku process

```
waiting for oku process 4312 to finish
```

Only one oku changes the machine at a time. The command goes on when the other
one ends. Commands that only read, such as `list`, `generations` and `doctor`,
never wait. A killed oku leaves no stale lock, because the OS releases it.

## npm programs cannot find node

Without `[runtimes] node`, an `npm:` program runs the `node` on your `PATH`,
and `oku add` says so. On Windows the add fails instead:

```
oku: npm:prettier needs node, and Windows cannot run a script through PATH
set runtimes.node in ~/.config/oku/oku.toml to the ref of a package that provides node
```

See [npm, PyPI, Go and cargo packages](guides/npm-pypi-go-cargo.md).

## Windows

- A home file that oku copied, rather than linked, and that you edited stops
  the next change with `<path> changed since oku wrote it, and a sync would
  overwrite it`. Move the change into its source or into `oku.toml`, delete
  the file, and run the command again.
- A build has no sandbox, and oku says so after every build.
- `oku self uninstall` removes `oku.exe` a few seconds after it returns.

See [Windows](guides/windows.md) for what else differs.
