# Keep secrets in your list

You end up with SSH keys and tokens stored encrypted in your machines repo, and
decrypted into your home directory by `oku sync`. You create and edit the
encrypted files with [sops](https://github.com/getsops/sops) and
[age](https://github.com/FiloSottile/age). oku only decrypts them.

This guide starts from a machine with oku and a global list at
`~/.config/oku/oku.toml`, and no key yet.

## Install age and sops

Neither project publishes an oku manifest, so oku infers one from each
release. The age release also holds `age-keygen`, which oku picks up because
its name starts with `age-`:

```
$ oku add github:FiloSottile/age github:getsops/sops
github:FiloSottile/age has no manifest, so oku inferred one from its newest release, --verbose prints it
added age 1.3.2
github:getsops/sops has no manifest, so oku inferred one from its newest release, --verbose prints it
added sops 3.13.3
```

## Create an age key

oku reads your age key from `~/.config/sops/age/keys.txt`. Create it there:

```
$ mkdir -p ~/.config/sops/age
$ age-keygen -o ~/.config/sops/age/keys.txt
Public key: age1mmpjtr95xhszd2j4vqgpx8u5gpwdg802uup8ntyxuzmkgtm9ds8ql7dxsl
```

The file holds your secret key. Keep a copy somewhere safe, such as a password
manager, because a new machine needs it and oku never creates or writes one.
The public key, `age1...`, is what you encrypt to.

oku looks for the key in this order:

1. The file that `SOPS_AGE_KEY_FILE` names.
2. `sops/age/keys.txt` in your config directory. That is
   `$XDG_CONFIG_HOME/sops/age/keys.txt` when `XDG_CONFIG_HOME` is set, else
   `~/.config/sops/age/keys.txt`. On Windows it is
   `%APPDATA%\sops\age\keys.txt`.

oku passes the same path to sops when it runs it. sops on its own looks
elsewhere on macOS, so set `SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt` in
your shell before you edit a file with `sops` there.

## Encrypt a file with sops

Tell sops which key to encrypt to. Save this as `~/.config/oku/.sops.yaml`,
with your own public key:

```yaml
creation_rules:
  - path_regex: secrets/.*
    age: age1mmpjtr95xhszd2j4vqgpx8u5gpwdg802uup8ntyxuzmkgtm9ds8ql7dxsl
```

Write the values in plain YAML, then encrypt the file in place:

```sh
cd ~/.config/oku
mkdir -p secrets
$EDITOR secrets/secrets.yaml
sops encrypt --in-place secrets/secrets.yaml
```

```yaml
# secrets/secrets.yaml, before sops encrypts it
github:
  token: ghp_example
ssh:
  id_ed25519: |
    -----BEGIN OPENSSH PRIVATE KEY-----
    ...
    -----END OPENSSH PRIVATE KEY-----
```

Each value is now `ENC[AES256_GCM,...]`, and the file is safe to commit. Later,
`sops edit secrets/secrets.yaml` opens it decrypted and encrypts it again when
you save.

## Place secrets with oku

oku places a secret in one of two ways.

A `secret` entry of `[files]` writes one decrypted value to a path. A
`[secrets]` entry gives a value a name, and a `text` or a `render` template
uses it as `{{secret.<name>}}`:

```toml
[packages]
age = "github:FiloSottile/age"
sops = "github:getsops/sops"

[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
"{{config}}/gh/hosts.yml" = { render = "./files/gh-hosts.tmpl", mode = "0600" }
```

```
# files/gh-hosts.tmpl
github.com:
    oauth_token: {{secret.github_token}}
```

```
$ oku sync
decrypting ./secrets/secrets.yaml, key github/token
decrypting ./secrets/secrets.yaml, key ssh/id_ed25519
wrote the secret /home/you/.config/gh/hosts.yml
wrote the secret /home/you/.ssh/id_ed25519
profile now holds 2 packages, 2 files, generation 4, 70ms
```

- `key` is the path of one value in a sops file, with `/` between its parts.
  So `ssh/id_ed25519` is `id_ed25519` under `ssh`.
- Without `key`, the whole decrypted file is the value.
- A path is relative to the list that declares it.
- A template that names a secret missing from `[secrets]` is an error.

The other `[files]` keys, `[vars]` and templates are in
[Manage your dotfiles](dotfiles.md).

## Use an age file instead of sops

oku looks at a file to tell its format:

| Format | How oku decrypts it |
|---|---|
| An age file, binary or armored | oku decrypts it itself, with no other program. |
| A sops file, in YAML, JSON, dotenv or INI | oku runs `sops decrypt`. sops also handles PGP and cloud keys. |

An age file holds one value, so it takes no `key`:

```sh
age -r age1mmpjtr95xhszd2j4vqgpx8u5gpwdg802uup8ntyxuzmkgtm9ds8ql7dxsl -o secrets/backup_key.age backup_key
```

```toml
[files]
"{{home}}/.ssh/backup_key" = { secret = "./secrets/backup_key.age" }
```

oku looks for `sops` first in the packages of your list, including the
generation it is about to activate, then on `PATH`. So the first `oku sync` of a new machine
can install sops and decrypt with it.

## Know what lands on disk

| Where | What |
|---|---|
| A [generation](../how-oku-works.md#generation) | A copy of the encrypted file. For a `text` or a template, the content with every other variable filled in and only the name of the secret. Never decrypted bytes. |
| `~/.local/share/oku/secrets/` | The decrypted files. The path in your home directory is a symlink to one of them. |
| `oku.lock`, the [ledger](../how-oku-works.md#ledger), what oku prints, an error | Never decrypted bytes. An error names the encrypted file and the `key`. |

Only you can read the decrypted files:

- On macOS and Linux, `secrets/` has mode `0700` and each file `0600`, or the
  `mode` of its entry.
- When oku has to create the directory above a secret, such as `~/.ssh`, it
  creates it with mode `0700`. A directory that exists keeps its mode.
- On Windows the directory and each file get an access control list that names
  you alone and inherits nothing. The path in your home directory is a copy,
  not a symlink, and gets the same list. See [Windows](windows.md).

After you edit a file with `sops`, the next `oku sync` writes the new value.
`oku rollback` decrypts the file of the generation it returns to, so an old
generation never holds an old key in readable form. oku deletes a decrypted
file whose entry leaves the list, and `oku self uninstall` deletes all of them.

## Set up a new machine

A new machine needs your age key before its first sync. Copy `keys.txt` to
`~/.config/sops/age/keys.txt` yourself, then run `oku sync`. oku decrypts age
files with no other program, and installs sops from your list before it needs
it.

`oku doctor` reports a list with secrets on a machine where the key is missing:

```
problem  the list has secrets, and the age identities are not at /home/you/.config/sops/age/keys.txt
copy your key file there, or set SOPS_AGE_KEY_FILE
```

It also reports a sops file when `sops` is neither in the list nor on `PATH`.

## When a secret cannot be decrypted

oku decrypts every secret in memory before it changes anything. A missing key,
a missing `sops`, a wrong `key` or a file that was not encrypted to your key
stops the sync, and the machine stays as it was:

```
$ oku sync
oku: decrypt ./secrets/secrets.yaml, key github/token: exit status 128
Failed to get the data key required to decrypt the SOPS file.
```

sops prints more lines after these that say which key it tried.

## Give a project's commands a secret

A project list may load an encrypted `.env` file with `[[env.file]]` and
`secret = true`, and load it for `oku exec` only with `scope = "exec"`. oku
decrypts it in memory each time and writes no decrypted file. See
[Keep secrets out of the shell](projects.md#keep-secrets-out-of-the-shell).

## What secrets cannot do

- Only the global list and the lists it includes may hold `[secrets]` or a
  `secret` entry of `[files]`. A [project](projects.md) list may not, and
  loads an encrypted `.env` file with `[[env.file]]` instead.
- An included list at a URL may not hold them. An included list from a repo
  may, and oku reads its encrypted files from the repo at the pinned commit.
- oku does not create, edit or re-encrypt a secret. `sops` and `age` do that.
- `key` with an age file is an error.
