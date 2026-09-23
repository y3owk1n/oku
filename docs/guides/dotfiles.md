# Manage your dotfiles

You end up with the config files of your home directory declared in
`oku.toml`, placed by `oku sync`, and undone by `oku rollback` together with
your packages.

## Place your first files

Put your configs in a `files/` directory beside `oku.toml`, then add a
`[files]` table to the list. Each key is the path to write, and each value says
where the content comes from:

```toml
# ~/.config/oku/oku.toml
[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.ssh/allowed_signers" = { text = "me@example.com ssh-ed25519 AAAA\n", mode = "0600" }
```

Run `oku sync`:

```
$ oku sync
wrote /home/you/.config/nvim
wrote /home/you/.ssh/allowed_signers
profile now holds 0 packages, 2 files, generation 1, 40ms
```

`~/.config/nvim` is now a symlink to `~/.config/oku/files/nvim`. oku created
`~/.ssh` with mode `0700` because the entry has a mode of `0600`.

`oku sync --dry-run` shows what a sync would write and changes nothing. See
[Commands](../reference/commands.md).

### Start a path with a location

Every key starts with one of these locations:

| Location | Path |
|---|---|
| `{{home}}` | Your home directory. |
| `{{config}}` | `$XDG_CONFIG_HOME`, else `~/.config`. `%APPDATA%` on Windows. |
| `{{data}}` | `$XDG_DATA_HOME`, else `~/.local/share`. `%LOCALAPPDATA%` on Windows. |
| `{{appdata}}`, `{{localappdata}}` | Windows only. An entry that uses one needs `when = { os = "windows" }`. |

The rest of the key may use your own [variables](#fill-files-from-variables).
A key that is only the location, such as `"{{home}}"`, is an error.

## Choose link, text, render or secret

An entry holds exactly one of these four keys:

| Key | What the path becomes |
|---|---|
| `link` | A symlink to a file or a directory. An edit of the source shows at once, with no sync, and your repo versions the content. |
| `text` | A file with this content. oku keeps the content in the [generation](../how-oku-works.md#generation), read-only, and the path is a symlink to it. |
| `render` | Like `text`, with the content read from a template, see [Fill files from variables](#fill-files-from-variables). |
| `secret` | A value that oku decrypts from a sops or an age file, see [Place a secret](#place-a-secret). |

A relative source of `link`, `render` or `secret` starts at the directory of
the list that declares the entry, not at your working directory. A `link` may
also name an absolute path.

Use `link` for a config that you edit all day, such as an editor config. Use
`text` or `render` for a file that only oku should change.

### Set a file's mode

`mode` sets the permission of a `text`, `render` or `secret` file:

```toml
[files]
"{{home}}/.ssh/config" = { text = "Host *\n  IdentitiesOnly yes\n", mode = "0600" }
"{{home}}/.local/bin/backup" = { render = "./files/backup.sh.tmpl", mode = "0755" }
```

- Any mode up to `"0777"` works. `"0755"` gives a script you can run.
- Without `mode` the file is read-only, and a secret is `0600`.
- A sync that changes only the mode applies it.
- `mode` on a `link` is an error, because a link has the permissions of its
  source.

When the directory above the path is missing, oku creates it. It gets mode
`0700` when the entry is a secret or its mode gives group and others nothing,
such as `"0600"` or `"0400"`. Any other entry gets `0755`. So the
`~/.ssh/config` entry above creates `~/.ssh` in the form ssh accepts. A
directory that already exists keeps its mode. On Windows modes do not apply.

## Fill files from variables

`[vars]` holds values that you name yourself. A path, a `text` and a template
use them as `{{name}}`:

```toml
[vars]
font = "JetBrainsMono Nerd Font Propo"
email = "me@example.com"

[vars.theme]
base00 = "0c1410"
base05 = "c2d6ba"

[files]
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
"{{home}}/.ssh/allowed_signers" = { text = "{{email}} ssh-ed25519 AAAA\n" }
```

```
# files/ghostty.tmpl
font-family = {{font}}
background = #{{theme.base00}}
foreground = #{{theme.base05}}
```

After `oku sync`, `~/.config/ghostty/config` holds:

```
font-family = JetBrainsMono Nerd Font Propo
background = #0c1410
foreground = #c2d6ba
```

The rules of a template:

- A value is a string. A table under `[vars]` gives names joined by a dot,
  such as `{{theme.base00}}`. A name may hold letters, digits, `_`, `-` and
  `.`.
- `{{ name }}` with spaces is the same as `{{name}}`. A base16 template works
  unchanged when you name your variables the way it does, such as
  `base00-hex`.
- `{{home}}`, `{{config}}` and `{{data}}` are variables too.
- Write `\{{` for the two braces themselves, for a config that has its own
  `{{...}}` syntax.
- There are no conditionals and no loops.
- oku writes every other byte of the template as it is, so the result is the
  same on every OS.

A name that is not set stops the sync before it changes anything. The error
gives the template and the line:

```
$ oku sync
oku: /home/you/.config/oku/files/ghostty.tmpl:1: font is not set in [vars]
```

### Override a variable for one entry

`vars` on a `text` or `render` entry overrides `[vars]` for that entry only:

```toml
[files]
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl", vars = { font-size = "13" } }
```

`vars` on a `link` or a `secret` is an error, because those fill in nothing.

### Change a theme in one step

Keep a colour scheme as a table under `[vars]` and use it from every template.
When you change a value and run `oku sync`, oku writes every file that uses it
again, in one generation:

```
$ oku sync
changed /home/you/.config/ghostty/config
profile now holds 0 packages, 3 files, generation 2, 20ms
```

`oku rollback` brings the old bytes back.

### Share variables between lists

An [included list](new-machine.md#split-the-list-with-include) may set
`[vars]`. A later include overrides an earlier one, and your own `oku.toml`
overrides them all. To theme one machine differently, set the same names in
that machine's own list.

## Write a different file on each OS

`when` limits an entry to matching machines, as it does for a package:

```toml
[files]
"{{config}}/kanata" = { link = "./files/kanata", when = { os = "darwin" } }
```

The keys are `os`, `arch` and `libc`. For several platforms, write an array of
tables, such as `when = [{ os = "darwin" }, { os = "linux" }]`. See
[oku.toml](../reference/oku-toml.md) for the values.

For one path whose content differs by OS, write two entries with the same
target and template, each with its own `when` and `vars`. TOML allows a key
once per table, so put the second entry in another list that you
[include](new-machine.md#split-the-list-with-include):

```toml
# oku.toml
include = ["./lists/files-linux.toml"]

[files]
"{{config}}/app/config.toml" = { render = "./files/app.toml.tmpl", when = { os = "darwin" }, vars = { shell = "/bin/zsh" } }
```

```toml
# lists/files-linux.toml
[files]
"{{config}}/app/config.toml" = { render = "../files/app.toml.tmpl", when = { os = "linux" }, vars = { shell = "/bin/sh" } }
```

Two lists may write one target when their `when` differ. When both have the
same `when`, the later list wins. You can also let the tool itself include a
second file.

## Point a config at a program in your profile

A config that names a program by its store path breaks when the program
updates. Name it through the [profile](../how-oku-works.md#profile) instead,
whose path stays the same:

```toml
[files]
"{{home}}/.config/tmux/shell.conf" = { text = "set -g default-shell {{data}}/oku/profiles/global/current/bin/fish\n" }
```

## Link files out of a package

Some repos ship no program at all: agent skills, templates, a colour scheme. A
package whose manifest says `data = true` holds only files. It puts nothing on
`PATH`, and the list reaches its files with `{{pkg.<name>}}`:

```toml
# packages/my-skills.toml
[package]
name = "my-skills"

[version]
value = "2026.09.18"

[[artifact]]
url = "https://github.com/someone/skills/archive/032be146865d973682535de75f2287da438550bf.tar.gz"
sha256 = "1d9c0f75def9a97cedd8cfff5eadc60475c03913a33ed9880bf8987999b3761f"
strip = 1
data = true
```

```toml
# oku.toml
[packages]
my-skills = "./packages/my-skills.toml"

[files]
"{{home}}/.claude/skills/deslop" = { link = "{{pkg.my-skills}}/skills/deslop" }
```

- `{{pkg.<name>}}` works at the start of a `link` source, for any package of
  the list, with or without `data = true`. It names the directory that holds
  the package's files in the [store](../how-oku-works.md#store).
- After `oku update` the link points into the new version, and
  `oku rollback` brings back the files of the version before.
- The lock pins the package like any other.
- A package that is not in the list on this machine, for example because its
  `when` does not match, is an error.

`data = true` together with another output, such as `bin`, is an error. See
[the manifest reference](../reference/manifest.md).

## Place a secret

A `secret` entry writes one value that oku decrypts from a
[sops](https://github.com/getsops/sops) or an
[age](https://github.com/FiloSottile/age) file:

```toml
[files]
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
```

`key` names one value in a sops file. A `text` or a template can use a named
secret as `{{secret.<name>}}`. [Secrets](secrets.md) covers the key, the
formats and what lands on disk.

## Move an existing file under oku

oku refuses a path that exists and that it did not write. It names the path
and changes nothing:

```
$ oku sync
oku: /home/you/.config/git/config already exists and oku did not put it there
move it away, or take it out of [files]
```

Move the file into your repo, point the entry at it, and sync again:

```sh
mv ~/.config/git/config ~/.config/oku/files/git-config
oku sync
```

oku records every path it writes in its
[ledger](../how-oku-works.md#ledger), and never overwrites a path that is not
in it.

## Remove a file

Delete the entry from `oku.toml` and run `oku sync`. oku removes the path:

```
$ oku sync
removed /home/you/.config/nvim
profile now holds 0 packages, 2 files, generation 3, 30ms
```

If you replaced oku's link with a file of your own in the meantime, oku leaves
that file alone. `oku self uninstall` removes every file oku placed.

## Undo a change

`oku rollback` puts back the files of the generation before, with the rest of
the machine:

```
$ oku rollback
wrote /home/you/.config/nvim
generation 2 is active, 0 packages, 3 files: + /home/you/.config/nvim
```

- A `text` or `render` file gets back the exact bytes of that generation,
  even when the template has changed since.
- A `link` comes back as a link. The content of its source is in your repo, so
  git brings back an old version of it.
- A secret is decrypted again from the encrypted file that the generation
  holds.

Rollback does not change `oku.toml`. The next `oku sync` applies the list as it
is. See [Undo and clean up](undo-and-clean-up.md).

## Know where [files] may live

- Only the global list and the lists it includes may hold `[files]`, `[vars]`
  and `[secrets]`. A [project](projects.md) list with `[files]` is an error,
  so that a cloned repo cannot write into your home directory:

  ```
  $ cd ~/src/app && oku sync
  project /home/you/src/app
  oku: /home/you/src/app/oku.toml has [files], and only the global list may place files
  ```

- An included list may hold `[files]` when it is a file on this machine or
  comes from a repo. A list at a URL may not:

  ```
  $ oku sync
  oku: https://example.com/base.toml: a list at a URL cannot hold [files] or [secrets], put it in a repo
  ```

- For a list from a repo, oku puts the repo's files at the pinned commit in
  the store, and a `link` leads there. The linked file is read-only, and it
  changes when the list's commit changes. A `link`, `render` or `secret` path
  of such a list must stay inside the repo.
- `oku add` and `oku remove` carry `[files]` over unchanged. Only `oku sync`
  and `oku update` read the table again.

## On Windows

A normal Windows user cannot create a symlink, so `[files]` uses what Windows
allows:

| Entry | On Windows |
|---|---|
| `link` to a directory | A junction. An edit of the source shows at once. |
| `link` to a file | A copy. Run `oku sync` after you edit the source. |
| `text`, `render` | A copy of the content in the generation. `oku rollback` writes the bytes of that generation. |
| `secret` | A copy, with an access control list that names you alone. |

oku records the sha256 of each copy. When a copy no longer holds those bytes,
the next `sync`, `add`, `remove`, `update` or `rollback` stops before it
changes anything and names the file. Move the change into the source or into
`oku.toml`, delete the copy, and run the command again. See
[Windows](windows.md).
