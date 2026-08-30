# HC1 attempt 1 — file manifest

- internal/services/metrics/metrics.go — added `HealthcareCoverageStart` (earliest negative-amount outflow-typed Health Insurance tx) and `ClippedHealthcareMonths` (single clipping helper); `Calculate` gained `coverageStart, hasCoverage` params, clips all healthcare accrual (`HealthcareActual`, `HealthcareTargetTotal`, `HealthcareCumulativeDelta`, `HasHealthcareTarget`, the `combinedCumulativeBalance` per-segment walk) through the new helper, guards the actual/coverageMonths division against NaN, and sets new `HealthcareCoverageStart(InRange)`/`HealthcareHasCoverage` fields; `CombinedCumulativeDelta` now derives as `cumulativeDelta + healthcareCumulativeDelta`; `Comparison` derives coverage start once from its full unfiltered `data` param and passes it to both `Calculate` calls.
- internal/services/metrics/metrics_test.go — updated every pre-existing `Calculate(...)` call to the new signature (added a `fullCoverage` sentinel before all fixture dates, `true`) preserving each test's original full-window-accrual intent; added new tests for `HealthcareCoverageStart` (earliest-bill, refunds ignored, no-bills, empty set, non-outflow-type ignored), `ClippedHealthcareMonths` (no-coverage flag, coverage after/before/inside segment), and `Calculate`'s coverage-inside/before/after-window behavior plus the no-coverage-flag and exact-boundary cases, asserting no NaN/Inf.
- internal/models/dashboard.go — added `HealthcareCoverageStart`, `HealthcareHasCoverage`, `HealthcareCoverageStartInRange` fields to `DashboardMetrics`; updated stale field comments (`HealthcareTargetTotal`/`HealthcareCumulativeDelta`/`HealthcareActual`/`HasHealthcareTarget`) to describe the clipped-coverageMonths basis instead of `MonthsInRange`.
- internal/handlers/dashboard/handlers.go — both `metrics.Calculate` call sites (`handleDashboard`, `handleKPIsPartial`) now derive `coverageStart, hasCoverage` from `metrics.HealthcareCoverageStart(data.Active())` (the full unfiltered set) before the date-range filter, and pass them through; `handleChartData`'s `budget-vs-actual` case does the same; `buildBudgetVsActualChartData` gained `coverageStart, hasCoverage` params, computes a prorated target line `(livingTarget*monthsInRange + healthcareTarget*coverageMonths)/monthsInRange` for the dashed shape/annotation, and clips each month's healthcare share of the running cumulative-balance accrual via `metrics.ClippedHealthcareMonths`.
- internal/handlers/dashboard/handlers_test.go — updated the four `buildBudgetVsActualChartData(...)` call sites to the new signature, choosing coverage args that preserve each test's original intent (full accrual for the Structure test via a coverage start before the window; coverage irrelevant for the Empty/NoTarget/Refund tests since healthcareTarget is 0 there).
- internal/handlers/dashboard/verdict.go — added `centsFromDecimalString` (duplicated from `analysis.centsFromDecimalString`, same %.2f-then-parse algorithm) and, in `BuildBudgetVerdict`, derive `Healthcare.Delta`'s RENDERED cents as `Delta`'s cents minus `Living.Delta`'s cents whenever both buckets are configured, so the verdict-bar's Living+Healthcare breakdown always sums to the combined figure on rendered strings (ruling 2026-08-29b), even at floating-point ties introduced by HC1's new clipped-months division.
- internal/handlers/dashboard/verdict_fractional_cent_test.go — NEW. Regression test proving the Living+Healthcare rendered-string sum invariant on a fractional-cent fixture (1804.415 twice) that fails without the `verdict.go` fix (verified via a temporary revert) and passes with it.
- internal/services/dataloader/transfers_test.go — updated the two `metrics.Calculate(...)` calls to the new signature (`start, true`; healthcareTarget is 0 in this fixture so coverage is a no-op).
- internal/services/mcpsvc/spend/summary.go — `summarize_spending` now derives `coverageStart, hasCoverage` from the full active (duplicate-excluded, unfiltered-by-window) set before the window filter, and passes them into its `metrics.Calculate` call; `healthcare_monthly_target/actual/delta` and `combined_cumulative_delta` flow from `Calculate` unchanged.
- web/templates/components/kpis.html — Monthly Healthcare card gains a provenance line `since {{formatDate .Metrics.HealthcareCoverageStart}}` (Go `Jan 2, 2006` layout via the existing `formatDate` helper), shown only when `.Metrics.HealthcareCoverageStartInRange` is true, no arithmetic added.

## go test ./... (tail)

```
ok  	budget2/cmd/enrich-amazon	(cached)
ok  	budget2/cmd/server	(cached)
ok  	budget2/cmd/validate	(cached)
ok  	budget2/internal/config	(cached)
ok  	budget2/internal/handlers/accounts	(cached)
ok  	budget2/internal/handlers/approval	(cached)
ok  	budget2/internal/handlers/backup	(cached)
ok  	budget2/internal/handlers/dashboard	(cached)
ok  	budget2/internal/handlers/duplicates	(cached)
ok  	budget2/internal/handlers/explorer	(cached)
ok  	budget2/internal/handlers/insights	(cached)
ok  	budget2/internal/handlers/majorexpenses	(cached)
ok  	budget2/internal/handlers/transfers	(cached)
ok  	budget2/internal/handlers/whatif	(cached)
ok  	budget2/internal/http	(cached)
ok  	budget2/internal/models	(cached)
ok  	budget2/internal/services/accounts	(cached)
ok  	budget2/internal/services/amazon	(cached)
ok  	budget2/internal/services/anomalies	(cached)
ok  	budget2/internal/services/backup	(cached)
ok  	budget2/internal/services/classifier	(cached)
ok  	budget2/internal/services/dataloader	(cached)
ok  	budget2/internal/services/insights	(cached)
ok  	budget2/internal/services/majorexpenses	(cached)
ok  	budget2/internal/services/mcpsvc	(cached)
ok  	budget2/internal/services/mcpsvc/admin	(cached)
ok  	budget2/internal/services/mcpsvc/confirm	(cached)
ok  	budget2/internal/services/mcpsvc/curate	(cached)
ok  	budget2/internal/services/mcpsvc/ledger	(cached)
ok  	budget2/internal/services/mcpsvc/plan	(cached)
ok  	budget2/internal/services/mcpsvc/snapshot	(cached)
ok  	budget2/internal/services/mcpsvc/spend	(cached)
ok  	budget2/internal/services/merchants	(cached)
ok  	budget2/internal/services/metrics	(cached)
ok  	budget2/internal/services/pricecreep	(cached)
ok  	budget2/internal/services/restore	(cached)
ok  	budget2/internal/services/retirement	(cached)
ok  	budget2/internal/services/retirement/analysis	(cached)
ok  	budget2/internal/services/retirement/completeness	(cached)
ok  	budget2/internal/services/retirement/engine	(cached)
ok  	budget2/internal/services/retirement/history	(cached)
ok  	budget2/internal/services/retirement/overrides	(cached)
ok  	budget2/internal/services/retirement/prepare	(cached)
ok  	budget2/internal/services/storage	(cached)
ok  	budget2/internal/services/transfers	(cached)
ok  	budget2/internal/templates	(cached)
ok  	budget2/internal/testutil	(cached)
ok  	budget2/internal/version	(cached)
ok  	budget2/web	(cached)
```

## Oracle: bash .swarm/tier3/HC1/accept.sh

```
CHECK build: PASS
CHECK contract-tests: PASS
CHECK server-up: PASS
CHECK budget-card-clipped-total: PASS
CHECK verdict-over-plan: PASS
CHECK healthcare-card-since: PASS
CHECK chart-clipped-target: PASS
CHECK mcp-combined-over: PASS
checks: 8 passed, 0 failed
ORACLE PASS
```
