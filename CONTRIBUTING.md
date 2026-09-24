# Contributing

How to build oku, test a change, and cut a release. Users install a release
with the scripts in [Getting started](docs/getting-started.md).

## Build from source

oku needs Go 1.26.4 or newer, as `go.mod` says. The repo's own `oku.toml`
pins the tools of the project: go, golangci-lint, gofumpt, golines, just and
the editor tools. With oku installed, get them from the lock:

```sh
git clone https://github.com/y3owk1n/oku
cd oku
oku sync
```

Then build:

```sh
just build
```

This writes `bin/oku`, a static binary (`CGO_ENABLED=0`) whose version is the
newest `v*` tag from `git describe`, or `dev`. Only release tags count, because
the moving `nightly` tag would name every local build `nightly`. A plain
`go build ./cmd/oku` works too, and reports its version as `dev`.

A binary you built has the release key, so `oku self update` replaces it with
the newest release.

## The just gate

Run these before you open a pull request. CI runs the same checks.

| Recipe | Runs |
|---|---|
| `just build` | `go build` of `./cmd/oku` into `bin/oku`. |
| `just test` | `go test ./...`. |
| `just lint` | `golangci-lint run`, with the settings in `.golangci.yml`. |
| `just fmt` | `gofumpt -w .` and `golines -w .`. |

CI in `.github/workflows/ci.yml` runs `go vet ./...` and `go test ./...` on
ubuntu, macOS and Windows runners, lints once per `GOOS` on Linux, and runs the
live scripts below. It installs its tools with the `oku` action from
`oku.lock`, with the nightly oku, so a broken commit cannot break the tools
that test it.

## Tests and behaviours

`prd/behaviours.md` lists what oku promises, one testable line each, such as
`B195 [1] oku remove takes several names and drops them in one generation`.
The number in brackets is the build step in `prd/product.md`.

- A new promise gets a new line with the next free number. It lands in the
  same pull request as the code.
- A test that checks a behaviour is named after it, such as
  `TestB227ATableFitsTheTerminalWidth`.
- Test what the project promises: a behaviour in `prd/behaviours.md`, or a
  public boundary such as a command, its output, or `oku.toml` and `oku.lock`.
  Go through the CLI, and fake only the network and the OS at their edges.
- Most CLI tests in `internal/cli` run the real command tree against a
  throwaway config, data and cache directory with local fixtures. Those
  fixtures are shell scripts, so the suite skips them on Windows, where the
  live script covers the same ground.

`prd/decisions.md` records why oku works the way it does. Add a decision when a
change picks one way over another that a reader could question.

## Live test beds

Unit tests use fixtures. A change that touches the OS also needs a run of the
real binary on the OS it is for, in throwaway directories. Point
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_CACHE_HOME` at a scratch directory,
and remove it afterwards.

### Linux in Docker

Linux-only behaviour, such as the build sandbox with user namespaces, glibc and
musl detection, dconf and systemd units, can run in a container on a Mac.
Cross-compile and mount the binary, so no Go image is needed:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/oku-linux/oku ./cmd/oku
docker run --rm -v /tmp/oku-linux:/oku ubuntu:24.04 /oku/oku --version
```

- The default seccomp profile of Docker may block user namespaces. Test the
  sandbox with `--privileged`, and test the fallback without it.
- Test system scope as root inside the container, never with `sudo` on your
  own machine.
- Use `--rm`, mount only scratch directories, and remove any image you pulled
  for the test.

In CI, `.github/scripts/live-linux.sh` runs the real oku on the ubuntu runner
against a real dconf database, in its own D-Bus session. `OKU` names a built
binary, and without it the script builds one.

### Windows in GitHub Actions

There is no Windows machine to test on, so `.github/scripts/live-windows.ps1`
runs the real `oku.exe` on the `windows-latest` runner, on every pull request.
It builds oku, sets the XDG variables to a directory under `RUNNER_TEMP`, and
installs real releases from GitHub, GitLab and gitea.com. Each check throws on
failure, which fails the job.

To test a Windows change, push it to your branch and read the run:

```sh
gh run view --log
```

The runner is an administrator with no desktop, so the consent prompt of system
scope and a service task of a standard user are not covered.

### The sources oku reads

`.github/workflows/sources.yml` runs `.github/scripts/sources.sh` once a day on
the `macos-latest` runner. The script builds oku from `main` and asks each
outside source that oku reads for one real package: the releases of GitHub,
GitLab and Gitea, the aqua registry, the Homebrew API and the feeds of three
casks, npm, PyPI, crates.io, the Go module proxy, Scoop and winget. A failure
means a source changed its format or its address, or is down. The run then
opens an issue titled "A source that oku reads has changed", or comments on the
open one. The run log shows each failure with oku's error.

To check the sources now, run the workflow by hand, or run the script:

```sh
gh workflow run sources.yml
.github/scripts/sources.sh
```

## Releasing

This section is for the maintainer.

### How a release happens

1. Pull requests merge into `main` with conventional commit subjects, such as
   `feat(store): ...` and `fix(infer): ...`.
2. [release-please](https://github.com/googleapis/release-please) keeps one
   pull request open that bumps the version and writes `CHANGELOG.md`. While
   oku is below 1.0, a `feat` bumps the minor version and a `fix` the patch
   version.
3. Merging that pull request makes the tag `v<version>` and the GitHub
   release.
4. The same workflow run then calls `publish-artifacts.yml`. It builds a static
   binary for linux, darwin and windows on amd64 and arm64, writes
   `checksums.txt`, signs every file with minisign with the trusted comment
   `oku <tag>`, and uploads them to the release.

`.release-please-manifest.json` holds the last released version and started at
`0.0.0`. release-please finds the git tag `v0.0.0` on the first commit and
takes `0.0.0` as the last release, so the first `feat` made `v0.1.0`. Without
that tag it reads the repo as never released and proposes `1.0.0`.
`bootstrap-sha` in `release-please-config.json` names the same commit, which is
where the first changelog starts.

### The nightly build

`nightly.yml` runs on every push to `main`. It calls `publish-artifacts.yml`
with that commit and the version `nightly-<timestamp>-<commit>`, and then moves
the tag `nightly` and the prerelease of that name to the commit. The files and
their signatures are the same set as for a release, and they replace the ones
from the commit before.

Use it to test a merged change without a release:

```sh
oku self update --nightly   # or OKU_VERSION=nightly in front of the install script
oku self update --release   # back to the newest release
```

- A prerelease is not the `latest` release, so the install scripts and
  `oku self update` without a flag never take it. On a nightly build, a bare
  `oku self update` refuses, and `--release` goes back.
- release-please ignores the tag, because it is no version.
- The files upload before the tag moves, so for a few seconds the release holds
  the new files under the old commit. In those seconds
  `oku self update --nightly` prints the old commit for the new build.

### Secrets

| Secret | Needed | For |
|---|---|---|
| `MINISIGN_SECRET_KEY` | yes | Signing the release files. Without it `publish-artifacts.yml` fails, because `oku self update` refuses a file that the release key did not sign. |
| `MY_RELEASE_PLEASE_TOKEN` | no | A personal access token with `contents` and `pull requests` write access. With it, CI runs on the release pull request, because GitHub starts no workflow for a pull request that the default job token opened. When set, it also uploads the release files. |

Create the signing key once, without a password so that CI can use it, and
store the secret half straight from the file:

```sh
minisign -G -W -p oku-release.pub -s oku-release.key
gh secret set MINISIGN_SECRET_KEY --repo y3owk1n/oku < oku-release.key
```

The public half is `releaseKey` in `internal/cli/selfupdate.go` and
`release_key` in `install.sh`. Keep the secret half outside the repo. A new key
means a new value in both places, and every oku that is already installed keeps
trusting the old key until it is reinstalled.

### Rotate the release-please token

`MY_RELEASE_PLEASE_TOKEN` expires. When it has, release-please fails with an
authentication error, or CI stops running on the release pull request. Make a
new token with `contents` and `pull requests` write access on this repo and
store it:

```sh
gh secret set MY_RELEASE_PLEASE_TOKEN --repo y3owk1n/oku
```

Nothing else changes, and no user notices.

### Rotate the signing key

An installed oku trusts exactly one key, the one built into it. What a user
sees during a rotation is in
[Security](docs/reference/security.md#when-the-release-key-changes).

#### As planned maintenance

The change needs one release that the old key signs and whose binary trusts
the new key:

1. Make the new key, and keep the old secret in place for now:

   ```sh
   minisign -G -W -p oku-release-new.pub -s oku-release-new.key
   ```

2. Put the new public key into `releaseKey` in `internal/cli/selfupdate.go`
   and into `release_key` in `install.sh`, and merge that.
3. Merge the release pull request. The old key still signs this release, call
   it N, so every installed oku accepts it. Its binary trusts the new key.
4. Only after N is published, switch the secret:

   ```sh
   gh secret set MINISIGN_SECRET_KEY --repo y3owk1n/oku < oku-release-new.key
   ```

5. The new key signs every release after N.

Keep release N as the newest release for a while. `oku self update` always goes
to the newest release, so an oku older than N cannot update once a later
release exists. It fails with `is not signed by <old key>` and a line that says
to run the install script again, which puts the newest binary in place. Say so
in the notes of the first release after N.

`install.sh` on `main` holds the new key from step 2 on. Between step 2 and
step 4 it therefore rejects the signature of release N for anyone who has
`minisign` installed. Do steps 2 to 4 right after each other.

#### After a leak

When the secret key may have leaked, a signature by the old key proves
nothing, so skip the handover release:

1. Make a new key and switch the secret right away, as in steps 1 and 4 above.
2. Put the new public key into `selfupdate.go` and `install.sh`, merge, and
   release.
3. Tell users to run the install script again. `oku self update` refuses that
   release on every installed oku, which is the intended result, because those
   binaries still trust the leaked key.
4. Delete the release files that were published after the leak, if any, and
   say in the release notes which versions to distrust.

Until a user reinstalls, their oku accepts any release file that the leaked key
signed. `self update` only downloads from this repo's GitHub releases over
HTTPS, so an attacker also needs write access to the releases to use the key.

#### Cache keys and manifest keys

These belong to the people who publish a cache or a manifest, not to oku's
releases. A cache user swaps a key with `oku key revoke` and `oku key trust`,
see [Build caches](docs/guides/build-caches.md). A manifest's `signing_key`
change needs `--accept-key` from every user, see
[Security](docs/reference/security.md#signing-keys-of-a-manifest).
