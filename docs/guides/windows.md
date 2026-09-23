# Use oku on Windows

You end up with oku working on Windows the way it does on macOS and Linux,
without administrator rights and without developer mode. This page covers only
what differs. Everything else in these docs applies as written.

## Install oku

In PowerShell:

```powershell
irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
```

The script puts `oku.exe` in `%LOCALAPPDATA%\oku\bin` after it checks the
sha256 against the release's `checksums.txt`. It edits no file of yours.

## Set up PowerShell

The script ends by printing one line for the file that `$PROFILE` names, and a
command that appends it. The line is:

```powershell
if (Test-Path "$HOME\AppData\Local\oku\bin\oku.exe") { Invoke-Expression ((& "$HOME\AppData\Local\oku\bin\oku.exe" hook pwsh) -join [Environment]::NewLine) }
```

It puts `oku.exe` and the programs oku installs on `PATH`, loads the
completions of `oku`, and applies a [project's](projects.md) programs while you
are inside it. It wraps your `prompt` function and keeps `$LASTEXITCODE`. oku
never edits your profile itself. `oku doctor` says whether the line is in
place.

PowerShell gets the completions of `oku` only, not those of the programs you
install.

## Know why a profile looks different

A normal Windows user may not create symlinks, so oku builds a
[profile](../how-oku-works.md#profile) from what every user may create:

- `current` is a directory junction, not a symlink.
- Each program in `bin` is a [shim](../how-oku-works.md#shim), such as
  `bin\rg.exe` beside `bin\rg.shim`. The shim is a copy of `oku.exe` that
  starts the real program in the store, passes the arguments, stdin and stdout
  through, and exits with the program's exit code.
- Other files are hard links into the store, or copies across volumes.

A shim also puts the `bin` directories of the package's runtime deps, and the
downloads of the package and those deps, at the front of `PATH` for the
program. That is how a program finds the DLLs it ships and those of its deps,
from any directory.

Shims are hard links to one copy of oku in `%LOCALAPPDATA%\oku\shims\`, so they
take no extra space. After you replace `oku.exe`, new shims use a new copy, and
old generations keep the old one until `oku gc --keep N` removes them. The old
copy in `shims\` stays.

Switching generations takes two renames on Windows, one on macOS and Linux.
When oku is killed between them, the next command puts the previous generation
back.

## Place apps and fonts

| | Where |
|---|---|
| `[[app]]` | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk`, a shortcut to the program in the store |
| `font = [...]` | `%LOCALAPPDATA%\Microsoft\Windows\Fonts\<file>`, and a value `oku <file>` under `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |

Windows shows a per-user font to programs only when the registry names it, so
oku writes that value and deletes it again with the font. oku creates the
shortcut through `powershell`. `oku remove`, `oku rollback` and
`oku self uninstall` take all of it away, as on the other systems.

A macOS `app = [...]` bundle is not used on Windows.

## Place files in your home directory

[`[files]`](dotfiles.md) uses what Windows allows:

| Entry | On Windows |
|---|---|
| `link` to a directory | A junction. An edit of the source shows at once. |
| `link` to a file | A copy. Run `oku sync` after you edit the source. |
| `text`, `render` | A copy of the content in the generation. `oku rollback` writes the bytes of that generation. |
| `secret` | A copy, with an access control list that names you alone and inherits nothing. |

oku records the sha256 of each copy. When you edit a copy by hand, the next
`sync`, `add`, `remove`, `update` or `rollback` stops before it changes
anything and names the file. Move the change into the source or into
`oku.toml`, delete the copy, and run the command again. oku writes a deleted
copy again.

`mode` does not apply on Windows.

| Location | On Windows |
|---|---|
| `{{config}}` | `%APPDATA%`, unless `XDG_CONFIG_HOME` is set |
| `{{data}}` | `%LOCALAPPDATA%`, unless `XDG_DATA_HOME` is set |
| `{{appdata}}`, `{{localappdata}}` | Windows only. An entry that uses one needs `when = { os = "windows" }`. |

Your age key for [secrets](secrets.md) goes in `%APPDATA%\sops\age\keys.txt`.

## Change registry settings

[`[registry]`](os-settings.md) sets values under `HKCU` through `reg.exe`. oku
puts back the value a name had before, with its type, when the name leaves the
list, on `oku rollback` and on `oku self uninstall`. Some programs read a value
only when they start, so restart them to see a change.

## Run a service

A [service](services.md) is a scheduled task named `oku-<name>`. It runs as
you, with your normal rights, and only while you are logged on.

| | |
|---|---|
| Turned on | The task has a trigger for your logon, and oku starts it right away. |
| Installed, not turned on | The task has no trigger. `oku service start <name>` runs it. |
| Definition | `%LOCALAPPDATA%\oku\services\<name>.json` |
| Output | `%LOCALAPPDATA%\oku\logs\<name>.log` |

The program runs without a window. Stopping the task ends the program too.

Task Scheduler restarts a task only after it fails. So `restart = "always"`
behaves like `restart = "on-failure"`. One minute after a failure the task
starts again. A program that exits with code 0 stays stopped.

`oku service start` points at no log when a service exits at once, because
Task Scheduler keeps none.

## Install for every user

[System scope](system-wide.md) works as on the other systems: `system = true`
in the list, `--system` on the command, and oku lists every file and asks
before it writes.

| | Where |
|---|---|
| Apps | `%ProgramData%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk` |
| Fonts | `%SystemRoot%\Fonts\<file>`, and a value `oku <file>` under `HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |
| Services | A scheduled task `oku-<name>` that runs as the `SYSTEM` account, with a trigger at boot when turned on |
| Service definitions and output | `%ProgramData%\oku\services\` and `%ProgramData%\oku\logs\` |
| Shared store root | `%ProgramData%\oku`, from `oku setup --system`, with full control for your user |

Windows has no `sudo`. In a terminal that runs as administrator, oku does the
privileged step directly. In a normal terminal it asks through the Windows
consent prompt, once per file.

A system service is a scheduled task, not a Windows service. A program has to
implement the Windows service control protocol to be a Windows service, and a
package's program does not. The task runs from boot with no user logged on.

## Build from source

Windows has no sandbox that oku can use, so a build from source can reach the
network and read your files. oku prints that warning after every build. The
build still gets a scrubbed environment with a scratch home and temp
directory, see [the build environment](../reference/manifest.md#the-build-environment).

A manifest's `run` step that can run on Windows must name its shell, `pwsh` or
`cmd`. `pwsh` is PowerShell 7 when it is installed, else the Windows PowerShell
5.1 that ships with Windows. oku's own steps for `npm:`, `pypi:`, `go:` and
`cargo:` refs need no PowerShell 7.

## Uninstall

Windows refuses to delete a running program. So `oku self uninstall` renames
`oku.exe` to `oku.exe.uninstalled` and starts a hidden `cmd` that deletes it
about four seconds later. Nothing is left after those seconds.

## What is not verified

oku has code for these parts, but it has not run on a real machine of the kind
it is for:

- The consent prompt for system scope, in a terminal that does not run as
  administrator.
- Registering a service task as a standard user.
- A program that loads a DLL from another package. One that loads a DLL from
  its own download works.
