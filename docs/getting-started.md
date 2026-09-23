# Getting started

In about ten minutes you will install oku, use it to install ripgrep, move
ripgrep to a newer version, and undo that change. Along the way you will see
the two files oku keeps for you.

You need macOS, Linux or Windows, and a terminal. You do not need root.

## 1. Install oku

On macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
```

The script downloads oku for your OS and CPU, checks its sha256, and puts it
in `~/.local/bin`, or `%LOCALAPPDATA%\oku\bin` on Windows. It changes none of
your files. At the end it prints one line for your shell, which you add in the
next step.

## 2. Set up your shell

Add the line for your shell to its startup file. This puts `oku` and every
program oku installs on your `PATH`.

For zsh, the default shell on macOS:

```sh
echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc
exec zsh
```

For other shells, add this line to the file in the second column, then open a
new terminal:

| Shell | File | Line |
|---|---|---|
| bash | `~/.bashrc` | `[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook bash)"` |
| fish | `~/.config/fish/config.fish` | `test -x "$HOME/.local/bin/oku"; and "$HOME/.local/bin/oku" hook fish \| source` |
| PowerShell | the file `$PROFILE` names | `if (Test-Path "$HOME\AppData\Local\oku\bin\oku.exe") { Invoke-Expression ((& "$HOME\AppData\Local\oku\bin\oku.exe" hook pwsh) -join [Environment]::NewLine) }` |

The line does nothing on a machine without oku, so it is safe in a dotfiles
repo you share between machines. oku never edits your startup files itself.

## 3. Install a package

oku installs packages from wherever their author publishes them. There is no
central catalogue, so you point oku at the project. ripgrep lives on GitHub:

```
$ oku add github:BurntSushi/ripgrep@14.1.1
github:BurntSushi/ripgrep has no manifest, so oku inferred one from its newest release, --verbose prints it
added ripgrep 14.1.1
```

`@14.1.1` asks for that version. Without it, oku takes the newest release.

The ripgrep repo has no manifest, so oku read the release itself.
It picked the download for your OS and CPU, checked it against the checksum
ripgrep publishes, and found the `rg` program inside. Now it runs:

```
$ rg --version
ripgrep 14.1.1 (rev 4649aa9700)
```

> [!TIP]
> oku asks the GitHub API about releases. Without a token, GitHub allows 60
> requests an hour. If you have the `gh` CLI logged in, oku uses its login.
> Otherwise set `GITHUB_TOKEN`.

## 4. Look at your list

`oku add` wrote ripgrep into your list, `~/.config/oku/oku.toml`:

```toml
[packages]
ripgrep = { ref = "github:BurntSushi/ripgrep", version = "14.1.1" }
```

This file is what you want installed. oku also wrote `oku.lock` next to it,
which records exactly what it installed, down to the sha256 of the download.
You edit `oku.toml`, and oku keeps `oku.lock` up to date. Keep both in git, and
another machine can install the same thing. [How oku works](how-oku-works.md)
explains why there are two files.

## 5. Change the version

Open `~/.config/oku/oku.toml` in an editor and change the version to `"15"`,
which means the newest 15.x release:

```toml
[packages]
ripgrep = { ref = "github:BurntSushi/ripgrep", version = "15" }
```

Ask oku what that would change, without changing anything:

```
$ oku sync --dry-run
would change ripgrep from 14.1.1 to 15.2.0
dry run: nothing was changed
```

Then apply it:

```
$ oku sync
ripgrep 14.1.1 -> 15.2.0
profile now holds 1 package, generation 2, 1s
```

`oku sync` makes your machine match your list. Every change like this one
makes a new numbered generation.

## 6. Undo the change

List the generations, then go back one:

```
$ oku generations
  1  2026-09-23 21:37  1 package  + ripgrep 14.1.1
* 2  2026-09-23 21:37  1 package  ripgrep 14.1.1 -> 15.2.0

$ oku rollback
generation 1 is active, 1 package: ripgrep 15.2.0 -> 14.1.1
```

`rg --version` prints 14.1.1 again. Rollback downloads nothing, because the old
version is still on disk.

Rollback does not edit `oku.toml`. It still asks for version 15, so the next
`oku sync` would move to 15.2.0 again. To stay on 14.1.1, change the version
in `oku.toml` back to `"14.1.1"`:

```
$ oku sync
already in sync
```

## 7. Remove it

```
$ oku remove ripgrep
removed ripgrep
```

That removes ripgrep from your list and from your `PATH`. The files stay on
disk so you can still roll back. `oku gc` deletes what no generation uses.

## Next steps

- [Add packages](guides/add-packages.md) from GitHub, GitLab, a URL, npm, PyPI,
  Go or crates.io.
- [Set up a machine from a repo](guides/new-machine.md), so a new laptop is one
  `git clone` and one `oku sync` away.
- [Manage your dotfiles](guides/dotfiles.md) with the same list.
- Run `oku doctor` any time something looks wrong. It checks your `PATH`, the
  shell line and the store, and says what to fix.

To remove oku itself, see [Undo and clean up](guides/undo-and-clean-up.md).
