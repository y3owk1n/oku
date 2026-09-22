#!/bin/sh
# Installs oku for the current user. It needs no root and edits no existing file.
#
#   curl -fsSL https://raw.githubusercontent.com/y3owk1n/oku/main/install.sh | sh
#
# OKU_INSTALL_DIR  where the binary goes, default ~/.local/bin
# OKU_VERSION      a release tag such as v0.1.0, or nightly for the build of the
#                  newest commit on main, default the newest release
set -eu

repo="y3owk1n/oku"
# The minisign public key that signs oku's releases. The script checks the
# signature when minisign is installed, and always checks the sha256.
release_key="RWSjFGqIxI8IPGwKE/uRgugZ51qCEMe1CDbFRVTMUAuin42JiOxg2HNW"
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
	echo "checked the sha256 of $name. Install minisign to check its signature too, see https://jedisct1.github.io/minisign/"
fi

mkdir -p "$dir"
chmod +x "$tmp/$name"
mv "$tmp/$name" "$dir/oku"

version="$("$dir/oku" --version 2>/dev/null | head -n 1)"
echo "installed ${version:-oku} at $dir/oku"

# The line uses $HOME when the binary is under it, so it also works in a
# dotfiles repo that several machines share.
case "$dir" in
"$HOME"/*) oku="\$HOME${dir#"$HOME"}/oku" ;;
*) oku="$dir/oku" ;;
esac

shell="$(basename "${SHELL:-sh}")"
case "$shell" in
bash)
	rc="$HOME/.bashrc"
	line="[ -x \"$oku\" ] && eval \"\$(\"$oku\" hook bash)\""
	;;
zsh)
	rc="$HOME/.zshrc"
	line="[ -x \"$oku\" ] && eval \"\$(\"$oku\" hook zsh)\""
	;;
fish)
	rc="$HOME/.config/fish/config.fish"
	line="test -x \"$oku\"; and \"$oku\" hook fish | source"
	;;
*)
	rc=""
	line=""
	;;
esac

# What to do once oku is on PATH.
next_steps() {
	echo
	echo "then:"
	echo "  oku doctor                  checks the setup"
	echo "  oku add github:sharkdp/fd   installs a first program, try: fd --version"
	echo "  oku self update             replaces oku with the newest release later"
}

echo
if [ -z "$line" ]; then
	echo "next: put $dir on PATH, then run \"oku hook --help\" for the line your shell needs"
	next_steps
	exit 0
fi

pretty="~${rc#"$HOME"}"

# A startup file that loads the hook already, from an earlier install, needs no
# second line. The pattern is the one "oku doctor" uses.
if [ -r "$rc" ] && grep -v '^[[:space:]]*#' "$rc" | grep -Eq 'oku" hook|oku hook'; then
	echo "$pretty already loads oku. Open a new terminal, or run:  exec $shell"
	next_steps
	exit 0
fi

echo "There is one step left. Add this line to $pretty:"
echo
echo "  $line"
echo
echo "It puts oku and the programs it installs on PATH, and loads their completions. This command adds it for you:"
echo
echo "  echo '$line' >> $pretty && exec $shell"
next_steps
