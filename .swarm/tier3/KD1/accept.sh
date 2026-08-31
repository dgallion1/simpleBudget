#!/usr/bin/env bash
# KD1 oracle — executable acceptance checks for the KD-2026-08-30d
# reconciliation contract (.swarm/KD-RUN-SPEC.md). Emits "ORACLE PASS" as
# the final line only when every check passes.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL"; exit 1; }

SRC=".swarm/tier3/KD1/zz_oracle_kd1_test.go"
DEST="internal/handlers/dashboard/zz_oracle_kd1_test.go"
pass=1

cp "$SRC" "$DEST" || { echo "oracle: cannot stage test file"; echo "ORACLE FAIL"; exit 1; }
cleanup() { rm -f "$DEST"; }
trap cleanup EXIT

echo "== KD-2026-08-30d reconciliation + regressions =="
go test -count=1 -run 'TestOracleKD1' -v ./internal/handlers/dashboard/ 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|PASS|FAIL|ok  )" || true
go test -count=1 -run 'TestOracleKD1' ./internal/handlers/dashboard/ >/dev/null 2>&1 || pass=0

echo "== package suites =="
go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/ || pass=0

if [[ "$pass" -eq 1 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
