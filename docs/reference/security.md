# Security

What oku checks, what it pins, what it asks you before, and what it does not
protect against.

oku has no registry, so nobody reviews a manifest for you. oku verifies every
download, pins what it resolved in [`oku.lock`](lock.md), and changes nothing
after the first install unless you run `oku update`.

## What oku verifies

oku checks every artifact download against a sha256. It takes the expected
digest from the first of these that exists:

1. `sha256` in the manifest.
2. The checksum file at the manifest's `sha256_url`.
3. The sha256 that the version source publishes for the file. With
   `github-releases` it is the one the GitHub API reports for a release file,
   which GitHub has for most files uploaded since mid 2025. With `crates` it is
   the one crates.io publishes for the `.crate` file. GitLab, Gitea and
   Forgejo report none.
4. The digest `oku.lock` pinned for the same package version and URL.

An artifact may also have an `integrity`, a sha512 the way npm publishes it. A
manifest with `version.from = "npm"` gets it from the registry for each
version, and oku checks the download against it too. Such a download has a
published checksum, so oku does not trust it on first use.

oku deletes a download that does not match, and installs nothing:

```
oku: checksum mismatch for <url>: expected <digest>, download is <digest>
```

## Minimum release age

When an attacker publishes a version from a stolen account, the registry
usually removes it within hours. On 2025-09-08 the malicious chalk 5.6.1 and debug
4.4.2 were on npm for about two hours. So `add`, `update`, `sync` and
`oku shell` take the newest version that came out at least a day ago, and
leave a newer one waiting. [`oku outdated`](commands.md#oku-outdated) lists
what waits and when oku takes it.

- `[lock]` `min_release_age` in `oku.toml` sets the age for the list, and
  `min_release_age` on a package sets it for that one. `"0"` turns it off. See
  [`[lock]`](oku-toml.md#lock).
- `--min-release-age 0` on `add`, `update`, `sync` or `oku shell` takes the
  newest version for one run, such as for a security fix.
- A version you name exactly, as in `oku add github:x/y@1.2.3`, and a version
  that `oku.lock` pins skip the check.
- It never takes a package back to an older version than `oku.lock` holds,
  such as one you took with `--min-release-age 0`.
- When every version that fits is too new and the package is new to the lock,
  the command stops and names the version that passes first, and when.
- A moving tag such as `nightly` and a `git-branch` are new by design and skip
  the check.
- `oku self update` follows the global list's age too, so by default a new
  oku release waits a day. `--to <tag>` and `--nightly` skip it. The install scripts, which
  run before oku exists on the machine, take the newest release.

oku reads when each version came out from its source: the publish time of a
GitHub, GitLab, Gitea or Forgejo release, the npm registry, PyPI, crates.io and
the `pubDate` of a Sparkle feed's item. The Go module proxy gives the time of
the version's commit, which its author sets.

A GitLab release's author can date it in the past, so oku counts from the
later of that date and the time GitLab made the release. A Sparkle feed's
`pubDate` is what its publisher writes, and oku cannot check it.

`git-tags`, `page` and `redirect` give no time, and neither does a Sparkle item
without a `pubDate`. When each artifact finds its own version, as in a
translated cask, oku asks for each platform's new version, and names a platform
other than this machine's, as in `tool 2.0.0 for linux-arm64`. `[lock]`
`unknown_release_age` in `oku.toml` says what oku does with a new version from
such a source:

| Value | What happens |
|---|---|
| `"warn"` | The default. On a terminal oku asks `take it?`. Without one, as in CI, it does not take the version. |
| `"refuse"` | oku does not take the version. |
| `"allow"` | oku takes the version and says that it could not check it. |

- When oku does not take a new version of a package that `oku.lock` holds, the
  package stays at its locked version, oku says so, and the rest of the
  command goes on.
- For a package that is new to the lock there is nothing to keep, so the
  command stops and says how to go on.
- `--accept-unknown-age` on `add`, `update`, `sync` or `oku shell` takes such
  versions for one run without asking.
- `min_release_age = "0"` on a package turns the check off for it, so oku
  neither asks nor refuses.
- A version you name exactly and a version `oku.lock` pins are never asked
  about, and neither are a moving tag and a `git-branch`.

The packages that an `npm:` or `pypi:` build installs with it come from before
that version was published, and a `go:` or `cargo:` build takes the versions
its own lock file names. So the age of the version you add covers them too.

## Malicious packages

Before oku takes a new version of an `npm:`, `pypi:`, `cargo:` or `go:`
package, it asks the [OSV](https://osv.dev) database at `api.osv.dev` whether
the [OpenSSF malicious packages](https://github.com/ossf/malicious-packages)
list names that version. Those advisories have ids that start with `MAL-`.
oku does not install a version that one names:

```
oku: npm:nx 21.5.0: OSV lists it as malicious in MAL-2025-41443
add another version with @<version>
```

- A package that `oku.lock` holds stays at its locked version, oku says why,
  and the rest of the command goes on.
- `oku add --plan` stops the same way.
- A build asks about every package its `vendor` step installs too: the npm
  tree, the PyPI packages uv or pip got, the crates of `Cargo.lock` and the Go
  modules. oku asks once the step downloaded them, before any install script
  or build step runs, and stops the build at a malicious one:

  ```
  oku: tool: the npm package step installed packages that OSV lists as malicious:
    left-pad 1.3.0, see https://osv.dev/vulnerability/MAL-2025-2
  ```

  A build from a [cache](../guides/build-caches.md) runs no vendor step, so
  oku asks nothing about it.
- oku does not ask again about a version that `oku.lock` pins.
- When oku cannot reach OSV, it installs the version and says what it could
  not check.
- OSV lists other advisories too, such as a known vulnerability. oku refuses
  only the `MAL-` ones, and some older malicious packages have none.

## Trust on first use

When none of the four exists, oku accepts the download and says so:

```
hello publishes no checksum, so oku trusted this download and pinned sha256 667f61a0... in ~/.config/oku/oku.lock
```

From then on the lock's digest applies. When the same URL later serves other
bytes, the install fails on every machine that uses your lock. When oku pins
[other platforms](lock.md#pins-for-other-platforms) it names them in one notice.
It hashes those downloads and does not unpack them:

```
hello publishes no checksum for linux-amd64-glibc, windows-amd64, so oku trusted those downloads and pinned them in ./oku.lock
```

The first download is the one you trusted. Prefer manifests that publish
`sha256` or `sha256_url`.

- `oku sync --locked` checks the lock before it downloads anything, so it never
  trusts a download on first use.
- `oku shell` writes no lock, so without a published checksum it trusts the
  download again each time.
- `oku manifest hash` trusts the download it gets. Compare its output with a
  checksum the project publishes.

`[lock]` `unverified` in `oku.toml` says whether oku may trust a first
download:

| Value | What happens |
|---|---|
| `"allow"` | The default. oku trusts the download and says so, as above. |
| `"warn"` | On a terminal oku asks `trust it?`, once for every platform of the same version. Without one, as in CI, it does not trust it. |
| `"refuse"` | oku does not trust it. |

```
oku: nothing states a digest for the download of hello 1.2.0 for darwin-arm64, and [lock] unverified refuses to trust a first download
run the command with --accept-unverified, or ask the developer to publish a checksum
```

- When oku does not trust the new version of a package that `oku.lock` holds,
  the package stays at its locked version, oku says so, and the rest of the
  command goes on. A package that is new to the lock stops the command.
- `--accept-unverified` on `add`, `update`, `sync`, `shell` or `run` trusts
  such downloads for one run without asking.
- oku never asks about a digest that `oku.lock` already pins.

## Weaker checks

`oku.lock` records what oku checked each download against, such as a signature,
the manifest's `sha256`, a checksum file, or nothing. See
[What oku checked](lock.md#what-oku-checked). When `oku update` or `oku add`
would pin a new download that oku checks more weakly, it stops and names both
checks:

```
oku: tool: oku.lock checked the download for darwin-arm64 against the digest that its source publishes, and nothing states a digest for the new one, so oku would trust its first download
if the developer announced this change, run the command again with --accept-weaker-check
```

A project can stop publishing checksums for good reasons. Someone who replaced
a release would remove them too, so check before you pass
`--accept-weaker-check`.

- A signature by a `signer_workflow` or a `signing_key` is the strongest
  check, then a digest in the manifest, then a checksum file or a digest the
  source publishes, which rank the same, then nothing.
- oku compares the check for each platform it pins, your own and those of
  `[lock]`.
- `oku sync` installs the pins of `oku.lock`, so it never stops for this.
- oku does not compare a package whose ref you changed, since you chose the
  new source.
- `oku add --plan` stops the same way.
- A lock written before oku recorded the check has nothing to compare. The
  first `oku update` records the check without comparing it.

## Inferred manifests

For a repo with no manifest, oku writes one from the release and says so.
`--verbose` prints it, so you can read what it installed from. The lock stores
that text, other machines install from the stored text, and only `oku update`
infers again.

A manifest inferred from a URL of the download has no checksum to read, so oku
always trusts that download on first use.

An inferred manifest keeps the Sigstore signatures, GitHub attestations and
SLSA provenance that the release holds, when their certificate names a run
for the repo itself, see
[How inference finds signatures](manifest.md#how-inference-finds-signatures).
Without those, it keeps a cosign key that the release holds, when the key
signed the checksum file or the asset. oku takes the workflow or the key from
the first release you add, and `oku.lock` pins it. The key comes from the same
release as the files, so the first install trusts it as a first use. After
that, a changed key stops `oku update` until `--accept-key`.

## Tokens

oku sends a forge token to the host it is for and to no other, over https, and
not across a redirect to another host. The token of a server of your own goes
only to a host that `[forge] hosts` in `config.toml` lists, since a manifest
names the host oku reads. When `GITHUB_TOKEN` or
`GH_ENTERPRISE_TOKEN` is not set and `gh` is on `PATH`, oku runs
`gh auth token --hostname <host>` and sends that login to the same host only.
The variables and their hosts are in [tokens per host](refs.md#tokens-per-host).

## Downloads

- oku follows at most 10 redirects, never from https to plain http, and never
  to a `file://` URL.
- A manifest or a list ref over plain `http://` is an error, except on this
  machine, such as `http://127.0.0.1`. `oku manifest lint` refuses an
  `http://` download without a `sha256`.
- Only a manifest that is a file on this machine, or in a `git+file://` repo,
  may name a `file://` URL. A manifest from anywhere else could otherwise read
  a file such as `~/.aws/credentials` into the store or into a build.
- A git collection reads its manifests inside the clone. When a manifest file
  is a link out of the repo, oku reads none of the collection, and
  `oku search` skips that source and names the file.

## Where oku connects

`[network]` in `config.toml` says which hosts oku connects to. A list or a
project `oku.toml` cannot change it, so a repo you clone cannot either.

- oku does not connect to a private address: loopback, `10.0.0.0/8`,
  `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, link-local such as the
  cloud metadata address `169.254.169.254`, `fc00::/7`, and multicast. A
  manifest could otherwise make oku read a service on your network.
- A URL that names this machine, such as `http://127.0.0.1:8080` or
  `localhost`, still works, since you named it. A public name that resolves to
  loopback does not, and neither does a redirect to this machine from another
  host.
- oku checks the address it connects to, after DNS, on every redirect. When a
  second lookup of a name gives a private address, oku does not connect.
- `private` names the hosts that may resolve to a private address. The hosts
  of `caches` and `[forge] hosts` may already. `deny_private = false` turns the
  check off.
- `allow`, when set, lists the only hosts oku connects to, such as for a
  machine that may reach only a mirror. A URL of this machine and the hosts of `caches` and
  `[forge] hosts` pass too.

```toml
[network]
private = ['git.corp.example']
allow = ['github.com', 'api.github.com', '*.githubusercontent.com', 'git.corp.example']
```

A release file on GitHub redirects to `release-assets.githubusercontent.com`,
and a manifest to `raw.githubusercontent.com`, so an allow list for GitHub
names both. A manifest with [Sigstore signatures](#sigstore-signatures-of-a-manifest)
also needs `tuf-repo-cdn.sigstore.dev`, where oku reads Sigstore's trust root,
`tuf-repo.github.com` for the attestations of a private repo,
`rekor.sigstore.dev` for a cosign signature or a provenance envelope without
a bundle, and with
attestations the host where GitHub stores large ones. A new version of an
npm, PyPI, crates.io or Go package needs `api.osv.dev`, see
[Malicious packages](#malicious-packages). oku refuses a host and names it:

```
oku: fetch https://example.org/x.toml: Get "https://example.org/x.toml": example.org is not in [network] allow in config.toml, so oku does not connect there
```

What it does not cover:

- Behind `HTTPS_PROXY` or `HTTP_PROXY`, oku connects to the proxy, which
  resolves the name. oku checks the host names and not their addresses.
- oku checks a git URL before it runs `git`, and git follows no http
  redirect. An `insteadOf` in your git config that sends the URL elsewhere is
  your own, and oku does not check where it leads.
- A build step with the network on runs its own tool, and `[network]` does
  not reach it. That covers a `vendor` step, a `run` step with
  `network = true`, and an npm install script. See [Sandbox](sandbox.md).

## Trusted sources

A project's `oku.toml` comes with the repo, and someone else may have written
it. Before oku installs from it, you trust each source it names. A source is who wrote
the manifest:

| Ref | Source |
|---|---|
| `github:acme/tool`, `gitea:host/acme/tool`, `gitlab:acme/group/tool` | the owner or top group, `github:acme` |
| `https://example.com/x.toml`, `git+https://example.com/x` | the host, `example.com` |
| `npm:@scope/name` | the scope, `npm:@scope` |
| `npm:name`, `pypi:`, `cargo:`, `go:`, `cask:`, `scoop:`, `aqua:`, `winget:` | the package, such as `pypi:ruff` |
| a path on this machine, `localhost` or `127.0.0.1` | yours, and needs no trust |

oku checks the packages, includes and `[runtimes]` of the project's own
`oku.toml`:

- On a terminal, `sync`, `update` and `add` name the sources you have not
  trusted and ask once.
- Without a terminal they stop, and so do `outdated`, `list` and the other
  commands that read the project's list. `--yes` on `sync`, `update` or `add`
  trusts them, which the [GitHub Action](../guides/ci.md) passes.
- [`oku allow`](commands.md#oku-allow-oku-deny) trusts them too.

```
oku: /home/you/work/api installs from sources you have not trusted:
  github:acme for tool
    tool downloads from downloads.acme.dev
  npm:@scope for lint
run `oku allow` to trust them, or pass --yes to oku sync
```

The source is who wrote the manifest, and the manifest may download from
somewhere else. When the project's `oku.lock` pins a package, oku names who
serves its downloads for this machine, when that differs from the source. On a
host of many owners, such as `github.com`, that is the host and the owner, as in
`github.com/acme`. oku leaves out npm, PyPI, crates.io and Go packages, whose
downloads come from their registry. A package that the lock does not pin gets
no such line, since oku reads nothing from the source before you trust it.
`[network] allow` limits the hosts themselves, see
[Where oku connects](#where-oku-connects).

You trust a source without being asked when you:

- type it, as in `oku add github:acme/tool`
- list it in `[trust] sources` in `config.toml`, as the owner such as
  `"github:acme"` or as one repo such as `"github:acme/tool"`

oku keeps the rest of what you trusted in `trust/sources.toml` in the data
directory. The global list is yours, so it needs no trust. oku also trusts a dep or an
include that a trusted source names, since the lock pins what it resolved to.

A `git pull` in your config directory is yours too, and oku does not check
what it brings. Read the changes before you run `oku sync`.

## Changed included lists

You trust an included list as it was when you added it. A list from a repo can
place files, change settings and set variables, so its author's later edits
need your yes too. `oku sync` reads a list from a repo or a URL at the commit
and sha256 that `oku.lock` pinned, and stops when its content changed.
`oku update` with no names reads it fresh. When the content changed, it shows
the lines that were added and removed, each under its table, and asks:

```
the included list git+https://example.com/machines#base.toml changed since oku.lock was written:
  [packages]
  + other = "github:acme/other"
  [files]
  - "{{home}}/.gitconfig" = { link = "./files/gitconfig" }
? take the change? y/N
```

- A no changes nothing.
- Without a terminal it stops. `--yes` takes the change and says so.
- A list at a URL has no commit, so oku cannot read the version it pinned.
  It shows the whole new list instead.
- A local list is your own file, and oku reads it as it is.

## Approve build commands

A manifest with a `[build]` can run commands on your machine. Before the first
build of such a manifest, oku shows every `run` step that applies to your
machine and asks:

```
tree 2.3.2 builds from source and runs these commands on your machine:

  step 1
    make -j{{jobs}}

? run them? y/N
```

- oku records your answer for that exact manifest, by its sha256, in
  `<data>/oku/trust/approvals.toml`. The same manifest never asks twice, and a
  manifest that changed asks again.
- When stdin is not a terminal, oku refuses, and `--yes` approves. Use `--yes`
  in scripts only for manifests you have read.
- A package that `oku.lock` holds stays at its locked version when you do not
  approve the build of its new version, or when oku cannot ask without a
  terminal. oku says so, and the rest of `update` or `sync` goes on.
- oku installs that locked version from the manifest the lock pins. You
  approved its build before, so oku does not ask again.
- A package that is new to the lock has no version to keep, so the command
  stops. So does a local manifest file you edited, since its old text is
  gone.
- A dep that builds from source asks for its own approval, before the package
  that needs it.
- A package that sets [`[env]`](manifest.md#env) asks the same way, even when
  it installs from a download, and lists each variable with its value. A
  variable such as `GIT_CONFIG_*` or `PAGER` can make another program run code
  in every shell. `oku run` and `oku shell` ask too.
- An approval applies to one machine. `oku sync` on a new machine asks again.
- The prompt shows `vendor` steps too. A vendor step runs the language's package tool with the
  network on, and `oku.lock` pins a digest of what it downloads.
- A manifest with only `install`, `copy`, `fetch` and `extract` steps runs no
  commands and needs no approval, unless an `install` step generates
  completions or the package sets `[env]`.
- An artifact whose completions a command generates runs the download, so it
  asks `run it?`, and `oku.lock` records `commands = true`.
- A step marked `(wants network)` in the prompt gets the network.
- oku prints a control character in a package's text as `\x1b` and the like,
  in the prompt, in `--plan`, in `oku info`, in search results and in errors.
  A manifest cannot hide part of a command it asks you to approve, move the
  cursor over the prompt, or write to your terminal's clipboard.
- A package from a trusted cache needs no approval for its commands, because
  oku runs none of them. It still asks for its `[env]`.

On a terminal the prompt disappears once you answer, and one line stays:
`✓ approved tree 2.3.2` or `✗ rejected tree 2.3.2`. Packages that install in
parallel wait below it, and a second package that needs approval asks after the
first.

## The build sandbox

On macOS and Linux, build commands run in a sandbox with no network, no access
to your home directory, your `ssh-agent` or your processes, and a scrubbed
environment. Windows, and a Linux host
that forbids unprivileged user namespaces, have no sandbox. oku says so in the
approval prompt and after the build, and `require_sandbox = true` in
`config.toml` makes it refuse instead. See [The build sandbox](sandbox.md) for
what each platform blocks.

The sandbox limits what an approved command can reach. It is not a reason to
approve commands you have not read.

## Signing keys of a manifest

A manifest can name its developer's minisign public key as `signing_key`. oku
then downloads `<artifact url>.minisig` and installs the artifact only when
that key signed it, and when the signed comment names that file as
`file:<name>`, which `minisign -S` writes, or holds the version, as in
`tool 1.2.3`. The key signs every release, so without that check an older
signed file served at the new version's URL would pass. With a cosign public
key, oku checks the bundles and signatures that the artifacts name instead,
and each must be in Sigstore's transparency log.
[Cosign key](manifest.md#cosign-key) gives the format.

`oku.lock` pins the key at the first install. When the manifest later shows
another key, or drops it, `oku sync` and `oku update` stop:

```
oku: foo: oku.lock pinned the signing key RWTr8ko..., and the manifest now has the signing key RWSwtYz...
if the developer announced this change, run the command again with --accept-key
```

A new key is what an attacker who took over the repo would publish. Check with
the developer before you run `oku update foo --accept-key`. oku trusts the key
it sees at the first install, and does not know whether that key belongs to the
developer. To check that too, [pin the key yourself](#pin-a-signer-yourself).

## Sigstore signatures of a manifest

A manifest can name the GitHub Actions workflow that signs its releases with
[Sigstore](https://www.sigstore.dev), as `signer_workflow`, and where its
signatures are: GitHub artifact attestations, or a bundle or a cosign
signature and certificate beside the download or beside its checksum file. A
manifest can also name the download's [SLSA provenance](manifest.md#slsa-provenance)
from slsa-github-generator, whose builders oku trusts as slsa-verifier does. See
[Sigstore signatures](manifest.md#sigstore-signatures). oku installs the
artifact only when the signature shows that:

- Sigstore's certificate authority issued the certificate to that workflow,
  run by GitHub Actions, at any ref of the workflow file
- the run was for the repo of the releases, so a workflow that other repos
  share cannot sign for this one
- for a bundle, the run was for the release's tag, so an older signed file
  cannot pass for a newer release
- Sigstore's transparency log holds the signature
- the signature covers the bytes oku downloaded, or the checksum file whose
  sha256 for the download matched

```
oku: tool: signature check failed: the bundle at https://github.com/you/tool/releases/download/v1.2.0/checksums.txt.sigstore.json does not show that you/tool/.github/workflows/release.yml signed https://github.com/you/tool/releases/download/v1.2.0/checksums.txt: ...
```

oku reads Sigstore's public trust root through TUF from
`tuf-repo-cdn.sigstore.dev`, checks it against the root that ships inside
oku, and keeps it in its cache for a day. GitHub signs the attestations of a
private repo with a Sigstore of its own. For a bundle whose certificate GitHub
issued, oku reads GitHub's trust root the same way from `tuf-repo.github.com`.
It then requires a timestamp of GitHub's timestamp authority in place of the
transparency log, as `gh attestation verify` does.

`oku.lock` pins `signer_workflow` at the first install. When the manifest
later names another workflow, or none, `oku sync` and `oku update` stop:

```
oku: tool: oku.lock pinned the signer workflow you/tool/.github/workflows/release.yml, and the manifest now has the signer workflow you/tool/.github/workflows/other.yml
if the developer announced this change, run the command again with --accept-key
```

A Sigstore signature needs no key for the developer to keep safe. It shows
which workflow built the file, and nothing more. Someone who can push to the
repo and run that workflow can sign what they like.

## Pin a signer yourself

oku trusts the `signing_key` or `signer_workflow` that a manifest names at the
first install. When the developer publishes their key or workflow somewhere
else, such as their website, write it on the package's entry in `oku.toml`
before you install it:

```toml
[packages]
foo = { ref = "github:acme/foo", signing_key = "RWTr8koGkq7wBGTVdGedU8b7CkkiIu+LBp6uQLJ7ltH/7iGXMKNF1b27" }
bar = { ref = "github:acme/bar", signer_workflow = "acme/bar/.github/workflows/release.yml" }
```

Then run `oku sync`. oku checks the first download against your pin, so
nothing is trusted on first use.

- `signing_key` applies to the manifest whether it names a key or not, so oku
  checks the `.minisig` files of a release even when its manifest says
  nothing of them. A manifest that names another key fails:

  ```
  oku: foo: oku.toml pins the signing key RWTr8ko..., and the manifest names RWSwtYz...
  ```

- `signer_workflow` must be the manifest's own, since the manifest says where
  the signatures are. A manifest that names another workflow, or no Sigstore
  signature, fails.
- Your pin takes the place of the lock's. When you change it in `oku.toml`,
  `oku sync` takes the new one without `--accept-key`.
- `oku add` and `oku update` keep the pins on the entry.

## When oku stops

`oku update` is the only command that accepts a change upstream. `oku sync`
stops, and names the fix, when:

- the manifest differs from the lock. This happens with local files and URLs,
  which have no commit to pin:

  ```
  oku: ripgrep: the manifest changed since oku.lock was written
  run `oku update ripgrep` to accept it
  ```

- the checksum file at `sha256_url` now holds another digest for a version and
  URL that the lock pinned:

  ```
  oku: ripgrep: checksum changed: upstream publishes sha256 <new>, oku.lock pinned <old>
  run `oku update ripgrep` to accept the new checksum
  ```

oku cannot tell an upstream that replaced a release file from an attack. Check
with the author before you accept it, and read the lock diff before you commit
it. Every pin and what it stops is in [what each pin does](lock.md#what-each-pin-does).

## Archives

oku unpacks archives and installers itself and never runs anything a package
ships during install. That includes the maintainer scripts of a `.deb`, the
scriptlets of an `.rpm`, and the install scripts of a macOS `.pkg`.

- It refuses entries that are absolute or contain `..`, and symlinks that
  resolve outside the package.
- After unpacking, it follows every symlink in the package the way the OS
  would, through other links too, and refuses the package when a link leads
  outside it. For example, `x -> .` followed by `x/l -> ../outside` fails, although each
  entry looks safe alone.
- It refuses an archive that unpacks to more than 32 GiB, and an `.xz` file
  that asks for a dictionary over 128 MiB, which the decoder would allocate
  before it reads any data. A small download cannot fill the disk or the
  memory.

## Projects and the shell hook

The shell hook changes `PATH` when you enter a directory, so it acts only for a
project you allowed with `oku allow`, and only while its `oku.toml` is
unchanged. oku records the allow in `<data>/oku/trust/allow.toml` with the
list's sha256.

- The hook never installs, never uses the network, and never runs anything
  from a manifest.
- `oku exec` inside a project you have not allowed refuses to run and names
  `oku allow`.
- A package's `[env]` cannot set `PATH`, `LD_PRELOAD` or similar variables.
  A list's `[env]` can only put entries in front of `PATH`, and cannot set the
  others.
- A list's `[env]` sets values and reads variables as `${NAME}`. It runs no
  command, and neither does a `.env` file it loads.
- oku decrypts a `secret = true` `.env` file in memory and writes no
  decrypted copy. A file of `scope = "exec"` loads for `oku exec` only, never
  for the shell.
- The allow covers each `oku.<env>.toml`, `oku.local.toml` and `.env` file of
  the project that git tracks, whatever `OKU_ENV` names, so a pull
  that changes one stops the hook until a new `oku allow`. oku asks git about
  a file that git did not track only when the file changes.
- A project list may not hold `[files]`, `[vars]`, `[secrets]` or settings
  tables, so a cloned repo cannot write into your home directory.

See [Projects](../guides/projects.md).

## Signed caches

A [build cache](../guides/build-caches.md) serves packages that someone else
built. oku accepts an entry only when its minisign signature comes from a key
you added with `oku key trust`.

- oku ignores an entry with no signature, a signature by another key, or a
  file that changed after it was signed, and builds the package itself:

  ```
  ignored https://example.com/oku-cache/jq-1.7.1-0c1d5a3f9e2b7a41.tar.zst, no trusted key signed it
  ```

- Get a cache's public key from its owner directly, because oku installs
  whatever that key signed without asking.
- `oku key revoke` stops trusting a key. Packages already installed stay.
- `oku cache push` refuses a build that had network access, because it can
  differ from run to run.
- `signing.key` has no password, so a push can run in CI. Keep it private.
  `oku self uninstall` deletes it, even with `--keep-list`.
- The signatures are plain minisign signatures, so `minisign -V` verifies them
  too.

## Self update signatures

`oku self update` downloads `oku-<os>-<arch>` and its minisign signature from
the GitHub releases of `y3owk1n/oku` over HTTPS. It replaces itself only when:

- the release key built into the running binary made the signature, and
- the signed comment names the release, as `oku v0.5.0` or `oku nightly`. This
  stops an older release, which the same key signed, from passing for a newer
  one.

oku writes nothing near the running binary before that check passes, so a
failed check leaves oku as it was.

Every nightly binary is signed `oku nightly`, so `oku self update --nightly`
also checks the nightly's `checksums.txt`. Its signed comment is the full
version, such as `oku nightly-20260927084032-b796323`. It must name the commit
of the release, be newer than the nightly that runs, and list the sha256 of
the binary. An older nightly served as the newest one fails.

The release key is:

```
RWSjFGqIxI8IPGwKE/uRgugZ51qCEMe1CDbFRVTMUAuin42JiOxg2HNW
```

The install scripts check the sha256 of the binary always. When `minisign` is
installed, they also check its signature against the same key and read the
signed comment. The comment must be `oku <OKU_VERSION>` when you name a
release, and any `oku v...` release otherwise. `OKU_REQUIRE_SIGNATURE=1`
makes them refuse to install without that check, and the GitHub Action always
sets it.

The sha256 alone only guards against a broken download, since `checksums.txt`
comes from the same release as the binary. The signature is what shows the
binary is oku's.

### When the release key changes

An installed oku trusts exactly one key, the one built into it.

- A planned change ships one release signed by the old key whose binary trusts
  the new key. Update to that release before a later one exists. An oku older
  than it cannot update once a later release exists, and fails with
  `is not signed by <old key>` and a line that says to run the install script
  again. Run it, and it puts the newest binary in place.
- After a leak, no handover release exists. `oku self update` refuses every
  new release, which is the intended result. Run the install script again,
  and read the release notes for versions to distrust. Until you reinstall,
  your oku accepts any release file the leaked key signed, but an attacker
  also needs write access to the repo's releases to use it.

How the maintainer rotates the key is in
[CONTRIBUTING](../../CONTRIBUTING.md#rotate-the-signing-key).

## Check the store

oku records the sha256 of every file of a package when it installs it.
[`oku verify`](commands.md#oku-verify) hashes them again and names each file
that changed, appeared or went away since then. `oku verify --repair` removes
those packages, and `oku sync` downloads them again and checks them against
`oku.lock`.

On macOS and Linux each store path is also read-only, so a program cannot
write into a package by mistake. [The store](paths.md#the-store) says how. A
program that runs as you could still make a package writable again and change
it and its record together. `verify` finds a corrupted disk or an edit by
mistake, not malware on your account.

## What oku does not protect against

- A manifest that was malicious the first time you added it. Read manifests
  from sources you do not know.
- A malicious version that nobody finds within the minimum release age, or a
  release time that the source reports wrong.
- A `sha256_url` on the same host as the download. It catches corruption and
  tampering after you locked, not a compromised host on first use.
- A `signing_key` or `signer_workflow` that was the attacker's at your first
  install, unless you [pinned your own](#pin-a-signer-yourself). oku pins the
  first one it sees.
- Someone who can push to the repo and run its release workflow. The
  workflow's Sigstore signature covers whatever that run built.
- An older release file that the workflow attested, served under a newer
  release's address. An attestation names no tag, so oku cannot tell.
- The sources of a `[build]`. `signing_key` covers artifacts only, and `fetch`
  steps rely on their `sha256`.
- A build command you approved, on a host where the sandbox is not available.
- A build that asks a service outside the sandbox to start a program, such as
  a launchd job that is already loaded on macOS, or a daemon whose socket
  appeared while the step ran on Linux before 7.1. The sandbox blocks the ways
  that [The build sandbox](sandbox.md) lists, not every way.
- A cache key you trusted that signs something malicious.
