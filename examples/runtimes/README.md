# Runtimes

Manifests for the toolchains that `npm:`, `pypi:`, `go:` and `cargo:` packages
need, and a list that uses them. Copy the files you need next to your
`oku.toml` and name them in `[runtimes]`:

```toml
[runtimes]
node = { ref = "./node.toml", version = "^22" }
python = "./python.toml"
go = "./go.toml"
rust = "./rust.toml"
```

| File | Used by | Platforms |
|---|---|---|
| [`node.toml`](node.toml) | `npm:` packages run through it, and their builds run its npm | macOS, Linux with glibc, Windows |
| [`python.toml`](python.toml) | `pypi:` packages run through it, and uv installs them for it | macOS, Linux, Windows |
| [`go.toml`](go.toml) | `go:` packages build with it | macOS, Linux, Windows |
| [`rust.toml`](rust.toml) | `cargo:` packages build with its cargo and rustc | macOS, Linux |

None of these toolchains goes on your `PATH`. A build finds its toolchain in
the store, and a program from an `npm:` or `pypi:` package finds its node or
python first on its own `PATH`. Add the toolchain under `[packages]` too only
if you want to run it yourself.

node, go and rust follow upstream releases, so `oku update` moves them.

Only node has a version constraint. node ships several release lines at once,
and the newest release is often not the line your tools target. `^22` keeps
node on one line and still takes that line's patches. A newer go or rust still
builds older code, and many crates need a recent rustc, so those two take the
newest release.

python pins its version, because python-build-standalone names each download
after the python version and its release date. Change both in `python.toml` to
move to a newer python.

On Windows, `rust.toml` does not build, because it runs the installer's
`install.sh`. A `cargo:` package there uses the cargo on `PATH`, such as the
one rustup installs.

[`oku.toml`](oku.toml) is a list that installs one package of each kind with
these runtimes. See [Refs](../../docs/refs.md#npm-packages) for how each kind
of package uses its runtime.
