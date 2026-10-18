#!/usr/bin/env bash
#
# Runs one shard of the Go test suite. The integration suite dominates CI time
# and Go 1.27 has no -shard flag, so the workflow splits tests by name instead
# of by package.
#
# Two groups are handled separately:
#   - unit packages run in full on every shard; they take seconds and splitting
#     them would cost more in per-shard setup than it saves.
#   - tests/integration is filtered to the names assigned to this shard by
#     scripts/test-shards.txt.
#
# Usage: scripts/test-shard.sh <shard-index> <shard-count>
# See docs/04-ui-and-cli-specification.md section 6.

set -euo pipefail

cd "$(dirname "$0")/.."

SHARD="${1:?usage: test-shard.sh <shard-index> <shard-count>}"
TOTAL="${2:?usage: test-shard.sh <shard-index> <shard-count>}"
MANIFEST="scripts/test-shards.txt"

if [ ! -f "$MANIFEST" ]; then
  printf 'error: %s is missing; run scripts/balance-test-shards.sh to generate it\n' "$MANIFEST" >&2
  exit 1
fi

# Shard assignment is 1-based. Every test in the manifest belongs to exactly one
# shard, so the union of all shards is the full suite.
pattern="$(awk -v s="$SHARD" -v n="$TOTAL" '
  /^#/ { next }
  NF == 0 { next }
  {
    # Lines are "<name> <shard>"; a shard number above TOTAL is a stale
    # manifest, which would silently drop tests, so fail loudly instead.
    if ($2 > n) { printf "stale manifest: %s is assigned to shard %s but only %s shards exist\n", $1, $2, n > "/dev/stderr"; exit 2 }
    if ($2 == s) { printf "%s|", $1 }
  }
' "$MANIFEST")" || exit 1
pattern="${pattern%|}"

if [ -z "$pattern" ]; then
  printf 'error: shard %s/%s matched no tests; the manifest is incomplete\n' "$SHARD" "$TOTAL" >&2
  exit 1
fi

printf 'shard %s/%s: %s integration tests\n' "$SHARD" "$TOTAL" "$(printf '%s' "$pattern" | tr '|' '\n' | wc -l | tr -d ' ')"

go test ./internal/... ./plugins/... -count=1
go test ./tests/integration -count=1 -run "^(${pattern})$" -v
