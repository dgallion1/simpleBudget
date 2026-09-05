# CB7/CB8 — range-level totals and velocity baseline go signed

Opened 2026-09-03 from a re-verification of three earlier findings. Two
confirmed as defects (CB7, CB8); the third (MoM spending-trend `prev > 0`
guard) is reclassified as a product/UI-semantics choice and is OUT OF
SCOPE unless the user says otherwise (see "Not in scope" below).

Both tasks close the last two members of the refund-dominant defect class
opened by CB1 (per-month walk), CB2 (nine month-bucket surfaces), CB3
(per-transaction / per-period). CB1–CB3 each deliberately left the RANGE
total and the velocity BASELINE alone; this run finishes them.

Ledger prefix: `CB7`, `CB8` (continuing the CB series; no collision).
Territory: budget2 only. No other run is live in this tree.

---

## CB7 — range totals: `math.Abs(range net)` → signed negated net

### Defect (confirmed against a09c9cf)
`internal/services/metrics/metrics.go:378`
`totalExpenses := math.Abs(outflows.SumAmount())` — the range-level total
is an absolute value while every per-month figure feeding the same
surfaces has been signed since CB2. A range whose outflow-typed rows net
POSITIVE (refunds exceed spending) therefore reports positive spending,
understates NetSavings (income − |net| instead of income + net refund),
understates SavingsRate, and breaks the CombinedCumulativeBalance
partition invariant that `internal/models/dashboard.go:120-136` today
documents as "out of scope" for exactly this case.

### Sibling sites (same defect class, same task — enumerated, not diffed)
The split-classification rule: one convention, every surface. Five
range-level `math.Abs(... SumAmount())` sites exist:

| # | Site | Figure |
|---|------|--------|
| 1 | `internal/services/metrics/metrics.go:378` | `totalExpenses` |
| 2 | `internal/services/metrics/metrics.go:406` | `healthcareTotal` |
| 3 | `internal/services/metrics/metrics.go:436` | `livingTotal` |
| 4 | `internal/handlers/explorer/handlers.go:191` | explorer `totalExpenses` (page) |
| 5 | `internal/handlers/explorer/handlers.go:341` | explorer `totalExpenses` (partial/HTMX) |

All five become `-set.SumAmount()` (the SY1/CB2 convention: positive =
net spend, negative = net refund). No new helper; the negation is the
established idiom (`-lo.SumAmount()` at metrics.go:526 etc.).

Derived figures inherit the sign with NO further change and must be
proven honest by test, not clamped:
- `netSavings = totalIncome - totalExpenses` (refund-dominant range ⇒
  savings EXCEED income; correct — the refund is cash in).
- `savingsRate` (guard `totalIncome > 0` unchanged; may exceed 100).
- `healthcareActual`, `healthcarePerMonthDelta`, `healthcareCumulativeDelta`.
- `actualMonthly`, `perMonthDelta`, `cumulativeDelta`, `LivingExpensesTotal`.
- `PeriodComparison.ExpensesChange` via `PercentChange` — ALREADY uses the
  `|previous|` denominator (metrics.go:741, CB3-E); a fixture must prove
  the sign tracks the change with a negative base. `PercentChange` itself
  is NOT modified.

### Consumers that must change (rendering / contract)
1. `web/templates/components/kpis.html:36` —
   `{{formatMoney (abs .Metrics.TotalExpenses)}}` re-flips the sign in
   the template. Drop `abs`. Value color becomes sign-aware using the
   CB6-4 idiom (`kpi-month-detail.html:49`): `gt 0.0` → existing rose,
   else → emerald-700/emerald-400. Card tint/border unchanged.
   ⚠ a11y: emerald text on the rose-50 card is a contrast hazard
   (NEXT.md records emerald-600 at 3.43:1 on tinted bands) — the worker
   must pick a shade that meets 4.5:1 on BOTH the light rose-50 and dark
   rose-900/20 grounds, and checker-a11y measures it.
2. `web/templates/pages/explorer.html:755` — `{{if isPositive
   .TotalExpenses}}` hides the Expenses chip for any refund-dominant
   filter. Becomes `{{if ne .TotalExpenses 0.0}}` (guard against a float
   compare helper if `ne` on float64 misbehaves — `isNonNegative`/`isNegative`
   exist in render.go:105 area; use whatever the template funcmap already
   supports, no new helper). Value color sign-aware as in (1), same
   contrast rule.
3. MCP contract — three wording sites say `total_expenses` is "always
   non-negative (it is an absolute value)":
   `internal/services/mcpsvc/spend/summary.go:185` (tool description),
   `internal/services/mcpsvc/server.go:84` (server instructions),
   `internal/services/mcpsvc/server_test.go:196` (pins the wording).
   New contract wording: `total_expenses` is SIGNED — positive is net
   spend; it goes NEGATIVE when this window's refunds exceed its spending
   overall; the sum of `by_month` now EQUALS `total_expenses` exactly
   (delete the "in MAGNITUDE / negation of each other" clause). Worker
   also greps `~/.claude/skills/budget2-mcp/` and repo `docs/` for the
   same phrase and reports hits (edit repo docs; report skill-dir hits
   in the manifest for the lead — the skill lives outside the repo).
4. `internal/models/dashboard.go:120-136` — rewrite the invariant doc:
   the "range as a whole nets outflow-negative" precondition is REMOVED;
   the partition holds for every range because TotalExpenses is now the
   signed net. Also update the metrics.go:304-306 comment ("The range
   total still runs math.Abs(...)") and the explorer handler comment
   (handlers.go:188-190).
5. `.swarm/NEXT.md` SY-run backlog bullet (line ~491, "CombinedCumulative
   Balance walk assumes per-month |sum| partitions the range-level
   |sum|") — mark resolved by CB7.

### Not affected (worker/checker confirm, do not touch)
- `engine.TotalExpenses` / `month.TotalExpenses` in
  `internal/services/retirement/**` and `internal/handlers/whatif/
  spending_trajectory.go:84` — the projection ENGINE's expense model, a
  different type; unrelated to ledger totals.
- `PlanExcludedTotal` (already signed, SY4).
- Per-month series (already signed, CB2).

### Tests (committed, not throwaway)
- `metrics_test.go`: a REFUND-DOMINANT RANGE fixture (two months, one
  ordinary, one with a refund exceeding the whole range's spend; income
  present; living + healthcare targets set, HI category rows included)
  asserting: `TotalExpenses < 0` with the exact signed value;
  `NetSavings == TotalIncome - TotalExpenses` (> TotalIncome);
  `LivingExpensesTotal`, `HealthcareTotal`-derived deltas exact;
  and the CombinedCumulativeBalance invariant `last == -CombinedCumulativeDelta`
  now HOLDS in the previously-excluded case. A `math.Abs` mutant at any
  of sites 1–3 must fail this test (checker-tests verifies by mutation).
- `PeriodComparison`: fixture with a negative comparison-period
  TotalExpenses proving `ExpensesChange` sign tracks the change.
- Explorer handler test: refund-dominant filter ⇒ negative TotalExpenses,
  NetAmount = income − (negative). Both handler paths (191, 341).
- MCP `summary_test.go`: refund-dominant window ⇒ `total_expenses`
  negative AND `sum(by_month) == total_expenses` exactly (the reconcile
  test at :403 extended or a sibling added).
- Rendered-string probe, promoted to a test (rendered-string-arithmetic
  rule): render `kpis.html` with a refund-dominant metrics fixture and
  assert the Expenses tile string carries the minus sign via
  `formatMoney` (the "$-163" vs "-$163" backlog item is separate — assert
  whatever formatMoney produces for a negative today, pinned, not
  redesigned).
- Existing tests whose fixtures assumed Abs (e.g. `summary_test.go:100,
  258, 383`, `plan_exclusions_test.go:76`) are expected to keep passing —
  their data is ordinary-signed. Any that FAIL must be reported, not
  silently re-pinned (CB2-2026-09-02a precedent: calibration updates are
  a lead ruling, not a worker choice).

### Acceptance criteria
(a) `go build ./... && go vet ./... && go test ./...` clean.
(b) Zero `math.Abs(` applied to a range/filter-level `SumAmount()` remains
    in metrics.go and explorer/handlers.go (checker greps; per-transaction
    Abs elsewhere is out of scope).
(c) All new tests above present and each proven load-bearing by one
    mutation (checker-tests reverts one site at a time and shows the
    failing test).
(d) MCP wording at all three sites updated; `server_test.go` passes with
    the new pin; no remaining "always non-negative" claim about
    `total_expenses` anywhere in the repo.
(e) Real-data check (checker-second): on the live ledger every current
    dashboard range and the explorer default view render BYTE-IDENTICAL
    before/after (the live ledger has no refund-dominant range — CB1
    established zero refund-dominant months exist), AND a synthetic
    refund-dominant range renders negative with the sign-aware color.
(f) checker-a11y: contrast ≥ 4.5:1 for the new negative-value color on
    both themes' card grounds, for kpis.html and explorer.html.

Tier **2**, checks **tests,second,a11y**. Rationale: money figure on
three surfaces plus a threshold (sign) classification — two
defect-history triggers; markup color change → a11y mandatory (CB6
precedent). Reversible, so not Tier 3.

---

## CB8 — velocity `BurnRateChange` with a refund-dominant baseline

### Defect (confirmed)
`internal/services/insights/trends.go:348`
`if historicalDaily > 0 { burnRateChange = ... }` — CB3-D made
`historicalDaily` signed, then left this guard, so a ledger whose entire
history nets a refund reports `BurnRateChange = 0` regardless of the
current pace. The insights page then renders the amber "on pace" band
(`insights.html:164-178`, verdict bar `insights-verdict-bar.html:14-16`)
for a period that may be spending far faster than its history. The
CB3-D comment itself calls this "an unreported change stat".

### Contract (ruling CB8-2026-09-03a, mirrors CB3-c exactly)
```
change := dailyAvg - historicalDaily
switch {
case historicalDaily != 0:
    burnRateChange = change / math.Abs(historicalDaily) * 100
case change > 0:  burnRateChange = 100
case change < 0:  burnRateChange = -100
default:          burnRateChange = 0
}
```
- `|historicalDaily|` denominator so the sign ALWAYS tracks the sign of
  the change (spending faster than history ⇒ positive ⇒ "above your
  usual pace"), never inverting on a negative base.
- Zero base ⇒ sign-of-change (CB3-c), NOT `PercentChange`'s unconditional
  +100 — do not call `metrics.PercentChange` (its zero-base rule differs;
  documented CB3-E).
- Downstream unchanged: `insights/verdict.go` thresholds (`<0` green,
  `>paceRedThreshold` red) and both templates already render the signed
  value; a large positive on a negative base is honest ("+160% vs avg").
- Rewrite the CB3-D comment block at trends.go:339-346.

### Tests (committed)
- New `TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange`:
  `allData` = current-period purchases PLUS an older large refund so the
  ledger-wide `historicalDaily < 0` while the current period's `dailyAvg
  > 0`; assert `HistoricalDaily < 0`, `DailyAverage > 0`, and
  `BurnRateChange` equals `(dailyAvg - hist)/|hist|*100` to 0.01 and is
  POSITIVE. The `> 0` guard mutant yields 0 and must fail. Fixture MUST
  be dated in the current calendar month (CB3 attempt-1 lesson,
  CB3-RUN-SPEC.md:117/141).
- Extend `TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative`
  (trends_test.go:828) to assert `BurnRateChange == 0` there (period ==
  allData ⇒ change is 0 — pins that identical sets report 0, not ±100).
- Zero-base case: fixture whose ledger nets exactly zero with a spending
  current period ⇒ `+100`; with a refund-dominant current period ⇒ `-100`.
- Sign-inversion mutant: a `historicalDaily` (signed) denominator must
  fail the first test (change positive, result negative).

### Acceptance criteria
(a) `go build ./... && go test ./internal/services/insights/... ./internal/handlers/insights/...` clean; full `go test ./...` clean.
(b) Each new assertion proven load-bearing by mutation (checker-tests).
(c) Real data (checker-second): live ledger `historicalDaily > 0`, so
    `BurnRateChange` is byte-identical before/after on the live insights
    page; synthetic refund-dominant ledger renders a non-zero signed
    figure in the correct band.
(d) No template or verdict.go change (checker confirms diff scope).

Tier **2**, checks **tests,second**. Rationale: money-derived figure with
a threshold classification on two surfaces (band + verdict bar); no
markup change, so no a11y lane.

---

---

## CB9 — negative-zero source sites CB7 left out; formatPercent belt; MCP doc

Opened from checker-second's CB7.3 and CB8.1 observations. Lead-authored
(lean exception), Tier 2, checks tests,second (money surfaces: CSV export
and modal totals; no markup change → no a11y lane).
- `internal/handlers/dashboard/handlers.go`: classifiedMonthlyTotals (feeds
  the KPI modal rows AND the CSV export's raw `%.2f`), expAmt ×2,
  handleKPIMonthDetail's three `-sumSigned` totals (via a new
  `negSumSigned` wrapper), and the chart walk's hcAmt/livingMonth/spend all
  route through `metrics.SignedNet`. Accumulator loops (`+= -t.Amount`)
  are untouched: an accumulator starting at +0 cannot land on -0.
- `formatPercent` gets the same -0 belt as formatMoney.
- `mcpsvc/spend/trends.go` BurnRateChange doc states the CB8 rule.
- Attempt 2 (both lanes FAILED attempt 1 on untested chart-walk sites, and
  checker-tests found a fifth site the lead's grep missed —
  buildSpendingTrendChartData's indexed `-monthlyOutflowSets[m].SumAmount()`):
  that site fixed; chart-endpoint tests added (spending-trend and
  budget-vs-actual with targets wired) asserting no `-0` token and
  Signbit-clear traces; checker-second's same-class observation folded
  in — SpendingVelocity's three negated sums (insights/trends.go) reached
  MCP get_trends as `-0` via round2, now SignedNet with a test; and
  formatNumber gets the belt. Re-enumeration rule: `grep -n "= -\|:= -"`
  (the `[a-zA-Z]*[.(]` pattern missed indexed expressions).
- Tests: exactly-cancelling February at every slicing (healthcare,
  living, expenses); export cells "0.00" for all kinds; month-detail JSON
  free of `-0` tokens and Signbit-clear; KPI detail response free of
  "$-0"; chart JSON free of `-0`; velocity struct free of `-0`;
  formatPercent(-0) == "0.0"; formatNumber(-0) == "0".
Acceptance: `make check` green; every SignedNet site load-bearing by
mutation or provably consumer-unobservable (CB7 precedent); live-ledger
export/detail bytes identical except any genuine "-0.00"→"0.00" cell.
Stated survivors (attempt 2, both proven unobservable by checker-tests):
`spend` in the cumulative walk (`running += monthTarget - spend` absorbs
-0 bit-for-bit) and `spentSoFar` in SpendingVelocity (reaches only
MonthProjection, diverges only when daysRemaining==0 && dailyAvg<0, and
is belted by formatMoney / dropped by MCP). Both converted for
single-source consistency.

## Not in scope (recorded so it is not rediscovered)
- **MoM spending-trend `prev > 0` guard** (`internal/handlers/dashboard/
  handlers.go:1533`), pinned by `cb2_signed_spending_trend_test.go:48`
  under CB2 amendment CB2-c: a refund-dominant BASE month renders 0%.
  Reclassified 2026-09-03 from defect to product semantics. If the user
  wants surface consistency with CB3-c/CB8 (|prev| denominator,
  sign-of-change on zero), it is a one-line change plus re-pinning that
  test — a separate lead ruling, not silently folded into CB7/CB8.
- `CategoryTrends` abs-based classifier (CB3 noted, still untouched).
- `formatMoney` negative rendering "$-163" vs "-$163" (SY backlog).

## Rulings
- CB7-2026-09-03a — the three SY-era "remainder nets refund" tests
  (`metrics/plan_exclusions_remainder_test.go` ×2, `mcpsvc/spend/
  plan_exclusions_test.go` ×1) pinned `LivingExpensesTotal ==
  math.Abs(remainder)` for a refund-dominant remainder — the exact
  precondition CB7 removes. Re-pin to the SIGNED figures (-3000 /
  -2945.56), rename `...LivingEqualsAbsRemainder` →
  `...LivingEqualsSignedRemainder`, rewrite the prose that argued for
  Abs. Magnitudes unchanged (got == -want in all three) confirms the
  change is the intended sign, not new arithmetic.
- CB7-2026-09-03b — `TestHandleTransactionsPartial_RefundReducesTotalExpenses`
  (explorer) feeds a 7-row positive-convention CSV that the loader's
  sign-flip heuristic never normalizes, so internally purchases are
  positive; the old Abs masked that the fixture is non-canonical. Convert
  the fixture to the app's canonical convention (negate every amount:
  purchases negative, the refund +199.78), keep `want = 349.61` and the
  NetAmount assertion. The test's intent ("refund subtracts, not adds")
  is preserved. Not a CB7 arithmetic defect.
- CB7-2026-09-03c (attempt 2, CONCEDED checker-second FAIL) — `-set.SumAmount()`
  on a set whose sum is exactly 0.0 yields IEEE negative zero; `formatMoney`
  tests `v < 0` (false for -0) then `%.2f` (honors the sign bit) → the
  literal "$-0.00". On the live ledger every populated month/year has zero
  Health-Insurance rows, so the Monthly Healthcare KPI (kpis.html:105/173)
  would render "$-0.00" — a byte-identical-before/after violation
  (criterion e). The old Abs cleared the sign bit for free. Contract for
  attempt 3: ONE helper `metrics.SignedNet(ts) float64` = `-ts.SumAmount()`
  with -0 normalized to +0, used at EVERY negated-sum site in metrics.go
  (391, 422, 442, 455, 530, 536, 545, 618 — the CB2/SY4 per-month sites
  carry the same latent idiom and are folded in as the same single-source
  fix) and both explorer sites; plus `formatMoney` normalizes -0 to 0 as
  a formatter-layer belt. (Coverage claim corrected by checker-second at
  attempt 3: the belt covers the HTML modal but NOT the KPI CSV export,
  which is raw %.2f — so the KD-era dashboard/handlers.go sites are fixed
  at source in CB9, not left as backlog.) Boundary tests required at source (Signbit false),
  at JSON (no "-0" token), and at the formatter ("$0.00").
- CB8-2026-09-03a — velocity percent-change rule = CB3-c (|base|
  denominator, sign-of-change on zero base), stated above.

## Catches this run (mechanism attribution — the experiment's output)
- CB7 attempt 1: worker self-flagged four legacy tests pinning the old
  Abs contract (correct per brief; lead rulings a/b, not a defect).
- CB7 attempt 2: **checker-second (adversarial lane)** — negative zero
  "$-0.00" on the live Healthcare KPI, found by the real-ledger
  byte-identity sweep (17/17 populated windows). Neither the worker's
  mutation proofs nor checker-a11y's rendered probes had a zero-sum
  window. The mechanism was the "every populated range on real data"
  probe, not the diff.
- CB7 attempt 3: no catch; all three lanes PASS. checker-second's re-run
  sweep (24 windows, signbit + JSON-token checks) clean; it surfaced the
  CSV-export gap in the ruling's coverage claim → CB9.
- CB9 attempt 1 (lead-authored): **both lanes FAIL**, same ground —
  checker-tests (primary) found the fifth negated site via a wider grep
  and proved hcAmt/livingMonth observable in chart JSON; checker-second
  proved the same three chart-walk sites both-ends against the real
  classifier pipeline and found the velocity→MCP round2 `-0` leak. The
  lead's own mutation sanity had covered only the sites its grep found:
  the enumeration was the defect (lean-exception data point — non-author
  verification caught the lead's blind spot exactly as W4 predicted).
- CB8 attempt 1: no catch. checker-second PASS with live-ledger probe
  (HistoricalDaily 226.17 > 0, BurnRateChange 23.358011 byte-identical
  before/after); five mutants killed.

## Observations (backlog, not FAIL grounds)
- [checker-tests, CB9.2] `mcpsvc/spend/summary.go:91/121/144` `round2(-…)`
  per-row negations (by_category/by_merchant/by_month) can emit a JSON
  `-0` for an exactly-cancelling row; pre-existing, self-documented, a
  JSON consumer reads -0 == 0. Same class; route through SignedNet when
  next touching summarize_spending.
- [checker-tests, CB9.2] accumulators at `insights/trends.go:45/50` and
  dashboard `:403/:1598` are -0-safe (start at +0) — enumerated here so
  they are not re-audited.
- [checker-second, CB7] the registered budget2 MCP server still serves the
  OLD "always non-negative" wording — it runs a binary built from another
  worktree; rebuild/redeploy after merge (deploy note, not a code defect).
- [checker-second, CB7] `kpi-month-detail` handler's `-sumSigned(...)`
  (KD-2026-08-30d) has the same latent negative-zero idiom at source;
  covered on the rendered surface by the formatMoney belt, source fix is
  backlog.
- [checker-a11y, CB7] explorer chip's PRE-EXISTING positive-value
  `text-rose-600` / `text-emerald-600` at 14px measures ~4.28:1 on the
  tinted chips, below 4.5:1 AA — byte-identical to master (joins the
  NEXT.md tinted-band contrast backlog). CB7's new negative-value shades
  pass (6.99:1 light, 9.43–11.02:1 dark).
- [checker-second, CB8] `internal/services/mcpsvc/spend/trends.go:80-82`
  doc comment still describes the pre-CB8 unconditional formula for the
  velocity row (value is a correct passthrough). → CB9, Tier 1 doc fix.
- [checker-second, CB8] Near-zero-residual hazard shared with CB3-c: a
  ledger that truly nets to zero can leave a ~1e-10 float residual and
  take the division branch, yielding an astronomical percent. Inherited
  from the CB3-c rule CB8 was told to mirror; not introduced here. Fix
  candidate: compare |base| against a cent epsilon in BOTH classifiers.
