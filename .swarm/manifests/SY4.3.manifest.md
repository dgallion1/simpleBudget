# SY4 — manifest (attempt 3, last before hard stop)

Attempt 2 was rejected: checker-second proved the signed-net fix
algebraically incomplete. Ruling **SY-2026-08-30d** (lead concedes,
CONTRACT DEFECT — T18 rule, second consecutive failure to the same root
class means the defect is the lead's contract, not the worker's
implementation) rewrites the contract entirely: **set exclusion, not
arithmetic subtraction**. This attempt implements exactly that.

## The defect attempts 1+2 shared

Both prior attempts kept the shape `Abs(everything) - <exclusion amount>`.
Attempt 1's exclusion amount was `Abs(flagged net)` (wrong sign when the
flagged group nets a refund). Attempt 2's was `-flagged.SumAmount()`
(signed, but still SUBTRACTED from an already-Abs'd total). Ruling d's
probe shows attempt 2 is *still* wrong: remainder S = -1000 grocery + 4000
outflow-typed credit (net +3000), flagged F = one ordinary -500 car
payment → attempt 2 computes `|S+F|+F` = `|3000-500|-(-500)` = 2000+500 =
**wait, the exact shipped defect value was `|S+F|+F` = |2500|+(-500) =
2000** (matches the ruling's stated observed value), when the correct
answer is `|S|` = **3000**. The REMAINDER's own sign was never exercised by
attempts 1-2's fixtures — only the flagged group's sign was.

## The rewrite

New exported `metrics.LivingOutflows(outflows, planExclusions)`
(`internal/services/metrics/metrics.go`): returns the outflow SET with
`HealthInsuranceCategory` rows and flagged (non-HI) rows removed —
built once, then the ORDINARY pre-existing `math.Abs(...SumAmount())`
arithmetic runs directly on it, at every granularity:

- Range total: `livingTotal := math.Abs(livingOutflows.SumAmount())`
  (replaces `totalExpenses - healthcareTotal - planExcludedTotal` entirely;
  `healthcareTotal`'s own computation is untouched, used only by the
  Healthcare KPI fields).
- Per-month trend (`LivingExpensesTrend`): `math.Abs(monthlyLiving[m].SumAmount())`
  via `livingOutflows.GroupByMonth()`.
- Combined cumulative walk: `spend := math.Abs(monthlyNonExcluded[...].SumAmount())`,
  where `nonExcludedOutflows` is `livingOutflows.Transactions` merged with
  `healthcareOutflows.Transactions` (every outflow except plan-sync-excluded
  rows — HI stays in, matching the walk's pre-SY4 living+healthcare-combined
  basis) — built by concatenating two ALREADY-classified sets, not a third
  independent classifier.
- `internal/handlers/dashboard/handlers.go`'s `buildBudgetVsActualChartData`
  mirrors the same pattern: `metrics.LivingOutflows(outflows, planExclusions)`
  → `monthlyLiving` → `math.Abs(...)` for the Living bar; the chart's own
  cumulative walk composes `livingMonth + hcAmt` (already correct once
  `livingMonth` is fixed — no separate nonExcluded merge needed there, since
  that walk was never built from a raw monthlyOutflows lookup in the first
  place).

`PlanExcludedTotal`/`PlanExcludedCount` (`metrics.PlanExcludedOutflows`,
unchanged signed-net convention from attempt 2) are now **strictly
display-only annotation data** — computed once, stored on
`DashboardMetrics`, and never read again inside any living-arithmetic
formula. Verified by grep (see below): `PlanExcluded`/`planExcluded`
appears in `metrics.go` only inside `PlanExcludedOutflows` itself, the
annotation-computation block, and the final struct literal; `handlers.go`
and `summary.go`/`register.go` have **zero** occurrences (fully replaced by
`LivingOutflows`).

```
$ grep -n "PlanExcluded\|planExcluded" internal/services/metrics/metrics.go
  (only: PlanExcludedOutflows func + its doc, the annotation block at
   ~412-424, and the DashboardMetrics struct-literal fields at ~621-622)
$ grep -n "PlanExcluded\|planExcluded" internal/handlers/dashboard/handlers.go
  (no matches)
$ grep -n "PlanExcluded\|planExcluded" internal/services/mcpsvc/spend/summary.go internal/services/mcpsvc/spend/register.go
  (no matches)
```

Nil/empty `planExclusions` reproduces master behavior byte-identical BY
CONSTRUCTION: `LivingOutflows` with `planExclusions == nil` is exactly the
HI filter alone (the map lookup `planExclusions[t.Hash]` on a nil map
always misses), so `math.Abs(LivingOutflows(outflows, nil).SumAmount())`
is the same set master/attempt-1/2 all computed for the no-flag case. The
golden `reflect.DeepEqual` test (`TestCalculateMetrics_PlanExclusions_
NilMapEqualsEmptyMapEqualsPreSY4Fields`, kept from attempt 1) still passes
unmodified.

## Both probes verified through the real Calculate

```
# (a) attempt 1/2's flagged-net-refund probe (ruling c) — still correct:
LivingExpensesTotal=3000 want 3000; PlanExcludedTotal=-500 want -500
--- PASS: TestProbeFlaggedNetRefundStillCorrect

# (b) the NEW remainder-refund probe (ruling d), verbatim:
LivingExpensesTotal=3000 want 3000; PlanExcludedTotal=500 want 500
--- PASS: TestProbeRemainderNetRefund
```

(Both probes were run as throwaway `zz_probe*_test.go` files during
development and removed before the final commit-ready state; the permanent
coverage for (b) lives in the new test files below.)

## New tests: remainder-sign-divergent fixture per consumer package

All attempt-1 and attempt-2 tests are **kept, unmodified** (23 tests across
the three packages, all still passing — see the full pass list below).
New for attempt 3:

- `internal/services/metrics/plan_exclusions_remainder_test.go`:
  - `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder`
    — the ruling's exact probe through `Calculate`.
  - `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend`
    — per-month trend array.
  - `TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows`
    — both comparison windows.
  - **Omitted deliberately**: a `CombinedCumulativeBalance` invariant test
    for the remainder fixture. See "Scope note" below.
- `internal/handlers/dashboard/plan_exclusions_chart_test.go` (extended):
  `TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder`.
- `internal/services/mcpsvc/spend/plan_exclusions_test.go` (extended):
  `TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder`.

### Scope note: CombinedCumulativeBalance + the remainder fixture

A remainder-refund fixture large enough to net positive (the ruling's
probe uses a +4000 outflow-typed credit) necessarily also flips that
CALENDAR MONTH's raw combined (living+healthcare+flagged) sum positive,
which breaks the walk's own **pre-existing, master-native** precondition
that "per-month `|sum|` partitions range-level `|sum|`" — a precondition
`Abs`-of-parts only satisfies when every part shares one sign. Verified
BOTH-ENDS that this is NOT an SY4 regression: the identical fixture with
`planExclusions=nil` trips the SAME invariant break:

```
last CombinedCumulativeBalance point = -995.48, want -CombinedCumulativeDelta = 1204.52
--- FAIL: TestProbeInvariantWithoutFlag (planExclusions=nil, no SY4 involvement at all)
```

This is the same defect class ruling SY-2026-08-30d already declared out
of scope ("the pre-existing HI-abs quirk ... is master-native and out of
scope") — here triggered by a non-HI outflow-typed credit instead of an HI
one. No walk-invariant test was added for the remainder fixture; the
flagged-sign-divergent walk-invariant test kept from attempt 2
(`TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds`,
whose fixture never flips a month's raw sign) still covers the walk's own
plan-exclusion arithmetic and passes.

## Both-ends calibration (temporarily restore attempt-2's subtraction
shape, confirm the exact 2000-vs-3000-class failure, then re-fix)

Each new remainder test was calibrated by patching the relevant file back
to attempt 2's `Abs(everything) - <signed exclusion>` shape, confirming
the failure, then restoring from a pre-patch copy (`diff` clean after
restore):

```
# metrics package (livingTotal reverted to totalExpenses - healthcareTotal
# - planExcludedTotal; per-month trend reverted to expAmt - hcAmt -
# excludedAmt):
--- FAIL: TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder
    LivingExpensesTotal = 2000, want 3000 (|remainder| exactly; ...)
--- FAIL: TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend
    LivingExpensesTrend[Jan] = 2000, want 3000 (|remainder| exactly)
--- FAIL: TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows
    Current.LivingExpensesTotal = 2000, want 3000 (|remainder| exactly)
    Previous.LivingExpensesTotal = 2000, want 3000
$ diff metrics.go.v3bak metrics.go && echo IDENTICAL   # after restore
IDENTICAL

# dashboard package (buildBudgetVsActualChartData's livingMonth reverted):
--- FAIL: TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder
    trace[0].y (Living) = [2000 1500], want [3000 1500] (...)
$ diff handlers.go.v3bak handlers.go && echo IDENTICAL   # after restore
IDENTICAL

# mcpsvc/spend package (via metrics.go's livingTotal reverted, transitively):
--- FAIL: TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder
    budget.living_monthly_actual = 1963.71, want ~2945.56 (|remainder| exactly)
$ diff metrics.go.v3bak2 metrics.go && echo IDENTICAL   # after restore
IDENTICAL
```

## Attempt-1/2 wiring tests and other coverage — kept, still green

`TestHandleDashboard_PlanExclusionWiring`,
`TestHandleKPIsPartial_PlanExclusionWiring`,
`TestHandleChartData_BudgetVsActual_PlanExclusionWiring` (attempt 2's real-
HTTP wiring tests, mutant-calibrated then) all pass unmodified against the
attempt-3 rewrite — `handleDashboard`/`handleKPIsPartial`/`handleChartData`
still build `planExclusions` via `planSyncExclusions` and pass it through
unchanged; only the ARITHMETIC downstream of that map changed.

## Criterion-3 surface enumeration (unchanged from attempts 1-2 — the
rewrite changed HOW each already-identified surface applies the exclusion,
not WHICH surfaces need it)

| # | Surface | File / function | Status |
|---|---------|------------------|--------|
| 1 | Dashboard page KPIs | `handlers.go: handleDashboard` | **TOUCHED** — wiring unchanged, arithmetic now via `metrics.Calculate`'s rewritten body |
| 2 | KPIs HTMX partial | `handlers.go: handleKPIsPartial` | **TOUCHED** — same |
| 3 | Budget-vs-actual chart data endpoint | `handlers.go: handleChartData` (`budget-vs-actual`) | **TOUCHED** — same |
| 4 | Budget-vs-actual chart builder | `handlers.go: buildBudgetVsActualChartData` | **TOUCHED** — rewritten to `metrics.LivingOutflows` + set-based `math.Abs` |
| 5 | Verdict band model | `verdict.go: BuildBudgetVerdict` | **CLEARED, unmodified** — derives from already-corrected `DashboardMetrics` fields |
| 6 | KPI detail drilldown | `handlers.go: handleKPIDetail` | **CLEARED, unmodified** — full-set totals, untouched by design |
| 7 | KPI CSV export | `handlers.go: handleKPIExport` | **CLEARED, unmodified** |
| 8 | Major-expense donut chart + drilldown | `handlers.go: bucketMajorExpenses` etc. | **CLEARED, unmodified** — not a living-vs-target surface |
| 9 | Spending-by-category trend chart | `handlers.go: buildSpendingTrendChartData` | **CLEARED, unmodified** |
| 10 | Merchants chart | `handlers.go: buildMerchantsChartData` | **CLEARED, unmodified** |
| 11 | Cumulative income-vs-expense chart | `handlers.go: buildCumulativeChartData` | **CLEARED, unmodified** |
| 12 | `metrics.Comparison` | `metrics.go: Comparison` | **TOUCHED** (arithmetic only, via `Calculate`) |
| 13 | `summarize_spending` budget block | `summary.go` | **TOUCHED** (arithmetic only, via `Calculate`) |
| 14 | Other MCP spend tools | `search.go`, `insights_tools.go`, `recurring.go`, `trends.go` | **CLEARED, unmodified** |
| 15 | Client-side JS | `web/static/js/*` | **CLEARED, unmodified** |
| 16 | `kpis.html` template | `web/templates/components/kpis.html` | **CLEARED, unmodified** — no template edit made |
| 17 | `dashboard-verdict-bar.html` template | `web/templates/components/dashboard-verdict-bar.html` | **CLEARED, unmodified** |
| 18 | `internal/handlers/whatif/sync.go` | out-of-territory, read-only check | **N/A** — SY1's own, already-signed classifier for the sync TARGET side, untouched |

## Verification

```
$ go build ./...
(clean)

$ go vet ./...
(clean)

$ gofmt -l <every file in SY4.3.files>
(clean, no output)

$ go test ./internal/services/metrics/... ./internal/handlers/dashboard/... ./internal/services/mcpsvc/spend/... -count=1
ok  	budget2/internal/services/metrics	0.004s
ok  	budget2/internal/handlers/dashboard	0.633s
ok  	budget2/internal/services/mcpsvc/spend	0.669s

$ go test ./... -count=1   (whole tree, other runs' dirty files riding along)
(all packages ok, zero non-"ok" lines)
```

Full pass list (attempts 1+2 tests kept + attempt 3's new remainder tests):

```
--- PASS: TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder
--- PASS: TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend
--- PASS: TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundGroupAddsBackNotSubtracts          [attempt 2, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundMonthInLivingTrend                 [attempt 2, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds [attempt 2, kept]
--- PASS: TestComparison_PlanExclusions_NetRefundAppliedToBothWindows                      [attempt 2, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_DropsLivingByExactFlaggedNetTotalExpensesUnchanged [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_NilMapEqualsEmptyMapEqualsPreSY4Fields       [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_HIOverlapExcludedOnceNotDoubleSubtracted     [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_LivingTrendExcludesFlaggedMonth              [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_CombinedCumulativeBalanceInvariantHolds      [attempt 1, kept]
--- PASS: TestComparison_PlanExclusionsAppliedToBothWindows                                [attempt 1, kept]
ok  	budget2/internal/services/metrics	0.004s
--- PASS: TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative [attempt 1, kept]
--- PASS: TestBuildBudgetVsActualChartData_NetRefundGroupAddsBackNotSubtracts              [attempt 2, kept]
--- PASS: TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder
--- PASS: TestDashboardVerdictBar_RenderedFiguresReflectPlanExclusion                      [attempt 1, kept]
--- PASS: TestDashboardVerdictBar_RenderedSpentReflectsNetRefundExclusion                  [attempt 2, kept]
--- PASS: TestHandleDashboard_PlanExclusionWiring                                          [attempt 2, kept]
--- PASS: TestHandleKPIsPartial_PlanExclusionWiring                                        [attempt 2, kept]
--- PASS: TestHandleChartData_BudgetVsActual_PlanExclusionWiring                           [attempt 2, kept]
ok  	budget2/internal/handlers/dashboard	0.633s
--- PASS: TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion                        [attempt 1, kept]
--- PASS: TestSummarizeSpendingBudgetBlockNetRefundGroupAddsBackNotSubtracts               [attempt 2, kept]
--- PASS: TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder
ok  	budget2/internal/services/mcpsvc/spend	0.669s
```

## Notes for the checker

- No `.html` template file was changed — no a11y check is implicated.
- `internal/services/dataloader/transfers_test.go`'s mechanical `, nil`
  call-site edit is unchanged from attempts 1-2 and still required for the
  package to compile.
- The untracked `budget2.old-1345` file noted in attempt 2's reply is
  unrelated to this work (stray environment artifact, not touched).
