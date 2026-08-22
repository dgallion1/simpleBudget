#!/usr/bin/env bash
# Tier-3 acceptance oracle for R12. Run from a worktree root.
# Exit 0 = all checks pass. Every line of output is evidence.
set -u
ORACLE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/oracle/zz_oracle_r12_test.go"
DEST="internal/services/accounts/zz_oracle_r12_test.go"
rc=0
step() { echo; echo "### $*"; }

step "1. build"
go build ./... || rc=1

step "2. vet"
go vet ./... || rc=1

step "3. byDay must not be keyed by a raw time.Time"
if grep -n 'map\[time\.Time\]' internal/services/accounts/projection.go; then
  echo "FOUND: byDay still keyed by time.Time (compares the *Location pointer)"; rc=1
else
  echo "no raw time.Time map key — ok"
fi

step "4. blast-radius guard: dayOf, BalanceAt, Freshness must keep their contracts"
if ! git diff --quiet HEAD -- internal/services/accounts/balance.go; then
  echo "NOTE: balance.go modified — allowed only if dayOf/BalanceAt/Freshness signatures are unchanged:"
  git diff HEAD -- internal/services/accounts/balance.go | grep -E '^[-+]func ' && { echo "SIGNATURE CHANGED"; rc=1; } || echo "  no signature changes — ok"
else
  echo "balance.go unchanged — ok"
fi

step "5. lead-authored oracle tests under -race, in five timezones"
cp "$ORACLE" "$DEST" || { echo "could not stage the oracle"; exit 1; }
for tz in UTC America/New_York Asia/Tokyo Pacific/Midway Pacific/Kiritimati; do
  echo "-- TZ=$tz"
  TZ="$tz" go test -race -count=1 -run 'TestZZOracleR12' ./internal/services/accounts/ || rc=1
done
rm -f "$DEST"

step "6. the accounts package's own suite under -race, in three timezones"
for tz in UTC America/New_York Asia/Tokyo; do
  echo "-- TZ=$tz"
  TZ="$tz" go test -race -count=1 ./internal/services/accounts/... || rc=1
done

step "7. accepted downstream behaviour must survive (R3, R5, dashboard/MCP parity)"
for tz in UTC Asia/Tokyo; do
  echo "-- TZ=$tz"
  TZ="$tz" go test -race -count=1 ./internal/services/mcpsvc/ledger/... ./internal/handlers/dashboard/... || rc=1
done

step "8. full suite under -race (no regressions)"
go test -race -count=1 ./... || rc=1

echo; echo "=== accept.sh exit: $rc ==="
exit $rc
