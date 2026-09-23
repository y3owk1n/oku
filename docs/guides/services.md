# Run a background service

You end up with a package's long-running program, such as a database or a sync
daemon, running now and at every login, controlled with `oku service`. oku
hands it to your OS's service manager: launchd on macOS, systemd on Linux and
Task Scheduler on Windows. There is no oku daemon.

A package that ships a service declares it in its manifest, see
[`[[service]]`](../reference/manifest.md#service). When you install the
package, oku writes the service's definition and leaves it stopped.

## Turn a service on

Add the package with `--service`:

```
$ oku add github:you/recipes#postgres --service
service postgres is running and starts at login
added postgres 17.2
```

`--service` writes `service = true` to the package's entry in `oku.toml`. You
can also write it yourself and run `oku sync`:

```toml
[packages]
postgres = { ref = "github:you/recipes#postgres", service = true }
```

The setting is in your list, so a new machine that runs `oku sync` gets the
service running too.

To turn it off, remove `service = true` and run `oku sync`.

## Control a service

```
$ oku service list
postgres  postgres  running, starts at login, pid 9449

$ oku service stop postgres
postgres: stopped
$ oku service start postgres
postgres: running, starts at login, pid 9501
```

| Command | Effect |
|---|---|
| `oku service list` | Every service of your installed packages, the package it belongs to, and its state. |
| `oku service start <name>` | Starts it. For a service that is not turned on, this lasts until you log out. |
| `oku service stop <name>` | Stops it. A service that is turned on starts again at the next login. |
| `oku service restart <name>` | Stops it and starts it again. |
| `oku service status <name>` | Whether it runs and whether it starts at login. It never fails for a stopped service. |
| `oku service logs <name>` | The last 50 lines it printed. |

The commands work the same on macOS, Linux and Windows. An unknown name fails
and lists the services you have.

## Find out why a service stopped

`start` and `restart` check the service again one second after they start
it. A program that exits at once, such as one that finds a stale socket, fails
the command, and oku says where to look:

```
$ oku service start atuin
oku: atuin started and then exited, look at ~/.local/share/oku/logs/atuin.log
```

Then read what it printed:

```sh
oku service logs atuin
```

On Linux the hint is a `journalctl` command, because the output goes to the
user journal. On Windows there is no hint, because Task Scheduler keeps no log
of its own.

## Update a running service

`oku update` of a package with a running service stops the old program and
starts the new one. On macOS oku waits up to 30 seconds for the old program to
exit, because launchd does not load a service while its old program still
runs.

## Know where the definitions go

| | macOS | Linux |
|---|---|---|
| Turned on | `~/Library/LaunchAgents/dev.oku.<name>.plist`, loaded into your login session | `~/.config/systemd/user/oku-<name>.service`, enabled and started |
| Installed, not turned on | `~/.local/share/oku/services/dev.oku.<name>.plist`, which launchd does not read | the same unit file, disabled |
| Output | `~/.local/share/oku/logs/<name>.log` | the user journal, `journalctl --user -u oku-<name>` |

When `XDG_CONFIG_HOME` or `XDG_DATA_HOME` is set, oku uses it in place of
`~/.config` or `~/.local/share`. The full list of paths is in
[Paths](../reference/paths.md).

On Windows a service is a scheduled task named `oku-<name>`, see
[Windows](windows.md#run-a-service).

oku records each of these files in its [ledger](../how-oku-works.md#ledger).
`oku remove` stops the service and deletes its file, and `oku self uninstall`
does that for every service.

A service manager starts a program with a bare `PATH`. On macOS and Linux oku
puts your global profile's `bin` in front of it, so a service can call your
other installed programs by name.

## Roll back

`oku rollback` restores which services were turned on along with the packages.
See [Undo a change](undo-and-clean-up.md).

## What services cannot do

- Services come from your global list only. A package in a
  [project](projects.md) installs its programs, and oku says that its services
  were skipped.
- On Linux a user service runs only while you are logged in, unless lingering
  is on for your account (`loginctl enable-linger`). That is a systemd setting,
  and oku does not change it.
- A Linux machine that systemd does not run, such as a container or a distro
  with another init, has no service manager oku knows. oku installs the
  packages, writes no unit, and prints
  `services are skipped, because systemd does not run this machine`. oku
  checks for `/run/systemd/system`, which systemd creates when it is the init.
- A service runs as you, for your login. To run one as root from boot, for the
  whole machine, see [Install for every user](system-wide.md).
