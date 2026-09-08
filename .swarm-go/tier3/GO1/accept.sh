#!/usr/bin/env bash
set -euo pipefail
oracle_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=${1:-$(cd "$oracle_dir/../../.." && pwd)}
overlay=$(mktemp -d /tmp/go1-overlay.XXXXXX)
trap 'rm -rf "$overlay"' EXIT
python3 - "$repo" "$oracle_dir" "$overlay" <<'PY'
import json,sys,pathlib
repo,oracle,tmp=map(pathlib.Path,sys.argv[1:])
(tmp/'overlay.json').write_text(json.dumps({'Replace': {str(repo/'internal/services/retirement/analysis/go1_acceptance_test.go'):str(oracle/'acceptance_test.go')}}))
PY
cd "$repo"
go test -count=1 -overlay "$overlay/overlay.json" ./internal/services/retirement/analysis -run '^TestGO1' -v
echo 'ORACLE PASS'
