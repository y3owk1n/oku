# Journeys

How oku feels from each side. Output shown is illustrative. The promises
behind it are in `prd/behaviours.md`, cited as B-numbers.

```
developer                          user
---------                          ----
oku.pkg.toml in repo   <-- ref --  oku.toml (what I want)
release assets         <-- url --  oku.lock (exactly what I got)
optional cache + key   <-- key --  config.toml (who I trust)
```

## Developer

### Level 0: publish nothing

A repo that ships release assets named like `tool_1.2.0_linux_amd64.tar.gz`
is already installable (B25, B26).

```
$ oku add github:you/tool
github:you/tool has no manifest, so oku inferred one from its newest release, --verbose prints it
added tool 1.2.0
```

oku says it inferred a manifest and installs without asking, so the command
works in scripts. The lock stores the manifest text (B25).

### Level 1: own the manifest

```
$ oku manifest init --from you/tool
wrote oku.pkg.toml
$ oku manifest lint
oku.pkg.toml: ok
```

Bin names, man pages, completions and a signing key are now explicit (B27,
B28). Version discovery plus templated URLs mean a new release tag needs no
manifest change. The file changes only when the packaging changes.

### Level 2: build from source

```toml
[build]
needs = ["cc"]
deps = [
  { ref = "github:someone/recipes#rust" },
  { ref = "github:someone/recipes#openssl", version = ">=3" },
]
source = { git = "https://github.com/you/tool", tag = "v{{version}}" }

[[build.step]]
vendor = "cargo"

[[build.step]]
run = "cargo build --release --offline"
shell = "sh"

[[build.step]]
install = { bin = ["target/release/tool"] }
```

Absent from that manifest: `PKG_CONFIG_PATH`, `-L` and `-I` flags, rpath
handling, a macOS special case, a crate download step. oku supplies the link
environment from `deps` (B37), the vendor step fetches with network and pins
its hash (B52), and the build runs offline in the sandbox (B50). A `run` step
that can reach Windows names its shell (D4). Here `shell = "sh"` covers every
OS. A step that differs on Windows gets a `when` guard and `shell = "pwsh"`.

```
$ oku manifest test
[1/3] vendor cargo        ok  (sha256 pinned)
[2/3] run cargo build     ok  (sandboxed, no network)
[3/3] install             ok  bin/tool
built tool-1.2.0-9f3a in throwaway store
```

### Level 3: serve prebuilt results

```
$ oku cache push ./cache tool
```

CI packs signed results into a directory and copies it to any static web
host. The developer publishes the public key. Users who trust it download
instead of compiling (B85 to B87).

### Curators

A repo of `ripgrep.toml`, `fd.toml`, `openssl.toml` is a collection. It plays
the role of a homebrew tap or the AUR, except anyone can run one and none is
the default. Toolchain and C library collections are what `deps` point at.

## User

### First contact

```
# one static binary, no root
$ curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
$ oku add github:BurntSushi/ripgrep
$ rg --version
```

### Living with it

```
$ oku source add core github:someone/recipes
$ oku search postgres
core/postgres   17.2   object-relational database
$ oku add core/postgres
```

Every `add` edits the global `oku.toml` and `oku.lock`. Those two files are
the machine description.

```toml
include = ["github:kyle/machines#base"]

[packages]
ripgrep   = "github:BurntSushi/ripgrep"
neovim    = "core/neovim"
postgres  = { ref = "core/postgres", service = true }
rectangle = { ref = "core/rectangle", when = { os = "darwin" } }
jetbrains-mono = "core/jetbrains-mono"
```

One list holds a CLI tool, an editor built from source, a running service, a
macOS-only GUI app, and a font.

### New machine

```
$ curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
$ oku sync github:kyle/machines
resolving from lock: 23 packages, 41 store paths
  38 artifacts, 3 from cache (key: someone-recipes), 0 to build
exposing: 2 apps, 1 font, 1 service (postgres: started)
generation 1 active
```

Same versions, same bytes (B18). On a Linux server the same list skips
`rectangle` (B17). When the list names `linux-amd64-glibc` in `[lock]
platforms`, the Mac already pinned every package for it, and
`oku sync --locked` on the server downloads only what the lock pins (B179,
B181).

### Something breaks

```
$ oku update
neovim 0.11.2 -> 0.12.0
generation 14 active
$ oku rollback
generation 13 active (neovim 0.11.2)
```

Instant, because 0.11.2 never left the store (B22).

### Trust moments

oku stops in four cases. Everything else is silent because the lock already
recorded the decision.

1. A manifest with `run` steps, the first time. It shows the commands and
   whether any wants network (B41, B53).
2. A manifest that changed under a locked ref. `sync` refuses until `update`
   (B13).
3. A signing key that changed. oku refuses until `--accept-key` (B89).
4. Anything needing elevation, which only happens with `--system` (B75).

### Leaving

```
$ oku self uninstall --keep-list
this removes:
  store        41 paths, 2.3 GB   ~/.local/share/oku
  profiles     global + 3 projects
  services     postgres (running, will be stopped)
  apps         Rectangle.app, Foo.app
  fonts        JetBrains Mono
  cache        ~/.cache/oku
  binary       ~/.local/bin/oku
keeps:
  ~/.config/oku/oku.toml, oku.lock
  project oku.toml files (never touched)
continue? [y/N] y
done. remove this line from ~/.config/fish/config.fish:
  oku hook fish | source
```

One command, one question, nothing left behind (B94 to B101). It works
because oku never scatters files: everything outside its own directories is
in a ledger (D17), and it never edits rc files. With `--keep-list`, coming
back later is `oku sync`.

### Per project

```
$ cd ~/work/api
oku: oku.toml found, run `oku allow` to activate
$ oku allow && oku sync
$ which node
~/.local/share/oku/profiles/3fa9/current/bin/node     # project list
$ cd ~ && which node
~/.local/share/oku/profiles/global/current/bin/node   # global list
```

A teammate on Windows clones the repo, runs the same two commands, and gets
the same node version (B60 to B67, B82).
