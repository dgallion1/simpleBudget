# CB1 attempt 1 — manifest

## Files touched

- `internal/services/metrics/metrics.go` — CombinedCumulativeBalance walk:
  changed the month's `spend` term from `math.Abs(bucket.SumAmount())` to
  `-bucket.SumAmount()` (the signed negated net), so a refund-dominant month
  (outflow-typed rows netting positive) enters the walk as a credit instead
  of being charged as spend. Updated the surrounding comment and the outer
  walk-loop comment block to document the signed contract and note that
  range-level `totalExpenses` (line ~375, `math.Abs(outflows.SumAmount())`)
  is untouched.

- `internal/handlers/dashboard/handlers.go` — the dashboard chart's
  cumulative-balance walk (same shared computation, replicated for the
  chart JSON): changed the identical `spend` term at the identical site
  (was `math.Abs(bucket.SumAmount())`, now `-bucket.SumAmount()`) so both
  surfaces move together, preserving the chart-vs-metrics equality guarded
  by `plan_exclusions_chart_walk_test.go`. Updated the surrounding comment
  to document the signed contract and cross-reference the metrics.go fix.

- `internal/models/dashboard.go` — updated the `CombinedCumulativeBalance`
  field doc: states the signed-spend contract (a refund-dominant month
  enters as a credit), and documents the invariant's remaining
  precondition explicitly — the RANGE-level `TotalExpenses` still uses
  `math.Abs` over the range net, so the invariant holds only while the
  RANGE as a whole still nets outflow-negative; a wholly refund-dominant
  RANGE is out of scope and not guaranteed to satisfy the invariant.
  Range-level arithmetic itself was NOT changed, per the task's constraint.

- `internal/services/metrics/metrics_test.go` — added a new committed
  regression test,
  `TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit`.
  Two-month fixture (Jan ordinary spend month -$1800 rent; Feb
  refund-dominant: +$1100 furniture-store return net against a -$200
  utility bill, netting +$900) — different amounts than the lead's oracle
  fixture (2000/500), written independently using the package's own
  `makeTransaction`/`makeTransactionSet`/`floatEqual`/`fullCoverage`
  helpers. Asserts: (a) Jan is unchanged (harness-validity guard), (b) the
  Feb per-month step equals accrual PLUS the $900 credit (discriminates
  signed arithmetic from `math.Abs`), and (c) the documented invariant
  (`last == -CombinedCumulativeDelta`) holds with the refund-dominant month
  present. No existing test in this file encoded the buggy expectation —
  all pre-existing `CombinedCumulativeBalance` fixtures use outflow rows
  that net negative in every month, so none needed an expectation change;
  none were deleted or renamed.

### Attempt-1 touch-up (checker-tests F1/F4, comments + this manifest only)

- `internal/handlers/dashboard/handlers.go` (comment-only, no code/assertion
  change) — F1: the `nonExcludedOutflows` doc block (the one both new
  spend-site comments point readers at) still said "take ONE Abs of the
  combined signed sum"; corrected to "take ONE signed negation of the
  combined sum (CB1: -sum, not Abs...)" to match the actual arithmetic now
  in the walk.

- `internal/handlers/dashboard/plan_exclusions_chart_walk_test.go`
  (comment-only, no assertion change) — F1: the mismatch error string at
  the chart-vs-metrics equality check still said "take ONE Abs"; corrected
  to "take ONE signed negation, CB1: -sum not Abs" to match.

## Point 4 (existing tests encoding the buggy expectation)

None found. Every pre-existing `CombinedCumulativeBalance` test fixture
(`_NoTargetReturnsNil`, `_AccumulatesMonthlyBalance`,
`_LastIsNegationOfCumulativeDelta`, `_MoreThanSixMonths_CapsAndCarriesIn`,
`_PartialMonthRange`, `_ZeroTransactionMiddleMonth`) uses only outflow rows
that net negative per month, so `math.Abs(x)` and `-x` agree and none of
their expected values changed. All pass unmodified after the fix.

## Point 5 (observation only, not fixed — backlog)

Corrected/expanded per checker-tests F4: five sibling per-month `math.Abs`
sites carry the same refund-dominant exposure as the two walk sites this
task fixed (not two — the earlier draft under-enumerated the surfaces):

1. `internal/handlers/dashboard/handlers.go:1053` — `hcAmt =
   math.Abs(hc.SumAmount())`, the Healthcare bar-chart trace value.
2. `internal/handlers/dashboard/handlers.go:1062` — `livingMonth =
   math.Abs(lo.SumAmount())`, the Living bar-chart trace value.
3. `internal/services/metrics/metrics.go:500` — `expAmt =
   math.Abs(exp.SumAmount())`, feeding `ExpensesTrend`.
4. `internal/services/metrics/metrics.go:505` — `hcAmt =
   math.Abs(hc.SumAmount())`, feeding `HealthcareTrend`.
5. `internal/services/metrics/metrics.go:513` — `livingMonth =
   math.Abs(lo.SumAmount())`, feeding `LivingExpensesTrend`.

All five have the SAME refund-dominant exposure as the walk this task
fixed: if a month's outflow-typed rows in the relevant bucket (all
outflows, healthcare, or living) net positive (e.g. an insurer
overpayment refund, or a large returned purchase), that trend/bar series
will show a positive "spend" value for that month instead of reflecting
the net credit. Sites 1-2 feed the dashboard chart's bar traces; sites
3-5 feed `DashboardMetrics.ExpensesTrend` / `HealthcareTrend` /
`LivingExpensesTrend` (a different basis than the cumulative-balance walk
per metrics.go's own doc, computed in a separate per-transaction-month
loop). This is the same defect CLASS (per-month `math.Abs` on a bucket
that can net positive) but on DIFFERENT surfaces from the
cumulative-balance walk/line this task fixed, and is explicitly out of
this task's scope (task block point 5: "OBSERVE and report (do not
fix)"). Recommend a follow-up task if the lead wants these trend/bar
series to display refund-dominant months as credits too (would likely
require rendering a negative-height bar or a distinct visual treatment
for the bar traces — a content/design decision, not purely arithmetic —
while the trend-array sites could take the same signed-negation fix
mechanically).

## Verification run (from worktree root)

- `go build ./...` — clean, no output.
- `go vet ./...` — clean, no output.
- `go test -count=1 ./internal/services/metrics/ ./internal/handlers/dashboard/`
  — both packages `ok`.
- `bash .swarm/tier3/CB1/accept.sh` — final line `ORACLE PASS`.

No binary was built or executed at any point (constraint honored — only
`go build`/`go vet`/`go test`).

Not committed, not pushed, no other checkout touched, per task
instructions. `.swarm/tier3/CB1/` was not modified.
