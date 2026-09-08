#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=${1:-$(cd "$here/../../.." && pwd)}
# Capability absence is a deliberate behavioral-contract failure, not a Go
# compilation error masquerading as a calibrated defect.
if ! grep -Rq 'func OptimizeGuardrails(' "$root/internal/services/retirement/analysis"; then
  echo 'ORACLE FAIL: optimizer capability absent'
  exit 1
fi
overlay=$(mktemp "$here/overlay.XXXXXX.json")
trap 'rm -f "$overlay"' EXIT
python3 - "$root" "$here" "$overlay" <<'PY'
import json,sys
root,here,out=sys.argv[1:]
with open(out,'w') as f:
    json.dump({'Replace': {root+'/internal/services/retirement/analysis/go2_acceptance_external_test.go':here+'/oracle_test.go'}},f)
PY
cd "$root"
go test -count=1 -timeout=120s -overlay "$overlay" ./internal/services/retirement/analysis -run '^TestGO2Oracle' -v
echo 'ORACLE PASS'
