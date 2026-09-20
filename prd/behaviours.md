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

- B10 [2] `add` accepts file, https, `github:` and `git+` refs.
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

## Versions and generations

- B20 [3] With `[version] from`, `add` picks the newest discovered version and
  `add <ref>@x` picks x.
- B21 [3] `oku update [name]` re-resolves and rewrites the lock. Without it,
  versions never move.
- B22 [3] Every profile change creates a generation. `oku rollback` restores
  the previous one, `oku rollback <n>` a named one.
- B23 [3] `oku gc` deletes store paths referenced by no generation of any
  profile, and nothing else.

## Publishing

- B25 [4] `oku add github:owner/repo` on a repo with no manifest infers one
  from release assets, prints it, and marks the lock entry `inferred`.
- B26 [4] Inference that finds no asset for the host fails and lists the asset
  names it saw.
- B27 [4] `oku manifest init --from <repo>` writes the inferred manifest to a
  file.
- B28 [4] `oku manifest lint` rejects: unknown keys, a step with zero or
  several type keys, a windows-reachable `run` without `shell`, unknown
  template variables, an artifact with no output keys.
- B29 [4] `oku manifest bump` rewrites a static version and its checksums to
  the newest upstream release.
- B30 [4] `oku source add <alias> <ref>` makes `alias/name` resolve to the
  manifest `name` in that collection, and `add` accepts `alias/name` refs.
- B31 [4] `oku search <term>` matches names and descriptions across the user's
  sources, and nothing else.

## Builds

- B35 [5] With no matching artifact, or with `--from-source`, oku runs
  `[build]` steps in order and installs what `install` steps name.
- B36 [5] A missing `needs` tool fails before any step runs, naming the tool.
- B37 [5] Deps are realized first. The build finds their headers, libraries
  and pkg-config files with no manifest-side flags, and the built binary finds
  their shared libraries at runtime from any working directory.
- B38 [5] Deps are absent from the profile. `oku why <name>` names the
  packages that pull a dep in.
- B39 [5] Two packages depending on different versions of one dep both install
  and both run.
- B40 [5] An unsatisfiable dep version constraint fails, naming the constraint
  and the versions found.
- B41 [5] The first install of a manifest with `run` steps prints them and
  asks for approval. The same manifest hash is never asked twice. A changed
  manifest asks again.
- B42 [5] Non-interactive runs refuse unapproved `run` steps unless `--yes`.
- B43 [5] A step with a non-matching `when` is skipped.
- B44 [5] A failing step aborts the build, reports the step index and its
  output, and leaves store and profile unchanged.

## Sandbox

- B50 [6] On Linux and macOS a `run` step cannot reach the network and cannot
  read the user's home directory.
- B51 [6] A `fetch` step without sha256 fails lint. With one, it may download.
- B52 [6] A `vendor` step's output hash is pinned in the lock. A later
  mismatch fails the build.
- B53 [6] `network = true` on a `run` step is shown in the approval prompt and
  marks the package impure in `oku info`.
- B54 [6] Build env contains only oku's variables, the link environment, step
  `env`, and a PATH of `needs` and `deps`.
- B55 [6] `oku manifest test` builds into a throwaway store and reports
  success or the failing step.

## Projects and activation

- B60 [7] Inside a directory tree with an `oku.toml`, `add`, `remove`, `sync`
  and `list` act on that project. `--global` overrides.
- B61 [7] With `oku hook <shell>` loaded, entering an allowed and synced
  project prepends its profile `bin` to PATH and exports its packages'
  `[env]`. Leaving restores both.
- B62 [7] Entering a project that is not allowed changes nothing and prints a
  hint to run `oku allow`.
- B63 [7] Editing an allowed `oku.toml` revokes the allow until `oku allow`
  runs again.
- B64 [7] Entering an allowed project whose profile is behind its lock changes
  nothing and prints a hint to run `oku sync`.
- B65 [7] The hook performs no network access and runs no manifest code.
- B66 [7] `oku env` prints the exports the hook would apply.
- B67 [7] Project packages shadow global ones on PATH while active.
- B68 [7] The hook exports `[env]` of global packages in every shell.

## Apps, fonts, services

- B70 [8] A package `app` appears in the OS launcher for the current user.
  `remove` and `rollback` take it away again.
- B71 [8] A package `font` is usable by applications for the current user.
  `remove` and `rollback` take it away again.
- B72 [8] `extract` unpacks dmg, pkg, msi, deb, rpm and AppImage without
  executing anything inside them.
- B73 [8] A package with `service = true` in `oku.toml` is running after
  `sync` and after the next login. Without it, the service is installed and
  stopped.
- B74 [8] `oku service start|stop|restart|status|logs` behave the same on all
  three OSes.
- B75 [8] System scope needs `--system`. oku names what it will write and
  prompts before elevating. Without the flag oku never elevates.
- B76 [8] Rolling back to a generation restores which services are enabled.

## Windows

- B80 [9] Profile `bin` entries are shims that exec the store binary with
  arguments, stdio and exit code passed through.
- B81 [9] A binary with DLL deps in other store paths starts from any working
  directory.
- B82 [9] `oku hook pwsh` gives B61 to B68 in PowerShell.

## Cache and signing

- B85 [10] With a cache configured and its key trusted, a package present in
  the cache is substituted and no build step runs.
- B86 [10] A cache entry with a missing, invalid or untrusted signature is
  ignored and the package builds locally.
- B87 [10] `oku cache push` uploads the closure of the named packages, signed.
  Impure packages are refused.
- B88 [10] A non-relocatable entry built under a different store root is never
  substituted.
- B89 [10] A manifest with `signing_key` has its artifacts verified against
  it. The key is pinned in the lock, and a changed key stops `sync` and
  `update` until approved.

## Tooling

- B90 [11] `oku shell <ref>...` opens a shell with those packages on PATH and
  leaves `oku.toml`, the lock and every profile unchanged.
- B91 [11] `oku doctor` reports store root, sandbox availability, hook status,
  PATH order problems and broken profile links.
- B92 [11] `oku self update` replaces the oku binary after verifying its
  signature.
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
- B101 [9] On Windows the oku binary is gone once the command has exited.
