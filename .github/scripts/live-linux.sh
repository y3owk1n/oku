#!/bin/sh
# Runs the real oku on Linux against the real dconf database, in throwaway
# directories and its own D-Bus session. OKU names a built binary. Without it the
# script builds one.
set -eu

# The dconf service is started by the session bus and inherits its environment.
# So the directories are set first and the script then runs itself inside a new
# session. Otherwise the service and the dconf tool would use two databases.
if [ -z "${OKU_LIVE_ROOT:-}" ]; then
    OKU_LIVE_ROOT=$(mktemp -d)
    export OKU_LIVE_ROOT
    trap 'rm -rf "$OKU_LIVE_ROOT"' EXIT

    if [ -z "${OKU:-}" ]; then
        OKU="$OKU_LIVE_ROOT/oku"
        go build -o "$OKU" ./cmd/oku
    fi

    export OKU
    export XDG_CONFIG_HOME="$OKU_LIVE_ROOT/config" XDG_DATA_HOME="$OKU_LIVE_ROOT/data"
    export XDG_CACHE_HOME="$OKU_LIVE_ROOT/cache"

    dbus-run-session -- "$0"
    exit
fi

oku=$OKU
mkdir -p "$XDG_CONFIG_HOME/oku"
list="$XDG_CONFIG_HOME/oku/oku.toml"

# The checkout has an oku.toml, which would make every command act on that
# project and not on the global list.
cd "$OKU_LIVE_ROOT"

check() {
    if [ "$2" != "$3" ]; then
        echo "FAILED: $1: got [$2], want [$3]"
        exit 1
    fi

    echo "ok: $1"
}

# Two keys exist before oku, so that it has something to put back.
dconf write /org/oku/live/scheme "'default'"
dconf write /org/oku/live/count "uint32 7"

cat > "$list" <<'LIST'
[dconf."org/oku/live"]
scheme = "prefer-dark"
count = 9
enabled = true
scale = 1.5
names = ["one", "it's"]

[defaults."com.apple.dock"]
tilesize = 48

[registry.'HKCU\Software\oku-live-test']
Delay = "0"
LIST

"$oku" sync
check 'a string arrives' "$(dconf read /org/oku/live/scheme)" "'prefer-dark'"
check 'an integer arrives' "$(dconf read /org/oku/live/count)" '9'
check 'a boolean arrives' "$(dconf read /org/oku/live/enabled)" 'true'
check 'a float arrives' "$(dconf read /org/oku/live/scale)" '1.5'
check 'an array arrives, with a quote escaped' "$(dconf read /org/oku/live/names)" "['one', \"it's\"]"

sed -i 's/count = 9/count = 11/' "$list"
"$oku" sync
check 'a changed value arrives' "$(dconf read /org/oku/live/count)" '11'

"$oku" rollback
check 'rollback gives the value of the generation before' "$(dconf read /org/oku/live/count)" '9'

: > "$list"
"$oku" sync
check 'a string gets back what it held before oku' "$(dconf read /org/oku/live/scheme)" "'default'"
check 'a value gets back its type too' "$(dconf read /org/oku/live/count)" 'uint32 7'
check 'a key that oku added is reset' "$(dconf read /org/oku/live/enabled)" ''

dconf reset -f /org/oku/live/
echo 'live test passed'
