# Troubleshooting

Find the symptom or the message you see, then follow the fix.

When a command fails, oku prints `oku: <reason>` on stderr and exits with
status 1. The lines after the first say what to do. `add`, `remove`, `sync`,
`update` and `rollback` change the machine all the way or not at all, so after
a failure your machine is as it was before the command.

Start with `oku doctor`. It checks the store, `PATH`, the shell hook, the
build sandbox, the profiles, your secrets setup, what `[host]` names and any
unfinished change, and names the fix for each problem:

```sh
oku doctor
```

## A program does not run after oku add

The shell cannot find the programs oku installed until the profile's `bin` is
on `PATH`. `oku add` says so when it is missing:

```
! to run it, add this line to ~/.zshrc, then open a new terminal:
  [ -x "$HOME/.local/bin/oku" ] && eval "$("$HOME/.local/bin/oku" hook zsh)"
```

Add that line, then open a new terminal. [Set up your shell](getting-started.md#2-set-up-your-shell)
has the line for bash, fish and PowerShell. When oku does not know your shell,
it prints `add <dir> to PATH to run it` instead, with the directory to add.

`oku doctor` reports the same problem as:

```
problem  ~/.local/share/oku/profiles/global/current/bin is not on PATH, so installed programs do not run by name. The hook line puts it there
```

Inside a [project](how-oku-works.md#project), a package goes to the project's
profile. oku prints `this project's programs are in <dir>`, and the hook puts
them on `PATH` once you run `oku allow`, see [Projects](guides/projects.md).

## A project's variables are not set

The hook sets a project's [`[env]`](reference/oku-toml.md#env) only while the
project is allowed and synced. Look for the one-line hint it printed when you entered:

- `` run `oku allow` ``: the project is not allowed, or its `oku.toml` changed
  since, and editing `[env]` changes it too.
- `` run `oku sync` ``: the project's profile is behind its `oku.lock`.
- `<NAME> is not set, <hint>`: the list requires `<NAME>`. Set it in your
  shell.
- `oku.toml: env.<NAME>: ...`: the `[env]` does not parse, and the error says
  why.
- `<file> does not exist, create it or give the file optional = true`: an
  [`[[env.file]]`](reference/oku-toml.md#envfile) is missing.
- `OKU_ENV=<env>, and <dir>/oku.<env>.toml does not exist`: unset `OKU_ENV`,
  or create the file.
- `<file> holds packages, and a list over oku.toml holds [env] alone`: move
  the packages to `oku.toml`.
- `decrypt <file>: ...`: a `secret = true` file did not decrypt. The rest of
  the line comes from age or sops, and
  [the secrets guide](guides/secrets.md#when-a-secret-cannot-be-decrypted)
  covers the causes.
- `<file>: line <n>: ...`: the `.env` file does not parse. oku sets none of
  its variables until you fix the line.

The hint shows once per directory. `oku env --dotenv` prints what the directory
sets now, and its hints again.

## Another program runs in place of oku's

A program of the same name earlier on `PATH`, such as `/usr/bin/rg`, runs
instead of oku's. `oku which` shows which one wins:

```
$ oku which rg
program  rg
package  ripgrep 15.2.0
path     ~/.local/share/oku/store/ripgrep-15.2.0-82921c938f26f8ff/pkg/rg
PATH runs /usr/bin/rg instead, because its directory comes first. `oku doctor` says how to fix that
```

Put the profile's `bin` before that directory, or remove the other copy. `oku doctor` names every hidden program in one line, because one
`PATH` change fixes them all:

```
problem  /usr/bin/rg runs in place of oku's rg, because /usr/bin is earlier on PATH. Put ~/.local/share/oku/profiles/global/current/bin before it, or remove the other copy
```

The shell hook moves oku's directories to the front of `PATH` even when they
are there already. A login shell that tmux starts puts the system's
directories first, and the hook puts oku's back in front.

For a program oku did not install, `oku which` says so:

```
$ oku which ls
oku: ls is not from oku, PATH runs /bin/ls
`oku search ls` looks for a package that provides it
```

## A program fails with No such file or directory

bash and zsh remember where they found each program. A shell that ran a
program before `oku remove` can keep using its old path after you install
another copy:

```
bash: ~/.local/share/oku/profiles/global/current/bin/repomix: No such file or directory
```

The hook clears the remembered paths before each prompt. The hook of an older
oku does not, so in a shell that loaded it, run `hash -r` in bash or `rehash`
in zsh, or open a new terminal.

## GitHub rate limit reached

```
oku: GitHub rate limit reached, set GITHUB_TOKEN to raise it
```

GitHub allows 60 API requests an hour without a login. Set `GITHUB_TOKEN`, or
log in with the `gh` CLI, which oku uses when the variable is not set:

```sh
export GITHUB_TOKEN=$(gh auth token)
```

A different message means too many requests in a short time:

```
oku: GitHub asked oku to slow down, try again in 60 seconds
```

Wait that long and run the command again. See
[Give oku a GitHub token](guides/add-packages.md#give-oku-a-github-token).

## oku picked the wrong file of a release

For a repo with no manifest, oku picks a release file for your machine. When
it picks wrong or cannot pick, name the file with `--asset` and the program
with `--bin`. See [Fix a wrong pick](guides/add-packages.md#fix-a-wrong-pick-with---asset-and---bin).

| Message | Fix |
|---|---|
| `no release asset fits this machine (darwin-arm64)`, then the files of the release | No file name says your OS and CPU. Pass one of the listed files to `--asset`. `oku add`, `oku run` and `oku shell` all take it. |
| `release <tag> of <repo> has no assets` | The newest release holds no files, which happens when a project publishes its downloads on its own site. Name an older release that has them, as `github:owner/repo@1.2.0`, or write a manifest. |
| `no release asset fits linux-amd64-glibc`, with a platform that is not yours | oku pins the package for a `[lock]` platform, and no file name says that OS and CPU. Leave the platform out of the package's `when`, or write a manifest. |
| `cannot tell which file is the program, executables found: ...` | The download holds several programs and none has the repo's name. Pass one to `--bin`. |
| `no file in it is executable` | Write a manifest, see [Publish a manifest](guides/publish-a-manifest.md). |
| `<file>: it is a setup program, which oku does not run` | The release file for your machine is an Inno Setup or NSIS installer. Pass another file to `--asset`, try `scoop:` or `winget:`, or write a manifest. |
| `it chose the asset <file> for this machine` | The install failed. The next lines list the other files that fit, and the `oku add --asset` command that picks one. `--verbose` adds the manifest oku inferred. |
| `--asset "<glob>" names 0 assets for <platform>, want one of: ...` | The glob matches no file that fits your machine. Pick a name from the list. |
| `--asset and --bin apply when oku infers a manifest, and <ref> has one` | The ref has a manifest, so these flags do nothing. Drop them. |
| `--asset and --bin do not apply, <ref> ...` | An `npm:`, `pypi:`, `go:` or `cargo:` ref. The registry names the download. |
| `--asset and --bin do not apply, <ref> names its downloads and programs` | A `cask:`, `scoop:`, `aqua:` or `winget:` ref. The recipe names the download. |
| `the cask runs an installer that makes its files`, `the manifest runs an installer` | An installer makes the files, and oku runs none. Write a manifest for the app. |
| `the cask uses the <kind> stanza, which oku does not place` | The cask installs something oku has no place for, such as a preference pane. |
| `the cask installs a kernel extension` | A kernel extension works only where macOS loads it, which oku does not do. |
| `its download needs a pre_install script to unpack it`, `its installer script unpacks the download` | A script of the Scoop manifest makes the files. Write a manifest for it. |
| `its programs come from <part> and <part>, parts of a package` | The `.pkg` installs its parts into one folder, which oku does not do. Write a manifest for it. |
| `open <url> to find what it installs` | oku opens a `.pkg` on macOS only. Translate the cask on a Mac, or write a manifest. |
| `the aqua registry has no entry for it` | The registry lists the repo under another name, or not at all. Try `github:owner/repo`. |
| `oku has no match for {{...}}` | The aqua entry uses a template function oku cannot translate. Write a manifest for it. |
| `winget has no such package` | Check the identifier, as `winget search` shows it, such as `jqlang.jq`. |
| `its <type> installer runs when it installs` | Every installer of the winget package for that arch is a setup program. Try `scoop:` or write a manifest. |
| `holds no program <name>.exe, and winget names no command` | The winget package's MSI holds several programs, and neither its name nor its `Moniker` is one of them. Write a manifest that names the program. |
| `<source>'s answer ... lacks <field>, which oku needs, and its format may have changed` | The source changed its format, and this oku reads the old one. Update oku. What `oku.lock` pins still installs. |
| `GitHub no longer serves version <date> of its API` | GitHub retired the API version this oku asks for. Update oku. |
| `the aqua registry is at <tag>, whose format this oku does not read` | The registry moved to a new major. Update oku. |
| `no bucket of main or extras has it` | Name the bucket, as in `scoop:versions/<name>`. |
| `<ref> has no manifest and no release to infer one from` | The repo has neither. Write a manifest, or point at one elsewhere. |
| `there is no repo at <url>` | The name is wrong, or the repo is private. Check the name, or set the token of its host, see [tokens per host](reference/refs.md#tokens-per-host). |

## oku cannot find the package or the version

| Message | Fix |
|---|---|
| `there is no file named ripgrep here` | A bare word is a local file. Write a full ref, such as `github:BurntSushi/ripgrep`, or `alias/name` for a [source](guides/add-packages.md#use-a-short-name-for-a-collection). `oku add --help` lists every form. |
| `<alias> is not a source and <arg> is not a file, see oku source list` | No source has that alias. `oku source list` shows yours. |
| `... has no version 99.0, the newest are 10.5.0, ...` | Pick a version from the list, or drop `@version`. |
| `the manifest provides version X, not Y` | The manifest fixes one version, and `@version` asked for another. |
| `<name> has no artifact for darwin-arm64` | The manifest has no download for this machine and no build. |
| `<name> has no [build], so it cannot be built from source` | Drop `--from-source`. |
| `fetch git+https://...: no such repo, or git cannot read it without a login` | Check the URL. For a private repo, give git a login, such as a credential helper or an SSH key and a `git+ssh://` ref. |
| `download <url>: server returned 404 Not Found` | The manifest names a file that the release does not have, often for an older `@version`. `oku add <ref> --plan` finds this before anything installs. |

## The manifest changed since oku.lock was written

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

`oku sync` checks each manifest against the hash in `oku.lock` before it
downloads anything. A local file or a URL has no commit to pin, so an edit
shows up here. Read what changed, then run the `oku update` it names. When
several packages drifted, one error lists them all and ends with one
`oku update` that names each.

An included list works the same way:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

`oku update` then shows what changed in the list and asks. Without a terminal
it stops:

```
oku: include github:you/machines#base changed, and this is not a terminal
pass --yes to take the change
```

Read the lines it printed, then run `oku update` on a terminal, or
`oku update --yes` to take them. See
[Changed included lists](reference/security.md#changed-included-lists).

## Checksum mismatch

```
oku: checksum mismatch for <url>: expected <digest>, download is <digest>
```

The download is not the file the manifest or the lock pinned. oku deleted it
and installed nothing. Run the command again in case the download broke on the
way. If it fails again, the file on the server changed. Ask the project before
you trust it.

A checksum file upstream can also change:

```
oku: ripgrep: checksum changed: upstream publishes sha256 <new>, oku.lock pinned <old>
run `oku update ripgrep` to accept the new checksum
```

oku cannot tell an upstream that replaced a release file from an attack. Check
with the author before you run the `oku update`.

## The vendored packages changed

```
oku: the vendored packages changed: oku.lock pinned <digest>, this build downloaded <digest>
run `oku update tool` to accept what it downloads now
```

A build downloads its dependencies in a vendor step, and the lock pins a digest
of them. This time it got different ones, and oku installed nothing. `oku update`
accepts the new set.

## The signing key changed

```
oku: foo: oku.lock pinned the signing key RWTr8ko..., and the manifest now has the signing key RWSwtYz...
if the developer announced this change, run the command again with --accept-key
```

A new key is what someone who took over the repo would publish. Check with the
developer first. See [Security](reference/security.md).

## A Sigstore signature does not check out

```
oku: tool: signature check failed: the bundle at <url> does not show that you/tool/.github/workflows/release.yml signed <file>: ...
```

The manifest names a `signer_workflow`, and the signature beside the release,
or its attestation, came from another workflow, another repo or another tag,
or does not cover the file oku downloaded. The end of the line says which.
When it ends in `rekor.sigstore.dev holds no entry of this signature`, nobody
recorded the signature in Sigstore's log, which a forged signature also lacks.
An error that names `the provenance at` comes from the same check for SLSA
provenance, whose builder must be one that slsa-verifier trusts.
oku installed nothing. A release that someone replaced looks like this, so ask
the developer before you work around it.

When the manifest names a new workflow, `oku sync` and `oku update` stop with
`oku.lock pinned the signer workflow ...`. Check with the developer, then pass
`--accept-key`. See
[Sigstore signatures](reference/security.md#sigstore-signatures-of-a-manifest).

## OSV lists a version as malicious

```
oku: npm:nx 21.5.0: OSV lists it as malicious in MAL-2025-41443
add another version with @<version>
```

The OpenSSF malicious packages list names that version, which usually means
someone published it from a stolen account. oku installed nothing. When a
build stops at a package its vendor step installed, the lines below the error
name it, and a version of the package you asked for from before or after the
incident usually avoids it. Read the
advisory at `https://osv.dev/vulnerability/<id>`, then add a version from
before or after the incident, such as `oku add npm:nx@21.4.0`. A package that
`oku.lock` holds stays at its locked version, and `update` says so. See
[Malicious packages](reference/security.md#malicious-packages).

## A package changed since oku installed it

```
oku: the files of 1 package changed since oku installed them
run `oku verify --repair`, then `oku sync`, which downloads them again and checks them against oku.lock
```

`oku verify` found a file in the store that differs from the one oku
installed. The lines above it name the file. Something wrote to the store, or
the disk corrupted it. Run `oku verify --repair`, then `oku sync`. See
[oku verify](reference/commands.md#oku-verify).

## The download has a weaker check

```
oku: tool: oku.lock checked the download for darwin-arm64 against the digest that its source publishes, and nothing states a digest for the new one, so oku would trust its first download
if the developer announced this change, run the command again with --accept-weaker-check
```

oku checked the version you had against something the new one lacks, such as a
checksum file the project stopped publishing or a `sha256` the manifest
dropped. Someone who replaced the release would remove it too.
Check the project's release notes or ask the developer, then run
`oku update tool --accept-weaker-check`. See
[weaker checks](reference/security.md#weaker-checks).

## oku sync --locked fails

`--locked` never changes `oku.lock`, for CI that cannot commit it back.

```
oku: ./oku.lock does not pin ripgrep for linux-amd64-glibc
run `oku sync` without --locked, and commit oku.lock
```

```
oku: ./oku.lock is out of date, and --locked does not change it
run `oku sync` without --locked, and commit oku.lock
```

Run `oku sync` on a machine, commit the lock, and push. To pin other platforms
from your own machine, name them in `[lock] platforms`, see
[A new machine](guides/new-machine.md). A `pypi:` package cannot be pinned
from another kind of machine, so a machine of that platform has to build it
once.

## The download is a web page, not a program

A URL, or the `url` of a manifest, gave oku an HTML page where it wanted a
file to install. Often the URL is the page of a repo, and the error names the
ref to use, such as `github:owner/repo` or `gitea:host/owner/repo`. A
download link that now leads to a login or an error page gives the same
error. Find the file's current URL.

`decompress: ...` means the download starts as gzip, bzip2, xz or zstd but
fails to decompress. A cut-off download does this, and so does an xz file
with an ARM64 or RISC-V BCJ filter, which oku does not read. oku reads the x86,
ARM, ARM-Thumb, PowerPC, IA-64, SPARC and Delta filters.

`the download is no archive and no program` means the file is neither an
archive oku unpacks nor a program. A single file that a runtime runs, such
as a script without `#!`, needs a `bin` table with `run` in its manifest.

## A package has nothing for this platform

```
rectangle has no artifact or build for linux-amd64-glibc, so sync did not install it
change its line in ~/.config/oku/oku.toml to
  rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
```

`oku sync` installed the rest of the list and never edits `oku.toml`. Paste the
line it gives. See [One list for several OSes](guides/new-machine.md).

## A build needs approval

```
oku: just 1.40.0 needs approval to run them, and this is not a terminal
pass --yes to approve
```

A build runs commands on your machine, so oku asks first. In a script there is
nobody to ask. Read the commands it printed above the error, then run it again
with `--yes`. Answering no to the question gives
`not approved, nothing was built`.

## A version is too new

oku takes only a release that is at least as old as the
[minimum release age](reference/security.md#minimum-release-age), one day by
default. When every version that fits is newer, it stops:

```
oku: newer than the minimum release age: every version of BurntSushi/ripgrep that fits came out less than 1d ago, and the first to pass is 14.1.1, from 2026-09-30 10:12
run the command with --min-release-age 0 to take the newest now
```

Wait until that time, or run the command again with `--min-release-age 0`.

Some sources give no release time, such as `git-tags`, so oku cannot check the
age. At a terminal it asks before it takes such a version. In a script it stops:

```
oku: lua 5.4.7: its source gives no release time, so oku asks before it takes it, and this is not a terminal: not taken
run the command with --accept-unknown-age, or set [lock] unknown_release_age = "allow"
```

With `unknown_release_age = "refuse"` it stops at a terminal too:

```
oku: lua 5.4.7: its source gives no release time, and [lock] unknown_release_age refuses such a version: not taken
run the command with --accept-unknown-age, or set min_release_age = "0" on lua
```

Run the command with `--accept-unknown-age`, or set `unknown_release_age` in
[`[lock]`](reference/oku-toml.md#lock).

## oku does not trust a first download

With `[lock]` `unverified = "warn"`, oku asks before it trusts a download that
nothing states a digest for. In a script it stops:

```
oku: nothing states a digest for the download of hello 1.2.0 for darwin-arm64, so oku asks before it trusts it, and this is not a terminal
run the command with --accept-unverified, or set [lock] unverified = "allow"
```

With `unverified = "refuse"` it stops at a terminal too. Ask the developer to
publish a checksum, or check the file yourself and run the command with
`--accept-unverified`. A package that `oku.lock` holds stays at its locked
version instead, and the line says `hello stays at 1.1.0`. See
[Trust on first use](reference/security.md#trust-on-first-use).

## Something the machine needs is missing

A package can name what it needs of the machine in `[host]`, such as Xcode
tools or a distro library. oku never installs these. The sync finished, and it
warns about each one that is missing:

```
! libgl, which glfw needs, is missing
  get it with `sudo apt-get install libgl1`
```

The second line is the command that installs it, or the text the entry gives
instead. Run it, then `oku doctor` to check again. A failed build also ends its
error with these lines. When an entry names a package only for another package
manager, oku says so and cannot check it:

```
! libgl names a package only for apt, so oku cannot check it on this machine
```

See [Name what the machine must have](guides/new-machine.md#name-what-the-machine-must-have).

## A build fails

| Message | Fix |
|---|---|
| `the build needs "<tool>", which is not on PATH` | Install that tool. oku does not install what a manifest `needs`. |
| `build.step[N] (run) failed`, then the end of its output | A build step failed. `--verbose` shows its whole output. |
| `no version satisfies ">=9", the versions found are ...` | A dep's version constraint matches nothing upstream. |
| `dependency cycle: ...` | Two manifests depend on each other. Fix one of them. |

## A git fetch fails with an ssh error

```
oku: tool: fetch the source: git ... fetch ...: exit status 128: Host key verification failed.
```

oku never lets ssh ask a question, since the progress line would hide it. So
ssh fails when it would ask to trust a new host or to unlock a key. A manifest
may name an `https://` repo, and your git config can still send it over ssh:

```ini
[url "git@github.com:"]
	insteadOf = "https://github.com/"
```

Change `insteadOf` to `pushInsteadOf`, so fetches stay on https and only pushes
use ssh. Or, to keep ssh, run `ssh -T git@github.com` once to trust the host,
and `ssh-add` to load the key into your agent.

## oku does not connect to a host

```
oku: ...: git.corp.example resolves to 10.0.0.5, a private address, and oku does not connect there. Add "git.corp.example" to [network] private in config.toml to reach it
```

oku does not connect to an address on your network unless you name the host.
When you trust it, add it to `config.toml`:

```toml
[network]
private = ['git.corp.example']
```

```
oku: ...: example.org is not in [network] allow in config.toml, so oku does not connect there
```

Your `[network] allow` list does not name the host. Add it, or `*.` and its
domain for every subdomain. A message that ends in `Redirected from` lists
the URLs that redirected to the refused host. See
[Where oku connects](reference/security.md#where-oku-connects).

## A project installs from sources you have not trusted

```
oku: /home/you/work/api installs from sources you have not trusted:
  github:acme for tool
run `oku allow` to trust them, or pass --yes to oku sync
```

The project's `oku.toml` names a source you have not trusted, and oku cannot
ask without a terminal. Read the project's `oku.toml`, then run `oku allow`
in it, or `oku sync` on a terminal to answer the question. In CI,
`oku sync --yes` trusts them. See
[Trusted sources](reference/security.md#trusted-sources).

## A build ran without the sandbox

```
tree was built without the sandbox, because this host does not let an unprivileged user set up namespaces (...)
its build commands could use the network and read your files
```

On Linux the sandbox needs unprivileged user namespaces. Ubuntu 24.04 and
later forbid them by default, through AppArmor, and so does a default Docker
container. To allow the sandbox on Ubuntu:

```sh
sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0
```

Put the same setting in a file under `/etc/sysctl.d/` to keep it after a
reboot. Windows has no sandbox at all. `oku doctor` shows the state as a
`note`.

## A file already exists

```
oku: ~/.config/nvim already exists and oku did not put it there
move it away, or take it out of [files]
```

oku never overwrites a file it did not write. Move the file away, then run the
command again. For an app or a font the message ends with
`so <package> cannot expose its app` instead. See [Dotfiles](guides/dotfiles.md).

A template that uses a name `[vars]` does not set stops with
`<name> is not set in [vars]`, before anything changes.

## Two packages provide the same program

```
oku: <a> and <b> both provide bin/<program>
```

Two packages ship a file of the same name, so oku refuses the second one. Keep
one of them.

## oku run does not know what to start

| Message | Fix |
|---|---|
| `<pkg> ships 2 apps, so name one with --app: <a>, <b>` | Pick one, as in `oku run <ref> --app <a>`. |
| `<pkg> ships no app and 3 programs, so run one with oku shell <ref> -- <program>` | The package is a set of command line programs, so name the one to run. |
| `<pkg> ships no app and no program to run` | The package only holds files, such as fonts or templates. There is nothing to start. |
| `<pkg> ships no app <name>, it ships <a>, <b>` | `--app` named an app the package does not have. |
| `oku run takes one ref, so use oku shell <ref> <ref> for several` | `run` starts one thing. A shell holds as many packages as you name. |

An app that `oku run` starts is in no menu and in no Spotlight result. A
permission you grant it may not carry over to a later install. Add it with
`oku add` to keep it.

## oku remove or oku add refuses

| Message | Fix |
|---|---|
| `fd is not installed, so nothing was removed` | `oku list` shows the names. |
| `fd is not in ~/.config/oku/oku.toml, so it comes from an include` | oku never edits an included list. Remove it there, or take the include out. |
| `... defines fd as a [packages.fd] table, edit it by hand` | oku edits one-line entries only. Edit that table yourself. |
| `fd is not in ~/.config/oku/oku.toml or its includes` | `oku update` got a name the list does not have. |

## oku sync with a list ref refuses

```
oku: ~/.config/oku/oku.toml already holds a list, so oku will not replace it
add "github:you/machines" to its include array and run `oku sync`
```

`oku sync github:you/machines` sets up a machine whose list is missing or
holds nothing, not even `[vars]`, `[files]` or a setting. On any other machine,
add the ref to `include` as it says. Inside a project it refuses
too, and `--global` sends it to your own list. A list ref takes no `@version`.
See [A new machine](guides/new-machine.md).

## The last change did not finish

When an oku process is killed halfway through a change, the next `add`,
`remove`, `sync`, `update` or `rollback` puts the machine back first:

```
the last change did not finish, so oku put generation 4 back
```

An `oku.toml` or `oku.lock` that you edited since stays as you left it, and oku
says so. Check that it holds what you want, then run `oku sync`.

Until then, `oku gc` and any `--dry-run` refuse to run and tell you to run
`oku sync` first. When oku cannot undo a step, for example because the service
manager refuses, it names the step. `oku doctor` then reports
`a change from generation 4 to 5 did not finish` until a later command
succeeds. Fix what it names, then run `oku sync`.

## oku waits for another oku process

```
waiting for oku process 4312 to finish
```

Only one oku changes the machine at a time. The command goes on when the other
one ends. On a terminal the line shows how long it has waited, and goes away
once the command goes on. Commands that only read, such as `list`, `generations` and `doctor`,
never wait. A killed oku leaves no stale lock, because the OS releases it.

## A disk image is mounted

```
oku: tool: unpack https://...: the disk image ... is mounted at /Volumes/Tool, eject it and try again
```

oku mounts a `.dmg` to copy its files, and macOS mounts a file at one place at
a time. When a killed oku process left an image mounted, the next install that
needs the image detaches it, and so does `oku gc`. oku leaves any other mount
alone, such as one you opened in Finder, so eject it and run the command again.
When another oku process is copying the same image, run the command again once
that process ends.

## A system extension is turned on

```
oku: /Applications/Tailscale.app has the system extension io.tailscale.ipn.macsys.network-extension turned on, and it keeps running when oku deletes the app
turn the extension off from the app, then run this again
```

Deleting an app does not turn its system extension off, and only the app can
do that. Turn it off from the app, for Tailscale with
`tailscale configure sysext deactivate`, and run the command again. See
[Install for every user](guides/system-wide.md#remove-system-scope).

## npm or PyPI packages need a runtime

Without `[runtimes] node`, an `npm:` program runs the `node` on your `PATH`,
and `oku add` says so. The add fails for a package that lists dependencies,
and on Windows for every `npm:` package:

```
oku: the npm package repomix lists dependencies, and only the npm of a node package installs them
set runtimes.node in ~/.config/oku/oku.toml to the ref of a package that provides node and npm
example: https://github.com/y3owk1n/oku/blob/main/examples/runtimes/node.toml
guide: https://github.com/y3owk1n/oku/blob/main/docs/guides/npm-pypi-go-cargo.md#name-the-toolchains-in-runtimes
```

A `pypi:` package always needs `[runtimes] python`:

```
oku: pypi:ruff needs python
set runtimes.python in ~/.config/oku/oku.toml to the ref of a package that provides python3
example: https://github.com/y3owk1n/oku/blob/main/examples/runtimes/python.toml
guide: https://github.com/y3owk1n/oku/blob/main/docs/guides/npm-pypi-go-cargo.md#name-the-toolchains-in-runtimes
```

Copy the example next to your `oku.toml`, name it in `[runtimes]`, and run the
add again. An npm package that an older oku installed without its
dependencies fails with `Cannot find package`. Name the node the same way and
run `oku update <name>`, which installs the package again with its
dependencies.

See [npm, PyPI, Go and cargo packages](guides/npm-pypi-go-cargo.md).

## A file you edited stops the next change

```
oku: ~/.ssh/allowed_signers changed since oku wrote it, and a sync would overwrite it
move the change into its source or into oku.toml, then delete the file
```

A `text` or `render` file that you edited in place, or a `secret` whose link
you replaced with a file of your own, stops `oku sync` and every other change,
so oku never writes over your edit. Move the change into the list, or into the
file the entry renders, delete the file, and run the command again. oku then
writes it from the list.

## Windows

- A home file that oku copied, rather than linked, and that you edited stops
  the next change with `<path> changed since oku wrote it, and a sync would
  overwrite it`. Move the change into its source or into `oku.toml`, delete
  the file, and run the command again.
- A build has no sandbox, and oku says so after every build.
- `oku self uninstall` removes `oku.exe` a few seconds after it returns.
- Windows Installer runs one installation at a time. oku unpacks one `.msi` at
  a time, and waits up to 3 minutes while another program, such as Windows
  Update, installs something. After that it stops with `another installation
  is still running`, so run the command again later.

See [Windows](guides/windows.md) for what else differs.
