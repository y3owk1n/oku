# System scope

By default oku installs a package's apps, fonts and services for your user, and
needs no administrator rights. System scope puts them where every user of the
machine gets them, and runs services as root from boot. Writing there needs
`sudo`.

The package's programs are not affected. They stay in your profile's `bin`.

## Put a package in system scope

```
$ oku add github:you/recipes#postgres --system --service
this changes, with administrator rights:
  write  service  /etc/systemd/system/oku-postgres.service
continue? [y/N] y
service postgres is running and starts at boot
added postgres 16.3
```

oku lists every file it will write or remove, asks, and then runs itself through
`sudo` once per file. `sudo` asks for your password the first time. When you run
oku as root, it does not call `sudo`.

`--system` writes `system = true` to `oku.toml`:

```toml
postgres = { ref = "github:you/recipes#postgres", service = true, system = true }
```

You can also add `system = true` by hand and run `oku sync --system`.

## oku never elevates without the flag

Only `oku add --system`, `oku sync --system`, `oku update --system`,
`oku service <action> <name> --system` and `oku self uninstall` use `sudo`.
Every other command leaves system scope as it is and tells you:

```
$ oku remove postgres
left unchanged, because system scope needs administrator rights:
  remove service  /etc/systemd/system/oku-postgres.service
run "oku sync --system" to apply them
removed postgres
```

The package is out of your profile, and the service keeps running until you run
`oku sync --system`. The same happens after `oku rollback`, and after a plain
`oku sync` on a new machine whose list has `system = true` entries. Answering no
to the question has the same result.

## Where the files go

| | macOS | Linux |
|---|---|---|
| Apps | `/Applications/` | `/usr/local/share/applications/oku-<name>.desktop` |
| Fonts | `/Library/Fonts/` | `/usr/local/share/fonts/oku/` |
| Enabled services | `/Library/LaunchDaemons/dev.oku.<name>.plist` | `/etc/systemd/system/oku-<name>.service` |
| Installed, not enabled services | `/Library/Application Support/oku/services/` | the same unit file, disabled |
| Service output | `/Library/Logs/oku/<name>.log` | the system journal |

oku records each file in the ledger with `system = true`, see
[Files and directories](files.md#outside-okus-directories).

A system service runs a program from the store in your user's data directory.
oku does not put the package's programs on the `PATH` of other users.

## Control a system service

`oku service list` and `oku service status <name>` work without the flag and
mark the service with `system scope`. `start`, `stop` and `restart` need
`--system`:

```
$ oku service stop postgres
oku: postgres runs in system scope, so stop needs administrator rights: run "oku service stop postgres --system"
$ oku service stop postgres --system
postgres: stopped, starts at boot, system scope
```

On Linux the output of a system service is in the system journal, which a normal
user may not read. `oku service logs <name> --system` reads it through `sudo`.

## Uninstalling

`oku self uninstall` marks everything that needs administrator rights and asks
about it in a second question:

```
  service             /etc/systemd/system/oku-postgres.service (needs administrator rights)
continue? [y/N] y
remove what needs administrator rights? [y/N] n
oku is uninstalled
left in place, because removing them needs administrator rights:
  service  /etc/systemd/system/oku-postgres.service
remove them with:
  sudo systemctl disable --now oku-postgres.service
  sudo rm -rf "/etc/systemd/system/oku-postgres.service"
```

Answering no removes everything in user scope and prints the commands for the
rest. A service you leave behind fails at its next start, because its program was
in the store. `--yes` alone counts as no, and `--yes --system` counts as yes.

## What was tested

The Linux paths are tested against real systemd as a normal user with `sudo`.
On macOS only the paths that do not elevate are tested on a real machine. The
LaunchDaemon code is the LaunchAgent code with the `system` domain.
