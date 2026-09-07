## Task 4: DI4 — Insights investigation

Files: handlers/insights and view-model helpers/tests; pages/insights.html;
insights components and web/static/js/insights.js; render tests.
Scope Tier2 tests,a11y,second. Consumes DI1 groups and DI2 explicit comparisons.

Lead reporting preflight: overall What changed uses signed all-outflow totals
for the two DI2 periods, rounded through the shared reporting precision, then
the existing ChangeDisplay producer for their difference. Contributors reuse
the existing category/Major Expense trend producer and its Change fields;
label that grouping/scope, including unmatched exclusions when applicable.
The top three are not a complete reconciliation of all spending. Do not invent
a second threshold, rounding algorithm, or browser monetary arithmetic.

Recurring display ruling: retain the existing raw detector and compatibility
data. Row money uses the shared reporting precision (same as MCP row amounts).
If displaying group/grand estimates, sum the displayed row estimates through
that same precision, and state once that totals sum displayed estimates and
monthly/annual figures are rounded separately. Do not claim monthly rounded
amount times 12 must equal independently rounded annual amount. This is an
estimate presentation rule, not DI3's raw-aggregate actual-period total rule.
No stored data, detector algorithm or MCP schema change is needed for it.

- [ ] Test rendered comparison bounds, largest three contributors (absolute
  dollar difference, stable category tie-break), no-history and no-findings.
- [ ] Order: period context; What changed; Review these transactions; three
  recurring groups; supporting charts/tables. Replace repeated summary tiles.
- [ ] Findings preview <=5, hash+type dedup, full count and usable all-results
  destination. Reuse existing anomaly and price-creep evidence; no invented
  findings, savings promises, or confidence. Preserve category+date drilldowns.
  Label the post-dedup count as findings, not distinct transactions: one hash
  can have two different finding types. All-results access must reveal actual
  remaining results (not a dead anchor or a link that drops the selected dates).
  Preserve existing detector semantics: anomaly/price-creep evidence is based
  on loaded active history, not a new algorithm. Say so. The combined selected-
  period preview includes only evidence tied to a real transaction within that
  period; join price-creep groups to an actual representative/latest transaction
  with a stable hash, never invent one.
  Anchor price creep to the latest eligible transaction in its FULL-history
  detector group (same date/hash ordering), then filter that anchor by the
  selected period. Do not move its anchor backward merely to fit a historical
  selection; full-history supporting evidence remains separately accessible.
  Its recent median is not necessarily the actual anchor transaction amount;
  label each distinctly if both are shown. Build the full findings list before
  preview caps: the existing ten-row price-creep presentation cap must not
  silently truncate the new all-results list or its count. Existing whole-
  history supporting tables may retain their clearly labeled separate scope.
- [ ] Merchant names first, Major Expense labels secondary; estimated annual
  and monthly values explicitly distinct from actual spending. Historical
  expected payments anchored to reference, not presented as overdue today.
- [ ] Scope recurring row totals consistently; all retained rows accessible.
  Verify HTMX refresh replaces context and findings as well as chart/table.
- [ ] Run renderer/handler/service tests and responsive/theme probes; write
  report/manifest, named review and gate.
