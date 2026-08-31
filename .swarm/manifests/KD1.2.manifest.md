# KD1 attempt 2 — ruling KD-2026-08-30c (healthcare zero-coverage-divisor) + K8/K9

Attempt 2 landed in two coordinator messages; this manifest covers ALL
attempt-2 changes across both (the files list is identical between them —
no new file was needed for K8 since `handleKPIExport` already lives in
`internal/handlers/dashboard/handlers.go`).

## Part A — KD-2026-08-30c: healthcare zero-coverage-divisor fix

### Defect (conceded checker-second FAIL, attempt 1)

`GET /dashboard/kpi/healthcare` for a range where the coverage-clipped
divisor is zero (`hasCoverage` false, or `coverageStart` after the range
end) rendered "Per Month: $0.00" beside a non-zero classified row —
`HealthcareActual` is 0 because `ClippedHealthcareMonths()==0` (an undefined
rate), not because spend is zero, while the row's own classified total
(category-based) was correctly non-zero. This is the "two kinds of totals"
confusion ruling KD-2026-08-30a forbids.

### Fix

For the `healthcare` kind, when the coverage-clipped month count for the
selected range is zero, the Per Month tile renders `&mdash;` plus the text
"no coverage in this range" (real text, not color-only, both themes), and
the vs-Avg column renders `&mdash;` for every row (no comparison basis).
The range Total is untouched (a real, defined figure regardless of the
divisor). A genuinely zero rate WITH zero rows still renders `$0.00`
(unaffected code path). `living` is unaffected (`MonthsBetween` is never
zero), so the guard is scoped to `kpiType == "healthcare"` only.

### internal/handlers/dashboard/handlers.go (Part A)
- Added `healthcareNoCoverageInRange bool`, computed only inside the
  existing `kpiType == "living" || kpiType == "healthcare"` block, set only
  for `kpiType == "healthcare"`:
  ```go
  healthcareNoCoverageInRange = metrics.ClippedHealthcareMonths(startDate, endDate, calcInputs.coverageStart, calcInputs.hasCoverage) <= 0
  ```
  Calls `metrics.ClippedHealthcareMonths` — the SAME exported, single-source
  helper `metrics.Calculate` already calls internally to produce
  `coverageMonths` (and therefore `HealthcareActual`) — over
  `calcInputs.coverageStart`/`calcInputs.hasCoverage` already gathered by
  `gatherDashboardCalcInputs` (attempt 1). No new coverage-derivation logic.
- Added `"HealthcareNoCoverageInRange": healthcareNoCoverageInRange` to
  `partialData` (present, `false`, for every other kind/state — harmless
  additive field).

### web/templates/components/kpi-detail.html (Part A)
- Per Month tile: wrapped the `{{formatMoney .Average}}` `<p>` in
  `{{if and (eq .Type "healthcare") .HealthcareNoCoverageInRange}} ... {{else}} ... {{end}}`;
  the new branch renders `&mdash;` (same bold/size classes, both themes)
  plus a `text-xs text-gray-500 dark:text-gray-400` line reading "no
  coverage in this range".
- vs-Avg row column: added
  `{{$noCoverage := and (eq .Type "healthcare") .HealthcareNoCoverageInRange}}`
  alongside the pre-existing `$avg`/`$type`/`$isRate` locals, then wrapped
  the vs-Avg `<td>` in `{{if $noCoverage}}<td ...>&mdash;</td>{{else}}<td
  ...>{{formatPercent ...}}%</td>{{end}}` — neutral `text-gray-500
  dark:text-gray-400` (no green/red signal, since there is no comparison).
- `living` and every pre-existing kind unaffected.

### internal/handlers/dashboard/handlers_http_test.go (Part A)
- **New test (K3b):** `TestHandleKPIDetail_Healthcare_NoCoverageInRange_ShowsDashNotZero`
  reproduces the checker's exact fixture (`healthcareNoCoverageFixtureRows`:
  a single Health Insurance category row that is a POSITIVE-amount refund,
  `+150` on 2025-01-15, no other HI rows; range `2025-01-01..2025-01-31`
  has no coverage overlap since `metrics.HealthcareCoverageStart` only
  counts NEGATIVE-amount HI rows). Asserts Total still renders `$150.00`,
  body contains "no coverage in this range", Per Month contains `&mdash;`
  and never `0.00`, and the vs-Avg cell for the row renders `&mdash;`
  (checked both by presence of the exact dash-cell markup and absence of
  any `%</td>` percentage cell in the whole modal body).

## Part B — K8 (Export CSV zero-byte regression) + K9 (strict rendered-string equality)

### K8 defect (checker-tests F2)
`handleKPIExport`'s header/row `switch` statements had no `living`/
`healthcare` case (and no `default`), so those exports silently wrote zero
bytes to the CSV (probe: `kind=expenses bodyLen=31, kind=living
bodyLen=0`).

### K8 fix — shared month-total helper, no duplicated classification

Added `classifiedMonthlyTotals(classified *models.TransactionSet)
map[string]float64` (new unexported helper, `handlers.go`, placed next to
`sumSigned`): groups a classified set by month and returns each month's
`Abs(signed sum)` — the same refund-netting shape every living/healthcare
month figure in this package already used. **`handleKPIDetail`'s own
per-month loop was refactored to call this same helper** (replacing its
prior inline `monthlyLivingOutflows[m]`/`GroupByMonth()`+`Abs(SumAmount())`
pattern with `monthlyLivingTotals[m]`/`monthlyHealthcareTotals[m]` map
lookups) — so the modal table and the CSV export literally call the same
function on the same classified sets, not two independently-written
month-reduction loops. `handleKPIExport` gained a `kpiType == "living" ||
kpiType == "healthcare"` block that classifies via
`metrics.LivingOutflows(outflows, planExclusions)` /
`outflows.FilterByCategory(metrics.HealthInsuranceCategory)` — the SAME
single-source classification calls used everywhere else in this file — then
reduces via `classifiedMonthlyTotals`, and new `case "living"`/`case
"healthcare"` arms in both the header-write switch (columns "Living
Expenses" / "Healthcare", matching the modal table's own column header) and
the row-write switch (`fmt.Sprintf("%.2f", monthlyLivingTotals[m])` /
`monthlyHealthcareTotals[m]`, missing-month map lookup reads as 0).
`planExclusions` comes from `planSyncExclusions(data.Active())` — the SAME
helper `handleKPIMonthDetail`'s living case already calls — since export
only needs the exclusion map, not the full budget-target/coverage bundle
`gatherDashboardCalcInputs` assembles for the modal's "Per Month" tile.

### K8 tests
- `TestHandleKPIExport_Living` (reuses attempt-1's
  `setupLivingHealthcareEnv`/`livingHealthcareFixtureRows`): asserts a
  non-empty CSV, header `[Month, Living Expenses]`, and the Jan row =
  `1300.00` — the SAME figure
  `TestHandleKPIDetail_LivingClassification` (K2) asserts the modal shows.
- `TestHandleKPIExport_Healthcare` (reuses attempt-1's
  `healthcareCoverageFixtureRows`): asserts a non-empty CSV, header
  `[Month, Healthcare]`, and both Jan/Feb rows = `300.00` — the SAME
  figures `TestHandleKPIDetail_HealthcareClassificationExcludesLiving` (K3)
  asserts the modal shows.

### K9 — strict formatMoney rendered-string equality

Both Per-Month tests (`TestHandleKPIDetail_LivingPerMonthMatchesCardFigure`,
`TestHandleKPIDetail_HealthcarePerMonthMatchesCoverageClippedCard`) were
tightened from `math.Abs(got-want) > 0.01` (parsed-float tolerance) to
strict string equality:
- Added `extractDollarStringAfter` (test helper): like the pre-existing
  `extractDollarAfter`, but returns the raw matched `"$X,XXX.XX"` string
  (via the same `verdictDollarRe`) instead of a parsed float.
- Added `formatMoneyExpected` (test helper): a byte-for-byte mirror of
  `templates.formatMoney`'s algorithm (that function is unexported, so it
  can't be called directly from this package) — same `fmt.Sprintf("%.2f",
  ...)` rounding, same comma-grouping loop — used ONLY to build the expected
  string for comparison; it does not touch handler or template behavior.
- Both tests now assert `extractDollarStringAfter(...) ==
  formatMoneyExpected(want)` exactly, no tolerance.

## Commands run + result tails

```
$ go build ./...
Go build: Success

$ go vet ./...
(clean, no output)

$ gofmt -l internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_http_test.go
(no output — both gofmt-clean)

$ grep -n "Health Insurance" internal/handlers/dashboard/handlers.go
0 matches for 'Health Insurance'   [K6 grep gate: PASS, unchanged]

$ go test -count=1 -run "Living|Healthcare|KPIExport|ExpensesResponseByteIdentical" -v ./internal/handlers/dashboard/...
--- PASS: TestHandleKPIExport_Income / _Expenses / _Savings / _SavingsRate / _DefaultDates / _LoadError / _ZeroIncome / _LoadErrorReturns500
--- PASS: TestDashboardKPIs_LivingSparkline_HasTargetAttribute
--- PASS: TestHandleKPIDetail_Living_Title
--- PASS: TestHandleKPIDetail_Healthcare_Title
--- PASS: TestHandleKPIDetail_LivingHealthcare_RenderedTitles
--- PASS: TestDashboardKPIs_LivingHealthcareCardsWiredToOwnKinds
--- PASS: TestHandleKPIDetail_LivingClassification
--- PASS: TestHandleKPIDetail_LivingPerMonthMatchesCardFigure          (K9, now strict)
--- PASS: TestHandleKPIDetail_HealthcareClassificationExcludesLiving
--- PASS: TestHandleKPIDetail_HealthcarePerMonthMatchesCoverageClippedCard (K9, now strict)
--- PASS: TestHandleKPIMonthDetail_LivingExcludesHealthAndPlanExcluded
--- PASS: TestHandleKPIDetail_ExpensesResponseByteIdentical
--- PASS: TestHandleKPIDetail_Healthcare_NoCoverageInRange_ShowsDashNotZero (K3b)
--- PASS: TestHandleKPIExport_Living                                    (new, K8)
--- PASS: TestHandleKPIExport_Healthcare                                (new, K8)
--- PASS: TestBuildBudgetVsActualChartData_* / TestDashboardVerdictBar_* (unrelated, incidentally in the same run filter)
PASS

$ go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/
ok  	budget2/internal/handlers/dashboard	0.769s
ok  	budget2/internal/services/metrics	0.003s
```

## Deviations / notes for checkers

- No new file was needed for K8: `handleKPIExport` lives in
  `internal/handlers/dashboard/handlers.go`, already a permitted file.
- The only test EDIT (not addition) across attempt 2 remains the K5
  byte-identical `want` map (from the earlier `KD-2026-08-30c` half of this
  attempt) gaining `"HealthcareNoCoverageInRange": false` — a mechanically
  required consequence of that field's addition to `partialData`, not a
  relaxation of what K5 guards.
- `handleKPIDetail`'s per-month living/healthcare computation was refactored
  (not just handleKPIExport added to) to call the new
  `classifiedMonthlyTotals` helper, per the coordinator's explicit
  instruction to "factor the month-row computation so modal and export
  share it rather than duplicating it" — this was a genuine, in-scope
  extraction inside the already-permitted `handlers.go`, not a drive-by
  refactor of unrelated code.
- Files touched this attempt: `internal/handlers/dashboard/handlers.go`,
  `internal/handlers/dashboard/handlers_http_test.go`,
  `web/templates/components/kpi-detail.html` — exactly the three the
  coordinator named. `kpis.html` and `kpi-month-detail.html` untouched.
