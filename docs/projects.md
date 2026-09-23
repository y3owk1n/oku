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

A project needs the hook line in your shell's startup file, the same line that
puts oku on `PATH`. [Set up your shell](getting-started.md#set-up-your-shell) has
it for bash, zsh, fish and PowerShell. `oku doctor` says whether it is in place.

Then allow the project, once:

```
$ cd ~/work/api
oku: /home/you/work/api/oku.toml is not allowed, run `oku allow` to use its programs here
$ oku allow
allowed /home/you/work/api
$ which rg
/home/you/.local/share/oku/profiles/project-2d27013d8c67/current/bin/rg
$ cd ~ && which rg
/home/you/.local/share/oku/profiles/global/current/bin/rg
```

Inside the project, in any subdirectory, the hook puts the project's `bin` first
on `PATH` and exports the [`[env]`](manifest.md#env) of its packages. A project
program therefore runs instead of a global one with the same name. When you
leave, the hook removes both again.

### Why a project has to be allowed

A cloned repo could contain an `oku.toml` that puts its own `make` or `git`
ahead of yours. So the hook does nothing for a project until you run
`oku allow`.

- The allow belongs to the `oku.toml` as it is at that moment. After any edit,
  including a `git pull` that changes it, the hook stops and asks again.
- `oku deny` removes the allow.
- `oku allow` and `oku deny` take a directory, and default to the project you
  are in.

### When the hook does nothing

The hook prints one line, once per directory, and changes nothing when:

- the project is not allowed, or its `oku.toml` changed. Run `oku allow`.
- the project's profile is behind its `oku.lock`, for example after a teammate's
  commit. Run `oku sync`.

The hook reads local files only. It never uses the network, never installs, and
never runs anything from a manifest. One run takes a few milliseconds.

### direnv

If you use direnv, `oku env` prints what the hook would apply:

```sh
# .envrc
eval "$(oku env --shell bash)"
```

### Editors and scripts

An editor or a script does not run the shell hook. `oku exec` gives a command
the same `PATH` and `[env]` as the hook would, without `oku allow`, so an editor
can start its language server as `oku exec gopls`. See
[`oku exec`](commands.md#oku-exec).

## A teammate's first run

```
$ git clone git@github.com:you/api && cd api
$ oku sync
project /home/you/api
profile now holds 3 packages
```

`add` and `update` pin each package for your own platform. Name the platforms
of your teammates' machines in `[lock]`, and a teammate on Linux installs from
the lock that your Mac wrote. Without it, the first `sync` on their machine
adds the entry and they commit the lock back, see
[One lock for several machines](list-and-lock.md#one-lock-for-several-machines).

`sync` installs what `oku.lock` pins, as it does for the global list. A package
that builds from source asks for approval on each machine.

## Uninstalling

`oku self uninstall` removes every profile, including project profiles. It never
touches a project's `oku.toml` or `oku.lock`.
