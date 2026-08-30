# SY4 — manifest (attempt 4, single scoped remediation after hard stop)

Ruling **SY-2026-08-30e**: attempt 3's set-exclusion rewrite was sound at
every `metrics.go` site, but `buildBudgetVsActualChartData`'s cumulative
walk was left half-rewritten: it still summed `livingMonth + hcAmt` where
`livingMonth` had become an INDEPENDENT `|LivingOutflows bucket|` (attempt
3's own change). Master's identity `livingMonth = expAmt − hcAmt` used to
make that sum cancel back to the month's true combined `|sum|` exactly;
once decoupled, `|a|+|b| ≠ |a+b|` whenever the two buckets diverge in sign
— proven live as a ~$615-class divergence **even with `planExclusions=nil`**,
a regression against master that was impossible before attempt 3. Third
failed attempt at Tier 2 triggered the hard stop; the user deferred and
authorized this single, exactly-scoped attempt 4.

## Scope discipline

Only `internal/handlers/dashboard/handlers.go` was touched (plus one new
test file). Nothing in `internal/services/metrics/metrics.go`,
`internal/models/dashboard.go`, `internal/services/mcpsvc/spend/*`, or any
of attempts 1-3's test files was modified — `git status` confirms every
other tracked file in the SY4 territory is unchanged since attempt 3's
accepted state.

## The fix (exactly what was asked, nothing else)

`buildBudgetVsActualChartData`'s cumulative-balance walk's monthly spend
accrual was rewritten to merge-then-one-Abs, mirroring `metrics.go`'s own
combined walk exactly:

1. New `nonExcludedOutflows` set: `livingOutflows.Transactions` (already
   classified by `metrics.LivingOutflows`, attempt 3) merged with
   `healthcareOutflows.Transactions` (already classified by the existing
   `FilterByCategory(HealthInsuranceCategory)` call) — i.e. every outflow
   EXCEPT plan-sync-excluded rows, HI included. Built by concatenating two
   ALREADY-classified sets, not a third independent classifier — same
   discipline `metrics.go`'s `nonExcludedOutflows` used in attempt 3.
2. `monthlyNonExcluded := nonExcludedOutflows.GroupByMonth()`.
3. The walk's spend term is now `spend := math.Abs(monthlyNonExcluded[m].SumAmount())`
   — ONE Abs of the merged signed sum — replacing
   `running += monthTarget - (livingMonth + hcAmt)` with
   `running += monthTarget - spend`.

The Living BAR value (`livingValues`, `data[0]["y"]`) is UNCHANGED —
`|LivingOutflows month bucket|` — per the dispatch: "The Living BAR value
stays as-is... that part is correct and tested." All of attempts 1-3's
existing chart-bar assertions (`TestBuildBudgetVsActualChartData_*`) still
pass unmodified, confirming this.

## New test (added, nothing existing modified)

`internal/handlers/dashboard/plan_exclusions_chart_walk_test.go`:
`TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance` —
same sign-divergent fixture (Jan's living remainder nets a REFUND of
+3000 via a $1000 grocery outflow + $4000 outflow-typed credit, beside an
ordinary -$400 Health Insurance premium and a -$500 car payment that's
flagged in one sub-test) run through BOTH `metrics.Calculate`'s
`CombinedCumulativeBalance` and `buildBudgetVsActualChartData`'s
cumulative trace, asserting month-by-month agreement. Two subtests:
`nil planExclusions` and `flagged def`.

**Design note on the target values and tolerance**: the two walks' TARGET
accrual formulas have always differed by design, pre-dating SY4 entirely
— `metrics.go` prorates by `MonthsBetween`'s exact fractional day count
per calendar month, while the chart's `monthTarget` stays a flat per-month
rate (`buildBudgetVsActualChartData`'s own comment: "living's share stays
the flat monthly rate (unchanged from master)"). That is explicitly out of
this attempt's scope (ruling SY-2026-08-30e only asks for the SPEND
accrual's merge-then-Abs fix, not the accrual/target formula). The test
uses `budgetTarget=1, healthcareTarget=1` (deliberately tiny, not
realistic dollar figures) so that pre-existing, unrelated accrual-formula
drift shrinks to a few cents (observed ≤ ~$0.13 across the two months),
while the SPEND-side bug this attempt fixes — whose magnitude is driven by
the transaction amounts, not the target — stays at full scale ($475-$800
per point, observed). A tolerance of 0.2 cleanly separates the two: it
comfortably covers the residual accrual noise while remaining ~4000x
tighter than the bug's magnitude.

## Both-ends calibration

Patched `handlers.go`'s walk back to the broken `running += monthTarget -
(livingMonth + hcAmt)` shape (attempt-3-as-shipped), added a throwaway
`_ = spend` to keep the otherwise-unused `monthlyNonExcluded` construction
compiling, ran the new test, then restored from a pre-patch copy (`diff`
clean):

```
$ go test ./internal/handlers/dashboard/... -run TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance -v
=== RUN   TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance/nil_planExclusions
    point 0: metrics = -2097.963039014374, chart = -2898           (diff = $800.04)
    point 1: metrics = -3996.1232032854214, chart = -4796          (diff = $800.12)
=== RUN   TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance/flagged_def
    point 0: metrics = -2597.963039014374, chart = -3398           (diff = $800.04)
    point 1: metrics = -4496.123203285421, chart = -5296           (diff = $800.12)
--- FAIL: TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance (both subtests FAIL)
```

Confirms the exact defect class the ruling describes: an ~$800 divergence
(same order of magnitude as the ruling's own ~$615 probe on a different
fixture) that fails **even in the `nil planExclusions` subtest** — a
regression against master unrelated to any flag being set.

```
$ diff handlers.go.v4bak handlers.go && echo IDENTICAL   # after restoring the fix
IDENTICAL

$ go test ./internal/handlers/dashboard/... -run TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance -v
--- PASS: TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance (0.00s)
    --- PASS: .../nil_planExclusions (0.00s)
    --- PASS: .../flagged_def (0.00s)
```

## Verification

```
$ gofmt -l internal/handlers/dashboard/handlers.go internal/handlers/dashboard/plan_exclusions_chart_walk_test.go
(clean, no output)

$ go build ./...
(clean)

$ go vet ./...
(clean)

$ go test ./internal/services/metrics/... ./internal/handlers/dashboard/... ./internal/services/mcpsvc/spend/... -count=1
ok  	budget2/internal/services/metrics	0.004s
ok  	budget2/internal/handlers/dashboard	0.554s
ok  	budget2/internal/services/mcpsvc/spend	0.660s

$ go test ./... -count=1
(all packages ok, zero non-"ok" lines)
```

Every attempt-1/2/3 test verified still passing by name (31 SY4-specific
tests total across the three packages, including all
`TestCalculateMetrics_PlanExclusions_*`, `TestComparison_PlanExclusions*`,
`TestBuildBudgetVsActualChartData_*`, `TestDashboardVerdictBar_*`,
`TestHandle*_PlanExclusionWiring`, and
`TestSummarizeSpendingBudgetBlock*` — see the full `--- PASS` list captured
during this attempt's verification run) plus the one new
`TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance`.

## Notes for the checker

- Diff is intentionally small: one production file
  (`internal/handlers/dashboard/handlers.go`, +85/-12 lines including
  comments) and one new test file. No template, no other Go package.
- No `.html` template file was changed — no a11y check is implicated.
