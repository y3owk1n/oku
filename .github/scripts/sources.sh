#!/usr/bin/env bash
# Asks each outside source that oku reads for one real package, with the oku of
# this checkout, in throwaway directories. A failure means the source changed
# its format or its address, or is down. The script checks every source, writes
# the failures to report.md in its directory, and fails when there is one.
set -uo pipefail

root="${RUNNER_TEMP:-$(mktemp -d)}/oku-sources"
rm -rf "$root"
mkdir -p "$root/home"

export HOME="$root/home"
export XDG_CONFIG_HOME="$root/config"
export XDG_DATA_HOME="$root/data"
export XDG_CACHE_HOME="$root/cache"
export XDG_STATE_HOME="$root/state"

oku="$root/oku"
go build -o "$oku" ./cmd/oku || exit 1

# A pypi: package runs through the python that [runtimes] names, so the global
# list names the example one of this checkout.
mkdir -p "$XDG_CONFIG_HOME/oku"
printf '[runtimes]\npython = "%s"\n' "$PWD/examples/runtimes/python.toml" >"$XDG_CONFIG_HOME/oku/oku.toml"

# The checkout has an oku.toml, which would make every command below act on
# that project and not on the global profile.
cd "$root"

report="$root/report.md"
failed=0

# check runs oku with the rest of its arguments and records a failure under
# the name of the source.
check() {
  local source="$1"
  shift

  local out
  if out="$("$oku" "$@" 2>&1)"; then
    echo "ok: $source"

    return
  fi

  failed=$((failed + 1))
  echo "FAILED: $source"
  echo "$out" | sed 's/^/    /'

  {
    echo "### $source"
    echo
    echo '```'
    echo "oku $*"
    echo "$out"
    echo '```'
    echo
  } >>"$report"
}

# A plan reads the versions and the files of a release, and changes nothing.
check "GitHub releases" add github:BurntSushi/ripgrep --plan
check "GitLab releases" add gitlab:gitlab-org/cli --plan
check "Gitea releases" add gitea:gitea.com/gitea/tea --plan
check "the aqua registry" add aqua:cli/cli --plan
check "a cask with a JSON feed" add cask:obsidian --plan
check "a cask with a Sparkle feed" add cask:iina --plan
check "a cask with a redirect" add cask:slack --plan
check "the npm registry" add npm:prettier --plan
check "PyPI" add pypi:ruff --plan
check "crates.io" add cargo:ripgrep --plan
check "the Go module proxy" add go:golang.org/x/tools/gopls --plan

# Scoop and winget packages are for Windows, so oku only translates them.
check "Scoop" manifest init --from scoop:ripgrep -o -
check "winget" manifest init --from winget:jqlang.jq -o -

if [ "$failed" -gt 0 ]; then
  echo "$failed source(s) failed, see $report"

  exit 1
fi
