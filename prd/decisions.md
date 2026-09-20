# Decisions

Each entry: the choice, then why. Reopen one only with new information.

## D1. No registry, no bundled sources

oku resolves refs the user supplies. Why: package ownership lives in userland.
A default source would become a registry.

## D2. TOML manifests, fixed step types, no scripting language

Why: nix-like capability without a DSL. A closed set of primitives can be
validated by a JSON Schema and behaves the same on every OS.

## D3. Private store plus profiles, per-user by default

The store root defaults to the user's data dir. `oku setup --system` creates a
shared root (`/opt/oku`, `%ProgramData%\oku`) once, with elevation. Why: no
root for the common case, never fights the native package manager, rollback is
a link flip. The shared root exists because non-relocatable builds embed store
paths, so only machines with the same root can share them (D12).

## D4. `run` is a shell string with an explicit shell on Windows

`run = "..."` executes under `sh` on unix. A `run` step reachable on Windows
must set `shell` (`pwsh`, `cmd`, `sh`) or lint fails. Why: shell strings are
what authors already write. An explicit Windows shell stops a unix one-liner
from silently misbehaving there.

## D5. One manifest covers many versions

URLs are templated on `{{version}}`. Versions come from `value` or discovery
(`github-releases`, `git-tags`). Checksums come from inline `sha256`, from
`sha256_url`, or are trusted on first use and pinned in the lock. Why: one
file per version puts a nixpkgs-sized burden on owners. The lock carries the
strictness instead.

## D6. Deps form a closure of store paths, so no solver exists

A dep is a ref with an optional version constraint. Each package resolves its
own deps and the lock pins the whole closure. Dependents reference deps by
store path and deps are never linked into a profile. Two packages may
therefore use different versions of the same dep. Why: version conflicts only
exist when everything shares one prefix. Without a shared prefix there is
nothing to solve, which is how nix avoids a solver too.

## D7. oku owns the link environment

For each dep, oku sets `PKG_CONFIG_PATH`, `CPATH`, `LIBRARY_PATH`,
`CMAKE_PREFIX_PATH` and rpath linker flags pointing at the dep's store path.
On Windows, shims prepend dep `bin` dirs to the process PATH for DLL lookup.
oku never rewrites binaries after the build. A package whose output embeds no
store path declares `relocatable = true`. Why: this is the part every manifest
would otherwise reinvent badly, and it is what makes source builds with
shared libraries work at all.

## D8. Global and per-project lists

The nearest `oku.toml` walking up from the working directory is the project
list. `<config>/oku/oku.toml` is the global list. Project profiles activate
through `oku hook <shell>`. `oku env` prints the same exports for direnv
users. Why: the store makes per-project profiles nearly free.

## D9. Activation never installs and never runs untrusted input

The hook only edits the environment, and only for an allowed `oku.toml` whose
profile matches its lock. Anything else prints a one-line hint. Why: a `cd`
must not download or execute code. This is direnv's trust model.

## D10. Trust is pinned by hash and by key

The lock pins manifest hash, git commit and artifact checksums. `sync` stops
on a mismatch until `update`. A manifest may declare a signing key. The lock
pins that key on first use, and a key change needs explicit approval.
Manifests with `run` steps need approval once per manifest hash. Why: with no
registry to vouch for content, the lock and the user's approval are the only
trust roots.

## D11. Builds are sandboxed, network only where output is hashed

Build steps run with no network and see only the source, deps, `needs` tools
and `{{prefix}}`. Network is available to `fetch` steps (sha256 required) and
`vendor` steps (output hash pinned in the lock). `network = true` on a `run`
step is an escape hatch that marks the package impure: shown in the approval
prompt and never pushed to a cache. Mechanism: user namespaces on Linux,
sandbox-exec on macOS. Windows has no equivalent, so it gets the scrubbed
environment only and `oku doctor` says so. Why: "same input, same machine" is
false if a build can read the host or the network at will.

## D12. Build cache is decentralized and signed

A cache is any static HTTP host or local directory the user lists. Entries are
keyed by store hash and signed. oku substitutes from a cache only when the
signer is in the user's trusted keys. Relocatable packages are shareable by
anyone. Non-relocatable ones are keyed with the store root. oku runs no cache
of its own. Why: source builds are slow, and a team or a developer should be
able to serve prebuilt results without oku becoming a registry.

## D13. Inferred manifests

`oku add github:owner/repo` with no manifest in the repo infers one from
release assets by matching os, arch and libc in asset names. oku prints the
inferred manifest, and the lock pins it. `oku manifest init --from` writes the
same inference to a file for the developer to commit. Why: the strongest form
of "publish once" is "publish nothing new".

## D14. Output kinds beyond bin

`app`, `font`, `service` and `[env]` are first-class. Apps and fonts are
exposed in the OS's per-user locations and tracked per generation. Services
map one definition onto launchd, systemd and the Windows task scheduler, user
scope by default. System scope needs `--system` and an explicit elevation
prompt. Why: these are the reasons people still need casks, font taps and
`brew services`.

## D15. Lists compose

`oku.toml` supports `include = [refs]` and a `when` selector on any entry. The
lock pins included lists by hash. `oku sync <ref>` bootstraps from a remote
list. Why: one personal repo can describe a mac laptop, a linux server and a
windows desktop, and a new machine is one command.

## D17. Everything oku writes is either in its own dirs or in a ledger

Files oku places outside its config, data, cache and shared-root directories
(apps, fonts, service units, launcher entries) are recorded in
`<data>/oku/exposed.toml` at the moment they are written. oku never edits
shell rc files, the registry or the system PATH. The hook line is printed for
the user to add. `oku self uninstall` replays the ledger in reverse, stops and
unregisters services, deletes oku's directories and then its own binary. Why:
uninstall is only easy if nothing was scattered in the first place. nix's
hard uninstall (volumes, daemon users, rc edits) is the counterexample. A
plain TOML ledger also means a person can finish the job by hand if the
binary is already gone.

## D18. A machine adopts a published list by including it

`oku sync <ref>` writes a global `oku.toml` that holds `include = ["<ref>"]`
and an `oku.lock` that starts from the lock beside the list. It does not copy
the list. It refuses when the global list already has content. Why: a copy
stops matching the repo as soon as either side changes. With an include, the
published list decides what every machine installs, and `oku update` picks up
what it gained or lost. Because the command refuses to overwrite, it cannot
delete a user's list.

Known limit: the published lock is read once, at adoption. A later `oku update`
on that machine resolves fresh and can install newer versions than the
published lock. Machines that must stay on one lock share the config directory
through git instead.

## D19. oku uses only the lock beside the user's own list

oku uses the `oku.lock` beside the user's own `oku.toml`. It pins every
included list by commit and sha256, and ignores a lock beside an included
list. `oku update` with no names reads includes fresh. `oku update <name>`
keeps includes pinned. Why: one lock per machine records everything that
machine installed in one file. Updating a single package must not add or drop
other packages as a side effect.

## D20. oku.toml is edited as text, oku.lock is rewritten whole

`add` and `remove` change one line of `oku.toml` and leave comments, ordering
and other tables unchanged. `oku.lock` is regenerated with packages sorted by
name. Why: the user writes the list and oku writes the lock. Stable lock bytes
keep diffs small. A package written as a `[packages.<name>]` table is read but
never edited.

## D21. github refs use https, git+ refs use git

A `github:` ref resolves the default branch to a commit through the GitHub API
and reads files at that commit from raw.githubusercontent.com. A `git+` ref
shells out to `git` for a depth-1 fetch. Why: a fresh machine may have no git,
and `github:` is the common case. Other hosts share no HTTP API, so `git` is
the only portable way to read them.

## D22. Rollback restores the lock and never edits the list

Each generation stores a copy of `oku.lock`. `oku rollback` switches the
`current` link and writes that copy back. It writes no new generation, so
later generations stay reachable by number. It does not edit `oku.toml`, and
it prints a notice when the list names a package the generation lacks. Why: a
rollback that only switched the profile would be undone by the next `sync`,
because the lock would still hold the newer versions. The list is the user's
file, so oku reports the disagreement and leaves the edit to them.

## D23. gc deletes only unused store paths, and only --keep deletes generations

`oku gc` deletes only store paths that no generation uses. Generations are
deleted only by `--keep N`, which keeps the newest N and the active one. Why:
every generation stays a working rollback target until the user says
otherwise. A gc that deleted generations by default would remove rollback
targets without the user asking.

## D24. Versions are ordered by their numbers, and the tag is kept

Discovered tags become versions by cutting `strip_prefix`. A tag that then
does not start with a digit is ignored. Versions compare by dot-separated
numbers, and a `-` suffix sorts before the same version without one. GitHub
drafts and prereleases are skipped. The lock stores the tag beside the version,
and `{{tag}}` expands to it. Why: many upstream tags are not valid semver, so a
strict parser would reject real projects. Release URLs often contain the tag
and the version in different places, and keeping the tag in the lock lets
`sync` build the URL without asking upstream again.

## D25. An inferred manifest is printed, installed without a prompt, and stored

`oku add github:owner/repo` on a repo with no manifest prints the manifest it
inferred and installs from it without asking. The lock stores the full manifest
text. `sync` installs from that text and never infers. Only `update` infers
again. Why: a prompt would break scripted installs, and printing gives the user
the same information. Inference is a heuristic that will change between oku
versions, so re-running it on another machine could give a different manifest
for the same lock. Storing the text keeps "same input, same machine" true.

## D26. An alias expands before it reaches a list

`oku add core/ripgrep` writes the full ref to `oku.toml` and `oku.lock`. Lists
never hold aliases. A path that exists is used before an alias. Why: aliases
live in one user's `config.toml`. A published list that held them would fail on
every machine that lacks the same alias, which breaks `oku sync <list-ref>`.

## D27. add is lenient about keys, lint is strict

`oku add` ignores manifest keys it does not know. `oku manifest lint` decodes
against the whole schema and rejects them. Why: a manifest written for a newer
oku must still install on an older one. The author is the person who can fix a
misspelt key, so the strict check is a command the author runs.

## D28. A build installs straight into its final store path

`{{prefix}}` is the final store path, and a build writes into it directly. The
source is unpacked in a temporary directory outside the store. oku writes
`oku-meta.toml` last, and a store path without it is a crashed build that oku
deletes before it builds again. Why: build systems write the install prefix
into the files they produce, so building elsewhere and renaming would leave
wrong paths inside the package. Artifacts keep the unpack-then-rename install,
because no file inside them contains its path.

## D29. Approval is per manifest hash and per machine

Before the first build of a manifest with `run` steps for the host, oku shows
the commands and asks. The answer is stored by manifest sha256 in
`<data>/oku/trust/approvals.toml`. Without a terminal oku refuses unless `--yes`
is passed. A dep that builds asks for itself. `sync` on a new machine asks
again. Why: a changed manifest is different code, so it needs a new answer. An
approval stored in a published lock would let one person approve commands for
everyone who adopts the list. The executor and the prompt were merged in one
change so that no commit on main runs manifest commands unasked.

## D30. Each package pins its own deps, nested in the lock

The lock stores deps as `[[package.dep]]` under the package that needs them,
recursively. Generations record each package's closure of store paths, and gc
treats those as used. Why: D6 lets two packages use different versions of one
dep, so a flat list keyed by name cannot hold them. Nesting also makes
`oku update <name>` re-resolve exactly that package's deps and nothing else.

## D31. Runtime library lookup comes from the linker, not from rewriting

On Linux oku sets `LD_RUN_PATH` to the deps' lib directories, and the GNU linker
records them in what it links. On macOS a shared library must record its own
absolute install name, which build systems do when given `{{prefix}}`. oku
never edits a binary after the build. Why: this refines D7. The linker reads
`LD_RUN_PATH` itself, while `LDFLAGS` only works when the build system passes
it on. Rewriting a binary after the build can corrupt it, and an absolute
install name is already what a correct macOS library has.

Known limit: the GNU linker ignores `LD_RUN_PATH` when the build passes its own
`-rpath`. Such a build adds the dep paths itself with `{{dep.<name>.prefix}}`.

## D32. The build environment is scrubbed before the sandbox exists

A `run` step gets `PATH` with the deps' `bin`, the directories of the `needs`
tools, then `/usr/bin` and `/bin`, a temporary `HOME` and `TMPDIR`, the link
environment, oku's variables and the step's `env`. Why: it makes a missing
`needs` entry fail on the author's machine instead of the user's. `/usr/bin`
and `/bin` stay because `sh`, `make` and the system compiler driver are there
on most hosts. Step 6 replaces this with the sandbox and revisits B54.

## D33. Manifest commands are sandboxed, and a host with no sandbox still builds

`run` and `vendor` steps run in the sandbox. `source`, `fetch` and artifact
downloads are made by oku itself, outside it, and checked against a digest.
macOS uses a `sandbox-exec` profile that denies the network, file contents and
directory listings under the home directory, and every write outside the
build's own directories. Linux uses user, mount and network namespaces, and a
hidden `oku sandbox-init` command that mounts a tmpfs over the home directory
and binds the store and tool directories back. The step sees uid 0, mapped to
the real user. Where no sandbox exists, oku builds with the scrubbed
environment and prints a warning that names the reason. Why: the user already
approved the commands, so refusing to build would only make oku unusable in
containers and on Windows. The warning tells the user that the build had fewer
protections. This completes D11 and replaces the interim state in D32, except
that `/usr/bin` and `/bin` stay on `PATH`.

Known limit: the Linux sandbox hides the home directory and the network. It
does not make the rest of the filesystem read-only, which the macOS profile
does.

## D34. Vendor output is pinned per platform and checked after the build

A `vendor` step runs the language's own tool with the network on, and oku
hashes the directory it fills. The lock pins one digest per platform. `sync`
runs the vendor steps again, and a different digest deletes the package and
fails. `update` accepts the new digest. A vendor step needs approval and does
not make the package impure. Why: what `pip` and `npm` download depends on the
platform. The check runs after the build because the vendored files decide what
was compiled, so a package built from other files must not be kept. Checked
output is what separates a vendor step from `network = true`, which oku cannot
check.

## D35. A build may read rustup files in the home directory, and nothing else

When a build needs `cargo` or `rustc` and a rustup directory exists, oku passes
`RUSTUP_HOME` and `RUSTUP_TOOLCHAIN` through and makes that directory readable
in the sandbox. `CARGO_HOME` stays in the build's temporary home. Why: rustup is
the installer the Rust project recommends. It makes `cargo` a proxy whose
toolchain is in `~/.rustup`, which the sandbox hides, so without this a Rust
build fails for those users. The general
answer is a toolchain package as a dep, and this exception exists until such
packages are common.

## D36. manifest test uses a throwaway store and the real cache

`oku manifest test` installs into a temporary data directory and deletes it
afterwards. It shares the user's download cache. It tests the platform it runs
on. Why: the author's store, profile, list and lock must not change. The cache
is content-addressed, so sharing it cannot change a result and saves downloads.

## D37. A project is the nearest oku.toml, and oku says when it uses one

Commands that read or change a list use the nearest `oku.toml` at or above the
working directory. The config directory holds the global list and is never a
project. `--global` overrides. A project profile is named from a hash of the
project's path, and the store, cache, sources and build approvals stay shared.
oku prints `project <dir>` on stderr whenever it acts on one.
`oku sync <list-ref>` is refused inside a project. Why: git and direnv find
their root the same way, so users already expect it. The stderr line exists
because the same command now changes different files depending on where it
runs, and a user must be able to see which. Adoption writes the global list,
so oku refuses it inside a project.

## D38. The hook keeps its own state and reads only the generation

The hook stores what it applied in `OKU_HOOK_PATH`, `OKU_HOOK_KEYS` and
`OKU_HOOK_HINT`, and each run undoes exactly that before it applies what holds
now. A hint is printed once per reason. Each package's expanded `[env]` is
stored in the generation, and "behind its lock" is a byte comparison between
`oku.lock` and the copy in the active generation. Why: D9 forbids the hook from
installing, using the network or running manifest code, so everything it needs
must already be on disk in a form it can read without a manifest. Keeping state
in the environment lets the hook undo exactly what it applied, including after
the project's packages changed while the user was inside it.

## D39. An allow belongs to one oku.toml content

`oku allow` records the project directory and the sha256 of its `oku.toml`. Any
edit, including one that arrives by `git pull`, stops the hook until the user
allows again. Why: the allow exists because a cloned repo could put its own
`make` or `git` ahead of the user's. An allow that stayed valid after edits
would let a later commit do that to someone who only approved the first
version.

## D40. A package's [env] cannot set variables that control other programs

`[env]` may not set `PATH`, `HOME`, `SHELL`, `USER`, `IFS`, `ENV`, `BASH_ENV`,
`PS1`, `PROMPT_COMMAND`, or any name that starts with `LD_`, `DYLD_` or `OKU_`.
When two packages set one variable, the package whose name sorts last is used,
and a project package is used over a global one. Why: installing a prebuilt
package needs no approval, so its `[env]` must not be a way to run code in every
shell the user opens.

## D16. Installers are unpacked, never executed

oku understands tar, zip, 7z, dmg, pkg, msi, deb, rpm and AppImage. It reads
7z, deb and rpm in Go, so they unpack on any OS. It reads dmg and pkg with
`hdiutil` and `pkgutil`, so those unpack on macOS only, and msi with `msiexec`
on Windows only (D52). Why: running a vendor installer writes outside the store,
needs root, and cannot be rolled back.

## D41. One ledger, made to match the active generation

Every file oku writes outside its directories is in `<data>/oku/exposed.toml`,
written before the file exists. The ledger is not part of a generation. A
generation records packages, and oku derives the wanted files from the active
one and syncs the ledger to it. Each kind has a handler: apps and fonts are
copies, a service goes through the service manager. Why: one file is what
`self uninstall` replays, and a derived ledger cannot disagree with the
generation after a crash. Only the global profile exposes anything, because an
app, a font or a service is visible to the whole account, not to one directory.

oku copies apps and fonts. Finder, Spotlight and the font services do not treat
a symlink into the store as installed.

## D42. A service that is not enabled is installed and stopped

Installing a package never starts its service. `service = true` on the list
entry runs it now and at login. On macOS a service that is not enabled keeps its
plist in `<data>/oku/services`, because launchd loads everything in
`~/Library/LaunchAgents` at login. `oku service start` and `stop` last for the
session. Why: a user does not expect a daemon to start because a package
arrived as a dep, and the list is the one place that says what runs.

## D43. System scope is in the list, and only a flag elevates

`system = true` on a list entry puts that package's apps, fonts and services in
system scope. `--system` on `add`, `sync`, `update` and the `oku service`
actions is the only thing that makes oku call `sudo`. Without it oku leaves
system scope unchanged and lists what is pending, also on `remove` and
`rollback`. Why: the list must describe the whole machine, and a sync from a
script must never stop at a password prompt.

oku elevates one file at a time by running the hidden `oku system-apply` through
`sudo`. That command refuses a target outside the system directories. Why: the
ledger records each file before it exists, and the part that runs as root stays
small. When oku runs as root it does not call `sudo`.

## D44. The shared root belongs to the user

`oku setup --system` creates `/opt/oku` with `sudo` and makes the current user
its owner. It is the only elevated step, and `store_root` in `config.toml`
records it. Profiles, the ledger and approvals stay in the data directory.
Packages already installed stay in the old store until `sync` installs them
under the new root, and `gc` covers both stores. Why: a root-owned store would
make every install need `sudo`, and D3 wants the shared root only for
equal store paths across machines, not for sharing between users.

## D45. Declining elevation on uninstall still removes user scope

`self uninstall` asks one second question for everything that needs `sudo`.
After a no it removes user scope, empties the shared root, which the user owns,
and prints the `sudo` commands for what is left. `--yes` alone counts as no.
Why: uninstall must not fail half way because the user cannot enter a password,
and oku is gone afterwards, so the printed commands are the only way left.

## D46. A cache entry is one signed archive named after the store path

An entry is `<store name>.tar.zst` beside `<store name>.tar.zst.minisig`, in a
directory or on a static http(s) host. The archive's top directory is the store
name, so a signed entry cannot be served under another package's name. Entries
carry no owner and no file times. Why: a cache must work from any file host
with no server code, and minisign signatures verify with a tool that already
exists.

`oku cache push` writes into a directory and never uploads. It skips packages
that are plain downloads and refuses impure ones. The secret key is
`<config>/oku/signing.key` and has no password. Why: upload differs per host,
and a push must run in CI without a prompt.

## D47. The store hash of a build is the same on every machine that can share it

A build's hash covers the manifest, version, platform and the store names of
its deps, not their absolute paths. It also covers the store root unless the
manifest says `relocatable = true`. Why: two machines must compute the same
store name before one can use the other's build, and a build that can contain
its own path must never match a machine with another root. B88 follows from
the hash and needs no check at substitution time.

## D48. A trusted cache entry needs no build approval

oku looks in the caches before it asks for approval, for the package and for
each dep. An entry from a trusted key installs without the approval prompt, and
the lock records it as a build. An entry that oku cannot trust is ignored with
a note, and the package builds locally. Why: approval protects against running
a manifest's commands, and substitution runs none. Trusting a key grants more
than an approval does, because oku installs whatever that key signed.

## D49. A signing key covers artifacts, and the lock pins the first one seen

The signature of an artifact is at its URL with `.minisig` appended. oku accepts
the hashed and the legacy kind of minisign signature. The lock pins the key at
the first install. After that oku refuses a manifest with another key, or with
none, on `add`, `sync` and `update` until `--accept-key`. A signed artifact
without a sha256 is not trust on first use. Why: with no registry, the first
install is the only point where oku can learn a developer's key, and after it a
changed key is what someone who took over the repo would publish. Build sources
are outside the key and rely on the sha256 of their `fetch` steps.

## D50. Windows is tested on a CI runner, with a live script

There is no Windows machine to test on, so `.github/scripts/live-windows.ps1`
runs the real `oku.exe` on a GitHub Actions `windows-latest` runner against real
GitHub releases, on every pull request. The Go test suite skips on Windows,
because its fixtures are shell scripts. Why: code that only cross-compiles for
Windows has never run, and the first three runs each found a bug that no other
machine could show. The same CI found two bugs on Linux amd64 that an arm64 Mac
hid.

## D51. A Windows profile uses junctions, hard links and shims

A normal Windows user may create a symlink only in developer mode. So `current`
is a directory junction, a program in `bin` is a shim, and any other file is a
hard link, or a copy across volumes. A shim is a copy of `oku.exe` under the
program's name beside a `<name>.shim` file. oku checks for that file before
anything else, runs the program it names with the same arguments and stdio, puts
the `bin` of each dep at the front of `PATH` for DLL lookup (D7), and exits with
the program's code. Why: oku ships one binary and needs only a normal user's
rights. Scoop works the same way.

Shims are hard links to one copy of oku in `<data>\oku\shims`, not to `oku.exe`.
Why: Windows refuses to delete any link to a running program, and `gc` deletes
shims while oku runs. A junction cannot be replaced by a rename, so switching
generations on Windows has a moment without `current`. On unix it is one rename.

## D52. An .msi is unpacked with msiexec /a

oku recognises an `.msi` by its OLE header and runs the administrative install,
which copies the files out and skips the install sequence. It is Windows only,
as dmg and pkg are macOS only (D16). Why: a parser for the format in Go is a
large dependency for one OS, and `msiexec /a` is the tool Windows provides for
this. An `.msi` can define actions for the administrative install itself, and
those run. The docs say so.

## D53. Windows uninstall renames the running binary

Windows refuses to delete a running program but lets it be renamed. `self
uninstall` renames `oku.exe` to `oku.exe.uninstalled` and starts a detached
`cmd` that deletes it about four seconds later. B101 holds for the path of
`oku.exe` when the command returns, and for the last file a few seconds after.

## D54. A Windows service is a scheduled task with an oku wrapper

A user service is a task named `oku-<name>` that runs as the user with the least
rights. Enabled means a logon trigger. A task can neither set environment
variables nor redirect output, so it starts the hidden `oku service-run`, which
reads the stored definition, opens the log, and runs the program without a
window inside a job object with kill-on-close. Why: `schtasks /End` kills only
the task's own process, and without the job a stopped service keeps running.
The wrapper exits when the service's program exits, so D14's "no oku daemon"
still holds. Task Scheduler restarts only after a failure, so `always` behaves
like `on-failure`.

A system service is the same task, run as the `SYSTEM` account with a boot
trigger. D14 said "Windows service". A Windows service has to implement the
service control protocol, which a package's program does not, and a task from
boot with no user logged on gives the same result with the code that user
services already test. Windows has no `sudo`, so an elevated oku does the
privileged step directly, and otherwise starts it through the consent prompt.
The change goes through a file, because that prompt does not keep the quotes of
an argument.

## D55. The sandbox probe runs the real setup

The Linux sandbox probe created the namespaces and took that as a yes. Ubuntu
24.04 allows that and then denies every mount, so every build failed there. The
probe now runs the real sandbox setup with a command that does nothing. Why:
creating the namespaces succeeds on hosts where the setup then fails, so only
the setup itself shows whether a host can sandbox.

## D56. One hook line is the whole shell setup

`oku hook <shell>` puts the directory of `oku` and the global profile's `bin` on
`PATH`, and applies projects. The line in the startup file names `oku` by its
full path, written with `$HOME`, and does nothing when that file is gone. The
installer, `oku add` and `oku doctor` print the line together with the file it
goes into, and the installer also prints a command that appends it. oku still
edits no startup file itself (B93). Why: the first install on a fresh Mac ended
with "add ~/.local/bin to PATH" and nothing more. The user then needed a second
`PATH` entry for installed programs and a third line for projects, and a line
that calls `oku` by name cannot work before `oku` is on `PATH`.

## D57. A moving tag is a version source, and each build is its own version

`[version] from = "github-releases"` takes `tag = "nightly"`. oku then reads
that one release instead of listing releases, and accepts it when it is a
prerelease. A draft fails. `tag` excludes `strip_prefix`, and `git-tags` does
not take it.

The version is `<date>-<commit>`, such as `2026.09.20-a73243f`. Both come from
the commit the tag points at, read from `GET /repos/<repo>/commits/<tag>`. The
date is its committer date in UTC, and the commit is the first seven characters
of its hash. One commit therefore has one version. The release's
`published_at` cannot give the date. `gh release edit` leaves it at the first
publish, and neru and oku update their nightly release with that command.
`{{tag}}` expands to `nightly` and `{{version}}` to the version. The lock
stores the full commit as `tag_commit`.

When the manifest gives no `sha256` and no `sha256_url`, oku uses the `digest`
that the GitHub API reports for the asset whose download URL equals the
artifact's `url`. An asset without a digest falls back to trust on first use.

`update` moves the package when the tag's commit differs from `tag_commit`. The
URL stays the same, and the old digest no longer applies because the version
changed. `sync` with a locked version asks upstream only when it has to
download, which is when the store path is missing or the platform has no lock
entry. It then fails when the tag's commit is not `tag_commit`, and names
`oku update <name>`.
`add <ref>@<version>` succeeds only while upstream is at that version. A
`[build]` whose `source.git` clones `{{tag}}` fails when the clone is not at
`tag_commit`.

Why: D24 drops `nightly` on purpose, because it is no version. But a nightly
under a fixed `value` has one version for ever, so the lock pins the first
digest and every later download fails, and `update` has nothing to move. Making
each build a version keeps the rest unchanged. The store path differs per
build, `rollback` returns to yesterday's build with no download, and `gc`
decides how long old builds stay. The date sorts builds for a dep constraint,
and the commit tells two builds of one day apart. The API digest gives a
checked download for projects like neovim that publish no checksum file.

Upstream deletes the old build when the tag moves, so a lock that pins a
moving tag restores on another machine only until then. A build cache does not help, because `cache push` skips plain downloads
(D46). oku fails there with the reason instead of installing a newer build
under the old version.

Out of scope: inferring a manifest for `github:owner/repo@nightly`, moving
tags on `git-tags`, and moving branches.

## D58. oku's own nightly is a signed prerelease that only a flag selects

Every push to `main` moves the tag `nightly` and replaces the files of the
prerelease with that name. The release key signs them like a release. The
binaries report `nightly-<timestamp>-<commit>`. `oku self update --nightly`
reads that release and compares its commit with the end of the running version.
`oku self update` without the flag takes the newest release, also from a
nightly binary. The install scripts need no change, because `OKU_VERSION`
already names any tag.

Why: testing a change on a real machine needed a release, and release-please
makes one only from a version bump. A prerelease stays out of
`releases/latest`, so no user gets a nightly without asking. `self update`
could not install an unsigned nightly, and a second key would be one more
secret to rotate. The cost is that the release key now signs every commit on
`main`. Before, it signed releases only.
