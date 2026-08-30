# HC1 attempt 3 — manifest

## Files changed

- `internal/handlers/dashboard/handlers.go` — Phase 1 reconstruction (file had
  been reverted to master): three `metrics.Calculate`/`HealthcareCoverageStart`
  call sites (handleDashboard, handleKPIsPartial, handleChartData's
  budget-vs-actual case) now derive `coverageStart, hasCoverage` from
  `data.Active()` (full ledger, pre-window-filter) and pass them through;
  `buildBudgetVsActualChartData` gained `(coverageStart time.Time, hasCoverage
  bool)` parameters, matching the signature `handlers_test.go`'s four
  pre-existing call sites already expected. The dashed combined-target line
  is now the prorated monthly average
  `(livingTarget*monthsInRange + healthcareTarget*coverageMonths) / monthsInRange`;
  the per-month running-balance accrual keeps living's contribution flat and
  clips only healthcare's share via `metrics.ClippedHealthcareMonths` applied
  to each calendar month's own segment, normalized to a fraction of that
  month.
- `internal/handlers/dashboard/coverage_start_mutation_test.go` (new) —
  attempt-3 criterion-3b mutation-killing tests for the three handlers.go
  call sites (rows 1-3 of the matrix below): two test functions, each hitting
  all three HTTP endpoints (`/dashboard`, `/dashboard/kpis`,
  `/dashboard/charts/data/budget-vs-actual`) against one fixture, so a
  mutation at any one of the three sites fails at least one assertion.
  Fixture B uses the real dataloader duplicate-decision flow
  (`dl.SaveDuplicateDecision`, the same mechanism `accounts_card_test.go`
  already uses) to produce a genuine `Suppressed: true` row — no
  decisions-file UI machinery, one direct API call.
- `internal/services/metrics/metrics_test.go` — two new mutation-killing
  tests for `Comparison`'s `HealthcareCoverageStart` call site (row 4 of the
  matrix below), built with in-memory `models.Transaction` literals
  (`Suppressed: true` set directly, no dataloader needed at this level).
- `internal/models/dashboard.go` — F3: replaced the stale
  `CombinedCumulativeDelta` field comment (`(LivingExpensesTotal +
  HealthcareTotal) - CombinedTarget*MonthsInRange`) with the correct basis:
  `LivingCumulativeDelta + HealthcareCumulativeDelta`, i.e. living accrued
  over `MonthsInRange` and healthcare accrued over the separately-clipped
  `coverageMonths`, summed.
- `.swarm/tier3/HC1/oracle.3.log` — this attempt's oracle run (tee'd), final
  line `ORACLE PASS`, 14/14 checks.

No other product code changed (scope: Phase 1 reconstruction + F3 comment
only, per the Attempt-3 contract).

## Ten mutation-run results (5 sites × 2 mutations, each applied alone)

Verification method for every row: `cp` the target file to `.bak`, apply the
mutation with `sed`/a small Python script, run `go build ./...` (confirm it
still compiles) then the named test, confirm FAIL, `mv` the `.bak` back,
rebuild clean.

| # | Site | Mutation | Killing test | Result |
|---|------|----------|---------------|--------|
| 1 | `handlers.go:100` handleDashboard | → window-filtered (`filtered`) | `TestCoverageStartFullLedgerNotWindow_AllThreeHandlerSites` | FAIL (confirmed) |
| 2 | `handlers.go:100` handleDashboard | → duplicates-included (`data`) | `TestCoverageStartExcludesSuppressedDuplicate_AllThreeHandlerSites` | FAIL (confirmed) |
| 3 | `handlers.go:187` handleKPIsPartial | → window-filtered (`filtered`) | `TestCoverageStartFullLedgerNotWindow_AllThreeHandlerSites` | FAIL (confirmed) |
| 4 | `handlers.go:187` handleKPIsPartial | → duplicates-included (`data`) | `TestCoverageStartExcludesSuppressedDuplicate_AllThreeHandlerSites` | FAIL (confirmed) |
| 5 | `handlers.go:250` handleChartData (budget-vs-actual) | → window-filtered (`filtered`) | `TestCoverageStartFullLedgerNotWindow_AllThreeHandlerSites` | FAIL (confirmed) |
| 6 | `handlers.go:250` handleChartData (budget-vs-actual) | → duplicates-included (`data`) | `TestCoverageStartExcludesSuppressedDuplicate_AllThreeHandlerSites` | FAIL (confirmed) |
| 7 | `summary.go` summarize_spending (mcpsvc/spend) | → window-filtered (moved derivation after `filtered := ts.FilterByDateRange(...)`, argument `filtered`) | `TestSummarizeSpendingDerivesCoverageStartFromFullLedgerNotWindow` (attempt-2, verified not duplicated) | FAIL (confirmed) |
| 8 | `summary.go` summarize_spending (mcpsvc/spend) | → duplicates-included (introduced `rawTS := ts` before `ts = ts.Active()`, argument `rawTS`) | `TestSummarizeSpendingCoverageStartExcludesSuppressedDuplicates` (attempt-2, verified not duplicated) | FAIL (confirmed) |
| 9 | `metrics.go` `Comparison` | → window-filtered (`currentFiltered`) | `TestComparisonDerivesCoverageStartFromFullLedgerNotWindow` (new) | FAIL (confirmed) |
| 10 | `metrics.go` `Comparison` | → duplicates-included (`data`) | `TestComparisonCoverageStartExcludesSuppressedDuplicates` (new) | FAIL (confirmed) |

Rows 7-8 reused attempt-2's existing tests per the Attempt-3 contract ("where
attempt-2's summary tests already provide the kill, run the mutation anyway
and name the killing test") — the mutation itself required a small structural
rewrite of the call site (moving the derivation after the window filter for
row 7; introducing a pre-`Active()` alias for row 8) since production code
had already been restructured to the correct form; both were verified,
reverted (`mv .bak` back), and `go build ./...` confirmed clean afterward.

All ten reverts were clean: `go build ./...`, `go vet ./...`, and the full
`go test ./...` are green on the final (reverted) tree — see tails below.

## Verification tails

### `go build ./...` / `go vet ./...`
Both exit 0, no output.

### `go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/` (Phase 1 gate)
```
ok  	budget2/internal/handlers/dashboard	0.510s
ok  	budget2/internal/services/metrics	0.003s
```

### `go test -count=1 ./...` (final, full tree)
```
ok  	budget2/cmd/enrich-amazon	6.718s
ok  	budget2/cmd/server	5.462s
ok  	budget2/cmd/validate	0.012s
ok  	budget2/internal/config	0.004s
ok  	budget2/internal/handlers/accounts	1.234s
ok  	budget2/internal/handlers/approval	0.005s
ok  	budget2/internal/handlers/backup	38.580s
ok  	budget2/internal/handlers/dashboard	0.738s
ok  	budget2/internal/handlers/duplicates	0.032s
ok  	budget2/internal/handlers/explorer	0.671s
ok  	budget2/internal/handlers/insights	0.254s
ok  	budget2/internal/handlers/majorexpenses	0.421s
ok  	budget2/internal/handlers/transfers	0.322s
ok  	budget2/internal/handlers/whatif	17.301s
ok  	budget2/internal/http	0.005s
ok  	budget2/internal/models	0.009s
ok  	budget2/internal/services/accounts	0.032s
... (all remaining packages ok, including SY territory: whatif, majorexpenses,
     models, mcpsvc/* — no failures anywhere in the tree)
ok  	budget2/web	0.011s
```
No SY-territory failures observed; nothing to attribute.

### `bash .swarm/tier3/HC1/accept.sh` (attempt 3)
```
CHECK build: PASS
CHECK contract-tests: PASS
CHECK server-up: PASS
CHECK budget-card-clipped-total: PASS
CHECK verdict-over-plan: PASS
CHECK healthcare-card-since: PASS
CHECK chart-clipped-target: PASS
CHECK mcp-combined-over: PASS
CHECK server2-up: PASS
CHECK mcp-no-coverage-suppressed: PASS
CHECK dashboard-no-coverage-suppressed: PASS
CHECK dashboard-no-coverage-nan: PASS
CHECK full-ledger-prewindow-accrual: PASS
CHECK duplicates-excluded-derivation: PASS
checks: 14 passed, 0 failed
ORACLE PASS
```
(logged to `.swarm/tier3/HC1/oracle.3.log`)

## Notes for checkers

- `internal/handlers/dashboard/handlers_test.go` was NOT modified this
  attempt (already survived the master revert from attempts 1-2); its four
  `buildBudgetVsActualChartData` call sites were the signature spec Phase 1
  was reconstructed against.
- SY territory (`internal/models/major_expense.go`,
  `internal/services/majorexpenses/**`,
  `internal/services/dataloader/major_expenses.go`,
  `internal/handlers/whatif/sync.go`,
  `web/templates/components/whatif/sync-preview.html`) was not touched;
  their pre-existing uncommitted diffs are visible in `git status` but are
  not part of this manifest.
- `.swarm/ledger.tsv` shows as modified in `git status` from before this
  attempt started; not touched by this worker.
