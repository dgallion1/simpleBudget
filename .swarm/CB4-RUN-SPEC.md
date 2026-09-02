# CB4 — net-negative major-expense groups become visible

Authorized 2026-09-02 ("Fix the hidden net-negative groups filter") with
the design delegated to the lead. Closes the checker-second CB2/CB3
backlog finding: bucketMajorExpenses' `total > 0` filter hides any group
whose window nets a refund (Travel & vacations, real data, cruise
refunds) from the donut, the "Other"/smaller breakdown, AND the
drilldown — while the Major Expenses page shows the same group signed.

## Design contract (lead ruling CB4-2026-09-02a)
The completeness/geometry split: the SHARED SOURCE returns complete
data; each SURFACE applies only its own documented geometry constraint.
1. **bucketMajorExpenses** (the source): include every matched group
   that has at least one transaction, regardless of sign of total —
   drop the `total > 0` filter entirely. Sort stays total-descending
   (net-negative groups naturally land last). Zero-total groups WITH
   transactions are included (their drilldown is meaningful).
2. **Donut (buildMajorExpenseChartData)**: pie geometry cannot render a
   ≤ 0 wedge. The donut displays only total > 0 buckets — the filter
   moves HERE, documented as a geometry constraint. Net-negative and
   zero-total groups are returned alongside in a new `"credits"` list
   (name + amount, same shape as `"smaller"`) so the dashboard can show
   them as text under the chart. They must NEVER be folded into the
   "Other" wedge (a negative inside Other falsifies the wedge's size).
   `grandTotal` for percent math stays the sum of DISPLAYED (positive)
   values — unchanged semantics for existing wedges.
3. **Drilldown (handleMajorExpenseDrilldown)**: needs no logic change —
   once the source includes the group, lookup-by-name works; its Total
   is already signed (CB3-A). Verify with a test, don't assume.
4. **Template/JS**: render the `credits` list under the donut (same
   visual treatment as the existing "smaller" rolled-up breakdown, with
   signed amounts formatted like the rest of the CB series — a credit
   shows as a negative figure). If no txns produce credits, nothing
   renders (existing empty behavior preserved).

## Out of scope
- The always-red month-detail Total tile (separate parked item).
- Major Expenses page (already signed, untouched).
- Any change to match/pin logic.

## Tier 2 (lean), checks: tests,second
Defect-history surface (money figure + an inclusion threshold that
previously lived in one source and now deliberately becomes a
per-surface geometry rule — the checker must ENUMERATE the surfaces and
confirm each one's filter is local, documented, and consistent with
this contract). checker-tests: run the builders/handlers over fixtures
with a net-negative group, a zero-total-with-txns group, and > donut
limit positive groups; assert the donut never contains a wedge ≤ 0, the
credits list carries the hidden groups, Other excludes them, drilldown
resolves them, and existing positive-only behavior is byte-identical.
checker-second: real-data check — Travel & vacations must appear in the
credits list on the live-data default window with its signed net, and
the donut wedge set must equal master's exactly (no positive wedge
gained/lost); attack the Other-rollup boundary (limit-th bucket
positive, negatives beyond it) and the all-negative edge (donut empty,
credits full, no NaN percents).

## Attempt 1: dual-lane PASS (2026-09-02)
- checker-tests: all 4 contract mutations killed (incl. proof the
  worker's Other-drilldown helper fix was load-bearing); positives-only
  output byte-identical to master; real charts.js executed under jsdom
  (signed formatting, empty states, HTML escaping). O1 credits-header
  wording vs zero-total groups; O2 unstable sort on tied totals
  (pre-existing) — backlog.
- checker-second: real data — Travel & vacations -159.95 (25 txns) in
  credits, drilldown identical, positive wedge set == master on both
  windows; boundary fixtures verified by reading assertion bodies; no
  third consumer; no dual formatter. FINDING (spec-silent, backlog): a
  net-negative UNMATCHED total still silently disappears (its >0 wedge
  guard predates CB4 and Unmatched never routes through the shared
  helper) — same class, different path, follow-up candidate.
