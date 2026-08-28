#!/usr/bin/env bash
# Tier-3 oracle for T13 (re-export duplicate detection via Original Description).
# Validated 2026-08-28: fails on the pre-change tree (named tests absent),
# must pass on the finished work. Exit 0 = accept.
set -euo pipefail
cd "$(dirname "$0")/../../.."

echo "== build =="
go build ./...

echo "== named oracle tests =="
out=$(go test -count=1 -v -run 'TestDetect_SameDayReimport|TestLoadCSVFile_OriginalDescription' \
  ./internal/services/dataloader/ 2>&1) || { echo "$out"; echo "FAIL: oracle test run failed"; exit 1; }
for t in \
  TestDetect_SameDayReimport_LucidPair \
  TestDetect_SameDayReimport_WindowExceeded \
  TestDetect_SameDayReimport_DifferentOriginalDescription \
  TestDetect_SameDayReimport_EmptyOriginalNotPaired \
  TestLoadCSVFile_OriginalDescription; do
  echo "$out" | grep -q -- "--- PASS: $t" || { echo "FAIL: missing or failing $t"; exit 1; }
done

echo "== invariants preserved =="
go test -count=1 -run 'TestLoadData_TransferIsNotANearDuplicateCandidate|TestDetect_PairKeyIsOrderIndependent|TestDetect_Idempotency|TestLoadData_DedupUnchangedAcrossOverlappingExports' \
  ./internal/services/dataloader/ | grep -q ok || { echo "FAIL: invariant tests"; exit 1; }

echo "== every consumer: full suite =="
go test -count=1 ./...

echo "ORACLE PASS"
