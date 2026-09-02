# CB6 — cosmetic/test-hygiene close-out of the CB backlog

Authorized 2026-09-02 ("Fix the ±5 band and the other cosmetic backlog
items"). Five small items, one task. Tier 2 (lean), checks: tests,a11y —
a11y is mandatory (markup + interactive behavior change); no `second`
lane because NO money figure's arithmetic changes (rationale: the
defect-history triggers are value formatting/rounding, split
classification, rendered-string arithmetic — none apply; item 5 changes
summation ORDER only, proven sub-display-precision).

## CB6-1 — pin the ±5 direction band (V3 promotion, test-only)
internal/services/insights/trends_test.go: committed table test on
MajorExpenseTrends' classifier asserting the band edges: changePercent
exactly +5 → "stable", just above (+6 or a fixture yielding >5) → "up",
exactly -5 → "stable", below → "down". Must kill a band mutation to ±0
(checker-tests verified in CB3 that NOTHING pins this today — the test's
whole purpose). Construct fixtures via current/previous totals that land
exactly on the edges (e.g. prev 100, cur 105 → +5 stable; cur 105.01 →
up). No production-code change.

## CB6-2 — credits header wording
web/static/js/charts.js renderMajorExpenseCredits: header becomes
"Net credits (refunds met or exceeded spending)" — zero-total entries
(refunds MET spending) sit under it since CB4.

## CB6-3 — credits and rolled-up rows become drillable
The donut's wedge click already calls openMajorExpenseDrilldown(name).
Make each credits row AND each "smaller" (rolled-up) row a real button
invoking the same function with the row's name ("Unmatched" included —
its drilldown path exists). Accessibility contract (checker-a11y):
real <button type="button"> elements (not div-onclick — the KD/CB
precedent), accessible name = the group name (visible text suffices),
focus-visible ring consistent with the app's existing button idiom,
and no color-only signaling introduced. Keyboard operability via native
semantics. If openMajorExpenseDrilldown is not importable/reachable
from the breakdown renderer's scope, mirror how the wedge handler
resolves it (typeof guard).

## CB6-4 — sign-aware Total tile color in kpi-month-detail.html
Line ~49: the else-branch paints EVERY non-income kind's Total red.
Contract by kind polarity (matching the row-level convention at ~107
and the IsSavings Net tile at ~30):
- expense-like kinds (expenses, living, healthcare): Total > 0 red
  (money out), Total <= 0 green (net refund/credit).
- savings and savings-rate kinds: Total >= 0 green, < 0 red.
- income: green (unchanged).
Enumerate the kinds from handleKPIMonthDetail's switch — do not guess;
if a kind exists outside this list, STOP and report. Keep the exact
class pairs already used in the file (green-700/green-400,
red-600/red-400) — no new utilities (css-verify must stay clean).

## CB6-5 — pin one summation order (7e-15 divergence)
handleMajorExpenseDrilldown currently date-sorts txns BEFORE summing;
bucketMajorExpenses sums in match order. Move the drilldown's total/
count/avg computation ABOVE the sort.Slice so both sum in the same
(bucket) order; comment why (float64 summation order pinned to the
bucket's, so the modal Total is bit-identical to the credits/list
figure). Display sort unchanged. Add a regression test only if a
deterministic fixture can actually distinguish orders (the checker's
7e-15 case needed many transactions); otherwise the comment + the
existing cross-surface tests suffice — say which you did.

## Out of scope
Everything else. No arithmetic changes anywhere.

## Verdict record
Attempt 1: checker-tests PASS (band pinned bit-exactly both directions,
buttons executed under jsdom); checker-a11y FAIL (dark focus ring vs
hover bg 2.31:1) — CONCEDED. Attempt 2: dark:hover:bg-gray-700 →
bg-gray-900 at both sites; a11y independently recomputed 3.9714:1 PASS;
checker-tests re-issued PASS with byte-identity proof for the standing
criteria. Housekeeping in this commit: stale .swarm/flags (ND3, Z5 —
both tasks long accepted) swept per checker O4. Open observations O1-O3
recorded here as accepted residue (characterization tests, no CB6-5
regression test by spec option, full-row-text accessible names ruled
acceptable by a11y).
