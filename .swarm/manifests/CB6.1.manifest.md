# CB6.1 manifest (attempt 1)

## Files changed

- `internal/services/insights/trends_test.go` — added
  `TestAnalyzeMajorExpenseTrends_DirectionBandEdges`, a table test pinning
  `MajorExpenseTrends`' +-5 direction band exactly at its edges
  (changePercent=+5/-5 -> "stable"; just past each edge -> "up"/"down").
  No production-code change.
- `web/static/js/charts.js` — CB6-2: credits header text changed to "Net
  credits (refunds met or exceeded spending)". CB6-3: added a
  `triggerMajorExpenseDrilldown(name)` helper mirroring the donut wedge
  click handler's `typeof openMajorExpenseDrilldown === 'function'` guard;
  `renderMajorExpenseBreakdown` and `renderMajorExpenseCredits` now build
  each row as a real `<button type="button">` (was a plain `<div>`) wired
  to that helper, reusing only utility classes already present elsewhere
  in the tree (`w-full text-left rounded hover:bg-gray-50
  dark:hover:bg-gray-700 focus:outline-none focus-visible:ring-2
  focus-visible:ring-indigo-500`, the same focus-visible ring idiom as
  kpi-month-detail.html's sort buttons).
- `web/templates/components/kpi-month-detail.html` — CB6-4: line ~49's
  Total-tile class expression changed from `{{if eq .Type
  "income"}}green{{else}}red{{end}}` to `{{if eq .Type
  "income"}}green{{else if gt .Total 0.0}}red{{else}}green{{end}}`, so
  expense-like kinds (expenses/living/healthcare — the only non-income,
  non-savings kinds `handleKPIMonthDetail`'s switch produces) render red
  only when Total > 0 (money out) and green when Total <= 0 (net
  refund/credit). income stays unconditionally green. The savings/
  savings-rate branch (line ~30, `{{if ge .Total 0.0}}`) was already
  correct per the spec's contract and is untouched. Reused the file's
  exact existing class pairs (green-700/green-400, red-600/red-400) — no
  new utilities.
- `internal/templates/render_kpi_month_detail_test.go` — added three
  render tests following the file's existing style/fixture helper:
  `TestRenderKPIMonthDetail_ExpensesNegativeTotalIsGreen`,
  `TestRenderKPIMonthDetail_ExpensesPositiveTotalIsRed`,
  `TestRenderKPIMonthDetail_SavingsPositiveTotalIsGreen`. They assert on
  the rendered class+formatted-money substring, using the package's own
  `formatMoney` helper (same package) rather than reimplementing it.
- `internal/handlers/dashboard/handlers.go` — CB6-5: in
  `handleMajorExpenseDrilldown`, moved the total/count/avgAmount
  computation to run BEFORE `sort.Slice(txns, ...)` (was after), with a
  comment explaining the float64 summation-order rationale: for the
  "default" (named-bucket) case `txns` here is the same slice
  `bucketMajorExpenses` built (`b.txns`), still in `match.Groups`' match
  order — the exact order `bucket.total` was summed in — so summing
  before the display sort reorders the slice in place pins this handler's
  Total to be bit-identical to the list-row figure it must agree with.
  Display sort (date-descending) is unchanged, just moved after.

## CB6-1 band-mutation validation

Applied a throwaway edit to `internal/services/insights/trends.go`
collapsing the classifier's `> 5` / `< -5` band to `> 0` / `< -0`, ran
`go test ./internal/services/insights/ -run
TestAnalyzeMajorExpenseTrends_DirectionBandEdges -v`: the mutation FAILED
2 of 4 subtests (`exactly +5 is stable, not up` reported Direction="up";
`exactly -5 is stable, not down` reported Direction="down") — confirming
the new test kills the ±0-band mutation. Reverted the file immediately
after (`git diff --stat internal/services/insights/trends.go` shows no
diff post-revert); the test passes cleanly against the unmutated
classifier.

## CB6-5 test-or-comment decision

Took the "comment + existing cross-surface tests suffice" option — no new
regression test added. The order-dependent float64 divergence the spec
describes (~7e-15) only manifests with many transactions summed in two
different orders and is sub-display-precision (invisible at the two-decimal
`formatMoney` rendering used everywhere this Total is shown); constructing
a deterministic minimal fixture that reliably reproduces a *distinguishable*
order-dependent float64 result would be fragile and wouldn't demonstrate
anything a reader can't already see from the comment. The existing
cross-surface tests (`TestHandleMajorExpenseDrilldown_RefundInNormalGroupSignedNet`,
`TestHandleMajorExpenseDrilldown_RefundDominantGroupSignedNet`,
`TestHandleMajorExpenseDrilldown_NetNegativeGroupResolvesSignedTotal`) still
pass unchanged and continue to pin the Total's *value* contract; they were
re-run as part of the full package test below.

## Verification

- `go build ./...` — clean, no output.
- `go vet ./...` — clean, no output.
- `go test -count=1 ./internal/services/insights/ ./internal/handlers/dashboard/ ./internal/templates/`
  — all `ok`.
- `go test -count=1 ./...` — all packages `ok` (full list included every
  package in the module; no failures).
- `make check` (vet, staticcheck, govulncheck, css-verify, test) — all
  green; `make css-verify` output: `tailwind.css is up to date` (no new
  Tailwind utility classes were introduced — every class added to
  charts.js already exists verbatim elsewhere in the scanned content, so
  the compiled stylesheet is unchanged).

## Scope note

Touched exactly the five files the spec named plus their tests. No
arithmetic changes anywhere (CB6-5 changes summation ORDER only, not the
formula); no new Tailwind classes; no production-code change for CB6-1.
