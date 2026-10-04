# oku.lock

`oku.lock` is the [lock](../how-oku-works.md#lock), the record of what oku
resolved for the list beside it. oku writes it, and you commit it. Change it with oku commands
only.

It sits beside its `oku.toml`, in the config directory for the global list and
at the root of a [project](../how-oku-works.md#project).

## Example

```toml
# Written by oku. Commit this file, and change it with oku commands only.

[[include]]
ref = 'github:you/machines#base'
commit = 'dc2478ae14dc9931336430027ff284d4dc8e4d44'
sha256 = 'd60f12df...'

[[package]]
name = 'ripgrep'
ref = 'github:BurntSushi/ripgrep'
commit = '3fce3b5bb0236da2df6d99672afb8a719642eca7'
manifest_sha256 = '808d85a681d6da6d9acb07777d6a55d1a1d9c433a11b84a43c2fe4a9930a8dc9'
version = '14.1.1'
inferred = true
manifest = "[package]\nname = \"ripgrep\"\n..."

[package.platform]
[package.platform.darwin-arm64]
strategy = 'artifact'
url = 'https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-apple-darwin.tar.gz'
sha256 = '24ad76777745fbff131c8fbc466742b011f925bfa4fffa2ded6def23b5b937be'

[package.platform.linux-amd64-glibc]
strategy = 'artifact'
url = '...'
sha256 = '...'
```

## Include entries

One `[[include]]` per included list.

| Key | Meaning |
|---|---|
| `ref` | The ref from `include`. |
| `commit` | The commit the list was read at. Only for forge and `git+` refs. |
| `sha256` | Digest of the list's content. |

A list that is a file on this machine is yours, like `oku.toml`. Its entry holds
the ref and an empty `sha256`, so oku does not pin its content, and you edit it
and run `oku sync`.

## Package entries

One `[[package]]` per package of the list, sorted by name.

| Key | Meaning |
|---|---|
| `name` | The package name. |
| `ref` | The ref from `oku.toml`, without `@version`. |
| `commit` | The commit the manifest was read at. Only for `github:`, `codeberg:`, `gitea:`, `gitlab:` and `git+` refs. |
| `manifest_sha256` | Digest of the manifest file. |
| `version` | The version installed. When each artifact finds its own version, this is the version of the platform entry whose name sorts first, so every machine writes the same value. |
| `signing_key` | The manifest's minisign or cosign key, when it has one. |
| `signer_workflow` | The manifest's Sigstore signer, when it has one. |
| `tag` | The upstream tag of that version, when it differs, such as `v10.2.0`. When each artifact finds its own version, it is the tag of the same platform entry. |
| `tag_commit` | The full commit a moving tag pointed at for that version. |
| `inferred` | `true` for a package whose manifest oku inferred. |
| `manifest` | The full text of the inferred manifest. Other machines install from this text, and only `oku update` infers again. |
| `asset`, `bin` | The `--asset` and `--bin` the package was added with. `oku update` infers the next version with them, unless its entry in `oku.toml` names others. |
| `platform.<name>` | One [platform entry](#platform-entries) per platform that has resolved this package. |
| `dep` | The packages this one depends on, as [dep entries](#dep-entries). |

## Platform entries

A platform name is `os-arch`, plus `-glibc` or `-musl` on Linux:
`darwin-amd64`, `darwin-arm64`, `linux-amd64-glibc`, `linux-amd64-musl`,
`linux-arm64-glibc`, `linux-arm64-musl`, `windows-amd64` and `windows-arm64`.

| Key | Meaning |
|---|---|
| `strategy` | `artifact` for a download, `build` for a build from source. |
| `url` | The artifact's URL, or the source archive of a build. |
| `sha256` | The digest of that file. |
| `verified` | What oku checked that file against when it pinned `sha256`, see [What oku checked](#what-oku-checked). |
| `commands` | `true` when the manifest runs the download to generate its completions. |
| `vendor_sha256` | A build's digest of what its vendor steps downloaded. |
| `impure` | `true` when a `run` step of the build used `network = true`. |
| `version` | The version of this platform, when each artifact of the manifest [finds its own version](manifest.md#a-version-for-each-platform). |
| `tag` | The upstream tag of that version, when it differs, such as `v2.0.0`. `oku sync` downloads from it. |
| `signed_at` | The unix time in the minisign signature of the download, when a `signing_key` checked it. |

oku keeps `vendor_sha256` and `impure` beside the build in the store, so
`oku update` of a build that did not change writes the same lock.

### What oku checked

`verified` names what oku checked the file against when it pinned its
`sha256`. The table lists them strongest first.

| `verified` | oku checked the file against |
|---|---|
| `sigstore` | a Sigstore signature by the manifest's [`signer_workflow`](manifest.md#sigstore-signatures), of the file or of its checksum file, or the file's [SLSA provenance](manifest.md#slsa-provenance) |
| `minisign` | a signature by the manifest's [`signing_key`](manifest.md#signatures) |
| `cosign` | a cosign signature by the manifest's [`signing_key`](manifest.md#cosign-key), of the file or of its checksum file |
| `manifest` | the `sha256` or `integrity` in the manifest |
| `checksum-file` | the file at the manifest's `sha256_url` |
| `published` | the digest that the version source publishes, such as GitHub for a release file or the npm registry for a package |
| `first-use` | nothing. oku [trusted the first download](security.md#trust-on-first-use) |

A Sigstore, a minisign and a cosign signature rank the same. A checksum file and a
published digest rank the same, since both come from the same place as the
file. A build records how it checked its source archive. A
build from git has no `verified`, since its commit pins the source.

`oku sync` keeps the `verified` of a pin it reuses, so it writes the same lock.
A lock written before oku recorded it has none, and `oku update` adds it when
it resolves the package again.

### Pins for other platforms

The version, the manifest and the deps of a package are the same on every
platform, and only the platform entries differ. A machine installs from the
lock without changing it when the lock holds its platform's entry.

[`[lock]`](oku-toml.md#lock) in `oku.toml` names the platforms to pin besides
your own. `oku add`, `oku update` and `oku sync` then write an entry for each,
from any machine.

- oku takes another platform's checksum from the manifest's `sha256`, else its
  `sha256_url`. With neither it downloads the file, hashes it and says so, see
  [trust on first use](security.md#trust-on-first-use). It never unpacks or
  runs a download for another platform.
- A platform that the manifest builds from source gets `strategy = 'build'`,
  the source archive with its `sha256`, and `impure` when a `run` step for
  that platform uses the network. oku builds nothing for it.
- oku pins a package whose `when` leaves out your machine, with its deps,
  for the platforms that `when` matches. Without `[lock]` the first machine
  that `when` matches pins it.

`vendor_sha256` for another platform depends on the vendor step:

| `vendor` | Pinned from another machine |
|---|---|
| `go`, `cargo` | Yes. They download the same files on every platform, so the digest of your build holds for the others. A vendor step with a `when` shares it with the platforms where the same vendor steps run. |
| `npm` | Yes. oku downloads the other platform's packages into a temporary directory, hashes them and keeps nothing. It runs none of their scripts. This needs a build on your machine, and a manifest with no `run` step before the `npm` step. |
| `pip` with `package`, as `pypi:` writes | Yes. Every build asks uv for the wheels of a fixed platform, so oku downloads the other platform's wheels the same way. See [pypi packages](../guides/npm-pypi-go-cargo.md). |
| `pip` with `requirements.txt` | No. pip picks the wheels of the machine it runs on. The digest comes from the first build on that platform, and `oku sync --locked` fails there until a machine of that platform built the package and you committed the lock. |

When a package has nothing for some `[lock]` platforms:

- `oku add` pins it for the others, writes a `when` that leaves those
  platforms out, and says so. On your own machine among them it installs
  nothing. It fails only when the package has nothing for your machine or
  any `[lock]` platform.

  ```
  rectangle has no artifact or build for linux-amd64-glibc, windows-amd64,
  so its entry in ~/.config/oku/oku.toml says when = { os = "darwin" }
  ```

- `oku update` narrows the `when` the same way when a new version drops a
  platform. It never widens one, and tells you when a new version gains a
  platform that `when` leaves out.
- `oku sync` never edits `oku.toml`. It installs the rest, then fails with the
  line to write. For a package from an included list, it names that list.

  ```
  rectangle has no artifact or build for linux-amd64-glibc, so sync did not install it
  change its line in ~/.config/oku/oku.toml to
    rectangle = { ref = "github:you/recipes#rectangle", when = { os = "darwin" } }
  ```

## Dep entries

Deps nest under the package that needs them, as `[[package.dep]]`, and a dep's
own deps nest under it. A dep entry has the same keys as a package entry. Each
package pins its own, so two packages can pin different versions of the same
dep. A [runtime](oku-toml.md#runtimes) is a dep of each package that uses it,
with its constraint copied into that package's manifest.

`oku sync` installs every dep at its pinned version, and `oku update`
resolves them again with their parent.

## What each pin does

| Pinned | Effect |
|---|---|
| `commit` | `oku sync` reads the manifest at that commit, even after the branch moves. |
| `manifest_sha256` | `oku sync` stops when the manifest content changed. |
| `sha256` per platform | A download with other bytes fails. |
| `signing_key` | oku refuses a manifest with another `signing_key`, or none, until you pass `--accept-key`. |
| `signer_workflow` | oku refuses a manifest with another `signer_workflow`, or none, until you pass `--accept-key`. |
| `verified` per platform | `oku update` and `oku add` refuse a new download that oku would check more weakly, until you pass `--accept-weaker-check`. See [Security](security.md#weaker-checks). |
| `vendor_sha256` per platform | A build whose vendor steps download something else fails, and nothing is kept. |
| `signed_at` per platform | `oku update` refuses a newer version whose minisign signature is older than this one and names no version, until you pass `--accept-weaker-check`. See [Security](security.md#signing-keys-of-a-manifest). |
| `tag_commit` | oku refuses to download once upstream moved the tag off that commit. |
| include `commit` and `sha256` | `oku sync` reads the list at that commit and stops when its content changed. |

When a pin no longer holds, `oku sync` stops before it downloads anything and
names the `oku update` that accepts the change:

```
oku: include github:you/machines#base: the included list changed since oku.lock was written
run `oku update` to accept it
```

## When the lock changes

| Command | Change |
|---|---|
| `oku add` | Adds or replaces the package's entry, for your platform and each `[lock]` platform. |
| `oku remove` | Drops the package's entry. |
| `oku sync` | Adds an entry for a package that has none, adds your platform or a `[lock]` platform to an entry that lacks it, and drops a package that left the list. It changes no pin that is there. |
| `oku sync --locked` | Never. It fails when the lock would change, or when it holds a package that left the list, or lacks a `[lock]` platform. |
| `oku update` | Resolves again and rewrites the entries it touched. It clears the platform entries of a package whose manifest changed, because they described the old one. Each machine adds its entry again on its next sync. With no names it also reads includes fresh, and asks before it takes one that changed. |
| `oku rollback` | Replaces the lock with the copy saved in that generation. |
| `oku sync <list-ref>` | Writes a new global lock from the published one, see [Adopt a published list](commands.md#adopt-a-published-list). |

A machine whose platform is missing from the lock adds its entry the first
time it runs `oku sync`, and leaves the other entries alone. Commit the lock
back after that.

## Format rules

- oku sorts packages by name, so the same state always writes the same bytes
  and diffs stay small.
- oku stores a ref to a file inside the lock's directory relative to it, such
  as `./packages/node.toml`, so the lock works in another checkout or home
  directory.
- `sync --locked` ignores line endings, so a lock that git checked out with
  CRLF on Windows passes.
- A lock from an older oku may hold build entries with fewer pins. `oku sync`
  adds what is missing and keeps the pins that are there, and builds nothing
  for that. Only a new build records the vendor digest of your own machine.
- A lock that oku 0.4.0 or older wrote holds the full path of a runtime from
  the machine that wrote it. Run `oku update <name>` for each package that
  uses it.
