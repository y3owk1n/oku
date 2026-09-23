# Share builds through a cache

You end up installing packages that someone already built from source, so your
machine downloads the result and runs no build command. Or you fill a cache so
your team or your other machines skip the build.

A [build cache](../how-oku-works.md#build-cache) is a directory of signed
[builds](../how-oku-works.md#build), served over http(s) or shared as a
directory. oku has no cache of its own. oku uses an entry only when a key you
trust signed it.

## Use a cache

Add the cache and trust the key of whoever fills it:

```
$ oku cache add https://example.com/oku-cache
added cache https://example.com/oku-cache
$ oku key trust RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
trusted RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
```

The location is an http(s) URL or a directory, such as a shared drive. Get the
key directly from the person who fills the cache, because oku installs whatever
that key signed without asking.

From then on `oku add`, `oku sync` and `oku update` look in your caches before
they build, for the package and for each of its deps:

```
$ oku add github:you/recipes#jq
oniguruma came from a cache, nothing was built
jq came from a cache, nothing was built
added jq 1.7.1
```

A package from a cache needs no build approval, because oku runs none of its
manifest's commands. `oku.lock` records it as a build, the same as on the
machine that built it.

When no cache has the package, oku prints nothing and builds it.

## Understand an ignored entry

When a cache has an entry that oku cannot trust, oku says why, ignores it and
builds the package:

```
ignored https://example.com/oku-cache/jq-1.7.1-0c1d5a3f9e2b7a41.tar.zst, no trusted key signed it
```

That covers a missing signature, a signature by a key you do not trust, and a
file that changed after it was signed.

An entry is named after the package's [store](../how-oku-works.md#store) path,
`<name>-<version>-<hash>`. The hash covers the manifest, the version, the
platform and the deps, so an entry matches only a machine that would build the
same thing.

A build can write its own store path into the files it installs. Such a package
works only at that path, so its hash also covers the store root, and oku never
uses it under another root. Machines share those packages only when they have
the same root, which is what
[`oku setup --system`](system-wide.md#share-built-packages-through-a-common-store-root)
gives them. A manifest whose output holds no store path says so with
`relocatable = true` in `[package]`, and then its entry fits every store root.

## Manage caches and keys

| Command | Effect |
|---|---|
| `oku cache add <directory-or-url>` | Looks in this cache before building. |
| `oku cache remove <directory-or-url>` | Stops looking in it. |
| `oku cache list` | Lists your caches. oku tries them in this order. |
| `oku key trust <public-key>` | Accepts entries that this key signed. |
| `oku key revoke <public-key>` | Stops accepting them. Packages already installed stay. |
| `oku key list` | Lists the trusted keys, and your own public key when you have one. |

Your caches and trusted keys are the `caches` and `trusted_keys` arrays of
`~/.config/oku/config.toml`, see [the oku.toml reference](../reference/oku-toml.md).

## Fill a cache

Create a signing key once, then push the packages you built:

```
$ oku key generate
wrote the secret key to ~/.config/oku/signing.key, it has no password
people who use your cache run:
  oku key trust RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
$ oku cache push ./oku-cache jq
pushed jq-1.7.1-0c1d5a3f9e2b7a41
pushed oniguruma-6.9.9-5e8b1f60a2c4d913
```

`oku cache push <directory> [name...]` writes, for each named package of your
global profile and each of its deps:

- `<store path>.tar.zst`, the built package
- `<store path>.tar.zst.minisig`, its signature

Without names it takes every package of the global profile. It skips plain
downloads, because every machine can fetch those itself. When nothing is left,
it prints `nothing to push, none of these packages was built from source`.

`push` refuses a package whose build had network access, which is a `run` step
with `network = true`. Such a build can differ from run to run, so oku does not
give its result to another machine in place of a build.

`oku key generate` refuses to overwrite an existing `signing.key`. Delete the
file first to replace your key, and give the new public key to everyone who
uses your cache.

## Host a cache

oku never uploads anything. Copy the directory that `oku cache push` wrote to
any static web host, and users add its URL. Or share the directory, such as on
a network drive, and users add its path.

The signatures are [minisign](https://jedisct1.github.io/minisign/)
signatures, so `minisign -V` verifies them too.

## Keep the signing key safe

`~/.config/oku/signing.key` has no password, so a push can run in CI. Anyone
who has the file can sign entries that your users install without asking. Keep
it private.

`oku self uninstall` deletes `signing.key` with the rest of the config
directory, also with `--keep-list`.
