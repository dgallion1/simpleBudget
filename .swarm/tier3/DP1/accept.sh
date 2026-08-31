#!/usr/bin/env bash
# DP1 oracle — executable acceptance checks for the pending→posted duplicate
# shape (.swarm/DP-RUN-SPEC.md). Emits "ORACLE PASS" as the final line only
# when every check passes.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL"; exit 1; }

SRC=".swarm/tier3/DP1/zz_oracle_dp1_test.go"
DEST="internal/services/dataloader/zz_oracle_dp1_test.go"
pass=1

cp "$SRC" "$DEST" || { echo "oracle: cannot stage test file"; echo "ORACLE FAIL"; exit 1; }
cleanup() { rm -f "$DEST"; }
trap cleanup EXIT

echo "== A1/A2: oracle fixture + real-data tests =="
go test -count=1 -run 'TestOracleDP1' ./internal/services/dataloader/ || pass=0

echo "== A3: package suites =="
go test -count=1 ./internal/services/dataloader/ ./internal/services/mcpsvc/... || pass=0

echo "== A4: list_duplicates description names the pending→posted shape =="
if grep -Eiq '\bpending\b.*\bposted\b|\bposted\b.*\bpending\b' internal/services/mcpsvc/admin/duplicates.go; then
  echo "A4 ok"
else
  echo "A4 FAIL: internal/services/mcpsvc/admin/duplicates.go never mentions pending near posted"
  pass=0
fi

if [[ "$pass" -eq 1 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
