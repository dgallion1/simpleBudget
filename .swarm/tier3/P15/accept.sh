#!/usr/bin/env bash
# accept.sh — Tier 3 acceptance oracle for P15 (folder import + source delete).
#
# Run by swarm/tier3-compare.sh inside each blind worktree, with cwd set to
# that worktree. Its combined output and exit code are diffed between the two
# implementations, so EVERY line printed here must be deterministic:
#   - no timings, no ports, no temp paths, no file sizes, no ordering by mtime
#   - no `go test` package output (it carries durations and cached markers)
# Only `CHECK n <name>: PASS|FAIL` lines and a final SUMMARY are emitted.
#
# The oracle asserts on FILESYSTEM EFFECTS and HTTP STATUS CODES, never on
# response body structure. Two independent implementations will render their
# per-file outcome lists differently, and that difference is not a behavioral
# divergence. What both must agree on is which files exist afterward.
#
# Exit 0 iff every check passes.

set -u

PORT=18942
BASE="http://127.0.0.1:$PORT"
pass_count=0
fail_count=0

say() {           # say <n> <name> <PASS|FAIL>
  printf 'CHECK %s %s: %s\n' "$1" "$2" "$3"
  if [ "$3" = PASS ]; then pass_count=$((pass_count + 1)); else fail_count=$((fail_count + 1)); fi
}

TMP=$(mktemp -d)
DATA="$TMP/data"
IMPORT="$TMP/import"
OUTSIDE="$TMP/outside"
SRV_PID=""

cleanup() {
  if [ -n "$SRV_PID" ]; then kill "$SRV_PID" 2>/dev/null || true; wait "$SRV_PID" 2>/dev/null || true; fi
  chmod -R u+w "$TMP" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$DATA" "$IMPORT" "$OUTSIDE" "$IMPORT/subdir"

# Deterministic fixture content. Dates are fixed literals — never `date`.
csv() { printf 'Date,Description,Amount\n2026-03-01,COFFEE SHOP,-4.50\n2026-03-02,GROCERY,-52.10\n'; }

csv > "$IMPORT/alpha.csv"
csv > "$IMPORT/beta.csv"
csv > "$IMPORT/gamma.csv"
csv > "$IMPORT/delta.csv"
csv > "$IMPORT/collide.csv"
csv > "$IMPORT/subdir/nested.csv"
printf 'not a csv\n' > "$IMPORT/notes.txt"
csv > "$OUTSIDE/outside.csv"
# Pre-existing file in the data dir to force a name collision.
csv > "$DATA/collide.csv"

# ---------------------------------------------------------------------------
# Check 1 — the tree builds.
# ---------------------------------------------------------------------------
if go build -o "$TMP/budget2" ./cmd/server >/dev/null 2>&1; then
  say 1 build PASS
else
  say 1 build FAIL
  printf 'SUMMARY: %s passed, %s failed\n' "$pass_count" "$fail_count"
  exit 1
fi

# ---------------------------------------------------------------------------
# Check 2 — package unit tests are green. Only the exit code is observed;
# go test's own output carries durations and would diverge spuriously.
# ---------------------------------------------------------------------------
if go test ./internal/handlers/explorer/... ./internal/config/... >/dev/null 2>&1; then
  say 2 unit-tests PASS
else
  say 2 unit-tests FAIL
fi

# ---------------------------------------------------------------------------
# Boot the server against the temp dirs.
# ---------------------------------------------------------------------------
BUDGET_LISTEN_ADDR=":$PORT" \
BUDGET_DATA_DIR="$DATA" \
BUDGET2_IMPORT_DIR="$IMPORT" \
"$TMP/budget2" >"$TMP/server.log" 2>&1 &
SRV_PID=$!

ready=0
i=0
while [ "$i" -lt 100 ]; do
  if curl -fsS -o /dev/null -m 2 "$BASE/api/health" 2>/dev/null; then ready=1; break; fi
  if ! kill -0 "$SRV_PID" 2>/dev/null; then break; fi
  sleep 0.2
  i=$((i + 1))
done

if [ "$ready" = 1 ]; then
  say 3 server-boots PASS
else
  say 3 server-boots FAIL
  printf 'SUMMARY: %s passed, %s failed\n' "$pass_count" "$fail_count"
  exit 1
fi

status_of() {  # status_of <curl args...> -> prints HTTP status
  curl -s -o /dev/null -w '%{http_code}' -m 10 "$@"
}
body_of() {
  curl -s -m 10 "$@"
}

# ---------------------------------------------------------------------------
# Check 4 — scan lists CSVs directly in the import dir, and nothing else.
# Asserted by substring presence, which is format-agnostic.
# ---------------------------------------------------------------------------
scan=$(body_of "$BASE/explorer/import/scan")
scan_ok=1
for want in alpha.csv beta.csv gamma.csv delta.csv; do
  case "$scan" in *"$want"*) : ;; *) scan_ok=0 ;; esac
done
# notes.txt is not a CSV; nested.csv sits in a subdirectory (no recursion).
case "$scan" in *notes.txt*) scan_ok=0 ;; esac
case "$scan" in *nested.csv*) scan_ok=0 ;; esac
[ "$scan_ok" = 1 ] && say 4 scan-lists-only-direct-csvs PASS || say 4 scan-lists-only-direct-csvs FAIL

# ---------------------------------------------------------------------------
# Check 5 — import WITHOUT delete_source: file lands, source survives.
# ---------------------------------------------------------------------------
st=$(status_of -X POST --data 'name=alpha.csv' "$BASE/explorer/import")
if [ "$st" = 200 ] && [ -f "$DATA/alpha.csv" ] && [ -f "$IMPORT/alpha.csv" ]; then
  say 5 import-keeps-source PASS
else
  say 5 import-keeps-source FAIL
fi

# ---------------------------------------------------------------------------
# Check 6 — import WITH delete_source: file lands, source is gone.
# ---------------------------------------------------------------------------
st=$(status_of -X POST --data 'name=beta.csv&delete_source=true' "$BASE/explorer/import")
if [ "$st" = 200 ] && [ -f "$DATA/beta.csv" ] && [ ! -e "$IMPORT/beta.csv" ]; then
  say 6 import-deletes-source PASS
else
  say 6 import-deletes-source FAIL
fi

# ---------------------------------------------------------------------------
# Check 7 — collision skips AND the source is NOT deleted. This is the
# central safety property: a file that was not imported must never be
# removed from the user's folder.
# ---------------------------------------------------------------------------
before=$(cat "$DATA/collide.csv")
st=$(status_of -X POST --data 'name=collide.csv&delete_source=true' "$BASE/explorer/import")
after=$(cat "$DATA/collide.csv")
if [ "$st" = 200 ] && [ -f "$IMPORT/collide.csv" ] && [ "$before" = "$after" ]; then
  say 7 collision-skips-and-keeps-source PASS
else
  say 7 collision-skips-and-keeps-source FAIL
fi

# ---------------------------------------------------------------------------
# Check 8 — a name with a path separator is rejected and nothing outside the
# import dir is read or deleted.
# ---------------------------------------------------------------------------
# The `$st != 404` clause matters: without it this check passes vacuously on a
# tree where the endpoint was never implemented. Every safety check below
# carries the same guard.
st=$(status_of -X POST --data 'name=../outside/outside.csv&delete_source=true' "$BASE/explorer/import")
if [ "$st" != 404 ] && [ "$st" != 500 ] && [ -f "$OUTSIDE/outside.csv" ] && [ ! -e "$DATA/outside.csv" ]; then
  say 8 traversal-name-rejected PASS
else
  say 8 traversal-name-rejected FAIL
fi

# ---------------------------------------------------------------------------
# Check 9 — a non-CSV cannot be imported even when named explicitly, and its
# source is not deleted.
# ---------------------------------------------------------------------------
st=$(status_of -X POST --data 'name=notes.txt&delete_source=true' "$BASE/explorer/import")
if [ "$st" != 404 ] && [ "$st" != 500 ] && [ -f "$IMPORT/notes.txt" ] && [ ! -e "$DATA/notes.txt" ]; then
  say 9 non-csv-not-imported PASS
else
  say 9 non-csv-not-imported FAIL
fi

# ---------------------------------------------------------------------------
# Check 10 — a file outside the import dir reached via symlink is not
# followed, and the symlink target survives.
# ---------------------------------------------------------------------------
ln -sf "$OUTSIDE/outside.csv" "$IMPORT/link.csv" 2>/dev/null
st=$(status_of -X POST --data 'name=link.csv&delete_source=true' "$BASE/explorer/import")
if [ "$st" != 404 ] && [ -f "$OUTSIDE/outside.csv" ]; then
  say 10 symlink-target-survives PASS
else
  say 10 symlink-target-survives FAIL
fi

# ---------------------------------------------------------------------------
# Check 11 — when the write cannot succeed, the source MUST survive. This is
# what the readback guard exists for: a failed save must never clear the way
# for a delete. Enforced by making the data dir unwritable.
# ---------------------------------------------------------------------------
chmod a-w "$DATA"
st=$(status_of -X POST --data 'name=gamma.csv&delete_source=true' "$BASE/explorer/import")
chmod u+w "$DATA"
if [ "$st" != 404 ] && [ -f "$IMPORT/gamma.csv" ] && [ ! -e "$DATA/gamma.csv" ]; then
  say 11 failed-write-keeps-source PASS
else
  say 11 failed-write-keeps-source FAIL
fi

# ---------------------------------------------------------------------------
# Check 12 — malformed request (no name fields) is a 400 and changes nothing.
# ---------------------------------------------------------------------------
snapshot_before=$(ls "$IMPORT" | sort | tr '\n' ' ')
st=$(status_of -X POST --data 'delete_source=true' "$BASE/explorer/import")
snapshot_after=$(ls "$IMPORT" | sort | tr '\n' ' ')
if [ "$st" = 400 ] && [ "$snapshot_before" = "$snapshot_after" ]; then
  say 12 empty-batch-is-400-and-inert PASS
else
  say 12 empty-batch-is-400-and-inert FAIL
fi

# ---------------------------------------------------------------------------
# Check 13 — a multi-file batch with delete_source deletes exactly the files
# that were imported, and no others.
# ---------------------------------------------------------------------------
st=$(status_of -X POST --data 'name=delta.csv&name=collide.csv&delete_source=true' "$BASE/explorer/import")
# delta imports and its source goes; collide still collides, so its source stays.
if [ "$st" = 200 ] && [ -f "$DATA/delta.csv" ] && [ ! -e "$IMPORT/delta.csv" ] \
   && [ -f "$IMPORT/collide.csv" ]; then
  say 13 mixed-batch-deletes-only-imported PASS
else
  say 13 mixed-batch-deletes-only-imported FAIL
fi

printf 'SUMMARY: %s passed, %s failed\n' "$pass_count" "$fail_count"
[ "$fail_count" -eq 0 ] || exit 1
exit 0
