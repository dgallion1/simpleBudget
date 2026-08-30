# HC1 attempt 2 — manifest

## Files changed

- internal/services/mcpsvc/spend/summary.go
- internal/services/mcpsvc/spend/summary_test.go
- internal/services/metrics/metrics.go

## What changed

- **Fix 1** (`summary.go`): the budget block's `HealthcareTarget`/
  `HealthcareActual`/`HealthcareDelta` fields are now zeroed when
  `m.HasHealthcareTarget` is false, instead of copying
  `m.HealthcareTarget`/`HealthcareActual`/`HealthcarePerMonthDelta`
  unconditionally. `CombinedCumulativeDelta` was verified (not assumed)
  already correct in this case — `metrics.Calculate`'s
  `healthcareCumulativeDelta = healthcareTotal - healthcareTarget*coverageMonths`
  is 0 when `coverageMonths` is 0, so it never leaked.
- **Fix 2** (`summary_test.go`): added
  `TestSummarizeSpendingSuppressesPhantomHealthcareWhenNoCoverage` (Fix 1
  regression: plan with healthcare target configured, ledger with no Health
  Insurance transactions → healthcare fields 0, living fields intact) plus
  two mutation-killing tests:
  - `TestSummarizeSpendingDerivesCoverageStartFromFullLedgerNotWindow`
  - `TestSummarizeSpendingCoverageStartExcludesSuppressedDuplicates`
- **Fix 3** (`metrics.go`): `Comparison` now derives coverage start from
  `data.Active()` instead of raw `data`, so a future caller that passes an
  unfiltered set no longer inherits the duplicates-included loophole (today's
  two call sites in `handlers.go` already pre-filter with `.Active()`, so
  this is a no-op for them — `Active()` is idempotent).

## Mutation-kill confirmations

For each new test, the corresponding mutation was applied temporarily to
`internal/services/mcpsvc/spend/summary.go`, `go test` was run, the target
test was confirmed FAILING (with no other test in the package regressing),
then the mutation was reverted and `go build ./...` + the package test
re-run to confirm a byte-identical clean revert.

1. **Full-ledger (not window) derivation** — mutation: derive coverage start
   from `filtered` (window-filtered set) instead of `ts` (full active
   ledger, before the window filter). Result:
   `TestSummarizeSpendingDerivesCoverageStartFromFullLedgerNotWindow` FAILED
   (`healthcare_monthly_actual = 1790.44, want 981.85`); all 14 other tests
   in the package still PASSED. Reverted; suite green again.

2. **Duplicates excluded** — mutation: derive coverage start from the raw
   `ts` (before the `ts = ts.Active()` reassignment), i.e. including
   suppressed rows. Result:
   `TestSummarizeSpendingCoverageStartExcludesSuppressedDuplicates` FAILED
   (`healthcare_monthly_actual = 981.85, want 1790.44`); all 14 other tests
   in the package still PASSED. Reverted; suite green again.

## Verification (verbatim tails)

### go build / go vet

```
$ go build ./...
(no output)
$ go vet ./...
(no output)
```

### go test -count=1 ./...

```
ok  	budget2/cmd/enrich-amazon	6.563s
ok  	budget2/cmd/server	5.402s
ok  	budget2/cmd/validate	0.010s
ok  	budget2/internal/config	0.003s
ok  	budget2/internal/handlers/accounts	1.260s
ok  	budget2/internal/handlers/approval	0.004s
ok  	budget2/internal/handlers/backup	37.755s
ok  	budget2/internal/handlers/dashboard	0.684s
ok  	budget2/internal/handlers/duplicates	0.028s
ok  	budget2/internal/handlers/explorer	0.701s
ok  	budget2/internal/handlers/insights	0.246s
ok  	budget2/internal/handlers/majorexpenses	0.412s
ok  	budget2/internal/handlers/transfers	0.318s
ok  	budget2/internal/handlers/whatif	16.293s
ok  	budget2/internal/http	0.014s
ok  	budget2/internal/models	0.014s
ok  	budget2/internal/services/accounts	0.034s
ok  	budget2/internal/services/amazon	0.012s
ok  	budget2/internal/services/anomalies	0.014s
ok  	budget2/internal/services/backup	0.567s
ok  	budget2/internal/services/classifier	0.012s
ok  	budget2/internal/services/dataloader	1.816s
ok  	budget2/internal/services/insights	0.012s
ok  	budget2/internal/services/majorexpenses	0.012s
ok  	budget2/internal/services/mcpsvc	0.070s
ok  	budget2/internal/services/mcpsvc/admin	4.569s
ok  	budget2/internal/services/mcpsvc/confirm	0.048s
ok  	budget2/internal/services/mcpsvc/curate	0.605s
ok  	budget2/internal/services/mcpsvc/ledger	0.557s
ok  	budget2/internal/services/mcpsvc/plan	8.458s
ok  	budget2/internal/services/mcpsvc/snapshot	0.021s
ok  	budget2/internal/services/mcpsvc/spend	1.060s
ok  	budget2/internal/services/merchants	0.021s
ok  	budget2/internal/services/metrics	0.017s
ok  	budget2/internal/services/pricecreep	0.016s
ok  	budget2/internal/services/restore	1.128s
ok  	budget2/internal/services/retirement	34.005s
ok  	budget2/internal/services/retirement/analysis	24.878s
ok  	budget2/internal/services/retirement/completeness	0.004s
ok  	budget2/internal/services/retirement/engine	0.021s
ok  	budget2/internal/services/retirement/history	0.005s
ok  	budget2/internal/services/retirement/overrides	0.009s
ok  	budget2/internal/services/retirement/prepare	0.006s
ok  	budget2/internal/services/storage	75.201s
ok  	budget2/internal/services/transfers	0.008s
ok  	budget2/internal/templates	0.794s
ok  	budget2/internal/testutil	0.007s
ok  	budget2/internal/version	0.003s
ok  	budget2/web	0.009s
```

### bash .swarm/tier3/HC1/accept.sh (tee'd to oracle.2.log)

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
checks: 12 passed, 0 failed
ORACLE PASS
```

12/12 checks pass, including the new Stage 3 `mcp-no-coverage-suppressed`,
`dashboard-no-coverage-suppressed`, `dashboard-no-coverage-nan`,
`server2-up` checks added for attempt 2.

## Safety

No prebuilt `./budget2`/`./server` binary was run. Oracle server bound to
127.0.0.1:18093 only (built fresh from source into a tempdir by accept.sh);
no manual server was started; port 8080 was never touched.

## Constraints honored

- No refactors beyond the three fixes.
- `.swarm/tier3/HC1/accept.sh` and its fixture were not modified.
- No git-state changes (no commits, branches, stash, index changes).
