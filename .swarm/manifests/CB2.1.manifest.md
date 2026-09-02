# CB2.1 manifest

## Files touched

- internal/services/metrics/metrics.go — three of the seven sites: trend
  loop's expAmt (~line 500), hcAmt (~505), livingMonth (~513) changed from
  `math.Abs(<bucket>.SumAmount())` to `-<bucket>.SumAmount()`. Updated the
  comment blocks at each site (kept SY-2026-08-30d set-exclusion shape
  language, changed abs->signed negation, cited CB2). Also updated
  LivingOutflows' doc comment (lines ~299-318), which described per-month
  trend and the budget-vs-actual chart as still running math.Abs directly —
  now documents the split: range total still math.Abs, per-month trend and
  chart use the signed negated net. savingsTrend line (incAmt-expAmt) is
  UNCHANGED code, per the spec's explicit no-double-fix instruction.
- internal/handlers/dashboard/handlers.go — four of the original seven
  sites: handleKPIDetail's expAmt (~line 532), handleKPIExport's expAmt
  (~906), and buildBudgetVsActualChartData's hcAmt (~1071) and livingMonth
  (~1082), all changed from math.Abs to the signed negated net, with
  updated comment blocks (same shape-preserved / CB2-cited pattern).
  DERIVED savings/rate in both KPI handlers are unchanged code.
  PLUS, per amendment CB2-c (extending this same attempt), sites 8 and 9:
  handleKPIMonthDetail's expenseTotal (~line 738, the "Total Spent" tile)
  changed `math.Abs(sumSigned(monthOutflow))` -> `-sumSigned(monthOutflow)`,
  comment updated to cite CB2 + KD-2026-08-30d and note it now matches the
  KD-signed living/healthcare kinds in the SAME handler; and
  buildSpendingTrendChartData's monthlyTotals (~line 1409) changed
  `math.Abs(monthlyOutflowSets[m].SumAmount())` -> the signed negated net,
  comment updated ("per-month absolute totals" -> "signed negated totals")
  and explicitly notes the `prev > 0` %-change guard is UNCHANGED (a
  refund-dominant BASE month still renders 0%, an honest degradation, not
  a defect this fix touches). Checked all _test.go files referencing
  handleKPIMonthDetail / buildSpendingTrendChartData (ruling
  CB2-2026-09-02a step): none pin abs-specific behavior on a
  refund-dominant fixture (all existing fixtures are ordinary
  outflow-negative months, where signed-negation and abs agree) — full
  package suite passed unmodified before any new test was added, so no
  existing test needed updating for sites 8-9.
- internal/models/dashboard.go — field docs for ExpensesTrend, SavingsTrend,
  HealthcareTrend, LivingExpensesTrend updated to state the signed contract:
  refund-dominant month is negative (a credit); SavingsTrend for that month
  = income + refund.
- internal/services/metrics/plan_exclusions_remainder_test.go — ruling
  CB2-2026-09-02a: TestCalculateMetrics_PlanExclusions_
  RemainderNetsRefundMonthInLivingTrend's Jan assertion changed 3000 -> -3000
  (comment updated to the signed contract). Also rewrote the long stale NOTE
  on the CombinedCumulativeBalance walk-invariant break: it previously said
  the invariant was "out of scope, master-native, unresolved" — CB1 (PR #80,
  already on master before this run) in fact fixed it via the signed walk;
  the NOTE now says so and points at the walk's own regression tests instead
  of describing the break as still live. (TestCalculateMetrics_
  PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder, the OTHER test
  in this file, pins LivingExpensesTotal — a RANGE-level total — which stays
  math.Abs and is unchanged, correctly, per the spec's out-of-scope note.)
- internal/handlers/dashboard/plan_exclusions_chart_test.go — ruling
  CB2-2026-09-02a: TestBuildBudgetVsActualChartData_
  RemainderNetsRefundLivingEqualsAbsRemainder's Jan assertion changed
  3000 -> -3000 (comment updated to the signed contract; Feb's 1500 is
  unaffected, an ordinary month).
- internal/services/metrics/cb2_signed_trend_test.go (NEW) — committed
  regression test: two-month fixture (Jan ordinary, Feb refund-dominant in
  both the living and healthcare buckets), direct models.Transaction
  fixtures with TransactionType set explicitly. Asserts ExpensesTrend,
  LivingExpensesTrend, HealthcareTrend are signed negative for the
  refund-dominant month and SavingsTrend = income + net refund. Amounts
  (4000/1200/200/300/900/600) are distinct from the oracle's 5000/300/800.
  Both-ends validated by hand (fails on math.Abs, passes on the fix).
- internal/handlers/dashboard/cb2_signed_bar_test.go (NEW) — committed
  regression test: buildBudgetVsActualChartData's Living and Healthcare bar
  traces on the same two-month refund-dominant shape, asserting signed
  negative Feb values on both traces. Both-ends validated by hand.
- internal/handlers/dashboard/cb2_signed_kpi_http_test.go (NEW) — committed
  regression tests through the real HTTP surface (setupTestEnv/doGet) hitting
  /dashboard/kpi/expenses and /dashboard/kpi/expenses/export. CSV fixture
  description "Boutique Store Credit" avoids every classifier IncomeKeyword
  ("refund"/"rebate"/"cashback"/etc.) so the loader classifies it Outflow —
  the real-data refund shape. Asserts the JSON Expenses/Savings figures and
  the CSV's "-730.00" for the refund-dominant Feb row. Amounts
  (4200/1300/220/950) distinct from the oracle's 5000/300/800. Both-ends
  validated by hand (temporarily reverted just the two expAmt sites back to
  math.Abs, confirmed both new tests fail with the expected pre-fix values,
  then restored).
- internal/handlers/dashboard/cb2_signed_spending_trend_test.go (NEW,
  amendment CB2-c) — committed regression tests for site 9
  (buildSpendingTrendChartData), direct models.Transaction fixtures with
  TransactionType set explicitly (fixture gotcha does not apply to direct
  fixtures): (a) SignedCurrentMonth — Jan ordinary (net -1800), Feb
  refund-dominant (net +700) as the CURRENT month, asserts %change =
  -138.8889 (signed) vs the old -61.1111 (abs); (b)
  RefundDominantBaseStaysZeroGuard — Jan refund-dominant (net +700) as the
  BASE month, Feb ordinary, asserts %change stays 0 (the `prev > 0` guard
  is unchanged) — both-ends validated this test too, since at the abs end
  Jan's abs total (700) is >0 and produces a nonzero %change, so this test
  is a real discriminator, not vacuous. Amounts (1600/200/900/200) distinct
  from the oracle's 5000/300/800.
- internal/handlers/dashboard/cb2_signed_kpi_http_test.go (amendment CB2-c
  addition) — added TestHandleKPIMonthDetail_CB2_
  TotalSpentSignedForRefundDominantMonth, reusing the file's existing
  cb2KPIHTTPRows() fixture (Feb net +730, "Boutique Store Credit" avoiding
  every IncomeKeyword) through GET /dashboard/kpi/expenses/month/2025-02,
  asserting result["Total"] == -730 and TotalLabel == "Total Spent". Both-
  ends validated (temporarily reverted site 8 back to math.Abs, confirmed
  the test fails at 730, restored).

## Point 7 / amendment CB2-c — the two out-of-class sites found by the
worker's point-7 observations are now FIXED (sites 8-9 above), not just
reported. The lead's amendment CB2-c re-classified them in-class
(month-bucket abs feeding a rendered figure) after enumerating ALL
math.Abs call sites in both packages.

Sites classified OUT of CB2 as a DISTINCT per-transaction-abs class (CB3
candidates per the lead's amendment CB2-c — need their own contracts, NOT
fixed in this task; current line numbers, post-CB2-c edits):
- handlers.go ~383 — modal transaction-list total, `total +=
  math.Abs(t.Amount)` per individual transaction (not a month bucket).
- handlers.go ~1492 (spec's ~1482) — `merchantTotals[t.Label()] +=
  math.Abs(t.Amount)`; a refund inflates a merchant's apparent "spend".
- handlers.go ~1564-1566 (spec's ~1554-1556) — the cumulative cash-flow
  day walk's `dayTotal += math.Abs(t.Amount)` /
  `dayTotal -= math.Abs(t.Amount)`; an outflow-typed refund SUBTRACTS from
  cash flow (wrong direction) — flagged by the lead as a real bug worth
  its own run.
- metrics.go ~729 — `PercentChange`'s `math.Abs(previous)` denominator;
  %-change semantics against a signed base is a design question, not this
  class's fix.

Also checked and confirmed genuinely out of scope (range-level totals /
non-value comparator, unrelated to either class):
- metrics.go lines 378 (totalExpenses), 406 (healthcareTotal), 436
  (livingTotal) — RANGE-level totals, explicitly out of scope per the
  spec ("Range-level math.Abs totals (same as CB1's precondition)").
- handlers.go line 783 — sort comparator (`math.Abs(txns[i].Amount) >
  math.Abs(txns[j].Amount)`), orders rows, does not produce a rendered
  value itself.

## Verification run (final, post amendment CB2-c)

- `bash .swarm/tier3/CB2/accept.sh` -> final line `ORACLE PASS`, using the
  LEAD-EXTENDED probe (`.swarm/tier3/CB2/cb2_oracle_test.go` now includes
  `TestCB2Oracle_MonthDetailAndSpendingTrendSigned`, matched by the
  existing `-run 'TestCB2Oracle'` pattern since go test -run is an
  unanchored substring match).
- `go build ./...` -> clean (no output).
- `go vet ./...` -> clean (no output).
- `go test -count=1 ./internal/services/metrics/ ./internal/handlers/dashboard/`
  -> both `ok`.
- `go test -count=1 ./...` -> all packages `ok` (full suite, no regressions).
