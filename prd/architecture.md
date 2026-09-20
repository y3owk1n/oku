# Architecture

## Flow

```
ref -> fetch or infer manifest -> select version -> resolve dep closure
    -> per package: cache hit, else artifact, else build
    -> realize in store -> new profile generation -> expose apps, fonts, services
```

Strategy per package: the first artifact whose selector matches the host, else
`[build]`. `--from-source` forces the build. A trusted cache is consulted
before building.

## Paths

XDG on unix, `%APPDATA%` and `%LOCALAPPDATA%` on Windows. `<root>` is
`<data>/oku` or the shared root from `oku setup --system`.

```
<config>/oku/oku.toml, oku.lock      global list
<config>/oku/config.toml             sources, caches, trusted keys, store root
<root>/store/<name>-<version>-<hash>/
<data>/oku/profiles/global/gen-<n>/  current -> gen-<n>
<data>/oku/profiles/<project-hash>/  same shape, keyed by oku.toml path
<data>/oku/trust/allow.toml, approvals.toml
<data>/oku/exposed.toml              ledger of every file written elsewhere (D17)
<cache>/oku/downloads/, git/
```

A store path holds `bin/`, `lib/`, `include/`, `share/`, `apps/`, `fonts/`
and `oku-meta.toml` (closure, env, services). A profile generation mirrors
`bin/` and `share/` with links.

## Ref resolution

- `github:owner/repo` reads `oku.pkg.toml` at the repo root. When absent, the
  manifest is inferred from releases (D13).
- `github:owner/repo#name` reads `<name>.toml` or `packages/<name>.toml`.
- `alias/name` expands the alias from `config.toml`, then as above.
- A ref given to `sync` or `include` reads `oku.toml` and the `oku.lock`
  beside it.

## Manifest schema

```toml
[package]
name = "ripgrep"            # required
description = ""
homepage = ""
license = ""
relocatable = true
signing_key = ""            # minisign public key, optional

[version]
value = "14.1.0"            # static, or:
from = "github-releases"    # github-releases | git-tags
repo = "BurntSushi/ripgrep"
strip_prefix = "v"

[[artifact]]
match = { os = "linux", arch = "amd64", libc = "musl" }
url = "https://.../ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256 = ""                 # or sha256_url, or neither (pinned on first use)
signature_url = ""
strip = 1
bin = ["rg"]
lib = []
include = []
man = ["doc/rg.1"]
completions = { fish = "complete/rg.fish" }
share = []
app = []                    # "Foo.app", or a [[app]] table on linux and windows
font = []

[build]
needs = ["cc"]
deps = [{ ref = "github:someone/recipes#pcre2", version = ">=10.40" }]
source = { git = "https://github.com/BurntSushi/ripgrep", tag = "{{version}}" }
# or source = { url = "...", sha256 = "..." }

[[build.step]]
vendor = "cargo"            # cargo | go | npm | pip, output hash pinned in lock

[[build.step]]
run = "cargo build --release --offline"
env = { PCRE2_SYS_STATIC = "1" }
when = { os = "linux" }     # optional on any step
shell = "pwsh"              # required for run steps reachable on windows
network = false             # true marks the package impure

[[build.step]]
install = { bin = ["target/release/rg"], man = ["doc/rg.1"] }

[runtime]
deps = []

[env]
RIPGREP_CONFIG_PATH = "{{prefix}}/share/rg/config"

[[app]]                     # linux and windows launcher entries
name = "Foo"
exec = "bin/foo"
icon = "share/icons/foo.png"

[[service]]
name = "food"
command = "bin/food"
args = ["--port", "8080"]
env = {}
restart = "on-failure"      # never | on-failure | always
```

Step types: `run`, `install` (bin, lib, include, man, completions, share, app,
font), `patch` (file, strip), `fetch` (url, sha256, to), `extract` (file, to,
strip), `copy` (from, to), `vendor`. Exactly one type key per step.

Template variables: `{{version}}`, `{{os}}`, `{{arch}}`, `{{libc}}`,
`{{prefix}}`, `{{src}}`, `{{jobs}}`, `{{dep.<name>.prefix}}`. Values for `os`
and `arch` follow GOOS and GOARCH.

## oku.toml and oku.lock

```toml
# oku.toml
include = ["github:me/machines#base"]

[packages]
ripgrep = "github:BurntSushi/ripgrep"
fd = { ref = "./recipes/fd.toml", version = "10.2.0" }
rectangle = { ref = "mine/rectangle", when = { os = "darwin" } }
postgres = { ref = "mine/postgres", service = true }
```

```toml
# oku.lock
[[include]]
ref = "github:me/machines#base"
commit = ""
sha256 = ""

[[package]]
name = "ripgrep"
ref = "github:BurntSushi/ripgrep"
commit = "<git sha>"
manifest_sha256 = ""
inferred = false
signing_key = ""
version = "14.1.0"

[package.platform."linux-amd64-musl"]
strategy = "artifact"       # artifact | build
url = ""
sha256 = ""
vendor_sha256 = ""
deps = ["pcre2-10.44-<hash>"]
```

Dep packages appear as their own `[[package]]` entries. The lock adds a
platform entry the first time that platform resolves, so one lock serves a
mixed-OS team.

## Exposure of apps, fonts, services

| Kind | macOS | Linux | Windows |
|---|---|---|---|
| app | copy bundle to `~/Applications` | `.desktop` and icons in XDG data dir | Start Menu shortcut |
| font | `~/Library/Fonts` | `<data>/fonts` | per-user Fonts dir |
| service, user | launchd agent | systemd user unit | scheduled task at logon |
| service, system | launchd daemon | systemd system unit | Windows service |

Every exposed file is recorded in the generation, so rollback and remove
reverse it exactly.

## Packages

```
cmd/oku/            main
internal/cli/       cobra commands, thin
internal/ref/       parse and fetch refs
internal/manifest/  TOML types, validation, templating
internal/infer/     manifest inference from release assets
internal/platform/  os, arch, libc detection, selector matching
internal/resolve/   version discovery, closure, lock read and write
internal/store/     download, verify, extract, realize, gc
internal/build/     step executor, link environment, vendor steps
internal/sandbox/   linux userns, macos sandbox-exec, fallback
internal/profile/   generations, links, windows shims
internal/expose/    apps, fonts, services per OS
internal/trust/     allow list, approvals, keys, signatures
internal/cache/     substitute and push
internal/shellhook/ hook and env output per shell
```

## CLI

```
oku add <ref>[@version] [--from-source] [--global]
oku remove <name> [--global]
oku sync [ref | -f file]
oku update [name]
oku list | info <ref> | why <name> | search <term>
oku generations | rollback [n] | gc
oku source add|remove|list
oku hook <bash|zsh|fish|pwsh> | env | allow [path] | deny [path]
oku shell <ref>...
oku service list|start|stop|restart|status|logs <name>
oku cache add|remove|list|push
oku key trust|revoke|list|generate
oku manifest init [--from <repo>] | lint | test | bump
oku setup --system | doctor
oku self update | self uninstall [--keep-list] [--yes]
```

`add` and `remove` act on the project list when one is found, else the global
list. `--global` forces the global list. Every command that prints data
accepts `--json`.
