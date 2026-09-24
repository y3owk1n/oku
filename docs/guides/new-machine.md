# Set up a new machine from your machines repo

You end up with one git repo that describes every machine you use. It lives at
`~/.config/oku` on each of them, and a new machine is a clone and one
`oku sync`.

## Create the machines repo

oku reads its [list](../how-oku-works.md#list) and
[lock](../how-oku-works.md#lock) from `~/.config/oku`, or
`$XDG_CONFIG_HOME/oku`. Make that directory a git repo:

```
~/.config/oku/
  oku.toml       what you want: packages, files, settings
  oku.lock       what oku resolved. oku writes it.
  files/         configs that [files] places
  packages/      manifests of your own, if any
  secrets/       sops or age files, if you use secrets
```

A small `oku.toml` to start from:

```toml
[packages]
ripgrep = "github:BurntSushi/ripgrep"
fd = "github:sharkdp/fd"
gh = "./packages/gh.toml"

[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }

[defaults."com.apple.dock"]
autohide = true
```

Run `oku sync` once so that oku writes `oku.lock`, then commit both and push:

```sh
cd ~/.config/oku
git init
git add oku.toml oku.lock files packages
git commit -m "my machines"
git remote add origin git@github.com:you/machines.git
git push -u origin main
```

Keep every path in the list relative, such as `./packages/gh.toml`. A
relative path starts at the directory of the list that holds it, so it works in
any clone. oku writes a path inside the list's directory as a relative path in
`oku.lock` too, and one outside it as its absolute path.

Commit only encrypted secrets. See [Secrets](secrets.md) for the key and the
files.

## Set up a new machine

1. Install oku:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
   ```

   On Windows:

   ```powershell
   irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
   ```

2. Add the line the installer prints to your shell's startup file, and open a
   new shell. For zsh:

   ```sh
   echo '[ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"' >> ~/.zshrc
   exec zsh
   ```

   [Getting started](../getting-started.md) has the line for bash, fish and
   PowerShell.

3. If your list has secrets, copy your age key to
   `~/.config/sops/age/keys.txt`, or `%APPDATA%\sops\age\keys.txt` on
   Windows. oku never creates a key, and without it the
   sync stops before it changes anything.

4. Give oku a GitHub token, or log in with `gh`. Without either, GitHub allows
   60 requests an hour, which a first list of a dozen packages can use up:

   ```sh
   export GITHUB_TOKEN=<a token with no scopes>
   ```

5. Clone the repo into the config directory. A new machine has no SSH key yet,
   so use the https URL:

   ```sh
   git clone https://github.com/you/machines ~/.config/oku
   ```

   On Windows the directory is `%APPDATA%\oku`.

6. Look before you apply. A dry run checks everything and changes nothing:

   ```sh
   oku sync --dry-run
   ```

7. Apply it:

   ```sh
   oku sync
   ```

   Add `--yes` to approve the build steps of packages that build from source,
   instead of answering a prompt for each.

`oku sync` installs every package at the version, the commit and the sha256
that `oku.lock` pins, so the new machine gets the same bytes and the same
store paths as the machine that wrote the lock.

When the lock has no entry for the new machine's platform, oku resolves each
package for it and adds that entry. It does not change the entries of other
platforms. Commit the lock back afterwards, or name the platform in
[`[lock]`](#pin-every-platform-in-one-lock) so that no machine has to.

## Keep machines in step

Change the setup on any machine, then share it through git:

```sh
# on the machine where you made the change
oku add github:junegunn/fzf      # or edit oku.toml, then oku sync
git -C ~/.config/oku commit -am "add fzf"
git -C ~/.config/oku push

# on every other machine
git -C ~/.config/oku pull
oku sync
```

Versions stay fixed until you run `oku update`. Run it on one machine, commit
`oku.lock`, and the others get the same versions on their next pull and sync.
`oku update fd` moves one package.

`oku rollback` switches `oku.lock` back to the generation before. Commit the
lock again afterwards, so that the other machines follow.

## Split the list with include

`include` merges other lists under yours. Use it to keep packages, files and
settings in their own files, or to share a base list between machines:

```toml
include = [
  "./lists/packages.toml",
  "./lists/files.toml",
  "./lists/macos.toml",
  "github:you/machines#base",
]

[packages]
ripgrep = "./packages/ripgrep.toml"
```

Each item is a [ref](../how-oku-works.md#ref) to a list:

| Ref | Reads |
|---|---|
| `./work.toml` | That file, relative to the including list. |
| `https://host/base.toml` | That URL. |
| `github:you/machines` | `oku.toml` at the root of the repo. |
| `github:you/machines#base` | `base.toml` at the root, else `lists/base.toml`. |
| `git+https://host/repo` | `oku.toml` at the root of the repo. |
| `git+https://host/repo#dir/base.toml` | That file in the repo. |

- Includes merge in order, so a later include overrides an earlier one.
- A package in your own `[packages]` overrides the same name from any include.
- An included list may include others, up to 8 levels deep. A list included
  twice, or one that includes itself, is an error.
- In a list from a repo or a URL, a relative path names a file in the same
  repo at the same commit, or the URL beside the list. An absolute path is an
  error, because it names a file on the author's machine, and so is a path
  that leaves the repo.
- oku uses only the `oku.lock` beside your own `oku.toml`, and only the
  `[lock]` of your own `oku.toml`.

A list that is a file on this machine is yours, like `oku.toml`. You edit it
and run `oku sync`. oku pins a list from a URL or a repo in `oku.lock`, at its
commit and sha256, and `oku sync` stops when its content changed:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

`oku update` with no names reads includes fresh, so oku installs their new
packages and removes the ones they dropped. `oku update <name>` keeps them pinned.

oku never edits an included list. `oku remove` refuses a package that only an
include declares, so remove it there or take the include out.

[Dotfiles](dotfiles.md#know-where-files-may-live) and
[OS settings](os-settings.md#know-the-limits) say which includes may hold
`[files]`, `[secrets]` and settings.

## Use one list for macOS, Linux and Windows

`when` limits a package or a `[files]` entry to matching machines:

```toml
[packages]
rectangle = { ref = "github:rxhanson/Rectangle", when = { os = "darwin" } }
patchelf = { ref = "github:you/recipes#patchelf", when = { os = "linux", libc = "glibc" } }
fd = { ref = "github:sharkdp/fd", when = [{ os = "darwin" }, { os = "linux" }] }
```

- The keys are `os`, `arch` and `libc`. A missing key matches anything, and
  any other key is an error. See [oku.toml](../reference/oku-toml.md) for the
  values.
- An array of tables matches a machine that any of them matches.
- `oku sync` does not install a package whose `when` does not match the
  machine. An entry for it in the lock from another machine stays.

`oku add --when` writes the `when` for you. Give it once per table:

```sh
oku add github:rxhanson/Rectangle --when os=darwin
oku add github:sharkdp/fd --when os=darwin --when os=linux
```

When `--when` leaves out the machine you run it on, `add` pins the package
for the `[lock]` platforms it matches and installs nothing, as `sync` does.

Settings tables need no `when`. oku skips `[defaults]` off macOS,
`[registry]` off Windows and `[dconf]` off Linux, see
[OS settings](os-settings.md).

## Pin every platform in one lock

A package's version, manifest and deps are the same on every platform. Only
the download differs. `[lock]` names the platforms to pin besides the machine
you run oku on:

```toml
[lock]
platforms = ["darwin-arm64", "linux-amd64-glibc", "linux-arm64-glibc"]
```

`oku add`, `oku update` and `oku sync` then pin every package for each of them,
from any machine. A Mac pins the Linux download, and the Linux machine installs
it with no change to the lock. The platform names are `darwin-amd64`,
`darwin-arm64`, `linux-amd64-glibc`, `linux-amd64-musl`, `linux-arm64-glibc`,
`linux-arm64-musl`, `windows-amd64` and `windows-arm64`.

Without `[lock]`, oku pins the machine it runs on and no more. Name the
platforms of the machines that share the lock, and oku does the work for those
alone.

oku never unpacks or runs a download for another platform. It takes the
checksum from the manifest, else from upstream's checksum file. With neither,
it downloads the file and hashes it, and tells you. See
[Security](../reference/security.md).

### A package that some platforms lack

A macOS app, or a tool with no Windows release, has nothing for some of the
platforms. `oku add` pins it for the others and writes a `when` that leaves
the rest out:

```
rectangle has no artifact or build for linux-amd64-glibc, windows-amd64,
so its entry in ~/.config/oku/oku.toml says when = { os = "darwin" }
```

- When your own machine is one of the platforms left out, `add` pins the
  package for the others and installs nothing. It fails only when the package
  has nothing for your machine or for any `[lock]` platform.
- oku pins a package whose `when` leaves out your machine, with its deps, for
  the platforms that `when` matches, and prints a line such as
  `patchelf 0.18.0, pinned and not installed on darwin-arm64`.
- When a new version drops a platform, `oku update` narrows the `when` the same
  way. It never widens one. When a new version gains a platform, it tells you,
  and you widen the `when` yourself.
- `oku sync` never edits `oku.toml`. It installs the rest of the list and then
  fails with the line to write:

  ```
  rectangle has no artifact or build for linux-amd64-glibc, so sync did not install it
  change its line in ~/.config/oku/oku.toml to
    rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
  ```

  For a package from an included list, it names that list and the `when` its
  entry needs there.

### Builds from source on other platforms

oku builds nothing for another platform. It pins the source archive and its
sha256. Whether it can also pin the packages that a build downloads depends on
the language:

| Vendor step | Pinned from another machine |
|---|---|
| `go`, `cargo` | Yes, when no vendor step of the manifest has a `when`. |
| `npm` | Yes, when the manifest has no `run` step before the `npm` step. It needs a build on your machine. |
| `pip` | No. The first build on that platform pins it. `oku sync --locked` fails there until a machine of that platform has built the package and you have committed the lock. |

See [the lock reference](../reference/lock.md) for the fields.

## Adopt a published repo instead

If you do not want a clone on the new machine, point `oku sync` at the repo:

```
$ oku sync github:you/machines
adopted github:you/machines with 23 locked packages
profile now holds 23 packages
```

oku reads the list and the lock beside it at the same commit, then writes a
global `oku.toml` that holds only `include = ["github:you/machines"]` and an
`oku.lock` that starts from the published one. The lock's name follows the
list's, so `oku.toml` pairs with `oku.lock` and `base.toml` with `base.lock`.
Without a lock beside the list, oku prints a notice and resolves every package
fresh.

This works only on a machine whose global `oku.toml` is missing or empty. On
any other machine oku refuses and tells you to add the ref to your `include`
instead. A list ref takes no `@version`. If the install fails, oku removes the
two files again, so the same command works once you fix the cause.

> [!WARNING]
> The published lock counts only once, at adoption. After that, `oku update` on
> the machine resolves packages fresh and can install newer versions than the
> published lock. `oku add` writes to that machine's own `oku.toml` only. To
> keep machines on exactly the published lock, use the clone described above.

To share a package with every adopted machine, add it to the published list.
To publish new versions, run `oku update` on a machine whose config directory
is the repo, and commit the lock. `oku update` on an adopted machine picks up
what the published list gained or lost.

You can also copy `oku.toml` and `oku.lock` into `~/.config/oku` yourself, or
symlink that directory from a dotfiles repo, and run `oku sync` with no
argument.
