#!/usr/bin/env bash
# accept.sh — executable acceptance oracle for S5 attempt 3 (Tier 3).
#
# The contract: the client-side regression harness must be impossible to skip
# accidentally. A developer or CI invoking ANY Makefile target that runs
# `go test` must either run the harness or be told, loudly, that it cannot.
#
# Written before dispatch; neither arm may edit it. An arm that changes
# accept.sh fails by definition.
set -u

fails=0
pass() { echo "PASS  $1"; }
bad()  { echo "FAIL  $1"; shift; [[ $# -gt 0 ]] && printf '      %s\n' "$@"; fails=$((fails+1)); }

echo "== S5 oracle =="

# --- 1. the tree still builds and the suite is green -------------------------
for step in "go build ./..." "go vet ./..."; do
  if out=$($step 2>&1); then pass "$step"; else bad "$step" "$out"; fi
done
if out=$(go test -count=1 ./... 2>&1); then
  pass "go test ./..."
else
  bad "go test ./..." "$(tail -20 <<<"$out")"
fi

# --- 2. the harness actually RUNS here (node is present in this environment) --
if ! command -v node >/dev/null 2>&1; then
  bad "node present" "the oracle requires node to distinguish run from skip"
else
  out=$(go test -count=1 -v -run TestSyncWarnings_ClientRegressionHarness \
        ./internal/handlers/accounts/ 2>&1)
  if grep -q -- "--- SKIP" <<<"$out"; then
    bad "harness runs (not skipped) when node is present" "$(grep -- '--- SKIP' <<<"$out")"
  elif grep -q -- "--- PASS: TestSyncWarnings_ClientRegressionHarness" <<<"$out"; then
    pass "harness runs and passes when node is present"
  else
    bad "harness runs and passes when node is present" "$(tail -20 <<<"$out")"
  fi
fi

# --- 3. THE GUARD LIVES IN THE TEST, NOT IN MAKE -----------------------------
# Rewritten 2026-08-20 after three failed attempts at policing Makefile idioms
# (user decision 2026-08-20f). The previous version of this oracle scanned
# make's database for recipes matching `$(GO) test` and shared the very blind
# spot it was meant to police: a `$(GOCMD)` alias, a `define`d recipe or an
# `include`d fragment evaded both the Makefile guard AND this check, which
# would still have printed ORACLE: PASS. The contract is now a property of the
# Go test, so no Make idiom, include, alias or future target can route around
# it, and this oracle no longer has to enumerate a space it cannot bound.
optout=$(grep -ohE '[A-Z][A-Z0-9_]*(SKIP|ALLOW)[A-Z0-9_]*' \
        internal/handlers/accounts/warnings_client_regression_test.go 2>/dev/null \
        | sort -u | head -1)
if [[ -z "$optout" ]]; then
  bad "an explicit opt-out env var exists" \
      "no opt-out variable found in the test; a hard failure with no escape hatch breaks node-less machines"
else
  echo "      opt-out variable: $optout"
fi

if ! command -v node >/dev/null 2>&1; then
  bad "node present" "the oracle requires node to distinguish run from skip"
else
  stripped=$(printf '%s' "$PATH" | tr ':' '\n' \
             | grep -v -F "$(dirname "$(command -v node)")" | paste -sd: -)
  if PATH="$stripped" command -v node >/dev/null 2>&1; then
    bad "node can be removed from PATH for the negative test" "node still resolves"
  else
    # (a) node absent, no opt-out -> the test must FAIL, not skip.
    out=$(PATH="$stripped" go test -count=1 -v \
          -run TestSyncWarnings_ClientRegressionHarness \
          ./internal/handlers/accounts/ 2>&1); rc=$?
    if (( rc == 0 )); then
      bad "node absent and no opt-out -> test FAILS" \
          "exited 0 — the harness can still be skipped silently"
    elif grep -q -- "--- SKIP" <<<"$out"; then
      bad "node absent and no opt-out -> test FAILS" "it skipped instead of failing"
    else
      pass "node absent and no opt-out -> test fails loudly"
    fi
    if grep -qi "node" <<<"$out"; then
      pass "the failure message names node"
    else
      bad "the failure message names node" "$(tail -5 <<<"$out")"
    fi

    # (b) node absent WITH the opt-out set -> a clean, named skip.
    if [[ -n "$optout" ]]; then
      out=$(PATH="$stripped" env "$optout=1" go test -count=1 -v \
            -run TestSyncWarnings_ClientRegressionHarness \
            ./internal/handlers/accounts/ 2>&1); rc=$?
      if (( rc == 0 )) && grep -q -- "--- SKIP" <<<"$out" && grep -qi "node" <<<"$out"; then
        pass "opt-out set -> skips cleanly with a named reason"
      else
        bad "opt-out set -> skips cleanly with a named reason" "$(tail -10 <<<"$out")"
      fi
    fi

    # (c) The guard must not be reachable around: EVERY make target that runs
    # the suite inherits it, because it is the test that refuses. Spot-check
    # the target that evaded every previous attempt.
    out=$(PATH="$stripped" timeout 600 make test-unit 2>&1); rc=$?
    if (( rc == 0 )); then
      bad "make test-unit fails when node is absent" \
          "exited 0 — the guard did not travel with the test"
    else
      pass "make test-unit fails when node is absent (exit $rc)"
    fi
  fi
fi

# --- 3b. the make targets re-run this package UNCACHED ------------------------
# Added 2026-08-20 (third amendment, disclosed in report.md) to encode user
# decision 2026-08-20h. Rationale: `go test` tracks the PATH STRING, not node's
# presence, so on Debian/Ubuntu — where the nodejs package installs into
# /usr/bin, already on PATH — `apt remove nodejs` leaves PATH byte-identical and
# a cached pass replays with the guard never firing. checker-second reproduced
# that from the distro package itself. The make targets now re-run only this
# one package with -count=1 (~0.5s) to close it. Checked via `make -n` so the
# assertion is about the recipe make would actually execute.
# checker-tests demonstrated that `make -n` alone is NOT proof of enforcement:
# it strips an ignore-errors `-` prefix, and it cannot see `|| true`, `.IGNORE`,
# `-k` or `.ONESHELL`. It built two throwaway Makefiles -- `-$(GO) test ...` and
# `$(GO) test ... || true` -- that satisfied the first draft of this check while
# enforcing nothing. So the recipe TEXT is inspected too, for the two evasions
# actually demonstrated. This still is not a proof of enforcement; the check
# with real teeth is the symlink experiment (node deleted at a byte-identical
# PATH), which both lanes run by hand. Recorded so nobody mistakes a green 3b
# for that.
for t in test test-unit test-coverage race; do
  if ! make -n "$t" 2>/dev/null | grep -qE 'test[^|]*-count=1[^|]*internal/handlers/accounts'; then
    bad "make $t re-runs internal/handlers/accounts uncached" \
        "without it, removing node without changing PATH replays a cached pass and the guard never fires"
    continue
  fi
  # The recipe line as WRITTEN, not as dry-run: reject a swallowed status.
  # Match on -count=1 alone: the recipe writes the package as $(ACCOUNTS_PKG),
  # so grepping the raw text for the expanded path finds nothing. The first
  # draft of this check did exactly that and fell through to a pass-by-default
  # "inherited" branch -- a false PASS in the check written to catch false
  # passes. There is no pass-by-default branch now: an unfindable recipe line
  # is a FAIL, because it means this check cannot see what it is asserting.
  line=$(awk -v t="$t" '
      $0 ~ "^"t":" {inr=1; next}
      inr && /^\t/ {print}
      inr && !/^\t/ && NF {inr=0}
    ' Makefile | grep -E -- '-count=1')
  if [[ -z "$line" ]]; then
    bad "make $t's uncached re-run is visible in the Makefile text" \
        "no -count=1 recipe line found under target '$t'; the status check below cannot run"
  elif grep -qE '^\s*-|\|\||\|[^|]' <<<"$line"; then
    bad "make $t's uncached re-run can fail the target" \
        "status is swallowed: $line"
  else
    pass "make $t re-runs internal/handlers/accounts uncached, status not swallowed"
  fi
done
if grep -qE '^\.IGNORE:|^MAKEFLAGS.*-k|^\.ONESHELL:' Makefile; then
  bad "no makefile-wide directive defeats the re-run's exit status" \
      "$(grep -nE '^\.IGNORE:|^MAKEFLAGS.*-k|^\.ONESHELL:' Makefile | head -3)"
else
  pass "no makefile-wide directive defeats the re-run's exit status"
fi
# ...and only that package: the rest of the suite must keep its cache.
if make -n test 2>/dev/null | grep -qE 'test .*-count=1 \./\.\.\.'; then
  bad "caching is disabled only for internal/handlers/accounts" \
      "the whole suite runs uncached — ~32s of storage tests on every repeat run"
else
  pass "caching is disabled only for internal/handlers/accounts"
fi

# --- 4. no Makefile scanning machinery survives ------------------------------
# The scan was the defect class. If it is still present, the design the user
# directed has not actually been adopted.
if grep -qE 'GOTEST_TARGETS|foreach.*check-node|MAKECMDGOALS.*node' Makefile; then
  bad "the Makefile target-scanning machinery is gone" \
      "$(grep -nE 'GOTEST_TARGETS|foreach.*check-node|MAKECMDGOALS.*node' Makefile | head -3)"
else
  pass "the Makefile target-scanning machinery is gone"
fi
# Non-test targets must not have acquired a node dependency.
for t in build clean vet dev; do
  if make -n "$t" 2>/dev/null | grep -qi "node is required"; then
    bad "target '$t' does not require node" "a node-less machine cannot run 'make $t'"
  fi
done
pass "non-test targets (build, clean, vet, dev) do not require node"

# --- 5. the harness verdict cannot be faked ---------------------------------
# Tested by BEHAVIOUR, not by grepping the glue's source. The first draft of
# this check grepped for a `PASS"` token and produced a false FAIL against a
# correct implementation whose matcher ended `PASS(\\s|$)`. An oracle that
# tests spelling instead of behaviour is worse than no oracle: it punishes the
# arm that solved the problem differently.
harness=internal/handlers/accounts/testdata/js/warnings_dom_harness.js
if [[ ! -f "$harness" ]]; then
  bad "harness file present at the expected path" "missing $harness"
elif ! command -v node >/dev/null 2>&1; then
  bad "node present for the fake-verdict test" "cannot distinguish run from skip"
else
  backup=$(mktemp); cp "$harness" "$backup"
  # Idempotent: it runs explicitly on the happy path AND from the trap, and a
  # second call must not chase a backup the first one already removed.
  restore() { [[ -f "$backup" ]] && cp "$backup" "$harness"; rm -f "$backup"; return 0; }
  # trap, because checker-tests correctly flagged that an interrupted oracle
  # would otherwise leave a FORGED harness in a tracked file.
  trap restore EXIT INT TERM
  cat > "$harness" <<'FAKE'
// injected by accept.sh: reports FAIL for every check but exits 0
console.log("RESULT dismiss_resolve_recreate FAIL {}");
console.log("RESULT dismiss_then_reload_unchanged FAIL {}");
console.log("RESULT guard_directions FAIL {}");
console.log("SUMMARY SOME_FAILED");
process.exit(0);
FAKE
  if go test -count=1 -run TestSyncWarnings_ClientRegressionHarness \
       ./internal/handlers/accounts/ >/dev/null 2>&1; then
    bad "a harness reporting FAIL while exiting 0 fails the Go test" \
        "it passed — the glue accepts a check on its name alone"
  else
    pass "a harness reporting FAIL while exiting 0 fails the Go test"
  fi
  restore
fi

echo
if (( fails == 0 )); then echo "ORACLE: PASS"; exit 0; fi
echo "ORACLE: FAIL ($fails check(s))"; exit 1
