#!/usr/bin/env bash
# accept.sh — Tier 3 acceptance oracle for A1 (StableID assignment and
# StableID-first sidecar resolution with legacy-hash fallback).
#
# Run by swarm/tier3-compare.sh inside each blind worktree, with cwd set to
# that worktree. Its combined output and exit code are diffed between the two
# implementations, so EVERY line printed here must be deterministic:
#   - no timings, no temp paths, no file sizes, no ordering by mtime
#   - no raw `go test` package output (it carries durations and cached markers)
# Only `CHECK n <name>: PASS|FAIL` lines and a final SUMMARY are emitted.
#
# The oracle asserts on BEHAVIOR through the API pinned in the task brief, via
# a lead-authored probe test copied in from the tracked location. It is
# deliberately independent of whatever tests each worker wrote for itself.
#
# Exit 0 iff every check passes.

set -u

pass_count=0
fail_count=0

say() {           # say <n> <name> <PASS|FAIL>
  echo "CHECK $1 $2: $3"
  if [ "$3" = PASS ]; then pass_count=$((pass_count + 1)); else fail_count=$((fail_count + 1)); fi
}

# The probe lives in the main tree's .swarm (tracked); each worktree has its
# own copy of .swarm at the same relative path because it is committed.
PROBE_SRC=".swarm/tier3/A1/probe_test.go"
PROBE_DST="internal/handlers/explorer/zz_probe_a2_test.go"

cleanup() { rm -f "$PROBE_DST"; }
trap cleanup EXIT

# --- 1. build ---------------------------------------------------------------
if go build ./... >/dev/null 2>&1; then
  say 1 build PASS
else
  say 1 build FAIL
  echo "SUMMARY: $pass_count passed, $fail_count failed"
  exit 1
fi

# --- 2. vet -----------------------------------------------------------------
if go vet ./... >/dev/null 2>&1; then
  say 2 vet PASS
else
  say 2 vet FAIL
fi

# --- 3. existing suite still green ------------------------------------------
# Exit code only; package output carries durations and cache markers.
if go test ./... >/dev/null 2>&1; then
  say 3 existing-tests PASS
else
  say 3 existing-tests FAIL
fi

# --- 4. probe compiles against the pinned API -------------------------------
if [ ! -f "$PROBE_SRC" ]; then
  say 4 probe-present FAIL
  echo "SUMMARY: $pass_count passed, $fail_count failed"
  exit 1
fi
cp "$PROBE_SRC" "$PROBE_DST"
if go vet ./internal/handlers/explorer/ >/dev/null 2>&1; then
  say 4 probe-compiles PASS
else
  say 4 probe-compiles FAIL
  # Without a compiling probe the behavioral checks cannot run; report them
  # as failures explicitly rather than silently skipping.
  say 5 stable-id-format FAIL
  say 6 occurrence-index-separates-collisions FAIL
  say 7 unassigned-uses-file-prefix FAIL
  say 8 stable-id-uses-post-flip-amount FAIL
  say 9 legacy-pin-still-resolves FAIL
  say 10 pin-rewrites-to-stable-id FAIL
  echo "SUMMARY: $pass_count passed, $fail_count failed"
  exit 1
fi

# --- 5..10. behavioral checks, one per probe test ---------------------------
probe() {         # probe <n> <check-name> <go-test-name>
  if go test -count=1 -run "^$3\$" ./internal/handlers/explorer/ >/dev/null 2>&1; then
    say "$1" "$2" PASS
  else
    say "$1" "$2" FAIL
  fi
}

probe 5  stable-id-format                         TestProbeA1_StableIDFormat
probe 6  occurrence-index-separates-collisions    TestProbeA1_OccurrenceIndexSeparatesCollisions
probe 7  unassigned-uses-file-prefix              TestProbeA1_UnassignedUsesFilePrefix
probe 8  stable-id-uses-post-flip-amount          TestProbeA1_StableIDUsesPostFlipAmount
probe 9  legacy-pin-still-resolves                TestProbeA1_LegacyPinStillResolves
probe 10 pin-rewrites-to-stable-id                TestProbeA1_PinRewritesToStableID

echo "SUMMARY: $pass_count passed, $fail_count failed"
[ "$fail_count" -eq 0 ]
