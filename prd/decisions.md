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
`sha256_url`, or are trusted on first use and pinned in the lock. The same
holds for the source archive of a build, so a source build follows upstream
like an artifact does. Why: one file per version puts a nixpkgs-sized burden on
owners. The lock carries the strictness instead.

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

When several assets fit one platform, oku takes the command line build. It
skips a name with `desktop`, `app`, `gui`, `dmg`, `installer`, `setup` or
`.app.`, prefers a tar archive over a zip, then the smaller asset when the
host reports sizes, then the shorter name. For the checksum file it takes
`<asset>.sha256`, else the shared file that names the asset's OS and arch,
then one that names its OS or arch alone, then a generic file such as
`checksums.txt`, and never one that names another platform. Why: a repo that
ships a desktop app beside its CLI (sst/opencode) or one checksum file per OS
(stripe/stripe-cli) is common, and a wrong pick fails at install with a
checksum that lists no such file or an archive with no program in it. When an
install from a manifest inferred in the same run fails, the error ends with
that manifest, because the user never saw it and cannot tell what oku tried
otherwise.

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
(apps, fonts, service units, launcher entries, the files of `[files]`) and the
settings it changes (D62) are recorded in `<data>/oku/exposed.toml` at the
moment they are written. oku writes only targets the list names. It never
edits a file it did not place, so it never edits a shell rc file or the system
PATH. The hook line is printed for the user to add. `oku self uninstall` replays the ledger in reverse, stops and
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
included list from a URL or a repo by commit and sha256, and ignores a lock
beside an included list. oku does not pin a list that is a file on this
machine. It is the user's own file, like `oku.toml`, and a user who splits
their list into files must be able to edit one and sync. `oku update` with no names reads includes fresh. `oku update <name>`
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
numbers, and a `-` suffix sorts before the same version without one. Two
suffixes compare piece by piece, with a number as a number, because some
projects count their releases there, such as ImageMagick 7.1.2-31. GitHub
drafts and prereleases are skipped. A version with a prerelease word in it, such
as Go's 1.27rc1, sorts behind every release, because a tag list has no
prerelease flag. The lock stores the tag beside the version,
and `{{tag}}` expands to it. Why: many upstream tags are not valid semver, so a
strict parser would reject real projects. Release URLs often contain the tag
and the version in different places, and keeping the tag in the lock lets
`sync` build the URL without asking upstream again.

## D25. An inferred manifest is announced, installed without a prompt, and stored

`oku add github:owner/repo` on a repo with no manifest says that it inferred
one and installs from it without asking. `--verbose` prints the manifest. The
lock stores the full manifest text. `sync` installs from that text and never
infers. Only `update` infers again. Why: a prompt would break scripted
installs. The manifest is long and most users never read it, so one line says
it exists, and `--verbose` or `oku.lock` shows it. Inference is a heuristic
that will change between oku versions, so re-running it on another machine
could give a different manifest for the same lock. Storing the text keeps
"same input, same machine" true.

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

macOS ships libraries such as zlib with headers and no pkg-config file. In a
build on macOS oku writes one for each of them, last on `PKG_CONFIG_PATH`, with
the version from the SDK on the machine. Why: the pkg-config file of libpng,
freetype or libtiff says `Requires: zlib`, and without a `zlib.pc` every
configure script that asks pkg-config for them reports them missing. A manifest
that wrote those files itself would carry the versions of one SDK.

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

The Linux sandbox makes every mount read-only, then binds the build's own
directories back writable, with `mount_setattr` or, before Linux 5.12, one
remount per mount. It mounts empty tmpfs over `/run/user` and `/tmp/.X11-unix`
and gives the build its own `/dev/shm`. The macOS profile denies Apple Events,
LaunchServices and running `launchctl`. Why: a build that can write to the
store can change the programs of other packages, and a build that can reach
the user's D-Bus, X server or LaunchServices can start a program outside the
sandbox.

Known limit: the sandbox limits a malicious build and does not contain it. A
program that talks to launchd over XPC on macOS, or to a socket outside the
hidden directories on Linux, such as Docker's, can still start a program
outside it.

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
app, a font, a service, a file in the home directory or a setting is visible
to the whole account, not to one directory. A project list that holds
`[files]` or a settings table is an error, because a cloned repo must not
write into the home directory.

oku copies apps and fonts. Finder, Spotlight and the font services do not treat
a symlink into the store as installed.

## D42. A service that is not enabled is installed and stopped

Installing a package never starts its service. `service = true` on the list
entry runs it now and at login. On macOS a service that is not enabled keeps its
plist in `<data>/oku/services`, because launchd loads everything in
`~/Library/LaunchAgents` at login. `oku service start` and `stop` last for the
session. Why: a user does not expect a daemon to start because a package
arrived as a dep, and the list is the one place that says what runs.

A service of the user gets the `bin` of the global profile first on its `PATH`,
and its `args` and `env` expand the locations of D60. Why: a service manager
starts a program with a bare `PATH`, and a manifest cannot know the user's home
directory. A hotkey daemon that calls another installed program by name would
otherwise need the full store path in its config.

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
`PATH`, loads the completions under the profile's `share/completions` and the
ones of `oku` itself, and applies projects. The line in the startup file names
`oku` by its full path, written with `$HOME`, and does nothing when that file is
gone. The installer, `oku add` and `oku doctor` print the line together with the
file it goes into, and the installer also prints a command that appends it. oku still
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

Every push to `main` replaces the files of the prerelease `nightly` and then
moves the tag with that name. The files come first, so that the files a reader
gets are from the commit the tag points at. The release key signs them like a
release. The binaries report `nightly-<timestamp>-<commit>`.
`oku self update --nightly` reads that release and compares its commit with the end of the running version.
It refuses a release that upstream made from a branch, because oku cannot tell
from a branch name which commit the files are from.
`oku self update` without the flag takes the newest release, also from a
nightly binary. The install scripts need no change, because `OKU_VERSION`
already names any tag.

Why: testing a change on a real machine needed a release, and release-please
makes one only from a version bump. A prerelease stays out of
`releases/latest`, so no user gets a nightly without asking. `self update`
could not install an unsigned nightly, and a second key would be one more
secret to rotate. The cost is that the release key now signs every commit on
`main`. Before, it signed releases only.

## D59. oku checks a change first, and reverts it when it fails

`add`, `remove`, `sync`, `update` and `rollback` run in two parts. The plan
changes nothing a user can see. It resolves, downloads, verifies and builds
into the store, renders every template into the new generation, checks each
target for a file oku does not own, reads the current value of each setting
and checks its type. A failure there stops the command with the machine
unchanged. The store and the cache may have gained entries that nothing uses,
and `gc` deletes them.

The apply writes `<data>/oku/pending.toml` with the numbers of the active and
the new generation, switches `current`, makes the ledger match the new
generation, writes `oku.lock` and deletes `pending.toml`. `current` comes
first, because the target of a file with content points through it (D60). When
a step fails oku makes the ledger match the old generation again, deletes the
new one and leaves the lock as it was. It then reports the first error. A
command that finds `pending.toml` does that revert before anything else, and
says so. When a revert step fails too, oku stops, names what is left and keeps
`pending.toml`, and `oku doctor` reports it.

`--dry-run` on `sync` and `update` runs the plan, prints what the apply would
do, and stops. It can still fill the store and the cache, like any plan. It does
not revert a change that did not finish, because that would change the machine,
and says to run `oku sync` first.

Why: after a half-applied change the machine matches neither the old list nor
the new one, and the user cannot tell which parts changed. Before this, a
failure while exposing left the new generation active beside the old lock.
Reverting is the ledger sync of D41 run toward the old generation, the same
code as `rollback`, so there is no second undo mechanism to keep correct. No
filesystem gives one atomic step over files, services and OS settings, so the
promise is check first and revert, not a true atomic commit.

## D60. The global list places files in the home directory

```toml
[files]
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.hushlogin" = { text = "", mode = "0600" }
"{{localappdata}}/nvim" = { link = "./files/nvim", when = { os = "windows" } }
```

The key is the target. It starts with a location variable: `{{home}}`,
`{{config}}`, `{{data}}`, and on Windows `{{appdata}}` and `{{localappdata}}`.
A variable this OS lacks is an error unless `when` excludes the entry. The
value holds exactly one of `link`, `text`, `render` (D61) and `secret` (D63),
and may hold `when` and `mode`. A source path starts at the directory of the
list that declares it. A link source may start with `{{pkg.<name>}}`, the
files of a package in the list.

`link` makes the target a symlink to the source, so an edit shows at once and
the user's own repo versions the content. `text` and `render` write the content
into the generation under `files/`, read-only, and the target is a symlink
through `current`. Switching `current` therefore switches every such file in
one step, and only a new or a removed target is a ledger step. On Windows a
linked directory is a junction, and every file is a copy. oku stops before any
change when a copy no longer has the bytes oku wrote.

A package may hold only files, such as a repo of agent skills. Its artifact
says `data = true` and exposes nothing. The key is explicit, because a manifest
that forgot its `bin` must still fail.

oku refuses a target that exists and is not in the ledger. `file` is one more
ledger kind, so `remove`, `rollback` and `self uninstall` handle it like an
app or a font.

`[files]` in a list from a repo came later, in D74. A list at a URL still
cannot hold them.

Why: the table is keyed by target so that two entries for one path are a TOML
error. Content in the generation makes rollback restore the exact bytes with
no copy of the user's repo. A link is the exception on purpose, because a
config the user edits often must not need a sync per edit.

## D61. Templates take variables and have no logic

```toml
[vars]
font = "JetBrainsMono Nerd Font Propo"
email = "me@example.com"

[vars.theme]
base00 = "0c1410"
```

`[vars]` holds strings. A nested table joins its names with a dot, so the
colour above is `{{theme.base00}}`. Includes merge `[vars]` like `[packages]`,
and the user's own list wins. `render = "./x.tmpl"` writes the file with each
`{{name}}` replaced. `text` and targets expand the same way, and a location
such as `{{data}}` is a variable too. A name that is not set is an error that
names the file and the line. `\{{` writes the two braces themselves. There are
no conditionals and no loops.

Why: a difference between platforms belongs in `when` on the entry, and most
tools can include a second file, so logic in templates would only add a
language to learn. The same template then gives the same bytes on every OS. A
colour scheme is a table of variables that the user writes, so oku needs no
loader for one scheme format. A name may hold `-` and spaces may surround it,
which is how base16 templates are written, so one of those works unchanged with
variables named `base00-hex`. `when` matches a platform, not one machine (D15),
so a machine that differs sets its own `[vars]` in its own list.

## D62. Settings are per OS, in user scope, with the old value kept

```toml
[defaults."com.apple.dock"]
tilesize = 48

[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"

[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

Each table is named after the mechanism of one OS, and oku skips the tables of
the other OSes like a package whose `when` does not match. TOML types map to
the types of the mechanism. oku owns a table value, such as
`NSUserKeyEquivalents`, and writes it whole. oku writes settings in user scope
only. A registry key outside `HKCU` is an error, and oku never writes
`/Library/Preferences`.

macOS keeps some preferences per Mac, which `defaults -currentHost` writes.
They get their own table, `[defaults-currenthost.<domain>]`, and go to the same
store with a marked domain. Without it such a setting needs the path of a file
under one user's home directory as its domain, which ties the list to one user
name.

`setting` is a ledger kind. Before oku first writes a key it records the value
it found, or that there was none. A key that leaves the list gets that value
back, also on `rollback` and `self uninstall`. Settings have no atomic switch,
so each one is a step of the apply in D59.

Why: setting keys are not portable, and one `[settings]` table would suggest
that a key works on every OS. Without the recorded old value oku could not
reverse a setting. User scope keeps settings out of the elevation path of D43.

## D63. oku decrypts a secret at apply time, and keeps only the ciphertext

```toml
[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
"{{home}}/.ssh/backup_key" = { secret = "./secrets/backup_key.age" }
"{{home}}/.config/gh/hosts.yml" = { render = "./files/gh-hosts.tmpl" }
```

A `secret` entry writes one decrypted value to the target. `[secrets]` names a
value, and a `text` or a template uses it as `{{secret.github_token}}`. `key` is
the path of one value in a sops file, with `/` between its parts. Without `key`
the whole decrypted file is the value. The mode of a file that holds a secret
is `0600` unless the entry gives one.

oku reads two formats and tells them apart by the content of the file. It
decrypts an age file itself, with the library of age's author, so a new machine
needs no tool for it. It runs `sops decrypt` for a sops file, and looks for
`sops` in the packages of the list first, also in the generation it is about to
activate, and then on `PATH`. So the first sync of a machine can install sops
and decrypt with it. The age identities are in `SOPS_AGE_KEY_FILE`, else in
`{{config}}/sops/age/keys.txt`, for both formats, and oku passes that path to
sops, which looks elsewhere on macOS and on Windows. oku never writes or
creates a key.

The generation holds a copy of each encrypted file, never decrypted bytes. For a
`text` or a template that uses a secret it holds the content with every other
variable filled in and the secret left as its name. The plan decrypts every
secret in memory, so a missing identity, a missing `sops`, a wrong `key` or a
secret that no entry of `[secrets]` names stops the change before it starts. The
apply writes the final bytes to `<data>/oku/secrets/`, and the target is a link
to that file. On Windows the target is a copy. Only the user can read that
directory and those files: mode `0700` and `0600` on macOS and Linux, and on
Windows an access control list that names the current user alone and inherits
nothing.

`secret` is a ledger kind. Its identity is the hash of the content the
generation holds, of the encrypted files it uses and of their `key` values. So
a changed encrypted file writes the secret again, and `rollback` decrypts the
encrypted files of the generation it returns to. Removing the entry, and
`self uninstall`, delete the decrypted file. `oku doctor` reports a list with
secrets on a machine that has no identity file, or no `sops` for a sops file.

No generation, ledger entry, output or error holds decrypted bytes. An error
names the encrypted file and the `key`. Only the global list and its includes
may hold `[secrets]` or a `secret` entry, as for `[files]`.

Creating and editing secrets is not oku's job and will not become one. `sops`
and `age` own recipients, key rotation and the editor, and a second tool that
writes encrypted files would be a second place where a mistake leaks a secret.

Why: the user already has a sops file, a `.sops.yaml` and the habit of `sops
secrets.yaml`, and sops also covers PGP and cloud keys, which oku would
otherwise have to support one by one. oku does not link the sops library,
because it would bring the SDKs of three cloud vendors into a static binary,
and it does not read the sops format itself, because a second implementation of
a format with its own integrity check is a risk with no gain. An age file is
one call into a small audited library, and it lets a machine with nothing
installed decrypt its first key. Decrypted bytes in a generation would stay on
disk in every old generation until `gc`, and a generation kept for rollback
must not store old keys. Keeping the ciphertext there gives rollback the right
value, and the only decrypted bytes on disk are the files in use.

## D64. A branch is a source of versions, and the lock holds its commit

`version.from = "git-branch"` with `version.branch` follows the newest commit of
a branch. The version is `<date>-<commit>`, the form a moving tag has (D57), and
`{{tag}}` is the branch. oku reads the commit with a bare clone of depth 1 that
holds no files. `oku.lock` records the commit in `tag_commit`, and a build
fetches that commit instead of the branch.

Why: a developer wants to run the programs they write from `main`, before a
release. A fixed version with `tag = "main"` builds once and never again, and
its lock names no commit. A moving tag loses the locked build when upstream
moves it, because a release holds one set of files. A git host keeps every
commit, so `sync` can fetch the locked commit of a branch at any time.

## D65. oku pins other platforms when it writes the lock

`[lock] platforms` names the platforms besides the host that `oku.lock` pins
every package for. `add`, `update` and `sync` write those entries from any
host, and a platform that oku cannot pin is an error. Without the table oku
pins the host alone, in a project as in the global list. The digest comes
from the manifest, else from
`sha256_url`, else from a download that oku hashes and never unpacks. Only the
user's own list may hold the table (D19).

Why: the version, the manifest and the deps were always shared, but a platform
entry appeared only when a machine of that platform synced (B15). So the
person who wrote the lock never pinned the other platforms' digests, every new
platform changed a committed file, and CI, which commits nothing, trusted its
download on every run. Writing the entries with the lock moves the first use
to the author's machine, where someone reads the notice. Without the table oku
pins the host alone. A project used to pin every platform it could, which
hashed up to seven downloads per package and ran the npm vendor step once per
platform (D68) for machines that may never exist, and skipped a platform that
failed without saying so. The author knows which machines share the lock, so
the author names them once. Every command then does the work for those
platforms and no more. A missing platform still gets its entry the first time
a machine of that platform syncs (B15).

Known limits: oku pins the build deps of a platform that builds only when the
host builds too. For the `vendor_sha256` of another platform see D68.

## D66. `sync --locked` refuses to change the lock

`oku sync --locked` stops before any download when `oku.lock` has no entry of
the host's platform for a package of the list. After resolving, it stops when
the bytes of the lock would differ from the file. It writes nothing in both
cases.

Why: `sync` fills a missing platform entry without asking (B15), which suits a
person who commits the lock afterwards. CI commits nothing, so there the same
step trusts a download on every run, and nobody learns that the author forgot
to complete the lock (D65). The check runs before the downloads so that CI
never runs bytes that nobody pinned. The byte comparison afterwards finds the
changes that the first check does not look for, such as a dep with no entry or
a package that left the list.

## D67. oku pins a package that the host does not install

`install` handles a package whose `when` leaves out the host too, with a
request that only locks. oku fetches the manifest, picks
the version, pins each lock platform that `when` matches (D65) and does the
same for the deps. It selects no artifact for the host, runs no build and adds
nothing to the store or the profile. For a repo with no manifest, oku infers
one for the first of those platforms, because inference opens the asset of one
platform to find the program. `sync` does not resolve a package again when the
lock already pins it, so a sync with nothing to do still reads no manifest
(B178).

Why: before this the first machine that `when` matched resolved such a
package, so the author of a lock on a Mac could not pin a Linux-only tool, and
`sync --locked` failed for it on Linux (D66). A second resolver beside
`install` would have to repeat the rules for versions, signing keys, inferred
manifests and nested deps, and every later change to one of them would have
to be made twice.

Known limit: oku pins the build deps when one of the pinned platforms builds,
and a constraint of a dep picks one version for all platforms.

## D68. A go or cargo vendor digest holds for every platform

When oku pins a build for another platform (D65), the entry holds everything
that a build there would write: `impure`, the source archive and its digest,
and the `vendor_sha256` of the build on the host when every vendor step of the
manifest is `go` or `cargo` and has no `when`. For `npm` and `pip` the digest
still comes from the first build on each platform (D34). oku pins again an
entry that only says `strategy = 'build'`, which older versions wrote.

Why: without those fields the first build on another platform changed the
lock, so `sync --locked` (D66) failed for every package that builds. `go mod
vendor` copies the packages of every platform, and `cargo vendor` takes every
target of `Cargo.lock`. On 2026-09-22 goimports 0.44.0 gave the same digest on
darwin-arm64 and linux-arm64, and `cargo vendor --locked` of eza 0.23.5 gave
the same 18193 files on both with cargo 1.98.1. cargo 1.90.0 gave 18200 files,
so the digest depends on the version of cargo. The lock pins one version of
the toolchain dep for all platforms, so every platform runs the same cargo. npm
installs optional packages by `os` and `cpu`, and pip downloads wheels for the
platform, so their trees differ. A wrong pin stops the build on the other
platform with "the vendored packages changed", and the message names `oku
update`.

## D69. oku downloads the npm packages of another platform to pin them

npm installs optional packages by `os`, `cpu` and `libc`, so the tree differs
between platforms (D68). npm also takes those three as configuration. To pin a
build for another platform, oku runs the npm vendor steps with
`npm_config_os`, `npm_config_cpu` and `npm_config_libc` set to that platform,
in the sandbox and the environment of a build, with a temporary directory for
the prefix. It hashes what npm wrote and deletes it. It runs no other step.

oku does this only when the host built the package, because the build was
approved and its deps, such as node, are in the store. It also needs every
vendor step of the manifest to be `npm`, and no `run` step before them, because
a command for another platform may not run on the host.

Why: a lock for several platforms (D65) was complete for every package but
the npm ones, and `sync --locked` (D66) failed for them on the second platform.
On 2026-09-22 `npm install --os=linux --cpu=arm64 --libc=glibc` of esbuild
0.25.0 on darwin-arm64 gave the same tree as the install on linux-arm64, with
`@esbuild/linux-arm64` in it, and the install on darwin-arm64 without the flags
gave another. Both sides ran npm 10.9.3, and the lock pins one node for all
platforms. The first runs on a Mac gave a new digest each time, because the
temporary directory of macOS is behind a symlink and npm wrote its name into
`node_modules/.package-lock.json`. A build now resolves the symlinks of its
directory first. The install runs with `--ignore-scripts`, as every npm step of oku
does, so no code of the packages runs on the host. pip can name a platform too,
but only for wheels. oku does not do that yet.

A step may name dependencies in `scripts`. After the install is hashed, oku
runs `npm rebuild <names>` in the same sandbox with the network on, which runs
the lifecycle scripts of the named packages alone, and marks the build impure
the way `network = true` on a run step does (D11). The trade-off is that a
package such as claude-code or opencode-ai has a dependency whose postinstall
downloads or builds a native binary, and without it the install succeeds and
the program fails at run time. What the script downloads is not in the digest
that the lock pins, because the hash is taken first, so `sync` still checks
the packages but the build cannot be reproduced from the lock and never goes
to a cache. The names are a list, and not `scripts = true`, so a manifest says
whose code runs. oku checks each name against the package's dependencies from
the registry when it builds, and fails on one it does not find. A step
without `scripts` still runs none. When oku pins the step for another
platform it does not run the scripts, since the digest covers the install
alone and the scripts would run the host's code.

## D70. A rebuild replaces a build under its own store path

`oku sync --rebuild <name>` builds a package again although the store holds
its build. The store path stays the same, because it is a hash of the
manifest, the version, the platform and the deps, and none of those changed.
oku renames the old build to `<path>.old`, builds into the path, and deletes
the old build on success or renames it back on failure. A build that finds
`<path>.old` and no build at the path puts the old one back first, which
covers a rebuild that a crash interrupted. `gc` keeps `<path>.old` while a
generation uses `<path>`, because until the next install it holds the only good
build. A rebuild takes nothing from a cache.

Why: a store path is immutable by rule, and one case needs an exception. A
build from an older oku holds no vendor digest beside it (D34, B188), and only
a new build gives one. The way around was to delete the store path by hand,
which leaves every generation with a broken package until the build ends, and
with nothing when it fails. A build must write into its final path, because
build systems write the prefix into the files they install, so oku cannot
build beside the old one and swap. The flag names packages, and not
everything, because a rebuild of all thirty builds of a list takes about an
hour. The flag belongs to `sync` and not to `update`, because it keeps the
locked version.

## D71. A `needs` tool reaches a build as a link, not as its directory

For each `needs` tool oku puts one link under the tool's name into a directory
of the build's temporary directory, and that directory goes on `PATH` after the
deps. The directory the tool really lives in stays readable in the sandbox and
stays off `PATH`. On Windows the link is a shim, as in a profile. Why: `needs
= ["cc"]` used to put the whole directory of the found `cc` on `PATH`, so a
build on a machine with a Nix profile or a Homebrew `bin` saw that directory's
`python`, `make` and everything else, and built differently from one without.
A link keeps the tool in its own directory, so a compiler driver still finds
its assembler and linker beside itself, which is why the link is not a copy.

## D72. An artifact may run its download to generate completions

`completions = { generate = "..." }` runs the command once per shell after
unpacking, under the build approval, and `oku.lock` records `commands = true`
for the platform. Why: many releases ship a bare binary that prints its own
completions and no files. The approval is the same prompt as a build because
both execute what oku downloaded. The lock flag is a record, not a check: a
changed template is a changed manifest hash, which already stops `oku sync`.

`completions = "dir/"` links the conventional files that exist and fails only
when none do. Why: lint cannot see the archive, and releases ship two of the
three often enough that a missing one must not fail the install.

## D73. One oku process changes the machine at a time

Every command that writes oku's state takes an OS lock on `<data>/oku/busy`
(`flock` on unix, `LockFileEx` on Windows) and holds it for its whole run.
Another such command waits and says which process it waits for. Commands that
only read take no lock. Why: `pending.toml` means "a change that did not
finish", and a second process could not tell that from a change still running,
so it reverted it. `gc` could delete a store path that a running install had
built but not yet put in a generation. The OS releases the lock when the
process exits, so a killed oku never leaves a stale lock. A lock that is only a
file on disk would stay behind.
Waiting rather than failing keeps a sync started from two terminals, or from a
script, working.

The lock is per user, in the data directory, because a store root from
`oku setup --system` belongs to one user as well. `oku shell` holds it only
while it installs, and `oku self uninstall` releases it before it deletes the
data directory, which Windows refuses while the file is open. With the lock
held, a `.tmp-` directory in the store can only be left by a killed install,
so `gc` deletes it.

## D74. A relative path in a remote list names a file of the same repo

In a list or manifest that oku read from a repo, a relative path names the file
at that path beside it in the same repo, and oku reads it at the same commit.
From a URL it names the URL beside it. The lock stores such a ref as
`<repo ref>#<path>`, and a forge fragment with a `/` or ending in `.toml` is a
path, not a manifest name. An absolute path, and a relative one that leaves the
repo, stay errors. Why: a machine repo is laid out as a list, its includes and
its manifests, with relative paths between them, and before this `oku sync
<repo>` refused every such repo. The commit is the
list's, so one lock pin covers files that were written together. A package
keeps its own commit in the lock after that, so `oku update <name>` can move it
alone.

`[files]` and `[secrets]` in a list from a repo work too. `oku sync` downloads
the repo's files at the list's commit as one archive into the store, once per
repo and commit, and the paths of the entries start there. A `link` leads into
that store path, and `gc` keeps a store path while a generation links into it.
A path that leaves the repo is an error, and so is an absolute one. Why: a
machine repo keeps its dotfiles beside its lists. A link into the store points
at the files of the pinned commit. A clone in the cache would not do, because
another ref in the same repo checks out another commit there. A list at a URL
still cannot hold them, because a URL has no directory to download.
