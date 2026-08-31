# SY4 — manifest (attempt 1)

## Summary

`metrics.Calculate` gains `planExclusions map[string]models.MajorExpense`
(nil-safe). Rows whose `Hash` is a key in the map are removed from the
living-expense figures ONLY (Monthly Living Expenses card, per-month living
rate/trend, budget cumulative variance, combined cumulative walk's living
share) via the new exported `metrics.PlanExcludedOutflows(outflows,
planExclusions)` helper — the single place the HI-first D-SY-e ordering
(skip HealthInsuranceCategory rows even if also flagged, so an overlap row
is claimed once, by HI, never double-subtracted) is implemented, consumed by
both `Calculate` and the dashboard's budget-vs-actual chart builder.
`TotalIncome`/`TotalExpenses`/`NetSavings`/`SavingsRate` are computed before
this point and are byte-for-byte unaffected. `DashboardMetrics` gains
`PlanExcludedTotal float64` / `PlanExcludedCount int`.

`metrics.Comparison` also gains the same param (threaded unmodified to both
of its internal `Calculate` calls) — its own living-actual computation feeds
kpis.html's "vs prior" deltas and is exactly the kind of surface criterion 3
says must not be missed.

All three call sites building the map do so via
`majorexpenses.ComputePlanSyncExclusions` over the FULL active/unfiltered
transaction set (never nil while defs exist, best-effort degrade to nil on a
load failure — same tolerance `bucketMajorExpenses`/`annotateMajorExpenses`
already use for the same file):

- `internal/handlers/dashboard/handlers.go`: new `planSyncExclusions(ts)`
  helper, called in `handleDashboard`, `handleKPIsPartial`, and the
  `budget-vs-actual` branch of `handleChartData`.
- `internal/services/mcpsvc/spend/register.go`: new `Deps.planSyncExclusions(ts)`
  method (mirrors `Deps.annotateMajorExpenses`), called from
  `internal/services/mcpsvc/spend/summary.go`'s `summarize_spending` handler.

No template file was changed — every consumer already renders from
`.Metrics`/`.BudgetVerdict` fields that are now pre-adjusted upstream, so no
new rendered annotation was needed (kept per the "prefer no template change"
constraint). **No a11y check is implicated.**

## Criterion-3 surface enumeration

Every living-actual-from-transactions computation site found by grepping
`internal/handlers/dashboard/`, `internal/services/mcpsvc/spend/`, and
(read-only, for completeness) the rest of the repo for
`metrics.Calculate|LivingExpensesTotal|ActualMonthly|BudgetTarget` usage,
plus `web/static/js` for any client-side reclassification:

| # | Surface | File / function | Status | Notes |
|---|---------|------------------|--------|-------|
| 1 | Dashboard page KPIs | `handlers.go: handleDashboard` | **TOUCHED** | `planSyncExclusions(data.Active())` built and passed to `metrics.Calculate` + `metrics.Comparison` |
| 2 | KPIs HTMX partial | `handlers.go: handleKPIsPartial` | **TOUCHED** | same as #1 |
| 3 | Budget-vs-actual chart data endpoint | `handlers.go: handleChartData` (`budget-vs-actual` case) | **TOUCHED** | `planSyncExclusions(data.Active())` passed to `buildBudgetVsActualChartData` |
| 4 | Budget-vs-actual chart builder (Living bar values + cumulative-balance walk) | `handlers.go: buildBudgetVsActualChartData` | **TOUCHED** | uses `metrics.PlanExcludedOutflows(outflows, planExclusions).GroupByMonth()`; subtracts per-month excluded amount from `livingMonth`, which feeds both the bar trace and the walk's `running` term |
| 5 | Verdict band model (Living/Healthcare buckets, Spent/Target, sentence) | `verdict.go: BuildBudgetVerdict` | **CLEARED, unmodified** | derives purely from already-adjusted `*models.DashboardMetrics` fields (`LivingExpensesTotal`, `CumulativeDelta`, etc.); no local transaction arithmetic to fix |
| 6 | KPI detail drilldown (income/expenses/savings/savings-rate) | `handlers.go: handleKPIDetail` | **CLEARED, unmodified** | only ever computes `TotalIncome`/`TotalExpenses`/`NetSavings`/`SavingsRate` per month — the fields the design explicitly leaves UNCHANGED; no "living-expenses" kpiType exists (the Monthly Living Expenses card links to the same generic `expenses` detail as Total Expenses, unchanged pre-existing behavior) |
| 7 | KPI CSV export | `handlers.go: handleKPIExport` | **CLEARED, unmodified** | same reasoning as #6 |
| 8 | Major-expense donut chart + drilldown | `handlers.go: bucketMajorExpenses`, `buildMajorExpenseChartData`, `handleMajorExpenseDrilldown` | **CLEARED, unmodified** | per-expense-group spend totals, not a living-vs-target comparison; the flag never changes how much was spent on a group, only whether it counts toward "living" |
| 9 | Spending-by-category trend chart | `handlers.go: buildSpendingTrendChartData` | **CLEARED, unmodified** | total spend by category, not living-budget |
| 10 | Merchants chart | `handlers.go: buildMerchantsChartData` | **CLEARED, unmodified** | total spend by merchant, not living-budget |
| 11 | Cumulative income-vs-expense chart | `handlers.go: buildCumulativeChartData` | **CLEARED, unmodified** | raw income/expense balance, not a living-vs-target comparison |
| 12 | `metrics.Comparison` ("vs prior" deltas: `ActualMonthlyChange`, `CumulativeDeltaChange`, rendered by kpis.html) | `metrics.go: Comparison` | **TOUCHED** | gained `planExclusions` param, threaded to both internal `Calculate` calls so current and comparison windows are classified identically |
| 13 | `summarize_spending` budget block | `summary.go` | **TOUCHED** | `Deps.planSyncExclusions(ts)` (new, `register.go`) built over the full active set before the window filter, passed to `metrics.Calculate` |
| 14 | Other MCP spend tools (`search_transactions`, `get_anomalies`, `get_price_creep`, `get_recurring`, `get_trends`) | `search.go`, `insights_tools.go`, `recurring.go`, `trends.go` | **CLEARED, unmodified** | grepped for `metrics.\|BudgetTarget\|LivingExpenses\|ActualMonthly` — no hits; none of these tools compute a living-vs-target figure at all |
| 15 | Client-side JS | `web/static/js/*` | **CLEARED, unmodified** | grepped for `living`/`Living` — no matches; no client-side reclassification exists to duplicate |
| 16 | `kpis.html` template | `web/templates/components/kpis.html` | **CLEARED, unmodified** | renders `.Metrics.LivingExpensesTotal`/`.ActualMonthly`/`.PerMonthDelta`/`.CumulativeDelta`/`.LivingExpensesTrend`/`.CombinedCumulativeDelta`/`.PeriodComparison.*` — all already adjusted upstream by `metrics.Calculate`/`Comparison`; no new annotation required so **no template edit made** |
| 17 | `dashboard-verdict-bar.html` template | `web/templates/components/dashboard-verdict-bar.html` | **CLEARED, unmodified** | same reasoning; `SpentTotal`/`Living.Delta`/`Healthcare.Delta` all derive from `BuildBudgetVerdict`, itself derived from already-adjusted metrics |
| 18 | `internal/handlers/whatif/sync.go` (`computeDashboardSync`) | out-of-territory, read-only check | **N/A, not this task's surface** | SY1's own, ALREADY-correct classifier for the sync TARGET side (D-SY-b); confirmed (via repo-wide grep for `LivingExpensesTotal`/`ActualMonthly`) that it does not import or duplicate `metrics.Calculate`'s arithmetic — a separate, pre-existing computation this task does not touch |

## Verification

```
$ go build ./...
(clean)

$ go vet ./...
(clean)

$ gofmt -l internal/services/metrics/metrics.go internal/services/metrics/plan_exclusions_test.go \
    internal/services/metrics/metrics_test.go internal/models/dashboard.go \
    internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go \
    internal/handlers/dashboard/plan_exclusions_render_test.go internal/handlers/dashboard/plan_exclusions_chart_test.go \
    internal/services/mcpsvc/spend/register.go internal/services/mcpsvc/spend/summary.go \
    internal/services/mcpsvc/spend/plan_exclusions_test.go internal/services/dataloader/transfers_test.go
(clean, no output)

$ go test ./internal/services/metrics/... ./internal/handlers/dashboard/... ./internal/services/mcpsvc/spend/... -count=1
ok  	budget2/internal/services/metrics	0.003s
ok  	budget2/internal/handlers/dashboard	0.566s
ok  	budget2/internal/services/mcpsvc/spend	0.645s

$ go test ./... -count=1   (whole tree, including other runs' dirty files riding along)
(all packages ok, zero non-"ok" lines)
```

New/extended tests and what they cover (acceptance criterion 5):

- `internal/services/metrics/plan_exclusions_test.go`
  - `TestCalculateMetrics_PlanExclusions_DropsLivingByExactFlaggedNetTotalExpensesUnchanged` — 5(a)
  - `TestCalculateMetrics_PlanExclusions_NilMapEqualsEmptyMapEqualsPreSY4Fields` — 5(b), `reflect.DeepEqual` golden comparison (nil map, empty map, and a map with a non-matching Hash all produce byte-identical `*DashboardMetrics`)
  - `TestCalculateMetrics_PlanExclusions_HIOverlapExcludedOnceNotDoubleSubtracted` — D-SY-e ordering mirrored into `Calculate`
  - `TestCalculateMetrics_PlanExclusions_LivingTrendExcludesFlaggedMonth` — `LivingExpensesTrend` per-month array
  - `TestCalculateMetrics_PlanExclusions_CombinedCumulativeBalanceInvariantHolds` — the walk's independent "spend" subtraction, checked against the documented `last == -CombinedCumulativeDelta` invariant
  - `TestComparison_PlanExclusionsAppliedToBothWindows` — criterion 3, surface #12
- `internal/handlers/dashboard/plan_exclusions_render_test.go`
  - `TestDashboardVerdictBar_RenderedFiguresReflectPlanExclusion` — criterion 4 (rendered-string arithmetic, ruling 2026-08-29b): real `TransactionSet` + flagged def + fractional-cent amounts run through the REAL `metrics.Calculate` → `BuildBudgetVerdict` → `dashboard-verdict-bar` render pipeline; asserts the rendered "Spent" figure reflects the exclusion AND that rendered Living + Healthcare still sum to the rendered combined total
- `internal/handlers/dashboard/plan_exclusions_chart_test.go`
  - `TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative` — criterion 3/5(d), surface #4
- `internal/services/mcpsvc/spend/plan_exclusions_test.go`
  - `TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion` — criterion 5(c), surface #13; includes a same-fixture "MajorExpenses not wired" control showing the unexcluded figure, so the assertion is proven to actually discriminate

Both-ends calibration performed on the three new "arithmetic" tests (not
just the render one) by temporarily reverting the relevant subtraction line
and re-running — each failed with the expected number (e.g. rendered `Spent
= 2705.24, want 2104.83`; `budget.living_monthly_actual = 2061.9, want
1472.78`), then the code was restored and reverified green. The chart test's
break attempt failed to even COMPILE (`declared and not used: excludedAmt`),
confirming the subtraction is load-bearing there, not decorative.

## Notes for the checker

- `internal/services/dataloader/transfers_test.go` is listed as HC/foreign
  territory in `.swarm/SY-RUN-SPEC.md`'s concurrency-era fence, but that
  fence predates HC's merge (master is now `4c3b65e`, HC territory is no
  longer "live"). Its two `metrics.Calculate(...)` calls needed a mechanical
  `, nil` appended to keep the package compiling under the new signature —
  no behavior change, no new test, not a scope expansion.
- `internal/models/dashboard.go` had one pre-existing `gofmt -l` finding
  (an unrelated struct-field-comment alignment group, `CombinedTarget` et
  al.) already present on `master`/before this attempt (`git stash` +
  `gofmt -l` reproduces it on a clean tree). Since the verify step requires
  `gofmt -l` clean on every touched file and this file is now touched, it
  was `gofmt -w`'d; the diff is alignment-only and untouched by my new
  field block.
- No `.html` template file was changed (see enumeration rows 16–17), so no
  a11y check is implicated by this task.
