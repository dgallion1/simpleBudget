#!/usr/bin/env bash
# Tier-3 acceptance oracle for R1. Run from a worktree root.
# Exit 0 = all checks pass. Every line of output is evidence.
set -u
ORACLE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/oracle/zz_oracle_r1_test.go"
DEST="internal/services/accounts/zz_oracle_r1_test.go"
rc=0
step() { echo; echo "### $*"; }

step "1. build"
go build ./... || rc=1

step "2. vet"
go vet ./... || rc=1

step "3. API surface — accounts.Mutate must exist with the pinned signature"
if ! grep -rn 'func Mutate(s \*storage\.Storage, fn func(\[\]models\.Account) (\[\]models\.Account, error)) error' \
     internal/services/accounts/; then
  echo "MISSING: accounts.Mutate with the pinned signature"; rc=1
fi

step "4. no unserialized load-modify-save left at the call sites"
echo "-- accounts.Load in handlers/MCP (each hit must be inside a Mutate fn or a read-only path):"
grep -rn 'accounts\.Load(\|\.LoadAccounts()' internal/handlers/accounts/ internal/services/mcpsvc/ledger/ || true
echo "-- direct Save calls outside the accounts service (expect none):"
if grep -rn 'accounts\.Save(\|\.SaveAccounts(' internal/handlers/accounts/ internal/services/mcpsvc/ledger/ | grep -v '_test.go'; then
  echo "FOUND: a save outside the serialized API"; rc=1
else
  echo "none — ok"
fi

step "5. lead-authored oracle tests under -race"
cp "$ORACLE" "$DEST" || { echo "could not stage the oracle"; exit 1; }
go test -race -count=1 -run 'TestZZOracleR1' ./internal/services/accounts/ || rc=1
rm -f "$DEST"

step "6. full suite under -race (no regressions)"
go test -race -count=1 ./... || rc=1

echo; echo "=== accept.sh exit: $rc ==="
exit $rc
