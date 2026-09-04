# CB7.2 manifest (attempt 2)

## What changed since attempt 1

Attempt 1 left 4 pre-existing tests failing and reported them under "Needs
lead ruling" rather than silently re-pinning them (per instruction). The
lead recorded two rulings in `.swarm/CB7-RUN-SPEC.md` "Rulings"
(CB7-2026-09-03a, CB7-2026-09-03b). This attempt applies both rulings.
No production code changed in this attempt — only test files.

### Ruling CB7-2026-09-03a — re-pin the three SY-era remainder tests to the signed contract

`internal/services/metrics/plan_exclusions_remainder_test.go`:
- Renamed `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder`
  → `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsSignedRemainder`.
  Assertion changed from `floatEqual(m.LivingExpensesTotal, 3000)` to
  `floatEqual(m.LivingExpensesTotal, -3000)`. Rewrote the function's doc
  comment and the file's top-of-file doc comment to state the SIGNED
  contract and cite ruling CB7-2026-09-03a instead of arguing for
  `math.Abs`. `PlanExcludedTotal`/`PlanExcludedCount` assertions unchanged
  (they were never in scope — display-only, already signed).
- `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend`
  — untouched (its assertions were already signed correctly since CB2; not
  part of this ruling).
- Rewrote the "NOTE on CombinedCumulativeBalance (UPDATED, CB2)" comment
  block: removed the claim that range-level totals "still use math.Abs...
  while the RANGE as a whole nets outflow-negative (unchanged, out of
  scope)"; now states CB7 (ruling CB7-2026-09-03a) closed that gap and
  cross-references `TestCalculateMetrics_RefundDominantRange_SignedTotalsAndCombinedInvariantHolds`
  (added in attempt 1) as the range-level regression coverage.
- `TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows` —
  not renamed (no `...LivingEqualsAbsRemainder` suffix, per the ruling's
  rename instruction which named only the two `...AbsRemainder`-suffixed
  tests). Both assertions changed from `floatEqual(..., 3000)` to
  `floatEqual(..., -3000)` for `Current.LivingExpensesTotal` and
  `Previous.LivingExpensesTotal`, with updated error messages citing the
  ruling. Every other assertion in the file (fixture data, PlanExcluded*,
  the LivingExpensesTrend test) left exactly as attempt 1 left them.

`internal/services/mcpsvc/spend/plan_exclusions_test.go`:
- Renamed `TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder`
  → `TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsSignedRemainder`.
  `wantLivingActual` changed from `math.Round((3000/months)*100) / 100` to
  `math.Round((-3000/months)*100) / 100` (same tolerance check,
  `math.Abs(out.Budget.LivingActual-wantLivingActual) > 0.01`, unchanged).
  Rewrote the doc comment to state the signed contract and cite the
  ruling.

### Ruling CB7-2026-09-03b — canonicalize the explorer fixture's sign convention

`internal/handlers/explorer/handlers_test.go`,
`TestHandleTransactionsPartial_RefundReducesTotalExpenses`: negated every
`Amount` in the CSV fixture so it is written in the app's canonical bank
convention (purchases negative, the 2026-04-12 Shenandoah Lodging refund
now `+199.78`) instead of the original positive-convention export. `want =
349.61` and the `NetAmount` assertion are byte-identical to attempt 1 (the
ruling's own arithmetic check: canonical sum
`-(4.84+259.78+30.31+34.68+20.00)+199.78-199.78 = -349.61`, so
`TotalExpenses = -SumAmount() = 349.61`, unchanged). Updated the fixture's
doc comment to explain WHY the conversion was needed (the file is 7 rows,
below the loader's `minSignConventionSample` of 10, so the sign-convention
auto-detect heuristic never fires and the fixture's own convention is
whatever the app actually computes on — the old fixture happened to pass
under both `math.Abs` and the new signed formula for a reason unrelated to
CB7's fix). Added an explicit code comment noting — per the ruling's
instruction to "just note it" — that this fixture does NOT distinguish
`math.Abs` from the signed formula (`math.Abs(-349.61) == 349.61` too);
the two refund-DOMINANT fixtures added in attempt 1
(`TestHandleTransactionsPartial_RefundDominantFilterNegatesTotalExpenses`
and `TestHandleExplorer_WithRenderer_RefundDominantFilterRendersNegativeExpenses`)
remain the mutation-killers for the sign fix at both explorer.go sites (both
already re-verified as mutation-killers in attempt 1's manifest; not
re-run this attempt since neither file nor its production code changed).

## Verification

- `gofmt -l` on every file this task has touched across BOTH attempts
  (the full list in `CB7.2.files`, `.go` files only — `gofmt` does not
  apply to `.html` templates) — clean, zero output, exit 0.
- `go build ./...` — clean, exit 0.
- `go vet ./...` — clean, exit 0.
- `go test ./...` (cached) — every package `ok`, zero exceptions.
- `go test -count=1 ./...` (forced re-run, no cache) — every package `ok`,
  zero exceptions. Full tail:

```
ok  	budget2/cmd/enrich-amazon	7.242s
ok  	budget2/cmd/server	6.036s
ok  	budget2/cmd/validate	0.013s
ok  	budget2/internal/config	0.003s
ok  	budget2/internal/handlers/accounts	1.574s
ok  	budget2/internal/handlers/approval	0.005s
ok  	budget2/internal/handlers/backup	40.467s
ok  	budget2/internal/handlers/dashboard	1.649s
ok  	budget2/internal/handlers/duplicates	0.031s
ok  	budget2/internal/handlers/explorer	1.050s
ok  	budget2/internal/handlers/insights	0.362s
ok  	budget2/internal/handlers/majorexpenses	0.651s
ok  	budget2/internal/handlers/transfers	0.471s
ok  	budget2/internal/handlers/whatif	19.550s
ok  	budget2/internal/http	0.005s
ok  	budget2/internal/models	0.006s
ok  	budget2/internal/services/accounts	0.030s
ok  	budget2/internal/services/amazon	0.004s
ok  	budget2/internal/services/anomalies	0.013s
ok  	budget2/internal/services/backup	0.566s
ok  	budget2/internal/services/classifier	0.005s
ok  	budget2/internal/services/dataloader	1.881s
ok  	budget2/internal/services/insights	0.006s
ok  	budget2/internal/services/majorexpenses	0.004s
ok  	budget2/internal/services/mcpsvc	0.056s
ok  	budget2/internal/services/mcpsvc/admin	4.602s
ok  	budget2/internal/services/mcpsvc/confirm	0.048s
ok  	budget2/internal/services/mcpsvc/curate	0.727s
ok  	budget2/internal/services/mcpsvc/ledger	0.636s
ok  	budget2/internal/services/mcpsvc/plan	10.399s
ok  	budget2/internal/services/mcpsvc/snapshot	0.011s
ok  	budget2/internal/services/mcpsvc/spend	1.174s
ok  	budget2/internal/services/merchants	0.011s
ok  	budget2/internal/services/metrics	0.011s
ok  	budget2/internal/services/pricecreep	0.011s
ok  	budget2/internal/services/restore	1.119s
ok  	budget2/internal/services/retirement	38.149s
ok  	budget2/internal/services/retirement/analysis	28.044s
ok  	budget2/internal/services/retirement/completeness	0.011s
ok  	budget2/internal/services/retirement/engine	0.057s
ok  	budget2/internal/services/retirement/history	0.011s
ok  	budget2/internal/services/retirement/overrides	0.012s
ok  	budget2/internal/services/retirement/prepare	0.015s
ok  	budget2/internal/services/storage	78.914s
ok  	budget2/internal/services/transfers	0.009s
ok  	budget2/internal/templates	1.066s
ok  	budget2/internal/testutil	0.006s
ok  	budget2/internal/version	0.008s
ok  	budget2/web	0.008s
```

No FAIL lines anywhere in either run. CB8's `internal/services/insights`
package (a separate concurrent task, now accepted and stable per the
coordinator's message) compiled and passed cleanly.

## Complete file list, both attempts combined

See `.swarm/manifests/CB7.2.files` — the union of attempt 1's file list
plus `internal/services/metrics/plan_exclusions_remainder_test.go` and
`internal/services/mcpsvc/spend/plan_exclusions_test.go` (both touched only
in this attempt, to apply the two rulings).
