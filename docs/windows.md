# Windows

oku runs on Windows without administrator rights and without developer mode.

## What works

Tested on a GitHub Actions `windows-latest` machine with real releases:

- `oku add`, `remove`, `list`, `sync`, `update`, `generations`, `rollback` and
  `gc` for packages that ship a prebuilt `.zip` or `.msi`.
- `oku add github:owner/repo` for a repo without a manifest. oku picks the
  `windows` asset for your CPU.
- Programs start through the profile from any directory, with their arguments,
  stdin, stdout and exit code unchanged.
- Apps and fonts for your user, see [Apps and fonts](#apps-and-fonts).
- Services for your user, see [Services](#services).
- [System scope](#system-scope): apps, fonts and services for the whole machine,
  and the shared store root.
- Building from source, see [Builds](#builds).
- `oku self uninstall`, see [Uninstalling](#uninstalling).
- [Projects](projects.md) with the PowerShell hook: `oku allow`, and a project's
  programs on `PATH` while you are inside it.

## What is not verified

Everything the spec promises for Windows is built. These parts are built and
have not run on a real machine of the kind they are for:

- The Windows consent prompt for [system scope](#system-scope). The test machine
  is already an administrator and has no desktop.
- Registering a service task as a standard user. The test machine is an
  administrator.
- A program that loads a DLL from a dep, see [Shims](#shims).

Windows has no sandbox that oku can use, so a build from source can reach the
network and read your files, see [Builds](#builds).

## Put oku's programs on PATH

```powershell
$bin = "$env:LOCALAPPDATA\oku\profiles\global\current\bin"
[Environment]::SetEnvironmentVariable('Path', "$bin;" + [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')
```

Open a new terminal afterwards. oku never edits `PATH` itself.

## How a profile differs

Windows lets a normal user create symlinks only in developer mode, so oku uses
what every user may create:

| Unix | Windows |
|---|---|
| `current` is a symlink to `gen-<n>` | `current` is a directory junction to `gen-<n>` |
| `bin/rg` is a symlink into the store | `bin\rg.exe` is a shim, beside `bin\rg.shim` |
| other files are symlinks into the store | other files are hard links, or copies across volumes |

oku replaces a junction by deleting it and creating a new one, so for a moment
`current` does not exist. On unix the switch is one rename.

### Shims

A shim is a copy of `oku.exe` under the program's name. When oku starts and
finds `<name>.shim` beside itself, it runs the program that file names, passes
the arguments and stdio through, and exits with the program's exit code:

```
path = C:\Users\you\AppData\Local\oku\store\ripgrep-15.2.0-f635eb888c8bcf4b\bin\rg.exe
dir = C:\Users\you\AppData\Local\oku\store\pcre2-10.44-0a1b2c3d4e5f6a7b\bin
```

Each `dir` line is the `bin` directory of one of the package's deps. The shim
puts them at the front of `PATH` for the program, which is how Windows finds the
DLLs of those deps from any working directory. The live test checks that the
shim of a built package lists its dep. It has no package that loads a DLL from a
dep yet.

Shims are hard links to one copy of oku in `<data>\oku\shims\`, so they take no
extra space. They do not link to `oku.exe` itself, because Windows refuses to
delete any link to a running program, and `oku gc` deletes shims while oku
runs. After you replace `oku.exe`, new shims use a new copy, and old generations
keep the old one until `oku gc --keep N` removes them. The leftover copy in
`shims\` stays.

## Apps and fonts

| | Where |
|---|---|
| `[[app]]` | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk`, a shortcut to the program in the store |
| `font = [...]` | `%LOCALAPPDATA%\Microsoft\Windows\Fonts\<file>`, and a value `oku <file>` under `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |

Windows shows a per-user font to programs only when the registry names it, so
oku writes that value and deletes it again with the font. oku creates the
shortcut through the Windows shell, by way of `powershell`. `oku remove`,
`oku rollback` and `oku self uninstall` take all of it away, as on the other
systems.

## Services

A [service](services.md) is a scheduled task named `oku-<name>`. It runs as you,
with your normal rights, and only while you are logged on.

| | |
|---|---|
| Enabled | The task has a trigger for your logon, and oku starts it right away. |
| Installed, not enabled | The task has no trigger. `oku service start <name>` runs it. |
| Definition | `<data>\oku\services\<name>.json` |
| Output | `<data>\oku\logs\<name>.log` |

A scheduled task can neither set environment variables nor send output to a
file. So the task starts `oku service-run`, a hidden command that reads the
definition, opens the log, sets the service's `env` and runs its program without
a window. It puts the program in a Windows job object, so that ending the task
ends the program too. `oku service stop`, `oku remove` and `oku self uninstall`
rely on that.

Task Scheduler restarts a task only after it fails. So `restart = "always"`
behaves like `restart = "on-failure"` here: one minute after a failure the task
starts again. A program that exits with code 0 stays stopped.

## System scope

[System scope](system-scope.md) works as on the other systems: `system = true`
in the list, `--system` on the command, and oku lists every file and asks before
it writes.

| | Where |
|---|---|
| Apps | `%ProgramData%\Microsoft\Windows\Start Menu\Programs\oku-<name>.lnk` |
| Fonts | `%SystemRoot%\Fonts\<file>`, and a value `oku <file>` under `HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts` |
| Services | a scheduled task `oku-<name>` that runs as the `SYSTEM` account, with a trigger at boot when enabled |
| Service definitions and output | `%ProgramData%\oku\services\` and `%ProgramData%\oku\logs\` |
| Shared store root | `%ProgramData%\oku`, from `oku setup --system`, with full control for your user |

Windows has no `sudo`. In a terminal that runs as administrator oku does the
privileged step directly. In a normal terminal it starts that step through the
Windows consent prompt, once per file. The consent prompt path is not tested,
because the test machine has no desktop to show it on.

A system service is a task, as a user service is, and not a Windows service. A
program has to implement the service control protocol to be a Windows service,
and a package's program does not. The task runs from boot with no user logged
on, which a service needs.

## Builds

A `run` step that can run on Windows must name its shell, `pwsh` or `cmd`.
`oku manifest lint` reports a step that does not.

Windows has no sandbox that oku can use, so a build can reach the network and
read your files. oku prints that warning after every build. The build still gets
a scrubbed environment:

| Variable | Value |
|---|---|
| `PATH` | the `bin` of each dep, the directories of the `needs` tools, `System32`, the Windows directory, Windows PowerShell, and the directory of `pwsh` when it is installed |
| `USERPROFILE`, `HOME`, `APPDATA`, `LOCALAPPDATA` | a scratch home that oku deletes after the build |
| `TEMP`, `TMP` | a scratch directory that oku deletes after the build |
| `SystemRoot`, `SystemDrive`, `ComSpec`, `PATHEXT` | as on the host, because Windows programs do not start without them |

The `OKU_*` variables and the dep variables are the same as on unix, see
[The build environment](manifest.md#the-build-environment).

## Uninstalling

Windows refuses to delete a program that is running, but it lets one be renamed.
`oku self uninstall` renames `oku.exe` to `oku.exe.uninstalled` and starts a
hidden `cmd` that deletes that file about four seconds later. `oku.exe` is gone
from its path when the command returns, and nothing is left after those seconds.
