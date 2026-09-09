#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=${1:-$(cd "$here/../../.." && pwd)}
if ! rg -q 'func SummarizeGuardrailSimulationYears\(' "$root/internal/services/retirement/analysis"; then
  echo 'ORACLE FAIL: simulated annual bands capability absent'
  exit 1
fi
overlay=$(mktemp)
trap 'rm -f "$overlay"' EXIT
python3 - "$root" "$here" "$overlay" <<'PY'
import json,sys
root,here,out=sys.argv[1:]
with open(out,'w') as f:
 json.dump({'Replace':{root+'/internal/services/retirement/analysis/go7_acceptance_external_test.go':here+'/oracle_test.go'}},f)
PY
cd "$root"
go test -count=1 -timeout=180s -overlay "$overlay" ./internal/services/retirement/analysis -run '^TestGO7Oracle' -v
go test ./internal/services/retirement/analysis ./internal/services/retirement/engine ./internal/models
echo 'ORACLE PASS'
