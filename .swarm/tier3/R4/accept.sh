#!/usr/bin/env bash
# Tier-3 acceptance oracle for R4. Run from a worktree root.
# Exit 0 = all checks pass. Every line of output is evidence.
set -u
ORACLE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/oracle/zz_oracle_r4_test.go"
DEST="internal/services/mcpsvc/confirm/zz_oracle_r4_test.go"
rc=0
step() { echo; echo "### $*"; }

step "1. build"
go build ./... || rc=1

step "2. vet"
go vet ./... || rc=1

step "3. API surface — Create/Find must carry an operation id"
if ! grep -rn 'func (a \*Approvals) Create(tool, subject, opID, title, detail string)' \
     internal/services/mcpsvc/confirm/; then
  echo "MISSING: Approvals.Create with the pinned signature"; rc=1
fi
if ! grep -rn 'func (a \*Approvals) Find(tool, subject, opID string)' \
     internal/services/mcpsvc/confirm/; then
  echo "MISSING: Approvals.Find with the pinned signature"; rc=1
fi

step "4. all four guarded call sites pass an operation id"
echo "-- askForApproval / awaitApproval call sites:"
grep -rn 'askForApproval(\|awaitApproval(' internal/services/mcpsvc/ledger/ internal/services/mcpsvc/admin/ \
  | grep -v 'func askForApproval\|func awaitApproval' || true
missing=0
for f in internal/services/mcpsvc/ledger/anchor.go \
         internal/services/mcpsvc/ledger/resolve.go \
         internal/services/mcpsvc/admin/restore.go \
         internal/services/mcpsvc/admin/shutdown.go; do
  if ! grep -q 'token' <<<"$(grep -A3 'askForApproval(deps' "$f")"; then
    echo "SITE NOT BOUND: $f does not pass the token to askForApproval"; missing=1
  fi
  if ! grep -q 'token' <<<"$(grep -A1 'awaitApproval(ctx' "$f")"; then
    echo "SITE NOT BOUND: $f does not pass the token to awaitApproval"; missing=1
  fi
done
(( missing )) && rc=1
(( missing )) || echo "all four sites bound — ok"

step "5. lead-authored oracle tests under -race"
cp "$ORACLE" "$DEST" || { echo "could not stage the oracle"; exit 1; }
go test -race -count=1 -run 'TestZZOracleR4' ./internal/services/mcpsvc/confirm/ || rc=1
rm -f "$DEST"

step "6. full suite under -race (no regressions)"
go test -race -count=1 ./... || rc=1

echo; echo "=== accept.sh exit: $rc ==="
exit $rc
