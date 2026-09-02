#!/usr/bin/env bash
# CB3 oracle — per-transaction/per-period abs class (drilldown modal,
# merchant chart, cash-flow direction, insights aggregates, PercentChange
# pin). Final line "ORACLE PASS" only on the all-pass path.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL: cannot locate repo root"; exit 1; }

PROBE_SRC=.swarm/tier3/CB3/cb3_oracle_test.go
PROBE_DST=internal/handlers/dashboard/zz_cb3_oracle_test.go
fail() { rm -f "$PROBE_DST"; echo "ORACLE FAIL: $1"; exit 1; }

[ -f "$PROBE_SRC" ] || fail "probe source missing"
cp "$PROBE_SRC" "$PROBE_DST" || fail "cannot install probe"

# Check A: the five contract probes.
go test -count=1 -run 'TestCB3Oracle' ./internal/handlers/dashboard/ || fail "CB3 contract probes failed"

# Check B: CB1+CB2 non-regression by exact name.
go test -count=1 -run 'TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit' -v ./internal/services/metrics/ 2>&1 | grep -q -- '--- PASS: TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit' || fail "CB1 regression test missing or failing"
go test -count=1 -run 'CB2' ./internal/services/metrics/ ./internal/handlers/dashboard/ || fail "CB2 test family failing"

# Check C: full suites of every touched package + MCP consumer.
go test -count=1 ./internal/services/metrics/ || fail "metrics suite failed"
go test -count=1 ./internal/handlers/dashboard/ || fail "dashboard suite failed"
go test -count=1 ./internal/services/insights/ || fail "insights suite failed"
go test -count=1 ./internal/services/mcpsvc/spend/ || fail "mcpsvc/spend (get_trends consumer) suite failed"

rm -f "$PROBE_DST"
echo "ORACLE PASS"
