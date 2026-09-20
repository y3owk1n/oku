# oku

A cross-platform package manager with no central registry. Like nix in what it
guarantees, unlike nix in what it asks you to learn.

> **Status: early.** oku installs prebuilt packages from local files, URLs,
> GitHub repos and git repos on Linux and macOS, records them in `oku.toml` and
> `oku.lock`, and restores them on another machine with `oku sync`. Building
> from source, version discovery and per-project lists do not work yet.

Start with [Getting started](docs/getting-started.md). All user documentation
is in [`docs/`](docs/README.md).

## The idea

The install instructions for any software, on any OS, are one line that the
author controls and the user can trust.

```
oku add github:you/tool
```

- **Developers publish once.** One TOML manifest next to the code, or nothing
  at all when release assets follow common naming. No submitting to homebrew,
  apt, aur, nixpkgs, scoop and winget.
- **Users own their list.** Packages come from refs you choose: a repo, a URL,
  a local file, a collection anyone curates. oku ships no registry.
- **Same input, same machine.** `oku.toml` plus `oku.lock` rebuilds your setup
  on a new machine with one command, on Linux, macOS and Windows.
- **No DSL.** Manifests are TOML with a fixed set of build primitives.
- **No root, easy exit.** A private store in your home, instant rollback, and
  `oku self uninstall` leaves nothing behind.

## Read the spec

The design for the whole product, including what is not built yet:

| File | What it holds |
|---|---|
| [`prd/product.md`](prd/product.md) | Vision, promises, boundaries, build order |
| [`prd/journeys.md`](prd/journeys.md) | Walkthroughs from the developer and user side |
| [`prd/decisions.md`](prd/decisions.md) | Design decisions and why |
| [`prd/architecture.md`](prd/architecture.md) | Manifest schema, lock format, paths, CLI |
| [`prd/behaviours.md`](prd/behaviours.md) | Every testable promise |
| [`prd/glossary.md`](prd/glossary.md) | Terms |

## License

MIT
