# Manifest reference

A [manifest](../how-oku-works.md#manifest) is one TOML file that describes one
package. This page lists every key, what oku does with it, and how oku infers a
manifest for a repo that has none. To write your first one, follow
[Publish a manifest](../guides/publish-a-manifest.md).

Where oku looks for a manifest:

| File | Users add it with |
|---|---|
| `oku.pkg.toml` at the root of a repo | `oku add github:you/tool` |
| `<name>.toml` at the root, else `packages/<name>.toml` | `oku add github:you/repo#<name>` |
| any path in a repo | `oku add github:you/repo#dir/name.toml` |

A repo or directory that holds many manifests is a collection. Users give it a
short name with `oku source add` and search it, see
[Refs](refs.md#sources-and-aliases). `oku search` matches each manifest's `description`.

`oku add` ignores a key it does not know, so an older oku still installs a
manifest written for a newer one. `oku manifest lint` knows the whole schema and
reports an unknown key as an error.

## Example

```toml
[package]
name = "ripgrep"
description = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT"

[version]
from = "github-releases"
repo = "BurntSushi/ripgrep"

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256_url = "https://github.com/BurntSushi/ripgrep/releases/download/{{tag}}/ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz.sha256"
strip = 1
bin = ["rg"]
man = ["doc/rg.1"]
completions = { fish = "complete/rg.fish", zsh = "complete/_rg" }
```

## Tables

| Table | Required | Holds |
|---|---|---|
| [`[package]`](#package) | yes | The name and the facts `oku search` shows. |
| [`[version]`](#version) | yes | One fixed version, or where oku discovers versions. |
| [`[[artifact]]`](#artifact) | no | A prebuilt download for the machines its `match` fits. |
| [`[build]`](#build) | no | How to build the package from source. |
| [`[runtime]`](#runtime) | no | Other packages the installed package needs. |
| [`[env]`](#env) | no | Variables the user's shell exports while the package is installed. |
| [`[[app]]`](#apps-and-fonts) | no | A desktop launcher on Linux and Windows. |
| [`[[service]]`](#service) | no | A long-running program the OS service manager can run. |

A machine gets the package only when an artifact or the build fits it.

## [package]

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | Lowercase letters, digits, `.`, `_` and `-`. Starts with a letter or digit. |
| `description` | no | One line. `oku search` matches it, and `oku manifest lint` warns when it is empty. |
| `homepage` | no | A URL. |
| `license` | no | An SPDX identifier. |
| `signing_key` | no | Your minisign public key. oku then checks every artifact against its signature, see [Signatures](#signatures). |
| `relocatable` | no | `true` when the built files contain no store path. A [build cache](../guides/build-caches.md) then offers the package to machines with any store root. Default `false`. Only matters for `[build]`. |

## [version]

A manifest either fixes one version with `value` or discovers versions with
`from`. Discovery suits a published manifest, because a new release needs no
manifest change.

| Key | Required | Meaning |
|---|---|---|
| `value` | one of `value` and `from` | The one version this manifest installs. |
| `from` | one of `value` and `from` | Where versions come from, see the next table. |
| `repo` | with `from` | What to read. Its form depends on `from`. |
| `strip_prefix` | no | Text cut off the front of a tag to get the version, such as `"v"`. oku ignores a tag without the prefix, except for `v`, see [How oku reads tags](#how-oku-reads-tags). Not for `npm`, `pypi`, `go`, `crates` or `git-branch`. |
| `tag` | no | One tag that upstream moves, such as `"nightly"`. Only with `github-releases`, `gitea-releases` or `gitlab-releases`, and not with `strip_prefix`. See [Follow a moving tag](#follow-a-moving-tag). |
| `branch` | with `git-branch` | The branch to follow, such as `"main"`. See [Follow a branch](#follow-a-branch). |
| `regex` | with `redirect` or `page` | Finds the version, see [Follow a download URL](#follow-a-download-url). Not with `sparkle`. |
| `join` | no | What joins the groups of `regex` into the version, `.` by default. `+` keeps the parts apart for [`{{version_part1}}`](#template-variables) and the rest. With `sparkle` it joins the short version and the build. One of `.`, `+`, `-` and `_`. |

| `from` | `repo` | Reads |
|---|---|---|
| `github-releases` | `owner/repo`, or `host/owner/repo` on a GitHub Enterprise Server | The newest 1000 releases, page by page. Skips drafts and prereleases. |
| `gitea-releases` | `host/owner/repo`, such as `codeberg.org/owner/repo` | The same on a Gitea or Forgejo server. |
| `gitlab-releases` | `group/project`, or `host/group/project` on a GitLab server of your own | The newest 1000 releases. Skips a release dated in the future, which GitLab calls upcoming. |
| `git-tags` | a git URL | Every tag, with `git ls-remote`. Needs `git` on `PATH`. |
| `git-branch` | a git URL | The newest commit of `branch`. Needs `git` on `PATH`. |
| `npm` | a package name, such as `@scope/name` | Every version in `registry.npmjs.org`. Skips a prerelease, which has a `-` in its version. |
| `pypi` | a package name, such as `black` | Every version in the Python Package Index. |
| `go` | a module path, such as `golang.org/x/tools/gopls` | The tagged versions of the module from the Go module proxy. A module with no tags has one version, the pseudo-version of its newest commit. |
| `crates` | a crate name, such as `ripgrep` | Every version of the crate on crates.io. |
| `redirect` | an http(s) URL | One version, from the URL it redirects to. |
| `page` | an http(s) URL | One version, from the text at the URL. |
| `sparkle` | the http(s) URL of a Sparkle feed | One version, the newest in the feed, see [Follow a Sparkle feed](#follow-a-sparkle-feed). |

```toml
[version]
from = "github-releases"
repo = "sharkdp/fd"
strip_prefix = "v"
```

`oku add` installs the newest version, and `oku add <ref>@1.2.0` installs that
one. The user's [lock](../how-oku-works.md#lock) records the version and its
tag, and `oku sync` installs the locked version without asking upstream again.
Version pins and ranges are in [Refs](refs.md#pin-a-version).

### How oku reads tags

- After `strip_prefix`, a tag that does not start with a digit is ignored. That
  drops tags such as `nightly`.
- A `v` is optional either way, because many repos switched once between tags
  such as `1.2.0` and `v1.2.0`. With `strip_prefix = "v"` a tag `1.2.0` counts
  too, and without a prefix `v1.2.0` counts. When both tags of a version exist,
  oku downloads from the one in the form `strip_prefix` names.
- `npm`, `pypi`, `go` and `crates` list versions, not tags, so `{{tag}}` equals
  `{{version}}` there.

### How oku picks the newest version

- The newest version is the highest by its dot-separated numbers, so `1.10.0` is
  newer than `1.9.0`.
- A version with a `-` suffix, such as `2.0.0-rc1`, is older than `2.0.0`. A
  number in the suffix counts as a number, so `7.1.2-31` is newer than
  `7.1.2-9`, and `rc10` is newer than `rc9`.
- A tag list has no prerelease flag, so oku reads the version. A version with
  the word `rc`, `alpha`, `beta`, `pre`, `preview`, `dev` or `snapshot` in it,
  such as `1.27rc1` or `2.0.0-rc1`, is a prerelease. Other letters behind a
  number make a newer version, so `1.1.1w` is newer than `1.1.1`.
- With `pypi`, a yanked version and a PEP 440 prerelease are never the newest.
  With `crates`, a yanked version and one with a `-` are never the newest. With
  `go`, a version with a `-` is never the newest.
- A prerelease is never the newest, and `oku add <ref>@1.27rc1` still installs
  it.

### Follow a moving tag

Some projects publish every nightly build under one tag and replace its files
each time. Name that tag, and oku follows it:

```toml
[version]
from = "github-releases"
repo = "neovim/neovim"
tag = "nightly"

[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://github.com/neovim/neovim/releases/download/{{tag}}/nvim-macos-arm64.tar.gz"
strip = 1
bin = ["bin/nvim"]
```

- oku reads that one release and accepts it when it is a prerelease. A draft
  fails.
- Each build is its own version, `<date>-<commit>`, such as
  `2026.09.20-a73243f`. Both parts come from the commit the tag points at, its
  day in UTC and the start of its hash, so one commit always has one version.
  `{{tag}}` stays `nightly`.
- `oku update` moves the package when the tag points at another commit. Every
  build has its own store path, so `oku rollback` returns to the earlier build
  without a download.
- Without `sha256` and `sha256_url`, oku checks the download against the sha256
  that the GitHub API reports for that file. `url` must be the file's GitHub
  download URL for that, and `oku manifest lint` warns when it is not. Gitea,
  Forgejo and GitLab report no sha256 for a file, so with `gitea-releases` or
  `gitlab-releases` the user trusts the first download, and `lint` warns about
  that too.
- A `[build]` whose `source` clones `{{tag}}` fails when the clone is not at the
  commit of the version.

Upstream deletes the old build when it moves the tag. `oku sync` on a machine
that lacks the locked build can download it only until then. Afterwards it
stops, and installs no newer build under the locked version:

```
oku: nvim: upstream moved the tag nightly to 2026.09.21-0c1f2aa since oku.lock was written, and the locked build 2026.09.20-a73243f is gone
run `oku update nvim` to take the new build
```

`oku add <ref>@2026.09.20-a73243f` works only while upstream is at that build.
A build cache does not keep old builds, because it holds no plain downloads.

### Follow a branch

A manifest can build the newest commit of a branch, to run a program before its
next release:

```toml
[version]
from = "git-branch"
repo = "https://github.com/someone/tool"
branch = "main"

[build]
needs = ["git", "make"]
source = { git = "https://github.com/someone/tool", tag = "{{tag}}" }
```

- The version is `<date>-<commit>`, such as `2026.09.20-a73243f`, the day and
  the first seven characters of the newest commit. `{{tag}}` is the branch.
- `oku.lock` records the commit. `oku sync` fetches that commit, so every
  machine builds the same source after the branch has newer commits. The host
  must serve a commit by its id, which GitHub, GitLab and Gitea do.
- `oku update <name>` takes the newest commit. Every push is a new version with
  its own store path, so `oku rollback` returns to the earlier build.
- A branch has no releases, so the manifest needs a `[build]`. An `[[artifact]]`
  whose `url` has no `{{version}}` would install one download under every
  version.

### Follow a download URL

Many apps publish one URL that always leads to their newest download, such as
`https://discord.com/api/download?platform=osx`. That URL redirects to a file
whose path holds the version:

```toml
[version]
from = "redirect"
repo = "https://discord.com/api/download?platform=osx"
regex = '/osx/([0-9.]+)/'

[[artifact]]
match = { os = "darwin" }
url = "https://stable.dl2.discordapp.net/apps/osx/{{version}}/Discord.dmg"
app = ["Discord.app"]
```

`page` reads the text at the URL instead, such as an update feed or a download
page:

```toml
[version]
from = "page"
repo = "https://updates.discord.com/distributions/app/manifests/latest?channel=stable&platform=osx&arch=x64"
regex = '"host_version":\[(\d+),(\d+),(\d+)\]'
```

- `redirect` follows up to 10 redirects of `repo` and stops at the first URL
  that `regex` matches, so oku downloads nothing from it. A hop without the
  version, such as `.../installers/latest`, is fine. `page` applies `regex`
  to the first 8 MiB of text at `repo`.
- The first match counts. Its groups joined with `.` are the version, so the
  `page` example reads `[0,0,413]` as `0.0.413`. `join = "+"` joins them with
  `+` instead, such as `1.2.3+45`, and `{{version_part1}}` and
  `{{version_part2}}` give each in the URL. oku leaves out a group that
  matched nothing. `regex` needs at least one group, in
  [Go syntax](https://pkg.go.dev/regexp/syntax).
- A version starts with a digit, and holds only letters, digits, `.`, `_`,
  `+` and `-`. `oku add` and `oku update` fail when `regex` matches nothing or
  the groups do not make such a version, and change nothing.
- Upstream shows only its newest version, so `oku add <ref>@1.2.0` works only
  while upstream is at `1.2.0`. `oku sync` installs the locked version and asks
  `repo` nothing, so it works while upstream still serves that version's
  download. Use `{{version}}` in `url`, not `repo` itself, or every version
  would get the same download.
- oku has no checksum for the download, so the user trusts the first download
  of each version, and `oku manifest lint` warns.
- `strip_prefix`, `tag` and `branch` do not apply.

### Follow a Sparkle feed

Many macOS apps announce updates in a Sparkle feed, an XML file that lists
releases. `sparkle` reads one:

```toml
[version]
from = "sparkle"
repo = "https://www.iina.io/appcast.xml"

[[artifact]]
match = { os = "darwin" }
url = "https://dl.iina.io/IINA.v{{version}}.dmg"
app = ["IINA.app"]
```

- The version is the `sparkle:shortVersionString` of the newest item, whether
  an element or an attribute of the enclosure. Like Sparkle, oku ranks the
  items by their build, `sparkle:version`, and by the short version where an
  item names no build. The order of the items does not matter.
- oku reads a short version such as `1.165.1 (87405)` up to its space.
- With `join = "+"` the version is the short version and the item's
  `sparkle:version`, the build, such as `1.165.1+87405`. `{{version_part1}}`
  and `{{version_part2}}` give each in the URL.
- oku skips an item on a `sparkle:channel` such as `beta`. It reads `stable`
  and `release` as no channel, because some feeds put every release on one.
  It also skips an item whose `sparkle:os` names another system, as a feed
  shared with WinSparkle on Windows has. Sparkle is for macOS, so give other platforms
  their own [`version` table](#a-version-for-each-platform).
- As with `page`, the feed names only the newest version, oku has no
  checksum for the download, and `strip_prefix`, `tag` and `regex` do not
  apply.

### A version for each platform

Some vendors keep each platform at its own version, or publish each platform
in its own place. Discord's macOS download is `0.0.413` while its Linux one is
`1.0.159`. Give each artifact a `version` table instead of `[version]`, with
the same `from`, `repo`, `regex` and `strip_prefix`:

```toml
[package]
name = "discord"

[[artifact]]
match = { os = "darwin" }
version = { from = "redirect", repo = "https://discord.com/api/download?platform=osx", regex = '/osx/([0-9.]+)/' }
url = "https://stable.dl2.discordapp.net/apps/osx/{{version}}/Discord.dmg"
app = ["Discord.app"]

[[artifact]]
match = { os = "linux", arch = "amd64" }
version = { from = "redirect", repo = "https://discord.com/api/download?platform=linux&format=tar.gz", regex = '/linux/([0-9.]+)/' }
url = "https://stable.dl2.discordapp.net/apps/linux/{{version}}/discord-{{version}}.tar.gz"
strip = 1
bin = ["discord"]
```

A macOS app may announce its updates in a Sparkle feed while its Linux build
comes from GitHub releases:

```toml
[[artifact]]
match = { os = "darwin" }
version = { from = "sparkle", repo = "https://example.com/appcast.xml" }
url = "https://example.com/Tool-{{version}}.dmg"
app = ["Tool.app"]

[[artifact]]
match = { os = "linux", arch = "amd64" }
version = { from = "github-releases", repo = "owner/tool", strip_prefix = "v" }
url = "https://github.com/owner/tool/releases/download/{{tag}}/tool-linux-amd64.tar.gz"
bin = ["tool"]
```

- `from` is `github-releases`, `gitea-releases`, `gitlab-releases`,
  `git-tags`, `redirect`, `page` or `sparkle`. A moving `tag`, a `branch` and
  the registries need `[version]`. Every artifact then needs a `version`, and
  the manifest cannot have `[version]` or `[build]`.
- `{{version}}` and `{{tag}}` in an artifact are its own version and tag. oku
  checks a file of a GitHub release against the digest GitHub reports, as in
  [Checksums](#checksums).
  `oku add` and `oku update` find the version of this machine and of each
  platform of [`[lock] platforms`](oku-toml.md#lock), and `oku.lock` keeps each
  one in its [platform entry](lock.md#platform-entries). oku asks upstream
  once for artifacts that share a `version` table.
- `oku update` moves only the platforms whose version changed. `oku sync`
  installs the version locked for this machine and asks upstream nothing.
- `oku list` and `oku outdated` show the version of this machine.
- You cannot pick a version for the package, so `oku add <ref>@1.2.0` and a
  `version` in `oku.toml` fail. `oku manifest bump` refuses the manifest.

## [[artifact]]

One table per prebuilt download. oku uses the first one whose `match` fits the
machine, so put specific entries before general ones.

| Key | Required | Meaning |
|---|---|---|
| `match` | no | `{ os, arch, libc }`, see [Match and when](#match-and-when). A missing key matches anything, and a missing `match` matches every machine. |
| `url` | yes | Where the download is. `https://`, `http://` or `file://`. Expands [template variables](#template-variables). |
| `sha256` | no | The download's digest, 64 lowercase hex characters. Not with `sha256_url`. |
| `sha256_url` | no | The URL of a checksum file, see [Checksums](#checksums). Not with `sha256`. |
| `version` | no | Where this artifact's own version comes from, see [A version for each platform](#a-version-for-each-platform). |
| `integrity` | no | A sha512 digest the way npm publishes it, `sha512-` and the digest in base64. |
| `strip` | no | How many leading path components to drop when unpacking. Default 0. |
| `bin` | see below | Executables inside the download. An entry may be a table, see [bin entries](#bin-entries). |
| `man` | see below | Man pages, see [Man pages](#man-pages). |
| `completions` | see below | Shell completions, see [Completions](#completions). |
| `lib`, `include`, `share` | see below | Files for the package's `lib`, `include` and `share`, see [Prebuilt libraries](#prebuilt-libraries). |
| `app` | see below | macOS app bundles, such as `["Foo.app"]`, see [Apps and fonts](#apps-and-fonts). |
| `font` | see below | Font files, see [Apps and fonts](#apps-and-fonts). |
| `data` | see below | `true` for a package that only holds files, see [Data packages](#data-packages). |

Each artifact needs at least one of `bin`, `lib`, `include`, `share`, `man`,
`completions`, `app` and `font`, or `data = true`.

oku keeps the whole unpacked download, so a program that needs files next to it
keeps working. The output keys only choose what gets linked into the user's
[profile](../how-oku-works.md#profile).

### Paths inside the download

Paths in `bin`, `man`, `completions` and the other output keys are relative to
the unpacked download after `strip`. A path that does not exist, is not a
regular file, or points outside the package fails the install. Two outputs with
the same file name fail too.

To find the paths inside a download, run `oku manifest test --keep` and look in
the `pkg/` directory of the store path it prints.

### Downloads oku can unpack

oku recognises a download by its content, not by its file name.

| Format | Notes |
|---|---|
| tar, tar.gz, tar.bz2, tar.xz, tar.zst | An xz file may use a BCJ filter for x86, ARM, ARM-Thumb, PowerPC, IA-64 or SPARC, or the Delta filter. ARM64 and RISC-V BCJ are not read. |
| zip | |
| 7z | An archive made on Windows has no unix file modes. Its programs still run, because oku marks every `bin` as executable. |
| `.deb` | oku unpacks only the data archive. Its files are at `usr/bin/...`. |
| `.rpm` | oku unpacks only the file payload. Its files are at `usr/bin/...`. |
| `.dmg` | macOS only. oku mounts the image read-only, copies it, and unmounts it. An image that holds a `.pkg` and no app at its top only carries the package, so oku expands each package into a folder of its name. Its files are then at `<package>.pkg/<component>.pkg/Payload/...`. |
| `.pkg` | macOS only. Its files are at `<component>.pkg/Payload/...`. |
| `.msi` | Windows only. oku runs `msiexec /a`, the administrative install. It copies the files out and skips the install sequence, so it writes no registry entries, services or shortcuts. Its files are at paths such as `Program Files/<product>/...`. |
| anything else | The executable itself. This covers a plain binary and an AppImage. |

- A file from a tar, zip, 7z or rpm archive keeps the time the archive gives
  it. A release tarball relies on that. Its `aclocal.m4` and `configure` are
  newer than their inputs, so `make` does not try to run autotools.
- `strip` applies to tar, zip, 7z, deb and rpm. oku copies a `.dmg`, a `.pkg`
  and an `.msi` whole.
- In a `.deb` or an `.rpm` a symlink to an absolute path, such as
  `usr/bin/fdfind -> /usr/bin/fd`, names a file of the package and becomes a
  relative link.
- From a `.dmg` oku leaves out the hidden Finder files and any link that points
  out of the image, such as the shortcut to `/Applications`.
- oku unpacks installers and never runs them. It never executes a `.deb`'s
  maintainer scripts, an `.rpm`'s scriptlets, a `.pkg`'s install scripts or an
  `.msi`'s install sequence. A package that depends on its post-install script
  does not work from oku. An `.msi` can define actions for the
  administrative install itself, and `msiexec /a` runs those. Few packages
  define any.
- oku refuses an archive entry that is absolute or contains `..`, and a symlink
  whose target is absolute or resolves outside the package.

A download that is the executable itself needs exactly one `bin` and nothing
else, and oku installs the file under that name. That one `bin` may be a
[table with `run`](#run-a-program-through-an-interpreter). The file then keeps
the name it has in the URL, without `.gz`, `.xz`, `.bz2` or `.zst`, and `run` or
`args` name it as `{{pkg}}/<name>`. This example starts a program with a
variable set:

```toml
[[artifact]]
match = { os = "linux", arch = "arm64" }
url = "https://github.com/artempyanykh/marksman/releases/download/{{tag}}/marksman-linux-arm64"
bin = [{ name = "marksman", run = "/usr/bin/env", args = ["DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1", "{{pkg}}/marksman-linux-arm64"] }]
```

### Checksums

oku checks a download against the first of these that exists:

1. `sha256` in the artifact.
2. The file at `sha256_url`.
3. The sha256 that GitHub reports for the file, when the manifest, or the
   artifact's own `version`, follows `github-releases`. GitHub has one for
   most files uploaded since mid 2025, so such a manifest needs no `sha256`
   for them, and `oku manifest lint` does not warn.
4. The digest that the user's `oku.lock` pinned earlier.

oku also checks `integrity` when the artifact has one, or when the npm registry
publishes one for the download. The `npm` source reads a sha512 for each
version's download, and oku checks it when the artifact's `url` is that
download and the manifest has no checksum of its own.

With none of them oku trusts the first download and pins its digest, see
[Security](security.md). Publish `sha256`, `sha256_url` or `integrity`.
`oku manifest hash <url>` prints the `sha256` and `integrity` lines for a
download.

A `sha256_url` file may hold:

- a single digest
- `digest  filename` lines, as `sha256sum` writes them. oku picks the line that
  names the download's file.
- JSON, either an object that maps file names to digests or an array of objects
  with `name` and `sha256` fields. A name may hold a path, and oku matches its
  base name against the download's file name.

```json
{"tool-1.2.3-linux-amd64.tar.gz": "9f86d0..."}
```

```json
[{"name": "dist/tool-1.2.3-linux-amd64.tar.gz", "sha256": "9f86d0..."}]
```

### Signatures

A checksum in the manifest protects the download, but not the manifest. A
signing key lets users notice when someone else publishes under your name:

```toml
[package]
name = "foo"
signing_key = "RWTr8koGkq7wBGTVdGedU8b7CkkiIu+LBp6uQLJ7ltH/7iGXMKNF1b27"
```

Create the key and sign each release file with
[minisign](https://jedisct1.github.io/minisign/):

```sh
minisign -G                           # once, writes minisign.pub and the secret key
minisign -S -m foo-1.2.0-linux.tar.gz # writes foo-1.2.0-linux.tar.gz.minisig
```

- Upload each `.minisig` beside its file. oku downloads the artifact's URL with
  `.minisig` appended and refuses the artifact when the signature is missing or
  not from `signing_key`.
- oku accepts the current and the legacy (`-l`) kind of signature.
- With a signing key, an artifact without `sha256` is no longer trust on first
  use, and `oku manifest lint` does not warn about it.
- The user's `oku.lock` pins the key at the first install. After that, oku
  refuses a manifest with another key, or with none, until the user passes
  `--accept-key`. Tell your users before you change the key.
- `signing_key` covers artifacts. It does not sign the sources of a `[build]`.

### Prebuilt libraries

A build that [depends](#build-dependencies) on a package looks for headers in
its `include`, for libraries in its `lib` and for pkg-config files in
`lib/pkgconfig` and `share/pkgconfig`. A prebuilt download fills them with
`lib`, `include` and `share`:

```toml
[[artifact]]
match = { os = "darwin", arch = "arm64" }
url = "https://example.com/libwebp-{{version}}-mac-arm64.tar.gz"
strip = 1
bin = ["bin/cwebp"]
include = ["include/webp"]
lib = ["lib/libwebp.a", "lib/libsharpyuv.a"]
```

An entry is a file or a directory in the download, and it keeps its last name.
`include/webp` becomes the package's `include/webp`, so
`#include <webp/decode.h>` works.

A static library works as it is. A shared library from a download keeps the
install name its builder gave it. On macOS that name is a path that does not
exist on your machine, so a program linked against the library does not start.
Build such a library from source with `[build]`.

### Data packages

`data = true` makes a package of a repo that ships no program, such as agent
skills, templates or a colour scheme. It puts nothing on `PATH` and exposes
nothing. A list reaches its files with `{{pkg.<name>}}` in `[files]`, see
[Dotfiles](../guides/dotfiles.md).

```toml
[package]
name = "my-skills"

[version]
value = "2026.09.18"

[[artifact]]
url = "https://github.com/someone/skills/archive/032be146865d973682535de75f2287da438550bf.tar.gz"
sha256 = "1d9c0f75def9a97cedd8cfff5eadc60475c03913a33ed9880bf8987999b3761f"
strip = 1
data = true
```

The package is in the store, the lock pins it, and `oku rollback` brings back
the files of the version before. `data = true` together with another output is
an error, so a manifest that forgot its `bin` still fails.

## bin entries

A `bin` entry is a path, or a table. A list may mix them.

```toml
bin = ["bin/ffmpeg", { name = "ffprobe", path = "bin/ffprobe" }]
```

| Entry | Linked as |
|---|---|
| `"dir/tool"` | `bin/tool`, the file's own name |
| `{ name, path }` | `bin/<name>`, the file at `path` |
| `{ name, run, args }` | `bin/<name>`, a program that oku writes |

A table's `name` uses the characters of a package name. `path` and `run` do not
go together, and `path` takes no `args`.

### Run a program through an interpreter

Some packages ship a script and no executable, such as a Node or Python tool or
a `.jar`. A table with `run` makes oku write the program. It runs `run` with
`args` in front of the user's own arguments.

```toml
[runtime]
deps = ["./node.toml"]

[[artifact]]
url = "https://registry.npmjs.org/@actions/languageserver/-/languageserver-{{version}}.tgz"
sha256 = "d152725064c64f862da5158cd630d4c67973edfb58bdabaa44054ffef03b9d03"
strip = 1
bin = [{ name = "gh-actions-language-server", run = "{{dep.node.prefix}}/bin/node", args = ["{{pkg}}/bin/actions-languageserver"] }]
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | The program's name in the user's profile. It need not match any file in the download. |
| `run` | yes | The program to run, as an absolute path. On Windows, a path with no extension names the `.exe` beside it when that exists, so `{{dep.node.prefix}}/bin/node` works there too. |
| `args` | no | Arguments that go before the user's. |

`run` and `args` expand the [template variables](#template-variables) of an
artifact, `{{pkg}}`, `{{prefix}}` and `{{dep.<name>.prefix}}`. They must not
hold a line break.

- The interpreter is a [runtime dep](../how-oku-works.md#runtime-dep), so the
  user does not need it on `PATH`, and it does not appear there either. The
  package stays a plain download. oku runs no build and asks for no approval.
- The program puts the `bin` of each runtime dep first on its own `PATH`.
  prettierd, for example, starts its daemon as `node`, and so gets the same node
  that runs prettierd. A [build dep](../how-oku-works.md#build-dep), such as the
  cargo, go or uv that built the package, is never on that `PATH`.
- On macOS and Linux the program is a shell script that ends in `exec`. On
  Windows it is a [shim](../how-oku-works.md#shim) that holds the arguments, so
  `run` names an `.exe` there. Use one `[[artifact]]` per OS when the paths
  differ.

### Expose a file under another name

A table with `path` links the file at `path` under `name`. `path` is relative to
the unpacked download for an artifact, and to the source directory in an
`install` step.

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | The program's name in the user's profile. |
| `path` | yes | The file inside the package. |

## Man pages and completions

### Man pages

`man` lists man pages. The file name needs a section, such as `rg.1` or
`rg.1.gz`. An entry may be a [pattern](#patterns-in-font-and-man). A build that
installs its own files, such as `make install`, may put man pages under
`{{prefix}}/share/man` or `{{prefix}}/man`. Both reach the profile's
`share/man`.

### Completions

`completions` takes one of three forms.

A table maps a shell to a file in the package:

```toml
completions = { fish = "complete/rg.fish", zsh = "complete/_rg", bash = "complete/rg.bash" }
```

A string names a directory that holds the conventional file of each shell,
named after the program in the first `bin` entry: `<name>.fish`, `_<name>` and
`<name>.bash`. oku links the ones the directory has, and fails when it has none:

```toml
completions = "complete/"
```

A table with `generate` runs the program to print them, for a release that
ships a bare binary. `{{shell}}` expands to `fish`, `zsh` and `bash` in turn,
and each run's stdout becomes the conventional file of that shell:

```toml
[[artifact]]
bin = ["atuin"]
completions = { generate = "atuin gen-completions --shell {{shell}}" }
```

- The files take their name from the first `bin` entry, or from `name` when that
  is another program, as in
  `{ generate = "just --completions {{shell}}", name = "just" }`.
- oku runs the command after it unpacks the download and before it links the
  package into the profile. The working directory is the unpacked package, the
  package's own `bin` is first on `PATH`, and the command runs in the
  [build sandbox](#the-build-sandbox).
- A non-zero exit or an empty stdout fails the install, and the error holds the
  command and its stderr.
- `generate` executes the download, so oku asks for the same approval as for a
  build, see [Approval](#approval). `oku.lock` records `commands = true` for the
  platform. A changed command is a changed manifest, so `oku sync` stops until
  `oku update` takes it.
- `generate` takes no shell paths beside it, and `oku manifest lint` rejects the
  two together. `{{shell}}` is its only template variable.

### Where installed files are linked

| Key | Linked at |
|---|---|
| `bin = ["dir/tool"]` | `bin/tool` |
| `bin = [{ name = "probe", path = "dir/ffprobe" }]` | `bin/probe` |
| `man = ["doc/tool.1"]` | `share/man/man1/tool.1` |
| `completions = { fish = "c/tool.fish" }` | `share/completions/fish/tool.fish` |
| `completions = "c/"` | `share/completions/fish/tool.fish`, `share/completions/zsh/_tool` and `share/completions/bash/tool.bash`, the ones that exist |
| `completions = { generate = "tool completions {{shell}}" }` | The same three files, from the command's output |

## Apps and fonts

A package can ship a desktop app and fonts. oku copies them to where the OS
looks for them, and removes them when the user removes the package.

```toml
[[artifact]]
match = { os = "darwin" }
url = "https://github.com/rxhanson/Rectangle/releases/download/{{tag}}/Rectangle{{version}}.dmg"
app = ["Rectangle.app"]
```

```toml
[[artifact]]
url = "https://github.com/JetBrains/JetBrainsMono/releases/download/{{tag}}/JetBrainsMono-{{version}}.zip"
font = ["fonts/ttf/*.ttf"]
```

| Key | macOS | Linux | Windows |
|---|---|---|---|
| `app = ["Foo.app"]` in an artifact | Copies the bundle to `~/Applications/Foo.app`. | Not used. | Not used. |
| `[[app]]` at the top level | Not used. | A desktop entry at `<data home>/applications/oku-<name>.desktop`. | A Start Menu shortcut, `oku-<name>.lnk`. |
| `font = [...]` in an artifact | Copies them to `~/Library/Fonts/`. | Copies them to `<data home>/fonts/oku/`. | Copies them to the user's font folder and names them in the registry. |

`<data home>` is `$XDG_DATA_HOME`, or `~/.local/share`. The Windows paths are in
[Windows](../guides/windows.md#place-apps-and-fonts).

On Linux and Windows an app is a program plus a launcher. Ship the program with
`bin` and describe the launcher in `[[app]]`:

```toml
[[app]]
name = "Foo"
exec = "bin/foo"
icon = "share/icons/foo.png"
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | The launcher's name. |
| `exec` | yes | A path inside the installed package, normally `bin/<program>`. |
| `icon` | no | A path inside the installed package. Windows ignores it, because a shortcut shows the icon of its program. |

- oku copies apps and fonts, because Finder, Spotlight and font services do not
  treat a symlink as installed. A macOS bundle keeps its code signature.
- oku refuses to overwrite a file that it did not place, and the install fails
  with a message that names the file.
- Apps and fonts come from the user's global list only. A package in a
  [project](../guides/projects.md) installs its programs, and oku says that its
  apps and fonts were skipped.
- A `[build]` installs them with `install = { app = [...], font = [...] }`.

### Patterns in font and man

A `font` or `man` entry may be a pattern, relative to the unpacked package:

```toml
font = ["fonts/ttf/*.ttf", "**/*.otf"]
man = ["doc/*.1"]
```

`*` and `?` match within one path segment, and `**` matches any number of
segments. oku resolves a pattern once, when it installs the package. A pattern
that matches no file is an error that names the pattern. An entry without `*` or
`?` is a plain path. The `install` step of a `[build]` takes the same patterns.

## [env]

Variables the package needs in the user's shell. The
[shell hook](commands.md#oku-hook) exports them while the package is installed,
in every shell for a globally installed package and inside the project for a
project package.

```toml
[env]
JAVA_HOME = "{{pkg}}/lib/jvm"
```

Values expand `{{version}}`, `{{tag}}`, `{{pkg}}` and `{{prefix}}`, see
[Template variables](#template-variables).

A package may not set a variable that changes how other programs load or run.
oku rejects `PATH`, `HOME`, `SHELL`, `USER`, `IFS`, `ENV`, `BASH_ENV`, `PS1`,
`PROMPT_COMMAND`, and any name that starts with `LD_`, `DYLD_` or `OKU_`. When
two packages set the same variable, the one whose name sorts last wins.

## [runtime]

| Key | Meaning |
|---|---|
| `deps` | Other packages installed with this one, for prebuilt artifacts and builds alike. Same form as [`build.deps`](#build-dependencies). |

```toml
[runtime]
deps = ["./node.toml"]
```

oku does not link runtime deps into the user's profile, so their programs are
not on the user's `PATH`. A program that oku writes with [`run`](#run-a-program-through-an-interpreter)
gets each runtime dep's `bin` first on its own `PATH`.

## [build]

`[build]` says how to produce the package from source. oku builds when no
`[[artifact]]` fits the machine, or when the user passes `--from-source`. A
manifest may have both, and oku then uses an artifact when one fits.

```toml
[build]
needs = ["cc", "make"]
source = { git = "https://github.com/Old-Man-Programmer/tree", tag = "{{tag}}" }

[[build.step]]
run = "make -j{{jobs}}"
shell = "sh"

[[build.step]]
install = { bin = ["tree"], man = ["doc/tree.1"] }
```

| Key | Meaning |
|---|---|
| `needs` | Tools that must be on the user's `PATH`, such as `cc` or `cargo`. oku checks them before any step runs and never installs them. See [The build environment](#the-build-environment). |
| `source` | Where the code comes from, see the next table. Without `source` the build starts in an empty directory. |
| `deps` | Other oku packages the build uses, see [Build dependencies](#build-dependencies). |
| `when` | The platforms the build works on, see [Match and when](#match-and-when). oku does not build on any other platform. A missing `when` builds everywhere. |
| `step` | The `[[build.step]]` tables, run in order, see [Build steps](#build-steps). |

| `source` | Does |
|---|---|
| `{ git, tag }` | Clones that tag at depth 1. Needs `git`. |
| `{ url, sha256, strip }` | Downloads and unpacks an archive, checked against `sha256`. |
| `{ url, sha256_url, strip }` | The same, checked against a checksum file that upstream publishes. |
| `{ url, strip }` | With `github-releases`, oku checks a file of the repo's release against the sha256 GitHub reports for it. With `crates`, it checks the `.crate` file against the sha256 crates.io publishes. Otherwise it trusts the first download and pins its sha256 in `oku.lock`, and `oku manifest lint` warns. |

A platform that has neither an artifact nor a build gets no package, see
[New machine](../guides/new-machine.md).

### Follow upstream with a source archive

A source archive with no fixed `sha256` can follow upstream. With a
`[version] from` and `{{version}}` or `{{tag}}` in the `url`, `oku update`
builds the new release and pins the digest of its archive:

```toml
[version]
from = "github-releases"
repo = "tukaani-project/xz"
strip_prefix = "v"

[build]
needs = ["cc", "make"]
source = { url = "https://github.com/tukaani-project/xz/releases/download/{{tag}}/xz-{{version}}.tar.gz", strip = 1 }
```

A later download of the same version that has another digest fails, until
`oku update <name>` accepts it.

### Build steps

Steps run in order, in the source directory. Each `[[build.step]]` sets exactly
one of these keys:

| Key | Does |
|---|---|
| `run = "..."` | Runs a command string in a shell. |
| `install = { ... }` | Copies files from the source directory into the package, see [install](#install). |
| `copy = { from, to }` | Copies one file. `from` is relative to the source directory and `to` to the package. |
| `fetch = { url, sha256, to }` | Downloads a file to `to` in the source directory, see [fetch](#fetch). |
| `extract = { file, to, strip }` | Unpacks an archive that is in the source directory into `to`. Both are relative to the source directory. |
| `patch = { file, strip }` | Applies a unified diff to the source, see [patch](#patch). |
| `vendor = "go"` | Downloads the language's packages with the network on, see [Vendoring](#vendoring). |

Any step may also set:

| Key | Meaning |
|---|---|
| `when` | The step only runs on a matching machine, see [Match and when](#match-and-when). |
| `shell` | For `run`, `sh`, `bash`, `pwsh` or `cmd`. Default `sh`, except on Windows, which has no default. On Windows without PowerShell 7, `pwsh` runs Windows PowerShell 5.1. `oku manifest lint` requires `shell` on every `run` step that can reach Windows, which is every step whose `when` does not name another `os`. |
| `env` | Extra variables for `run`, as a table. Values expand template variables. |
| `network` | `true` gives a `run` step the network, see [The build sandbox](#the-build-sandbox). |

### run

`sh` and `bash` run with `-e`, so the step fails at the first failing command. A
`run` step may write into `{{prefix}}` itself, so a project with a working
`make install` needs no `install` step. A build that leaves `{{prefix}}` empty
fails.

### install

`install` takes `bin`, `lib`, `include`, `man`, `share`, `completions`, `app` and
`font`, with the paths relative to the source directory.

- `bin` files become executable. A `bin` entry may be a
  [table](#bin-entries) too, and there `{{pkg}}` equals `{{prefix}}`.
- `man` and `completions` go where an artifact's would, and `completions` takes
  the same [three forms](#completions). With `generate`, the command runs in the
  source directory after the files are copied, with the package's `bin` first on
  `PATH`, and the step appears in the approval prompt.
- `font` and `man` take [patterns](#patterns-in-font-and-man).

### fetch

A `fetch` step needs `sha256`, or `sha256_url` in its place for a checksum file
that upstream publishes, such as the `.sha256` file beside the download. oku
reads that file on each build, and `oku.lock` pins no digest for a fetched file.

### patch

```toml
[[build.step]]
fetch = { url = "https://example.com/fix-build.patch", sha256 = "...", to = "fix.patch" }

[[build.step]]
patch = { file = "fix.patch" }
```

`file` is a path in the source directory, so a `fetch` step or the source itself
provides it. `strip` removes that many leading directories from the file names
in the diff, as `-p` does for the `patch` program. It defaults to 0:

| The diff comes from | `strip` |
|---|---|
| `git diff` or `git format-patch` | 0. oku leaves out the `a/` and `b/` prefixes of a diff with a `diff --git` line. |
| `diff -ur old new` | 1, which removes `old/` and `new/`. |
| a diff with `a/` and `b/` prefixes but no `diff --git` line | 1 |

- A patch can change, create, delete and rename files, and change a file's mode.
- oku applies it in Go and never calls the `patch` program, so the step works
  the same on Windows.
- Every hunk has to fit exactly. oku does not apply a hunk at an offset or with
  fuzz, and a hunk that does not fit fails the build and names the file.
- Line endings count. A patch with Windows line endings fits a source that has
  them too, and not one with unix line endings.

### Build dependencies

A dep is another oku package, given as a [ref](refs.md). oku installs
`build.deps` before the build and makes them visible to it. It installs
`runtime.deps` with the package.

```toml
[build]
deps = [
  "github:someone/recipes#zlib",
  { ref = "github:someone/recipes#openssl", version = ">=3, <4" },
]

[runtime]
deps = ["github:someone/recipes#ca-certificates"]
```

A dep is a ref string, or a table with `ref`, an optional `version` and an
optional `when`. `when` takes the form of a package's
[`when` in oku.toml](oku-toml.md#when), and limits the dep to matching
platforms. The same dep may appear twice with a version for each platform:

```toml
[runtime]
deps = [
  { ref = "./node.toml", version = "^22", when = { os = "linux" } },
  { ref = "./node.toml", version = "^20", when = [{ os = "darwin" }, { os = "windows" }] },
]
```

- Each entry is installed on the machines its `when` matches, and pinned in
  `oku.lock` for the `[lock]` platforms it matches. A platform that no entry
  matches gets no such dep.
- Two entries of one dep must not both match a platform.
- A `[runtimes]` entry in oku.toml takes no `when`.

A `version`:

- A bare version such as `22` picks that version, or the newest 22.x when no
  release is exactly 22.
- A range is one or more comma-separated parts, and all must hold. A part is
  `>=`, `>`, `<=`, `<`, `=`, `^` or `~` followed by a version. `^1.4` allows
  versions below 2 and `~1.4` those below 1.5.
- oku picks the newest version that satisfies it. When none does, the error
  names the range and the versions it found.

A relative file ref starts at the directory of the manifest that names it. In a
manifest from a repo, it names a file of the same repo, read at the same commit.
In a manifest from a URL, it names the URL beside it. A manifest from a repo or
a URL cannot depend on an absolute path. A dependency cycle is an error.

- oku does not link deps into the user's profile. Each package gets its own deps,
  and two packages may use different versions of the same one. `oku why <name>`
  shows what uses a dep.
- A package's store path depends on the deps it was built against, so a new dep
  version leads to a new build instead of changing an installed package.
- `{{dep.<name>.prefix}}` expands to a dep's directory, by its package name.

For every build dep, a `run` step gets these variables, so compilers, linkers,
pkg-config and cmake find it with no flags in your manifest:

| Variable | Holds, for each dep |
|---|---|
| `PATH` | `<dep>/bin`, ahead of the `needs` tools |
| `CPATH` | `<dep>/include` |
| `LIBRARY_PATH` | `<dep>/lib` |
| `LD_RUN_PATH` | `<dep>/lib`. The GNU linker records it, so the result finds the dep's shared libraries at runtime. |
| `PKG_CONFIG_PATH` | `<dep>/lib/pkgconfig` and `<dep>/share/pkgconfig` |
| `CMAKE_PREFIX_PATH` | `<dep>` |

Each platform needs something extra:

- macOS ships zlib, bzip2, expat, libxml2, sqlite3, libcurl and ncurses with
  headers and with no pkg-config file, so a dep whose own file says
  `Requires: zlib` would not resolve. On macOS oku adds a pkg-config file for
  each of them, last on `PKG_CONFIG_PATH`, with the version it reads from the
  SDK on the machine. A dep with the same name wins over it.
- On macOS the linker has no `LD_RUN_PATH`. A shared library must record its own
  absolute install name, which cmake and autotools do when they are given
  `{{prefix}}`. With a bare compiler call it is
  `cc -dynamiclib -install_name {{prefix}}/lib/libfoo.dylib ...`.
- The GNU linker ignores `LD_RUN_PATH` once the link passes its own `-rpath`,
  and cmake and libtool both do. Such a build must name the dep's `lib` itself,
  for example with `-DCMAKE_INSTALL_RPATH={{dep.foo.prefix}}/lib` for cmake, or
  `LDFLAGS="-Wl,-rpath,{{dep.foo.prefix}}/lib"` for a libtool configure.

A build dep is only there for the build. A program that loads a build dep's
shared library at runtime needs that dep in `runtime.deps` too. Otherwise
`oku gc` removes the library and the program stops working, and on another
machine it never works. After a build, oku reads each Mach-O or ELF file in
`bin` and `lib` of the result and warns once per store package when a file loads
a dep that `runtime.deps` does not name:

```
app: bin/app loads greet, which is not in runtime.deps, so it breaks after `oku gc` or on another machine
```

The install still succeeds. The check reads the linked paths from the files
themselves, so it needs no `otool` or `ldd`. On macOS it counts every library a
file loads, including weak (`-weak-l`), re-exported, lazy and upward links, since
a weak link that `oku gc` removes changes what the program does. It runs for builds only, never for
a download, and not on Windows.

### Vendoring

`run` steps have no network, so `go build` or `cargo build` cannot download
packages. A [vendor step](../how-oku-works.md#vendor-step) does that download
for them. It runs the language's own tool with the network on, and oku hashes
everything it downloaded.

```toml
[build]
needs = ["go", "git"]
source = { git = "https://github.com/rakyll/hey", tag = "{{tag}}" }

[[build.step]]
vendor = "go"

[[build.step]]
run = "go build -mod=vendor -o hey ."
shell = "sh"
env = { GOTOOLCHAIN = "local", GOFLAGS = "-buildvcs=false" }

[[build.step]]
install = { bin = ["hey"] }
```

| `vendor` | Runs | Fills | Then build with |
|---|---|---|---|
| `"go"` | `go mod vendor` | `vendor/` | `go build -mod=vendor` |
| `"cargo"` | `cargo vendor --locked vendor`, and points `.cargo/config.toml` at it | `vendor/` | `cargo build --offline --locked` |
| `"npm"` | `npm ci --ignore-scripts` | `node_modules/` | the project as it is |
| `"pip"` | `pip download -r requirements.txt -d vendor/pip` | `vendor/pip/` | `pip install --no-index --find-links vendor/pip` |

- The tool must be in `needs`, or come from a dep.
- `cargo` and `npm` need the project's lockfile, and `pip` needs
  `requirements.txt`. `pip` uses `pip3` when there is no `pip`.
- The user's `oku.lock` pins one digest for what the vendor steps downloaded.
  `oku sync` runs them again and fails if the digest differs, so a locked build
  cannot get different packages. `oku update` accepts the new digest. What `pip`
  and `npm` download depends on the platform, and the lock keeps one digest per
  platform.
- A vendor step runs a program, so it appears in the approval prompt. It does
  not make the package impure, because oku checks its output.

A vendor step with `package` installs one package from its registry instead of
what a lockfile lists:

| Key | Meaning |
|---|---|
| `package` | With `vendor = "npm"` an npm package name, with `"pip"` a Python package name, with `"go"` a Go package path, with `"cargo"` the crate that the source is. |
| `scripts` | With `vendor = "npm"` and `package` only. The packages whose install scripts run, see below. |

#### npm with package

An npm step with `package` needs no source and no lockfile. It installs that
package from the registry with its dependencies, at the manifest's version, into
`{{prefix}}/lib/node_modules`:

```toml
[build]
deps = ["./node.toml"]

[[build.step]]
vendor = "npm"
package = "typescript"

[[build.step]]
install = { bin = [{ name = "tsc", run = "{{dep.node.prefix}}/bin/node", args = ["{{prefix}}/lib/node_modules/typescript/bin/tsc"] }] }
```

oku runs `npm install --ignore-scripts --omit=dev --before=<time>`, where the
time is when the registry says that version was published. npm then picks each
dependency as it was at that time, so a later install gets the same packages,
and the digest in `oku.lock` still fits. `oku add npm:<name>` writes this build
for a package that lists dependencies, see
[Registry packages](../guides/npm-pypi-go-cargo.md).

No install script runs, unless `scripts` names the package. Some packages have a
dependency whose install script downloads or builds a native binary, and
without it the program fails when run:

```toml
[[build.step]]
vendor = "npm"
package = "opencode-ai"
scripts = ["opencode-darwin-arm64"]
```

- oku hashes the install, then runs `npm rebuild <names>` in the same sandbox
  with the network on, which runs the install scripts of the named packages
  alone.
- A name must be the package itself or one of its dependencies as the registry
  lists them, and the build fails on any other.
- The approval prompt names the packages whose scripts run.
- The digest in `oku.lock` covers the install and not what a script downloads,
  so oku marks the build impure, as it does for a `run` step with
  `network = true`, and never pushes it to a build cache.

#### pip with package

A pip step with `package` works the same for a Python package. It needs uv from
a dep and `python3` on the build's `PATH`, from a dep or the system:

```toml
[version]
from = "pypi"
repo = "black"

[runtime]
deps = ["./python.toml"]

[build]
deps = ["./python.toml", "github:astral-sh/uv"]

[[build.step]]
vendor = "pip"
package = "black"
```

- oku runs `uv pip install --target {{prefix}}/lib/python --exclude-newer <time>`,
  where the time is one second after the version's last file was uploaded, so uv
  picks each dependency as it was then.
- oku hashes `{{prefix}}/lib/python` without the programs that uv writes,
  because those name the python by its path on this machine.
- Then it writes `{{prefix}}/bin/<name>` for each console script of the package,
  and copies the package's other programs there. Those are the programs oku
  links when the build has no `install` step.
- `oku add pypi:<name>` writes this build.

#### cargo with package

A cargo step with `package` names the crate that the source is. It vendors what
the crate's `Cargo.lock` pins, and once oku has hashed that, it runs
`cargo install --path . --locked --offline --no-track --root {{prefix}}`. On
Windows the step runs through PowerShell. `oku add cargo:<name>` writes this
build.

#### go with package

A go step with `package` builds that Go package:

```toml
[version]
from = "go"
repo = "golang.org/x/tools/gopls"

[build]
needs = ["go"]

[[build.step]]
vendor = "go"
package = "golang.org/x/tools/gopls"
```

- The go command downloads the module that `version.repo` names, and every
  module it needs, into a module cache in the source directory, and checks each
  against the checksum database.
- oku hashes `modcache/cache/download` without the checksum database's own
  files, which change as it grows, so the digest is the same on every platform.
- Then `go install` builds the package from that cache alone, with cgo off, into
  `{{prefix}}/bin`. On Windows the step runs through PowerShell.
- `oku add go:<path>` writes this build.

### When a build fails

oku reports the step number, its kind, and the last 40 lines of its output,
deletes the half-built package, and leaves the user's profile as it was.
`oku add -v` shows the output while the build runs.

### Approval

Before the first build of a manifest that runs commands, oku shows the user
those commands and asks. That covers `run` steps, `vendor` steps, an `install`
step that generates completions, and an artifact's `completions.generate`.
`--yes` approves without asking. See [Security](security.md).

## [[service]]

A long-running program that the OS service manager can run, see
[Services](../guides/services.md). A package may have several.

```toml
[[service]]
name = "postgres"
command = "bin/postgres"
args = ["-D", "{{prefix}}/share/postgres/data"]
env = { PGPORT = "5432" }
restart = "on-failure"
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | What the user types in `oku service start <name>`. Same characters as a package name. Two installed packages cannot ship a service of the same name. |
| `command` | yes | A path inside the installed package, normally `bin/<program>`. |
| `args` | no | Arguments. They expand `{{prefix}}`, `{{version}}`, `{{home}}`, `{{config}}` and `{{data}}`, so a service can name its config file, as in `["--config", "{{config}}/tool/rc"]`. |
| `env` | no | Variables for the service. Values expand the same variables as `args`. |
| `restart` | no | `never`, `on-failure` or `always`. Default `never`. |
| `when` | no | Limits the service to matching machines, see [Match and when](#match-and-when). oku installs no service on a machine that `when` leaves out. |

A program that runs from inside an app bundle on macOS and from `bin` on Linux
gets one service for each:

```toml
[[service]]
name = "tool"
command = "Tool.app/Contents/MacOS/tool"
when = { os = "darwin" }

[[service]]
name = "tool"
command = "bin/tool"
when = { os = "linux" }
```

- The program must stay in the foreground and must not fork into the
  background. The service manager starts it, watches it, and restarts it
  according to `restart`.
- What it prints goes to a log file on macOS and to the user journal on Linux.
- A service manager starts a program with a bare `PATH`. On macOS and Linux a
  service of the user gets the `bin` of the global profile in front of it, so a
  program such as a hotkey daemon can call other installed programs by name. A
  `PATH` in `env` replaces that. A service in
  [system scope](../how-oku-works.md#system-scope) keeps the bare `PATH`.
- Installing a package never starts its service. The user turns it on with
  `service = true` in their list.

## Match and when

`match` on an artifact and `when` on a build, a build step or a service pick
machines with the same keys:

| Key | Values |
|---|---|
| `os` | `linux`, `darwin`, `windows` |
| `arch` | `amd64` or `arm64`, Go's names |
| `libc` | `glibc` or `musl`. Linux only. oku reports `musl` when `/lib/ld-musl-*.so.1` exists. |

A missing key matches anything. `match` is one table. `when` is one table or an
array of tables, and then matches where any table matches:

```toml
when = [{ os = "darwin" }, { os = "linux" }]
```

An empty array matches no platform and is an error. oku runs on these
platforms, named as `oku.lock` names them: `darwin-amd64`, `darwin-arm64`,
`linux-amd64-glibc`, `linux-amd64-musl`, `linux-arm64-glibc`,
`linux-arm64-musl`, `windows-amd64` and `windows-arm64`.

## Template variables

`{{name}}` in a value expands to the variable's value. An unknown variable is an
error, and `oku manifest lint` reports it.

| Variable | Value |
|---|---|
| `{{version}}` | The version being installed, such as `10.2.0`. |
| `{{tag}}` | The upstream tag of that version, such as `v10.2.0`. With a fixed version it equals `{{version}}`. |
| `{{version_major}}`, `{{version_minor}}`, `{{version_patch}}` | The numbers of the version, before any `+`. `1` of `1.2.3+45`. A URL that needs a number the version lacks fails, and the error names it. |
| `{{version_nodots}}`, `{{version_underscores}}`, `{{version_dashes}}` | The version without its dots, or with `_` or `-` for them. `123` of `1.2.3`. |
| `{{version_part1}}`, `{{version_part2}}`, ... | The pieces of the version between `+`, such as `45` for part 2 of `1.2.3+45`. See `join` in [[version]](#version). |
| `{{os}}`, `{{arch}}`, `{{libc}}` | The machine, with the values of [`match`](#match-and-when). |
| `{{pkg}}` | The directory that holds the package's files. For an artifact that is the unpacked download, `{{prefix}}/pkg`. For a build it equals `{{prefix}}`. |
| `{{prefix}}` | The package's directory in the store. In a build, pass it to `make install PREFIX={{prefix}}` or `./configure --prefix={{prefix}}`. |
| `{{src}}` | The build's source directory. |
| `{{jobs}}` | The number of CPUs. |
| `{{dep.<name>.prefix}}` | The store directory of a dep, by its package name. A dep's programs are in its `bin`. |
| `{{shell}}` | `fish`, `zsh` or `bash`, in `completions.generate` only. |
| `{{home}}`, `{{config}}`, `{{data}}` | The user's home, config and data directories, in a service only. |

The variables derived from the version work wherever `{{version}}` does.

Where each one works:

| Value | Variables |
|---|---|
| artifact `url`, `sha256_url` | `version`, `tag`, `os`, `arch`, `libc` |
| `bin` table `run`, `args` | the artifact variables, `pkg`, `prefix`, `dep.<name>.prefix` |
| `completions.generate` | `shell` |
| `[env]` values | `version`, `tag`, `pkg`, `prefix` |
| `build.source` `tag`, `url` | the artifact variables, `prefix`, `src`, `jobs` |
| `build.source` `sha256_url` | the artifact variables |
| `run`, step `env` values, `fetch.url` | the artifact variables, `prefix`, `src`, `jobs`, `dep.<name>.prefix` |
| service `args`, `env` values | `prefix`, `version`, `home`, `config`, `data` |

Release URLs often hold the tag in one place and the version in another:

```toml
url = "https://github.com/sharkdp/fd/releases/download/{{tag}}/fd-{{tag}}-aarch64-apple-darwin.tar.gz"
```

## Inferred manifests

An [inferred manifest](../how-oku-works.md#inferred-manifest) is one that oku
writes itself. `oku add github:owner/repo` on a repo with no `oku.pkg.toml`
writes a manifest from the repo's newest release and installs from it.
`--verbose` prints it. To get it as a file you can edit and commit:

```
$ oku manifest init --from owner/repo
wrote oku.pkg.toml
```

| Ref | Inferred `version.from` |
|---|---|
| `github:` | `github-releases` |
| `codeberg:`, `gitea:` | `gitea-releases` |
| `gitlab:` | `gitlab-releases`. Its assets are the links of the release, not the source archives GitLab adds. |
| `npm:`, `pypi:`, `go:`, `cargo:` | `npm`, `pypi`, `go`, `crates`, see [Registry packages](#registry-packages) |
| `cask:`, `scoop:` | the recipe's own rule, see [Recipes of other package managers](#recipes-of-other-package-managers) |
| `aqua:` | `github-releases` of the repo, see [Recipes of other package managers](#recipes-of-other-package-managers) |
| `winget:` | `github-releases` for a download from GitHub releases, else a fixed `value` |
| a URL of a download | a fixed `value`, see [A URL of the download](#a-url-of-the-download) |

With `@version` oku reads that version's release. It tries the tag `version`,
then `v<version>`. With any other tag prefix it reads the newest release.

The lock pins an inferred manifest like any other. `oku.lock` stores its full text,
so `oku sync` on another machine installs from the same text and does not infer
again. `oku update` infers again.

### How inference picks an asset

- It matches asset names to platforms by the words in them, listed in the table
  below. On macOS an asset for your arch comes before a universal one. `gnu`
  names a libc on Linux only, so `x86_64-pc-windows-gnu` is a Windows asset.
- On Linux it writes the glibc build first and the musl build second with no
  `libc` in its `match`, so glibc machines with no build of their own use the
  musl build.
- It takes tar archives in any compression oku knows, zip and 7z archives,
  single binaries, compressed or not, and the installers oku
  [unpacks](#downloads-oku-can-unpack): `.deb`, `.rpm`, `.msi`, `.dmg`, `.pkg`
  and AppImage. It skips editor extensions (`.vsix`).
- An installer's format names its OS, so `Tool1.2.dmg` is a macOS asset with no
  OS word, and `tool-aarch64.AppImage` is a Linux one. One that names no arch
  fits amd64 and arm64 of that OS.
- oku can open a `.dmg` or `.pkg` on macOS and an `.msi` on Windows only, so run
  inference for those on that OS. oku cannot unpack a Windows `-setup.exe`
  without running it, so it takes none.

With several candidates for one platform, it prefers, in order:

1. A command line build over a desktop app, which is an asset with `desktop`,
   `app`, `gui`, `installer`, `setup` or `.app.` in its name.
2. A tar archive over a zip, both over a single binary, that over an installer,
   and a `.dmg` over a `.pkg`.
3. The smaller asset, when the host reports sizes.
4. The shortest name.

When other assets fit your machine as well, in any format, a comment in the
manifest lists them.

| For | Words it looks for in an asset name |
|---|---|
| `linux` | `linux` |
| `darwin` | `darwin`, `macos`, `macosx`, `apple`, `osx`, `mac` |
| `windows` | `windows`, `win64`, `win` |
| `amd64` | `x86_64`, `x86-64`, `amd64`, `x64` |
| `arm64` | `aarch64`, `arm64` |
| `386` | `i386`, `i686`, `386` |
| `arm` | `armv7`, `armv7l`, `armhf`, `arm` |
| `riscv64` | `riscv64` |
| any arch, on `darwin` | `universal`, `universal2`, `all` |
| `glibc` | `gnu`, `glibc` |
| `musl` | `musl` |

### How inference reads an asset

- The version starts at the first digit of the tag, and everything before it
  becomes `strip_prefix`. `v1.2.0` gives `"v"` and `jq-1.8.1` gives `"jq-"`.
- It downloads assets and looks inside. It opens one asset per archive ending,
  so a Windows zip gets its own layout and the tar archives share one.
- `oku add` opens assets for your machine and for the `[lock]` platforms only.
  Another platform gets an artifact when its asset has the ending of one oku
  opened anyway. Otherwise oku leaves it out, as it leaves out a platform whose
  asset it cannot read. `oku manifest init` opens one for every platform,
  because a published manifest serves them all.
- The program is the executable named after the repo, else the only
  executable. In an archive with no executable files, which is what a zip made
  on Windows is, it is the file named after the repo.
- An executable next to the program is a program too when its name starts
  with the program's name and a `-`, such as `age-keygen` next to `age`. In a
  Windows zip an `.exe` of such a name counts.
- With `--bin`, the programs are the files it names instead.
- A single top-level directory becomes `strip = 1`.
- Files ending in `.1` become `man`. With more than 8 of them only the
  program's own page is kept.
- An asset that holds a macOS app bundle, `Name.app/Contents/...`, gives
  `app = ["Name.app"]` and treats nothing inside the bundle as a program. A
  program beside the bundle still becomes `bin`.

### How inference finds a checksum

- It uses `<asset>.sha256` as `sha256_url` when that exists, else a release
  file with `checksum` or `sha256sum` in its name that is not a signature.
- When a release has one such file for each OS or platform, such as
  `tool-mac-checksums.txt` or `tool-linux-arm64-checksums.txt`, oku takes the
  one that names the asset's OS and arch, then one that names its OS or arch
  alone, then a generic file such as `checksums.txt` or `SHA256SUMS`. It never
  takes a file that names another OS or arch.
- When GitHub reports a digest for the asset, oku reads the checksum file and
  skips it if it states another digest. zellij's files do that, because they
  hash the program inside the archive. oku then checks the download against
  GitHub's digest.
- With no checksum file and no digest, oku trusts the package on first use, see
  [Security](security.md).

### A release for some platforms

A release for one OS, such as a macOS app shipped as a `.dmg`, cannot pin the
other platforms in `[lock]`. `oku add` pins that OS alone and writes
`when = { os = "darwin" }` on the package's entry in `oku.toml`, see
[New machine](../guides/new-machine.md). When the release has no asset for your
machine, oku infers the manifest from the first `[lock]` platform that has one,
and `oku add` pins the package without installing it.

### When inference picks wrong

When the install from an inferred manifest fails, the error says which asset oku
chose for your machine, which other assets fit, and the `oku add --asset`
command that picks one. `--verbose` adds the manifest oku inferred.

```
$ oku add github:owner/repo --asset 'tool-*-macos.zip' --bin tool-cli
```

- `--asset` is a glob that must name exactly one asset of the release, and oku
  uses that asset for your machine.
- `--bin` is the file name of the program inside the assets.
- Both apply to an inferred manifest only. `oku add` fails when you pass them
  for a ref that has a manifest.

Limits of inference:

- The repo has no releases, or no asset for your machine or a `[lock]`
  platform. The error lists the asset names it saw. Pass one to `--asset`.
- The archive holds several executables and none is named after the repo. Pass
  the right one to `--bin`.
- It names the package after the repo, so `github:cli/cli` installs a package
  called `cli` whose program is `gh`. Commit a manifest to choose the name.
- It writes `bin` and `man` only. Completions need a manifest.

### Registry packages

`oku add npm:@scope/name` infers a manifest from the npm registry. It has
`version.from = "npm"`, the registry's download as its artifact, and one
[`bin` table](#run-a-program-through-an-interpreter) for each program in the
package's `bin`. A package that lists dependencies gets an
[npm build](#npm-with-package) instead. `pypi:`, `go:` and `cargo:` refs always
get a build with a [vendor step](#vendoring) and `package`. Where the runtimes
come from is in [Registry packages](../guides/npm-pypi-go-cargo.md).

### Recipes of other package managers

`oku add cask:<token>` and `oku add scoop:<name>` read the recipe of a Homebrew
cask or a Scoop manifest and translate it into a manifest of oku's own. The
manifest downloads from the vendor, the same URLs the recipe uses, and never
from Homebrew or Scoop. After that, the recipe only matters to `oku update`,
which translates it again.

| The recipe says | The manifest gets |
|---|---|
| A download for each platform. For a cask, macOS arm64 and Intel, and Linux when it has a Linux build. For Scoop, each arch. | One `[[artifact]]` per platform, with a `match` |
| A URL with the version in it, `#{version}` or `$version` | `url` with `{{version}}` |
| A cask's `app`, `binary`, `font`, `manpage`, `app_image` | `app`, `bin`, `font`, `man`, and `bin` for an AppImage |
| A cask's `.pkg` or `suite` | The apps that the package installs into Applications, or the apps in the suite. The programs the cask links, found where the package puts them. With no app and no such program, the programs under its `bin` folders, or else the one program named after the cask. oku opens the download to find them, on macOS only. |
| A cask's `artifact` that moves a folder | The folder in the download, for a `binary` inside it |
| Scoop's `bin`, `extract_dir`, `shortcuts` | `bin`, `strip`, and a program with an [`[[app]]`](#apps-and-fonts) launcher for each shortcut |
| Scoop's `bin` with arguments | A [`bin` table](#run-a-program-through-an-interpreter) with `run` and `args` |
| Scoop's `env_add_path` | Every program in that folder of the download, which oku opens to find them |
| Scoop's `depends` | [`[runtime] deps`](#runtime) on `scoop:` refs, without 7zip, lessmsi, innounp and dark, which Scoop needs only to unpack |
| A cask's livecheck, or Scoop's checkver, that reads GitHub releases | `from = "github-releases"` |
| One that follows a redirect | `from = "redirect"` with a `regex`. Without a regex, the regex finds the version in the file name of the download. |
| One that matches a regex at a URL, or reads one JSON key there | `from = "page"` with a `regex` |
| One that reads a Sparkle feed for its short version | `from = "sparkle"` |
| No rule at all, and a download from GitHub releases | `from = "github-releases"` of that repo |
| A cask's `#{version.csv.first}`, `#{version.major}`, `#{version.no_dots}` and the like | `{{version_part1}}`, `{{version_major}}`, `{{version_nodots}}` and the like. A cask's version `1.2.3,45` becomes `1.2.3+45`. |
| A livecheck that joins its regex's groups with commas, or Sparkle's version with no block | `join = "+"` |
| Scoop's `$cleanVersion`, `$majorVersion`, `$minorVersion`, `$patchVersion`, `$underscoreVersion`, `$dashVersion` | `{{version_nodots}}`, `{{version_major}}` and the like |

`oku add cask:owner/tap/token` reads a cask of another tap, the file
`Casks/<token>.rb` of the GitHub repo `owner/homebrew-tap`. The Homebrew API
has the official casks alone, so oku reads the tap's Ruby file for the values
it declares and never runs it:

- `version`, `sha256` with `arm:` and `intel:`, `url`, `desc`, `homepage`,
  `arch arm: ..., intel: ...`, `depends_on arch:`, and the stanzas that place
  files, such as `app` and `binary`.
- `#{version}` and its parts, `#{arch}`, `#{HOMEBREW_PREFIX}` and `#{appdir}`
  in those values.
- `on_arm` and `on_intel` blocks, and local variables that hold a string, as
  in `url_arm = "..."` and then `url url_arm`.
- A `binary` whose target is a folder of shell completions is no program.

A cask that picks a value with Ruby logic, such as `if`, or names another
`#{...}`, is refused. The rest of the translation is the one in the table above.

`oku add aqua:owner/repo` translates the repo's entry in the
[aqua registry](https://github.com/aquaproj/aqua-registry), which names the
release file of each platform:

| The entry says | The manifest gets |
|---|---|
| The rule for the newest releases, `version_constraint: "true"` | Its asset, format, files and checksum. oku leaves out the rules for older releases. |
| `asset` with `{{.Version}}`, `{{.SemVer}}` or `{{trimV .Version}}` | `url` with `{{tag}}` or `{{version}}` |
| `{{.OS}}` and `{{.Arch}}`, with `replacements` and `overrides` | One `[[artifact]]` per platform, each with its own file name |
| `supported_envs`, `rosetta2`, `windows_arm_emulation` | The platforms it covers. Rosetta 2 and Windows emulation run the Intel build on arm64. |
| `files` with a `src` | `bin`. A folder named after the version at the top becomes `strip`. On Windows a program gets `.exe`. |
| `checksum` of type `github_release` with sha256 | `sha256_url` |
| `version_prefix` | `strip_prefix` |
| `type: http` with a `url` | That `url` as the download, and the repo's releases as the version source |

`oku add winget:Publisher.Package` reads the newest version of a package of
[winget's community manifests](https://github.com/microsoft/winget-pkgs).
winget records no rule for new versions, so the manifest follows the GitHub
releases its download comes from, or pins that version with its sha256.

| The installer says | The manifest gets |
|---|---|
| `Architecture` `x64`, `arm64`, `x86` or `neutral` | One `[[artifact]]` per arch, for Windows |
| `InstallerType: portable` | The program itself, named after its first command |
| `zip` with `NestedInstallerType: portable` | `bin` from `NestedInstallerFiles`, with `PortableCommandAlias` as the name. A folder named after the version becomes `strip`. |
| `msi` or `wix` | The programs that `Commands` names, or else the one named after the package. oku opens the MSI to find them, on Windows only. |
| `exe`, `inno`, `nullsoft`, `burn`, `msix`, `appx` | Nothing. These installers run when they install, so oku refuses an arch that has only them. |

When an arch has several installers, oku takes one it can place, then one for
the user's scope, then one for English.

```toml
# Translated from the Homebrew cask obsidian.
[package]
name = "obsidian"
homepage = "https://obsidian.md/"

[version]
from = "page"
repo = "https://raw.githubusercontent.com/obsidianmd/obsidian-releases/master/desktop-releases.json"
regex = "\"latestVersion\"\\s*:\\s*\"([0-9][0-9A-Za-z._+-]*)\""

[[artifact]]
match = { os = "darwin" }
url = "https://github.com/obsidianmd/obsidian-releases/releases/download/v{{version}}/Obsidian-{{version}}.dmg"
bin = [{ name = "obsidian", path = "Obsidian.app/Contents/MacOS/obsidian-cli" }]
app = ["Obsidian.app"]

[[artifact]]
match = { os = "linux", arch = "amd64" }
url = "https://github.com/obsidianmd/obsidian-releases/releases/download/v{{version}}/Obsidian-{{version}}.AppImage"
bin = ["obsidian"]
```

- oku reads the Ruby of a cask as text and never runs it.
- An aqua template that uses a function oku has no match for, such as
  `{{title .OS}}`, fails and names it.
- A URL template counts only when it gives back the recipe's own download
  for the recipe's version, on every platform. Other parts of the URL keep
  the value they have in that download, such as `arm64` for `#{arch}`.
- When the rule's URL differs by platform, each artifact gets its own
  [`version` table](#a-version-for-each-platform).
- When no template gives back the download, when oku has no source for the
  rule, such as a livecheck that runs Ruby, or when a file inside the
  download is named after the version, the manifest pins the recipe's version
  with the recipe's URLs and sha256 digests. A comment at the top says why.
  `oku update` then moves when the recipe moves.
- A manifest that follows versions has no checksum, so oku trusts the first
  download of each version and pins its digest, unless GitHub reports one.
- oku runs no script of a recipe. A script that only sets up the app, such as
  a cask's `postflight` or Scoop's `pre_install`, `post_install` and
  `persist`, stays out of the manifest, and a comment at the top names it. A program whose
  Scoop arguments name a Scoop folder runs without them, and the comment says
  so.
- oku refuses a recipe whose files an installer makes: a cask's `installer`,
  a Scoop `installer` with a `file`, `innosetup`, and a Scoop script that
  unpacks the download. A Scoop arch whose download needs such a script gets
  no artifact. oku refuses a cask with a kernel extension too.
- oku refuses programs that come from two parts of one `.pkg`, because the
  installer puts the parts in one folder and oku keeps each part apart.
- `--asset` and `--bin` do not apply.

### A URL of the download

`oku add https://host/tool-1.2.3-linux-amd64.tar.gz` installs from a URL that is
the package itself. A URL whose path ends in `.toml` is always a manifest. oku
reads any other URL as a manifest first, and takes it as the download when it
does not parse as one or is larger than 1 MiB.

- The inferred manifest has one artifact, for the OS and arch of the machine
  that ran `oku add`. `oku sync` on another kind of machine fails with "no
  artifact".
- The package name is the file name up to its version, and the version is the
  first `1.2.3` in the file name. A file name with no version gives the version
  `0`.
- The version is fixed, so `oku update` never changes it. To get a newer
  version, add that version's URL, or write a manifest that
  [follows a download URL](#follow-a-download-url).
- oku finds the program the way it does in a release asset, and `--bin` names
  it when that fails. `--asset` does not apply.
- A URL on its own has no checksum, so oku trusts the download on first use and
  pins its digest in `oku.lock`.

## The build environment

A `run` step does not see the user's environment. On macOS and Linux it gets:

- `PATH` with each dep's `bin`, then a directory that holds one link per
  `needs` tool, then `/usr/bin`, `/bin`, `/usr/sbin` and `/sbin`
- the [dependency variables](#build-dependencies)
- `HOME` and `TMPDIR` pointing at empty temporary directories
- `OKU_PREFIX`, `OKU_SRC`, `OKU_JOBS`, and the step's `env`

A tool the build uses must therefore be in `needs`. A `needs` tool is on `PATH`
as a link under its own name, not with its directory, so a `cc` from a Nix
profile or a Homebrew `bin` does not put that directory's `python` or `make` on
`PATH` too. The link keeps the tool in its real directory, so a compiler driver
still finds the assembler and linker beside itself.

oku lets two tools read the files they keep in the home directory:

- A `cargo` that rustup manages reads `RUSTUP_HOME`. oku passes it through, and
  `RUSTUP_TOOLCHAIN` when it is set, and lets the build read that directory.
- A `go` reads its GOROOT, which oku asks `go env GOROOT` for.

For any other tool that loads files from elsewhere in the home directory,
depend on a toolchain package or on a system-wide install.

On Windows the link is a shim, and the environment differs, see
[Windows](../guides/windows.md#build-from-source).

## The build sandbox

On macOS and Linux a `run` step runs in a sandbox:

- It has no network.
- It cannot read the user's home directory. The store and the directories of the
  `needs` tools stay readable, even when they are inside it.
- It can only write to the source directory, its temporary `HOME` and `TMPDIR`,
  and `{{prefix}}`. On Linux everything else is mounted read-only, and
  `/dev/shm` is the build's own.
- It cannot ask the user's session to start a program. On macOS Apple Events,
  LaunchServices and `launchctl` are denied. On Linux `/run/user`, which holds
  the user's D-Bus and systemd sockets, and the X server's sockets are hidden.

A build must therefore get everything it downloads through `source`, a `fetch`
step, or a [`vendor` step](#vendoring). oku checks all three against a digest.

`network = true` on a `run` step gives that step the network and nothing else.
oku shows it in the approval prompt as `(wants network)`, and marks the package
`impure` in the store, in `oku.lock` and in `oku info`. Use it only when a
checksummed `fetch` is not enough.

```toml
[[build.step]]
run = "npm ci"
shell = "sh"
network = true
```

oku uses `sandbox-exec` on macOS and user, mount and network namespaces on
Linux. A default Docker container and a default Ubuntu 24.04 forbid
unprivileged user namespaces. On such a Linux host, and on Windows, the step
runs with the scrubbed environment only. oku then warns that the build could
use the network and read the user's files.
