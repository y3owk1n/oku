# Secrets

The global list can place a value that a [sops](https://github.com/getsops/sops)
or an [age](https://github.com/FiloSottile/age) file holds, such as an SSH key
or a token. You create and edit those files with `sops` and `age`, as before.
oku only decrypts them.

```toml
[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{home}}/.ssh/id_ed25519" = { secret = "./secrets/secrets.yaml", key = "ssh/id_ed25519" }
"{{home}}/.ssh/backup_key" = { secret = "./secrets/backup_key.age" }
"{{home}}/.config/gh/hosts.yml" = { render = "./files/gh-hosts.tmpl" }
```

```
# files/gh-hosts.tmpl
github.com:
    user: {{user}}
    oauth_token: {{secret.github_token}}
```

## Two ways to use a secret

A `secret` entry of [`[files]`](list-and-lock.md#files-in-your-home-directory)
writes one decrypted value to a path. `key` is the path of one value in a sops
file, with `/` between its parts, so `ssh/id_ed25519` is `id_ed25519` under
`ssh`. Without `key` the whole decrypted file is the value, which is what an
age file needs.

`[secrets]` gives a value a name. A `text` and a
[template](list-and-lock.md#variables-and-templates) then use it as
`{{secret.<name>}}`, beside the other variables. A name that `[secrets]` lacks
is an error.

A path is relative to the list that declares it.

## Two formats

oku looks at the file to tell the format.

| Format | How oku decrypts it |
|---|---|
| An age file, binary or armored | oku does it itself, so no other program is needed. A new machine can decrypt its first key this way. |
| A sops file, in YAML, JSON, dotenv or INI | oku runs `sops decrypt`. sops also handles PGP and cloud keys. |

oku looks for `sops` in the packages of your list first, also in the generation
it is about to activate, and then on `PATH`. So the first `oku sync` of a machine
can install sops and decrypt with it:

```toml
[packages]
sops = "github:getsops/sops"
```

## The key

oku reads the age identities from the file in `SOPS_AGE_KEY_FILE`. Without that
variable it reads `sops/age/keys.txt` in your config directory, which is
`~/.config/sops/age/keys.txt` unless `XDG_CONFIG_HOME` is set, and
`%APPDATA%\sops\age\keys.txt` on Windows. It passes the same path to sops, which
would look elsewhere on macOS.

oku never writes or creates a key. Copy your key file to a new machine
yourself, once, before the first sync.

## What is on disk

| Where | What |
|---|---|
| A generation | A copy of the encrypted file. For a `text` or a template, the content with every other variable filled in and only the name of the secret. Never decrypted bytes. |
| `<data>/oku/secrets/` | The decrypted files. The path in your home directory is a symlink to one of them. |
| `oku.lock`, the ledger, what oku prints, an error | Never decrypted bytes. An error names the encrypted file and the `key`. |

Only you can read `<data>/oku/secrets/` and the files in it. On macOS and Linux
the directory has mode `0700` and a file `0600`, or the `mode` of the entry. On
Windows both get an access control list that names you alone and inherits
nothing. So does the copy that Windows gets in place of a symlink, see
[Windows](windows.md#files-in-your-home-directory).

A generation holds the encrypted file. So `oku rollback` decrypts the file of
the generation it returns to, and an old generation never holds an old key in
readable form. After you edit a file with `sops`, the next `oku sync` writes
the new value. oku deletes a secret whose entry leaves the list, and
`oku self uninstall` deletes all of them.

## When a secret cannot be decrypted

oku decrypts every secret in memory before it changes anything. A missing key
file, a missing `sops`, a wrong `key` or a file that was not encrypted to your
key stops the sync, and the machine stays as it was:

```
oku: decrypt ./secrets/secrets.yaml, key github/token: exit status 128
Failed to get the data key required to decrypt the SOPS file.
```

`oku doctor` reports a list with secrets on a machine where the key file is
missing, or where a sops file has no `sops` to decrypt it.

## Limits

- Only the global list may hold `[secrets]` or a `secret` entry, and only a
  list on this machine, not one that an include reads from a URL or a repo.
- oku does not create, edit or re-encrypt a secret. `sops` and `age` do that.
- An age file holds one value, so `key` with an age file is an error.
