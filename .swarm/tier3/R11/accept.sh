#!/usr/bin/env bash
# Tier-3 acceptance oracle for R11. Run from a worktree root.
# Exit 0 = all checks pass. Every line of output is evidence.
set -u
ORACLE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/oracle/zz_oracle_r11_test.go"
DEST="internal/services/storage/zz_oracle_r11_test.go"
rc=0
step() { echo; echo "### $*"; }

step "1. build"
go build ./... || rc=1

step "2. vet"
go vet ./... || rc=1

step "3. atomicWrite must not stage at a name derived only from the destination"
if grep -n 'tmpPath := path + "\.tmp"' internal/services/storage/storage.go; then
  echo "FOUND: atomicWrite still stages at a fixed path+\".tmp\""; rc=1
else
  echo "no fixed staging name in storage.go — ok"
fi

step "4. dayOf and the balance API must be untouched (blast-radius guard)"
if ! git diff --quiet HEAD -- internal/services/accounts/balance.go; then
  echo "MODIFIED: internal/services/accounts/balance.go is out of scope for R11"; rc=1
else
  echo "balance.go unchanged — ok"
fi

step "5. lead-authored oracle tests under -race"
cp "$ORACLE" "$DEST" || { echo "could not stage the oracle"; exit 1; }
go test -race -count=1 -run 'TestZZOracleR11' ./internal/services/storage/ || rc=1
rm -f "$DEST"

step "6. the storage package's own suite under -race"
go test -race -count=1 ./internal/services/storage/... || rc=1

step "7. full suite under -race (no regressions)"
go test -race -count=1 ./... || rc=1

echo; echo "=== accept.sh exit: $rc ==="
exit $rc
