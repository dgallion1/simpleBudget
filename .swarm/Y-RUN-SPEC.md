# Y run — Budget sparkline basis reconciliation

Commissioned 2026-08-29 by the user (single scoped fix). Ledger prefix `Y`
(`W`, `X` taken; per-run-prefix lesson from tier3-oracle-methodology).
Isolated worktree `.claude/worktrees/y-sparkline`, branch
`fix/sparkline-budget-basis` off origin/master @ 8ed11c3 (the
fix/phase-visibility merge). The main tree is mid-flight with the X run and
is not touched by this run.

## Rulings

- **Y-2026-08-29a (run completion):** the merged ledger carries the X run's
  `pending` rows, which belong to another session's run. This run is complete
  when `swarm/gate.sh check Y1` exits 0 (plus `escalate-scan` showing no Y
  flag); `gate.sh done` is the X run's close-out and is not gated on here.
- **Y-2026-08-29b (full-suite verification):** this run has an isolated
  worktree, so X-2026-08-29a's package-scope concession does NOT apply.
  Workers and checkers run the full `go build ./...` / `go vet ./...` /
  `go test ./...`.

## Defect record (source: .swarm/verdicts/W4.4.judge-claude.verdict, ruling 2026-08-29d)

The Budget KPI card's headline/tint/rows classify
`CombinedCumulativeDelta = totalSpend − combinedTarget·MonthsBetween(start,end)`
(fractional months, days/30.4375). The card's sparkline plots
`CombinedCumulativeBalance`, built as `combinedTarget·1 − spend` per whole
transaction-month, truncated to the last six months. The two bases diverge by
`combinedTarget·(len(trendMonths) − MonthsInRange)`; measured at live scale
the sparkline's zero crossing sits at delta ≈ +$375 for a Jan–Mar range, so
the chart shows green while the card says "$300 over" (and vice versa).
Additionally charts.js colors balance sparklines by bare `v>0`/`v<0` with no
neutral band, while the card applies a $1 dead band (`onBudgetEps`).
`metrics_test.go` `…_LastIsNegationOfCumulativeDelta` papers over this with
$100 of slack.

## Y1 — sparkline shares the card's month basis and dead band

Tier 2, checks `tests,a11y,second`. Territory:
- `internal/services/metrics/metrics.go`, `internal/services/metrics/metrics_test.go`
- `internal/models/dashboard.go` (field doc comment only)
- `internal/handlers/dashboard/verdict.go` + its test files
- `web/templates/components/kpis.html` (the `sparkline-budget` div only)
- `web/static/js/charts.js`
Nothing else.

### Lead design decisions (not worker choices)

**D-Y1-basis** — `CombinedCumulativeBalance` moves to a calendar-month walk
over `[rangeStart, rangeEnd]`:
- For each calendar month intersecting the range: `segStart` = later of
  (first day of month, rangeStart); `segEnd` = earlier of (last day of month,
  rangeEnd); `accrual = combinedTarget · MonthsBetween(segStart, segEnd)`;
  `spend` = abs sum of that month's `monthlyOutflows` bucket (this equals the
  old `livingMonth + hcAmt`); `running += accrual − spend`; append `running`.
- The per-month inclusive-day segments partition the range's inclusive days,
  so accruals sum to `combinedTarget · MonthsInRange` exactly; `Calculate`'s
  callers all pass a set pre-filtered to the range (verified:
  dashboard/handlers.go:93+96, 177+180; mcpsvc/spend/summary.go:285), so
  spends sum to `totalExpenses`. Invariant: the final element equals
  `−CombinedCumulativeDelta` up to float noise. Document the pre-filtered
  precondition on the `CombinedCumulativeBalance` field comment in
  `internal/models/dashboard.go`.
- A month with no transactions still produces a point (target accrues,
  nothing spent).
- Six-month cap: KEPT, as a display slice of the LAST 6 points of the walked
  series. Running totals retain the dropped months' carry-in, so the cap
  trims what is plotted, never the accounting, and the tail invariant
  survives any range length.
- The other trend arrays (`incomeTrend`, `expensesTrend`, `savingsTrend`,
  `healthcareTrend`, `livingTrend`, `trendLabels`) are UNCHANGED — still
  transaction-month based, still capped at 6 by the existing code. Build the
  balance series in its own loop; do not couple it to the trend loop.
- Series still emitted only when `hasCombinedTarget`.

**D-Y1-deadband** — the sparkline mirrors the card's `onBudgetEps` ($1):
- `verdict.go`: `BudgetVerdictView` gains `Eps float64`, set unconditionally
  to `onBudgetEps` in `BuildBudgetVerdict`. No classification change.
- `kpis.html`: the `sparkline-budget` div gains
  `data-neutral-band="{{.BudgetVerdict.Eps}}"`. No numeric threshold literal
  in the template (ruling 2026-08-29a lineage).
- `charts.js` `initSparklines`: parse `data-neutral-band` →
  `options.neutralBand` (finite and > 0 only, same guard style as target).
- `charts.js` `renderSparkline`, balance mode only: with
  `nb = options.neutralBand` (default 0), the green fill clamps at `v > nb`,
  the red fill at `v < −nb`; values inside `±nb` feed neither fill. The main
  line trace and the zero baseline are unchanged. Update the JSDoc.
- Consequence (the point of the task): sign stories now agree by
  construction. `delta > eps` ⇔ tail `< −eps` → red; `delta < −eps` ⇔ tail
  `> eps` → green; `|delta| ≤ eps` → neutral tail, matching "On budget".

**D-Y1-tests**
- Tighten `TestCalculateMetrics_CombinedCumulativeBalance_LastIsNegationOfCumulativeDelta`:
  slack $100 → $0.01; rewrite the comment to state the reconciled
  relationship (exact negation, float noise only).
- Update the existing per-month balance test (~line 895: Jan +500 / Feb 0) to
  the new fractional accruals, with the derivation shown in comments.
- New tests, each asserting tail == −delta within $0.01:
  1. range spanning > 6 calendar months → `len(series) == 6` AND the tail
     invariant holds (proves carry-in).
  2. partial-month range (mid-month start and end dates).
  3. a middle month with zero transactions → it still gets a point
     (`len(series)` == calendar months in range) and the invariant holds.
- Red-green evidence: write the tightened/new tests first, run against the
  unmodified algorithm, capture the failure output, then implement and show
  green. Include both outputs in the final report.
- `verdict.go` tests: `Eps` populated with 1.0.
- Full `go build ./...`, `go vet ./...`, `go test ./...` green
  (Y-2026-08-29b).

### Acceptance criteria (checker-facing)

1. `go build ./...`, `go vet ./...`, `go test ./...` all green in this
   worktree.
2. `go test ./internal/services/metrics/ -run CombinedCumulativeBalance -v`
   exercises: multi-month calendar range, partial-month range, >6-month
   range (cap + carry-in), empty-middle-month range; each asserts the tail
   equals `−CombinedCumulativeDelta` within $0.01.
3. The `> 100` slack literal is gone from metrics_test.go.
4. charts.js: only balance mode changed; fills honor `neutralBand`; the
   attr→options plumbing exists in `initSparklines`.
5. No numeric eps literal in kpis.html; the band value flows from
   `onBudgetEps` via `BudgetVerdictView.Eps`.
6. A >6-month range still yields at most 6 sparkline points.
7. Trend arrays and labels byte-identical behavior (their code path
   untouched by the diff).
8. Manifest at `.swarm/manifests/Y1.<attempt>.files` lists every touched
   file, repo-relative, one per line.

## Rulings (adjudications)

(none yet)
