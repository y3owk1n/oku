# oku

A cross-platform package manager with no central registry. Like nix in what it
guarantees, unlike nix in what it asks you to learn.

Repo: https://github.com/y3owk1n/oku. Language: Go, CLI built on cobra.

## Vision

The install instructions for any software, on any OS, are one line that the
author controls and the user can trust: `oku add github:you/tool`.

The manifest lives with the code and versions with the code. The user's
machine is described by a file they own, and that file plus its lock rebuilds
the machine anywhere. Nothing sits between the two. The developer's repo is
the source of truth for how to install. The user's lock is the source of truth
for what was installed. oku turns the first into the second, deterministically.

From nix oku takes the immutable store, the lock that pins everything, and
rollback. It leaves behind the language, the central package set, and the
requirement to take over the system.

Walkthroughs for both sides are in `prd/journeys.md`.

## The problem

A developer who wants users to install their tool must submit and maintain it
in homebrew, apt, aur, nixpkgs, scoop, winget and more. A user who wants the
same tools on every machine must learn each of those. oku removes both costs.

## Promises

- **Publish once**: a developer commits one TOML manifest next to their code,
  or publishes nothing at all when their releases follow common naming
  (inferred manifest). Every oku user on every OS can install it.
- **User owns the list**: packages come from refs the user chooses: a repo, a
  URL, a local file, a collection they or anyone curates. oku ships no
  registry and no default sources.
- **Same input, same machine**: `oku.toml` plus `oku.lock` on a new machine
  yields the same versions, the same bytes for artifacts, and the same build
  inputs for source builds. One command bootstraps from a remote list.
- **One set of build primitives**: fixed TOML step types that behave the same
  on every OS, with deps, link environment, vendoring and sandboxing handled
  by oku rather than by each manifest.
- **No root by default**: a private store and profiles in the user's home.
  Elevation only for outputs the user explicitly puts in system scope.

## What a package can deliver

Binaries, libraries and headers, man pages, shell completions, share data, GUI
apps, fonts, services, and environment variables.

## Platforms

Linux (glibc and musl), macOS, Windows. amd64 and arm64. The manifest schema
is open to other GOOS and GOARCH values.

## Boundaries

These are permanent edges of the product, not deferrals.

- oku manages packages, not the operating system. Users, dotfiles, kernel
  modules, drivers and OS settings belong to other tools.
- oku never drives apt, brew, winget or any native package manager. Doing so
  would break "same input, same machine".
- oku unpacks installers (msi, pkg, dmg, deb, rpm), it never executes them.
  Nothing a package ships runs outside the store at install time.

## Known hard parts

- **Source-build bootstrap**: compilers and C libraries must exist as
  manifests before `deps` can point at them. Prebuilt relocatable toolchains
  (go, rust, zig, node, `zig cc` as a C compiler) cover much of it. A good
  community collection decides whether source builds feel good. Artifact-only
  packages are unaffected.
- **Inference is a heuristic**: it handles common asset naming and fails on
  odd ones. The fallback is a short hand-written manifest.
- **macOS GUI apps**: quarantine, notarization and self-updating bundles fight
  an immutable store. Expect the most platform-specific care here.
- **Windows sandboxing**: no unprivileged mechanism exists. Builds get the
  scrubbed environment only, and `oku doctor` says so.

## Build order

Everything below is in scope. The order is dependency order, not priority.
Steps 1 to 8 and step 10 are built. `docs/` describes what works today.

1. Core: local ref, artifact, store, global profile. `add`, `remove`, `list`.
2. Refs, `oku.toml`, `oku.lock`, `sync`, list `include` and `when`, bootstrap
   from a remote list.
3. Version discovery, `update`, generations, `rollback`, `gc`.
4. Inferred manifests, sources, `search`, `manifest init|lint|bump`.
5. Build executor: steps, `needs`, dep closure, link environment, approvals.
6. Sandboxed builds, `vendor` steps, `manifest test`.
7. Projects: `hook`, `env`, `allow`, `deny`, package `[env]`.
8. Apps, fonts, services, system scope.
9. Windows parity: shims, DLL search path, pwsh hook, `.msi` unpacking.
10. Build cache and signing.
11. `oku shell`, `doctor`, `self update`, install script.
