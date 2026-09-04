# CB9.1 manifest (attempt 1, lead-authored)

Closes the two observations checker-second raised on CB7/CB8:

1. Negative-zero source sites outside CB7's listed scope, in
   `internal/handlers/dashboard/handlers.go`: `classifiedMonthlyTotals`
   (`totals[m] = -set.SumAmount()`, feeds the KPI modal rows AND the CSV
   export's raw `%.2f`, which bypasses the formatMoney belt and printed
   "-0.00"); handleKPIDetail/handleKPIExport `expAmt = -exp.SumAmount()`
   (2 sites); handleKPIMonthDetail `-sumSigned(...)` (3 sites, expenses/
   living/healthcare Total tiles); the chart walk's `hcAmt`, `livingMonth`,
   `spend` (3 sites). All now route through `metrics.SignedNet`; a new
   `negSumSigned` wrapper replaces the three `-sumSigned(...)` sites.
   The `total += -t.Amount` accumulators (1340, 1401) are untouched: an
   accumulator that starts at +0 cannot produce -0 (+0 + -0 = +0, and a
   cancelling sequence of non-zero terms lands on +0).
2. `formatPercent` gets the same -0 belt as formatMoney.
3. `internal/services/mcpsvc/spend/trends.go` BurnRateChange doc comment
   now states the CB8 rule (|history| denominator, sign-of-change on zero).
4. Stale comment in `classifiedMonthlyTotals` ("card figure is an Abs")
   rewritten for the post-CB7 contract.

## Tests
- `cb9_negzero_http_test.go`: an exactly-cancelling February at every
  slicing (healthcare -300/+300, living -100/+100, expenses = union).
  Export (expenses/living/healthcare/savings) Feb cells must be "0.00" and
  never start with "-0"; month-detail JSON for expenses/living/healthcare
  must carry no `-0` token and decode to +0 with a clear sign bit; KPI
  detail response must contain no "$-0"/"-$0.00"/`-0` token.
- `render_helpers_test.go` TestFormatPercent: `math.Copysign(0,-1)` → "0.0".

## Lead mutation sanity (checkers re-run independently)
- classifiedMonthlyTotals reverted to `-set.SumAmount()` → CB9 export test
  FAILS (healthcare/living Feb "-0.00").
- negSumSigned reverted to `-sumSigned` → month-detail test FAILS.
- both expAmt sites reverted → export/detail expenses FAIL.
(handlers.go restored byte-identical after each, verified with cmp.)

## Verification
`gofmt -l` clean; `go test -count=1 ./internal/handlers/dashboard/
./internal/templates/ ./internal/services/mcpsvc/spend/` ok. Full
`make check` runs in the pre-commit hook at commit time.
