# KD1 attempt 1 — living/healthcare KPI detail kinds

## Extracted shared helper

`dashboardCalcInputs` (struct) + `gatherDashboardCalcInputs(settings, active, startDate, endDate) dashboardCalcInputs`
— `internal/handlers/dashboard/handlers.go`, defined just above `RegisterRoutes`.

Bundles the five plan-derived inputs `metrics.Calculate` needs beyond the
transaction set: `target`, `healthTarget`, `coverageStart`, `hasCoverage`,
`planExclusions`.

Call sites:
- `handleDashboard` — replaced its inline `metrics.BudgetTargets` +
  `metrics.HealthcareCoverageStart` + `planSyncExclusions` sequence with one
  `gatherDashboardCalcInputs(settings, data.Active(), startDate, endDate)`
  call; the later `metrics.Comparison(...)` call now reads
  `calcInputs.planExclusions` instead of the old local `planExclusions` var.
  Behavior unchanged (verified: full existing dashboard test suite green,
  including `TestHandleDashboard_PlanExclusionWiring` and the fractional-cent
  verdict-bar test).
- `handleKPIDetail` — gated to `kpiType == "living" || kpiType == "healthcare"`
  (so the other four kinds' request shape, including loader I/O, is
  unchanged): calls `gatherDashboardCalcInputs` then feeds its fields into
  `metrics.Calculate(filtered, startDate, endDate, ...)` to get
  `cardMetrics.ActualMonthly` / `cardMetrics.HealthcareActual` — the SAME
  fractional-divisor figure `handleDashboard` computes for the card, so the
  two surfaces cannot drift apart by construction (design point 4).

`handleKPIsPartial`, `handleChartData`, `buildBudgetVsActualChartData` were
NOT touched — spec named only handleDashboard/handleKPIDetail for the
extraction; leaving the other two call sites' inline duplication alone per
"no drive-by refactors beyond the specified input-assembly extraction."

## Per-file summary

### internal/handlers/dashboard/handlers.go
- Added `dashboardCalcInputs` type + `gatherDashboardCalcInputs` helper.
- `handleDashboard`: extraction only (see above); behavior unchanged.
- `kpiTitles`: added `"living": "Monthly Living Expenses"`,
  `"healthcare": "Monthly Healthcare"`.
- `handleKPIDetail`: for `living`/`healthcare` only, computes
  `monthlyLivingOutflows`/`monthlyHealthcareOutflows` via
  `metrics.LivingOutflows(outflows, planExclusions)` /
  `outflows.FilterByCategory(metrics.HealthInsuranceCategory)` (single-source
  rule — no new category literals, no re-implemented exclusion logic), adds
  `case "living"`/`case "healthcare"` to the per-month `value` switch (same
  signed-sum-then-Abs refund-netting shape as the existing kinds), and — per
  the mid-task spec amendment (KD-2026-08-30a) — overrides `avg` to
  `classifiedCardPerMonth` (the card's `ActualMonthly`/`HealthcareActual`)
  for these two kinds only, so "Per Month" is the single card figure (not
  Total÷months) and doubles as the "vs Avg" column's comparison basis without
  a second, separately-rendered average stat. Min/Max/Total tiles are
  unaffected (they already derive from the classified `values` slice).
- `handleKPIMonthDetail`: added `case "living"`/`case "healthcare"` before
  the `default` (savings) branch — each restricts the classified set
  (`metrics.LivingOutflows(...)` / `FilterByCategory(HealthInsuranceCategory)`)
  to the requested month via the existing `transactionsInMonth` helper, sets
  `total = math.Abs(sumSigned(...))`, `totalLabel` = "Living Spent" /
  "Healthcare Spent". Both existing helpers (`transactionsInMonth`,
  `sumSigned`) reused unmodified.
- No new "Health Insurance" string literal anywhere in the file (grep-gate
  K6 verified: `grep -n "Health Insurance" handlers.go` → 0 matches); all
  healthcare classification goes through `metrics.HealthInsuranceCategory`.

### internal/handlers/dashboard/handlers_http_test.go
Additions only — no existing test edited. Added `math` and
`budget2/internal/services/metrics` imports (both previously unused in this
file). New tests (K1–K5, see names below) plus two small fixtures/helpers
(`livingHealthcareFixtureRows`, `setupLivingHealthcareEnv`,
`healthcareCoverageFixtureRows`) reusing the file's existing patterns
(`writeTempCSV`, `decodeMonthDetail`, `monthDetailDescriptions`,
`extractDollarAfter`, `trunc` — all pre-existing helpers from other test
files in the same package, none redefined).

- `TestHandleKPIDetail_Living_Title`, `TestHandleKPIDetail_Healthcare_Title`,
  `TestHandleKPIDetail_LivingHealthcare_RenderedTitles`,
  `TestDashboardKPIs_LivingHealthcareCardsWiredToOwnKinds` — K1.
- `TestHandleKPIDetail_LivingClassification`,
  `TestHandleKPIDetail_LivingPerMonthMatchesCardFigure` — K2.
- `TestHandleKPIDetail_HealthcareClassificationExcludesLiving`,
  `TestHandleKPIDetail_HealthcarePerMonthMatchesCoverageClippedCard` — K3.
- `TestHandleKPIMonthDetail_LivingExcludesHealthAndPlanExcluded` — K4.
- `TestHandleKPIDetail_ExpensesResponseByteIdentical` — K5 (expected JSON
  built with the SAME arithmetic the handler performs, so a float64
  bit-pattern mismatch can't produce a false result; guards JSON shape +
  values, not just field presence).

### web/templates/components/kpi-detail.html
Two one-line changes inside the pre-existing `{{else}}` branch of the month
table (previously only reached by `kpiType == "expenses"`, now also reached
by `living`/`healthcare`):
- Column header: `{{if eq .Type "living"}}Living Expenses{{else if eq .Type
  "healthcare"}}Healthcare{{else}}Expenses{{end}}` (was the bare literal
  "Expenses").
- Row cell + "vs Avg" comparison: `.Value` instead of `.Expenses`. For
  `kpiType == "expenses"` these are numerically identical (`Value` is set to
  `expAmt` in that case in handlers.go, same as `Expenses`), so existing
  rendered output for "expenses" is unchanged — confirmed by
  `TestHandleKPIDetail_ExpensesResponseByteIdentical` (JSON path) and the
  full existing HTML-renderer test suite staying green.
- Summary tiles (Total/Per Month/Low/High) block above the table was NOT
  touched — it already reads `.Total`/`.Average`/`.Min`/`.Max` generically,
  which is exactly where the Go-side `avg` override lands for living/
  healthcare, so no template change was needed there.

### web/templates/components/kpis.html
Two onclick attribute changes only:
- Line ~57 (Monthly Living Expenses card): `openKPIDetail('expenses')` →
  `openKPIDetail('living')`.
- Line ~101 (Monthly Healthcare card): `openKPIDetail('expenses')` →
  `openKPIDetail('healthcare')`.
No other markup touched.

### web/templates/components/kpi-month-detail.html
NOT touched (permitted by spec: "only if its rendering needs a type-aware
label"). It already renders `.TotalLabel` dynamically and only special-cases
color by `{{if eq .Type "income"}}`, defaulting to red otherwise — which is
correct for `living`/`healthcare` (spending) with no change needed.

## Commands run + result tails

```
$ go build ./...
(clean, exit 0)

$ go vet ./...
(clean, exit 0)

$ gofmt -l internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_http_test.go
(no output — both already gofmt-clean)

$ grep -n "Health Insurance" internal/handlers/dashboard/handlers.go
0 matches for 'Health Insurance'   [K6 grep gate: PASS]

$ go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/
ok  	budget2/internal/handlers/dashboard	0.735s
ok  	budget2/internal/services/metrics	0.003s

$ go test -count=1 -run "KD1|Living|Healthcare|ExpensesResponseByteIdentical" -v ./internal/handlers/dashboard/...
--- PASS: TestDashboardKPIs_LivingSparkline_HasTargetAttribute
--- PASS: TestHandleKPIDetail_Living_Title
--- PASS: TestHandleKPIDetail_Healthcare_Title
--- PASS: TestHandleKPIDetail_LivingHealthcare_RenderedTitles
--- PASS: TestDashboardKPIs_LivingHealthcareCardsWiredToOwnKinds
--- PASS: TestHandleKPIDetail_LivingClassification
--- PASS: TestHandleKPIDetail_LivingPerMonthMatchesCardFigure
--- PASS: TestHandleKPIDetail_HealthcareClassificationExcludesLiving
--- PASS: TestHandleKPIDetail_HealthcarePerMonthMatchesCoverageClippedCard
--- PASS: TestHandleKPIMonthDetail_LivingExcludesHealthAndPlanExcluded
--- PASS: TestHandleKPIDetail_ExpensesResponseByteIdentical
--- PASS: TestBuildBudgetVsActualChartData_RefundReducesMonthLiving
--- PASS: TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative
--- PASS: TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder
--- PASS: TestDashboardVerdictBar_LivingHealthcareBreakdown_RenderedSumHoldsOnFractionalCentBase
PASS
ok  	budget2/internal/handlers/dashboard	0.101s
```

## Deviations from spec / notes for checkers

- Applied spec amendment KD-2026-08-30a (relayed mid-task by the
  coordinator): for `living`/`healthcare`, "Per Month" is the card's own
  `metrics.Calculate(...).ActualMonthly`/`.HealthcareActual` figure, NOT
  `Total÷months`. This value doubles as the "vs Avg" column's basis
  (existing template mechanism unchanged); there is no second,
  separately-labeled "Average" stat rendered anywhere for these two kinds.
  Summary tiles for living/healthcare remain exactly Total, Per Month, Low,
  High (same four-tile grid the other non-rate/non-savings kinds already
  use — no template restructuring needed).
- `handleKPIMonthDetail`'s living/healthcare cases call
  `planSyncExclusions(data.Active())` directly (not through
  `gatherDashboardCalcInputs`) since only the exclusion map is needed there,
  not the full bundle (target/healthTarget/coverageStart/hasCoverage are
  irrelevant to a transaction-list drill-down). This is still the single
  `planSyncExclusions` helper, not a re-implementation.
- No other files touched; `internal/services/dataloader/**` (foreign
  territory per the run's Territories section) was not read or modified.
