# CB8.1 manifest (attempt 1)

## Files changed

- `internal/services/insights/trends.go` — `SpendingVelocity`'s
  `burnRateChange` calculation (was ~:339-350) rewritten per ruling
  CB8-2026-09-03a to fix the `historicalDaily > 0` guard: a ledger whose
  entire history nets a refund (`historicalDaily < 0`) previously reported
  `BurnRateChange = 0` no matter how fast the current period was actually
  spending. New logic:
  ```go
  change := dailyAvg - historicalDaily
  var burnRateChange float64
  switch {
  case historicalDaily != 0:
      burnRateChange = change / math.Abs(historicalDaily) * 100
  case change > 0:
      burnRateChange = 100
  case change < 0:
      burnRateChange = -100
  default:
      burnRateChange = 0
  }
  ```
  Mirrors the CB3-c classifier at `MajorExpenseTrends`'s `changePercent`
  exactly: `|historicalDaily|` denominator so the sign of the result always
  tracks the sign of `change`, never inverted by a negative base; a zero
  base picks its result by the sign of `change` (not `metrics.PercentChange`,
  whose zero-base case is an unconditional +100 — not called here). The
  CB3-D comment block that documented the old guarded-degradation behavior
  was rewritten to document the new rule and cite ruling CB8-2026-09-03a.
  For an ordinary positive `historicalDaily`, `change /
  math.Abs(historicalDaily)` is arithmetically identical to the pre-CB8
  `change / historicalDaily` (dividing by a value equals dividing by its
  absolute value when the value is already positive) — acceptance
  criterion (c): ordinary-ledger BurnRateChange is unchanged.

- `internal/services/insights/trends_test.go` — five additions:
  1. `TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative` (existing,
     ~:828) extended with one new assertion: `BurnRateChange != 0 -> error`
     (pins that IDENTICAL current/historical sets, where `change == 0`
     exactly, report a flat 0, not ±100).
  2. New `TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange`
     — `allData` = the current period's own purchase txn plus a much
     larger refund dated 65 days earlier, so ledger-wide `HistoricalDaily <
     0` while the current period's `DailyAverage > 0`. Asserts both signs
     via harness-error fatals, then asserts `BurnRateChange` equals
     `(DailyAverage - HistoricalDaily) / |HistoricalDaily| * 100` to within
     0.01 and is positive.
  3. New `TestCalculateSpendingVelocity_ZeroHistoricalBaseSpendingIsPositive100`
     — `allData` nets exactly zero (one -300 purchase 60 days ago, one +300
     refund 59 days ago); current period is a single spending transaction.
     Asserts `HistoricalDaily == 0` (harness fatal) and
     `BurnRateChange == 100` exactly.
  4. New `TestCalculateSpendingVelocity_ZeroHistoricalBaseRefundIsNegative100`
     — same zero-net `allData`; current period is a single refund
     transaction (`DailyAverage < 0`). Asserts `BurnRateChange == -100`
     exactly — this is the test that actually distinguishes the sign-of-
     change rule from `metrics.PercentChange`'s unconditional +100.
  No template/verdict.go/other-package change.

## Mutation validation (spec-required, all three mutants)

Applied each mutant to `internal/services/insights/trends.go` only, ran
`go test ./internal/services/insights/... -run 'TestCalculateSpendingVelocity' -v`,
recorded the failing test(s), then restored the file from a pre-edit backup
(`cp internal/services/insights/trends.go /tmp/trends.go.bak` before any
mutation; `cp /tmp/trends.go.bak internal/services/insights/trends.go` to
restore after each). Confirmed post-restore the file matched the intended
fix via `gofmt -l` (clean) and a full re-run of the package + full `go test
./...` (see Verification below).

1. **Old `> 0` guard** (`if historicalDaily > 0 { burnRateChange =
   ((dailyAvg - historicalDaily) / historicalDaily) * 100 }`, restoring the
   pre-CB8 code verbatim): FAILED
   `TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange`,
   `TestCalculateSpendingVelocity_ZeroHistoricalBaseSpendingIsPositive100`,
   and `TestCalculateSpendingVelocity_ZeroHistoricalBaseRefundIsNegative100`
   (all report 0 under the old guard; all three new tests expect nonzero).
   All other tests (including the extended
   `TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative`) still
   passed under this mutant.
2. **Signed denominator** (`burnRateChange = change / historicalDaily *
   100`, dropping `math.Abs`): FAILED
   `TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange`
   only (change is positive, historicalDaily is negative, so the signed
   division flips the result negative against the test's `BurnRateChange
   <= 0` check and the exact-match check). The two zero-base tests still
   passed under this mutant since they never divide by a nonzero
   `historicalDaily`.
3. **Zero-base hardcoded +100** (collapsing the zero-base `switch` cases to
   a single `default: burnRateChange = 100`, i.e. `metrics.PercentChange`'s
   unconditional-+100 zero-base rule): FAILED
   `TestCalculateSpendingVelocity_ZeroHistoricalBaseRefundIsNegative100`
   (expects -100, mutant reports +100). The positive-change zero-base test
   coincidentally still passed (both rules agree on +100 for a positive
   change), which is exactly why the negative-change companion test is the
   one that proves this branch — noted in that test's own comment.

## Verification

- `gofmt -l ./internal/services/insights` — no output (clean).
- `go build ./...` — clean, no output.
- `go vet ./internal/services/insights/...` — clean, no output.
- `go test ./internal/services/insights/... ./internal/handlers/insights/...`
  — both `ok` (run twice, including once with `-v`, all subtests PASS).
- `go test ./...` — run three times. Consistent result across all three
  runs: every package `ok` EXCEPT three pre-existing failures entirely
  inside task CB7's declared concurrent-edit territory (per the dispatch
  brief, CB7 is mid-editing `internal/services/metrics/**` and
  `internal/handlers/explorer/**` in this same tree):
  - `FAIL budget2/internal/handlers/explorer` —
    `TestHandleTransactionsPartial_RefundReducesTotalExpenses`
  - `FAIL budget2/internal/services/metrics` —
    `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder`,
    `TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows`
  - `FAIL budget2/internal/services/mcpsvc/spend` (downstream consumer of
    `internal/services/metrics`) —
    `TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder`
  None of these packages, files, or test names touch `insights` or
  `SpendingVelocity`; confirmed via
  `git diff --stat` that the only files this task modified are the two
  named in this manifest, and every other modified file in the tree
  (`internal/handlers/explorer/handlers.go`, `internal/models/dashboard.go`,
  `internal/services/mcpsvc/server.go`, `internal/services/mcpsvc/server_test.go`,
  `internal/services/mcpsvc/spend/summary.go`, `internal/services/metrics/metrics.go`,
  `web/templates/components/kpis.html`, `web/templates/pages/explorer.html`,
  `.swarm/NEXT.md`, `.swarm/ledger.tsv`) is on CB7's declared do-not-touch
  list. My package-scoped test command
  (`./internal/services/insights/... ./internal/handlers/insights/...`) was
  green on every run.
- `git diff --stat` — confirmed diff limited to exactly
  `internal/services/insights/trends.go` and
  `internal/services/insights/trends_test.go` (acceptance criterion d).

## Acceptance criteria

(a) Package tests and full `go test ./...` clean — package tests fully
    green; full-suite failures are pre-existing, isolated to CB7's declared
    concurrent territory (metrics/explorer/mcpsvc-spend), reproduced
    identically across three runs, and unrelated to `SpendingVelocity` or
    either changed file.
(b) Every new assertion proven load-bearing by mutation — see "Mutation
    validation" above; all three specified mutants (old guard, signed
    denominator, zero-base hardcoded +100) were each killed by a specific
    new test.
(c) Ordinary ledgers (`historicalDaily > 0`) produce a BurnRateChange
    identical to before — `change / math.Abs(historicalDaily)` equals
    `change / historicalDaily` whenever `historicalDaily > 0`, since
    `math.Abs` is a no-op on positive inputs; no test previously
    established this failed to disprove it, and all pre-existing positive-
    base tests (`TestCalculateSpendingVelocity_BurnRateChange`, etc.)
    continued to pass unchanged.
(d) No template or verdict.go change; diff limited to the two files —
    confirmed via `git diff --stat` (see Verification).
