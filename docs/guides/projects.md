# Pin tools for one repo

You end up with an `oku.toml` and an `oku.lock` in a repo, and the repo's tools
on `PATH` whenever you are inside it. Your global tools stay as they are, and a
teammate gets the same versions with one `oku sync`.

A directory with its own `oku.toml` is a [project](../how-oku-works.md#project).
Inside it, oku installs into a [profile](../how-oku-works.md#profile) that
belongs to that project.

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

oku wrote `oku.toml` and `oku.lock` in `~/work/api`. Commit both. The first
line, on stderr, names the project oku is using.

A project list takes `[packages]`, `include`, `when`, version pins,
`[runtimes]` and `[lock]`. A relative ref starts at the project directory. The
keys are in [the oku.toml reference](../reference/oku-toml.md).

## Know which list a command uses

oku looks for `oku.toml` in the directory you run it in, then in each parent,
and uses the nearest one. With none, it uses your global list. Your config
directory holds the global list and never counts as a project.

`add`, `remove`, `list`, `sync`, `update`, `info`, `why`, `generations` and
`rollback` act on the project. Pass `--global`, or `-g`, to use the global list
from inside a project:

```
$ oku list
project /home/you/work/api
ripgrep  14.1.1  github:BurntSushi/ripgrep

$ oku list --global
ripgrep  15.2.0  github:BurntSushi/ripgrep
```

`gc`, `source`, `search`, `manifest` and `self` use no list. `oku gc` looks at
every profile, global and project, before it deletes anything.

Global and project profiles link into the same
[store](../how-oku-works.md#store). When both use a package, oku downloads and
keeps it once.

## Put the project's programs on PATH

The project needs the shell hook, the same line in your shell's startup file
that puts oku on `PATH`. [Set up your shell](../getting-started.md#2-set-up-your-shell)
has it for bash, zsh, fish and PowerShell. `oku doctor` says whether it is in
place.

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

Inside the project, in any subdirectory, the hook puts the project's `bin`
first on `PATH` and exports the `[env]` of its packages. A project program
runs instead of a global one with the same name. When you leave, the hook
removes both.

The hook reads local files only. It never uses the network, never installs,
and never runs anything from a manifest. One run takes a few milliseconds.

### Why you have to allow a project

A cloned repo could hold an `oku.toml` that puts its own `make` or `git` ahead
of yours. So the hook does nothing for a project until you run `oku allow`.

- The allow belongs to the `oku.toml` as it is now. After any edit, including
  a `git pull` that changes it, the hook stops and asks again.
- `oku deny` removes the allow.
- `oku allow` and `oku deny` take a directory, and default to the project you
  are in. Both fail when there is no `oku.toml` in the directory or above it.

### When the hook does nothing

The hook prints one line, once per directory, and changes nothing when:

- the project is not allowed, or its `oku.toml` changed since you allowed it.
  Run `oku allow`.
- the project's profile is behind its `oku.lock`, for example after a
  teammate's commit. Run `oku sync`.

### Use direnv instead

`oku env` prints what the hook would apply. With direnv:

```sh
# .envrc
eval "$(oku env --shell bash)"
```

## Give an editor the project's programs

An editor or a script does not run the shell hook. `oku exec` runs a command
with the same `PATH` and `[env]` the hook would give it:

```
$ oku exec gopls version
golang.org/x/tools/gopls v0.22.0
```

Point your editor's language server command at `oku exec gopls`. Flags after
the command go to the command, and oku exits with the command's exit code.

`oku exec` needs no `oku allow`, because you name the command yourself. The
project's profile must match its `oku.lock`, or `exec` fails and names
`oku sync`. `--global` leaves the project out.

## Try a package without adding it

`oku shell` puts packages in the store and starts `$SHELL` with their programs
first on `PATH`. It changes no `oku.toml`, no `oku.lock` and no profile:

```
$ oku shell github:BurntSushi/ripgrep github:sharkdp/fd
oku shell with github:BurntSushi/ripgrep, github:sharkdp/fd, leave it with exit
$ rg --version
ripgrep 15.2.0
$ exit
```

After `--`, oku runs that command in place of a shell:

```sh
oku shell github:BurntSushi/ripgrep -- rg TODO src/
```

No generation uses these packages, so the next `oku gc` deletes them. The
details are in [oku shell](../reference/commands.md#oku-shell).

## Share the project with teammates

A teammate clones the repo and syncs:

```
$ git clone git@github.com:you/api && cd api
$ oku sync
project /home/you/api
profile now holds 3 packages
```

`sync` installs what `oku.lock` pins. oku asks for approval on each machine
before it builds a package from source.

`add` and `update` pin each package for your own platform only. Name your
teammates' platforms in `[lock]` so a Linux teammate installs from the lock
your Mac wrote:

```toml
[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc"]
```

Without it, the first `sync` on a teammate's machine adds the entry for their
platform, and they commit the lock back. See
[One list for several OSes](new-machine.md) for how `[lock]` and `when` work.

## Run the project's tools in CI

Run `oku sync --locked` in CI. It fails when the lock does not pin a package
for the runner's platform, and it never changes the lock. For GitHub Actions,
see [Use oku in CI](ci.md).

## What a project cannot do

- A project installs programs only. Apps, fonts and services come from the
  global list, and oku prints `apps, fonts and services are only set up from
  the global list, not from a project`.
- A project list may not hold `[files]`, `[vars]`, `[secrets]` or OS settings.
  oku refuses such a list, because a cloned repo must not write into your home
  directory.
- `oku sync <list-ref>` sets up the global list. Inside a project oku refuses
  it and tells you to add the ref to the project's `include` array, or to pass
  `--global`.
- The profile name comes from a hash of the project's path. When you move the
  directory, run `oku sync` there and the project gets a new profile. `oku gc`
  removes what the old one used once its generations are gone.
- `oku self uninstall` removes every profile, project profiles too. It never
  touches a project's `oku.toml` or `oku.lock`.
