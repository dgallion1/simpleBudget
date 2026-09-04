# CB7.1 manifest (attempt 1)

## Scope

Fixed the five sibling `math.Abs(range/filter-level SumAmount())` sites (CB7
spec, sites 1-5) to the signed-negated-net convention (`-set.SumAmount()`,
metrics.go:526 idiom), so a range/filter/window whose outflow-typed rows net
POSITIVE (refunds exceed spending) reports NEGATIVE expenses instead of a
math.Abs'd positive figure. Updated every downstream consumer named in the
spec (kpis.html, explorer.html, three MCP wording sites, dashboard.go's
invariant doc, .swarm/NEXT.md's backlog bullet) and added committed,
mutation-proven tests at every layer.

## Files changed

- `internal/services/metrics/metrics.go` — the three range-level sites:
  `totalExpenses := -outflows.SumAmount()` (was `math.Abs(...)`, line
  ~378), `healthcareTotal := -healthcareOutflows.SumAmount()` (was
  `math.Abs(...)`, line ~406), `livingTotal := -livingOutflows.SumAmount()`
  (was `math.Abs(...)`, line ~436). Rewrote the stale doc comments that
  described the OLD math.Abs range-level behavior: `LivingOutflows`' doc
  (previously said "the range total still runs math.Abs(...)" and used
  `math.Abs(LivingOutflows(...).SumAmount())` as the nil-safety byte-for-byte
  example — now says both range and per-month use the same signed
  convention), the SY-2026-08-30d ruling comment ("ordinary |sum|
  arithmetic" → "signed negated-net arithmetic (CB7)", two spots), and the
  CombinedCumulativeBalance walk's inline comment (removed the "does not
  touch range-level totalExpenses, still math.Abs... only while the RANGE
  as a whole nets outflow-negative" claim — now correctly states the
  partition invariant holds for every range).
- `internal/services/metrics/metrics_test.go` — two new committed tests:
  `TestCalculateMetrics_RefundDominantRange_SignedTotalsAndCombinedInvariantHolds`
  (two-month fixture: Jan ordinary Rent+Health-Insurance-premium spend, Feb
  a non-HI refund exceeding the WHOLE RANGE's ordinary spend AND an HI
  premium refund exceeding Jan's own premium — so living, healthcare, AND
  the combined range total each independently flip refund-dominant; asserts
  TotalExpenses/LivingExpensesTotal/HealthcareTotal exact signed values,
  NetSavings==TotalIncome-TotalExpenses and > TotalIncome, every
  living/healthcare-derived delta exact, and the
  CombinedCumulativeBalance-last-element == -CombinedCumulativeDelta
  invariant now holding in this previously-out-of-scope case) and
  `TestCalculateComparison_ExpensesChangeTracksNegativeComparisonPeriod`
  (previous period nets a single refund → Previous.TotalExpenses=-3000;
  current period ordinary spend → Current.TotalExpenses=1000; asserts
  ExpensesChange matches the `|previous|`-denominator formula exactly and is
  POSITIVE, i.e. tracks the genuine worsening).
- `internal/handlers/explorer/handlers.go` — both `totalExpenses :=
  math.Abs(filtered.FilterByType(models.Outflow).SumAmount())` sites (page
  handler line ~191, partial/HTMX handler line ~341) changed to
  `-filtered.FilterByType(models.Outflow).SumAmount()`, each with a CB7
  comment; removed the now-unused `"math"` import (verified: those were the
  only two `math.` usages in the file).
- `internal/handlers/explorer/handlers_test.go` — two new committed tests:
  `TestHandleTransactionsPartial_RefundDominantFilterNegatesTotalExpenses`
  (JSON path, site 341: Outflow set nets -500+900=+400 → TotalExpenses=-400,
  NetAmount=2000-(-400)=2400) and
  `TestHandleExplorer_WithRenderer_RefundDominantFilterRendersNegativeExpenses`
  (real-renderer path, site 191: asserts the rendered body contains
  "-$400.00" and "$2,400.00" and that the Expenses chip label itself renders
  — proving both the handler's signed total AND the explorer.html
  isPositive→isPositive-or-isNegative template fix).
- `internal/models/dashboard.go` — rewrote the `CombinedCumulativeBalance`
  field doc's invariant section: removed the "AND the further precondition
  that the RANGE as a whole still nets outflow-negative... a wholly
  refund-dominant RANGE... is out of scope" clause: CB7 makes TotalExpenses
  itself the signed negated net of the whole range, so the invariant now
  holds unconditionally (both sides of the partition share one sign
  convention).
- `internal/services/mcpsvc/spend/summary.go` — `summarize_spending`'s tool
  `Description` string: replaced "by_month is never truncated, so summing it
  always matches total_expenses in MAGNITUDE, but total_expenses is always
  non-negative (it is an absolute value) while summing by_month can be
  negative... total_expenses and the sum of by_month are the negation of
  each other, not equal" with "...so summing it always EQUALS total_expenses
  exactly: total_expenses is SIGNED, not an absolute value -- positive means
  net spend, and it goes NEGATIVE when this window's refunds exceed its
  spending OVERALL..., same as any by_month row would." No production-code
  arithmetic change here — `TotalExpenses: round2(m.TotalExpenses)` already
  reads the (now-signed) metrics.Calculate output directly.
- `internal/services/mcpsvc/spend/summary_test.go` — one new committed
  test: `TestSummarizeSpendingRefundDominantWindowNegatesTotalExpensesAndReconciles`
  (whole window nets -50+500=+450 → total_expenses=-450, negative;
  sum(by_month) recomputed from the tool's own JSON output and asserted to
  equal total_expenses EXACTLY, not just in magnitude).
- `internal/services/mcpsvc/server.go` — `serverInstructions`: replaced
  "summarize_spending (total_expenses is always non-negative, but its
  by_category/by_merchant/by_month breakdown rows are normally positive and
  can go NEGATIVE when refunds outweigh spending in that row)" with
  "summarize_spending (total_expenses is SIGNED -- positive is net spend,
  and it goes NEGATIVE when this window's refunds exceed its spending
  overall; its by_category/by_merchant/by_month breakdown rows are normally
  positive and can go NEGATIVE the same way when refunds outweigh spending
  in that row)".
- `internal/services/mcpsvc/server_test.go` — updated the
  `TestServerInstructionsCarryLoadBearingClaims` pin from
  `"summarize_spending (total_expenses is always non-negative"` to
  `"summarize_spending (total_expenses is SIGNED"` to match the new wording
  (the test's own doc says "not a wording freeze: reword freely" as long as
  the pin is updated alongside the behavior it claims).
- `internal/handlers/dashboard/verdict_render_test.go` — one new committed
  rendered-string test:
  `TestKPIsTotalExpensesTile_RefundDominantRangeRendersSignedNegative`
  (renders "kpis" via the real renderer with `TotalExpenses: -1234.56`;
  asserts the output contains formatMoney's own negative-value string
  "-$1,234.56" pinned as-is, NOT the positive "-Abs'd" `>$1,234.56<`, and
  that the new sign-aware `text-emerald-800 dark:text-emerald-300` class is
  present).
- `web/templates/components/kpis.html` — Total Expenses tile (line ~36):
  `{{formatMoney (abs .Metrics.TotalExpenses)}}` → conditional class + no
  `abs`: `{{if gt .Metrics.TotalExpenses 0.0}}text-rose-600
  dark:text-rose-400{{else}}text-emerald-800
  dark:text-emerald-300{{end}}">{{formatMoney .Metrics.TotalExpenses}}`.
  Card tint/border (`bg-rose-50`/`border-rose-200`, light+dark) untouched
  per spec.
- `web/templates/pages/explorer.html` — `summary-stats` Expenses chip (line
  ~755): gate changed from `{{if isPositive .TotalExpenses}}` to `{{if or
  (isPositive .TotalExpenses) (isNegative .TotalExpenses)}}` (renders
  whenever nonzero, using only the two existing funcmap helpers named in the
  spec — no new helper); value class changed from the flat
  `text-rose-600 dark:text-rose-400` to the same sign-aware conditional as
  kpis.html (`gt 0.0` → rose, else → the new emerald-800/emerald-300 pair).
  Chip ground (`bg-rose-50`/`bg-rose-900/20`) untouched.
- `.swarm/NEXT.md` — appended "RESOLVED by CB7 2026-09-03: ..." to the SY-run
  backlog bullet ("CombinedCumulativeBalance walk assumes per-month |sum|
  partitions the range-level |sum|"), per instruction, without deleting the
  original text.

## Contrast decision (kpis.html + explorer.html sign-aware color)

Measured WCAG contrast (relative-luminance formula) for the new
negative-value (net-credit) text color against BOTH card grounds:

- Light: `bg-rose-50` = `#fff1f2`.
- Dark: `bg-rose-900/20` composited over the page's `dark:bg-gray-900`
  (`#111827`, from `web/templates/layouts/base.html:63`) = `rgb(41,23,42)`.

| Candidate | On `bg-rose-50` | On dark composited bg |
|---|---|---|
| emerald-600 | 3.43:1 (FAIL) | 4.46:1 |
| emerald-700 | 4.99:1 (pass) | 3.06:1 (FAIL) |
| **emerald-800** | **6.99:1 (pass)** | 2.19:1 (FAIL, wrong theme) |
| emerald-300 | 1.39:1 (FAIL, wrong theme) | **11.02:1 (pass)** |
| emerald-400 | 1.75:1 (FAIL, wrong theme) | 8.74:1 |

No single Tailwind shade clears 4.5:1 on both grounds simultaneously (a
light-mode-legible dark shade is necessarily too dark for the dark card, and
vice versa) — this is why the fix uses two THEME-SPECIFIC classes, exactly
like every other sign-aware pair already in these templates
(`text-rose-600 dark:text-rose-400`, `text-green-700 dark:text-green-400`
in kpi-month-detail.html): **`text-emerald-800`** for light mode (6.99:1 on
`bg-rose-50`) and **`text-emerald-300`** for dark mode (11.02:1 on the
composited dark card background) — both comfortably above 4.5:1 on their
respective ground. Computation script + full candidate table run via
`python3` during this task (relative-luminance / WCAG contrast formula,
Tailwind v3 hex values); not committed (throwaway, per calibration
discipline — the numbers above are the durable record).

## Mutation validation (every new test proven load-bearing)

For each test, the corresponding production `math.Abs(...)` mutant was
temporarily restored (via a throwaway `cp`+`python3` string-replace), the
test was re-run to confirm it FAILS, then the file was restored via `cp`
from the pre-mutation backup and `diff` was used to confirm a byte-identical
restore. All five sites are covered:

- **metrics.go site 1 (totalExpenses)**: mutant → `TestCalculateMetrics_RefundDominantRange...`
  failed with `TotalExpenses = 5300, want -5300`. Restored, re-passed.
- **metrics.go site 2 (healthcareTotal)**: mutant → same test failed with
  `HealthcareTotal = 300, want -300`. Restored, re-passed. (First fixture
  draft didn't make healthcare refund-dominant and this mutant slipped
  through silently — caught by re-checking each site individually per the
  spec's explicit instruction, then the fixture was redesigned to add a
  Feb health-insurance-category refund exceeding Jan's premium so all three
  sites are independently mutation-sensitive in ONE test.)
- **metrics.go site 3 (livingTotal)**: mutant → same test failed with
  `LivingExpensesTotal = 5000, want -5000`. Restored, re-passed.
- **metrics.go site 1 again, via the MCP surface**: mutant →
  `TestSummarizeSpendingRefundDominantWindowNegatesTotalExpensesAndReconciles`
  failed with `total_expenses = 450, want -450`. Restored, re-passed. (MCP's
  own totalExpenses is metrics.Calculate's output, not a separate call
  site — this proves the MCP-layer test is wired to the real fix, not just
  the response-shape.)
- **explorer/handlers.go site 191 (page handler)**: mutant (`absFloat`
  helper reintroducing `math.Abs`) →
  `TestHandleExplorer_WithRenderer_RefundDominantFilterRendersNegativeExpenses`
  failed (rendered body missing "-$400.00"/">Expenses<"). Restored (`cp`
  from pre-edit backup), `diff` confirmed byte-identical, re-passed.
- **explorer/handlers.go site 341 (partial handler)**: mutant → same
  approach; `TestHandleTransactionsPartial_RefundDominantFilterNegatesTotalExpenses`
  failed with `TotalExpenses = 400.00, want -400.00` and `NetAmount =
  1600.00, want 2400.00`. Restored, `diff` confirmed clean, re-passed.
- **kpis.html Expenses tile**: mutant (reverted the class expression + put
  `abs` back) →
  `TestKPIsTotalExpensesTile_RefundDominantRangeRendersSignedNegative`
  failed (rendered `$1,234.56` positive, not `-$1,234.56`). Restored via
  `cp`, `diff` confirmed byte-identical, re-passed.

Full command sequence used for each mutation round (repeated per site):
`cp <file> <scratch-backup>` → `python3 -c "...string replace..."` →
`go test ./<pkg>/... -run <TestName> -v` (observe FAIL) → `cp
<scratch-backup> <file>` → `diff <file> <scratch-backup>` (confirms
"restored clean").

## Needs lead ruling — pre-existing tests now failing

Per the task instruction, these were NOT silently re-pinned. All three are
the SAME defect class: their fixtures deliberately construct a plan-sync
"remainder" (living outflows after excluding one flagged row) that nets
POSITIVE (a refund-dominant remainder), and pin the OLD math.Abs contract
on the RANGE-level `LivingExpensesTotal`/`living_monthly_actual` figure
CB7 changes. `internal/services/metrics/plan_exclusions_remainder_test.go`
even has an explicit comment block ("NOTE on CombinedCumulativeBalance
(UPDATED, CB2)") stating in writing that range-level totals "still use
math.Abs... out of scope" — the exact precondition CB7's task removes.

1. `internal/services/metrics/plan_exclusions_remainder_test.go`:
   `TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder`
   — fixture: remainder = -1000 (grocery) + 4000 (outflow-typed credit) =
   +3000 (refund-dominant), flagged = one ordinary -500 car payment.
   **Old value asserted: `LivingExpensesTotal == 3000`** (`math.Abs(3000)`).
   **New value produced: `-3000`** (`-3000` is the correct signed negated
   net under CB7's contract — this range's living remainder is a $3000 net
   credit, not $3000 of spend).
2. Same file, `TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows`
   — same fixture shape applied to both Comparison windows (current +
   previous, each Jan/Feb structured identically). **Old value: both
   `Current.LivingExpensesTotal` and `Previous.LivingExpensesTotal` ==
   `3000`. New value: both `-3000`.**
3. `internal/services/mcpsvc/spend/plan_exclusions_test.go`:
   `TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder`
   — the MCP mirror of test #1's exact fixture, through the
   `summarize_spending` tool's `budget.living_monthly_actual` field.
   **Old value: `~2945.56` (== `3000/monthsInRange`, positive). New value:
   `-2945.56`** (same magnitude, correct sign now).

All three fail with the SAME shape: `got == -want` in every case (the sign
flipped, magnitude unchanged) — confirming this is exactly CB7's intended
behavior change reaching a test that pinned the old one, not a new
arithmetic bug. Recommend: update these three assertions' expected values
to the negative signed figures (and adjust each test's surrounding prose,
which currently argues FOR the old math.Abs contract) in a follow-up
task/commit, since editing them was out of this task's stated scope
("do NOT re-pin it silently").

## Verification

- `gofmt -l ./internal` — printed 21 pre-existing files, NONE of which this
  task touched (verified against `git status --short`; the untouched
  flagged files are unrelated drift, e.g.
  `internal/services/retirement/engine/expense.go`,
  `internal/models/user_profile.go`). Every file this task changed is
  gofmt-clean.
- `go build ./...` — clean, no output.
- `go vet ./...` — clean, no output.
- `go test ./...` — full run: every package `ok` EXCEPT the 4 pre-existing
  tests listed above under "Needs lead ruling" (in packages
  `budget2/internal/handlers/explorer` and `budget2/internal/services/mcpsvc/spend`
  and `budget2/internal/services/metrics`); no other regressions anywhere
  in the module. The concurrently-edited `insights` package (CB8's
  territory, untouched by this task) compiled and passed cleanly both times
  it was run — no transient compile failure was observed, so no re-run was
  needed.
- Targeted re-runs of every new CB7 test, individually and together, all
  `PASS` (commands shown inline above in the mutation-validation section).

## Files NOT touched (confirmed, per spec's "not affected" list)

- `engine.TotalExpenses`/`month.TotalExpenses` in
  `internal/services/retirement/**` and
  `internal/handlers/whatif/spending_trajectory.go:84` — untouched.
- `PlanExcludedTotal` (already signed) and per-month trend series (already
  signed, CB2) — untouched.
- `internal/services/insights/trends.go` /
  `internal/services/insights/trends_test.go` — CB8's concurrent territory;
  never opened for edit by this task (confirmed via `git status`: they show
  as modified, but not by any tool call in this session).

## MCP wording sweep (acceptance criterion d)

`grep -rn "total_expenses.*non-negative\|non-negative.*total_expenses\|total_expenses.*absolute value\|absolute value.*total_expenses"` across
`*.go`/`*.md` in the repo (excluding `.git/` and this run's own
`.swarm/CB7-RUN-SPEC.md`) now returns exactly one hit: the corrected
sentence in `summary.go` itself, which explicitly states total_expenses is
"SIGNED, not an absolute value." `~/.claude/skills/budget2-mcp/` has zero
occurrences of `total_expenses`/`non-negative`/`absolute value` at all (no
skill-dir edit needed, nothing to report there beyond this negative
result). `GLOSSARY.md` has no `total_expenses`/`non-negative` mentions
either.
