#!/usr/bin/env bash
# ND1 oracle — executable acceptance checks for the two detector gaps
# (.swarm/ND-RUN-SPEC.md). Emits "ORACLE PASS" as the final line only when
# every check passes.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL"; exit 1; }

SRC=".swarm/tier3/ND1/zz_oracle_nd1_test.go"
DEST="internal/services/dataloader/zz_oracle_nd1_test.go"
pass=1

cp "$SRC" "$DEST" || { echo "oracle: cannot stage test file"; echo "ORACLE FAIL"; exit 1; }
cleanup() { rm -f "$DEST"; }
trap cleanup EXIT

echo "== A1/A2/A3: oracle fixture + guard + real-data tests =="
go test -count=1 -run 'TestOracleND1' ./internal/services/dataloader/ || pass=0

echo "== A4: package suites =="
go test -count=1 ./internal/services/dataloader/ ./internal/services/mcpsvc/... || pass=0

echo "== A5: list_duplicates description names the scheduled→autopay shape =="
if grep -Eiq 'autopay' internal/services/mcpsvc/admin/duplicates.go; then
  echo "A5 ok"
else
  echo "A5 FAIL: internal/services/mcpsvc/admin/duplicates.go never mentions autopay settlement"
  pass=0
fi

if [[ "$pass" -eq 1 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
