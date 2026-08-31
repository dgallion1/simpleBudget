#!/usr/bin/env bash
# DP3 oracle — executable acceptance checks for the widened pending→posted
# matcher (.swarm/DP-RUN-SPEC.md, DP3 section). Emits "ORACLE PASS" as the
# final line only when every check passes.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL"; exit 1; }

# The worktree checkout carries no data/; fall back to the main checkout's.
if [[ -z "${BUDGET2_DATA_DIR:-}" && ! -d data ]]; then
  export BUDGET2_DATA_DIR=/home/darrell/bin/ai/budget2/data
fi

SRC=".swarm/tier3/DP3/zz_oracle_dp3_test.go"
DEST="internal/services/dataloader/zz_oracle_dp3_test.go"
pass=1

cp "$SRC" "$DEST" || { echo "oracle: cannot stage test file"; echo "ORACLE FAIL"; exit 1; }
cleanup() { rm -f "$DEST"; }
trap cleanup EXIT

echo "== B1: real-data regression (six new pairs, decisions bind) =="
go test -count=1 -run 'TestOracleDP3' ./internal/services/dataloader/ || pass=0

echo "== B2: package suites =="
go test -count=1 ./internal/services/dataloader/ ./internal/services/mcpsvc/... || pass=0

echo "== B3: list_duplicates description window matches the code =="
if grep -q 'within 5 days' internal/services/mcpsvc/admin/duplicates.go; then
  echo "B3 ok"
else
  echo "B3 FAIL: internal/services/mcpsvc/admin/duplicates.go still advertises the old pending→posted window"
  pass=0
fi

if [[ "$pass" -eq 1 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
