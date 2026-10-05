#!/bin/sh
# Runs the real oku inside a container of a distribution whose package manager
# answers for a name that another package provides (B453). OKU names a Linux
# build of oku. The container is thrown away, so the script works as root.
set -eu

check() {
    if [ "$2" = yes ]; then echo "ok: $1"; else echo "FAILED: $1"; exit 1; fi
}

. /etc/os-release

case "$ID" in
fedora)
    # zlib-devel is no package of its own here. zlib-ng-compat-devel provides it.
    dnf -y -q install zlib-ng-compat-devel >/dev/null
    manager=dnf provided=zlib-devel
    ;;
arch)
    # awk is no package of its own here. gawk provides it.
    manager=pacman provided=awk
    ;;
*)
    echo "no check for $ID"
    exit 1
    ;;
esac

root=$(mktemp -d)
export XDG_CONFIG_HOME="$root/config" XDG_DATA_HOME="$root/data" XDG_CACHE_HOME="$root/cache"
mkdir -p "$root/pkg" "$XDG_CONFIG_HOME/oku"

printf '#!/bin/sh\necho hi\n' >"$root/pkg/hi"
chmod +x "$root/pkg/hi"
tar czf "$root/hi.tar.gz" -C "$root/pkg" hi
sum=$(sha256sum "$root/hi.tar.gz" | cut -d' ' -f1)

cat >"$root/hi.toml" <<TOML
[package]
name = "hi"
[version]
value = "1.0.0"
[[artifact]]
url = "file://$root/hi.tar.gz"
sha256 = "$sum"
bin = ["hi"]
TOML

cat >"$XDG_CONFIG_HOME/oku/oku.toml" <<TOML
[packages]
hi = "$root/hi.toml"

[host]
provided = { $manager = "$provided" }
nope = { $manager = "oku-no-such-package" }
TOML

out=$("$OKU" sync --yes 2>&1)
echo "$out"

case "$out" in *"provided is missing"*) found=no ;; *) found=yes ;; esac
check "B453: $manager counts $provided as installed, which another package provides" "$found"

case "$out" in *"nope is missing"*) missing=yes ;; *) missing=no ;; esac
check "B453: $manager still reports a package that is not installed" "$missing"
