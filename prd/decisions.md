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

## D16. Installers are unpacked, never executed

`extract` understands tar, zip, 7z, dmg, pkg, msi, deb, rpm and AppImage. Why:
running a vendor installer writes outside the store, needs root, and cannot be
rolled back.
