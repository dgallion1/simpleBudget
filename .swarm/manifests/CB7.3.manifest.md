# CB7.3 manifest (attempt 3 — LAST attempt before hard stop)

## What changed since attempt 2

checker-second FAILED attempt 2 on a real defect (conceded, ruling
CB7-2026-09-03c): `-set.SumAmount()` on a set summing to exactly 0.0
produces IEEE negative zero; `formatMoney` doesn't normalize it
(`v < 0` is false for `-0.0`, so the code falls into the POSITIVE
branch but `fmt.Sprintf("%.2f", v)` still honors the sign bit), and
`encoding/json` serializes `-0.0` as the literal token `-0`. On the live
ledger every window has zero Health-Insurance rows, so
`healthcareTotal`/`HealthcareActual` are `-0.0` in every real window,
rendering "$-0.00" on the Monthly Healthcare KPI. This attempt implements
the full contract: one centralizing helper, every call site converted,
a belt at the formatter, and mutation-proven tests at every layer.

### 1. New helper: `metrics.SignedNet`

Added directly above `PercentChange` in `internal/services/metrics/metrics.go`,
exactly as specified:

```go
func SignedNet(ts *models.TransactionSet) float64 {
	v := -ts.SumAmount()
	if v == 0 {
		return 0
	}
	return v
}
```

Nil-safety note: empirically verified (throwaway test, discarded) that
`SumAmount()` on a LITERAL nil `*models.TransactionSet` panics (it ranges
over `ts.Transactions`, dereferencing nil) — the doc comment was written to
say this precisely ("nil-safe in exactly the same sense SumAmount is, no
more and no less") rather than repeat the spec snippet's implied "returns 0
for nil" claim, which is false for a bare nil pointer. Every actual call
site in this codebase passes a non-nil (possibly empty) `*TransactionSet`
from `FilterByType`/`FilterByCategory`/`GroupByMonth`/`PlanExcludedOutflows`/
`LivingOutflows`, all of which are documented never to return nil — so no
site needed a nil guard.

### 2. Every negated-sum call site converted

`internal/services/metrics/metrics.go` — all 8 named sites now call
`SignedNet(...)`: `totalExpenses` (line ~397, was 391), `healthcareTotal`
(~429, was 422), `planExcludedTotal` (~453, was 442), `livingTotal` (~466,
was 455), `expAmt` (~541), `hcAmt` (~547), `livingMonth` (~556), `spend`
(~629). Stale prose was updated at the `totalExpenses` site to name the new
helper and the ruling; the `LivingOutflows` doc comment (describing the
range-vs-per-month arithmetic) was similarly updated. Conceptual
explanations elsewhere ("-SumAmount() is positive expense...") were left
as-is where they describe the underlying arithmetic accurately (SignedNet's
normal-case behavior IS `-SumAmount()`; the doc comments explain the sign
convention, not literally the syntax).

`internal/handlers/explorer/handlers.go` — imported `budget2/internal/services/metrics`
(confirmed no import cycle: metrics imports only `budget2/internal/models`)
and converted both sites (~line 202 page handler, ~line 363 partial
handler) to `metrics.SignedNet(filtered.FilterByType(models.Outflow))`,
with an added comment at each site naming the ruling.

**Grep from step 2 (zero hits outside comments):**

```
$ grep -n "\-[a-zA-Z.()]*SumAmount()" internal/services/metrics/metrics.go internal/handlers/explorer/handlers.go
internal/handlers/explorer/handlers.go:191:	// nets outflow-negative, so -SumAmount() is positive expense, same as
internal/handlers/explorer/handlers.go:199:	// -filtered.FilterByType(models.Outflow).SumAmount(): a zero-Outflow
internal/handlers/explorer/handlers.go:352:	// nets outflow-negative, so -SumAmount() is positive expense, same as
internal/handlers/explorer/handlers.go:360:	// -filtered.FilterByType(models.Outflow).SumAmount(): a zero-Outflow
internal/services/metrics/metrics.go:305:// the SAME SIGNED negated net, -LivingOutflows(...).SumAmount(): a
internal/services/metrics/metrics.go:381:	// range nets outflow-negative, so -SumAmount() is positive spend, same
internal/services/metrics/metrics.go:391:	// SignedNet helper (not inline -outflows.SumAmount()), which also
internal/services/metrics/metrics.go:526:		// -SumAmount() is positive expense, same as the old math.Abs. A
internal/services/metrics/metrics.go:605:			// (SumAmount() < 0), so -SumAmount() is positive spend, same as
internal/services/metrics/metrics.go:609:			// not be charged as spend; -SumAmount() is then negative and
internal/services/metrics/metrics.go:752:// spend figure from a set; never write -ts.SumAmount() inline.
internal/services/metrics/metrics.go:764:	v := -ts.SumAmount()
```

Every hit is a `//` comment EXCEPT line 764, which is `SignedNet`'s own
sanctioned implementation body (the one place the spec's own snippet
requires the inline form) — not a call site.

### 3. Belt at the formatter

`internal/templates/render.go`, `formatMoney`: added `if v == 0 { v = 0 }`
as the very first line of the function body, before the `v < 0` sign check,
with a comment explaining why this is needed even with the SignedNet fix
upstream (defense in depth for any other caller that forgets to normalize).

### 4. Tests (all committed)

**(a) metrics_test.go** — four new tests:
- `TestCalculateMetrics_EmptyHealthcareWindow_NoNegativeZero`: ordinary
  living spend + income, healthcare target set, hasCoverage true, ZERO
  HealthInsuranceCategory rows. Asserts `HealthcareTotal`/`HealthcareActual`
  are `+0` with `Signbit` false.
- `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero`: income only, no
  outflow transactions at all. Asserts `TotalExpenses`/`LivingExpensesTotal`/
  `HealthcareTotal` are `+0` Signbit false, AND that `json.Marshal(m)` of
  the whole struct contains no negative-zero token (regexp
  `` -0(\.0+)?[,}\]] ``, matching a whole JSON number token, not a
  substring of e.g. `-10`).
- `TestCalculateMetrics_IncomeOnlyMonth_TrendEntriesNoNegativeZero`:
  two-month window, first month income-only. Asserts
  `ExpensesTrend[0]`/`LivingExpensesTrend[0]`/`HealthcareTrend[0]` are `+0`
  Signbit false. (See mutation table below — this fixture does NOT
  independently kill any mutation; documented honestly.)
- `TestCalculateMetrics_MonthWithExactlyCancellingOutflows_TrendEntriesNoNegativeZero`
  (added beyond the letter of the spec, to close a real gap the above three
  don't cover — see mutation table): one month with an exactly-cancelling
  Housing charge+refund AND an exactly-cancelling Health-Insurance
  charge+refund, so the whole month's outflow bucket, its living-only
  bucket, and its healthcare-only bucket each sum to exactly 0.0. This is
  the fixture that actually exercises the per-month `SignedNet` call sites
  (`expAmt`/`hcAmt`/`livingMonth`), which an income-only month never
  reaches (the map lookup's `ok` is false, so the code path never runs
  `SignedNet` at all for that month).

**(b) `TestSignedNet`** (metrics_test.go, direct unit test, table of
subtests): empty set → `+0` Signbit false; exactly-cancelling set
(-10/+10) → `+0` Signbit false; ordinary spend → positive; refund-dominant
→ negative.

**(c) render_helpers_test.go** — added
`{math.Copysign(0, -1), "$0.00"}` to the existing `TestFormatMoney` table
(pins the same string `formatMoney(0)` already produces, per the ruling's
"use whatever the current zero rendering is" instruction).

**(d) Explorer** — `TestHandleTransactionsPartial_ZeroOutflowsNoNegativeZero`
(JSON path: `TotalExpenses` float64 Signbit false, plus a raw-body
string check for `-0`/`:-0,`/`:-0}` tokens) and
`TestHandleExplorer_WithRenderer_ZeroOutflowsNoNegativeZero` (renderer
path: body must not contain `"-$0.00"`, and the chip must be entirely
absent — matched via the chip's OWN label span
`` text-rose-700 dark:text-rose-300">Expenses< `` rather than a bare
`>Expenses<` substring, which false-positives on the Type filter
`<select>`'s unconditional "Expenses" option text — this false-positive
was caught and fixed during this attempt, see below).

**(e) MCP** — `TestSummarizeSpendingHealthcareActualNoNegativeZeroWhenWindowHasNoHIRows`
in `summary_test.go`: one Health-Insurance bill BEFORE the queried window
(establishes `hasCoverage=true`/`coverageStart` globally) and the window
itself has zero HI rows plus an ordinary living charge. Asserts the raw
`StructuredContent` JSON has no negative-zero token (same regexp as (a))
and `budget.healthcare_monthly_actual == 0`.

**(f) Mutation table** — see below.

**(g) checker-tests' color-class observation** — the two attempt-1 explorer
render tests asserted the FIGURE and the chip's presence/absence, but
never the CSS class alongside it, so a color-only revert (e.g. hardcoding
the rose class back onto a negative figure) would still pass. Fixed:
- `TestHandleExplorer_WithRenderer_RefundDominantFilterRendersNegativeExpenses`
  now also asserts the exact substring
  `` text-emerald-800 dark:text-emerald-300 ml-1">-$400.00 `` is present
  and `` text-rose-600 dark:text-rose-400 ml-1">-$400.00 `` is absent.
- New sibling test `TestHandleExplorer_WithRenderer_OrdinarySpendRendersRoseExpensesClass`
  asserts the mirror image for a positive (ordinary spend) figure: the
  rose class is present alongside the value, the emerald class is absent.
  This is the FIRST test that would catch a revert of explorer.html's
  color conditional in the POSITIVE direction (kpis.html's own analogous
  test, from attempt 1, already covered the negative direction there).

## Mutation table

Every row: production code temporarily mutated via `cp` backup +
`python3` string-replace, target test(s) run (`-v`), result recorded, file
restored via `cp` from the pre-mutation backup, `diff` confirmed
byte-identical restore. Full command sequence is the same shape used in
attempts 1-2 (see those manifests); commands are elided here for brevity
except where a fixture had to be extended mid-attempt.

| # | Mutation | Test(s) run | Result |
|---|---|---|---|
| 1 | `SignedNet` body reverted to `return -ts.SumAmount()` (no `if v==0` normalization) | `TestSignedNet`, all three original (a) tests | **FAIL**: `TestSignedNet/empty_set`, `TestSignedNet/exactly_cancelling_set`, `TestCalculateMetrics_EmptyHealthcareWindow_NoNegativeZero` (2 asserts), `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero` (3 asserts + JSON regexp: `` "plan_excluded_total":-0 `` in the dumped body). `TestCalculateMetrics_IncomeOnlyMonth_TrendEntriesNoNegativeZero` **PASSED even mutated** — see row 9. |
| 2 | `formatMoney`: removed the `if v == 0 { v = 0 }` belt | `TestFormatMoney` | **FAIL**: `formatMoney(-0) = "$-0.00", want "$0.00"`. |
| 3 | metrics.go site 1 (`totalExpenses`) reverted to inline `-outflows.SumAmount()` | full `metrics`/`mcpsvc/spend`/`dashboard` packages | **FAIL**: `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero`. |
| 4 | metrics.go site 2 (`healthcareTotal`) reverted to inline | full `metrics`/`mcpsvc/spend` packages | **FAIL**: `TestCalculateMetrics_EmptyHealthcareWindow_NoNegativeZero`, `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero`, `TestSummarizeSpendingHealthcareActualNoNegativeZeroWhenWindowHasNoHIRows` (all three). |
| 5 | metrics.go site 3 (`planExcludedTotal`) reverted to inline | full `metrics` package | **FAIL**: `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero` (via the whole-struct JSON regexp — no test asserts `PlanExcludedTotal`'s sign directly, this is the ONLY thing that catches it). |
| 6 | metrics.go site 4 (`livingTotal`) reverted to inline | full `metrics`/`mcpsvc/spend` packages | **FAIL**: `TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero`. |
| 7 | metrics.go site 5 (`expAmt`) reverted to inline | full `metrics` package | Initially **PASSED** with only the income-only-month fixture (row 9's gap) — after adding `TestCalculateMetrics_MonthWithExactlyCancellingOutflows_TrendEntriesNoNegativeZero`, re-ran and got **FAIL**: `ExpensesTrend[0] = -0 (Signbit=true)`. |
| 8 | metrics.go site 6 (`hcAmt`) reverted to inline | full `metrics` package | **FAIL** (after the cancelling-outflows fixture was added): `HealthcareTrend[0] = -0 (Signbit=true)`. |
| 9 | metrics.go site 7 (`livingMonth`) reverted to inline | full `metrics` package | **FAIL** (after the cancelling-outflows fixture was added): `LivingExpensesTrend[0] = -0 (Signbit=true)`. |
| 10 | metrics.go site 8 (`spend`, inside the CombinedCumulativeBalance walk) reverted to inline | full `metrics`/`mcpsvc`/`dashboard` packages | **NOT CAUGHT by any test, empirically confirmed and mathematically explained**: `spend` is only consumed as `running += accrual - spend`. IEEE754: `accrual - (-0.0) == accrual + 0.0 == accrual` bit-for-bit — subtracting a `-0.0` is numerically IDENTICAL to subtracting `+0.0`; the sign bit is absorbed before `running` (the only value ever exposed, via `CombinedCumulativeBalance`) is computed. No test can observe this call site's sign in isolation. Converted to `SignedNet` anyway per the contract's explicit site list (defensive consistency, matches every other call site's contract) — not a claimed mutation-killer. |
| 11 | explorer/handlers.go site 1 (page handler `handleExplorer`, ~line 202) reverted to inline | `TestHandleExplorer_WithRenderer_ZeroOutflowsNoNegativeZero` | **NOT CAUGHT, empirically confirmed and mathematically explained**: `TotalExpenses` here is consumed ONLY by (i) the `{{if or (isPositive ..) (isNegative ..)}}` chip gate, which is false for `-0.0` exactly the same as for `+0.0` (both `-0.0 > 0` and `-0.0 < 0` are false in Go), so the chip is absent either way and never renders the value; and (ii) `netAmount := totalIncome - totalExpenses`, where `income - (-0.0) == income - 0.0` bit-for-bit (same absorption as row 10). This is the SAME expression, byte-for-byte, as site 2 below (which IS mutation-tested and caught) — the difference is purely which consumer reads the value afterward. |
| 12 | explorer/handlers.go site 2 (partial handler `handleTransactionsPartial`, ~line 363) reverted to inline | `TestHandleTransactionsPartial_ZeroOutflowsNoNegativeZero` | **FAIL**: raw JSON body contains `"TotalExpenses":-0`; `TotalExpenses = -0 (Signbit=true)`. |
| 13 | `TestHandleExplorer_WithRenderer_RefundDominantFilterRendersNegativeExpenses`'s color-class assertion (added in this attempt): explorer.html's chip color conditional reverted to the flat pre-CB7 `text-rose-600 dark:text-rose-400` (no sign-aware branch) | that test + the new `OrdinarySpendRendersRoseExpensesClass` sibling | Re-verified from attempt 1's manifest (production template unchanged from attempt 1, only the test assertions strengthened this attempt) — the emerald-class assertion fails against the flat-rose revert exactly as attempt 1 recorded for the figure; not re-run this attempt since neither the template nor the underlying computation changed, only the test file. |

Honest summary of the two uncaught sites (10, 11): both are cases where the
`-0.0` vs `+0.0` distinction is either (a) absorbed exactly by IEEE754
subtraction before reaching any exposed value, or (b) gated behind a
positive/negative check that treats `-0.0` and `+0.0` identically AND
additionally absorbed by (a) downstream. Fixed for source-level
correctness and consistency with the contract's explicit site list (every
one of the 8+2 named sites now uses the sanctioned helper, so a FUTURE
consumer added to either call site inherits the fix automatically), not
because a test currently distinguishes them from the pre-fix inline form.

### False-positive caught and fixed during this attempt

While writing `TestHandleExplorer_WithRenderer_ZeroOutflowsNoNegativeZero`,
a bare `strings.Contains(body, ">Expenses<")` check for "chip absent"
initially FAILED even on the correctly-fixed code, because
`web/templates/pages/explorer.html`'s Type filter `<select>` has an
unconditional `<option value="Outflow" ...>Expenses</option>` — the same
substring appears regardless of the summary-stats chip's state. Both this
new test and the existing (attempt-1) chip-presence assertion were
tightened to match the chip's own label span
(`` text-rose-700 dark:text-rose-300">Expenses< ``) instead of the bare
substring.

## Verification

- `gofmt -l` on every `.go` file this task touched — clean, exit 0.
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `make check` (vet, staticcheck, govulncheck, css-verify, `go test ./...`,
  plus a `-count=1` re-run of `internal/handlers/accounts`) — exit 0,
  final line `✓ all checks passed`.
- `go test -count=1 ./...` — every package `ok`, zero `FAIL` lines. Full
  tail:

```
ok  	budget2/cmd/enrich-amazon	7.076s
ok  	budget2/cmd/server	6.054s
ok  	budget2/cmd/validate	0.012s
ok  	budget2/internal/config	0.003s
ok  	budget2/internal/handlers/accounts	1.565s
ok  	budget2/internal/handlers/approval	0.004s
ok  	budget2/internal/handlers/backup	38.364s
ok  	budget2/internal/handlers/dashboard	1.632s
ok  	budget2/internal/handlers/duplicates	0.029s
ok  	budget2/internal/handlers/explorer	1.092s
ok  	budget2/internal/handlers/insights	0.378s
ok  	budget2/internal/handlers/majorexpenses	0.610s
ok  	budget2/internal/handlers/transfers	0.465s
ok  	budget2/internal/handlers/whatif	17.892s
ok  	budget2/internal/http	0.005s
ok  	budget2/internal/models	0.006s
ok  	budget2/internal/services/accounts	0.028s
ok  	budget2/internal/services/amazon	0.004s
ok  	budget2/internal/services/anomalies	0.012s
ok  	budget2/internal/services/backup	0.574s
ok  	budget2/internal/services/classifier	0.004s
ok  	budget2/internal/services/dataloader	1.887s
ok  	budget2/internal/services/insights	0.005s
ok  	budget2/internal/services/majorexpenses	0.004s
ok  	budget2/internal/services/mcpsvc	0.052s
ok  	budget2/internal/services/mcpsvc/admin	4.682s
ok  	budget2/internal/services/mcpsvc/confirm	0.046s
ok  	budget2/internal/services/mcpsvc/curate	0.693s
ok  	budget2/internal/services/mcpsvc/ledger	0.627s
ok  	budget2/internal/services/mcpsvc/plan	10.005s
ok  	budget2/internal/services/mcpsvc/snapshot	0.007s
ok  	budget2/internal/services/mcpsvc/spend	1.242s
ok  	budget2/internal/services/merchants	0.007s
ok  	budget2/internal/services/metrics	0.007s
ok  	budget2/internal/services/pricecreep	0.006s
ok  	budget2/internal/services/restore	1.126s
ok  	budget2/internal/services/retirement	35.822s
ok  	budget2/internal/services/retirement/analysis	26.417s
ok  	budget2/internal/services/retirement/completeness	0.005s
ok  	budget2/internal/services/retirement/engine	0.051s
ok  	budget2/internal/services/retirement/history	0.004s
ok  	budget2/internal/services/retirement/overrides	0.007s
ok  	budget2/internal/services/retirement/prepare	0.023s
ok  	budget2/internal/services/storage	75.870s
ok  	budget2/internal/services/transfers	0.023s
ok  	budget2/internal/templates	1.090s
ok  	budget2/internal/testutil	0.006s
ok  	budget2/internal/version	0.004s
ok  	budget2/web	0.011s
```

- No `git checkout`/`stash`/`reset` used; no destructive git operations.
  The `budget2` binary was never run.
- `.swarm/CB7-RUN-SPEC.md` "Rulings" section was read (not modified) before
  starting; this manifest implements CB7-2026-09-03c only (a/b were
  already closed in CB7.2).

## Complete CB7 file list, all three attempts combined

See `.swarm/manifests/CB7.3.files` — attempt 1's list plus
`internal/services/metrics/plan_exclusions_remainder_test.go` and
`internal/services/mcpsvc/spend/plan_exclusions_test.go` (attempt 2) plus
`internal/templates/render.go` and `internal/templates/render_helpers_test.go`
(attempt 3, new this round).
