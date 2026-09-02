TASK: CB4
ATTEMPT: 1
WORKER: worker-coder

FILES CHANGED
- internal/handlers/dashboard/handlers.go — bucketMajorExpenses drops the `total > 0` filter (includes every matched group with >=1 txn, any sign); added splitPositiveMajorExpenseBuckets helper (positive-vs-credit partition on the already-descending-sorted list); buildMajorExpenseChartData applies the donut's positive-only geometry filter locally (comment marks it as such), slices the donut-limit against positive buckets only, and adds a new "credits" field ({name, amount}, no percent) for the excluded zero/negative buckets; handleMajorExpenseDrilldown's "Other" case now filters to positive buckets before slicing at majorExpenseDonutLimit so a trailing net-negative group can never leak into the "Other" drilldown (this was the one non-trivial logic change flagged as possible in the task brief — confirmed necessary by test, see FLAGS below).
- internal/handlers/dashboard/handlers_http_test.go — added regression tests: net-negative group → credits (not a wedge, no percent key); zero-total-with-transactions group → credits; donut-limit slice applies to positive buckets only with a trailing run of negatives (both the >limit and exactly-at-limit boundary cases); all-negative edge case (empty donut, full credits, no NaN); positive-only fixture carries no "credits" key (parity with pre-CB4 shape); drilldown resolves a net-negative group by name with its signed Total; drilldown "Other" excludes a trailing negative group's transactions.
- web/static/js/charts.js — added renderMajorExpenseCredits(items), called alongside the existing renderMajorExpenseBreakdown after each major-expense chart render; renders a "Net credits (refunds exceeded spending)" list into #chart-major-expense-credits using the same DOM-textContent-only pattern as the existing "Other categories" breakdown (no innerHTML); amounts formatted via the same Intl.NumberFormat currency formatter (renders the sign automatically); clears/renders nothing when items is empty or missing (defensive against an old server response with no "credits" field).
- web/templates/pages/dashboard.html — added the `#chart-major-expense-credits` sibling div under the existing `#chart-major-expense-breakdown` div, same container the JS targets.

FLAGS
- Scope note: handleMajorExpenseDrilldown's "Other" case required a real logic change, not just verification. Task brief anticipated this ("No logic change expected — if one turns out to be needed, make the minimal one"). Without the fix, a fixture with exactly majorExpenseDonutLimit positive buckets plus trailing negative buckets would incorrectly fold those negatives into the "Other" drilldown's transaction list and Total (a regression test — TestHandleMajorExpenseDrilldown_OtherExcludesNegativeTail — proves this). The fix reuses the same splitPositiveMajorExpenseBuckets helper the chart builder uses, so the two call sites cannot drift apart on which buckets are "Other"-eligible.
- No changes made outside the named functions/tests/JS+template render site. Did not touch internal/services/insights/trends.go (a separate, out-of-scope MajorExpenseTrends function that mentions the same signed-net contract in a comment but does not call bucketMajorExpenses).

VERIFICATION
- go build ./... — clean
- go vet ./... — clean
- go test -count=1 ./internal/handlers/dashboard/ — PASS (includes all new CB4 tests + full pre-existing suite for the package)
- go test -count=1 ./... — PASS (all packages)
- staticcheck ./internal/handlers/dashboard/... ./web/... — exit 0, no findings
- make css-verify — "tailwind.css is up to date" (new template div reuses existing utility classes verbatim, no new Tailwind classes introduced)
