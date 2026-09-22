# Services

A package can ship a long-running program, such as a database or a sync daemon.
oku runs it through your OS's service manager, which is launchd on macOS,
systemd on Linux and Task Scheduler on Windows. There is no oku daemon.

## Turn a service on

A package's services are installed with it and stay stopped. To run them now and
at every login, enable them for that package:

```
$ oku add github:you/recipes#postgres --service
service postgres is running and starts at login
added postgres 17.2
```

`--service` writes `service = true` to the package's entry in `oku.toml`. You can
also write it yourself and run `oku sync`:

```toml
[packages]
postgres = { ref = "github:you/recipes#postgres", service = true }
```

Because it is in the list, a new machine that runs `oku sync` gets the service
running too. To turn it off, remove `service = true` and sync. `oku rollback`
restores which services were enabled along with the packages.

## Control a service

```
$ oku service list
postgres  postgres  running, starts at login, pid 9449

$ oku service stop postgres
postgres: stopped
$ oku service start postgres
postgres: running, starts at login, pid 9501
$ oku service logs postgres
```

| Command | Effect |
|---|---|
| `oku service list` | Every service of your installed packages, the package it belongs to, and its state. |
| `oku service start <name>` | Starts it. For a service that is not enabled, this lasts until you log out. |
| `oku service stop <name>` | Stops it. An enabled service starts again at the next login. |
| `oku service restart <name>` | Stops it and starts it again. |
| `oku service status <name>` | Whether it is running and whether it starts at login. |
| `oku service logs <name>` | The last 50 lines it printed. |

The commands work the same on macOS, Linux and Windows. An unknown name fails and lists
the services you have.

`start` and `restart` look at the service again one second after starting it,
because a program that exits at once, such as one that finds a stale socket,
still has a pid the moment it starts. When it has exited by then, the
command fails and says where to look:

```
$ oku service start atuin
oku: atuin started and then exited, look at ~/.local/share/oku/logs/atuin.log
```

On Linux the hint is a `journalctl` command. On Windows there is no log to point
at, because Task Scheduler keeps none. `status` reports what the service
manager says and never fails for a stopped service.

`oku update` of a package with a running service stops the old program and
starts the new one. On macOS oku waits up to 30 seconds for the old program to
exit, because launchd does not load a service while its old program still runs.

## What oku writes

| | macOS | Linux |
|---|---|---|
| Enabled | `~/Library/LaunchAgents/dev.oku.<name>.plist`, loaded into your login session | `<config home>/systemd/user/oku-<name>.service`, enabled and started |
| Installed, not enabled | `<data>/oku/services/dev.oku.<name>.plist`, which launchd does not read | the same unit file, disabled |
| Output | `<data>/oku/logs/<name>.log` | the user journal, `journalctl --user -u oku-<name>` |

`<config home>` is `$XDG_CONFIG_HOME`, or `~/.config`. Windows is described in
[Windows](windows.md#services).

Each of these files is in oku's [ledger](files.md#outside-okus-directories).
`oku remove` stops the service and deletes its file, and `oku self uninstall`
does that for every service.

On Linux a user service only runs while you are logged in, unless lingering is
on for your account (`loginctl enable-linger`). That is a systemd setting, and
oku does not change it.

A Linux machine that systemd does not run, such as a container or a distro with
another init, has no service manager that oku knows. oku installs the packages
there, writes no unit, and prints one notice:

```
services are skipped, because systemd does not run this machine
```

oku checks for `/run/systemd/system`, which systemd creates when it is the init.

Services come from your global list only. A package in a
[project](projects.md) installs its programs, and oku says that its services
were skipped. For services that run as root, for the whole machine, see
[System scope](system-scope.md).

## For package authors

See [`[[service]]`](manifest.md#service) in the manifest reference.
