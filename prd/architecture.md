# Architecture

## Flow

```
ref -> fetch or infer manifest -> select version -> resolve dep closure
    -> per package: cache hit, else artifact, else build
    -> realize in store -> render files into the new generation
    -> check targets and settings                          (plan ends, D59)
    -> switch current -> match the ledger -> write oku.lock  (apply)
```

A failure in the plan leaves the machine unchanged. A failure in the apply
makes oku match the ledger to the old generation again (D59).

Strategy per package: the first artifact whose selector matches the host, else
`[build]`. `--from-source` forces the build. A trusted cache is consulted
before building.

## Paths

XDG on unix, `%APPDATA%` and `%LOCALAPPDATA%` on Windows. `<root>` is
`<data>/oku` or the shared root from `oku setup --system`.

```
<config>/oku/oku.toml, oku.lock      global list
<config>/oku/config.toml             sources, caches, trusted_keys, store_root
<config>/oku/signing.key             secret key of `oku cache push`
<root>/store/<name>-<version>-<hash>/
<data>/oku/profiles/global/gen-<n>/  current -> gen-<n>
<data>/oku/profiles/project-<hash>/  same shape, keyed by the project's path
<data>/oku/trust/allow.toml, approvals.toml
<data>/oku/exposed.toml              ledger of every file written elsewhere (D17)
<data>/oku/pending.toml              present only while oku applies a change (D59)
<data>/oku/busy                      held by the oku process that changes the machine (D73)
<data>/oku/secrets/                  decrypted secrets, readable by the user only (D63)
<cache>/oku/downloads/, git/
```

A store path holds `pkg/` (the whole unpacked download), `bin/`, `lib/`,
`include/`, `share/`, `apps/`, `fonts/` and `oku-meta.toml` (closure, env,
services). For an artifact the output directories hold relative links into
`pkg/`. A built package has no `pkg/` and holds real files (D28). A profile
generation mirrors `bin/` and `share/` with links, and holds `oku-gen.toml`
(time, packages, files, settings), a copy of `oku.lock`, and `files/` with the
content of every `text` and `render` entry (D60).

## Ref resolution

A ref is read as a manifest or as a list. The kind sets the file names.

| Ref | As a manifest | As a list |
|---|---|---|
| `github:owner/repo` | `oku.pkg.toml` | `oku.toml` |
| `github:owner/repo#name` | `name.toml`, else `packages/name.toml` | `name.toml`, else `lists/name.toml` |
| `git+<url>` | `oku.pkg.toml` | `oku.toml` |
| `git+<url>#path` | that path | that path |
| file path, `https://` URL | that file | that file |

- A `github:` repo with no manifest is inferred from releases (D13).
- `alias/name` expands the alias from `config.toml`, then as above.
- The lock beside a list has the list's name with `.lock`.
- A relative file ref inside a list starts at that list's directory. In a list
  from a URL or a repo it names the file beside the list there (D74), and an
  absolute path is an error.
- Includes merge in order, the including list's own packages win, nesting
  stops at 8 levels, and a list included twice is an error.

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
from = "github-releases"    # github-releases | git-tags
repo = "BurntSushi/ripgrep"
strip_prefix = "v"
# tag = "nightly"           # with github-releases, follow one moving tag (D57)
# or a fixed version, not together with from:
# value = "14.1.0"

[[artifact]]
match = { os = "linux", arch = "amd64", libc = "musl" }
url = "https://.../ripgrep-{{version}}-x86_64-unknown-linux-musl.tar.gz"
sha256 = ""                 # or sha256_url, or neither (pinned on first use)
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
network = false             # true marks the package impure

[[build.step]]
run = "cargo build --release --offline"
when = { os = "windows" }
shell = "pwsh"              # a run step that can reach windows must name its shell

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

Template variables: `{{version}}`, `{{tag}}`, `{{os}}`, `{{arch}}`, `{{libc}}`,
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
caddy = { ref = "mine/caddy", service = true, system = true }

[vars]                                # global list only (D61)
email = "me@example.com"
theme = { base00 = "0c1410" }         # {{theme.base00}}

[files]                               # global list only (D60)
"{{home}}/.config/nvim" = { link = "./files/nvim" }
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
"{{home}}/.ssh/allowed_signers" = { text = "{{email}} ...\n", mode = "0600" }
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets.yaml", key = "ssh/id_ed25519" }  # D63

[secrets]                             # global list only, {{secret.token}} (D63)
token = { file = "./secrets.yaml", key = "github/token" }

[defaults."com.apple.dock"]           # macOS. Also [registry], [dconf] (D62)
tilesize = 48
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
manifest = ""               # the inferred manifest text, when inferred
signing_key = ""
version = "14.1.0"
tag = "v14.1.0"             # only when it differs from version
tag_commit = ""             # full commit of a moving tag (D57)

[package.platform."linux-amd64-musl"]
strategy = "artifact"       # artifact | build
url = ""
sha256 = ""
vendor_sha256 = ""          # what the vendor steps downloaded (D34)
impure = false              # true when a run step used network = true

[[package.dep]]             # pinned like a package, and may nest its own deps
name = "pcre2"
ref = "github:someone/recipes#pcre2"
version = "10.44"
```

```toml
# config.toml
[sources]
core = "github:someone/recipes"
```

A source names a collection. `core/ripgrep` expands to
`github:someone/recipes#ripgrep` before it is written anywhere (D26).

Deps nest under the package that needs them (D30). The lock adds a
platform entry the first time that platform resolves, so one lock serves a
mixed-OS team.

## Exposure of apps, fonts, services, files and settings

| Kind | macOS | Linux | Windows |
|---|---|---|---|
| app | copy bundle to `~/Applications` | `.desktop` and icons in XDG data dir | Start Menu shortcut |
| font | `~/Library/Fonts` | `<data>/fonts` | per-user Fonts dir and its registry value |
| service, user | launchd agent | systemd user unit | scheduled task at logon |
| service, system | launchd daemon | systemd system unit | scheduled task as SYSTEM at boot |

With `system = true` on the list entry the targets are `/Applications`,
`/Library/Fonts` and `/Library/LaunchDaemons` on macOS, and
`/usr/local/share/applications`, `/usr/local/share/fonts/oku` and
`/etc/systemd/system` on Linux.

| Kind | macOS and Linux | Windows |
|---|---|---|
| file, `link` | symlink to the source | junction for a directory, copy for a file |
| file, `text` and `render` | symlink through `current/files/` | copy |
| file, `secret` | symlink to `<data>/oku/secrets/` | copy |
| setting | `defaults` on macOS, dconf on Linux | registry under `HKCU` |

Files and settings exist in user scope only.

A generation records each package with its `service` and `system` flags, each
file and each wanted setting. oku computes the wanted items from a generation
and makes the ledger match, so rollback and remove reverse an exposure exactly
(D41), and a failed change is reverted the same way (D59). The ledger keeps
the value a setting had before oku first wrote it (D62).

## Packages

```
cmd/oku/            main
internal/cli/       cobra commands, thin
internal/ref/       parse and fetch refs
internal/forge/     one interface over the hosts that serve repos and releases
internal/manifest/  TOML types, validation, templating
internal/infer/     manifest inference from release assets
internal/platform/  os, arch, libc detection, selector matching
internal/resolve/   version discovery
internal/list/      oku.toml, edited as text
internal/lock/      oku.lock
internal/source/    config.toml: sources, caches, trusted keys, store root
internal/dirs/      config, data and cache directories
internal/store/     download, verify, extract, realize, build steps, vendor
                    steps, cache entries, signatures, gc
internal/sandbox/   linux userns, macos sandbox-exec, fallback
internal/profile/   generations, links, windows junction and shims
internal/shim/      what oku.exe does when it starts as a shim
internal/expose/    ledger of apps, fonts and services
internal/service/   launchd, systemd and task scheduler managers
internal/trust/     allow list, approvals
internal/shellhook/ hook and env output per shell
```

## CLI

```
oku add <ref>[@version] [--from-source] [--yes] [--verbose] [--global]
oku remove <name> [--global]
oku sync [list-ref]
oku update [name]
oku list | info <name> | why <name> | search <term>
oku generations | rollback [n] | gc [--keep N] [--dry-run]
oku source add|remove|list
oku hook <bash|zsh|fish|pwsh> | env [--shell] | allow [dir] | deny [dir]
oku shell <ref>...
oku service list|start|stop|restart|status|logs <name>
oku cache add|remove|list <dir-or-url> | push <dir> [name...]
oku key trust|revoke|list|generate
oku manifest init --from <repo> [-o file] | lint [file...] | bump [file]
oku manifest test [file] [--keep]
oku setup --system | doctor
oku self update | self uninstall [--keep-list] [--yes]
```

Commands that read or change a list act on the project list when one is found,
else the global list. `--global`, or `-g`, forces the global list (D37). The
commands that print data accept `--json` (B103).
