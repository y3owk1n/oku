# Publish a manifest

You end up with an `oku.pkg.toml` in your tool's repo, so anyone can run
`oku add github:you/tool` and get your tool on every platform you release for.

This guide uses `sharkdp/fd` as the tool. Put your own repo wherever it says
`sharkdp/fd`. A [manifest](../how-oku-works.md#manifest) is one TOML file that
describes one package. Every key is in the
[manifest reference](../reference/manifest.md).

## Check whether oku already installs your tool

oku can install a GitHub repo with no manifest. It reads the newest release,
picks the asset for your machine by its name, and finds the program inside.
Try it on your repo first:

```
$ oku add github:sharkdp/fd
reading github:sharkdp/fd
looking up the versions of sharkdp/fd
fd 10.5.0: unpacking fd-v10.5.0-aarch64-apple-darwin.tar.gz
github:sharkdp/fd has no manifest, so oku inferred one from its newest release, --verbose prints it
added fd 10.5.0
```

When this works, your users need nothing more. A manifest of your own still
helps when you want any of these:

- a `description` that `oku search` can match
- shell completions, which an inferred manifest never has
- a package name that differs from the repo name
- a checksum on every download
- a build from source for a platform you ship no binary for

When it fails, the error names the asset oku chose and the other assets that
fit. Your manifest fixes that for every user.

Remove the test install before you go on:

```sh
oku remove fd
```

## Start from the inferred manifest

`oku manifest init` writes the manifest that `oku add` inferred as a file you
can edit:

```
$ oku manifest init --from sharkdp/fd
downloading https://github.com/sharkdp/fd/releases/download/v10.5.0/fd-v10.5.0-x86_64-unknown-linux-gnu.tar.gz
downloading https://github.com/sharkdp/fd/releases/download/v10.5.0/fd-v10.5.0-x86_64-pc-windows-gnu.zip
wrote oku.pkg.toml
```

It opens one asset per archive format to see the layout, and writes an artifact
for every platform the release has an asset for. The start of the file:

```toml
[package]
name = "fd"
homepage = "https://github.com/sharkdp/fd"

[version]
from = "github-releases"
repo = "sharkdp/fd"
strip_prefix = "v"

[[artifact]]
match = { os = "linux", arch = "amd64", libc = "glibc" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-x86_64-unknown-linux-gnu.tar.gz"
strip = 1
bin = ["fd"]
man = ["fd.1"]
```

`[version] from = "github-releases"` makes oku read your releases, so a new
release needs no change to the manifest. `{{tag}}` becomes the release's tag,
such as `v10.5.0`.

Run `init` in a directory of your repo. `--from` also takes a ref such as
`codeberg:owner/repo` or `gitlab:group/project`.

## Lint it

`oku manifest lint` checks `oku.pkg.toml` against the whole schema:

```
$ oku manifest lint
oku.pkg.toml: warning: package.description is empty, and `oku search` matches it
oku.pkg.toml: ok
```

Add a description to `[package]`:

```toml
[package]
name = "fd"
description = "A simple, fast and user-friendly alternative to find"
homepage = "https://github.com/sharkdp/fd"
license = "MIT OR Apache-2.0"
```

Run it again:

```
$ oku manifest lint
oku.pkg.toml: ok
```

A warning does not fail the run. An error, such as a misspelt key, prints the
line and makes `lint` exit with status 1. `oku add` ignores keys it does not
know, so only `lint` catches a typo.

## Test it

`oku manifest test` installs the manifest into a throwaway store:

```
$ oku manifest test
looking up the versions of sharkdp/fd
fd 10.5.0: downloading https://github.com/sharkdp/fd/releases/download/v10.5.0/fd-v10.5.0-aarch64-apple-darwin.tar.gz
fd 10.5.0: unpacking fd-v10.5.0-aarch64-apple-darwin.tar.gz
fd 10.5.0 works on darwin-arm64 (artifact)
  bin/fd
  share/man/man1/fd.1
```

The last lines are the files a user gets in their profile. Your own store,
`oku.toml` and `oku.lock` stay as they were.

`test` checks only the platform you run it on. Run it on each OS you release
for, or in CI. `--keep` keeps the throwaway store and prints its path, so you
can run the program or look at the unpacked files.

## Commit it

```sh
git add oku.pkg.toml
git commit -m "Add an oku manifest"
git push
```

From now on `oku add github:you/tool` reads your manifest instead of inferring
one. A repo that holds manifests for several packages names them `<name>.toml`
or `packages/<name>.toml`, and users add `github:you/repo#<name>`.

## Write the artifacts by hand

The inferred manifest lists every platform and installs the program and its man
page. fd also ships completions, in an `autocomplete/` directory of each
archive. Here is the manifest trimmed to the platforms oku runs on, with
completions added:

```toml
[package]
name = "fd"
description = "A simple, fast and user-friendly alternative to find"
homepage = "https://github.com/sharkdp/fd"
license = "MIT OR Apache-2.0"

[version]
from = "github-releases"
repo = "sharkdp/fd"
strip_prefix = "v"

[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-aarch64-apple-darwin.tar.gz"
strip = 1
bin = ["fd"]
man = ["fd.1"]
completions = "autocomplete/"

[[artifact]]
match = { os = "darwin", arch = "amd64" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-x86_64-apple-darwin.tar.gz"
strip = 1
bin = ["fd"]
man = ["fd.1"]
completions = "autocomplete/"

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-x86_64-unknown-linux-musl.tar.gz"
strip = 1
bin = ["fd"]
man = ["fd.1"]
completions = "autocomplete/"

[[artifact]]
match = { os = "linux", arch = "arm64" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-aarch64-unknown-linux-musl.tar.gz"
strip = 1
bin = ["fd"]
man = ["fd.1"]
completions = "autocomplete/"

[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-x86_64-pc-windows-msvc.zip"
strip = 1
bin = ["fd.exe"]
```

What each part does:

- oku uses the first `[[artifact]]` whose `match` fits the machine. A key left
  out of `match` fits anything, so the musl builds above serve every Linux
  machine, glibc or musl.
- `strip = 1` drops the archive's top directory, `fd-v10.5.0-aarch64-apple-darwin/`.
  Every path after it is relative to what is left.
- `bin` lists the programs. `man` lists man pages, and the file name needs its
  section, `.1`.
- `completions = "autocomplete/"` names a directory. oku links `fd.fish`, `_fd`
  and `fd.bash` from it, named after the first `bin` entry.

Test it again. The completions now appear:

```
$ oku manifest test
looking up the versions of sharkdp/fd
fd 10.5.0: unpacking fd-v10.5.0-aarch64-apple-darwin.tar.gz
fd 10.5.0 works on darwin-arm64 (artifact)
  bin/fd
  share/man/man1/fd.1
  share/completions/bash/fd.bash
  share/completions/fish/fd.fish
  share/completions/zsh/_fd
```

A release that ships a bare binary can have oku generate completions, and a
script can run through an interpreter. See
[Completions](../reference/manifest.md#completions) and
[bin entries](../reference/manifest.md#bin-entries).

## Add checksums

oku checks every download against a checksum. For a manifest that follows
`github-releases`, oku takes the sha256 that GitHub reports for each release
file, so the fd manifest above needs no checksum key, and `lint` does not warn.

When your files are not GitHub release files, publish a checksum. If your
release has a checksum file, name it with `sha256_url`:

```toml
sha256_url = "https://example.com/tool/{{version}}/SHA256SUMS"
```

Otherwise put the digest in the artifact. `oku manifest hash` prints it:

```
$ oku manifest hash https://github.com/sharkdp/fd/releases/download/v10.4.2/fd-v10.4.2-aarch64-apple-darwin.tar.gz
downloading https://github.com/sharkdp/fd/releases/download/v10.4.2/fd-v10.4.2-aarch64-apple-darwin.tar.gz
sha256 = "623dc0afc81b92e4d4606b380d7bc91916ba7b97814263e554d50923a39e480a"
integrity = "sha512-2It9tU6qXb5cIKgLhSj1sp3x9bFdZGafI+OIB5h3PD9/S5gY/8NgypG0KnRMHDUi67QvUfd+rZTVdgoykrAaWQ=="
```

Paste either line into the artifact. `hash` trusts the download it gets, so
compare it with a checksum your project publishes. Without any checksum, users
trust the first download, and `lint` warns.

## Build from source where no artifact fits

A `[build]` table lets oku build your tool on a platform with no artifact. oku
still uses an artifact where one fits. Add this to the fd manifest:

```toml
[build]
needs = ["cargo"]
source = { git = "https://github.com/sharkdp/fd", tag = "{{tag}}" }
when = [{ os = "darwin" }, { os = "linux" }]

[[build.step]]
vendor = "cargo"

[[build.step]]
run = "cargo build --offline --locked --release"
shell = "sh"

[[build.step]]
install = { bin = ["target/release/fd"], man = ["doc/fd.1"] }
```

- `needs` names tools that must be on the user's `PATH`. oku never installs
  them.
- `source` clones the release's tag.
- `when` limits the build to macOS and Linux, where `sh` exists.
- `vendor = "cargo"` downloads the crates with the network on, and oku pins
  their digest. The `run` step has no network, see
  [The build sandbox](../reference/manifest.md#the-build-sandbox).
- `install` copies files from the source into the package.

`oku manifest test` builds from source whenever the build applies to your
machine, even where an artifact fits. It shows the commands first and asks you
to approve them:

```
$ oku manifest test
looking up the versions of sharkdp/fd
fd 10.5.0 builds from source and runs these commands on your machine:

  step 1  (downloads packages, checked against oku.lock)
    vendor cargo
  step 2
    cargo build --offline --locked --release
```

Pass `--yes` to approve without the prompt when you test repeatedly. Every
step type, build deps and vendoring for go, npm and pip are in
[`[build]`](../reference/manifest.md#build).

## Pin a version and bump it

A manifest with `[version] from` follows your releases and needs no change. A
manifest can also fix one version with `value` and a `sha256` for each file:

```toml
[version]
value = "10.4.2"

[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://github.com/sharkdp/fd/releases/download/v{{version}}/fd-v{{version}}-aarch64-apple-darwin.tar.gz"
sha256 = "623dc0afc81b92e4d4606b380d7bc91916ba7b97814263e554d50923a39e480a"
strip = 1
bin = ["fd"]
```

`oku manifest bump` moves it to the newest release and recomputes every
`sha256`:

```
$ oku manifest bump
looking up the versions of sharkdp/fd
fd 10.4.2 -> 10.5.0, 1 checksums updated in oku.pkg.toml
```

It edits the file as text, so comments and layout stay. `--to` picks another
version. On a manifest that uses `from`, `bump` has nothing to do:

```
$ oku manifest bump
oku: oku.pkg.toml discovers its versions from github-releases, so there is nothing to bump
```

## Next steps

- [Manifest reference](../reference/manifest.md): every key, the template
  variables, and how inference picks an asset.
- [Signatures](../reference/manifest.md#signatures): sign your release files so
  users notice a manifest that someone else publishes.
- [Services](../reference/manifest.md#service), [apps and fonts](../reference/manifest.md#apps-and-fonts),
  and [`[env]`](../reference/manifest.md#env) for what else a package can ship.
- [`oku manifest` commands](../reference/commands.md#oku-manifest-init): every
  flag of `init`, `lint`, `test`, `bump` and `hash`.
- [Refs](../reference/refs.md#sources-and-aliases): let users search a repo of many
  manifests.
