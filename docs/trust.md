# Trust and checksums

oku has no registry, so nobody reviews a manifest for you. oku verifies every
download and pins what it resolved in `oku.lock`. After the first install,
nothing changes unless you run `oku update`.

## What oku verifies

Every artifact download is checked against a sha256. oku takes the expected
digest from the first of these that exists:

1. `sha256` in the manifest.
2. The checksum file at the manifest's `sha256_url`.
3. For a [moving tag](manifest.md#a-moving-tag), the sha256 that the GitHub API
   reports for the file. GitLab, Gitea and Forgejo report none.
4. The digest `oku.lock` pinned for the same package version and URL.

An artifact may also have an `integrity`, a sha512 the way npm publishes it. A
manifest with `version.from = "npm"` gets it from the registry for each version.
oku checks the download against it as well. A download that fits has a
published checksum, so oku does not count it as a first use.

A download that does not match is deleted, and nothing is installed:

```
oku: checksum mismatch for <url>: expected <digest>, download is <digest>
```

## Trust on first use

When none of the four exists, oku accepts the download and says so:

```
hello publishes no checksum, so oku trusted this download and pinned sha256 667f61a0... in ~/.config/oku/oku.lock
```

From then on source 4 applies. The same URL serving different bytes fails on
every machine that uses your lock.

When oku pins [other platforms](list-and-lock.md#one-lock-for-several-machines)
it names them in one notice. It hashes those downloads and does not unpack
them:

```
hello publishes no checksum for linux-amd64-glibc, windows-amd64, so oku trusted those downloads and pinned them in ./oku.lock
```

If you see this notice, the first download is the one you trusted. Prefer
manifests that publish `sha256` or `sha256_url`.

## Inferred manifests

For a repo with no manifest, oku writes one from the release and says so. With
`--verbose` it prints the manifest, so you can read what it installed from. The
lock stores that text. Other machines install from the stored text, and only
`oku update` infers again.

A manifest inferred from a [URL of the download](manifest.md#a-url-of-the-download)
has no checksum to read, so oku always trusts that download on first use.

oku sends a token to the host it is for and to no other: `GITHUB_TOKEN`,
`GH_ENTERPRISE_TOKEN`, `GITLAB_TOKEN`, `GITLAB_SERVER_TOKEN`, `CODEBERG_TOKEN`
and `GITEA_TOKEN`. [Refs](refs.md#how-each-kind-is-fetched) lists the host
of each one.

## Build commands

A manifest with a `[build]` can run commands on your machine. Before the first
build of such a manifest, oku shows every `run` step that applies to your
machine and asks:

```
tree 2.3.2 builds from source and runs these commands on your machine:

  step 1
    make -j{{jobs}}

run them? [y/N]
```

- On a terminal the prompt goes once you answer, and one line stays in its
  place: `✓ approved tree 2.3.2` or `✗ rejected tree 2.3.2`. The packages that
  install in parallel do not write over the question. Their rows wait and
  appear below your answer, and a second package that needs approval asks
  after the first.
- Your answer is recorded for that exact manifest, by its sha256, in
  `<data>/oku/trust/approvals.toml`. The same manifest never asks twice, and a
  manifest that changed asks again.
- When stdin is not a terminal, oku does not ask. It refuses, and `--yes`
  approves. Use `--yes` in scripts only for manifests you have read.
- A dep that builds from source asks for its own approval, before the package
  that needs it.
- An approval applies to one machine. `oku sync` on a new machine asks again.
- A `vendor` step is shown too. It runs the language's package tool with the
  network on, and `oku.lock` pins a digest of what it downloads.
- A manifest with only `install`, `copy`, `fetch` and `extract` steps runs no
  commands and needs no approval, unless an `install` step
  [generates completions](manifest.md#completions).
- An artifact whose completions a command generates runs the download, so it
  asks the same way, with `run it? [y/N]`, and `oku.lock` records
  `commands = true` for that platform.

On macOS and Linux, build commands run in a
[sandbox](manifest.md#the-sandbox) with no network and no access to your home
directory, and with a scrubbed environment. A step marked `(wants network)` in
the prompt gets the network and still cannot read your home directory.

On macOS the developer tools in `/usr/bin` ask `xcrun` where the real tool
is, and `xcrun` caches the answer in your temporary directory, which the
sandbox does not let it write. oku points that cache at the build's own
temporary directory with `xcrun_db`, so `ar`, `install_name_tool` and the
others run without an "Operation not permitted" warning.

The sandbox is not available everywhere. On Windows, and on a Linux host that
forbids unprivileged user namespaces, oku builds without it and says so:

```
tree was built without the sandbox, because this host does not let an unprivileged user set up namespaces (...)
its build commands could use the network and read your files
```

Ubuntu 24.04 and later are such hosts by default. Their AppArmor policy lets a
program create a user namespace and then denies it every mount inside it. To
allow the sandbox there, run
`sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0`, and put the same
setting in a file under `/etc/sysctl.d/` to keep it after a reboot.

The sandbox limits what an approved command can reach. It is not a reason to
approve commands you have not read.

## Projects and the shell hook

The shell hook changes `PATH` when you enter a directory, so it only acts for a
project you allowed with `oku allow`, and only while its `oku.toml` is unchanged.
It never installs, never uses the network, and never runs anything from a
manifest. A package's `[env]` cannot set `PATH`, `LD_PRELOAD` or similar
variables. See [Projects](projects.md#why-a-project-has-to-be-allowed).

## Signing keys

A manifest can name its developer's minisign public key as `signing_key`. oku
then downloads `<artifact url>.minisig` and installs the artifact only when
that key signed it.

`oku.lock` pins the key at the first install. When the manifest later shows
another key, or drops it, `oku sync` and `oku update` stop:

```
oku: foo: oku.lock pinned the signing key RWTr8ko..., and the manifest now has the signing key RWSwtYz...
if the developer announced this change, run the command again with --accept-key
```

A new key is what an attacker who took over the repo would publish, so check
with the developer before you run `oku update foo --accept-key`.

oku trusts the key it sees at the first install. It does not know whether that
key belongs to the developer.

## What the lock pins

| Pinned | Effect |
|---|---|
| Commit of a `github:`, `codeberg:`, `gitea:`, `gitlab:` or `git+` ref | `oku sync` reads the manifest at that commit, even after the branch moves. |
| Manifest sha256 | `oku sync` stops if the manifest content changed. |
| Artifact sha256 per platform | A changed download fails. |
| Signing key | oku refuses a manifest with another `signing_key`, or with none, until you pass `--accept-key`. |
| Vendor digest per platform | A build whose vendor steps download something else fails, and nothing is kept. |

## When oku stops

`oku sync` refuses to continue in two cases, and both name the fix.

The manifest differs from the lock. This happens with local files and URLs,
which have no commit to pin:

```
oku: ripgrep: the manifest changed since oku.lock was written
run `oku update ripgrep` to accept it
```

The checksum file at `sha256_url` now holds a different digest for a version
and URL that the lock already pinned:

```
oku: ripgrep: checksum changed: upstream publishes sha256 <new>, oku.lock pinned <old>
run `oku update ripgrep` to accept the new checksum
```

oku cannot tell an upstream that replaced a release file from an attack. Check
with the author before you accept it.

`oku update` is the only command that accepts changes. Read its output, and
read the lock diff before you commit it.

## What oku does not protect against

- A manifest that was malicious the first time you added it. Read manifests
  from sources you do not know.
- A `sha256_url` on the same host as the download. It catches corruption and
  in-place tampering after you locked, not a compromised host on first use.
- A `signing_key` that was the attacker's at your first install. oku pins the
  first key it sees.
- The sources of a `[build]`. `signing_key` covers artifacts only, and `fetch`
  steps rely on their `sha256`.
- A build command you approved, on a host where the sandbox is not available.
- On Linux the sandbox hides your home directory and the network. It does not
  stop writes to other places your user can already write to.

## Archives

oku unpacks archives and installers itself and never runs anything a package
ships during install. That includes the maintainer scripts of a `.deb`, the
scriptlets of an `.rpm`, and the install scripts of a macOS `.pkg`. It refuses
entries that are absolute or contain `..`, and symlinks that resolve outside
the package. After unpacking, oku follows every symlink in the package the way
the OS would, including through other links. It refuses the package when a link
leads outside it. So `x -> .` followed by `x/l -> ../outside` fails, although
each entry looks safe alone.

## Packages from a cache

A [build cache](caches.md) serves packages that someone else built. oku accepts
an entry only when its minisign signature comes from a key you added with
`oku key trust`. It ignores every other entry and builds the package itself. A
trusted entry skips the build approval, because oku runs no command of the
manifest.
