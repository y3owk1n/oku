#!/usr/bin/env bash
# Runs go test for every package, with the tests of internal/cli split by name
# across parallel processes. Those tests set HOME and other variables for the
# whole process, so they cannot run in parallel inside one. Extra arguments go
# to every go test, such as -count=1.
set -euo pipefail

shards=${OKU_TEST_SHARDS:-4}
cli=github.com/y3owk1n/oku/internal/cli

tests=$(go test -list '.*' "$@" ./internal/cli | grep '^Test' || true)

# macOS has bash 3.2, which has no mapfile.
others=()
while read -r pkg; do others+=("$pkg"); done < <(go list ./... | grep -vx "$cli")

pids=()
go test "$@" "${others[@]}" &
pids+=($!)

for ((i = 0; i < shards; i++)); do
  names=$(echo "$tests" | awk -v n="$shards" -v i="$i" 'NR % n == i' | paste -sd '|' -)
  [ -n "$names" ] || continue
  go test "$@" -run "^($names)\$" ./internal/cli &
  pids+=($!)
done

status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=1
done

exit $status
