#!/usr/bin/env bash
set -euo pipefail
oracle_dir="$(cd "$(dirname "$0")" && pwd)"
root="$1"
node "$oracle_dir/revision.cjs" "$root"
cd "$root"
probe=internal/handlers/whatif/ra1_oracle_revision_test.go
test ! -e "$probe"
cp "$oracle_dir/revision_test.go" "$probe"
trap 'rm -f "$probe"' EXIT
go test -count=1 ./internal/models ./internal/handlers/whatif ./internal/services/retirement/... -run 'Test.*(Roth|OptimizerCandidate|RA1|Clone|RebaseSaved|FixedRoth)'
echo "ORACLE PASS"
