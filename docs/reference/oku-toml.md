# oku.toml and config.toml

This page lists every key of the two files you write by hand. `oku.toml` is a
[list](../how-oku-works.md#list): what a machine or a project should have.
`config.toml` holds your settings for oku itself. oku writes `oku.lock`, which
has [its own page](lock.md).

The global `oku.toml` and `config.toml` live in the config directory,
`~/.config/oku/` on macOS and Linux, see [paths](paths.md). A
[project](../how-oku-works.md#project) keeps its own `oku.toml` at its root.

## oku.toml

```toml
include = ["github:you/machines#base"]

[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc"]

[runtimes]
node = "./packages/node.toml"

[packages]
ripgrep = "github:BurntSushi/ripgrep"
fd = { ref = "github:sharkdp/fd", version = "^10" }
prettier = "npm:prettier"
rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }

[vars]
email = "me@example.com"

[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.ssh/allowed_signers" = { text = "{{email}} ssh-ed25519 AAAA\n", mode = "0600" }

[defaults."com.apple.dock"]
autohide = true
```

| Key | Type | Page section |
|---|---|---|
| `include` | array of refs | [include](#include) |
| `[packages]` | table | [\[packages\]](#packages) |
| `[lock]` | table | [\[lock\]](#lock) |
| `[runtimes]` | table | [\[runtimes\]](#runtimes) |
| `[env]` | table | [\[env\]](#env) |
| `[vars]` | table | [\[vars\]](#vars) |
| `[files]` | table | [\[files\]](#files) |
| `[secrets]` | table | [\[secrets\]](#secrets) |
| `[defaults]`, `[defaults-currenthost]`, `[registry]`, `[dconf]` | tables | [Settings tables](#settings-tables) |

oku ignores a key it does not know at the top of the file and inside a
`[packages]` table. `when`, `[lock]`, `[runtimes]`, `[env]`, `[files]` and
`[secrets]` reject a key they do not know.

### How oku edits the file

- `oku add` and `oku remove` edit `[packages]` as text. Your comments, the
  order of entries and every other table stay as you wrote them. A new
  package goes at the end of `[packages]`.
- A package written as its own table, `[packages.fd]`, makes `oku add fd` and
  `oku remove fd` stop and ask you to edit it by hand. `oku sync` reads it.
- `oku add` and `oku remove` carry `[files]`, `[vars]`, `[secrets]` and the
  settings tables over unchanged. Only `oku sync` and `oku update` read them
  again.
- You can edit the file by hand and run `oku sync`.

### [packages]

Each key is a package name, and it must equal the `name` in the manifest that
the [ref](refs.md) points at. A manifest oku infers takes the key as its
name. A name that contains `.` needs quotes, as in
`"node.js" = "..."`.

The value is a ref:

```toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"
```

or a table:

| Key | Type | Meaning |
|---|---|---|
| `ref` | string | Required. The [ref](refs.md), without `@version`. |
| `version` | string | An exact version, a prefix such as `"22"`, or a range such as `"^1.4"`. See [Pin a version](refs.md#pin-a-version). `oku add <ref>@<version>` writes it. |
| `when` | table or array of tables | Installs the package only on matching machines. See [when](#when). |
| `service` | boolean | `true` runs the package's services now and at every login. `oku add --service` writes it. See [Services](../guides/services.md). |
| `asset` | string | For a repo with no manifest, the glob that picks its release asset. `oku add --asset` writes it. See [Fix a wrong pick](../guides/add-packages.md#fix-a-wrong-pick-with---asset-and---bin). |
| `bin` | array of strings | For a repo with no manifest, the programs inside the asset. `oku add --bin` writes it. |
| `min_release_age` | string | Replaces [`[lock]` `min_release_age`](#lock) for this package, such as `"0"` for a package you want as soon as it is released. |
| `system` | boolean | `true` puts the package's apps, fonts and services in [system scope](../how-oku-works.md#system-scope). A plain `oku sync` lists those files and skips them, and `oku sync --system` applies them. `oku add --system` writes it. See [System-wide](../guides/system-wide.md). |

```toml
[packages]
fd = { ref = "github:sharkdp/fd", version = "10.2.0" }
postgres = { ref = "github:you/recipes#postgres", service = true, system = true }
atuin-server = { ref = "github:atuinsh/atuin", asset = "atuin-server-*" }
```

A relative file ref, such as `"./recipes/fd.toml"`, starts at the directory of
the list that holds it, not at your working directory. `oku add` writes a
file inside the list's directory this way, and any other file as its absolute
path. In a list that oku read from a repo or a URL, a relative path names a
file beside the list there, see [relative paths](refs.md#relative-paths-in-a-remote-list).

### when

`when` limits a package or a `[files]` entry to some machines.

| Key | Values |
|---|---|
| `os` | `darwin`, `linux`, `windows` |
| `arch` | `amd64`, `arm64` |
| `libc` | `glibc`, `musl`. Linux only. oku reports `musl` when `/lib/ld-musl-*.so.1` exists. |

- A missing key matches anything. Any other key is an error, and so is a
  value that is not a string.
- An array of tables matches a machine that any of them matches. An empty
  array is an error, because it matches nothing.

```toml
[packages]
rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
patchelf = { ref = "github:you/recipes#patchelf", when = { os = "linux", libc = "glibc" } }
fd = { ref = "github:sharkdp/fd", when = [{ os = "darwin" }, { os = "linux" }] }
```

`oku sync` does not install a package whose `when` does not match the
machine. It pins it for the platforms of [`[lock]`](#lock) that `when` matches,
and prints `patchelf 0.18.0, pinned and not installed on darwin-arm64`.
`oku add` and `oku update` may write or narrow a `when`, see
[platform entries](lock.md#platform-entries).

### include

`include` merges other lists under this one. Each item is a ref to a list:

| Item | Reads |
|---|---|
| `./work.toml` | That file, relative to the including list. |
| `https://host/base.toml` | That URL. |
| `github:you/machines` | `oku.toml` at the root of the repo. |
| `github:you/machines#base` | `base.toml` at the root, else `lists/base.toml`. |
| `github:you/machines#dir/base.toml` | That file in the repo. |
| `git+https://host/repo` | `oku.toml` at the root of the repo. |
| `git+https://host/repo#dir/base.toml` | That file in the repo. |

Every forge of [refs](refs.md#ref-forms) works the same way. A list ref takes
no `@version`.

```toml
include = [
  "github:you/machines#base",
  "./work.toml",
]
```

Rules:

- Includes merge in order, so a later include overrides an earlier one. Your
  own `[packages]`, `[vars]`, `[runtimes]` and settings override every include.
- An included list may include others, up to 8 levels deep. A list included
  twice, or one that includes itself, is an error.
- oku never edits an included list, and `oku remove` refuses a package that
  only an include declares.
- oku uses only the `oku.lock` beside your own `oku.toml`, and ignores a lock
  beside an included list. It pins a list from a repo or a URL at a commit and
  a sha256, and does not pin a local file, see [the lock](lock.md#include-entries).
- A relative path in a list from a repo names a file of the same repo at the
  same commit. An absolute path, or one that leaves the repo, is an error.

### [lock]

Says what `oku.lock` pins: the platforms besides the machine you run on, and
how old a version has to be.

| Key | Type | Meaning |
|---|---|---|
| `platforms` | array of strings | Platform names: `darwin-amd64`, `darwin-arm64`, `linux-amd64-glibc`, `linux-amd64-musl`, `linux-arm64-glibc`, `linux-arm64-musl`, `windows-amd64`, `windows-arm64`. |
| `unknown_release_age` | string | What `add`, `update` and `sync` do with a new version whose source gives no release time: `"allow"` takes it and says so, `"warn"` asks on a terminal and refuses without one, `"refuse"` refuses it. The default is `"warn"`. A package that has a locked version keeps it when oku does not take the new one. See [Minimum release age](security.md#minimum-release-age). |
| `min_release_age` | string | How long ago a version must have come out before `add`, `update` and `sync` take it, as a whole number of hours, days or weeks: `"12h"`, `"3d"`, `"2w"`. `"0"` takes the newest. The default is `"1d"`. See [Minimum release age](security.md#minimum-release-age). |

```toml
[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc", "linux-arm64-glibc"]
min_release_age = "3d"
```

Any other key is an error. Only the `[lock]` of your own `oku.toml` counts,
not one in an included list. Without it `add` and `update` pin your own
platform only. What oku writes for another platform is in
[platform entries](lock.md#platform-entries).

### [runtimes]

Names the package that provides an interpreter or a toolchain, for the
packages of registry refs.

| Key | Used by | Without it |
|---|---|---|
| `node` | `npm:` packages run through it. For an npm package with dependencies oku runs this package's `npm`, so on macOS and Linux it lists `bin/npm` beside `bin/node`. | The programs run the `node` on `PATH`, and `oku add` says so. `oku add` fails and names the key for a package that lists dependencies, and on Windows for every `npm:` package. |
| `python` | `pypi:` packages build and run with it. The package provides `python3`. | `oku add pypi:` fails and names the key. |
| `uv` | Installs `pypi:` packages. | `github:astral-sh/uv`, as a build dep. |
| `go` | Builds `go:` packages. | The `go` on `PATH`. |
| `rust` | Builds `cargo:` packages. It provides `cargo` and `rustc`. | The `cargo` on `PATH`, rustup included. |

The value is a ref, or a table:

| Key | Type | Meaning |
|---|---|---|
| `ref` | string | Required. The package's ref. |
| `version` | string | A constraint, written like the constraint of a [dep](manifest.md). oku picks the newest version it allows, when it adds a package and at `oku update`. |

```toml
[runtimes]
node = "./packages/node.toml"
go = { ref = "./packages/go.toml", version = "1.26" }
```

- Each package that uses the runtime gets it as a runtime dep, or a build dep
  for `go`, `rust` and `uv`. A runtime dep does not appear on your `PATH`, and
  a program still finds it first on its own `PATH`.
- `oku.lock` pins the version oku picked, and copies the constraint into the
  manifest of each package that uses it. After you change a runtime, run
  `oku update <name>` for those packages.
- An included list may set `[runtimes]`, and a later list overrides an
  earlier one. A relative path starts at the list that names it.
- `oku.lock` stores a runtime inside the list's directory relative to it, so
  the lock works in another checkout.
- [`examples/runtimes`](../../examples/runtimes) has manifests for node,
  python, go and rust.

How each registry uses its runtime is in
[npm, PyPI, Go and cargo](../guides/npm-pypi-go-cargo.md).

### [env]

`[env]` holds environment variables. The shell hook sets them, and so do
`oku exec` and `oku env`. The global list's apply everywhere, and a project's
apply inside the project once you [allow it](../guides/projects.md#why-you-have-to-allow-a-project).

```toml
[env]
AWS_PROFILE = "api-dev"
REGION = "${AWS_REGION:-eu-west-1}"
API_URL = "https://${REGION}.example.com"
PAGER = false
PATH = { prepend = ["scripts", "node_modules/.bin"] }
DEPLOY_TOKEN = { required = "ask ops for a token" }
```

| Value | Effect |
|---|---|
| a string | Sets the variable. |
| `false` | Unsets the variable. |
| `{ prepend = [...] }` | Puts the entries in front of a list variable such as `PATH` or `MANPATH`. A relative entry starts at the directory of the `oku.toml`. |
| `{ required = "hint" }` | Sets nothing. When the variable is unset or empty, the hook prints the hint once and `oku exec` refuses to run. |

- A string, or a `prepend` entry, may name a variable as `${NAME}`, or as
  `${NAME:-default}` for a default when `NAME` is unset or empty. `$NAME`
  without braces stays as it is, and oku runs no command.
- `${NAME}` reads another key of the same `[env]` as that key sets it, in any
  order, and fails on a cycle. A key that names itself, such as
  `GREETING = "${GREETING}!"`, reads the value from before.
- Later sources win, in this order: your shell, the `[env]` of global
  packages, the global list's `[env]`, the `[env]` of the project's packages,
  the project's `[env]`. The project's own `prepend` entries come before its
  `bin`.
- Leaving a project gives each variable back the value it had before, and
  removes the entries that `prepend` added. If you change such a variable by
  hand inside the project, leaving still restores the value from before you
  entered.
- A name holds letters, digits and `_`. oku refuses a list that sets `PATH`
  other than by `prepend`, or a variable that controls the shell or other
  programs: `HOME`, `SHELL`, `USER`, `IFS`, `ENV`, `BASH_ENV`,
  `PROMPT_COMMAND`, `PS1`, and any `LD_*`, `DYLD_*` or `OKU_*`.
- Only the list itself sets variables. oku refuses an included list with
  `[env]`.

#### [[env.file]]

`[[env.file]]` loads a `.env` file before the values of `[env]`:

```toml
[[env.file]]
path = ".env"

[[env.file]]
path = ".env.deploy"
optional = true
unless = ["CLAUDECODE", "CI"]
```

| Key | Type | Meaning |
|---|---|---|
| `path` | string | The file. A relative path starts at the directory of the `oku.toml`. |
| `optional` | bool | `true` lets the file be missing. Without it, a missing file prints a hint and `oku exec` refuses to run. |
| `unless` | array of names | oku skips the file while one of these variables is set and not empty. |
| `secret` | bool | `true` says the file is encrypted. oku decrypts an age file itself and a sops file with `sops`, then reads the `.env` text inside. |
| `scope` | `"shell"` or `"exec"` | `"exec"` loads the file for `oku exec` only. The shell and `oku env` never get its variables. Default `"shell"`. |

- oku loads the files in order, and a later file wins. The values of `[env]`
  win over every file and may read a file's variables as `${NAME}`.
- The file holds `NAME=value` lines. `export` in front, blank lines and `#`
  comments are fine.
- A value without quotes ends at the end of the line or at ` #`. Double quotes
  may span lines and take `\n`, `\t`, `\"`, `\\` and `\$`. Single quotes keep
  the value as it is.
- A value without quotes or in double quotes expands `${NAME}`,
  `${NAME:-default}` and `$NAME`, from the file's earlier lines and then from
  the environment.
- oku refuses a file that sets a variable a list may not set, such as `PATH`
  or `LD_PRELOAD`, and sets none of its variables.
- The hook reads the files at each prompt, so an edit applies at the next one.
  It decrypts a `secret` file of scope `"shell"` at each prompt too, which
  takes about 10 ms with an age key. A sops file encrypted to a cloud key
  service would call that service before each prompt, so give such a file
  `scope = "exec"`.
- oku finds `sops` in your global profile, then on `PATH`, and the age key
  where [the secrets guide](../guides/secrets.md#create-an-age-key) puts it.
  A file that does not decrypt prints a hint and makes `oku exec` refuse.
- In a project, `oku allow` covers each file that git tracks, and a change to
  one needs a new allow. A file that git does not track, such as a
  gitignored `.env.local`, is yours to change without one. See
  [Why you have to allow a project](../guides/projects.md#why-you-have-to-allow-a-project).

#### oku.\<env\>.toml and oku.local.toml

Two more lists beside a project's `oku.toml` set variables over it:

| File | When | Commit it |
|---|---|---|
| `oku.<env>.toml` | `OKU_ENV=<env>` is set, such as `OKU_ENV=staging` for `oku.staging.toml` | Yes |
| `oku.local.toml` | It exists | No, gitignore it |

```toml
# oku.local.toml
[env]
API_URL = "http://localhost:8080"

[[env.file]]
path = ".env.mine"
```

- Later lists win: `oku.toml`, then `oku.<env>.toml`, then
  `oku.local.toml`. A `required` variable of `oku.toml` counts as set when a
  later list sets it.
- They hold `[env]` alone. oku refuses one with `[packages]` or any other
  table, since packages belong in `oku.lock`.
- `OKU_ENV` takes letters, digits, `-` and `_`. `local` and `pkg` name no
  environment, since `oku.pkg.toml` is a manifest. When `oku.<env>.toml` does
  not exist, the hook prints a hint and `oku exec` refuses to run.
- `oku allow` covers each of them that git tracks, whatever `OKU_ENV` names,
  so a pull that changes `oku.prod.toml` needs a new allow. An untracked one,
  and the `.env` files it loads, are yours to change without one.
- They apply to a project only. The global list has none.

### [vars]

`[vars]` holds values that you name yourself, for `[files]` paths, `text`
entries and templates. Each one is available as `{{name}}`.

```toml
[vars]
font = "JetBrainsMono Nerd Font Propo"
email = "me@example.com"

[vars.theme]
base00 = "0c1410"
base05 = "c2d6ba"
```

- A value is a string. A table under `[vars]` gives names joined by a dot,
  such as `{{theme.base00}}`. Any other type is an error.
- A name may hold letters, digits, `_`, `-` and `.`.
- `{{ name }}` with spaces is the same as `{{name}}`.
- The locations of [`[files]`](#files), such as `{{home}}`, are variables too,
  and so is `{{secret.<name>}}` for a [secret](#secrets).
- A name that is not set stops the sync before it changes anything. The error
  gives the template and the line.
- `\{{` writes the two braces themselves.
- There are no conditionals and no loops. oku writes every other byte of a
  template as it is.
- An included list may set `[vars]`. A later include overrides an earlier one,
  and your own list overrides them all.
- Changing a variable and running `oku sync` writes every file that uses it
  again, in one generation.

See [Dotfiles](../guides/dotfiles.md) for templates in use.

### [files]

Places files in your home directory. Each key is the path to write, and it
starts with a location:

| Location | Path |
|---|---|
| `{{home}}` | Your home directory. |
| `{{config}}` | `$XDG_CONFIG_HOME`, else `~/.config`. `%APPDATA%` on Windows. |
| `{{data}}` | `$XDG_DATA_HOME`, else `~/.local/share`. `%LOCALAPPDATA%` on Windows. |
| `{{appdata}}`, `{{localappdata}}` | Windows only. An entry that uses one needs `when = { os = "windows" }`. |

Each value is a table with exactly one of `link`, `text`, `render` and
`secret`:

| Key | Type | Meaning |
|---|---|---|
| `link` | string | The path becomes a symlink to this file or directory. A relative source starts at the directory of the list. `{{pkg.<name>}}/...` links into a package of the list, and follows it to a new version after `oku update`. |
| `text` | string | The path gets this content. oku keeps it in the generation, read-only, and the path links to it. |
| `render` | string | Like `text`, with the content from a template file beside the list. See [\[vars\]](#vars). |
| `secret` | string | The path gets a value decrypted from a sops or an age file. See [\[secrets\]](#secrets). |
| `key` | string | With `secret` only. The path of one value in a sops file, with `/` between its parts, such as `ssh/id_ed25519`. Without it the whole decrypted file is the value. |
| `mode` | string | The permission of a `text`, `render` or `secret` file, such as `"0600"`, or `"0755"` for a script, up to `"0777"`. Without it the file is read-only, and a secret is `0600`. An error on a `link`. |
| `vars` | table of strings | Overrides `[vars]` for this `text` or `render` entry, such as `vars = { font-size = "13" }`. An error on `link` and `secret`. |
| `when` | table or array | As for [packages](#when). |

```toml
[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
"{{home}}/.claude/skills/deslop" = { link = "{{pkg.cursor-plugins}}/skills/deslop" }
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
```

Rules:

- oku refuses a path that exists and that it did not write. It names the
  path and changes nothing.
- The next `oku sync` removes a path whose entry left the list. A file of
  your own that replaced oku's link stays.
- A `mode` of `"0600"` or tighter, or a `secret`, makes a directory that oku
  has to create `0700`. Other entries get `0755`. A directory that exists
  keeps its mode.
- `oku rollback` brings back the bytes of that generation.
- On Windows a linked directory is a junction, and a linked file or a `text`
  is a copy, see [Windows](../guides/windows.md).

### [secrets]

Gives a decrypted value a name, for `{{secret.<name>}}` in a `text` or a
template.

| Key | Type | Meaning |
|---|---|---|
| `file` | string | Required. A sops file (YAML, JSON, dotenv or INI) or an age file, relative to the list. |
| `key` | string | The path of one value in a sops file. An age file holds one value, so `key` with it is an error. |

```toml
[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }
```

A name that `[secrets]` lacks is an error. oku finds the age key in
`SOPS_AGE_KEY_FILE`, else `sops/age/keys.txt` in your config directory, see
[paths](paths.md#environment-variables). For the whole setup, see
[Secrets](../guides/secrets.md).

### Settings tables

Each table writes per-user settings through the mechanism of one OS. oku
skips the tables of another OS, so one list works on every machine.

| Table | OS | Writes through |
|---|---|---|
| `[defaults."<domain>"]` | macOS | `/usr/bin/defaults`. |
| `[defaults-currenthost."<domain>"]` | macOS | `defaults -currentHost`, for a setting of this one Mac in `~/Library/Preferences/ByHost/`. oku prints it as `currentHost:<domain> <key>`. |
| `[registry.'HKCU\...']` | Windows | `reg.exe`. A key outside `HKCU` is an error on every OS. |
| `[dconf."<path>"]` | Linux | The `dconf` tool. The table name is the directory of its keys, without slashes at the ends. |

```toml
[defaults."com.apple.dock"]
autohide = true
tilesize = 48
autohide-delay = 0.0

[defaults."com.apple.Safari".NSUserKeyEquivalents]
"Show Next Tab" = "^l"

[defaults-currenthost."com.apple.controlcenter"]
BatteryShowPercentage = true

[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"

[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

- Quote a domain that has a dot. `[defaults.com.apple.dock]` without quotes is
  a table `com` that holds a table `apple`.
- Write a registry key in single quotes, so TOML keeps its backslashes.
- On Linux oku skips `[dconf]` when the `dconf` tool is not installed, and
  prints `[dconf] is skipped, because the dconf tool is not on PATH`.

The type comes from the TOML value:

| TOML | macOS | Windows | Linux |
|---|---|---|---|
| `true`, `false` | boolean | `REG_DWORD` 1 or 0 | boolean |
| `48` | integer | `REG_DWORD`, or `REG_QWORD` above 4294967295. A negative number is an error. | `int32`, or `int64` outside its range |
| `0.5` | float. Write `0.0`, not `0`, for a float setting. | An error. | double |
| `"left"` | string | `REG_SZ` | string |
| `["a", "b"]` | array | `REG_MULTI_SZ`, of strings that are not empty | An array of one type. An empty array is an error. |
| a table | dictionary, which oku owns and writes whole | An error. Write the subkey as its own table. | An error. |

Before oku first writes a key it records the value the key had, or that it had
none. When a key leaves the list, oku writes that value back with its type,
or deletes the key again. `oku rollback` and `oku self uninstall` do the same. What
happens after a write, such as the Dock restart, is in
[OS settings](../guides/os-settings.md).

### What each kind of list may hold

| Table | Global list | Included list, file or repo | Included list at a URL | Project list |
|---|---|---|---|---|
| `[packages]`, `include`, `[runtimes]` | yes | yes | yes | yes |
| `[env]` | yes | error | error | yes |
| `[lock]` | yes | ignored | ignored | yes |
| `[vars]`, settings tables | yes | yes | yes | error |
| `[files]`, `[secrets]` | yes | yes | error | error |

For a list from a repo, oku puts the repo's files at the pinned commit in the
store, and a `link` points there, read-only. A `link`, `render` or `secret` path of
such a list must stay inside the repo.

## config.toml

`config.toml` sits beside the global `oku.toml`. oku commands write most of it,
and you may edit it by hand.

| Key | Type | Written by | Meaning |
|---|---|---|---|
| `[sources]` | table of strings | `oku source add`, `oku source remove` | Aliases for manifest collections, `alias = "ref"`. See [sources](refs.md#sources-and-aliases). |
| `caches` | array of strings | `oku cache add`, `oku cache remove` | Directories and http(s) URLs of [build caches](../guides/build-caches.md), in the order oku tries them. |
| `trusted_keys` | array of strings | `oku key trust`, `oku key revoke` | minisign public keys whose cache entries oku accepts. |
| `[runtimes]` | table | by hand | The same table as in [oku.toml](#runtimes). oku uses it when no list names that runtime. A relative path starts at the directory of `config.toml`, and a ref may be a source alias such as `core/node`. |
| `store_root` | string | `oku setup --system` | The shared store root, such as `/opt/oku`. Delete the line to go back to the store in the data directory, then run `oku sync`. |

```toml
store_root = '/opt/oku'
caches = ['https://example.com/oku-cache']
trusted_keys = ['RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF']

[sources]
core = 'github:you/recipes'

[runtimes]
node = 'core/node'
```

`signing.key`, the secret key of `oku cache push`, is a separate file beside
it.
