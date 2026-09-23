# Install for every user of the machine

You end up with a package's apps, fonts and services placed for every account
on the machine, with its services running as root from boot. This is
[system scope](../how-oku-works.md#system-scope). A second, separate step on
this page moves the store to a shared root, so machines can share packages
built from source.

## Decide whether you need system scope

By default oku places apps, fonts and services for your user and needs no
administrator rights. That covers a personal machine. Use system scope when:

- a service has to run from boot, before anyone logs in, or as root. A
  user service runs only while you are logged in.
- other accounts on the machine need the app or the font.

System scope changes where apps, fonts and services go. A package's programs
stay in your profile's `bin`, and oku does not put them on the `PATH` of other
users.

## Put a package in system scope

Add the package with `--system`:

```
$ oku add github:you/recipes#postgres --system --service
this changes, with administrator rights:
  write  service  /etc/systemd/system/oku-postgres.service
continue? [y/N] y
service postgres is running and starts at boot
added postgres 16.3
```

oku lists every file it will write or remove, asks, and then runs itself
through `sudo` once per file. `sudo` asks for your password the first time.
When you run oku as root, it does not call `sudo`. On Windows, see
[Windows](windows.md#install-for-every-user).

`--system` writes `system = true` to the package's entry in `oku.toml`:

```toml
[packages]
postgres = { ref = "github:you/recipes#postgres", service = true, system = true }
```

You can also add `system = true` by hand and run `oku sync --system`.

## Know when oku asks for sudo

Only these commands use `sudo`:

- `oku add --system`
- `oku sync --system`
- `oku update --system`
- `oku service <action> <name> --system`
- `oku self uninstall`
- `oku setup --system`

Every other command leaves system scope as it is and tells you what it left:

```
$ oku remove postgres
left unchanged, because system scope needs administrator rights:
  remove service  /etc/systemd/system/oku-postgres.service
run "oku sync --system" to apply them
removed postgres
```

The package is out of your profile, and its service keeps running until you
run `oku sync --system`. The same happens after `oku rollback`, after a plain
`oku sync` on a new machine whose list has `system = true` entries, and when
you answer no to the question.

## Know where the files go

| | macOS | Linux |
|---|---|---|
| Apps | `/Applications/` | `/usr/local/share/applications/oku-<name>.desktop` |
| Fonts | `/Library/Fonts/` | `/usr/local/share/fonts/oku/` |
| Services that are turned on | `/Library/LaunchDaemons/dev.oku.<name>.plist` | `/etc/systemd/system/oku-<name>.service` |
| Services that are installed, not turned on | `/Library/Application Support/oku/services/` | the same unit file, disabled |
| Service output | `/Library/Logs/oku/<name>.log` | the system journal |

oku records each file in its [ledger](../how-oku-works.md#ledger), marked as
system scope. A system service runs a program from the store in your user's
data directory.

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

On Linux the output of a system service is in the system journal, which a
normal user may not be allowed to read. `oku service logs <name> --system`
reads it through `sudo`.

A system service starts with the bare `PATH` of the service manager. It does
not get your profile's `bin`.

## Share built packages through a common store root

A build from source can write its [store](../how-oku-works.md#store) path into
the files it installs. Such a package works only at that path, so a
[build cache](build-caches.md) offers it only to machines with the same store
root. `oku setup --system` gives every machine the same root, `/opt/oku`.
Prebuilt downloads work from any store, so skip this unless you share builds.

```
$ oku setup --system
this creates, with administrator rights:
  /opt/oku  owned by you
continue? [y/N] y
the store root is now /opt/oku
run "oku sync" to install your packages there, then "oku gc" to delete the old copies
```

`/opt` belongs to root, so oku runs one command through `sudo` to create the
directory. The directory then belongs to your user, and nothing after this
needs `sudo`. `--yes`, or `-y`, skips the question. Without `--system`,
`oku setup` fails and does nothing.

oku records the root as `store_root = "/opt/oku"` in
`~/.config/oku/config.toml`. Then:

1. Run `oku sync`. It installs your packages under `/opt/oku/store`.
2. Run `oku gc`. It deletes the old copies once no generation uses them.

Older generations still point at the old store, so rollback keeps working, and
`oku gc` covers both stores. Profiles, the ledger and build approvals stay in
your data directory.

To go back, delete the `store_root` line from `config.toml` and run
`oku sync`.

On Windows the shared root is `%ProgramData%\oku`, see
[Windows](windows.md#install-for-every-user).

## Remove system scope

To take one package out of system scope, remove `system = true` or the package
from `oku.toml` and run `oku sync --system`.

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
rest. A service you leave behind fails at its next start, because its program
was in the store. `--yes` alone counts as no, and `--yes --system` counts as
yes.

After `oku setup --system` the list includes the shared store root:

```
  shared store root   /opt/oku (needs administrator rights)
continue? [y/N] y
remove what needs administrator rights? [y/N] n
oku is uninstalled
left in place, empty:
  /opt/oku
remove it with: sudo rmdir /opt/oku
```

Answering no still deletes everything inside `/opt/oku`, because your user owns
it. Only the empty directory stays.
