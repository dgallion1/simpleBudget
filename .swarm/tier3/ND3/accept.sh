#!/usr/bin/env bash
# ND3 oracle — executable acceptance checks for the pendingPostedWindowDays
# 3→5 bump (task ND3, tier 3 after critical-glob escalation). Emits
# "ORACLE PASS" as the final line only when every check passes.
#
# Consumers of the window fact, each asserted on observable output:
#   C1  the detector itself (dataloader unit tests, incl. the three window pins)
#   C2  live data through the full loader path (staged probe: exactly the one
#       genuine 4-day Amazon pair appears, all bindable decisions still bind)
#   C3  the list_duplicates MCP tool description (the surface attempt 1 missed)
#   C4  the near_duplicates.go doc comments
#   C5  the .swarm/NEXT.md record (must not conflate recorded with bound)
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL"; exit 1; }

pass=1
fail() { echo "ND3 FAIL: $*"; pass=0; }

echo "== A: constant is 5 =="
grep -Eq '^\s*pendingPostedWindowDays = 5$' internal/services/dataloader/near_duplicates.go \
  || fail "pendingPostedWindowDays is not 5"

echo "== B (C1): window pin tests exist and the dataloader+mcpsvc suites pass =="
out=$(go test -count=1 -run 'TestDetect_PendingPosted_(FourDayLag_AmazonVerbatim|WindowBoundaryFiveDays|WindowExceeded)' -v ./internal/services/dataloader/ 2>&1)
echo "$out" | grep -q -- '--- PASS: TestDetect_PendingPosted_FourDayLag_AmazonVerbatim' || fail "4-day Amazon test missing or failing"
echo "$out" | grep -q -- '--- PASS: TestDetect_PendingPosted_WindowBoundaryFiveDays'    || fail "5-day boundary test missing or failing"
echo "$out" | grep -q -- '--- PASS: TestDetect_PendingPosted_WindowExceeded'            || fail "6-day negative test missing or failing"
go test -count=1 ./internal/services/dataloader/ ./internal/services/mcpsvc/... || fail "package suites"

echo "== C (C2): live-data probe — one new genuine pair, zero junk, decisions bind =="
SRC=".swarm/work/ND3/zz_probe_nd3_test.go"
DEST="internal/services/dataloader/zz_probe_nd3_test.go"
if cp "$SRC" "$DEST"; then
  go test -count=1 -run TestProbeND3 -v ./internal/services/dataloader/ || fail "live-data probe"
  rm -f "$DEST"
else
  fail "cannot stage probe"
fi

echo "== D (C3): list_duplicates description states 5 days for the pending→posted shape =="
SQ=$(tr -s ' \t\n"+' ' ' < internal/services/mcpsvc/admin/duplicates.go)
echo "$SQ" | grep -q 'rewritten description within 5 days' \
  || fail "list_duplicates description does not say 'within 5 days' for the rewritten-description shape"
echo "$SQ" | grep -q 'rewritten description within 3 days' \
  && fail "stale 'within 3 days' still present in list_duplicates description"

echo "== E (C4): no stale 3-day wording on the detector's own comments =="
grep -n '3 days' internal/services/dataloader/near_duplicates.go \
  && fail "stale '3 days' text in near_duplicates.go"

echo "== F (C5): NEXT.md does not conflate recorded with bound =="
SQN=$(tr -s ' \t\n' ' ' < .swarm/NEXT.md)
echo "$SQN" | grep -q 'all 24 recorded decisions still bind' \
  && fail "NEXT.md still claims 'all 24 recorded decisions still bind' (30 are recorded; 24 bind)"
echo "$SQN" | grep -q 'already unbound at the old window' \
  || fail "NEXT.md ND3 bullet missing the six-pre-existing-unbound clarification"

if [[ "$pass" -eq 1 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
