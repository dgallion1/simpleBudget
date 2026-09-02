#!/usr/bin/env bash
# CB1 oracle — refund-dominant months in the CombinedCumulativeBalance walk.
# Runs from the repo root of the tree under test. Emits "ORACLE PASS" as the
# final line ONLY when every check passes.
set -u
cd "$(dirname "$0")/../../.." || { echo "ORACLE FAIL: cannot locate repo root"; exit 1; }

PROBE_SRC=.swarm/tier3/CB1/cb1_oracle_test.go
PROBE_DST=internal/services/metrics/zz_cb1_oracle_test.go
fail() { rm -f "$PROBE_DST"; echo "ORACLE FAIL: $1"; exit 1; }

[ -f "$PROBE_SRC" ] || fail "probe source missing"
cp "$PROBE_SRC" "$PROBE_DST" || fail "cannot install probe"

# Check A: the refund-dominant fixture — discriminator + documented invariant.
go test -count=1 -run 'TestCB1Oracle' ./internal/services/metrics/ || fail "refund-dominant fixture (probe) failed"

# Check B: every existing consumer of the walk stays green — the metrics
# package itself and the dashboard chart walk (its chart-vs-metrics equality
# test must hold AFTER the fix, i.e. both surfaces moved together).
go test -count=1 ./internal/services/metrics/ || fail "metrics package suite failed"
go test -count=1 ./internal/handlers/dashboard/ || fail "dashboard handlers suite (chart walk equality) failed"

# Check C: the pre-existing invariant test still passes by its exact name —
# a fix that deleted or renamed the guard instead of satisfying it fails here.
go test -count=1 -run 'TestCalculateMetrics_CombinedCumulativeBalance_LastIsNegationOfCumulativeDelta' -v ./internal/services/metrics/ 2>&1 | grep -q -- '--- PASS: TestCalculateMetrics_CombinedCumulativeBalance_LastIsNegationOfCumulativeDelta' || fail "pre-existing invariant test missing or failing"

rm -f "$PROBE_DST"
echo "ORACLE PASS"
