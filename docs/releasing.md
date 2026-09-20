# Releasing oku

This page is for the maintainer. Users install a release with the scripts in
[Getting started](getting-started.md#install-oku).

## How a release happens

1. Pull requests merge into `main` with conventional commit subjects, such as
   `feat(store): ...` and `fix(infer): ...`.
2. [release-please](https://github.com/googleapis/release-please) keeps one pull
   request open that bumps the version and writes `CHANGELOG.md`. While oku is
   below 1.0, a `feat` bumps the minor version and a `fix` the patch version.
3. Merging that pull request makes the tag `v<version>` and the GitHub release.
4. The same workflow run then calls `publish-artifacts.yml`. It builds a static
   binary for linux, darwin and windows on amd64 and arm64, writes
   `checksums.txt`, signs every file with minisign, and uploads them to the
   release.

`.release-please-manifest.json` holds the last released version and started at
`0.0.0`. release-please finds the git tag `v0.0.0` on the first commit and takes
`0.0.0` as the last release, so the first `feat` makes `v0.1.0`. Without that
tag it reads the repo as never released and proposes `1.0.0`.
`bootstrap-sha` in `release-please-config.json` names the same commit, which is
where the first changelog starts.

## The nightly build

`nightly.yml` runs on every push to `main`. It calls `publish-artifacts.yml`
with that commit and the version `nightly-<timestamp>-<commit>`, and then moves
the tag `nightly` and the prerelease with that name to the commit. The files
and their signatures are the same set as for a release, and they replace the
ones from the commit before.

Use it to test a merged change without a release:

```sh
oku self update --nightly   # or OKU_VERSION=nightly in front of the install script
oku self update             # back to the newest release
```

A prerelease is not the `latest` release, so the install scripts and
`oku self update` without the flag never take it. release-please ignores the
tag, because it is no version. The files upload before the tag moves, so for a
few seconds the release holds the new files under the old commit. In those
seconds `oku self update --nightly` prints the old commit for the new build.

## Secrets

| Secret | Needed | For |
|---|---|---|
| `MINISIGN_SECRET_KEY` | yes | Signing the release files. Without it `publish-artifacts.yml` fails, because `oku self update` refuses a file that the release key did not sign. |
| `MY_RELEASE_PLEASE_TOKEN` | no | A personal access token with `contents` and `pull requests` write access. With it, CI runs on the release pull request. GitHub starts no workflow for a pull request that the default job token opened. |

Create the signing key once, without a password so that CI can use it, and store
the secret half straight from the file:

```sh
minisign -G -W -p oku-release.pub -s oku-release.key
gh secret set MINISIGN_SECRET_KEY --repo y3owk1n/oku < oku-release.key
```

The public half is `releaseKey` in `internal/cli/selfupdate.go` and
`release_key` in `install.sh`. Keep the secret half outside the repo. A new key
means a new value in both places, and every oku that is already installed keeps
trusting the old key until it is reinstalled.

## Rotating the keys

### The release-please token

`MY_RELEASE_PLEASE_TOKEN` is a personal access token, and tokens expire. When
it has, release-please fails with an authentication error, or CI stops running
on the release pull request. Make a new token with `contents` and
`pull requests` write access on this repo and store it:

```sh
gh secret set MY_RELEASE_PLEASE_TOKEN --repo y3owk1n/oku
```

Nothing else changes, and no user notices.

### The signing key, as planned maintenance

An installed oku trusts exactly one key, the one that was built into it. So the
change needs one release that the old key signs and whose binary trusts the new
key:

1. Make the new key, and keep the old secret in place for now:

   ```sh
   minisign -G -W -p oku-release-new.pub -s oku-release-new.key
   ```

2. Put the new public key into `releaseKey` in `internal/cli/selfupdate.go` and
   into `release_key` in `install.sh`, and merge that.
3. Merge the release pull request. This release, call it N, is still signed by
   the old key, so every installed oku accepts it. Its binary trusts the new
   key.
4. Only after N is published, switch the secret:

   ```sh
   gh secret set MINISIGN_SECRET_KEY --repo y3owk1n/oku < oku-release-new.key
   ```

5. Every release after N is signed by the new key.

Keep release N as the newest release for a while. `oku self update` always goes
to the newest release, so an oku older than N cannot update once a later release
exists. It fails with `is not signed by <old key>` and a line that says to run
the install script again, which puts the newest binary in place. Say so in the
notes of the first release after N.

`install.sh` on `main` holds the new key from step 2 on. Between step 2 and
step 4 it therefore rejects the signature of release N for anyone who has
`minisign` installed. Do steps 2 to 4 right after each other to keep that time
short.

### The signing key, after a leak

When the secret key may have leaked, a signature by the old key proves nothing,
so skip the handover release:

1. Make a new key and switch the secret right away, as in steps 1 and 4 above.
2. Put the new public key into `selfupdate.go` and `install.sh`, merge, and
   release.
3. Tell users to run the install script again. `oku self update` refuses that
   release on every installed oku, which is the intended result, because those
   binaries still trust the leaked key.
4. Delete the release files that were published after the leak, if any, and say
   in the release notes which versions to distrust.

Until a user reinstalls, their oku accepts any release file that the leaked key
signed. `self update` only downloads from this repo's GitHub releases over
HTTPS, so an attacker also needs write access to the releases to use the key.

### Cache keys and manifest keys

These belong to the people who publish a cache or a manifest, not to oku's
releases. A cache user swaps a key with `oku key revoke` and `oku key trust`,
see [Build caches](caches.md). A manifest's `signing_key` change needs
`--accept-key` from every user, see [Trust](trust.md#signing-keys).
