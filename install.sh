#!/bin/sh
# Installs oku for the current user. It needs no root and edits no existing file.
#
#   curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
#
# OKU_INSTALL_DIR  where the binary goes, default ~/.local/bin
# OKU_VERSION      a release tag such as v0.1.0, default the newest release
set -eu

repo="y3owk1n/oku"
# The minisign public key that signs oku's releases. The script checks the
# signature when minisign is installed, and always checks the sha256.
release_key=""
dir="${OKU_INSTALL_DIR:-$HOME/.local/bin}"
base="${OKU_RELEASE_URL:-https://github.com/$repo/releases}"

fail() {
	echo "install oku: $*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "this script installs on Linux and macOS, on Windows use install.ps1" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "oku has no release for the CPU $(uname -m)" ;;
esac

name="oku-$os-$arch"
if [ -n "${OKU_VERSION:-}" ]; then
	from="$base/download/$OKU_VERSION"
else
	from="$base/latest/download"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fetch() {
	curl -fsSL "$from/$1" -o "$tmp/$1" || fail "could not download $from/$1"
}

fetch "$name"
fetch checksums.txt

want="$(awk -v file="$name" '$2 == file { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "checksums.txt does not list $name"

if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "$tmp/$name" | awk '{ print $1 }')"
else
	got="$(shasum -a 256 "$tmp/$name" | awk '{ print $1 }')"
fi

[ "$got" = "$want" ] || fail "the sha256 of $name is $got, and checksums.txt says $want"

if [ -n "$release_key" ] && command -v minisign >/dev/null 2>&1; then
	fetch "$name.minisig"
	minisign -Vm "$tmp/$name" -P "$release_key" -q || fail "the signature of $name is not from oku's release key"
	echo "checked the minisign signature of $name"
else
	echo "checked the sha256 of $name. Install minisign to check its signature too."
fi

mkdir -p "$dir"
chmod +x "$tmp/$name"
mv "$tmp/$name" "$dir/oku"

echo "installed $dir/oku"

case ":$PATH:" in
*":$dir:"*) ;;
*) echo "add $dir to PATH to run it" ;;
esac

shell="$(basename "${SHELL:-sh}")"
case "$shell" in
bash | zsh) line="command -v oku >/dev/null 2>&1 && eval \"\$(oku hook $shell)\"" ;;
fish) line="command -q oku; and oku hook fish | source" ;;
*) line="" ;;
esac

if [ -n "$line" ]; then
	echo "to use oku in projects, add this line to your $shell startup file:"
	echo "  $line"
fi
