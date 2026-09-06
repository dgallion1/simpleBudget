#!/usr/bin/env bash
# Tier-3 oracle for U5 — zero-baseline change display, contract v3 (SPEC §2d).
# Run with cwd = the budget2 tree under test. Plants two lead-authored tests,
# runs them plus the package/MCP/full suites, removes the plants, and prints
# ORACLE PASS as the final line ONLY when every check passed.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
B=internal/templates/zz_oracle_u5_behavior_test.go
C=internal/services/insights/zz_oracle_u5_contract_test.go
LOG="$(mktemp)"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "0" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1)); else echo "CHECK $1: FAIL (exit $2)"; tail -n 40 "$LOG" | sed 's/^/    /'; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$B" "$C" "$LOG"; }
trap cleanup EXIT
go build ./... >"$LOG" 2>&1;                                   ck "1 go build ./..." $?
go vet ./... >"$LOG" 2>&1;                                     ck "2 go vet ./... (before planting the oracle tests)" $?
cp "$HERE/oracle_behavior_test.go.src" "$B"
cp "$HERE/oracle_contract_test.go.src" "$C"
go test -count=1 ./internal/services/insights/ -run 'TestOracleU5_ContractAPI' >"$LOG" 2>&1
                                                               ck "3 contract API: ChangeDisplay(prev,cur), kinds incl. none, Direction owned by the cell" $?
go test -count=1 ./internal/templates/ -run 'TestOracleU5_NamedFixtures_CategoryTrends' >"$LOG" 2>&1
                                                               ck "4 rendered named fixtures at both sites (dollar sum, new, percent, floor boundary, rounding divergence)" $?
go test -count=1 ./internal/templates/ -run 'TestOracleU5_MajorExpense_FloatNoiseAndSigned' >"$LOG" 2>&1
                                                               ck "5 MajorExpenseTrends: float-noise \$0→\$0 renders '—'/stable; signed rows; struct fields agree with cell" $?
go test -count=1 ./internal/templates/ -run 'TestOracleU5_Property_RenderedSelfConsistency' >"$LOG" 2>&1
                                                               ck "6 property sweep: ≥2000 rendered rows self-consistent (sum, percent-vs-dollars, arrow)" $?
go test -count=1 ./internal/services/insights/ ./internal/templates/ >"$LOG" 2>&1
                                                               ck "7 worker's own insights + templates suites green alongside the oracle" $?
go test -count=1 ./internal/services/mcpsvc/spend/ >"$LOG" 2>&1;  ck "8 MCP spend suite green (change_percent/direction from rounded totals)" $?
F=internal/services/mcpsvc/spend/trends.go
grep -q '\\"new\\"' "$F" && grep -qi 'no change' "$F" && grep -q 'positive' "$F" && grep -q 'negative' "$F" && grep -qE 'previous_amount.{0,80}(under|below|less than) \$100|\$100' "$F"
                                                               ck "9 get_trends description names all four UI cases (new / no-change / dollar delta / percent) and the sign conditions" $?
rm -f "$B" "$C"
go test -count=1 ./... >"$LOG" 2>&1;                           ck "10 full suite green (bare)" $?
staticcheck ./... >"$LOG" 2>&1;                                ck "11 staticcheck ./..." $?

echo "SUMMARY: $PASSN passed, $FAILN failed"
if [[ "$FAILN" == 0 ]]; then echo "ORACLE PASS"; else echo "ORACLE FAIL"; exit 1; fi
