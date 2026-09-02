#!/usr/bin/env bash
# CB2 oracle — the seven sibling per-month abs sites (trend series, bar
# traces, KPI monthly stats, CSV export) go signed. Final line "ORACLE PASS"
# only on the all-pass path.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL: cannot locate repo root"; exit 1; }

PROBE_SRC=.swarm/tier3/CB2/cb2_oracle_test.go
PROBE_DST=internal/handlers/dashboard/zz_cb2_oracle_test.go
fail() { rm -f "$PROBE_DST"; echo "ORACLE FAIL: $1"; exit 1; }

[ -f "$PROBE_SRC" ] || fail "probe source missing"
cp "$PROBE_SRC" "$PROBE_DST" || fail "cannot install probe"

# Check A: all three surface groups on refund-dominant fixtures.
go test -count=1 -run 'TestCB2Oracle' ./internal/handlers/dashboard/ || fail "refund-dominant probe failed"

# Check B: CB1 non-regression by exact name — the cumulative walk and its
# cross-surface equality binding must be untouched.
go test -count=1 -run 'TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit' -v ./internal/services/metrics/ 2>&1 | grep -q -- '--- PASS: TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit' || fail "CB1 regression test missing or failing"
go test -count=1 -run 'TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance' -v ./internal/handlers/dashboard/ 2>&1 | grep -q -- '--- PASS: TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance' || fail "CB1 equality test missing or failing"

# Check C: full consumer suites.
go test -count=1 ./internal/services/metrics/ || fail "metrics package suite failed"
go test -count=1 ./internal/handlers/dashboard/ || fail "dashboard handlers suite failed"

rm -f "$PROBE_DST"
echo "ORACLE PASS"
