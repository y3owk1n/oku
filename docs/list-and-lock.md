# List and lock

Two files in the config directory describe a machine:

- `oku.toml` is what you want installed. You edit it.
- `oku.lock` is what oku resolved. oku writes it.

Both live in `~/.config/oku/`, or `$XDG_CONFIG_HOME/oku/`. Keep both in version
control.

## oku.toml

```toml
# search tools
[packages]
ripgrep = "github:you/recipes#ripgrep"
fd = { ref = "github:you/recipes#fd", version = "10.2.0" }
"node.js" = "https://example.com/node.toml"
```

Each key under `[packages]` is a package name, and it must equal the `name` in
the manifest the [ref](refs.md) points at. The value is a ref, or a table with
`ref` and an optional `version`. A name that contains `.` needs quotes.

`oku add` and `oku remove` edit this file as text. Your comments, the order of
entries and any other tables stay as you wrote them. A new package goes at the
end of `[packages]`.

You can also edit the file by hand and run `oku sync`.

A relative file ref such as `"./recipes/fd.toml"` starts at the directory of the
list that contains it, not at your working directory.

`oku add` writes a file inside the list's directory this way, in `oku.toml` and
in `oku.lock`, so a project that keeps its manifests beside its list works in
any checkout. oku stores a file outside that directory as its absolute path.

### Services

`service = true` runs the package's [services](services.md) now and at every
login:

```toml
postgres = { ref = "github:you/recipes#postgres", service = true }
```

### Packages for some platforms only

`when` limits a package to matching machines:

```toml
[packages]
rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
patchelf = { ref = "github:you/recipes#patchelf", when = { os = "linux", libc = "glibc" } }
```

The keys are `os`, `arch` and `libc`, with the values described in the
[manifest reference](manifest.md#match-values). A missing key matches anything.
Any other key is an error.

For several platforms, write an array of tables. The package goes on a machine
that any of them matches:

```toml
[packages]
fd = { ref = "github:you/recipes#fd", when = [{ os = "darwin" }, { os = "linux" }] }
```

A `[files]` entry takes `when` in the same two forms.

`oku sync` does not install a package whose `when` does not match the machine.
If the lock already has an entry for it from another machine, that entry stays.
oku can also pin such a package from your machine, see
[One lock for several machines](#one-lock-for-several-machines).

### Including other lists

`include` merges other lists under this one:

```toml
include = [
  "github:you/machines#base",
  "./work.toml",
]

[packages]
ripgrep = "./my-ripgrep.toml"
```

Each item is a ref to a list:

| Ref | Reads |
|---|---|
| `./work.toml` | That file, relative to the including list. |
| `https://host/base.toml` | That URL. |
| `github:you/machines` | `oku.toml` at the root of the repo. |
| `github:you/machines#base` | `base.toml` at the root, else `lists/base.toml`. |
| `git+https://host/repo` | `oku.toml` at the root of the repo. |
| `git+https://host/repo#dir/base.toml` | That file in the repo. |

Rules:

- Includes merge in order, so a later include overrides an earlier one.
- A package in your own `[packages]` overrides the same name from any include.
- An included list may include others, up to 8 levels deep. A list that is
  included twice, or that includes itself, is an error.
- In a list from a URL or a repo, a relative path names a file in the same repo
  at the same commit, or the URL beside the list. So a repo laid out as
  `oku.toml`, `lists/` and `packages/` works with `oku sync github:you/machines`.
  An absolute path is an error, because it names a file on the author's
  machine, and so is a path that leaves the repo.
- oku never edits an included list. `oku remove` refuses a package that only
  an include declares. Remove it there, or take the include out.
- oku uses only the `oku.lock` beside your own `oku.toml`. It ignores a lock
  beside an included list.

One form is not editable by oku. A package written as its own table,
`[packages.fd]`, makes `oku add fd` and `oku remove fd` stop and ask you to
edit it by hand. `oku sync` reads that form.

### System scope

`system = true` puts the package's apps, fonts and services in
[system scope](system-scope.md), for every user of the machine:

```toml
postgres = { ref = "github:you/recipes#postgres", service = true, system = true }
```

A plain `oku sync` skips those files and lists them. `oku sync --system` applies
them with `sudo`.

### Files in your home directory

`[files]` places files in your home directory. Each key is the path to write,
and `oku sync` applies the table:

```toml
[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.ssh/allowed_signers" = { text = "me@example.com ssh-ed25519 AAAA\n", mode = "0600" }
"{{home}}/.claude/skills/deslop" = { link = "{{pkg.cursor-plugins}}/skills/deslop" }
```

A key starts with a location:

| Location | Path |
|---|---|
| `{{home}}` | Your home directory. |
| `{{config}}` | `$XDG_CONFIG_HOME`, else `~/.config`. `%APPDATA%` on Windows. |
| `{{data}}` | `$XDG_DATA_HOME`, else `~/.local/share`. `%LOCALAPPDATA%` on Windows. |
| `{{appdata}}`, `{{localappdata}}` | Windows only. An entry that uses one needs `when = { os = "windows" }`. |

An entry holds one of `link`, `text`, `render` and `secret`, and may hold
`when`, like a package, and `vars`.

| Key | Effect |
|---|---|
| `link` | The path becomes a symlink to a file or a directory. A relative source starts at the directory of the list. An edit of the source shows at once, with no sync, and your own repo versions the content. |
| `link = "{{pkg.<name>}}/..."` | The source is inside a package of the list, in the directory that holds its files. After `oku update` the link points into the new version. |
| `text` | The path gets this content. oku keeps the content in the generation, read-only, and the path is a symlink to it. `oku rollback` brings back the bytes of that generation. |
| `render` | Like `text`, with the content read from a template beside the list, see [Variables and templates](#variables-and-templates). |
| `secret` | The path gets a value that oku decrypts from a sops or an age file, see [Secrets](secrets.md). |
| `mode` | The permission of a `text`, `render` or `secret` file, such as `"0600"`, or `"0755"` for a script. Without it the file is read-only, and a file that holds a secret is `0600`. A mode of `"0600"` or tighter, or a secret, makes a directory that oku has to create `0700`, see [Files and directories](files.md#outside-okus-directories). |
| `vars` | A table of strings that overrides `[vars]` for this `text` or `render` entry, such as `vars = { font-size = "13" }`. |

oku refuses a path that exists and that it did not write. It names the path and
changes nothing, so move the file away first. The next `sync` removes a path
whose entry left the list. If you replaced oku's link with a file of your own
in the meantime, oku leaves that file alone.

`oku add` and `oku remove` carry the files over unchanged. Only `sync` and
`update` read `[files]` again.

### Variables and templates

`[vars]` holds values that you name yourself. A path, a `text` and a template
can use them as `{{name}}`:

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

- A value is a string. A table under `[vars]` gives names joined by a dot, such
  as `{{theme.base00}}`. A name may hold letters, digits, `_`, `-` and `.`.
- `{{ name }}` with spaces is the same as `{{name}}`. A base16 template works
  unchanged when you name your variables the way it does, such as `base00-hex`.
- The locations `{{home}}`, `{{config}}` and `{{data}}` are variables too. A
  config can name a program in oku's profile as
  `{{data}}/oku/profiles/global/current/bin/fish`, and that path stays the same
  when the program updates.
- A name that is not set stops the sync before it changes anything. The error
  gives the template and the line.
- Write `\{{` for the two braces themselves, for a config that has its own
  `{{...}}` syntax.
- There are no conditionals and no loops. For a difference between platforms,
  write two entries with `when` that share the target and the template, each
  with its own `vars`, or let the tool include a second file.
- oku writes every other byte of a template as it is, so the result is the same
  on every OS.

An included list may set `[vars]`. A later include overrides an earlier one, and
your own list overrides them all. To theme a machine differently, set the same
names in that machine's own list.

Changing a variable and running `oku sync` writes every file that uses it
again, in one generation. `oku rollback` brings the old bytes back.

### Settings of the OS

A settings table sets per-user settings. Each table belongs to the mechanism of
one OS, because a setting of one OS has no counterpart on another:

```toml
[defaults."com.apple.dock"]
autohide = true
tilesize = 48
autohide-delay = 0.0
orientation = "left"

[defaults.NSGlobalDomain]
AppleInterfaceStyle = "Dark"
KeyRepeat = 1

[defaults.".GlobalPreferences"]
AppleLanguages = ["en-SG", "ms-MY"]

[defaults."com.apple.Safari".NSUserKeyEquivalents]
"Show Next Tab" = "^l"
```

| Table | OS | Status |
|---|---|---|
| `[defaults."<domain>"]` | macOS | Works. oku writes through `/usr/bin/defaults`. |
| `[defaults-currenthost."<domain>"]` | macOS | Works. A setting of this one Mac, which `defaults -currentHost` writes. |
| `[registry.'HKCU\...']` | Windows | Works. oku writes through `reg.exe`. A key outside `HKCU` is an error on every OS. |
| `[dconf."<path>"]` | Linux | Works. oku writes through the `dconf` tool. |

oku skips the tables of another OS, so one list serves every machine. On Linux
it also skips `[dconf]` when the `dconf` tool is not installed, as on a server,
and prints `[dconf] is skipped, because the dconf tool is not on PATH`.

macOS keeps some settings per Mac and not per user, in
`~/Library/Preferences/ByHost/`. The battery percentage in the menu bar is one.
Such a key does nothing in `[defaults]`. Put it in `[defaults-currenthost]`:

```toml
[defaults-currenthost."com.apple.controlcenter"]
BatteryShowPercentage = true
```

`defaults -currentHost read <domain>` shows what a Mac holds there. oku prints
such a setting as `currentHost:com.apple.controlcenter BatteryShowPercentage`.

Quote a domain that has a dot. `[defaults.com.apple.dock]` without quotes is a
table `com` that holds a table `apple`, and not the domain you meant.

The type comes from the TOML value:

| TOML | macOS | Windows | Linux |
|---|---|---|---|
| `true`, `false` | boolean | `REG_DWORD` 1 or 0 | boolean |
| `48` | integer | `REG_DWORD`, or `REG_QWORD` above 4294967295. A negative number is an error. | `int32`, or `int64` outside its range |
| `0.5` | float. Write `0.0`, not `0`, where the setting is a float. | An error, the registry has no float. | double |
| `"left"` | string | `REG_SZ` | string |
| `["a", "b"]` | array | `REG_MULTI_SZ`, of strings that are not empty | An array of one type. An empty array is an error, because it has no type. |
| a table | dictionary. oku owns the whole dictionary and writes it whole. | An error. Write the subkey as its own table. | An error. |

Write a registry key in single quotes, so that TOML keeps its backslashes:

```toml
[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"
```

A `[dconf]` table is named after the directory of its keys, without the slashes
at the ends:

```toml
[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

Writing needs the dconf service and a D-Bus session, which a desktop login has.
Without a session the write fails and oku undoes the change. oku keeps a dconf
type that the list cannot write, such as `uint32`, when it puts a value back.

oku puts a value back with the type it had. That includes a type that the list
cannot write, such as `REG_BINARY` or `REG_EXPAND_SZ`.

Before oku first writes a key it records the value the key had, or that it had
none. A key that leaves the list gets that value back, or is deleted again.
`oku rollback` and `oku self uninstall` do the same. Settings go through the
same check and undo as everything else, see
[a change that fails](files.md#a-change-that-fails).

After a change oku tells macOS to read its settings again, so they show without
a logout. The Dock reads its own only when it starts, so oku restarts it when a
`com.apple.dock` key changed, the way nix-darwin does. Other apps that read
their settings only at start, such as Finder, show a change after you restart
them, for example with `killall Finder`. An open System Settings window can
write its own values over a change, so close it before you sync. oku never writes `/Library/Preferences` or anything else that
needs root. When oku deletes the last key of a domain that it created, macOS
keeps an empty file for that domain.

`oku add` and `oku remove` carry the settings over unchanged. Only `sync` and
`update` read the tables again. An included list may hold settings, and your own
list overrides a key that an include sets.

Limits for now:

- Only the global list may hold `[files]`, `[vars]`, `[secrets]` and settings
  tables. A project list with `[files]` is an
  error, so that a cloned repo cannot write into your home directory.
- An included list may hold `[files]` when it is a file on this machine or
  comes from a repo, not when it is at a URL.
- For a list from a repo, oku puts the repo's files at the pinned commit in the
  store, and a `link` leads there. The linked file is read-only, and it changes
  when the list's commit changes. A `link`, `render` or `secret` path of such a
  list must stay inside the repo.

On Windows a normal user cannot create a symlink. A linked directory is a
junction there, and a linked file or a `text` is a copy, see
[Windows](windows.md#files-in-your-home-directory).

## oku.lock

```toml
# Written by oku. Commit this file, and change it with oku commands only.

[[package]]
name = 'ripgrep'
ref = 'github:you/recipes#ripgrep'
commit = '3fce3b5bb0236da2df6d99672afb8a719642eca7'
manifest_sha256 = '17b39b37...'
version = '14.1.1'

[package.platform]
[package.platform.darwin-arm64]
strategy = 'artifact'
url = 'https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-apple-darwin.tar.gz'
sha256 = '24ad7677...'

[package.platform.linux-amd64-glibc]
strategy = 'artifact'
url = '...'
sha256 = '...'
```

The lock pins each included list too:

```toml
[[include]]
ref = 'github:you/machines#base'
commit = 'dc2478ae14dc9931336430027ff284d4dc8e4d44'
sha256 = 'd60f12df...'
```

A list that is a file on this machine is yours, like `oku.toml`. oku does not
pin it, so you edit it and run `oku sync`.

`oku sync` reads a list from a URL or a repo at its pinned commit and stops if
its content no longer has the pinned sha256:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

`oku update` with no names reads includes fresh. `oku update <name>` keeps them
pinned, so it never adds or drops packages.

Keys of a `[[package]]` entry:

| Key | Meaning |
|---|---|
| `ref` | The ref from `oku.toml`, without `@version`. |
| `commit` | The commit the manifest was read at. Only for `github:`, `codeberg:`, `gitea:`, `gitlab:` and `git+` refs. |
| `manifest_sha256` | Digest of the manifest file. |
| `signing_key` | The manifest's minisign key, when it has one. oku refuses a manifest with another key until you pass `--accept-key`. |
| `version` | The version installed. |
| `tag` | The upstream tag of that version, when it differs, such as `v10.2.0`. |
| `tag_commit` | The full commit a [moving tag](manifest.md#a-moving-tag) pointed at for that version. |
| `inferred`, `manifest` | Set for a package whose repo has no manifest. `manifest` holds the full text oku inferred. |
| `platform.<name>` | One entry per platform that has resolved this package. |
| `dep` | The packages this one depends on, pinned the same way. |

A platform entry has `strategy`, which is `artifact` or `build`. An artifact
also has `url` and `sha256`, and `commands = true` when the manifest runs the
download to [generate its completions](manifest.md#completions). A build has `vendor_sha256` when it has vendor
steps, and `impure = true` when one of its steps used `network = true`. oku keeps
these beside the build in the store, so `oku update` of a build that did not
change writes the same lock.

Platform names are `os-arch`, plus `-glibc` or `-musl` on Linux.

Deps are nested under the package that needs them, as `[[package.dep]]`, and a
dep's own deps nest under it. Each package pins its own, so two packages can
pin different versions of the same dep. `oku sync` installs every dep at its
pinned version, and `oku update` re-resolves them with their parent.

Packages are sorted by name, so the same state always writes the same bytes
and diffs stay small.

## One lock for several machines

The version, the manifest and the deps of a package are the same on every
platform. Only the `platform` entries differ. A machine installs from the lock
without changing it when the lock holds the entry of that machine's platform.

`[lock]` in `oku.toml` names the platforms to pin besides your own:

```toml
[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc", "linux-arm64-glibc"]
```

`oku add`, `oku update` and `oku sync` then write an entry for each of them,
from any machine. A Mac pins the Linux download, and the Linux machine installs
it with no change to the lock. The names are the platform names above:
`darwin-amd64`, `darwin-arm64`, `linux-amd64-glibc`, `linux-amd64-musl`,
`linux-arm64-glibc`, `linux-arm64-musl`, `windows-amd64` and `windows-arm64`.

oku takes the checksum of another platform from the manifest's `sha256`, else
from its `sha256_url`. With neither it downloads the file, hashes it and tells
you, see [Trust on first use](trust.md#trust-on-first-use). It never unpacks or
runs a download for another platform.

A platform that the manifest builds from source gets `strategy = 'build'`, the
source archive with its `sha256`, and `impure` when a `run` step for that
platform uses the network. oku builds nothing for it. The `vendor_sha256`
depends on the language:

| `vendor` | Pinned from another machine |
|---|---|
| `go`, `cargo` | Yes. They download the same files on every platform, so the digest of the build on your machine holds for the others. |
| `npm` | Yes. npm installs the packages of the platform that oku names, so oku downloads those of the other platform into a temporary directory, hashes them and keeps nothing. It runs none of their scripts. This needs a build on your machine, and a manifest with no `run` step before the `npm` step. |
| `pip` | No. pip picks packages by platform, so the digest comes from the first build on that platform. `oku sync --locked` fails there until a machine of that platform has built the package and you have committed the lock. |

The digest of a `go` or `cargo` step only holds when no vendor step of the
manifest has a `when`.

A lock from an older oku may hold build entries with fewer pins. `oku sync`
adds what is missing and keeps the pins that are there. It builds nothing for
that. The one exception is the vendor digest of your own machine. oku records
it beside the build in the store, a build from an older oku has none, and only
a new build gives it.

- A package may have no artifact and no build for some of the named
  platforms, such as a macOS app or a tool with no Windows release. `oku add`
  pins it for the others, writes a `when` that leaves those platforms out, and
  says so:

  ```
  rectangle has no artifact or build for linux-amd64-glibc, windows-amd64,
  so its entry in ~/.config/oku/oku.toml says when = { os = "darwin" }
  ```

  When your own machine is one of them, `add` pins the package for the others
  and installs nothing. `add` fails only when the package has nothing for
  your machine or for any `[lock]` platform.
- When a new version drops a platform that the entry's `when` matches,
  `oku update` narrows the `when` the same way. It never widens one, because
  you may have narrowed it on purpose. When a new version gains a platform
  that `when` leaves out, it tells you, and you can widen the `when` yourself.
- `oku sync` never edits `oku.toml`. It installs the rest of the list, does
  not install or pin the package on the platforms that have no artifact or
  build, and then fails with the line to write:

  ```
  rectangle has no artifact or build for linux-amd64-glibc, so sync did not install it
  change its line in ~/.config/oku/oku.toml to
    rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
  ```

  For a package from an included list, it names that list and the `when` its
  entry needs there.
- Without `[lock]`, `add` and `update` pin your own platform only, in a
  [project](projects.md) as in the global list. Name the platforms of the
  machines that share the lock, and oku does the work for those and no more.
- Only the `[lock]` of your own `oku.toml` counts, not one in an included list.
- oku also pins a package whose `when` leaves out your own machine, with its
  deps, for the platforms that `when` matches. It installs none of it and
  prints `patchelf 0.18.0, pinned and not installed on darwin-arm64`. The
  list without `[lock]` pins the host alone, so there the first machine
  that `when` matches pins the package.

A machine whose platform is missing from the lock adds its own entry the first
time it runs `oku sync`, and does not change the other entries. Commit the lock
back after that.

In CI, run `oku sync --locked`. It fails when the lock does not pin a package
for the runner's platform, and it never changes the lock, see
[oku sync](commands.md#oku-sync).

In GitHub Actions the action of this repo does that in one step:

```yaml
- uses: y3owk1n/oku@main
  with:
    path: .            # the directory of the oku.toml, the default
```

It installs the oku release that `version` names, unless an oku is already on
`PATH`. Without `version`, an action used by a release tag, as in
`uses: y3owk1n/oku@v1.2.3`, installs that release, and `@main` installs the
newest. So a pinned tag pins oku too, and a bot that bumps the tag moves both. Then it runs `oku sync --yes --locked` in `path`. `args`
replaces `--locked`. The later steps of the job find the programs of the global
profile and of the project on `PATH`, and the variables of their `[env]` in the
environment. It works on Linux, macOS and Windows runners. It keeps the store
and the downloads in the Actions cache, keyed by `oku.lock`, unless `cache` is
`"false"`.

Platform entries for a package are cleared when `oku update` accepts a changed
manifest, because they described the old one. Each machine adds its entry
again on its next sync.

## A new machine

Publish `oku.toml` and `oku.lock` side by side in a repo, then run one command
on the new machine:

```
$ oku sync github:you/machines
adopted github:you/machines with 23 locked packages
profile now holds 23 packages
```

oku reads both files at the same commit. `sync` then reads `github:` and `git+`
manifests at their locked commits and checks every download against the locked
sha256, so the new machine gets the same versions, the same bytes, and the same
store paths.

The new machine's own `oku.toml` holds one line, `include = ["github:you/machines"]`.
The published list decides what every machine installs:

- `oku update` on any machine picks up what the published list gained or lost.
- `oku add` on a machine writes to that machine's own `oku.toml` only. To share
  a package, add it to the published list.
- To publish new versions, run `oku update` on the machine whose config
  directory is the repo, and commit the lock.

`oku sync <list-ref>` reads the published lock once, when it sets the machine
up. Afterwards `oku update` on that machine resolves packages fresh, so it can
install newer versions than the published lock. To keep several machines on exactly the
published lock over time, clone the repo to `~/.config/oku/` on each one, and
run `git pull` and `oku sync` to update them together.

Packages in a published list must use URL or repo refs. A local path in it is
an error on every other machine.

You can also copy the two files into `~/.config/oku/` yourself, or symlink that
directory from a dotfiles repo, and run `oku sync` with no argument.

## Moving versions

Versions stay fixed until you run `oku update`. It reads every ref fresh and
rewrites the lock. `oku update <name>` does it for one package. See
[Commands](commands.md#oku-update).

If an update breaks something, `oku rollback` switches the profile and
`oku.lock` back to the generation before it. Commit the lock again afterwards
if you keep it in version control.
