# Projects

A project is a directory with its own `oku.toml`. Inside it, oku installs into
a profile that belongs to that project, so a repo can pin the tools it needs
without changing your global setup. A teammate gets the same versions with
`oku sync`.

## Start a project

Create an empty list at the root of the repo, then add packages from anywhere
inside it:

```
$ cd ~/work/api
$ touch oku.toml
$ cd src/handlers
$ oku add github:BurntSushi/ripgrep@14.1.1
project /home/you/work/api
added ripgrep 14.1.1
this project's programs are in /home/you/.local/share/oku/profiles/project-2d27013d8c67/current/bin
```

oku wrote `oku.toml` and `oku.lock` in `~/work/api`. Commit both. The first line
of output, on stderr, names the project oku is using.

## How oku finds the project

oku looks for `oku.toml` in the directory you run it in, then in each parent
directory, and uses the nearest one. With none, it uses your global list. Your
config directory holds the global list and never counts as a project.

These commands act on the project: `add`, `remove`, `list`, `sync`, `update`,
`info`, `why`, `generations` and `rollback`. Pass `--global`, or `-g`, to use
the global list from inside a project:

```
$ oku list
project /home/you/work/api
ripgrep  14.1.1  github:BurntSushi/ripgrep

$ oku list --global
ripgrep  15.2.0  github:BurntSushi/ripgrep
```

`gc`, `source`, `search`, `manifest` and `self` do not use a list.
`oku gc` looks at every profile, global and project, before it deletes anything.

`oku sync <list-ref>` sets up the global list. Inside a project oku refuses it
and tells you to add the ref to the project's `include` array instead.

## What a project owns

| | Global | Project |
|---|---|---|
| List and lock | `~/.config/oku/oku.toml`, `oku.lock` | `oku.toml`, `oku.lock` in the project directory |
| Profile | `profiles/global` | `profiles/project-<hash>` |
| Store, cache, sources, build approvals | shared | shared |

Both kinds of profile link into the same store, so oku downloads and keeps a
package once when the global list and a project both use it.

The profile name comes from a hash of the project's path. If you move the
directory, run `oku sync` there and the project gets a new profile. `oku gc`
removes what the old one used once its generations are gone.

Everything a list can do works in a project list: `include`, `when`, version
pins and relative refs, which start at the project directory. See
[List and lock](list-and-lock.md).

## Using the project's programs

oku does not put a project's programs on your `PATH` yet. The shell hook that
does it when you enter the directory is not built. Until then, add the `bin`
directory that `oku add` prints yourself, for example from a direnv `.envrc`:

```sh
PATH_add "$HOME/.local/share/oku/profiles/project-2d27013d8c67/current/bin"
```

## A teammate's first run

```
$ git clone git@github.com:you/api && cd api
$ oku sync
project /home/you/api
profile now holds 3 packages
```

`sync` installs what `oku.lock` pins, as it does for the global list. A package
that builds from source asks for approval on each machine.

## Uninstalling

`oku self uninstall` removes every profile, including project profiles. It never
touches a project's `oku.toml` or `oku.lock`.
