# CB3 — the per-transaction / per-period abs class (refund class, part 3)

Authorized by the user 2026-09-02 ("run CB3"). Closes the sites CB2
classified out plus checker-second's insights finding. Contracts derive
from precedent: KD-2026-08-30d (signed rows, Total = sum of rows),
CB1/CB2 (signed negated nets), and the existing signed-net display of
major-expense groups (the Travel group renders net-negative today).

## Sites and per-surface contracts (line numbers at master post-#81)

- **CB3-A** handlers.go ~383, major-expense drilldown modal:
  `total += math.Abs(t.Amount)` per transaction → `total = -(signed
  sum)`; avgAmount derives (total/count) unchanged in form. Contract:
  the modal Total must equal the group's signed-net figure the
  major-expense list already shows (today the Travel group's modal
  DISAGREES with its own list row). Refunds reduce the total; a
  refund-dominant group renders negative.
- **CB3-B** handlers.go ~1492, top-merchants chart:
  `merchantTotals[label] += math.Abs(t.Amount)` → `-= t.Amount`
  (signed net; a refund reduces that merchant's spend). Horizontal bar
  chart renders a negative bar for net-refund merchants — honest.
  Ordering stays by total desc.
- **CB3-C** handlers.go ~1564-1566, cumulative cash-flow day walk —
  the WRONG-DIRECTION bug: today `dayTotal -= math.Abs(t.Amount)` for
  every non-Income row, so an outflow-typed REFUND (+800) SUBTRACTS 800
  from cash flow. Fix: for non-Transfer rows, `dayTotal += t.Amount`
  (income positive adds, outflow negative subtracts, outflow-typed
  refund positive adds). NOTE the Income branch also changes shape:
  `+= math.Abs` → `+= t.Amount`; a negative-amount Income row (income
  reversal) then correctly subtracts — document this in the comment.
- **CB3-D** internal/services/insights/trends.go, five sites:
  - ~39/44 (MajorExpenseTrends period totals): `+= math.Abs(t.Amount)`
    → `-= t.Amount` (signed net per period; matches CB3-A's contract).
    ChangeAmount/sort by |change| stays (ordering only).
  - ~287/295/305 (SpendingVelocity dailyAvg / historicalDaily /
    spentSoFar): `math.Abs(bucket.SumAmount())` → `-bucket.SumAmount()`
    (signed period net). A refund-dominant period yields negative
    daily-average/spent-so-far — honest. Worker MUST trace downstream:
    any division, threshold, or projection consuming these that
    misbehaves on negatives (or a zero denominator) is a STOP-and-report
    unless a trivial guard preserves current behavior for the
    all-normal case.
- **CB3-E** metrics.go PercentChange (~729): NO code change. The
  `|previous|` denominator is the standard convention for signed bases
  (preserves direction). Document that contract in a comment and PIN it
  with a test (negative-previous case), so the next enumeration doesn't
  reopen it.

## Out of scope
- Sort comparators by |amount| / |ChangeAmount| (display ordering).
- verdict.go's math.Abs (formatting with explicit sign handling).
- Range-level metrics totals (CB1 precondition, unchanged).
- The kpi-month-detail.html unconditional red Total tile (cosmetic,
  checker-second CB2 finding — backlog, needs a design call).

## Tier 3, checks: tests,second
Money on multiple surfaces incl. MCP-rendered answers. Committed
regression tests per site (refund-dominant fixtures; income-keyword
gotcha applies to any CSV/HTTP fixture). Real-data differential by
checker-second: per-bucket refund-dominance enumeration extended to the
drilldown groups (Travel IS net-negative on real data — diffs are
EXPECTED there, must equal exactly 2×|refund sum| per affected figure)
and to merchant totals and the cash-flow chart (refund days exist —
enumerate and predict every diff).

## Oracle (.swarm/tier3/CB3/accept.sh)
Probe fixtures with (a) a refund inside an outflow-dominant group/
merchant/day and (b) a refund-DOMINANT group/period. Assertions:
- A: drilldown Total == -(signed sum) == sum-of-rows contract; equals
  the list row's signed net for the same group.
- B: merchant net reduced by refund; net-refund merchant negative.
- C: cash-flow running total INCREASES on a refund day (direction).
- D: MajorExpenseTrends period totals signed; SpendingVelocity signed
  aggregates on a refund-dominant period.
- E: PercentChange(current, negative previous) pinned.
- CB1+CB2 non-regression by name; both package suites + consumers green.
Both-ends validated before dispatch.

## Ruling CB3-2026-09-02a (lead, pre-dispatch): sign-vs-type conflict
TestBuildCumulativeChartData_PositiveAmountOutflows pins "positive-amount
outflows subtract (unsigned bank exports; use type not sign)". That
premise CONFLICTS with the classifier's documented pipeline contract
(classifier.go ClassifyTransactions): after classification, negative
amounts are normalized for purchases and positive non-income amounts are
DELIBERATELY kept positive AS credits/refunds. An unsigned bank export
cannot reach the chart un-normalized through the real loader; the test's
fixture bypasses the classifier. DECISION: the chart (and every CB3
surface) follows the pipeline contract — positive outflow-typed = refund,
adds cash. The test is REWRITTEN to assert the pipeline contract (same
fixture through the new arithmetic: [5000, 6500, 7000]) with a comment
explaining the supersession; if unsigned-export support is ever needed it
belongs in the LOADER, not per-chart abs. Not a deletion.

## Oracle validation record (pre-dispatch)
- FAIL end (pristine 50a5e06): every probe failure is the defect
  (drilldown 400 vs 200; merchant 400 vs 200; cash-flow 600 vs 800
  DIRECTION; velocity positive vs negative; trends 400 vs 200);
  PercentChange pin passes unchanged (CB3-E confirmed no-code-change).
- PASS end: prototype (all CB3-A..D sites signed) + ONLY the ruling
  CB3-a test rewrite → ORACLE PASS incl. the mcpsvc/spend consumer
  suite. Prototype discarded.

## Amendment CB3-b (lead, post-worker-report): the tool-docstring surface
The worker's downstream trace found the split-source documentation defect
(ND3 class) pre-checker: mcpsvc/spend/trends.go's get_trends docstring
says current/previous amounts are "POSITIVE dollar figures" for BOTH
trend kinds — now false for major_expense_trends (signed per CB3-D),
still true for category_trends (abs-based CategoryTotals, NOT in CB3
scope). Fix in attempt 1: reword the docstring for major_expense_trends
only (signed; refund-dominant periods negative); leave category_trends
wording and computation untouched. Catch attribution: WORKER
downstream-trace (a first — record in the experiment log).

## Attempt 1: dual-lane FAIL, both CONCEDED (lead, 2026-09-02)
- checker-tests (primary): mutation survivors at trends.go:44 (pin-match
  sumByExpense — live path via get_trends pins) and :320 (spentSoFar —
  the velocity test's fixture missed the current calendar month, and
  MonthProjection was asserted sign-only). The ORACLE missed both too —
  lead artifact defect, conceded.
- checker-second (adversarial): LIVE bug — MajorExpenseTrends' inline
  percent-change/direction classifier (~68-86) divides by SIGNED
  previous and mis-derives Direction. Real-ledger reproduction: Travel
  current=0 previous=-628 → change_amount=+628 but change_percent=-100,
  direction="down" (self-contradictory). Also previous==0,current<0 →
  "up"/+100. The worker's own refund fixture existed but asserted only
  CurrentAmount. Conceded.

## Amendment CB3-c (attempt 2 contract): the inline trend classifier
In MajorExpenseTrends (and ONLY there — CategoryTrends stays abs-based
and its classifier untouched):
- ChangePercent = (current - previous) / |previous| * 100 when
  previous != 0 (the CB3-E convention — preserves direction for signed
  bases).
- previous == 0: ChangePercent = +100 if ChangeAmount > 0, -100 if < 0,
  0 if == 0 (sign-consistent replacement for the old flat 100).
- Direction derives from the SIGN OF ChangeAmount (up / down / stable),
  never from raw current/previous comparison. ChangePercent's sign must
  always agree with ChangeAmount's (both zero together).
Attempt 2 must also add committed tests killing the two mutation
survivors: (a) an all-pinned refund fixture through MajorExpenseTrends
(pins path, ResolveByIdentity); (b) a velocity fixture DATED IN THE
CURRENT CALENDAR MONTH asserting the MonthProjection identity
MonthProjection == signedSpent + DailyAverage*DaysRemaining exactly, and
Direction/ChangePercent assertions on every refund trend fixture.

## Backlog (checker-second, pre-existing, NOT CB3): bucketMajorExpenses'
total > 0 filter hides net-negative groups (Travel & vacations) entirely
from the donut/list/drilldown on the default view — the group most
affected by refunds is invisible. Needs a design call (show negative
groups? distinct styling?). Also: checker-second self-reported writing
probes into the shared tree before self-correcting (freeze-handshake
reminder for future dispatches).

## Attempt 2: dual-lane PASS (2026-09-02)
- checker-second: real-ledger Travel row now up/+100/+628 sign-consistent;
  755-row invariant sweep (4 window sizes, 2024-01..2026-09) zero
  violations; boundary attacks clean; round2 proven sign-class-preserving
  on the MCP path; both attempt-1 mutants independently confirmed dead.
- checker-tests: both survivors killed by committed tests (on today's
  clock); classifier mutants (a)(b) caught; CategoryTrends byte-identical;
  scope exact. Findings: F1 60s/month test flake window (fixed in-attempt,
  one-line clamp); F2 the ±5 stable band is pinned by NOTHING (mutant
  ±0 survives repo-wide; PRE-EXISTING on master) — V3-promotion backlog
  candidate; F3 spec-wording reconcile below.
- F3 resolution (lead): CB3-c bullet 3 is corrected to read: Direction
  derives from ChangePercent through the EXISTING ±5 stable band; since
  ChangePercent's sign always agrees with ChangeAmount's (proven by the
  checker's 20k-fixture property probe), Direction never contradicts
  ChangeAmount — the band, not the raw sign, decides "stable".
