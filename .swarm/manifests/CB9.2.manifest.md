# CB9.2 manifest (attempt 2, lead-authored)

Attempt 1 failed both lanes on the same ground: the three chart-walk
SignedNet sites (hcAmt/livingMonth/spend in buildBudgetVsActualChartData)
had no endpoint coverage and two of them ARE observable in the
budget-vs-actual trace JSON (`"y":[300,-0]`); checker-tests also found a
FIFTH negated-sum site the lead's grep pattern missed (indexed map
expression): buildSpendingTrendChartData
`monthlyTotals[m] = -monthlyOutflowSets[m].SumAmount()` (handlers.go:1522),
which served `customdata":[[-0,520]]` on CB9's own fixture.

## Changes since attempt 1
1. handlers.go:1522 → `metrics.SignedNet(monthlyOutflowSets[m])`.
   Re-enumeration used `grep -n "= -\|:= -"` (catches indexed forms); the
   only remaining negations are the two `+= -t.Amount` accumulators
   (1340, 1401) plus 403/1598 checker-tests found, all provably -0-safe
   (accumulator starts +0; +0 + -0 = +0; cancelling non-zero terms → +0).
2. New `TestHandleChartData_CB9_CancellingMonthHasNoNegativeZero` drives
   `/dashboard/charts/data/spending-trend` and `/budget-vs-actual` (with
   living + legacy healthcare targets wired via whatif.json so the
   combined-target gate yields Living and Healthcare traces) and asserts
   no `-0` JSON token and Signbit-clear y values on every trace.
3. checker-second's observation folded in (same class): SpendingVelocity's
   three negated sums (insights/trends.go dailyAvg/historicalDaily/
   spentSoFar) route through metrics.SignedNet — a cancelling window
   reached the MCP get_trends JSON as `-0` via round2. New test
   `TestCalculateSpendingVelocity_CancellingWindowIsPositiveZero` asserts
   +0/Signbit-clear and no `-0` token in the marshalled struct.
4. `formatNumber` gets the same -0 belt (checker-second observation,
   latent); TestFormatNumber pins `Copysign(0,-1)` → "0".

## Lead mutation sanity (checkers re-run independently)
- hcAmt, livingMonth, spending-trend monthlyTotals inlined → chart test FAILS.
- velocity dailyAvg inlined → insights test FAILS.
- `spend` (cumulative walk) inlined → SURVIVES; both checkers proved at
  attempt 1 that `running += monthTarget - spend` absorbs -0 bit-for-bit,
  so it is consumer-unobservable (CB7 precedent: acceptable when stated).
  Converted anyway for single-source consistency.
- attempt-1 mutants (classifiedMonthlyTotals, negSumSigned, expAmt ×2,
  formatPercent) unchanged and still killed.
handlers.go / trends.go restored byte-identical after each (cmp).

## Verification
gofmt clean on all seven files; `go test -count=1 ./...` (below).
