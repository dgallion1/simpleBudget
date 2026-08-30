# SY4 — manifest (attempt 2)

Attempt 1 was rejected: checker-second FAIL (adversarial lane), conceded
lead-side as ruling **SY-2026-08-30c** — every plan-exclusion consumer
subtracted `math.Abs(planExcludedSet.SumAmount())` from an already-absolute
living total, which is wrong whenever the flagged group is in NET REFUND
(refunds exceed payments): the ruling's probe (flagged $2,000 payment +
$2,500 refund beside $3,000 rent) produced `LivingExpensesTotal 2000`,
wanted `3000`. checker-tests' F1 (folded into the same attempt) additionally
required a handler-level wiring test per dashboard call site, since no
attempt-1 test executed `handleDashboard`/`handleKPIsPartial`/
`handleChartData`'s own map-BUILDING code.

This attempt does two things. **All attempt-1 tests are kept, unmodified —
this attempt only extends coverage.**

## 1. Signed-net fix (the defect)

Every site that computed the flagged group's contribution via
`math.Abs(...SumAmount())` now uses the SIGNED net (`-...SumAmount()`),
matching the SY1 `Total += -t.Amount` convention already used in
`internal/handlers/whatif/sync.go`'s `ExcludedGroups` (positive = net
spend, negative = net refund). Grepped every `planExcluded*` site in the
touched packages (not trusting the dispatch message's list) — three sites
in `internal/services/metrics/metrics.go`, one in
`internal/handlers/dashboard/handlers.go`; `internal/services/mcpsvc/spend/summary.go`
has no separate Abs call of its own (it only consumes `metrics.Calculate`'s
now-fixed output), so it is fixed transitively:

| Site | Before | After |
|---|---|---|
| `metrics.go` `Calculate`, range total | `planExcludedTotal := math.Abs(planExcludedSet.SumAmount())` | `planExcludedTotal := -planExcludedSet.SumAmount()` |
| `metrics.go` `Calculate`, per-month trend (`LivingExpensesTrend`) | `excludedAmt = math.Abs(pe.SumAmount())` | `excludedAmt = -pe.SumAmount()` |
| `metrics.go` `Calculate`, combined cumulative walk (`CombinedCumulativeBalance`) | `spend -= math.Abs(pe.SumAmount())` | `spend -= -pe.SumAmount()` |
| `handlers.go` `buildBudgetVsActualChartData`, per-month Living bar | `excludedAmt = math.Abs(pe.SumAmount())` | `excludedAmt = -pe.SumAmount()` |

`grep -rn "math.Abs" internal/services/metrics/metrics.go internal/handlers/dashboard/handlers.go`
after the fix shows `math.Abs` remaining ONLY on `healthcareTotal`/`hcAmt`/
`expAmt` (pre-existing, out of SY4's scope — the healthcare split is HC's,
unchanged) and on the unrelated `totalExpenses`/general-outflow figures —
zero remaining `math.Abs` calls on any `planExcluded*`/`pe.SumAmount()`
value. `DashboardMetrics.PlanExcludedTotal`'s doc comment now states the
sign convention explicitly (positive = net spend, negative = net refund,
never `math.Abs`), and `Calculate`'s own doc comment gained the same note.

Algebra re-verified for the signed formula (both `LivingExpensesTotal` and
the `CombinedCumulativeBalance` walk's documented invariant `last ==
-CombinedCumulativeDelta` — re-derived by hand and confirmed by
`TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds`).

## 2. Handler wiring tests (checker-tests F1)

New `internal/handlers/dashboard/plan_exclusions_wiring_test.go` drives the
three dashboard call sites through their REAL HTTP handlers (httptest),
against a fixture data directory (`writeTempCSV` + `dl.SaveMajorExpenses`
with a flagged, keyword-matched "Lucid Loan" def + a $1200/mo living target
via a real `retirement.SettingsManager`), asserting the exclusion is visible
in each site's actual output:

- `TestHandleDashboard_PlanExclusionWiring` — GET `/dashboard` with a real
  renderer; asserts the rendered "Monthly Living Expenses" card figure.
- `TestHandleKPIsPartial_PlanExclusionWiring` — GET `/dashboard/kpis` with
  no renderer (JSON fallback); asserts `Metrics.living_expenses_total`.
- `TestHandleChartData_BudgetVsActual_PlanExclusionWiring` — GET
  `/dashboard/charts/data/budget-vs-actual` (always JSON); asserts the
  "Living" trace's January value.

`internal/services/mcpsvc/spend/plan_exclusions_test.go`'s
`TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion` already drove
the real MCP tool end to end with a "MajorExpenses not wired" control (its
own mutant), per the dispatch note that the spend-summary site already had
this — no new wiring test needed there, only the sign-fix test (below).

### Mutant calibration (one at a time, restored after each)

| Mutant | Site | Line mutated | Result |
|---|---|---|---|
| B_dash | `handleDashboard`'s `planExclusions := planSyncExclusions(data.Active())` → `nil` | handlers.go:129 | `TestHandleDashboard_PlanExclusionWiring` **FAILS**; `TestHandleKPIsPartial_PlanExclusionWiring` and `TestHandleChartData_BudgetVsActual_PlanExclusionWiring` still PASS |
| B_kpis | `handleKPIsPartial`'s same line → `nil` | handlers.go:218 | `TestHandleKPIsPartial_PlanExclusionWiring` **FAILS**; the other two still PASS |
| B_chart | `handleChartData`'s `budget-vs-actual` branch → `nil` | handlers.go:283 | `TestHandleChartData_BudgetVsActual_PlanExclusionWiring` **FAILS**; the other two still PASS |

Each mutant was applied via `sed` on the exact source line, `go build
./...` confirmed the mutant compiles, the targeted wiring test run showed
exactly the one expected failure (pasted below), then the file was restored
from a pre-edit copy and `go build ./...` reconfirmed byte-identical to the
pre-mutation source (`diff` clean).

```
$ sed -n '129p' handlers.go  →  planExclusions := map[string]models.MajorExpense(nil) // MUTANT-B_dash
--- FAIL: TestHandleDashboard_PlanExclusionWiring (0.03s)
--- PASS: TestHandleKPIsPartial_PlanExclusionWiring (0.01s)
--- PASS: TestHandleChartData_BudgetVsActual_PlanExclusionWiring (0.01s)

$ sed -n '218p' handlers.go  →  planExclusions := map[string]models.MajorExpense(nil) // MUTANT-B_kpis
--- PASS: TestHandleDashboard_PlanExclusionWiring (0.03s)
--- FAIL: TestHandleKPIsPartial_PlanExclusionWiring (0.01s)
--- PASS: TestHandleChartData_BudgetVsActual_PlanExclusionWiring (0.01s)

$ sed -n '283p' handlers.go  →  planExclusions := map[string]models.MajorExpense(nil) // MUTANT-B_chart
--- PASS: TestHandleDashboard_PlanExclusionWiring (0.03s)
--- PASS: TestHandleKPIsPartial_PlanExclusionWiring (0.01s)
--- FAIL: TestHandleChartData_BudgetVsActual_PlanExclusionWiring (0.01s)

$ diff handlers.go.wiringbak handlers.go && echo IDENTICAL   # after restoring all three
IDENTICAL
```

### Mutant/break calibration for the sign fix (both-ends)

Each new sign-divergent test was also run against the attempt-1 `math.Abs`
code (reverted, then restored) to confirm the RIGHT failure:

```
# metrics package (all three math.Abs sites reverted together):
--- FAIL: TestCalculateMetrics_PlanExclusions_NetRefundGroupAddsBackNotSubtracts
    LivingExpensesTotal = 2000, want 3000 (unflagged rent only ...)
    PlanExcludedTotal = 500, want -500 (signed net refund, never math.Abs)
--- FAIL: TestCalculateMetrics_PlanExclusions_NetRefundMonthInLivingTrend
    LivingExpensesTrend[Jan] = 700, want 1500 (...)
--- FAIL: TestComparison_PlanExclusions_NetRefundAppliedToBothWindows
    Current.LivingExpensesTotal = 700, want 1500 (...)
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds
    (expected: this test only guards internal CONSISTENCY between the walk
    and CombinedCumulativeDelta -- reverting all three sites TOGETHER keeps
    them mutually consistent, so this specific test doesn't discriminate
    the sign bug on its own; the other three tests in the same run do.)

# dashboard package (handlers.go's site reverted):
--- FAIL: TestBuildBudgetVsActualChartData_NetRefundGroupAddsBackNotSubtracts
    trace[0].y (Living) = [700 1500], want [1500 1500] (...)
    trace[2].y (cumulative) = [450 100], want [-350 -700] (...)

# mcpsvc/spend package (via metrics.go's site reverted, transitively):
--- FAIL: TestSummarizeSpendingBudgetBlockNetRefundGroupAddsBackNotSubtracts
    budget.living_monthly_actual = 687.3, want ~1472.78 (...)

# The exact ruling probe, reproduced directly against the FIXED code:
LivingExpensesTotal=3000 want 3000; PlanExcludedTotal=-500 want -500
--- PASS: TestProbeSignedNetRefund
```

All reverts were restored (`diff` against the pre-mutation copy is clean;
`go build ./...` green) before the final verification pass below.

## Criterion-3 surface enumeration (unchanged from attempt 1 — the sign fix
and wiring tests did not change which surfaces need the exclusion, only how
correctly/how-tested each already-identified surface applies it)

| # | Surface | File / function | Status | Notes |
|---|---------|------------------|--------|-------|
| 1 | Dashboard page KPIs | `handlers.go: handleDashboard` | **TOUCHED** | `planSyncExclusions(data.Active())` built and passed to `metrics.Calculate` + `metrics.Comparison`; now covered by a real-HTTP wiring test |
| 2 | KPIs HTMX partial | `handlers.go: handleKPIsPartial` | **TOUCHED** | same as #1; now covered by a real-HTTP wiring test |
| 3 | Budget-vs-actual chart data endpoint | `handlers.go: handleChartData` (`budget-vs-actual` case) | **TOUCHED** | `planSyncExclusions(data.Active())` passed to `buildBudgetVsActualChartData`; now covered by a real-HTTP wiring test |
| 4 | Budget-vs-actual chart builder (Living bar values + cumulative-balance walk) | `handlers.go: buildBudgetVsActualChartData` | **TOUCHED** | signed-net subtraction (attempt-2 fix); sign-divergent fixture added |
| 5 | Verdict band model (Living/Healthcare buckets, Spent/Target, sentence) | `verdict.go: BuildBudgetVerdict` | **CLEARED, unmodified** | derives purely from already-adjusted `*models.DashboardMetrics` fields; sign-divergent render test added to prove the rendered "Spent" figure is correct too |
| 6 | KPI detail drilldown (income/expenses/savings/savings-rate) | `handlers.go: handleKPIDetail` | **CLEARED, unmodified** | only `TotalIncome`/`TotalExpenses`/`NetSavings`/`SavingsRate` — explicitly UNCHANGED by design |
| 7 | KPI CSV export | `handlers.go: handleKPIExport` | **CLEARED, unmodified** | same reasoning as #6 |
| 8 | Major-expense donut chart + drilldown | `handlers.go: bucketMajorExpenses`, `buildMajorExpenseChartData`, `handleMajorExpenseDrilldown` | **CLEARED, unmodified** | per-expense-group spend totals, not a living-vs-target comparison |
| 9 | Spending-by-category trend chart | `handlers.go: buildSpendingTrendChartData` | **CLEARED, unmodified** | total spend by category, not living-budget |
| 10 | Merchants chart | `handlers.go: buildMerchantsChartData` | **CLEARED, unmodified** | total spend by merchant, not living-budget |
| 11 | Cumulative income-vs-expense chart | `handlers.go: buildCumulativeChartData` | **CLEARED, unmodified** | raw income/expense balance |
| 12 | `metrics.Comparison` ("vs prior" deltas) | `metrics.go: Comparison` | **TOUCHED** | `planExclusions` threaded to both internal `Calculate` calls (attempt 1); sign-divergent fixture added (attempt 2) |
| 13 | `summarize_spending` budget block | `summary.go` | **TOUCHED** | `Deps.planSyncExclusions(ts)` (attempt 1); fixed transitively by the metrics-layer sign fix; sign-divergent fixture added |
| 14 | Other MCP spend tools | `search.go`, `insights_tools.go`, `recurring.go`, `trends.go` | **CLEARED, unmodified** | no living-vs-target arithmetic at all |
| 15 | Client-side JS | `web/static/js/*` | **CLEARED, unmodified** | no client-side reclassification exists |
| 16 | `kpis.html` template | `web/templates/components/kpis.html` | **CLEARED, unmodified** | no new annotation required; no template edit made |
| 17 | `dashboard-verdict-bar.html` template | `web/templates/components/dashboard-verdict-bar.html` | **CLEARED, unmodified** | same reasoning |
| 18 | `internal/handlers/whatif/sync.go` (`computeDashboardSync`) | out-of-territory, read-only check | **N/A** | SY1's own, already-correct SIGNED classifier for the sync TARGET side (the `Total += -t.Amount` convention this attempt's fix now matches) |

## Verification

```
$ go build ./...
(clean)

$ go vet ./...
(clean)

$ gofmt -l <every file in SY4.2.files>
(clean, no output)

$ go test ./internal/services/metrics/... ./internal/handlers/dashboard/... ./internal/services/mcpsvc/spend/... -count=1
ok  	budget2/internal/services/metrics	0.004s
ok  	budget2/internal/handlers/dashboard	0.667s
ok  	budget2/internal/services/mcpsvc/spend	0.706s

$ go test ./... -count=1   (whole tree, other runs' dirty files riding along)
(all packages ok, zero non-"ok" lines)
```

Full pass output for the new/extended tests:

```
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundGroupAddsBackNotSubtracts (0.00s)
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundMonthInLivingTrend (0.00s)
--- PASS: TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds (0.00s)
--- PASS: TestComparison_PlanExclusions_NetRefundAppliedToBothWindows (0.00s)
--- PASS: TestCalculateMetrics_PlanExclusions_DropsLivingByExactFlaggedNetTotalExpensesUnchanged (0.00s)   [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_NilMapEqualsEmptyMapEqualsPreSY4Fields (0.00s)               [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_HIOverlapExcludedOnceNotDoubleSubtracted (0.00s)             [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_LivingTrendExcludesFlaggedMonth (0.00s)                      [attempt 1, kept]
--- PASS: TestCalculateMetrics_PlanExclusions_CombinedCumulativeBalanceInvariantHolds (0.00s)              [attempt 1, kept]
--- PASS: TestComparison_PlanExclusionsAppliedToBothWindows (0.00s)                                        [attempt 1, kept]
ok  	budget2/internal/services/metrics	0.004s
--- PASS: TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative (0.00s) [attempt 1, kept]
--- PASS: TestBuildBudgetVsActualChartData_NetRefundGroupAddsBackNotSubtracts (0.00s)
--- PASS: TestDashboardVerdictBar_RenderedFiguresReflectPlanExclusion (0.01s)                              [attempt 1, kept]
--- PASS: TestDashboardVerdictBar_RenderedSpentReflectsNetRefundExclusion (0.01s)
--- PASS: TestHandleDashboard_PlanExclusionWiring (0.02s)
--- PASS: TestHandleKPIsPartial_PlanExclusionWiring (0.01s)
--- PASS: TestHandleChartData_BudgetVsActual_PlanExclusionWiring (0.01s)
ok  	budget2/internal/handlers/dashboard	0.667s
--- PASS: TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion (0.01s)                                [attempt 1, kept]
--- PASS: TestSummarizeSpendingBudgetBlockNetRefundGroupAddsBackNotSubtracts (0.01s)
ok  	budget2/internal/services/mcpsvc/spend	0.706s
```

## Notes for the checker

- No `.html` template file was changed — no a11y check is implicated.
- `internal/services/dataloader/transfers_test.go`'s mechanical `, nil`
  call-site edit (from attempt 1) is unchanged and still required for the
  package to compile under `metrics.Calculate`'s new signature; per the
  dispatch note it stands.
- `internal/models/dashboard.go`'s pre-existing unrelated gofmt finding
  (attempt 1) remains fixed (alignment-only, untouched by this attempt's
  new doc comment).
