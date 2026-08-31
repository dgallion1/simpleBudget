# KD run — living/healthcare KPI detail kinds (dashboard drill-down mismatch)

Date: 2026-08-30. Lead: agents2 worktree duplicate-transaction-detection-89ef29
(same session as the DP run). User approval: "yes, build it" after the lead's
diagnosis of the Monthly Living Expenses card opening "Total Expenses Details".

## Problem

`/dashboard/kpi/{kpiType}` supports only income/expenses/savings/savings-rate.
The Monthly Living Expenses card (kpis.html:57) and Monthly Healthcare card
(kpis.html:101) are both wired `onclick="openKPIDetail('expenses')"`, so both
open the Total Expenses modal — different classification, different divisor,
numbers that look unrelated to the card. The month drill-down route
(`/dashboard/kpi/{kpiType}/month/{month}`) has the same gap one level deeper.

## Task KD1 — `living` and `healthcare` KPI detail kinds

Tier: **2** (no critical glob). Checks: tests,a11y,second.

Files (ONLY these, plus manifests):
- internal/handlers/dashboard/handlers.go
- internal/handlers/dashboard/handlers_http_test.go (tests)
- web/templates/components/kpis.html
- web/templates/components/kpi-detail.html
- web/templates/components/kpi-month-detail.html (only if its rendering needs
  a type-aware label; behavior for existing kinds byte-identical)

### Design (fixed)

1. `kpiTitles` gains `"living": "Monthly Living Expenses"` and
   `"healthcare": "Monthly Healthcare"` (titles render as "<Title> Details").
2. **Single-source classification** (split-classification rule; ruling
   2026-08-29a): the classified sets come ONLY from the existing metrics
   helpers — `metrics.LivingOutflows(outflows, planExclusions)` for living,
   `outflows.FilterByCategory(metrics.HealthInsuranceCategory)` for
   healthcare. No new category string literals, no re-implemented exclusion
   logic in the handler.
3. `handleKPIDetail` for the two new kinds: month rows are the classified
   set's per-month figures (signed sum per month, then Abs — the same
   refund-netting shape the existing code uses). Summary tiles: Total
   (classified range total), the single Per Month figure (item 4), Low,
   High. The vs-Avg row column for these kinds compares each month against
   the SAME single per-month figure the modal displays (item 4) — no hidden
   second average anywhere (lead-accepted worker refinement, KD-2026-08-30b,
   truer to KD-2026-08-30a than the original wording, which had kept the
   rows'-average mechanics invisibly).
4. **Exactly ONE per-month figure, the card's own** (user decision
   2026-08-30, mid-run: "not a good idea to keep two kinds of totals"):
   the modal's Per Month stat is `metrics.Calculate(...).ActualMonthly`
   (living) / `.HealthcareActual` (healthcare) — the fractional
   `MonthsBetween` / `ClippedHealthcareMonths` divisors. The
   Total÷calendar-months average is NOT shown anywhere in the modal for
   these kinds, so modal and card can never disagree. To guarantee the
   inputs match, extract handleDashboard's pre-Calculate input assembly
   (budget target, healthcare target, coverageStart, hasCoverage,
   planExclusions) into ONE shared unexported helper used by both
   handleDashboard and handleKPIDetail — extraction, not duplication, so the
   two surfaces cannot drift. handleDashboard's own behavior is unchanged.
5. `handleKPIMonthDetail` for the two new kinds: the transaction list is the
   classified set restricted to the month (`transactionsInMonth` over the
   classified set), total = Abs(signed sum) of exactly those rows,
   totalLabel "Living Spent" / "Healthcare Spent". The month figure must
   equal the parent modal's row figure exactly (same summing shape — see the
   existing comment at handlers.go:607).
6. kpis.html: living card onclick → `openKPIDetail('living')`; healthcare
   card onclick → `openKPIDetail('healthcare')`. No JS changes
   (dashboard.js passes the kind through generically).
7. kpi-detail.html: for the new kinds the row cell and column header must
   render the classified value (header label "Living Expenses" /
   "Healthcare"), NOT the `.Expenses` whole-month figure the expenses branch
   prints. Existing kinds' markup byte-identical.

### Acceptance criteria

K1. HTTP test: GET /dashboard/kpi/living renders title "Monthly Living
    Expenses Details"; /dashboard/kpi/healthcare renders "Monthly Healthcare
    Details"; both cards' onclick attributes updated.
K2. Fixture with a Health Insurance row, a plan-excluded (exclude-from-sync)
    row, an ordinary living row, AND a refund (positive outflow): living
    month rows exclude the first two, net the refund, sum to
    `Metrics.LivingExpensesTotal`, and the header Per Month figure equals
    `Metrics.ActualMonthly` for the same range (same value through
    formatMoney — assert on rendered string).
K3. Healthcare kind on a fixture with coverageStart inside the range: rows
    are Health-Insurance-only; header Per Month equals
    `Metrics.HealthcareActual` (coverage-clipped divisor), rendered-string
    asserted.
K4. Month drill-down for living lists ONLY living-classified transactions
    (no Health Insurance row, no plan-excluded row) and its total equals the
    parent row figure exactly.
K5. Existing kinds unchanged: current handlers_http_test.go suite green with
    no test edits except additions; income/expenses/savings/savings-rate
    responses byte-identical for a fixed fixture (assert at least one).
K6. Grep gate: handlers.go contains no literal "Health Insurance" outside a
    reference to metrics.HealthInsuranceCategory, and no duplicated
    exclusion logic (checker enumerates surfaces).
K7. checker-a11y: modal + card markup changes meet ACCESSIBILITY baseline
    (headings intact, no new color-only signaling, focus/Escape behavior of
    the modal unchanged).
K8 (added attempt 2, checker-tests F2): the modal's Export CSV button works
    for the new kinds — handleKPIExport gains living/healthcare cases built
    from the SAME classified month data the modal renders (single source),
    with an HTTP test asserting a non-empty CSV whose rows match the modal's
    month figures. Attempt 1 shipped a zero-byte download.
K9 (added attempt 2, checker-tests F4): the two Per-Month tests assert
    strict formatMoney rendered-string equality, not ±0.01 slack.

Backlog (not KD1): the Budget card (kpis.html:141) still opens the generic
expenses modal — same original defect, third card; needs a design decision
on what a combined Living+Healthcare drill-down should show. Pre-existing
a11y observations from checker-a11y attempt 1 (icon-only close button name,
clickable-div cards, th scope) — also backlog.

## Territories

Same shared checkout as the DP run. Currently clean besides untracked
budget2.old-1345 (leave it). The dashboard-owning session from earlier today
has committed and pushed (b48b61e); if NEW foreign uncommitted edits appear,
freeze and re-fence before checkers copy the tree. A separate spawned session
is working on near_duplicates thresholds in its own worktree — dataloader
paths are OUT of KD territory.

## Rulings

- KD-2026-08-30a (user, mid-run): the living/healthcare modal shows exactly
  ONE per-month figure — the card's own rate from metrics.Calculate. The
  calendar-month average stat is dropped for these kinds. Spec §Design 3–4
  amended before worker completion; K2/K3 assert the single figure.
- KD-2026-08-30b (lead, post-worker): worker refinement accepted — the
  vs-Avg column for the new kinds compares against the displayed per-month
  figure itself, not a hidden rows' average. §Design 3 amended to match.
- KD-2026-08-31a (user, after hard stop): three attempts failed; attempts
  1–2 on substance, attempt 3 solely on one untrimmed template action
  (kpi-detail.html line 93) leaving one inert whitespace line in the four
  pre-existing kinds' HTML. User authorized a SCOPED attempt 4: add the
  missing trim marker, plus one cheap whitespace-tripwire test (pin the
  rendered whitespace-only-line count of the expenses modal to master's
  own baseline), and NOTHING else. Acceptance at attempt 4 = oracle PASS +
  checker-tests byte-identity twin-dump PASS + checker-second PASS;
  checker-a11y is not re-run (attempt-4 diff is whitespace-only; attempt-3
  a11y PASS stands for the rendered pixels).
- KD-2026-08-30d (lead, attempt-3 contract rewrite after two same-class
  fails — figures not reconciling across surfaces; T18 precedent): for the
  living/healthcare kinds, a month row's value is the NEGATED SIGNED sum of
  the classified rows in that month — positive = net spend, negative = net
  refund, NO per-month Abs (matching the MCP by_month convention). The
  Total tile is the SUM OF THE DISPLAYED month values — one rounding path,
  so rows always reconcile with the tile exactly, and Total equals
  Metrics.LivingExpensesTotal whenever the range nets spend (the only
  divergence is a whole-range net refund, where Total honestly renders
  negative while the card's figure is an Abs — documented, accepted).
  Low/High run over the signed values. vs-Avg compares the signed value
  against the displayed per-month rate; the no-coverage "—" state is
  unchanged. CSV export uses the same shared month function (already does).
  Tests: (a) refund-dominant multi-month fixture — rendered rows sum to the
  rendered Total; (b) net-spend fixture — Total's rendered string equals
  formatMoney(LivingExpensesTotal). Plus: one test for the healthcare month
  drill-down (checker-second's coverage-gap observation), template
  {{- -}} trim markers so pre-existing kinds' rendered HTML is byte-
  identical to master (K5, attempt-2 regression was inert whitespace), and
  the a11y fix: dark:text-gray-400 → dark:text-gray-300 on the "—" cells.
- KD-2026-08-30c (lead CONCEDES checker-second FAIL, attempt 1): when the
  healthcare kind's coverage-clipped divisor is zero for the selected range
  (hasCoverage false, or coverageStart outside it) the modal must NOT
  render "Per Month: $0.00" beside non-zero classified rows. New criterion
  K3b: in that state the Per Month tile renders "—" with the text "no
  coverage in this range" (real text, not color-only), and the vs-Avg
  column renders "—" for every row (no comparison basis exists). A
  genuinely zero rate WITH zero rows may still render $0.00. Living is
  unaffected (MonthsBetween is never zero). Covered by a new HTTP test
  reproducing the checker's fixture (refund-only Health Insurance row,
  range with no coverage overlap).
