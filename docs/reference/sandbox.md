# The build sandbox

oku runs a package's commands in a sandbox where the host has one. This page
covers what each platform allows and blocks, what you see when there is no
sandbox, and how to make oku refuse to build without one.

## What runs in the sandbox

A package's code runs in the sandbox, and nothing else does:

- a `run` step of a manifest's `[build]`
- a `vendor` step, the `npm rebuild` that `scripts` asks for, and oku's own
  steps for `npm:`, `pypi:`, `go:` and `cargo:` refs
- a `completions = { generate = ... }` command, which runs the downloaded
  program

oku does `fetch`, `extract`, `patch`, `install` and `copy` steps itself, and
unpacks downloads itself. It never runs a script that a package ships, such as
the maintainer scripts of a `.deb` or the install scripts of a `.pkg`.

These steps run outside the sandbox, after `run` steps that could leave
symlinks in the source directory and in `{{prefix}}`. oku follows such a link
only while it stays inside the directory it is in. A step that reaches a link
leading outside fails, so a `run` step cannot use one to make oku read your
files or write elsewhere. When a command ends, oku also stops anything it left
running, so nothing changes a link while oku works.

Every command above needs your [approval](security.md#approve-build-commands)
first, once per manifest hash.

## On every platform

On every platform, a build command:

- does not see your environment. It gets its own `PATH`, an empty `HOME` and
  temporary directory, and the variables the
  [build environment](manifest.md#the-build-environment) lists. `GITHUB_TOKEN`
  and other secrets in your shell never reach it.

On Linux and macOS it also stops with all of its child processes when you
press Ctrl-C, and oku then fails the command and leaves the machine as it was.
It runs in a session of its own, without the terminal
oku runs in. It cannot read what you type, write to your screen, or push a
command into your shell to run after oku exits. This holds on a Linux host
without the sandbox too.

## macOS

oku runs each command under `sandbox-exec` with a profile that allows
everything except:

| Blocked | Details |
|---|---|
| Network | All of it, unless the step sets `network = true`. |
| Your sockets | A step with `network = true` reaches DNS and the sockets in its own directories, and no other unix socket. It cannot connect to your `ssh-agent`, to Docker or to another agent. |
| Your processes | Sending a signal to a process outside the build. |
| Your home directory | Reading any file or listing any directory in it. Names and sizes stay readable, because paths to the store pass through it. The store and the directories of the `needs` tools stay readable. |
| Your temporary and cache directories | Reading anything under `/private/var/folders`, where macOS keeps them. |
| Your preferences and keychain | The `cfprefsd` and `securityd` services, which would otherwise read `~/Library/Preferences` and the keychain for the build. |
| Writing | Anywhere except the build's source directory, its `HOME` and temporary directory, `{{prefix}}`, `/dev/null` and `/dev/fd`. |
| Starting programs outside the sandbox | Apple Events, LaunchServices (`open`) and `/bin/launchctl`. |
| Terminals | Reading `/dev/tty` and `/dev/ttys*`. |

The tools in `/usr/bin` ask `xcrun` where the real tool is, and `xcrun` caches
the answer. oku points that cache at the build's own temporary directory, so
`cc`, `ar` and the others run without an "Operation not permitted" warning.

A program that talks to launchd over XPC itself can still start a program
outside the sandbox. The sandbox makes a malicious build harder, and does not
stop one.

## Linux

oku starts each command in new user, mount, network and pid namespaces, and
sets them up before the command runs:

| Blocked | Details |
|---|---|
| Network | A network namespace with no interfaces, unless the step sets `network = true`. |
| Your home directory | oku covers it with an empty tmpfs, then mounts the store, the `needs` tools and the build's own directories back in. |
| Shared temporary directories | oku covers `/tmp` and `/var/tmp` with an empty tmpfs, then mounts the build's own directories back in. The sockets of `ssh-agent` and the X server are not in them. |
| Writing | Every mount is read-only except the build's source directory, its `HOME` and temporary directory, and `{{prefix}}`. |
| Your session | oku hides `/run/user`, which holds your D-Bus and systemd sockets. On Linux 6.12 and later a step with `network = true` cannot reach an abstract socket outside the build either. |
| Your processes | The build gets a `/proc` of its pid namespace. It sees only its own processes and cannot signal yours. |
| Shared memory | `/dev/shm` is the build's own. |
| Terminals | `/dev/pts` is a new instance, so your terminals are not in it. The build can still open terminals of its own. |

The command runs as you. Files it creates in `{{prefix}}` belong to you.

A host has to let an unprivileged user create these namespaces. These do not:

- Ubuntu 24.04 and later, by default. Their AppArmor policy lets a program
  create a user namespace and then denies it every mount inside it. To allow
  the sandbox, run
  `sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0`, and put the
  same setting in a file under `/etc/sysctl.d/` to keep it after a reboot.
- A default Docker container, whose seccomp profile blocks the namespaces.
- A kernel with `kernel.unprivileged_userns_clone=0`.

oku finds out by setting up a sandbox once with a command that does nothing.

In a container that masks parts of its `/proc`, such as a Docker container
that is not privileged, the kernel refuses a new `/proc`. The build then keeps
the container's `/proc`. It sees the container's processes there and still
cannot signal them.

## Windows

Windows has no sandbox that oku can use. A build command gets the scrubbed
environment and nothing more, so it can reach the network and read your files.
See [Windows](../guides/windows.md#build-from-source).

## Without a sandbox

On Windows and on a Linux host that forbids the namespaces, oku still builds,
and tells you three times:

- The approval prompt says so before you answer:

  ```
  this host cannot sandbox commands, because this host does not let an unprivileged user set up namespaces (...), so they can use the network and read your files
  ```

- After the build, oku warns:

  ```
  tree was built without the sandbox, because this host does not let an unprivileged user set up namespaces (...)
  its build commands could use the network and read your files
  ```

- `oku doctor` reports it as a `note`.

A build you approved before, or approve with `--yes`, gets only the warning
after the build.

### Refuse to build without a sandbox

Set `require_sandbox` in `config.toml`:

```toml
require_sandbox = true
```

oku then refuses, before anything changes, to run any package's commands on a
host that cannot sandbox them:

```
tree 2.3.2 runs commands, and this host cannot sandbox them, because ...
require_sandbox in config.toml refuses that, delete the line to run them anyway
```

A package that installs from a prebuilt download runs no commands and still
installs, unless it generates its completions with a command.

## When a build fails in the sandbox

A command that tries something the sandbox blocks fails with an error such as
`Operation not permitted`, `Permission denied` or `Could not resolve host`. oku
prints the last lines of its output.

- A build that downloads something needs a checksummed `source`, a `fetch`
  step, or a [`vendor` step](manifest.md#vendoring). `network = true` gives one
  `run` step the network. oku shows it as `(wants network)` in the approval
  prompt and marks the package impure.
- A build that reads a file under `/tmp` on Linux, or under your temporary
  directory on macOS, cannot see it. Put the file in the manifest's source.
- A build that reads a tool's files in your home directory needs that tool in
  `needs`, or a toolchain package as a dep. See
  [the build environment](manifest.md#the-build-environment).
