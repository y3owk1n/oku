# Build caches

A cache holds packages that someone already built from source, so your machine
downloads the result and runs no build command. oku has no cache of its own. A cache is a directory that you, your team or a package's developer
serves.

oku uses a cache entry only when a key you trust signed it.

## Use a cache

```
$ oku cache add https://example.com/oku-cache
added cache https://example.com/oku-cache
$ oku key trust RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
trusted RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
```

The location is an http(s) URL or a directory, such as a shared drive. The key is
the public key of whoever fills the cache. Get it from them directly, because
oku installs whatever that key signed without asking.

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

When a cache has an entry that oku cannot trust, oku says why, ignores it and
builds the package:

```
ignored https://example.com/oku-cache/jq-1.7.1-0c1d5a3f9e2b7a41.tar.zst, no trusted key signed it
```

That covers a missing signature, a signature by a key you do not trust, and a
file that changed after it was signed. A cache that does not have the package
prints nothing.

| Command | Effect |
|---|---|
| `oku cache add <directory-or-url>` | Looks in this cache before building. |
| `oku cache remove <directory-or-url>` | Stops looking in it. |
| `oku cache list` | Lists your caches. oku tries them in this order. |
| `oku key trust <public-key>` | Accepts entries that this key signed. |
| `oku key revoke <public-key>` | Stops accepting them. Packages already installed stay. |
| `oku key list` | Lists the trusted keys, and your own public key when you have one. |

Caches and trusted keys are the `caches` and `trusted_keys` arrays of
`<config>/oku/config.toml`.

## Which entry fits your machine

An entry is named after the package's store path, `<name>-<version>-<hash>`. The
hash covers the manifest, the version, the platform and the deps, so an entry
only matches a machine that would build the same thing.

A build can write its own store path into the files it installs. Such a package
only works at that path, so its hash also covers the store root, and oku never
uses it under another root. Machines share those packages only when they have
the same root, which is what [`oku setup --system`](commands.md#oku-setup) is
for. A manifest whose output contains no store path says so with
`relocatable = true` in `[package]`, and then the entry fits every store root.

## Fill a cache

```
$ oku key generate
wrote the secret key to ~/.config/oku/signing.key, it has no password
people who use your cache run:
  oku key trust RWRICenwB0kA6NZY/uo0EqhV0q1L4PIRu5svVTC7aZKX8n3URx0QbjmF
$ oku cache push ./oku-cache jq
pushed jq-1.7.1-0c1d5a3f9e2b7a41
pushed oniguruma-6.9.9-5e8b1f60a2c4d913
```

`oku cache push <directory> [name...]` writes one `<store path>.tar.zst` and one
`<store path>.tar.zst.minisig` for each named package of your global profile and
for each of its deps. Without names it takes every package. It skips packages
that are plain downloads, because every machine can fetch those itself.

Copy the directory to any static web host, or share it as a directory. oku never
uploads anything.

push refuses a package whose build had network access (`network = true` on a
`run` step). Such a build can differ from run to run, so oku does not give its
result to another machine in place of a build.

The signatures are [minisign](https://jedisct1.github.io/minisign/) signatures,
so `minisign -V` verifies them too. The secret key has no password, so that a
push can run in CI. Keep `signing.key` private. `oku self uninstall` deletes it
with the rest of the config directory, also with `--keep-list`.
