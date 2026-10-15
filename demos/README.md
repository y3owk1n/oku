# Demo tapes

Each tape here records one clip of [docs/demos](../docs/demos/README.md) with
[VHS](https://github.com/charmbracelet/vhs). A recording writes the GIF to
`docs/demos/` and an MP4 to `demos/out/`, which git ignores.

## Record

From the repo root:

```sh
GITHUB_TOKEN=$(gh auth token) just demos                  # every tape
GITHUB_TOKEN=$(gh auth token) vhs demos/01-install.tape   # one tape
```

Without a token GitHub allows 60 API requests an hour, and a full run makes
more. The token never shows on screen.

Record one tape at a time. VHS keeps only the frames it captures. When two
tapes record at once, or the frame rate is above 20 fps at this window size,
it captures fewer and the clip plays too fast.

## What a tape runs

`setup.sh` runs hidden at the start of each tape. It gives the scene its own
HOME and XDG directories under `/tmp/oku-demo/<scene>`, so a recording never
reads or writes your own oku setup. Then `fixtures/<scene>.sh` writes the files
the scene starts from. `style.tape` sets the shell, font, theme, window size
and frame rate of every tape.

The first run copies `oku`, `sops`, `age`, `age-keygen` and `bat` from your
`PATH` into `/tmp/oku-demo/bin`. To record a build of your own, put it there
before you start:

```sh
mkdir -p /tmp/oku-demo/bin && go build -o /tmp/oku-demo/bin/oku ./cmd/oku
```

Every tape downloads real releases. `08-os-settings` writes the macOS domain
`com.example.oku-demo` and rolls it back. `09-services` is a dry run, since a
real one would load a launchd agent for your account.

## Clean up

```sh
chmod -R u+rwX /tmp/oku-demo && rm -rf /tmp/oku-demo
rm -f ~/Library/Preferences/com.example.oku-demo.plist
```
