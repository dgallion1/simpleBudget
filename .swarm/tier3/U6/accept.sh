#!/usr/bin/env bash
# Tier-3 oracle for U6 — design tokens + type floor (SPEC §2a contract v2)
# + attempt-4 extension (ruling U-2026-09-04j): checks 9–10 cover colour
# classes emitted from Go/JS source and the POST/DELETE error-path banners.
# Run with cwd = the budget2 tree under test. Prints ORACLE PASS as the last
# line only when every check passed. Needs a scratch node_modules that has
# playwright + axe-core (searched under the agents2 run's .swarm/work).
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT=8198; BASE="http://localhost:$PORT"
LOG="$(mktemp)"; PASSN=0; FAILN=0
ck() { if [[ "$2" == "0" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1)); else echo "CHECK $1: FAIL"; grep -vE '^ok ' "$LOG" | tail -n 25 | sed 's/^/    /'; FAILN=$((FAILN+1)); fi; }
cleanup() { scripts/whatif-verify.sh stop $PORT >/dev/null 2>&1 || true; rm -f "$LOG"; }
trap cleanup EXIT
NM=""; for d in "$HERE"/../../work/*/*/node_modules "$HERE"/../../work/*/node_modules; do [[ -d "$d/playwright" && -f "$d/axe-core/axe.min.js" ]] && { NM="$(cd "$d" && pwd)"; break; }; done
[[ -n "$NM" ]]; ck "0 playwright+axe-core available ($NM)" $?

n=$(grep -rE 'text-\[1[01]px\]' web/templates | wc -l); [[ "$n" == 0 ]]; rc=$?; ck "1 zero text-[10px]/text-[11px] in templates (found $n)" $rc
fam=$(grep -rhoE '\b(text|bg|border|ring|from|to|via|divide|placeholder|stroke|fill|decoration|outline|shadow)-(indigo|red|amber|green|emerald|blue|rose|purple|orange|yellow|cyan|sky|teal|lime|pink|fuchsia|violet)-' web/templates | sed -E 's/.*-([a-z]+)-$/\1/' | sort -u | wc -l); [[ "$fam" -le 6 ]]; rc=$?; ck "2 hue families remaining in templates <= 6 (found $fam)" $rc
python3 "$HERE/typefloor_allowlist.py" >"$LOG" 2>&1; rc=$?; ck "3 type-floor allow-list: every surviving text-xs element is label-class (R1–R4)" $rc
grep -qE '^\s*--(accent|positive|negative|warning|neutral)(-soft|-strong)?:' web/static/css/styles.css && grep -q "positive" tailwind.config.js && grep -q "'body-sm'" tailwind.config.js; rc=$?; ck "4 tokens defined in styles.css and mapped in tailwind.config.js (incl. fontSize body-sm)" $rc
make css-verify >"$LOG" 2>&1; ck "5 make css-verify (committed tailwind.css matches templates)" $?
scripts/whatif-verify.sh start $PORT >"$LOG" 2>&1; ck "6 verify server up on :$PORT" $?
NODE_PATH="$NM" node "$HERE/render_probe.js" "$BASE" "$NM/axe-core/axe.min.js" >"$LOG" 2>&1; rc=$?; grep -E '^(OVERFLOW|CONTRAST|THEME|RENDER)' "$LOG" | head -n 40 | sed 's/^/    /'; ck "7 rendered: no horizontal overflow at 1440/1280 (details opened) and zero color-contrast violations, 9 pages x 2 themes" $rc
NODE_PATH="$NM" node "$HERE/error_probe.js" "$BASE" "$NM/axe-core/axe.min.js" >"$LOG" 2>&1; rc=$?; grep -E '^(STATUS|FRAGMENT|HUE|UNSTYLED|CONTRAST|ERROR PROBE)' "$LOG" | head -n 20 | sed 's/^/    /'; ck "10 error paths (accounts/whatif/major-expenses POST+DELETE) render a styled, token-only, contrast-clean banner in both themes" $rc
scripts/whatif-verify.sh stop $PORT >/dev/null 2>&1
python3 "$HERE/emitter_coverage.py" >"$LOG" 2>&1; rc=$?; grep -E '^(MISSING|EMITTER)' "$LOG" | head -n 20 | sed 's/^/    /'; ck "9 every colour class emitted from Go/JS source has a rule in the built tailwind.css" $rc
go build ./... >"$LOG" 2>&1 && go vet ./... >>"$LOG" 2>&1 && go test -count=1 ./... >>"$LOG" 2>&1 && staticcheck ./... >>"$LOG" 2>&1; ck "8 go build/vet/test/staticcheck green (bare)" $?
echo "SUMMARY: $PASSN passed, $FAILN failed"
if [[ "$FAILN" == 0 ]]; then echo "ORACLE PASS"; else echo "ORACLE FAIL"; exit 1; fi
