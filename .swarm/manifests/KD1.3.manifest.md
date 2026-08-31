# KD1 attempt 3 — ruling KD-2026-08-30d (signed rows, no per-month Abs) + regressions

Tier: **3** for this attempt (oracle-first, per the coordinator's contract
rewrite). Oracle: `.swarm/tier3/KD1/accept.sh` → `ORACLE PASS`.

## The three-line production fix (KD-2026-08-30d)

Prior attempts rendered each living/healthcare month row as
`Abs(signed sum)` — always non-negative, discarding whether the month netted
spend or a refund. Two same-class checker fails (attempt 1's "$0.00 beside
a non-zero row", attempt 2's reconciliation gap) both traced back to the
same root cause: per-month Abs throws away sign information the Total tile,
the CSV export, and the month drill-down all need to agree on. Ruling
KD-2026-08-30d rewrites the contract: a month row is the **negated signed
sum** (positive = net spend, negative = net refund, matching the MCP
`by_month` convention), and the Total tile is the **sum of the displayed
month values** — one rounding path, so rows and Total always reconcile
exactly by construction, not by coincidence.

### internal/handlers/dashboard/handlers.go — the fix
1. `classifiedMonthlyTotals`: `totals[m] = math.Abs(set.SumAmount())` →
   `totals[m] = -set.SumAmount()`. This single change propagates to the
   modal's month rows (which read this map), the modal's Total tile (which
   sums the same `values` slice the rows populate), AND the CSV export
   (`handleKPIExport` already called this same shared function since
   attempt 2's K8 fix) — so all three surfaces reconcile automatically from
   one change, exactly as the coordinator specified.
2. `handleKPIMonthDetail` case `"living"`: `math.Abs(sumSigned(monthLiving))`
   → `-sumSigned(monthLiving)`.
3. `handleKPIMonthDetail` case `"healthcare"`:
   `math.Abs(sumSigned(monthHealthcare))` → `-sumSigned(monthHealthcare)`.

Doc comments updated to describe the new convention (coordinator item 6):
`classifiedMonthlyTotals`'s doc now explains the negated-signed-sum
contract, the reconciliation guarantee, and the documented net-refund
divergence from the card's Abs-based figure; the per-month loop's "Set
exclusion" comment and both drill-down case comments were updated to match.

## Regressions from attempt 2 (also fixed this attempt)

### 4. K5 whitespace regression — `web/templates/components/kpi-detail.html`
Attempt 2's `{{if}}/{{else}}/{{end}}` wrappers (Per Month tile,
vs-Avg cell) left the pre-existing kinds' rendered HTML with stray blank
lines (the raw newline+indentation surrounding the control-flow tags leaked
into the "false" branch's output even though no visible content changed).
Fixed by adding `{{- -}}` trim markers on both wrappers:
```
{{if and (eq .Type "healthcare") .HealthcareNoCoverageInRange -}}
...true branch...
{{- else -}}
...false branch (unconditional prefix/suffix reproduce master exactly)...
{{- end}}
```
and identically for the vs-Avg `{{if $noCoverage -}} ... {{- else -}} ...
{{- end}}` cell. Traced by hand: for the false path (the four pre-existing
kinds, where the condition is always false), the trimmed output is now
byte-identical to what a single unconditional line would have produced —
matching the pre-KD1 baseline exactly. The true (no-coverage) path still
renders clean, correctly-indented markup.

### 5. A11y contrast fix — `web/templates/components/kpi-detail.html`
Both "—" (em-dash) elements attempt 2 introduced used `dark:text-gray-400`
(≈4.06:1 against the dark modal background — fails WCAG AA 4.5:1 for body
text). Changed to `dark:text-gray-300` (≈7.00:1, passes AA and AAA) in both
places: the Per Month tile's "no coverage in this range" subtext, and the
vs-Avg column's dash cell.

## Tests (`internal/handlers/dashboard/handlers_http_test.go`)

- **7(a)** `TestHandleKPIDetail_LivingSignedRowsReconcileWithRenderedTotal`
  — refund-dominant two-month living fixture (Jan: +500 refund only, Feb:
  Rent -1000). Asserts on RENDERED STRINGS (ruling 2026-08-29b, not parsed
  floats): the Jan row renders `formatMoneyExpected(-500)`, the Feb row
  renders `formatMoneyExpected(1000)`, the rendered Total equals the sum of
  those two rendered figures (`formatMoneyExpected(500)`), and — since this
  range nets spend overall — that same rendered Total also equals
  `formatMoney(Metrics.LivingExpensesTotal)`.
- **7(b)** `TestHandleKPIMonthDetail_HealthcareSignedNotAbs` — a
  refund-dominant healthcare month (Feb: a $100 "Health Insurance Autopay
  Reversal" refund, no spend) drilled down via `/dashboard/kpi/healthcare/month/2025-02`;
  asserts `Total == -100` (negated signed sum, not `Abs`), covering
  checker-second's coverage-gap observation for the month-drill-down route
  specifically (the list-level equivalent is in the oracle).
- **7(c)** Existing-expectation adjustment: the single line in
  `TestHandleKPIDetail_Healthcare_NoCoverageInRange_ShowsDashNotZero` (added
  attempt 2) asserting the dash cell's CSS class string was updated from
  `dark:text-gray-400` to `dark:text-gray-300` to match item 5's a11y fix —
  a mechanical consequence of that fix, not a behavior relaxation. No other
  existing test needed adjustment: every other attempt-1/2 fixture used in
  this package is single-month-net-spend (per the coordinator's prediction),
  where negated-signed-sum and `Abs` produce the identical figure, so those
  assertions (K2, K3, K4, K8's export tests) are numerically unaffected by
  the sign-convention change.

## Oracle result

```
$ bash .swarm/tier3/KD1/accept.sh
== KD-2026-08-30d reconciliation + regressions ==
=== RUN   TestOracleKD1_LivingSignedRowsReconcile
--- PASS: TestOracleKD1_LivingSignedRowsReconcile (0.01s)
=== RUN   TestOracleKD1_HealthcareSignedRowsReconcile
--- PASS: TestOracleKD1_HealthcareSignedRowsReconcile (0.00s)
=== RUN   TestOracleKD1_ExportMatchesSignedRows
--- PASS: TestOracleKD1_ExportMatchesSignedRows (0.00s)
=== RUN   TestOracleKD1_MonthDrillMatchesSignedRow
--- PASS: TestOracleKD1_MonthDrillMatchesSignedRow (0.00s)
=== RUN   TestOracleKD1_NoCoverageDashRegression
--- PASS: TestOracleKD1_NoCoverageDashRegression (0.02s)
PASS
ok  	budget2/internal/handlers/dashboard	0.032s
== package suites ==
ok  	budget2/internal/handlers/dashboard	0.808s
ok  	budget2/internal/services/metrics	0.003s
ORACLE PASS
```

Both-ends validation performed independently (not just trusting the
coordinator's prototype claim): ran the oracle against the tree BEFORE
applying the three-line fix — `TestOracleKD1_LivingSignedRowsReconcile`,
`_HealthcareSignedRowsReconcile`, `_ExportMatchesSignedRows`, and
`_MonthDrillMatchesSignedRow` all failed with exactly the expected
"want -500/-100, got 500/100"-shaped messages (log preserved at
`/tmp/kd1_oracle_run.log` locally, not committed); `_NoCoverageDashRegression`
already passed at both ends (it guards attempt 2's fix, untouched by this
attempt's change) — confirming the oracle discriminates the defect
correctly before the fix is applied, and passes cleanly after.

## Other verification

```
$ go build ./...
Go build: Success

$ go vet ./...
(clean, no output)

$ gofmt -l internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_http_test.go
(no output — both gofmt-clean)

$ grep -n "Health Insurance" internal/handlers/dashboard/handlers.go
0 matches for 'Health Insurance'   [K6 grep gate: PASS, unchanged]

$ go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/
ok  	budget2/internal/handlers/dashboard	0.666s
ok  	budget2/internal/services/metrics	0.003s
```

## Deviations / notes for checkers

- None from the coordinator's contract. Files touched: exactly the three
  named (`handlers.go`, `handlers_http_test.go`, `kpi-detail.html`).
  `.swarm/tier3/KD1/*` was read but not modified (the accept.sh script
  stages/unstages its own oracle test file via `cp`/`trap cleanup EXIT`; I
  never edited it).
- The oracle test file `.swarm/tier3/KD1/zz_oracle_kd1_test.go` briefly
  exists at `internal/handlers/dashboard/zz_oracle_kd1_test.go` only while
  `accept.sh` runs and is removed by its own `trap cleanup EXIT` — confirmed
  absent from the tree after every run (`git status --porcelain` shows no
  such file).
- Per Tier-3 discipline, the "vs-Avg column compares the signed value
  against the displayed per-month rate" requirement (ruling KD-2026-08-30d)
  needed NO template change: the vs-Avg `<td>` already read `.Value`
  directly (attempt 1) and `$avg` is already the card's single per-month
  figure (attempt 1, ruling KD-2026-08-30a/b) — the Go-side sign change
  alone makes that comparison operate on signed values, matching the
  ruling's intent without touching that cell's markup.
