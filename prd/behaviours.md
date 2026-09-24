# Behaviours

What oku promises. Each line is testable through the CLI. The tag is the build
order step in `prd/product.md`.

## Install and remove

- B1 [1] `oku add ./pkg.toml` realizes the artifact matching the host selector
  and its `bin` entries run from the profile `bin`.
- B122 [1] A `bin` entry that is a table makes oku write a program that runs
  `run` with `args` before the user's arguments. `run` may name a runtime dep,
  and that dep stays out of the user's profile.
- B277 [1] A program from a `bin` table finds the programs of its runtime deps
  on its `PATH`, and never those of its build deps.
- B209 [1] A `bin` table with `name` and `path` exposes the file at `path`
  inside the package under `name`, in an artifact and in an `install` step.
  A table with both `path` and `run` fails `oku manifest lint` and `add`.
- B128 [1] In `[env]`, `{{pkg}}` is the directory that holds the package's
  files, which for an artifact is the unpacked download.
- B2 [1] An artifact whose download does not match its sha256 is rejected and
  nothing enters the store or profile.
- B247 [1] An archive with a symlink that leads outside the package, when
  followed through the other links of the package, is refused and nothing
  enters the store.
- B3 [1] A manifest with no matching artifact and no `[build]` fails with an
  error naming the host platform.
- B4 [1] `oku remove <name>` drops the package from the profile. Its store
  path stays until `gc`.
- B195 [1] `oku remove` takes several names and drops them in one generation.
  A name that is not installed stops the command before anything changes.
- B227 [1] On a terminal a table fits the width: the last column wraps under
  itself, oku cuts another column that must give room and ends it with an
  ellipsis, and under 60 columns a table whose rows do not fit side by side
  prints each row as a block of label and value lines. A pipe gets the same
  text as before.
- B228 [1] On a terminal `sync` and `update` print a row for each package as
  it finishes, above the waits still running: a green check with the name,
  version and note, a dim dot for a pin on another platform, a red minus for a
  package that left. In a pipe the summary table at the end is unchanged.
- B229 [1] Once the user answers a build approval, the prompt goes and one
  line stays: a check with `approved <name> <version>`, or an x with
  `rejected <name> <version>`.
- B231 [1] On a terminal every line for something that finished starts with
  a green check, and a line for something removed with a red minus: the
  files, settings, apps and services a change placed, the store paths `gc`
  deleted, the packages `cache push` packed, and the closing line of `add`,
  `remove`, `rollback`, `gc`, `sync` and `update`, which says `done in <time>`
  with what the profile holds. In a pipe the lines are unchanged.
- B233 [1] On a terminal no row of a table ends in spaces, and a first
  column with no header, such as a generation's number, titles its block when
  the table stacks.
- B234 [1] On a terminal a label and value list wraps a long value under
  itself, a note, an error or a `doctor` line wraps under its text, and the
  help reflows its text and lists each flag's description beside it, or under
  it when two columns do not fit. A pipe gets the text unchanged.
- B235 [1] A command names its project on stderr once. On a terminal the
  rows of `sync --dry-run` start with a yellow tilde, not a check.
- B236 [1] `oku add` takes several refs and adds them one after another, one
  generation each, and says once how to run their programs, with "it" for one
  package and "them" for several. A failure stops
  it and keeps the packages added before. `--asset` and `--bin` take one ref.
- B241 [1] Each generation records the generation it replaced. `generations`
  shows what changed from that one, and starts the line with `from <n>,`
  when it is not the one numbered before, as after a rollback. `--json`
  gives it as `from`.
- B242 [1] When several packages no longer match `oku.lock`, `sync` and
  `update` finish the others, list every drifted package, and end with one
  `oku update` that names them all, with the names the command was given.
  When a terminal already got a checked row, the error says that nothing was
  installed.
- B237 [1] On a terminal each `command` in a hint, a note, an error or a
  `doctor` line is in colour without its backticks, and the lines of an
  error after the first are not dimmed. A pipe keeps the backticks.
- B238 [1] On a terminal `sync` prints one note that names the packages
  whose manifests oku inferred from the same kind of source. A note about a
  download with no published checksum shows the first 12 characters of its
  sha256. `info` shows the first 12 characters of the commit and says in
  words how oku installed the package.
- B239 [1] When a rollback activates a generation that holds a package the
  list no longer names, it says the next `sync` removes it again and which
  `oku add` keeps it. A list with includes gets no such note.
- B240 [1] A first `sync` of a list with nothing in it writes no generation
  and says there is nothing to sync. A run under a second reports its time in
  milliseconds, not `0s`.
- B196 [1] `oku which <program>` names the package and version that provide a
  program in the profile and the file in the store it runs. Inside a project
  it looks in the project's profile, then in the global one. It warns when
  another program earlier on PATH runs in its place, and it fails for a program
  oku did not install, saying what PATH runs.
- B5 [1] `oku list` shows name, version and ref for the active list.
- B6 [1] A failed install leaves the previous profile active and unchanged.
- B7 [1] `man` and `completions` entries appear under the profile `share`.
- B214 [1] `completions = { generate = "..." }` on an artifact runs the command
  once per shell in fish, zsh, bash with `{{shell}}` substituted, after
  unpacking and before linking, with the package's `bin` first on PATH, and
  writes each stdout under the profile `share/completions` as `<bin>.fish`,
  `_<bin>` and `<bin>.bash`. It needs the same approval as a build, and
  `oku.lock` records `commands = true` for the platform.
- B215 [1] A generate command that exits non-zero or prints nothing fails the
  install, and the error holds the expanded command and its stderr. Nothing
  enters the store.
- B216 [1] `completions = "<dir>/"` names the three conventional files under
  that directory, links the ones that exist, and fails when none do.
- B206 [1] A man page that a build installs under `{{prefix}}/man` appears
  under the profile `share/man`, the same as one under `{{prefix}}/share/man`.
- B8 [1] oku never requires root outside system scope, and writes only under
  its config, data and cache directories, `oku.toml`, `oku.lock`, the per-user
  exposure locations in `prd/architecture.md`, and the targets and settings
  that the global list names.
- B9 [1] Two packages exposing the same `bin` name fail the second install
  with an error naming both.
- B289 [1] `oku add <ref> --plan` prints the version, the download or build
  for this machine, how oku checks it, the programs, and whether the manifest
  runs commands. It checks that the files add downloads are there, and fails
  where add would fail. It writes no list, lock, store entry or generation,
  and does not wait for another oku process. `--json` prints the same fields.

## Refs, list and lock

- B10 [2] `add` accepts file, https, `github:`, `codeberg:`, `gitea:`,
  `gitlab:`, `npm:` and `git+` refs.
- B114 [2] `github:host/owner/repo` reads a GitHub Enterprise Server at `host`.
  oku sends it `GH_ENTERPRISE_TOKEN` and never `GITHUB_TOKEN`.
- B254 [2] Without `GITHUB_TOKEN`, oku sends GitHub the token that
  `gh auth token --hostname github.com` prints, when `gh` is on `PATH`. An
  Enterprise Server gets the one gh holds for its host.
- B115 [2] `codeberg:owner/repo` and `gitea:host/owner/repo` fetch, infer and
  list releases the way `github:owner/repo` does, with
  `version.from = "gitea-releases"`. oku sends `CODEBERG_TOKEN` to codeberg.org
  only.
- B116 [2] `gitlab:group/project`, with any depth of subgroups, fetches, infers
  and lists releases the way `github:owner/repo` does, with
  `version.from = "gitlab-releases"`. oku sends `GITLAB_TOKEN` to gitlab.com
  only.
- B11 [2] `add` writes the package to `oku.toml` and its resolution to
  `oku.lock`.
- B12 [2] `oku sync` makes the profile match `oku.toml`: missing packages are
  installed, undeclared ones are dropped, locked versions are used.
- B178 [2] `sync` and `update` install 8 packages at once, or the number in
  `OKU_PARALLEL`, and build one package at a time. A package the store holds
  at its locked checksum is not checked against its server again.
- B13 [2] `sync` stops with an error when a fetched manifest's hash differs
  from the lock. `update` clears it.
- B14 [2] A checksum learned on first use is written to the lock and enforced
  afterwards.
- B15 [2] `sync` on a platform missing from the lock resolves it and appends a
  platform entry without touching existing ones.
- B179 [2] `[lock] platforms` in the user's own list names platforms. `add`,
  `update` and `sync` pin every package for each one that its `when` matches,
  from any host. The entry holds the artifact's URL and its sha256 from the
  manifest, else from `sha256_url`, else from a download that oku hashes,
  never unpacks and reports as a first use. For a platform with no artifact
  oku pins a build when the manifest has one for it. B271 to B273 cover a
  platform with neither.
- B180 [2] Without `[lock]`, `add` and `update` pin the host alone, in the
  global list and in a project. Another platform gets its entry when a machine
  of that platform syncs (B15).
- B182 [2] oku pins a package whose `when` leaves out the host, with its deps,
  for the lock platforms that `when` matches, and installs nothing of it.
  `sync` does that when the lock has no entry for its ref, or with `[lock]`
  when a named platform is missing, and `update` always. `sync` keeps an entry
  that already pins the package and does not read its manifest.
- B187 [2] A build that oku pins for another platform holds `impure`, the
  source archive and its sha256, and the `vendor_sha256` of the host's build
  when every vendor step is `go` or `cargo` and has no `when`. oku pins again
  an entry that only says `strategy = 'build'`.
- B188 [2] oku keeps the `vendor_sha256`, the source pin and `impure` of a build
  in the lock when the store or a cache already holds that build. `update` of a
  build that did not change writes the same lock.
- B190 [2] `sync` completes a build entry that lacks a pin, for the host and for
  the other lock platforms. It adds the source archive, `impure` and the vendor
  digest it can pin without a build, keeps every pin the entry has, and builds
  nothing for it.
- B191 [2] `sync --rebuild <name>` builds a package again when the store holds
  its build, under the same store path, and takes no build from a cache. When
  the build fails, oku puts the old build back. It refuses a package that is
  a download on the host, one that the host does not install, and `--dry-run`.
- B245 [2] After a `sync --rebuild` was killed during the build, `oku gc`
  keeps the old build, and the next `sync` puts it back without building.
- B263 [2] The action in `action.yml` installs oku unless one is on `PATH`:
  the release that `version` names, else the release of the tag the action is
  used by, else the newest. It runs `oku sync --yes --locked` in its `path`,
  and puts the global profile, the project's tools and their `[env]` on the
  `PATH` and in the environment of the later steps, on Linux, macOS and
  Windows. The action and action-tag jobs of `ci.yml` run it on all three.
- B181 [2] `sync --locked` fails before any download when the lock does not pin
  a package of the list for the host, and names the packages and the platform.
  It also fails when the lock would change in any other way. It never writes
  the lock. Line endings do not count, so a lock and a local manifest that git
  checked out with CRLF pass.
- B16 [2] `include` merges the named lists. A local entry overrides an
  included entry of the same name. `sync` stops when a list from a URL or a
  repo changed since the lock, and reads a list on this machine as it is.
  `remove` refuses a package only an include declares.
- B17 [2] `sync` does not install an entry whose `when` does not match the
  host, and its lock entry stays for other platforms.
- B270 [2] `when` in a list is one table or an array of tables, and matches a
  platform when any of its tables does. A `[files]` entry takes both forms.
- B271 [2] `add` leaves out the host and each `[lock]` platform that the
  manifest has no artifact and no build for. It pins the others, writes a
  `when` on the entry that matches exactly the platforms the manifest has one
  for, and says which platforms it left out. With the host left out it
  installs nothing and says so. It fails only when no platform is left, and
  names the platforms the manifest has.
- B272 [2] When a new version of a package in the user's own list has no
  artifact or build for a platform that its `when` matches and oku works for,
  `update` narrows the `when` in the same way and says so. It never widens a
  `when`. When a new version gains a platform that the `when` leaves out,
  `update` names it.
- B273 [2] `sync` never edits `oku.toml`. When a package has no artifact or
  build for the host or a `[lock]` platform that its `when` matches, `sync`
  leaves the package out there and syncs the rest of the list. It then fails
  with the line of `oku.toml` that leaves those platforms out. For an included
  package it names the list and the `when` the entry needs there.
- B18 [2] `oku sync <ref>` on a machine with no global list adopts that list
  and its lock, then syncs. Two machines of the same platform doing so end
  with identical store hashes. On a machine that has a global list it refuses
  and changes nothing.
- B19 [2] A relative file ref in a list resolves against that list's
  directory. A list from a URL or a repo that names an absolute path is an
  error.
  `add` stores a file inside the list's directory relative to it, in the list
  and in the lock.
- B252 [2] In a list or manifest read from a repo, a relative include, package
  or dep names the file of the same repo, read at the commit of the list or
  manifest, and `oku.lock` stores it as `<repo ref>#<path>`. In one read from a
  URL it names the URL beside it. A relative path that leaves the repo is an
  error.
- B253 [2] A list from a repo may hold `[files]` and `[secrets]`. `sync` places
  them from the repo's files at the list's commit, a `link` leads into the
  store, and `gc` keeps what a generation links to. A path that leaves the repo
  is an error, and a list at a URL with `[files]` or `[secrets]` is an error.

## Versions and generations

- B20 [3] With `[version] from`, `add` picks the newest discovered version and
  `add <ref>@x` picks x. Drafts, prereleases and tags that are not versions
  are never picked, and an unknown x fails naming the newest versions. A number
  in a `-` suffix compares as a number, so 7.1.2-31 is newer than 7.1.2-9. A
  version that names a prerelease, such as 1.27rc1, is never the newest.
- B118 [3] `add <ref>@<version>` finds a version that is not on the first page
  of the host's release list.
- B200 [3] `github-releases`, `gitea-releases` and `gitlab-releases` read the
  newest 1000 releases, one page after another, so a repo whose newest 100
  releases belong to another stream still yields the version `add` wants.
- B250 [3] oku keeps each answer of a forge API with its ETag and asks again
  with `If-None-Match`. When the host answers 304, `update` uses the answer it
  kept, and a changed answer replaces it.
- B251 [3] When GitHub answers 429, or 403 with `Retry-After`, oku stops and
  says how many seconds to wait.
- B262 [3] `oku outdated` lists each package of `oku.lock` whose newest
  version that its `version` in the list allows, or whose latest release,
  differs from the locked one, with the three versions and its ref, and
  `--json` gives `name`, `version`, `newest`, `latest` and `ref`. It changes no
  lock, no profile and no store path.
- B268 [3] With `version.from = "github-releases"`, `add` and `update` check a
  file of the repo's release that has no `sha256` or `sha256_url` against the
  sha256 GitHub reports for it, and do not trust it on first use. The same
  holds for a build's `source` that is such a file. oku refuses a file that
  differs. `manifest lint` does not warn about such an artifact or source, nor
  about a `crates` source that is the crate's `.crate` file.
- B267 [3] `oku exec <command>` runs the command with the global profile's
  `bin` on `PATH` and its packages' `[env]`, and in a project with the
  project's `bin` and `[env]` first, without `oku allow`. Flags after the
  command go to the command, and oku exits with its exit code. oku refuses a
  project whose profile is behind its lock and names `oku sync`.
- B266 [3] A version source reads a tag with or without a `v` in front, whatever
  `strip_prefix` says, so the releases from before a repo changed its tag style
  stay visible. When both forms of a version exist, the tag in the declared form
  is the one oku downloads from.
- B265 [3] A list's `version`, and `@version` in `add`, may be a range such as
  `^1.4`, `~1.4` or `>=1.2, <2`, or a prefix such as `22` that no release has
  exactly. `add` and `update` take the newest version it allows, and an
  inferred package infers from that version. `sync` keeps the locked version
  while the list allows it, and picks again when it does not. A manifest dep
  and a `[runtimes]` entry read their `version` the same way.
- B123 [3] With `version.from = "npm"`, `add` picks the newest version of the
  package in the npm registry that is no prerelease, and `add <ref>@x` picks x.
- B124 [3] oku checks a download against the artifact's `integrity`, or against
  the sha512 the npm registry publishes for it. A download that does not fit is
  rejected and nothing enters the store. oku does not count one that fits as a
  first use.
- B21 [3] `oku update [name]` re-resolves and rewrites the lock. Without it,
  versions never move, and `sync` installs the locked version without asking
  upstream for versions. A package pinned in `oku.toml` stays on its version
  through `update`.
- B177 [3] With `version.from = "git-branch"`, `add` builds the newest commit of
  `version.branch` as version `<date>-<commit>`. `sync` builds the locked commit
  after the branch has moved, and `update` takes the newest commit.
- B280 [3] With `version.from = "redirect"`, `add` follows the redirects of
  `repo` past hops without a version, and installs the version that `regex`
  finds in the first URL it matches. `sync` installs the locked version
  without asking `repo`, and `update` takes the version `repo` leads to now.
  When upstream is not at x, `add <ref>@x` fails and names the version it is
  at.
- B281 [3] With `version.from = "page"`, the version is the groups of `regex`'s
  first match in the text at `repo`, joined with `.`. When `regex` matches
  nothing, or the groups make no version, `add` and `update` fail and change
  no lock.
- B297 [3] With `version.from = "sparkle"`, the version is the
  `sparkle:shortVersionString` of the newest item of the Sparkle feed at
  `repo`, from an element or an attribute. Like Sparkle, oku ranks the items
  by `sparkle:version`, and by the short version when an item names no build.
  An item on a channel, such as beta, or one whose `sparkle:os` is not
  `macos`, does not count. A channel named `stable` or `release` counts as
  none. `regex` does not apply.
- B304 [3] `{{version_major}}`, `{{version_minor}}`, `{{version_patch}}`,
  `{{version_nodots}}`, `{{version_underscores}}`, `{{version_dashes}}` and
  `{{version_partN}}` expand wherever `{{version}}` does. `join` in a
  `redirect`, `page` or `sparkle` version joins its parts, and `join = "+"`
  keeps them apart for `{{version_partN}}`. A variable whose part the version lacks fails and names
  the part.
- B283 [3] When each artifact has a `version` table, `add`, `update` and `sync`
  pin each platform of `[lock] platforms` at its own version, and install the
  host's. `update` moves only the platforms whose upstream moved, and names
  them. `sync` installs the host's locked version and leaves `oku.lock` as it
  was, whatever the host. `outdated` compares the host's version, and
  `add <ref>@x` fails.
- B300 [3] An artifact's `version` table may read GitHub, Gitea or GitLab
  releases or git tags, beside artifacts that follow a redirect, a page or a
  Sparkle feed. `{{tag}}` in its URL is the tag of its own version. The
  platform entry of `oku.lock` keeps that tag, and `sync` on another machine
  downloads from it. oku checks a release file against the digest
  the host reports for it, and `manifest lint` does not warn about it.
- B106 [3] With `[version] tag`, `add` installs the release of that tag, also
  when it is a prerelease, as version `<date>-<commit>`, the day and the first
  seven characters of the commit the tag points at. `update` moves the
  package when the tag points at another commit and prints both versions, and
  changes nothing when it does not. `rollback` returns to the earlier build
  without a download.
- B107 [3] With `[version] tag` and no checksum in the manifest, oku rejects a
  download that does not match the digest the GitHub API reports for that
  asset, and nothing enters the store or profile.
- B108 [3] `sync` of a locked moving-tag package that must download fails when
  the tag has moved since the lock was written, and names `oku update <name>`.
  It installs no newer build under the locked version. With the store path
  present it asks upstream nothing.
- B109 [3] `add <ref>@<version>` on a moving-tag manifest fails when upstream
  is at another version, and names the version upstream is at.
- B110 [4] `manifest lint` rejects `tag` with `git-tags`, with `value` and with
  `strip_prefix`. With `tag` it warns about a missing checksum only for an
  artifact whose `url` is no download of that repo's releases. `manifest bump`
  refuses a manifest with `tag`.
- B213 [4] `manifest lint` warns about every artifact that has neither
  `sha256` nor `sha256_url`, the first one included, and about no artifact
  that has one.
- B282 [4] `manifest lint` rejects `join` that is not `.`, `+`, `-` or `_`, or
  that has no `regex` outside `sparkle`. It rejects `redirect` and `page`
  without `regex`, and
  `redirect`, `page` and `sparkle` with a `repo` that is no http(s) URL, or
  with `strip_prefix` or `tag`. It rejects a `regex` with no group, one that
  does not compile, and one with another `from`. It warns about their
  artifacts without a checksum.
- B284 [4] `manifest lint` rejects an artifact `version` beside `[version]` or
  `[build]`, missing from another artifact, with a `from` that is a moving tag,
  a branch or a registry, with keys other than `from`, `repo`, `regex` and
  `strip_prefix`, with a `repo` that does not fit its `from`, or with a
  `regex` beside a source of releases or tags.
  `manifest bump` refuses such a manifest.
- B285 [4] An artifact `version` given as a string, such as `"1.0.0"`, fails
  `manifest lint` and `add` with an error that says it must be a table.
- B22 [3] Every profile change creates a generation. `oku rollback` restores
  the previous one, `oku rollback <n>` a named one. Rollback restores
  `oku.lock` with it, so a following `sync` changes nothing, and it never
  edits `oku.toml`.
- B23 [3] `oku gc` deletes store paths referenced by no generation of any
  profile, and nothing else. `--keep N` first deletes all but the newest N
  generations and the active one. `--dry-run` deletes nothing.

## Publishing

- B25 [4] `oku add github:owner/repo` on a repo with no manifest infers one
  from release assets, says so, prints it with `--verbose`, and marks the lock
  entry `inferred`. The lock stores the manifest text, and `sync` installs from
  it without inferring again.
- B174 [4] With several assets for a platform, inference prefers a command
  line build over a desktop app. With one checksum file for each OS, it reads
  the one of the asset's OS.
- B197 [4] Among shared checksum files, inference takes one that names the
  asset's OS and arch, then one that names its OS or arch alone, then a generic
  one such as `checksums.txt` or `SHA256SUMS`, and never one that names another
  OS or arch.
- B275 [4] When GitHub reports a digest for an asset, inference skips a
  checksum file that states another digest for it, and oku checks the
  download against GitHub's digest.
- B198 [4] With several assets that fit a platform equally, inference skips
  one whose name says `desktop`, `app`, `gui`, `installer` or `setup`,
  and takes the smaller of the rest when the host reports sizes. A comment
  lists every other asset that fits the host, in any format, and leaves out a
  universal build beside one for the arch.
- B222 [4] Inference takes a `.deb`, `.rpm`, `.msi`, `.dmg`, `.pkg` or
  AppImage asset when no archive or single binary fits, a `.dmg` before a
  `.pkg`. An installer's format names its OS, and one that names no arch fits
  amd64 and arm64 of that OS. An asset that holds `Name.app` gives
  `app = ["Name.app"]`, the files inside the bundle are no program, and oku
  does not strip a bundle at the top of the asset. A `.deb` or `.rpm` symlink
  to an absolute path names a file of the package.
- B199 [4] When an install from a manifest oku inferred in the same run fails,
  the error says which asset oku chose for this machine, which other assets
  fit, and the `oku add --asset` command that picks one. With `--verbose` it
  ends with the inferred manifest.
- B230 [4] When a release has no asset for the host, `add` and `sync` infer
  the manifest from the first `[lock]` platform that has one, and oku pins the
  package without installing it (B271).
- B223 [4] `add`, `update` and `sync` open an asset for the host and for each
  `[lock]` platform, and for no other. A platform outside the lock gets an
  artifact when its asset has the ending of an opened one, and none
  otherwise. `manifest init` opens one for every platform.
- B26 [4] Inference that finds no asset for the host or a `[lock]` platform
  fails and lists the asset names it saw.
- B27 [4] `oku manifest init --from <repo>` writes the inferred manifest to a
  file.
- B290 [4] `oku add <ref> --manifest` prints the manifest `add` would use. One
  that oku infers covers every platform, and `oku add` of the saved file
  installs the same package.
- B112 [4] `oku add --asset <glob>` and `--bin <name>` choose the asset and the
  program of an inferred manifest. When inference fails, the error names the
  flag to pass.
- B278 [4] An inferred manifest also exposes each executable beside its
  program whose name starts with the program's name and a `-`, such as
  `age-keygen` beside `age`. Other executables stay out.
- B279 [4] `--bin` may be given once per program. `oku update` infers a
  package's next version with the `--asset` and `--bin` it was added with.
- B113 [4] `oku add github:owner/repo@version` on a repo with no manifest infers
  from that version's release, not from the newest one.
- B126 [4] `oku add npm:@scope/name` infers a manifest from the npm registry
  with one program for each `bin` entry. With `runtimes.node` in `config.toml`
  the programs run through that package, which stays out of the user's
  profile. A relative path in `runtimes.node` starts at the directory of
  `config.toml`.
- B255 [4] `[runtimes] node` in `oku.toml` or a list it includes names the
  node of npm packages before `config.toml` does. A relative ref starts at the
  list that names it, and in a list from a repo it names the file of that repo.
  `oku sync <repo>` turns the relative refs of the lock it adopts into files of
  the repo, so the npm packages that lock pins run on the repo's node.
- B183 [4] The manifest that `oku.lock` stores for an npm package names a
  `runtimes.node` file inside the list's directory relative to that directory,
  so `sync` installs from the lock under another home directory, and from a
  project's lock in another checkout of the project.
- B129 [4] With `runtimes.node`, an npm package that lists dependencies is
  built by an npm vendor step with `package`. It installs the package with its
  dependencies as of the version's publish time, runs no install scripts, and
  `oku.lock` pins a digest of what it installed.
- B256 [4] `oku add pypi:<name>` installs a Python package with uv, with its
  dependencies as they were one second after the version's upload, and runs
  its console scripts through `[runtimes] python`, which stays out of the
  profile. `oku.lock` pins a digest of the install that is the same under
  another home directory.
- B257 [4] A pypi package follows the index's versions. A prerelease and a
  yanked version are never the newest, and `pypi:<name>@<version>` takes one.
- B258 [4] `oku add go:<package path>` finds the module that holds the
  package, downloads it with every module it needs through the go command that
  `[runtimes] go` names, pins a digest of the downloads that leaves out the
  checksum database's files, and installs the program with `go install`
  offline. go stays out of the profile.
- B259 [4] A go program follows the module's tagged versions. A version with a
  `-` is never the newest, and `go:<path>@<version>` takes one.
- B260 [4] `oku add cargo:<name>` downloads the crate's `.crate` file, checks
  it against the sha256 that crates.io publishes and refuses one that differs,
  vendors its dependencies, and builds its programs with the cargo that
  `[runtimes] rust` names. rust stays out of the profile.
- B261 [4] A crate follows crates.io's versions. A yanked version and one with
  a `-` are never the newest, and `cargo:<name>@<version>` takes one. A crate
  with no programs fails and says it is a library.
- B291 [4] `oku add cask:<token>` translates the Homebrew cask into a manifest
  of oku's own and installs from it with no brew. Each platform the cask builds
  for, macOS arm64 and Intel and Linux, gets an artifact with the vendor's
  download, and its `app`, `binary`, `font`, `manpage` and `app_image` become
  `app`, `bin`, `font` and `man`. oku says it translated the recipe, and the
  lock stores the manifest text.
- B292 [4] `oku add scoop:<name>` translates a Scoop manifest the same way,
  from main and else extras, and `scoop:<bucket>/<name>` names any bucket that
  Scoop knows by name except nonportable. Each arch gets an artifact, with
  `strip` from `extract_dir`. Each shortcut to a program becomes a program and
  a Start Menu launcher, and every program in a folder of `env_add_path` is a
  program. A program with arguments becomes a `bin` table with `run` and
  `args`, and one whose arguments name a folder of Scoop's runs without them.
  `depends` becomes `[runtime] deps` on `scoop:` refs, without the packages
  Scoop needs only to unpack.
- B293 [4] A translated manifest follows the recipe's own rule for new
  versions, the cask's livecheck or Scoop's checkver, when it reads GitHub
  releases, follows a redirect, reads a Sparkle feed, or matches a regex or
  one JSON key at a URL. A recipe with no rule follows the GitHub releases
  that its download comes from. A rule whose URL differs by platform gives
  each artifact its own `version` table.
- B294 [4] A translation uses a URL template only when it gives back the
  recipe's own download for the recipe's version on that platform. When no
  template does, oku cannot translate the rule for new versions, or a file
  inside the download is named after the version, the manifest pins the
  recipe's version with its downloads and sha256 digests, and a comment says
  why. `oku update` translates the recipe again.
- B295 [4] oku runs no script of a recipe. A script that only sets up the
  app, such as a cask's `postflight` or Scoop's `persist`, stays out of the
  manifest, and a comment in the manifest names it. A recipe whose files an installer or a
  script makes fails and says so: a cask's `installer` or kernel extension,
  and Scoop's `installer` with a `file`, `innosetup`, or a script that unpacks
  the download on every arch.
- B296 [4] `oku manifest init --from cask:<token>` or `--from scoop:<name>`
  writes the translated manifest.
- B298 [4] A cask that ships a `.pkg` or a `suite` translates on macOS, where
  oku opens the download. It takes the apps that the package installs into
  Applications, or the apps in the suite, and the files that the cask links
  from where the package installs them. With neither, it takes the programs
  under the package's `bin` folders, or else the one program named after the
  cask. Programs from two parts of one package fail, since oku keeps the
  parts apart. A `binary` in the folder that an `artifact` stanza moves is
  the file of the download.
- B299 [4] The `@` of a cask ref is part of the cask's name, as in
  `cask:temurin@21`, so a cask ref takes no `@version`.
- B302 [4] `oku add aqua:owner/repo` translates the repo's entry in the aqua
  registry into a manifest that follows the repo's GitHub releases. oku reads
  the rule for the newest releases. Each platform gets an artifact with the
  file name the entry gives it, `bin` from `files`, and `sha256_url` from the
  checksum. An entry whose template oku cannot translate fails and names the
  template. A `github:` ref never reads the registry.
- B303 [4] `oku add winget:Publisher.Package` translates the newest version
  of a package of winget's community manifests into a manifest for Windows,
  one artifact per arch. A portable program, a zip of them, or an MSI whose
  programs oku finds on Windows becomes `bin`. oku refuses a setup program.
  The manifest follows the GitHub releases of its download, or else pins the
  version with its sha256.
- B305 [4] A translation maps the parts of a version that a cask's or a
  Scoop manifest's URL uses, such as `#{version.csv.second}` or
  `$cleanVersion`, to the version variables. A cask's comma version becomes
  a `+` version, and a livecheck that only joins its regex's groups, or a
  Sparkle livecheck with no block, becomes a version source with
  `join = "+"`.
- B306 [4] An answer from a source that lacks a field oku needs, such as a
  cask without `url` or a release without a tag, fails `add` and `update`
  before anything changes. The error names the source and each missing field,
  and says its format may have changed.
- B307 [4] oku asks each source for a format version where the source has
  one, and refuses a version it does not read. It asks GitHub's REST API for
  2026-03-10 and says so when GitHub retires it. It reads PyPI's Simple API
  in JSON at major version 1, winget manifests at major version 1, and the
  aqua registry at v4.
- B264 [4] A `[runtimes]` entry may be a table with `ref` and a `version`
  constraint, in a list or in `config.toml`. `add` and `update` then build and
  run with the newest version of that package that the constraint allows, and
  `oku.lock` pins it.
- B212 [4] An npm vendor step with `package` may name dependencies in
  `scripts`. After the install oku runs their install scripts with the network
  on, the approval prompt says so, and `oku.lock` marks the build impure. A
  step without `scripts` runs none. A name that is not the package or one of
  its dependencies fails the build and is named.
- B189 [4] When the host builds a package whose vendor steps are all `npm` and
  no `run` step comes before them, oku pins the `vendor_sha256` of each other
  lock platform too. It runs the npm steps for that platform in a temporary
  directory, with no scripts, and adds nothing to the store.
- B127 [4] Without `runtimes.node`, the programs of an npm package run the
  `node` on `PATH`, and `add` says how to pin one.
- B186 [4] A download that is the program itself may list one table `bin`
  instead of a path. The file keeps the name it has in the URL, without the
  suffix of a compression, and `run` or `args` name it as `{{pkg}}/<name>`.
- B117 [4] `oku add <url>` on a URL that is no manifest infers a one-artifact
  manifest for the host from the download, says so, prints it with `--verbose`,
  and warns that it trusted the download. A URL that holds a manifest stays a
  manifest whatever its name, and a URL that does not exist fails as not found.
- B325 [4] A download that is not an archive must start as a Linux, macOS or
  Windows program or a `#!` script, or be a `.cmd`, `.bat` or `.ps1` script,
  or oku refuses it at `add` and at install. A single file that a `bin` table
  runs through a runtime only has to not be a web page. For a web page oku says so, and for the
  page of a repo names the forge ref to add. A compressed download that does
  not decompress fails with the reason.
- B326 [4] An inference error for a `[lock]` platform names that platform,
  and says "this machine" only for the host. It suggests `--asset` only for
  the host, and for a `[lock]` platform the package's `when` or a manifest.
- B327 [4] A download URL whose file names no version takes the version of
  the nearest folder that names one, such as `jq-1.8.1` or `v1.19.0`, and
  version `0` without one.
- B120 [4] Inference takes a macOS universal build for both darwin arches and a
  `windows-gnu` asset for Windows, and never reads a signature file as the
  checksum file.
- B121 [4] Inference installs a release asset that is one compressed binary.
- B28 [4] `oku manifest lint` rejects: unknown keys, a step with zero or
  several type keys, a windows-reachable `run` without `shell`, unknown
  template variables, an artifact with no output keys, a `fetch` step without
  sha256 or sha256_url, or with both. A missing checksum source and an empty description are warnings and
  do not fail it.
- B218 [4] `manifest lint` rejects shell paths together with `generate`, `name`
  without `generate`, and an unknown template variable in `generate`.
- B29 [4] `oku manifest bump` rewrites a static version and its checksums to
  the newest upstream release, or to `--to <version>`. It keeps the file's
  comments, and it refuses a manifest that discovers its versions.
- B125 [4] `oku manifest hash <url | file>` prints the download's `sha256` and
  `integrity` as lines a manifest accepts.
- B119 [4] `oku manifest bump --repo <ref>` reads releases from any forge ref.
- B30 [4] `oku source add <alias> <ref>` makes `alias/name` resolve to the
  manifest `name` in that collection, and `add` accepts `alias/name` refs.
  `oku.toml` gets the full ref, so the list works without the alias.
- B31 [4] `oku search <term>` matches names and descriptions across the user's
  sources, and nothing else.

## Builds

- B35 [5] With no matching artifact, or with `--from-source`, oku runs
  `[build]` steps in order and installs what `install` steps name. The lock
  records the strategy, so `sync` builds on that platform too.
- B36 [5] A missing `needs` tool fails before any step runs, naming the tool.
- B274 [5] `[build] when` limits the build to matching platforms, in the forms
  of a list's `when`. oku neither builds nor pins a build for any other.
- B208 [5] A `needs` tool is on the build's `PATH` as a link under its own
  name in a directory oku makes for the build. The other programs of the
  tool's directory are not on `PATH`.
- B37 [5] Deps are realized first. The build finds their headers, libraries
  and pkg-config files with no manifest-side flags, and the built binary finds
  their shared libraries at runtime from any working directory.
- B202 [5] After a build, a file in `bin` or `lib` that loads a shared library
  of another store package prints one warning per package on stderr, naming
  the file and the package, unless `runtime.deps` names that package. The
  install succeeds. A download is never checked, and Windows never warns.
- B38 [5] Deps are absent from the profile. `oku why <name>` names the
  packages that pull a dep in. `gc` keeps a dep while a generation uses it.
- B39 [5] Two packages depending on different versions of one dep both install
  and both run, and `sync` restores both versions from the lock.
- B40 [5] An unsatisfiable dep version constraint fails, naming the constraint
  and the versions found.
- B41 [5] The first install of a manifest with `run` steps prints them and
  asks for approval. The same manifest hash is never asked twice. A changed
  manifest asks again. A dep that builds asks for itself.
- B42 [5] Non-interactive runs refuse unapproved `run` steps unless `--yes`.
- B217 [5] A build `install` step accepts the same `completions` forms. With
  `generate`, the command runs in the source directory after the files are
  copied, with the package's `bin` first on PATH, and the step appears in the
  approval prompt.
- B43 [5] A step with a non-matching `when` is skipped.
- B44 [5] A failing step aborts the build, reports the step index and its
  output, and leaves store and profile unchanged.

## Sandbox

- B50 [6] On Linux and macOS a `run` step cannot reach the network and cannot
  read the user's home directory. On a host with no sandbox oku builds and
  prints a warning that names the reason.
- B248 [6] On Linux and macOS a `run` step cannot write outside its source
  directory, its temporary `HOME` and `TMPDIR` and `{{prefix}}`: not to
  another package in the store, and not to a directory the user can write to.
- B249 [6] On Linux a `run` step sees no `/run/user`. On macOS it cannot run
  `launchctl` or open an app.
- B51 [6] A `fetch` step without sha256 or sha256_url fails lint. With one, it
  may download, and the file must match that sha256, or the digest that the
  checksum file at sha256_url gives for the file's name.
- B52 [6] A `vendor` step's output hash is pinned in the lock. A later
  mismatch fails the build and keeps nothing in the store. `update` accepts
  the new hash.
- B53 [6] `network = true` on a `run` step is shown in the approval prompt and
  marks the package impure in `oku info`.
- B54 [6] Build env contains only oku's variables, the link environment, step
  `env`, and a PATH of `deps`, `needs` and the system directories of B207. A
  rustup-managed toolchain adds `RUSTUP_HOME`.
- B207 [6] The build `PATH` ends in `/usr/bin`, `/bin`, `/usr/sbin` and
  `/sbin`, in that order, so `sysctl` is found.
- B55 [6] `oku manifest test` builds into a throwaway store and reports
  success or the failing step. It leaves the user's store, profile, list and
  lock untouched, and it leaves no build directory behind.

## Projects and activation

- B60 [7] Inside a directory tree with an `oku.toml`, `add`, `remove`, `sync`
  and `list` act on that project. `--global` overrides. The config directory is
  never a project, and `oku sync <ref>` is refused inside one.
- B61 [7] With `oku hook <shell>` loaded, entering an allowed and synced
  project prepends its profile `bin` to PATH and exports its packages'
  `[env]`. Leaving restores both. A second prompt in the same directory changes
  nothing.
- B62 [7] Entering a project that is not allowed changes nothing and prints a
  hint to run `oku allow`, once.
- B63 [7] Editing an allowed `oku.toml` revokes the allow until `oku allow`
  runs again.
- B64 [7] Entering an allowed project whose profile is behind its lock changes
  nothing and prints a hint to run `oku sync`.
- B65 [7] The hook performs no network access and runs no manifest code.
- B66 [7] `oku env` prints the exports the hook would apply, for bash, zsh and
  fish.
- B69 [7] A manifest whose `[env]` sets `PATH`, `LD_PRELOAD` or another variable
  that controls other programs is rejected.
- B308 [7] Inside an allowed project, the hook sets the variables of its
  `oku.toml` `[env]`, with `${NAME}` and `${NAME:-default}` read from the shell
  and from other keys, and unsets a key set to `false`. Leaving gives each
  variable back the value it had before.
- B309 [7] `KEY = { prepend = [...] }` puts the entries in front of a list
  variable, a relative one from the directory of the `oku.toml`. The project's
  entries come before its `bin`. Leaving removes only those entries.
- B310 [7] `KEY = { required = "hint" }` prints the variable and the hint once
  while it is unset or empty, and `oku exec` refuses to run until it is set.
- B311 [7] The global `oku.toml` `[env]` applies in every directory. Inside a
  project the project's packages override it, and the project's `[env]`
  overrides those. `oku exec` sets the same variables.
- B312 [7] oku rejects a list whose `[env]` sets `PATH` without `prepend`,
  `LD_PRELOAD`, an `OKU_` variable or another variable that controls the shell,
  and an included list with `[env]`.
- B313 [7] `oku env --json` prints each variable the directory sets, with
  `null` for one it unsets, and `oku env --dotenv` prints them as a `.env`
  file.
- B314 [7] In a shell that an older oku's hook set up, the next prompt takes
  over the old hook's state, and leaving the project removes what the old hook
  added.
- B315 [7] A project whose list has `[env]` and no packages applies without an
  `oku.lock`.
- B316 [7] `[[env.file]]` loads `.env` files in order before `[env]`, a
  later file over an earlier one and `[env]` over both. A file takes
  `export`, `#` comments, bare values that end at ` #`, single quotes as they
  are, and double quotes over several lines with escapes. Bare and
  double-quoted values expand `${NAME}`, `${NAME:-default}` and `$NAME`.
- B317 [7] A missing `.env` file prints a hint and makes `oku exec` refuse,
  unless it has `optional = true`. `unless = [names]` skips the file while one
  of the names is set and not empty, and `oku exec` then leaves out what the
  shell's hook set from it.
- B318 [7] `oku allow` covers each `.env` file of the project that git
  tracks, and says for each file whether git tracks it. A change to a
  tracked file, or git starting to track a file, stops the hook until a new
  allow. The hook applies an untracked file's changes at the next prompt.
- B319 [7] oku refuses a whole `.env` file that sets a variable a list may
  not set, such as `LD_PRELOAD`, with a hint.
- B320 [7] `secret = true` decrypts an `[[env.file]]` with age or with sops
  before oku reads it as a `.env` file. A file that does not decrypt prints
  the reason as a hint and makes `oku exec` refuse.
- B321 [7] An `[[env.file]]` with `scope = "exec"` loads for `oku exec`
  alone. The hook and `oku env` leave its variables out.
- B322 [7] `OKU_ENV=<env>` applies the `[env]` of the project's
  `oku.<env>.toml` over its `oku.toml`, and `oku.local.toml` applies over
  both, in the hook, `oku exec` and `oku env`. A missing `oku.<env>.toml`,
  `OKU_ENV=local`, or an overlay that holds anything but `[env]` prints a hint
  and makes `oku exec` refuse.
- B323 [7] `oku allow` covers every overlay that git tracks, whatever
  `OKU_ENV` names. An untracked overlay, and the `.env` files it loads, change
  without a new allow.
- B324 [7] The hook sets `OKU_PROJECT` to the project's directory while the
  project applies, and removes it outside one or when the project does not
  apply. `oku exec` sets it for its command in a project.
- B67 [7] Project packages shadow global ones on PATH while active.
- B68 [7] The hook exports `[env]` of global packages in every shell.
- B219 [7] The hook loads the completions under the global profile
  `share/completions` for bash, zsh and fish, and the completions of `oku`
  itself in every shell. In zsh the hook registers the package files by name
  once `compinit` has run, whether the hook line comes before or after it.

## Apps, fonts, services

- B70 [8] A package `app` appears in the OS launcher for the current user.
  `remove` and `rollback` take it away again.
- B175 [8] A `font` or `man` entry may be a pattern such as `fonts/*.ttf` or
  `**/*.otf`. `*` and `?` match within one path segment and `**` matches any
  depth. oku resolves it once at install, and a pattern that matches no file is
  an error that names the pattern.
- B71 [8] A package `font` is usable by applications for the current user.
  `remove` and `rollback` take it away again.
- B72 [8] oku unpacks dmg, pkg, deb, rpm and AppImage downloads without
  executing anything inside them. `.msi` follows in step 9.
- B301 [8] When the top of a `.dmg` holds a `.pkg` and no app, oku unpacks
  each package into a folder named after it, so `bin` and `app` name files of
  its payload. oku keeps the packages of an image with an app as files.
- B287 [8] A disk image that a killed oku process left mounted does not stop
  the next install of it. That install detaches it and unpacks the image, and
  `oku gc` detaches it too.
- B73 [8] A package with `service = true` in `oku.toml` is running after
  `sync` and after the next login. Without it, the service is installed and
  stopped.
- B184 [8] oku installs a `[[service]]` with `when` on the platforms that
  `when` matches and on no other. Two services of a package may share a name
  when their `when` tables match different platforms.
- B276 [8] A `[[service]]` `when` and a build step's `when` take one table or
  an array of tables, as a list's `when` does, and match a platform when any
  table does.
- B185 [8] On a Linux machine that systemd does not run, or without
  `systemctl`, oku installs a package that ships a service, installs no
  service, and says so once with the reason.
- B170 [8] The `args` and `env` of a service expand `{{home}}`, `{{config}}` and
  `{{data}}`. On macOS and Linux a service of the user finds the programs of
  the global profile on its `PATH`, unless its `env` sets `PATH`.
- B74 [8] `oku service start|stop|restart|status|logs` behave the same on all
  three OSes.
- B203 [8] `oku service start` and `restart` look at the service again one
  second after starting it. A program that has exited by then is reported as
  `<name> started and then exited` with where its log is, and the command
  fails. `status` reports what the manager says and never fails for that.
- B75 [8] System scope needs `--system`. oku names what it will write and
  prompts before elevating. Without the flag oku never elevates. It leaves
  system scope unchanged and lists what is pending.
- B76 [8] Rolling back to a generation restores which services are enabled.

## Windows

- B80 [9] Profile `bin` entries are shims that exec the store binary with
  arguments, stdio and exit code passed through.
- B81 [9] A binary with DLL deps in other store paths, or beside its real file
  in its own download, starts from any working directory, and so does a build
  step that runs such a binary of a dep.
- B288 [9] On Windows, two `.msi` downloads in one sync both unpack, one
  after the other.
- B269 [9] On Windows, a build step with `shell = "pwsh"`, and oku's own steps
  for `npm:`, `pypi:`, `go:` and `cargo:` refs, run PowerShell 7 when it is on
  `PATH` and the Windows PowerShell 5.1 that Windows ships otherwise. The
  Windows live test builds a `go:` and a `pypi:` ref with PowerShell 7 off
  `PATH`.
- B82 [9] `oku hook pwsh` gives B61 to B68 in PowerShell, on Windows, macOS
  and Linux, and keeps `$LASTEXITCODE` across the prompt.

## Cache and signing

- B85 [10] With a cache configured and its key trusted, a package present in
  the cache is substituted and no build step runs.
- B86 [10] A cache entry with a missing, invalid or untrusted signature is
  ignored and the package builds locally.
- B87 [10] `oku cache push` writes the signed closure of the named packages
  into a directory. Impure packages are refused.
- B88 [10] A non-relocatable entry built under a different store root is never
  substituted.
- B89 [10] A manifest with `signing_key` has its artifacts verified against
  it. The key is pinned in the lock, and a changed or dropped key stops `sync`
  and `update` until `--accept-key`.

## Tooling

- B90 [11] `oku shell <ref>...` opens a shell with those packages on PATH and
  leaves `oku.toml`, the lock and every profile unchanged.
- B91 [11] `oku doctor` reports store root, sandbox availability, hook status,
  PATH order problems and broken profile links.
- B92 [11] `oku self update` replaces the oku binary after verifying its
  signature.
- B246 [11] `oku self update` refuses a binary whose signature the release key
  made for another release, and names both releases.
- B111 [11] `oku self update --nightly` replaces the oku binary with the build
  of the `nightly` release after the same signature check, and changes nothing
  when the running binary is that build.
- B221 [11] On a nightly build, `oku self update` without a flag refuses and
  names `--nightly` and `--release`. `--release` goes back to the newest
  release. `--to <tag>` takes that release, older or newer, after the same
  signature check, and says so when the running binary is that release. After
  a replacement oku prints the link to the release's notes.
- B232 [11] While oku asks the user something, such as a build approval or
  the password for `sudo`, the rows and build logs of the packages that
  install in parallel wait, and appear below the answer once the user gives it. Two
  packages that need an approval ask one after the other.
- B176 [11] While oku waits for a download, a lookup, a clone, an unpack, a
  cache or a build step, stderr says what it waits for and for which package.
  A terminal shows one line per package, from its first wait to its last, with
  the time and the bytes so far, which goes away when the wait ends. A URL shows
  as the file it names, and a download of known size gives a percentage.
  Anything else gets one line for each wait.
- B93 [11] The install script puts one static binary in place and prints the
  hook line for the user's shell. It needs no root and edits no existing file.
- B220 [11] The install script prints the installed version, and ends with the
  next commands: `oku doctor`, a first `oku add`, `oku self update`. When the
  startup file loads the hook already, it says so instead of printing the line
  again.

## Transactions

- B130 [12] An `add`, `remove`, `sync`, `update` or `rollback` that fails at
  any point leaves the active generation, `oku.lock` and every app, font,
  service, file and setting as they were before the command.
- B131 [12] oku finds every failure it can before the first change: a
  download, a checksum, a build, a template, a target it does not own, a
  setting of the wrong type.
- B167 [12] `sync --dry-run` and `update --dry-run` run every check of a real
  run, print what would change, and change nothing: no generation, no lock, no
  file, no service, no setting. They fail where the real run would fail.
- B132 [12] When a step of the apply fails, oku undoes the steps it made and
  reports the error of the failed step.
- B133 [12] After an oku process was killed during a change, the next command
  puts the machine back first, and says so.
- B134 [12] When oku cannot undo a step, it names what is left, and
  `oku doctor` reports it until the user resolves it.
- B243 [12] While one oku process changes the machine, another command that
  changes it waits, prints `waiting for oku process <pid> to finish`, and runs
  once the first one ends. A command that only reads, such as `list`, does not
  wait.
- B244 [12] `oku gc` deletes a temporary directory that a killed install left
  in the store.
- B286 [12] `oku gc` deletes the `oku-*` entries in the system's temporary
  directory whose oku process has ended, and keeps those of a process that
  runs. `--dry-run` names them and deletes nothing.

## Files

- B135 [13] A `link` entry makes the target a link to the source. An edit to
  the source shows at the target without a sync.
- B136 [13] A `text` entry writes that text to the target, with `mode` when
  given.
- B137 [13] oku refuses a target that exists and that it did not write, names
  it, and changes nothing.
- B138 [13] A target whose entry leaves the list is gone after `sync`.
  `rollback` brings it back with the bytes it had.
- B139 [13] A target starts with `{{home}}`, `{{config}}`, `{{data}}`,
  `{{appdata}}` or `{{localappdata}}`. One that this OS lacks is an error,
  unless `when` excludes the entry.
- B140 [13] A link source may start with `{{pkg.<name>}}`, and follows that
  package to its new version on `update`.
- B141 [13] On Windows a linked directory is a junction and a file is a copy.
  oku stops before any change when a copy was edited by hand, and names it.
- B192 [13] A `text` or a `render` entry may hold `vars`, a table of strings
  that overrides `[vars]` for that entry. Two entries may share a target when
  their `when` clauses differ. `vars` on a `link` or a `secret` entry is an
  error.
- B204 [13] A directory that oku creates above a `text`, `render` or `secret`
  target is `0700` when the entry is a `secret` or has a `mode` that gives
  group and others nothing, and `0755` otherwise. An existing directory keeps
  its mode. Modes do not apply on Windows.
- B205 [13] A `text`, `render` or `secret` entry with `mode = "0755"` is a file
  the user can run, and a sync that changes only the mode applies it.
- B171 [5] An artifact's `lib`, `include` and `share` entries, files or
  directories, fill those directories of the package, and a build that depends
  on the package finds its headers and libraries there.
- B166 [13] An artifact with `data = true` exposes nothing and puts nothing on
  `PATH`, and a link source reaches its files with `{{pkg.<name>}}`. An
  artifact with no output and no `data` key is an error that names the key.
- B142 [13] A project list with `[files]`, `[vars]` or a settings table is an
  error.

## Variables and templates

- B143 [14] A `render` entry writes its template with each `{{name}}` replaced
  by the value from `[vars]`. The file is read-only.
- B144 [14] A name that is not set fails before any change, with the file and
  the line.
- B145 [14] A table under `[vars]` gives names joined by a dot, and
  `{{ name }}` with spaces is the same as `{{name}}`.
- B146 [14] Changing a variable re-renders every file that uses it in one
  generation. `rollback` restores the bytes of the generation before.
- B147 [14] `[vars]` of an include are overridden by a later include and by
  the user's own list.
- B148 [14] The same template and variables give the same bytes on every OS.

- B224 [13] `add`, `sync`, `update` and `rollback` print one line for each
  file, secret, setting, app, font or service the change wrote, changed,
  removed or restored, and `sync` ends with the packages, files and settings
  the profile holds.
- B225 [13] `oku generations` counts the files and settings of each
  generation and lists which came, went or changed beside the packages.
  `--json` lists them, and `rollback` reports the same way.
- B226 [13] `oku list --files` shows each file entry for this machine with
  its kind, source and the list that declares it. `oku list --settings` shows
  each setting of this OS with its value and the value the key had before oku
  wrote it. Both take `--json`.

## Settings

- B149 [15] After `sync` a key under `[defaults.<domain>]` has the value and
  the type from the list: boolean, integer, float, string, array or table.
- B168 [15] A key under `[defaults-currenthost.<domain>]` is set for this Mac
  only, the way `defaults -currentHost` does, and leaves the domain of the user
  alone. B150 holds for it.
- B150 [15] A key that leaves the list gets back the value it had before oku
  first wrote it, or is deleted when it had none. `rollback` does the same.
- B151 [15] oku skips the settings tables of another OS without an error.
- B152 [15] A `[registry]` key outside `HKCU` is an error.
- B153 [15] `[registry]` on Windows and `[dconf]` on Linux behave as B149 and
  B150 describe.

## Secrets

- B154 [16] A `secret` entry writes the decrypted value of `key` to the target.
  Without `key` it writes the whole decrypted file.
- B155 [16] oku decrypts an age file with no other program installed, and a
  sops file through `sops`. It tells the two apart by the file's content.
- B156 [16] A `text` or a template may use `{{secret.<name>}}` for a value that
  `[secrets]` names. A name that `[secrets]` lacks is an error.
- B157 [16] A file that holds a secret can be read by the user alone: mode
  `0600` unless `mode` is given, and on Windows an access control list with the
  current user only.
- B158 [16] The generation holds the encrypted file. No generation, ledger
  entry, output or error holds the decrypted bytes.
- B159 [16] A secret that cannot be decrypted fails before any change. The
  error names the encrypted file and the `key`.
- B160 [16] After the encrypted file changed, `sync` writes the new value.
  `rollback` writes the value of the generation it returns to.
- B161 [16] A secret whose entry leaves the list is deleted with its target.
  `self uninstall` deletes every decrypted secret.
- B162 [16] oku finds `sops` in the packages of the list before `PATH`, so the
  sync that installs sops can also decrypt.
- B163 [16] oku reads the age identities from `SOPS_AGE_KEY_FILE`, else from
  `{{config}}/sops/age/keys.txt`, on every OS and for both formats.
- B164 [16] `oku doctor` reports a list with secrets on a machine without an
  identity file, or without `sops` when a secret is a sops file.
- B165 [16] A project list with `[secrets]` or a `secret` entry is an error.

## Uninstall

- B94 [1] `oku self uninstall` lists what it will remove, asks once, then
  removes every store path, profile, cache, trust record and the oku binary.
  `--yes` skips the question.
- B95 [8] Uninstall stops and unregisters every oku service, removes every
  exposed app, font, launcher entry and file, and restores every setting. Afterwards no file written by oku
  remains outside the project lists named in B97.
- B96 [1] Uninstall keeps the global `oku.toml` and `oku.lock` when given
  `--keep-list`, and says where they are.
- B97 [7] Uninstall never touches a project's `oku.toml` or `oku.lock`.
- B98 [8] With system-scope items or a shared root present, uninstall names
  them and prompts for elevation. Declining removes everything in user scope
  and prints what is left.
- B99 [7] Uninstall ends by printing the hook line to delete from the shell
  rc file, when `oku doctor` would have found one.
- B100 [7] A shell with a stale hook line and no oku binary starts without an
  error.
- B102 [1] When the global profile `bin` is on PATH, uninstall ends by
  printing that PATH entry for the user to remove.
- B101 [9] On Windows the oku binary is gone from its path once the command has
  exited, and the renamed file a few seconds later.
- B105 [11] The one hook line in a shell's startup file is the whole shell
  setup. After it, `oku` and the programs of the global profile run by name,
  before any program of the same name elsewhere, also when `PATH` held their
  directories behind others already. Loading it twice changes nothing. The installer, `oku add` and `oku doctor`
  print that line with the file it goes into.
- B173 [5] In a build on macOS, pkg-config resolves zlib, bzip2, expat,
  libxml-2.0, sqlite3, libcurl and ncurses of the OS, with the version of the
  SDK on the machine. A dep of the same name comes first.
- B201 [1] A `sha256_url` file may be a JSON object that maps file names to
  sha256 digests, or a JSON array of objects with `name` and `sha256` fields.
  oku reads the digest of the download's file name from it, and a download
  that differs is rejected.
- B172 [5] A `build.source` archive takes `sha256`, or `sha256_url`, or
  neither. With neither, oku pins the digest of the first download in
  `oku.lock`, says so, and refuses another digest for that version until
  `oku update <name>`.
- B169 [5] A file that oku unpacks keeps the time its archive gives it, so
  `make` in a release tarball does not take a generated file for stale.
- B104 [5] A `patch` step applies a unified diff to the source, the same on
  every OS. A hunk that does not fit fails the build and names the file.
- B103 [11] With `--json`, `list`, `info`, `why`, `generations`, `search`,
  `source list`, `cache list`, `key list`, `service list`, `service status` and
  `doctor` print JSON on stdout. An empty result is `[]`, and exit codes do not
  change.
