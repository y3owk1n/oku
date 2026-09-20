# Windows

oku runs on Windows without administrator rights and without developer mode.

## What works

Tested on a GitHub Actions `windows-latest` machine with real releases:

- `oku add`, `remove`, `list`, `sync`, `update`, `generations`, `rollback` and
  `gc` for packages that ship a prebuilt `.zip`.
- `oku add github:owner/repo` for a repo without a manifest. oku picks the
  `windows` asset for your CPU.
- Programs start through the profile from any directory, with their arguments,
  stdin, stdout and exit code unchanged.

## What does not work yet

- The shell hook for PowerShell, so [projects](projects.md) do not activate.
- Building from source. `run` steps need a `shell`, and there is no sandbox on
  Windows.
- Apps, fonts and services.
- `.msi` downloads.
- `oku self uninstall` cannot delete the running `oku.exe`.

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
DLLs of those deps from any working directory. No package with deps has been
tried on Windows yet, because deps come from source builds.

Shims are hard links to one copy of oku in `<data>\oku\shims\`, so they take no
extra space. They do not link to `oku.exe` itself, because Windows refuses to
delete any link to a running program, and `oku gc` deletes shims while oku
runs. After you replace `oku.exe`, new shims use a new copy, and old generations
keep the old one until `oku gc --keep N` removes them. The leftover copy in
`shims\` stays.
