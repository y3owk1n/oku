# Trust and checksums

oku has no registry, so nobody reviews a manifest for you. oku verifies every
download and pins what it resolved in `oku.lock`. After the first install,
nothing changes unless you run `oku update`.

## What oku verifies

Every artifact download is checked against a sha256. oku takes the expected
digest from the first of these that exists:

1. `sha256` in the manifest.
2. The checksum file at the manifest's `sha256_url`.
3. The digest `oku.lock` pinned for the same package version and URL.

A download that does not match is deleted, and nothing is installed:

```
oku: checksum mismatch for <url>: expected <digest>, download is <digest>
```

## Trust on first use

When none of the three exists, oku accepts the download and says so:

```
hello publishes no checksum, so oku trusted this download and pinned sha256 667f61a0... in ~/.config/oku/oku.lock
```

From then on source 3 applies. The same URL serving different bytes fails on
every machine that uses your lock.

If you see this notice, the first download is the one you trusted. Prefer
manifests that publish `sha256` or `sha256_url`.

## Inferred manifests

For a repo with no manifest, oku writes one from the release and prints it
before it installs, so you can read what it is about to do. The lock stores that
text. Other machines install from the stored text, and only `oku update` infers
again.

## Build commands

A manifest with a `[build]` can run commands on your machine. Before the first
build of such a manifest, oku shows every `run` step that applies to your
machine and asks:

```
tree 2.3.2 builds from source and runs these commands on your machine:

  step 0
    make -j{{jobs}}

run them? [y/N]
```

- Your answer is recorded for that exact manifest, by its sha256, in
  `<data>/oku/trust/approvals.toml`. The same manifest never asks twice, and a
  manifest that changed asks again.
- When stdin is not a terminal, oku does not ask. It refuses, and `--yes`
  approves. Use `--yes` in scripts only for manifests you have read.
- A dep that builds from source asks for its own approval, before the package
  that needs it.
- An approval applies to one machine. `oku sync` on a new machine asks again.
- A manifest with only `install`, `copy`, `fetch` and `extract` steps runs no
  commands and needs no approval.

On macOS and Linux, build commands run in a
[sandbox](manifest.md#the-sandbox) with no network and no access to your home
directory, and with a scrubbed environment. A step marked `(wants network)` in
the prompt gets the network and still cannot read your home directory.

The sandbox is not available everywhere. On Windows, and on a Linux host that
forbids unprivileged user namespaces, oku builds without it and says so:

```
tree was built without the sandbox, because this host does not allow unprivileged user namespaces (...)
its build commands could use the network and read your files
```

The sandbox limits what an approved command can reach. It is not a reason to
approve commands you have not read.

## What the lock pins

| Pinned | Effect |
|---|---|
| Commit of a `github:` or `git+` ref | `oku sync` reads the manifest at that commit, even after the branch moves. |
| Manifest sha256 | `oku sync` stops if the manifest content changed. |
| Artifact sha256 per platform | A changed download fails. |

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
- Signatures. Manifests cannot declare a signing key yet.
- A build command you approved, on a host where the sandbox is not available.
- On Linux the sandbox hides your home directory and the network. It does not
  stop writes to other places your user can already write to.

## Archives

oku unpacks archives itself and never runs anything a package ships during
install. It refuses entries that are absolute or contain `..`, and symlinks
that resolve outside the package.
