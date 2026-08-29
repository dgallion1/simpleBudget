#!/usr/bin/env bash
# T18 oracle — planned one-time expenses in the what-if engine.
set -u
cd "$(dirname "$0")/../../.."
FAIL=0
echo "== build =="
go build ./... || FAIL=1
echo "== oracle probes (oracle-owned, compile-fails on featureless tree) =="
cp .swarm/tier3/T18/oracle_test.go internal/services/mcpsvc/plan/zz_t18_oracle_test.go
go test -count=1 -run 'TestT18Oracle' ./internal/services/mcpsvc/plan/ || FAIL=1
rm -f internal/services/mcpsvc/plan/zz_t18_oracle_test.go
echo "== worker-named tests present and passing =="
OUT=$(go test -count=1 -run 'TestOneTimeExpense' ./... 2>&1)
echo "$OUT" | grep -q -- "--- FAIL" && FAIL=1
echo "$OUT" | grep -qE "^ok .*(models|retirement|whatif|plan)" || { echo "no TestOneTimeExpense tests found in expected packages"; FAIL=1; }
echo "== every consumer: full suite =="
go test ./... >/dev/null || FAIL=1
if [ "$FAIL" -eq 0 ]; then echo "ORACLE PASS"; else echo "ORACLE FAIL"; fi
exit $FAIL
