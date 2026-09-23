# Set OS settings from your list

You end up with per-user settings of macOS, Windows or GNOME, such as the Dock,
key repeat or dark mode, written from `oku.toml` and put back as they were when
you remove them.

Each OS has its own table, because a setting of one OS has no counterpart on
another:

| Table | OS | oku writes through |
|---|---|---|
| `[defaults."<domain>"]` | macOS | `/usr/bin/defaults` |
| `[defaults-currenthost."<domain>"]` | macOS | `defaults -currentHost`, for a setting of this one Mac |
| `[registry.'HKCU\...']` | Windows | `reg.exe` |
| `[dconf."<path>"]` | Linux | the `dconf` tool |

oku skips the tables of another OS, so one list works on every machine.

## Set macOS defaults

Name the domain in quotes and list its keys:

```toml
[defaults."com.apple.dock"]
autohide = true
tilesize = 48
autohide-delay = 0.0
orientation = "left"

[defaults.NSGlobalDomain]
AppleInterfaceStyle = "Dark"
KeyRepeat = 1

[defaults.".GlobalPreferences"]
AppleLanguages = ["en-SG", "ms-MY"]
```

Run `oku sync`. It prints one line per setting it wrote, such as
`set com.apple.dock tilesize`.

Quote a domain that has a dot. `[defaults.com.apple.dock]` without quotes is a
table `com` that holds a table `apple`, and not the domain you meant.

Write `0.0`, not `0`, where the setting is a float. A whole number gives macOS
an integer. `defaults read-type <domain> <key>` shows the type a Mac holds.

### Make the change show

After a change oku tells macOS to read its settings again, so most of them
show without a logout.

- The Dock reads its settings only when it starts, so oku restarts the Dock
  when a `com.apple.dock` key changed.
- Other apps that read their settings only at start, such as Finder, show a
  change after you restart them, for example with `killall Finder`.
- An open System Settings window can write its own values over a change. Close
  it before you sync.

### Set a dictionary

A table inside a domain is a dictionary. oku owns the whole dictionary and
writes it whole:

```toml
[defaults."com.apple.Safari".NSUserKeyEquivalents]
"Show Next Tab" = "^l"
```

### Set a setting of this one Mac

macOS keeps some settings per Mac and not per user, in
`~/Library/Preferences/ByHost/`. The battery percentage in the menu bar is one.
Such a key does nothing in `[defaults]`. Put it in `[defaults-currenthost]`:

```toml
[defaults-currenthost."com.apple.controlcenter"]
BatteryShowPercentage = true
```

`defaults -currentHost read <domain>` shows what a Mac holds there. oku prints
such a setting as `currentHost:com.apple.controlcenter BatteryShowPercentage`.

## Set Windows registry values

Name a key under `HKCU` in single quotes, so that TOML keeps its backslashes:

```toml
[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"
```

A key outside `HKCU` is an error on every OS. A subkey is its own table, not a
table inside another. A program reads the registry when it needs a value, so
some changes show only after the program restarts.

## Set GNOME settings with dconf

Name the table after the directory of its keys, without the slashes at the
ends:

```toml
[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
```

Writing needs the dconf service and a D-Bus session, which a desktop login
has. Without a session the write fails and oku undoes the change.

On a machine without the `dconf` tool, such as a server, oku skips the table
and says so:

```
[dconf] is skipped, because the dconf tool is not on PATH
```

## Know how a value maps

The type comes from the TOML value:

| TOML | macOS | Windows | Linux |
|---|---|---|---|
| `true`, `false` | boolean | `REG_DWORD` 1 or 0 | boolean |
| `48` | integer | `REG_DWORD`, or `REG_QWORD` above 4294967295. A negative number is an error. | `int32`, or `int64` outside its range |
| `0.5` | float | An error, the registry has no float. | double |
| `"left"` | string | `REG_SZ` | string |
| `["a", "b"]` | array | `REG_MULTI_SZ`, of strings that are not empty | An array of one type. An empty array is an error, because it has no type. |
| a table | dictionary, written whole | An error. Write the subkey as its own table. | An error. |

## Remove a setting

Before oku first writes a key, it records the value the key had, or that it
had none. Delete the key from `oku.toml` and run `oku sync`. oku puts the old
value back, or deletes the key again, and prints a line such as
`restored com.apple.dock autohide`.

oku puts a value back with the type it had. That includes a type the list
cannot write, such as `REG_BINARY`, `REG_EXPAND_SZ` or a dconf `uint32`.

When oku deletes the last key of a domain that it created, macOS keeps an
empty file for that domain.

## Undo a change

`oku rollback` sets every setting to what the generation before had, with the
packages and files. `oku self uninstall` puts back every value oku changed.

Settings go through the same check and undo as the rest of a change. When a
write fails partway, oku puts back the ones it already wrote. See
[Undo and clean up](undo-and-clean-up.md).

## Know the limits

- Only the global list and the lists it includes may hold settings tables. A
  [project](projects.md) list with one is an error:

  ```
  oku: /home/you/src/app/oku.toml has [defaults], and only the global list may change settings
  ```

- An included list may hold settings. Your own `oku.toml` overrides a key that
  an include sets, and a later include overrides an earlier one.
- oku never writes `/Library/Preferences` or anything else that needs root.
- `oku add` and `oku remove` carry the settings over unchanged. Only
  `oku sync` and `oku update` read the tables again.

See [oku.toml](../reference/oku-toml.md) for every key of these tables.
