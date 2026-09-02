# CB5 — Unmatched joins the CB4 credits contract

Authorized 2026-09-02 ("Fix the negative-Unmatched drop too"). Closes
checker-second's CB4 finding: `unmatchedTotal` can net negative (or
zero with transactions), and when it does it vanishes from the donut
response entirely — no wedge AND no credits entry — because the
pre-CB4 `if unmatchedTotal > 0` wedge guard is Unmatched's ONLY path
into the payload.

## Contract (extends ruling CB4-2026-09-02a to the Unmatched path)
- unmatchedTotal > 0: wedge, exactly as today (unchanged).
- len(unmatchedTxns) > 0 && unmatchedTotal <= 0: an entry in the
  `credits` list ({name: "Unmatched", amount}), appended after the
  matched credit buckets. Consistent with CB4's zero-total-with-
  transactions rule for matched groups.
- No unmatched transactions: absent everywhere (unchanged).
- Drilldown needs nothing: name="Unmatched" already serves the raw txns
  with a signed Total (CB3-A).

## Tier 2 (lean), checks: tests,second, worker=lead
Lead-direct under the 2026-08-31 lean exception (small, well-specified,
Tier 2); the named checkers are the non-author eyes and gate.sh check
decides acceptance, unchanged. checker-tests: mutation the guard back →
committed test fails; positives-path byte-parity with master; the three
Unmatched states (positive / ≤0-with-txns / empty) asserted.
checker-second: real-data — current ledger's Unmatched nets POSITIVE
(expected: no behavior change live; prove it, wedge set identical to
master); synthetic refund-dominant Unmatched exercises the new path;
attack the boundary (exactly 0 with txns → credits, not wedge).

## Attempt 1: dual-lane PASS (2026-09-02, lead-authored)
- checker-tests: block-revert + boundary (<=0 vs <0) + two self-invented
  mutations all killed; 14-fixture parity probe — unchanged paths
  byte-identical to master; test bodies read line-by-line, keyword-match
  of the ordering fixture proven via mutA. O1 credits-header wording
  (pre-existing, widened); O2 credits rows not drillable (consistent
  with CB4) — backlog.
- checker-second: live ledger's Unmatched is EMPTY (0 txns) — the fix is
  structurally dead on today's data, proven byte-identical vs master;
  synthetic refund-dominant path fires; renderer has no per-name special
  case; nil/boundary clean. Backlog: ~7e-15 summation-order float
  divergence credits-vs-drilldown (pre-existing since CB4, invisible at
  display precision) — pin one summation order in a follow-up.
