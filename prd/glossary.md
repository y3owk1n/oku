# Glossary

- **Manifest**: one TOML file describing one package. Owned by its publisher.
- **Inferred manifest**: a manifest oku derives from a repo's release assets
  when the repo has none.
- **Ref**: a pointer to a manifest or list. Forms: `./rg.toml`,
  `https://host/rg.toml`, `github:owner/repo`, `github:owner/repo#name`,
  `git+https://host/repo#path`, `alias/name`. An `@version` suffix pins the
  version.
- **List ref**: a ref read as a list. It names the file an `include` merges or
  `oku sync <ref>` adopts.
- **Adopt**: set a machine up from a published list by including it and
  starting from the lock beside it.
- **Collection**: a repo or directory holding many manifests as `<name>.toml`,
  at its root or under `packages/`.
- **Source**: a user-defined alias for a collection. oku ships none.
- **Artifact**: a prebuilt download in a manifest, chosen by a selector.
- **Selector**: `{ os, arch, libc }` match table. Omitted keys match anything.
- **Build**: the ordered steps that produce a package from source.
- **Step**: one build primitive: `run`, `install`, `patch`, `fetch`,
  `extract`, `copy`, `vendor`. Any step may carry a `when` selector.
- **Vendor step**: a networked step that downloads language deps (cargo, go,
  npm, pip) and whose output hash is pinned in the lock.
- **Needs**: host tools a build requires. oku checks them, never installs
  them. A toolchain that exists as an oku package belongs in `deps` instead.
- **Deps**: other oku packages, as refs with optional version constraints.
  `build.deps` exist during the build, `runtime.deps` stay referenced after.
- **Closure**: a package plus every store path it transitively depends on.
- **Link environment**: the compiler and linker variables oku sets so a build
  finds its deps.
- **Relocatable**: a package whose output embeds no store path.
- **Impure**: a package with a `network = true` run step.
- **oku.toml**: a declared package list. Global or per-project. May include
  other lists.
- **oku.lock**: resolved state beside an `oku.toml`. Pins manifests, versions,
  checksums, closures, vendor hashes, signing keys and included lists.
- **Store**: immutable realized packages at
  `<root>/store/<name>-<version>-<hash>/`.
- **Store hash**: digest of manifest content, version, platform, strategy and
  dep store hashes. Non-relocatable packages add the store root.
- **Profile**: a directory of links into the store. One global, one per
  allowed project.
- **Generation**: a numbered snapshot of a profile, including exposed apps,
  fonts, enabled services, rendered files and wanted settings.
- **Shim**: the Windows stand-in for a symlink in a profile `bin`. A copy of
  `oku.exe` under the program's name, beside a `<name>.shim` file.
- **Junction**: the Windows directory link that a normal user may create. A
  profile's `current` is one.
- **Activation**: the shell hook applying a project profile's PATH and `[env]`
  while the shell is inside that project.
- **Allow**: the user's recorded trust of one project `oku.toml`, by path and
  content hash.
- **Approval**: the user's recorded trust of one manifest's `run` steps, by
  manifest hash.
- **Cache**: a static host or directory of signed, store-hash-keyed build
  results.
- **Scope**: `user` or `system`. Where apps, fonts and services are exposed.
  `system = true` on a list entry selects system scope.
- **Ledger**: `exposed.toml`, the record of every file oku wrote outside its
  own directories and of every setting it changed.
- **Target**: the path in the home directory that a `[files]` entry writes.
- **Location variable**: `{{home}}`, `{{config}}`, `{{data}}`, `{{appdata}}`
  or `{{localappdata}}`, the start of a target.
- **Variable**: a named string from `[vars]` that `render`, `text` and targets
  expand.
- **Secret**: a value in a sops or an age file. The generation holds the
  encrypted file, and the decrypted bytes exist only in `<data>/oku/secrets/`.
- **Identity**: the private age key that decrypts a secret. oku reads it and
  never writes it.
- **Setting**: one key of a per-user OS mechanism: a macOS preference domain,
  dconf, or the current user's registry.
- **Plan** and **apply**: the two parts of a change. The plan checks and
  changes nothing visible. The apply changes the machine, and reverts on
  failure.
- **Shared root**: `/opt/oku`, the store root that `oku setup --system` creates
  so that every machine has the same store paths.
