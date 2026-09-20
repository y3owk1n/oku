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

`.release-please-manifest.json` holds the last released version. It starts at
`0.0.0`, so the first release is `v0.1.0`.

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

The public half goes into `releaseKey` in `internal/cli/selfupdate.go` and into
`release_key` in `install.sh`. Keep `oku-release.key` outside the repo.
