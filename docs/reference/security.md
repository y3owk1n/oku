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

## Inferred manifests

For a repo with no manifest, oku writes one from the release and says so.
`--verbose` prints it, so you can read what it installed from. The lock stores
that text, other machines install from the stored text, and only `oku update`
infers again.

A manifest inferred from a URL of the download has no checksum to read, so oku
always trusts that download on first use.

## Tokens

oku sends a forge token to the host it is for and to no other, over https, and
not across a redirect to another host. When `GITHUB_TOKEN` or
`GH_ENTERPRISE_TOKEN` is not set and `gh` is on `PATH`, oku runs
`gh auth token --hostname <host>` and sends that login to the same host only.
The variables and their hosts are in [tokens per host](refs.md#tokens-per-host).

## Approve build commands

A manifest with a `[build]` can run commands on your machine. Before the first
build of such a manifest, oku shows every `run` step that applies to your
machine and asks:

```
tree 2.3.2 builds from source and runs these commands on your machine:

  step 1
    make -j{{jobs}}

run them? [y/N]
```

- oku records your answer for that exact manifest, by its sha256, in
  `<data>/oku/trust/approvals.toml`. The same manifest never asks twice, and a
  manifest that changed asks again.
- When stdin is not a terminal, oku refuses, and `--yes` approves. Use `--yes`
  in scripts only for manifests you have read.
- A dep that builds from source asks for its own approval, before the package
  that needs it.
- An approval applies to one machine. `oku sync` on a new machine asks again.
- The prompt shows `vendor` steps too. A vendor step runs the language's package tool with the
  network on, and `oku.lock` pins a digest of what it downloads.
- A manifest with only `install`, `copy`, `fetch` and `extract` steps runs no
  commands and needs no approval, unless an `install` step generates
  completions.
- An artifact whose completions a command generates runs the download, so it
  asks with `run it? [y/N]`, and `oku.lock` records `commands = true`.
- A step marked `(wants network)` in the prompt gets the network.
- A package from a trusted cache needs no approval, because oku runs none of
  its manifest's commands.

On a terminal the prompt disappears once you answer, and one line stays:
`✓ approved tree 2.3.2` or `✗ rejected tree 2.3.2`. Packages that install in
parallel wait below it, and a second package that needs approval asks after the
first.

## The build sandbox

On macOS and Linux, build commands run in a sandbox with no network, no access
to your home directory, and a scrubbed environment. A step with
`network = true` gets the network and still cannot read your home directory.
The full list of what a build can reach is in the
[manifest reference](manifest.md).

The sandbox is not available everywhere. On Windows, and on a Linux host that
forbids unprivileged user namespaces, oku builds without it and says so after
the build:

```
tree was built without the sandbox, because this host does not let an unprivileged user set up namespaces (...)
its build commands could use the network and read your files
```

`oku doctor` reports the same as a `note`.

- Ubuntu 24.04 and later are such hosts by default. Their AppArmor policy lets
  a program create a user namespace and then denies it every mount inside it.
  To allow the sandbox, run
  `sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0`, and put the
  same setting in a file under `/etc/sysctl.d/` to keep it after a reboot.
- On macOS the tools in `/usr/bin` ask `xcrun` where the real tool is, and
  `xcrun` caches the answer in a place the sandbox does not let it write. oku
  points that cache at the build's own temporary directory, so `ar`,
  `install_name_tool` and the others run without an "Operation not permitted"
  warning.
- On Windows the build still gets a scrubbed environment, see
  [Windows](../guides/windows.md).

The sandbox limits what an approved command can reach. It is not a reason to
approve commands you have not read.

## Signing keys of a manifest

A manifest can name its developer's minisign public key as `signing_key`. oku
then downloads `<artifact url>.minisig` and installs the artifact only when
that key signed it.

`oku.lock` pins the key at the first install. When the manifest later shows
another key, or drops it, `oku sync` and `oku update` stop:

```
oku: foo: oku.lock pinned the signing key RWTr8ko..., and the manifest now has the signing key RWSwtYz...
if the developer announced this change, run the command again with --accept-key
```

A new key is what an attacker who took over the repo would publish. Check with
the developer before you run `oku update foo --accept-key`. oku trusts the key
it sees at the first install, and does not know whether that key belongs to the
developer.

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

## Projects and the shell hook

The shell hook changes `PATH` when you enter a directory, so it acts only for a
project you allowed with `oku allow`, and only while its `oku.toml` is
unchanged. oku records the allow in `<data>/oku/trust/allow.toml` with the
list's sha256.

- The hook never installs, never uses the network, and never runs anything
  from a manifest.
- A package's `[env]` cannot set `PATH`, `LD_PRELOAD` or similar variables.
  A list's `[env]` can only put entries in front of `PATH`, and cannot set the
  others.
- A list's `[env]` sets values and reads variables as `${NAME}`. It runs no
  command, and neither does a `.env` file it loads.
- oku decrypts a `secret = true` `.env` file in memory and writes no
  decrypted copy. A file of `scope = "exec"` loads for `oku exec` only, never
  for the shell.
- The allow covers each `.env` file of the project that git tracks, so a pull
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
failed check leaves oku as it was. The nightly build has the same check.

The release key is:

```
RWSjFGqIxI8IPGwKE/uRgugZ51qCEMe1CDbFRVTMUAuin42JiOxg2HNW
```

The install scripts check the sha256 of the binary always, and its minisign
signature against the same key when `minisign` is installed.

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

## What oku does not protect against

- A manifest that was malicious the first time you added it. Read manifests
  from sources you do not know.
- A `sha256_url` on the same host as the download. It catches corruption and
  tampering after you locked, not a compromised host on first use.
- A `signing_key` that was the attacker's at your first install. oku pins the
  first key it sees.
- The sources of a `[build]`. `signing_key` covers artifacts only, and `fetch`
  steps rely on their `sha256`.
- A build command you approved, on a host where the sandbox is not available.
- A build that reaches a service outside the sandbox, such as launchd over XPC
  on macOS or the Docker socket on Linux, and asks it to start a program. The
  sandbox blocks the ways that the [manifest reference](manifest.md) lists, not
  every way.
- A cache key you trusted that signs something malicious.
