# CB3 attempt 1 — manifest

## ATTEMPT 2 (dual-lane FAIL on attempt 1, both conceded; Amendment CB3-c)

Attempt 1's two verification lanes both FAILED and the lead conceded every
finding:
- checker-tests (primary): mutation survivors at `trends.go:44` (the PIN
  path through `sumByExpense` — real path via `get_trends`' pins) and at
  the `spentSoFar` site (the velocity test's fixture missed the current
  calendar month in `time.Local`, and `MonthProjection` was asserted
  sign-only, not the exact identity).
- checker-second (adversarial): LIVE bug in `MajorExpenseTrends`' inline
  percent-change/direction classifier — divided by the SIGNED `previous`
  and hardcoded `previous==0 -> +100/"up"`. Real-ledger reproduction:
  Travel `current=0, previous=-628` gave `change_amount=+628` but
  `change_percent=-100, direction="down"` (self-contradictory: spending
  fell to zero, which should read as an improvement, not a decline). Also
  `previous==0, current<0` incorrectly gave `"up"/+100`. My own attempt-1
  refund fixtures existed but asserted only `CurrentAmount`, not
  `Direction`/`ChangePercent`, so the bug slipped past them.

### Fix 1 — the live bug (Amendment CB3-c), `internal/services/insights/trends.go`

In `MajorExpenseTrends`'s per-name loop (CategoryTrends' own classifier is
a SEPARATE function and is untouched): introduced `change := current -
previous`; `previous == 0` now sets `changePercent` to `+100`/`-100`/`0`
by the SIGN of `change` (not a hardcoded `+100`); `previous != 0` now
divides by `math.Abs(previous)` (was: signed `previous`) — the same
convention `PercentChange` documents for CB3-E. `Direction` is then
derived from `changePercent` via the unchanged +-5 stable band. This
guarantees `ChangePercent`'s sign always agrees with `ChangeAmount`'s
(both zero together), closing the self-contradiction. Comment cites
CB3-c and the CB3-E convention it borrows.

### Fix 2 — PIN-path mutation survivor (`trends.go:44`)

Added `TestAnalyzeMajorExpenseTrends_PinnedRefundNetsSigned` in
`internal/services/insights/trends_test.go`: a spend row (-450) and a
refund row (+130) both reach the same `MajorExpense` def ONLY via
`models.ResolveByIdentity` (Hash-based pins), with keywords
(`"zzz-nomatch-2"`) that deliberately match neither description — a
broken pin path would drop both rows, not just mis-sign them. Asserts
signed net 320 (abs would give 580). Amounts differ from both the
oracle's 300/100 pin fixture and attempt 1's non-pin fixtures.

### Fix 3 — `spentSoFar` mutation survivor (MonthProjection identity)

Added `TestCalculateSpendingVelocity_MonthProjectionIdentity` in the same
test file: rows dated via the oracle's validated timezone-robust recipe
(`d1`/`d2` clamped into `[time.Date(now.Year(),now.Month(),1,...,
time.Local), now]`, since `SpendingVelocity`'s month-to-date bucket is
computed in `time.Local` and a UTC-midnight first-of-month row can
precede it west of UTC). Asserts the EXACT identity `MonthProjection ==
signedSpent + DailyAverage*DaysRemaining` (signedSpent = -580 for
-280/+860 fixture), not just the sign — this is what kills an abs-mutant
on `spentSoFar` (it would shift `MonthProjection` by `2*|signed net|`
instead of producing a `MonthProjection >= 0` false pass).

### Fix 4 — Direction/ChangePercent assertions on existing + new refund fixtures

- `TestAnalyzeMajorExpenseTrends_RefundInNormalPeriodSignedNet`
  (attempt-1 test): added assertions `ChangePercent == 100`,
  `Direction == "up"` (previous=0, current=350>0).
- `TestAnalyzeMajorExpenseTrends_RefundDominantPeriodSignedNet`
  (attempt-1 test): added assertions `ChangePercent == -100`,
  `Direction == "down"` — this IS one of the two conceded shapes
  (`previous=0, current<0` -> down/-100).
- New `TestAnalyzeMajorExpenseTrends_ZeroCurrentNetRefundPreviousSignConsistent`:
  covers the OTHER conceded shape (`current=0, previous<0` -> up/positive)
  — a refund-dominant PREVIOUS period (-600) with nothing this period;
  asserts `Direction == "up"` and `ChangePercent > 0` (the live bug would
  have divided by the signed `-600` and reported `"down"`/negative).

### Re-verification (attempt 2)

- `go build ./...` clean; `go vet ./...` clean.
- `go test -count=1 -v -run "MajorExpenseTrends|SpendingVelocity" ./internal/services/insights/` — all PASS (14 tests, including the 4 new/strengthened ones above).
- `bash .swarm/tier3/CB3/accept.sh` → `ORACLE PASS` (now includes
  `TestCB3Oracle_TrendClassifierSignConsistent`,
  `TestCB3Oracle_PinnedRefundNetsSigned`,
  `TestCB3Oracle_MonthProjectionIdentity`).
- `go test -count=1 ./internal/handlers/dashboard/ ./internal/services/insights/ ./internal/services/metrics/ ./internal/services/mcpsvc/spend/` — all `ok`.
- `go test -count=1 ./...` — full suite green (47 packages).

### Files touched this attempt

- `internal/services/insights/trends.go` (code fix)
- `internal/services/insights/trends_test.go` (new + strengthened tests)

No other files changed in attempt 2; the attempt-1 file set (below) and
attempt-1 doc touch-up (Amendment CB3-b, also below) are unchanged.

### Touch-up: checker-tests finding F1 (60s/year flake, non-blocking, fixed pre-commit)

`TestCalculateSpendingVelocity_MonthProjectionIdentity`'s `d1` clamp
(`monthStart.Add(time.Minute)`) could itself land AFTER `now` when `now`
falls within the first minute of the calendar month, pushing `d1` back
OUT of `SpendingVelocity`'s `[monthStart, now]` month-to-date bucket and
breaking the hardcoded `signedSpent = -580.0` identity (proven 13
failing minutes/year). Test-only fix: after the existing
`monthStart.Add(time.Minute)` clamp, added a one-line follow-up clamp —
if that value is still after `now`, use `now` instead — with a short
comment. Test-only, no production code touched.

Re-verified: `go test -count=1 ./internal/services/insights/` → `ok`;
`bash .swarm/tier3/CB3/accept.sh` → `ORACLE PASS`.

## Amendment CB3-b touch-up (post-report, same attempt)

- `internal/services/mcpsvc/spend/trends.go` — doc-only fix to the
  `get_trends` tool's Description string. The old text claimed
  current_amount/previous_amount are "POSITIVE dollar figures" for BOTH
  `category_trends` and `major_expense_trends` collectively. Reworded to
  split the claim: `category_trends`' current_amount/previous_amount
  remain POSITIVE (abs-based `CategoryTotals`, unaffected, wording and
  computation untouched per the amendment); `major_expense_trends`'
  current_amount/previous_amount are now documented as SIGNED, with a
  refund-dominant major-expense period explicitly called out as netting
  negative (a credit, not a debt). change_amount/change_percent's
  "SIGNED for both" language is unchanged in meaning, only reworded to
  read naturally after the split. No code change (Deps/handler logic,
  `categoryTrendRows`, `velocityRowFrom` all untouched).
- Checked `internal/services/mcpsvc/spend/*_test.go` for pinned old
  wording: `grep -n "POSITIVE" internal/services/mcpsvc/spend/*_test.go`
  found no hits in this package's tests (the "POSITIVE" hits in
  `summary_test.go` are comments about `summary.go`'s own docstring/
  behavior, an unrelated tool, not `get_trends`) — no test text needed
  updating.
- Re-verified: `go vet ./internal/services/mcpsvc/spend/` clean;
  `go test -count=1 ./internal/services/mcpsvc/spend/` all pass (63
  tests); `bash .swarm/tier3/CB3/accept.sh` → `ORACLE PASS`.

## Files touched

- `internal/handlers/dashboard/handlers.go` — CB3-A: `handleMajorExpenseDrilldown`'s
  Total loop changed from `total += math.Abs(t.Amount)` to `total -= t.Amount`
  (signed net, matches `bucketMajorExpenses`' existing list-row contract).
  CB3-B: `buildMerchantsChartData`'s per-merchant loop changed from
  `+= math.Abs(t.Amount)` to `-= t.Amount` (refunds net against the
  merchant; net-refund merchant renders a negative bar). CB3-C:
  `buildCumulativeChartData`'s day-total branch (both the Income and
  else/non-Transfer arms) collapsed to a single `dayTotal += t.Amount`
  for every non-Transfer row, fixing the wrong-direction cash-flow bug
  (an outflow-typed refund now correctly ADDS to cash flow) and, as a
  documented side effect, making a negative-amount Income row (an income
  reversal) correctly subtract instead of being forced positive by
  AbsAmount. Comments added at each site documenting the contract.

- `internal/handlers/dashboard/handlers_http_test.go` — added CB3-A
  regression tests `TestHandleMajorExpenseDrilldown_RefundInNormalGroupSignedNet`
  (mixed group, net positive, signed net != abs sum) and
  `TestHandleMajorExpenseDrilldown_RefundDominantGroupSignedNet` (refund-
  dominant "Unmatched" group renders a negative Total). Both use CSV/HTTP
  fixtures with descriptions ("Store Merchandise Return", "Widget Shop
  Merchandise Return") that avoid every classifier income keyword/category
  so the fixture reaches the handler through the real loader+classifier
  pipeline as a genuine positive-amount, Outflow-typed refund.

- `internal/handlers/dashboard/handlers_test.go` — added CB3-B regression
  `TestBuildMerchantsChartData_RefundNetsAgainstMerchant` (one merchant
  mixed net-positive, one merchant net-refund/negative bar) and CB3-C
  regression `TestBuildCumulativeChartData_RefundDayIncreasesRunningTotal`
  (income day, purchase day, refund day — running total must increase on
  the refund day). Also rewrote `TestBuildCumulativeChartData_PositiveAmountOutflows`
  per ruling CB3-2026-09-02a: same fixture, new expected series
  [5000, 6500, 7000], with a comment explaining the supersession (the old
  "unsigned bank export, use type not sign" premise conflicts with the
  classifier's documented pipeline contract; a real unsigned export can't
  reach the chart un-normalized, so a positive-amount Outflow-typed row IS
  a refund and correctly adds cash — unsigned-export support belongs in
  the loader, not per-chart abs). Not deleted, per the ruling.

- `internal/services/insights/trends.go` — CB3-D: `sumByExpense`'s two
  `+= math.Abs(t.Amount)` sites (pin-match and keyword-match branches)
  changed to `-= t.Amount` (signed net per period, same contract as
  CB3-A). `SpendingVelocity`'s three `math.Abs(<X>.SumAmount())` sites
  (`dailyAvg`, `historicalDaily`, `spentSoFar`) changed to
  `-<X>.SumAmount()`. Comments document the signed contract at each site
  and the downstream trace finding on `burnRateChange`'s existing
  `historicalDaily > 0` guard (see Downstream trace notes below).

- `internal/services/insights/trends_test.go` — added CB3-D regressions:
  `TestAnalyzeMajorExpenseTrends_RefundInNormalPeriodSignedNet` (mixed
  period, signed net 350 vs abs 550), `TestAnalyzeMajorExpenseTrends_RefundDominantPeriodSignedNet`
  (refund-dominant period renders -500), and
  `TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative`
  (refund-dominant period: DailyAverage/HistoricalDaily = -550,
  MonthProjection negative). All use amounts distinct from the oracle's
  300/100/800/1000 fixtures and from the existing
  `TestCalculateSpendingVelocity_RefundReducesDailyAverage` fixture.

- `internal/services/metrics/metrics.go` — CB3-E: no code change to
  `PercentChange`. Added a doc comment recording the `|previous|`
  denominator as the deliberate signed-base convention (not an
  abs-per-transaction bug) and pinning the two spec-mandated cases.

- `internal/services/metrics/metrics_test.go` — added
  `TestPercentChange_NegativeBase` pinning `PercentChange(-500, -1000) ==
  50` and `PercentChange(-1500, -1000) == -50`, per CB3-E.

## Existing tests checked for conflict (grepped by function name, none
## needed changes beyond the one named rewrite)

- `TestBuildMerchantsChartData_TopTen`, `_LessThanTen`, `_EmptyData`,
  `_AggregatesByLabel`: all-negative-amount fixtures (real spend only, no
  refund rows) — signed and abs computations coincide, unaffected.
- `TestBuildCumulativeChartData_PositiveBalance`, `_NegativeBalance`,
  `_EmptyData`, `_MultipleTxnsSameDay`, `_SkipsTransfers`: all use
  properly-signed (negative-outflow) fixtures — unaffected. Only
  `_PositiveAmountOutflows` used positive-amount Outflow rows and is the
  one rewritten per ruling CB3-2026-09-02a.
- `TestHandleMajorExpenseDrilldown_Unmatched/_DefaultDates/_UnknownName/_LoadError`:
  none pin a Total value — unaffected.
- `TestBuildMajorExpenseChartData_*` (EmptyData, AllUnmatched,
  FewerThanThreshold, ExactlyAtThreshold, AboveThresholdRollup,
  RollupWithUnmatched): exercise `buildMajorExpenseChartData` /
  `bucketMajorExpenses`, which already used the signed `total += -t.Amount`
  contract before this task (established by an earlier ruling) — out of
  scope, untouched, unaffected.
- `TestAnalyzeCategoryTrends_*`, `TestAnalyzeIncomePatterns_*`: use
  `catTxn`/`income` helpers with all-negative or all-income fixtures —
  unaffected; `CategoryTrends`/`CategoryTotals` are explicitly out of
  scope for CB3 and were not touched.
- `TestAnalyzeMajorExpenseTrends_GroupsByExpenseName`,
  `_PinOverridesKeyword`, `_NoDefsReturnsNil`: all-negative-amount
  fixtures via `catTxn` — signed and abs agree, unaffected.
- `TestCalculateSpendingVelocity_BasicCalculation`,
  `_EmptyData`, `_RefundReducesDailyAverage` (net-positive period, refund
  merely reduces the average — 550, same under signed and abs since net
  stays positive), `_BurnRateChange`, `_SingleDayData`: unaffected.
- `internal/handlers/insights/verdict_test.go`: constructs
  `models.SpendingVelocity` via literal struct values, not through
  `SpendingVelocity()` — unaffected by the CB3-D arithmetic change.
- `internal/services/mcpsvc/spend/trends_test.go`: exercises
  `get_trends`/`velocityRowFrom` with its own fixtures; none pin a
  refund-dominant case, all pass unchanged (confirmed by both the oracle
  run and the direct package test run).
- CB1/CB2 named regressions
  (`TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit`,
  `CB2*`): unaffected — CB3 does not touch `metrics.go`'s balance/expense
  totals, only `PercentChange`'s comment. Verified green via
  `accept.sh`'s explicit non-regression checks.

## Downstream trace notes (CB3-D, per spec requirement)

- `SpendingVelocity`'s `monthProjection := spentSoFar + (dailyAvg *
  daysRemaining)` is pure addition (no division/threshold); it inherits
  the sign of `spentSoFar`/`dailyAvg` correctly — a refund-dominant
  month-to-date projects negative, which is honest, not a misbehavior.
  Confirmed by the new `TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative`.
- `burnRateChange`'s existing `if historicalDaily > 0 { ... }` guard
  (present before CB3-D, originally to cover a zero-outflow ledger) now
  also silently covers a NEGATIVE `historicalDaily` (a refund-dominant
  full ledger): `burnRateChange` stays at its zero value instead of
  dividing by a negative baseline. This is a pre-existing guarded
  degradation extended to a new input range — no crash, no NaN, no sign
  inversion, just an unreported change stat when the historical baseline
  itself nets refund-dominant. Treated as the spec's "trivially guarded
  degradation... fine to note," not a STOP.
- `internal/handlers/insights/verdict.go`'s `BuildPaceVerdict` classifies
  `BurnRateChange < 0` as green ("below usual pace"); this remains
  semantically correct under the new signed contract (a negative burn
  rate — spending less, or net-negative — is still "good"). No code
  change needed.
- Templates rendering `.CurrentAmount`/`.PreviousAmount` for
  `MajorExpenseTrends` rows (`web/templates/pages/insights.html`) and the
  `handleTrendsChartData` bar chart (`internal/handlers/insights/handlers.go`)
  only call `formatMoney`/render bars directly — same honest-negative
  treatment as CB3-A/CB3-B's own surfaces (e.g. the existing Travel group
  rendering net-negative today). No semantic misbehavior found.
- Observation (originally reported for the backlog; now RESOLVED by
  Amendment CB3-b, see the touch-up section above): the `get_trends`
  docstring in `internal/services/mcpsvc/spend/trends.go` claimed
  current_amount/previous_amount were "POSITIVE dollar figures" for both
  `category_trends` and `major_expense_trends`. True for `category_trends`
  (abs-based `CategoryTotals`, unaffected by CB3), false for
  `major_expense_trends` after CB3-D's signed `sumByExpense`. The lead
  confirmed the finding and issued Amendment CB3-b authorizing a doc-only
  fix in this same attempt; applied above.

## Ruling implemented

- CB3-2026-09-02a: `TestBuildCumulativeChartData_PositiveAmountOutflows`
  rewritten (not deleted) in `internal/handlers/dashboard/handlers_test.go`
  with the supersession comment, same fixture, new expectations
  [5000, 6500, 7000].
