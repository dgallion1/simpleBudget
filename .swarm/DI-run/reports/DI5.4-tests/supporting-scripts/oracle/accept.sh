#!/usr/bin/env bash
set -euo pipefail
: "${DI5_SOURCE_ROOT:?isolated frozen source required}"
: "${DI5_BASE:?isolated synthetic server required}"
: "${DI5_OUTPUT:?oracle evidence directory required}"
oracle_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
case "$DI5_SOURCE_ROOT" in /tmp/*) ;; *) echo 'Refusing non-isolated source'; exit 2;; esac
node "$oracle_dir/focus-transitions.cjs"
node --test "$DI5_SOURCE_ROOT/web/static/js/page-refresh.test.cjs"
DI5_OUTPUT="$DI5_OUTPUT/obstruction" node "$DI5_SOURCE_ROOT/.swarm/DI-run/reports/DI5-focus-obstruction.cjs"
DI5_OUTPUT="$DI5_OUTPUT/layout" node "$DI5_SOURCE_ROOT/.swarm/DI-run/reports/DI5-refresh-layout.cjs"
DI5_OUTPUT="$DI5_OUTPUT/lifecycle" node "$DI5_SOURCE_ROOT/.swarm/DI-run/reports/DI5-refresh-browser.cjs"
echo 'ORACLE PASS'
