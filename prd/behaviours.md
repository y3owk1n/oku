# Behaviours

What oku promises. Each line is testable through the CLI. The tag is the build
order step in `prd/product.md`.

## Install and remove

- B1 [1] `oku add ./pkg.toml` realizes the artifact matching the host selector
  and its `bin` entries run from the profile `bin`.
- B2 [1] An artifact whose download does not match its sha256 is rejected and
  nothing enters the store or profile.
- B3 [1] A manifest with no matching artifact and no `[build]` fails with an
  error naming the host platform.
- B4 [1] `oku remove <name>` drops the package from the profile. Its store
  path stays until `gc`.
- B5 [1] `oku list` shows name, version and ref for the active list.
- B6 [1] A failed install leaves the previous profile active and unchanged.
- B7 [1] `man` and `completions` entries appear under the profile `share`.
- B8 [1] oku never requires root outside system scope, and writes only under
  its config, data and cache directories, `oku.toml`, `oku.lock`, and the
  per-user exposure locations in `prd/architecture.md`.
- B9 [1] Two packages exposing the same `bin` name fail the second install
  with an error naming both.

## Refs, list and lock

- B10 [2] `add` accepts file, https, `github:`, `codeberg:`, `gitea:`,
  `gitlab:` and `git+` refs.
- B114 [2] `github:host/owner/repo` reads a GitHub Enterprise Server at `host`.
  oku sends it `GH_ENTERPRISE_TOKEN` and never `GITHUB_TOKEN`.
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
- B13 [2] `sync` stops with an error when a fetched manifest's hash differs
  from the lock. `update` clears it.
- B14 [2] A checksum learned on first use is written to the lock and enforced
  afterwards.
- B15 [2] `sync` on a platform missing from the lock resolves it and appends a
  platform entry without touching existing ones.
- B16 [2] `include` merges the named lists. A local entry overrides an
  included entry of the same name. `sync` stops when an included list changed
  since the lock, and `remove` refuses a package only an include declares.
- B17 [2] An entry whose `when` does not match the host is skipped by `sync`
  and stays in the lock for other platforms.
- B18 [2] `oku sync <ref>` on a machine with no global list adopts that list
  and its lock, then syncs. Two machines of the same platform doing so end
  with identical store hashes. On a machine that has a global list it refuses
  and changes nothing.
- B19 [2] A relative file ref in a list resolves against that list's
  directory. A list from a URL or a repo that names a local path is an error.
  `add` stores a file inside the list's directory relative to it, in the list
  and in the lock.

## Versions and generations

- B20 [3] With `[version] from`, `add` picks the newest discovered version and
  `add <ref>@x` picks x. Drafts, prereleases and tags that are not versions
  are never picked, and an unknown x fails naming the newest versions.
- B118 [3] `add <ref>@<version>` finds a version that is not on the first page
  of the host's release list, among the newest 100 releases.
- B21 [3] `oku update [name]` re-resolves and rewrites the lock. Without it,
  versions never move, and `sync` installs the locked version without asking
  upstream for versions. A package pinned in `oku.toml` stays on its version
  through `update`.
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
- B22 [3] Every profile change creates a generation. `oku rollback` restores
  the previous one, `oku rollback <n>` a named one. Rollback restores
  `oku.lock` with it, so a following `sync` changes nothing, and it never
  edits `oku.toml`.
- B23 [3] `oku gc` deletes store paths referenced by no generation of any
  profile, and nothing else. `--keep N` first deletes all but the newest N
  generations and the active one. `--dry-run` deletes nothing.

## Publishing

- B25 [4] `oku add github:owner/repo` on a repo with no manifest infers one
  from release assets, prints it, and marks the lock entry `inferred`. The
  lock stores the manifest text, and `sync` installs from it without inferring
  again.
- B26 [4] Inference that finds no asset for the host fails and lists the asset
  names it saw.
- B27 [4] `oku manifest init --from <repo>` writes the inferred manifest to a
  file.
- B112 [4] `oku add --asset <glob>` and `--bin <name>` choose the asset and the
  program of an inferred manifest. When inference fails, the error names the
  flag to pass.
- B113 [4] `oku add github:owner/repo@version` on a repo with no manifest infers
  from that version's release, not from the newest one.
- B117 [4] `oku add <url>` on a URL that is no manifest infers a one-artifact
  manifest for the host from the download, prints it, and warns that it
  trusted the download. A URL that holds a manifest stays a manifest whatever
  its name, and a URL that does not exist fails as not found.
- B120 [4] Inference takes a macOS universal build for both darwin arches and a
  `windows-gnu` asset for Windows, and never reads a signature file as the
  checksum file.
- B121 [4] Inference installs a release asset that is one compressed binary.
- B28 [4] `oku manifest lint` rejects: unknown keys, a step with zero or
  several type keys, a windows-reachable `run` without `shell`, unknown
  template variables, an artifact with no output keys, a `fetch` step without
  sha256. A missing checksum source and an empty description are warnings and
  do not fail it.
- B29 [4] `oku manifest bump` rewrites a static version and its checksums to
  the newest upstream release, or to `--to <version>`. It keeps the file's
  comments, and it refuses a manifest that discovers its versions.
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
- B37 [5] Deps are realized first. The build finds their headers, libraries
  and pkg-config files with no manifest-side flags, and the built binary finds
  their shared libraries at runtime from any working directory.
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
- B43 [5] A step with a non-matching `when` is skipped.
- B44 [5] A failing step aborts the build, reports the step index and its
  output, and leaves store and profile unchanged.

## Sandbox

- B50 [6] On Linux and macOS a `run` step cannot reach the network and cannot
  read the user's home directory. On a host with no sandbox oku builds and
  prints a warning that names the reason.
- B51 [6] A `fetch` step without sha256 fails lint. With one, it may download.
- B52 [6] A `vendor` step's output hash is pinned in the lock. A later
  mismatch fails the build and keeps nothing in the store. `update` accepts
  the new hash.
- B53 [6] `network = true` on a `run` step is shown in the approval prompt and
  marks the package impure in `oku info`.
- B54 [6] Build env contains only oku's variables, the link environment, step
  `env`, and a PATH of `deps`, `needs`, `/usr/bin` and `/bin`. A rustup-managed
  toolchain adds `RUSTUP_HOME`.
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
- B67 [7] Project packages shadow global ones on PATH while active.
- B68 [7] The hook exports `[env]` of global packages in every shell.

## Apps, fonts, services

- B70 [8] A package `app` appears in the OS launcher for the current user.
  `remove` and `rollback` take it away again.
- B71 [8] A package `font` is usable by applications for the current user.
  `remove` and `rollback` take it away again.
- B72 [8] oku unpacks dmg, pkg, deb, rpm and AppImage downloads without
  executing anything inside them. `.msi` follows in step 9.
- B73 [8] A package with `service = true` in `oku.toml` is running after
  `sync` and after the next login. Without it, the service is installed and
  stopped.
- B74 [8] `oku service start|stop|restart|status|logs` behave the same on all
  three OSes.
- B75 [8] System scope needs `--system`. oku names what it will write and
  prompts before elevating. Without the flag oku never elevates. It leaves
  system scope unchanged and lists what is pending.
- B76 [8] Rolling back to a generation restores which services are enabled.

## Windows

- B80 [9] Profile `bin` entries are shims that exec the store binary with
  arguments, stdio and exit code passed through.
- B81 [9] A binary with DLL deps in other store paths starts from any working
  directory.
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
- B111 [11] `oku self update --nightly` replaces the oku binary with the build
  of the `nightly` release after the same signature check, and changes nothing
  when the running binary is that build.
- B93 [11] The install script puts one static binary in place and prints the
  hook line for the user's shell. It needs no root and edits no existing file.

## Uninstall

- B94 [1] `oku self uninstall` lists what it will remove, asks once, then
  removes every store path, profile, cache, trust record and the oku binary.
  `--yes` skips the question.
- B95 [8] Uninstall stops and unregisters every oku service and removes every
  exposed app, font and launcher entry. Afterwards no file written by oku
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
  setup. After it, `oku` and the programs of the global profile run by name, and
  loading it twice changes nothing. The installer, `oku add` and `oku doctor`
  print that line with the file it goes into.
- B104 [5] A `patch` step applies a unified diff to the source, the same on
  every OS. A hunk that does not fit fails the build and names the file.
- B103 [11] With `--json`, `list`, `info`, `why`, `generations`, `search`,
  `source list`, `cache list`, `key list`, `service list`, `service status` and
  `doctor` print JSON on stdout. An empty result is `[]`, and exit codes do not
  change.
