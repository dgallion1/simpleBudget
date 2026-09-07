# Dashboard and Insights Implementation Plan

> For agentic workers: use superpowers:subagent-driven-development and the
> repository's stronger named-checker/gate contract, task by task.

**Goal:** Turn the existing two pages into an accurate household overview and
an evidence-led investigation workflow.
**Architecture:** Shared Go service calculations, additive model fields, HTMX
partials and existing CSS tokens. Classification, periods and monetary values
have one producer and are consumed by templates, charts and MCP adapters.
**Tech Stack:** Go, html/template, HTMX, Tailwind, Plotly.
**Spec:** ../specs/2026-09-06-dashboard-insights-design.md (user approved).

## Global Constraints

- Seven calendar days for stale-data notice; current forecast needs at least
  seven elapsed days, selected current-month coverage and recent current data.
- Never turn a cash-flow difference into a measured investment withdrawal.
- Preserve signed refunds, transfer exclusion, ChangeCell and negative-zero
  protections. No JS arithmetic over formatted money strings.
- Retain semantic tokens, keyboard use, both themes and 390/768/1440px layouts.
- No storage, retirement engine, account service, transfer service or migration
  changes. No real-data writes. All probes use dereferenced synthetic copies.
- Gate state lives in .swarm/DI-run; prefix DI; current task attempts start at 1.
  Worker writes manifest/report, checker writes verdict; lead alone updates state.
- No worker git commits or branch operations. Lead commits reviewed work after
  required tests; preserves run evidence. This overrides the skill's commit flow.
- User hard stop (three fails any tier, two at Tier3) overrides skill fix caps.

## Preflight and task dependencies

| Task(s) | Interface/overlap | Resolution |
|---|---|---|
| DI1 / DI2 | models/insights.go and insights handlers | Serialize, additive fields |
| DI2 / DI3 | Dashboard period producer | DI2 exposes period context; DI3 consumes |
| DI2 / DI4 | Insights comparison and forecast producer | DI2 owns calculation; DI4 renders |
| DI3 / DI4 | CSS build and shared UI partials | Serialize to avoid overlapping CSS output |
| DI1 | Subscription positive evidence vs retail exclusions | Other recurring is retained, never guessed as subscription |
| DI2 | Today vs selected historical range | Inject clock and separate reference period from wall-clock |
| DI3 | Four summary tiles vs retained details | Move detail below, no information deletion |
| DI4 | Category vs merchant | Merchant identity first, category annotation second |
| DI5 | Broad review vs scope | Integration covers DI1–4, findings attributable to this run |

## Task 1: DI1 — Recurring payment semantics

Files: internal/services/insights/recurring.go and focused classification file;
internal/models/insights.go; internal/handlers/insights/handlers.go;
internal/services/mcpsvc/spend/recurring.go; corresponding *_test.go files.
Allow minimal template compatibility so no unknown recurring data vanishes
while DI4's final layout is pending. Scope Tier2 tests,second.

Consumes RecurringPayment and its original Transactions (not category labels).
Produces shared classification subscription/bill/other plus reason; preserve
IsSubscription(rp) bool wrapper derived from classifier. Expose classification
and reason in MCP rows; additive InsightsData group for other recurring.

- [ ] Add failing fixtures: Netflix/Spotify/Cloud Storage true; auto loan,
  travel, fuel, pets, ATM, dining, home maintenance false; renamed Major Expense
  cannot change category. Unknown regular merchant stays other. Retail with
  monthly cadence must not become subscription. Test UI/tool grouping parity.
- [ ] Implement positive-evidence classifier and use it in every caller; grep
  and Go semantic references enumerate impact before modifying symbols.
- [ ] Keep grouped merchant identity, preserve annual/monthly estimate semantics,
  remove detector cap or expose truthful scope before summing (prefer uncapped
  service results and presentation caps). Regression fixture >20 series must
  not silently omit a subscription or understate a labeled total.
- [ ] Run go test ./internal/services/insights ./internal/handlers/insights
  ./internal/services/mcpsvc/spend ./internal/templates and go build ./....
- [ ] Write .swarm/DI-run/manifests/DI1.1.files and reports/DI1.1.md with exact
  commands, consumer enumeration and red/green evidence. Named review, gate.

## Task 2: DI2 — Shared period, comparison and forecast context

Files: new internal/services/insights/period.go and tests; models/insights.go;
services/insights/trends.go; handlers/insights and handlers/dashboard adapters;
MCP spend/trends.go; shared period-context partial and minimal forecast bindings.
Scope Tier2 tests,second. Classifier from DI1 unchanged.

Consumes selected inclusive start/end, complete active TransactionSet and
explicit now. Produces one exported period context for both handlers: selected
and prior bounds, latest transaction, stale flag, history availability, reference
month/date, forecast availability/reason and amount. Choose idiomatic names,
document exact resulting signatures in report for DI3/DI4. Existing
SpendingVelocity callers remain compatible; UI uses explicit-period entry point.

- [ ] Red fixtures: today Sept6 with latest Aug28 has no Sept forecast; completed
  Aug compares July; Sept1–6 compares Aug1–6; arbitrary 10-day period compares
  prior 10 inclusive days; leap February; zero data; unknown prior history.
- [ ] Calendar-day logic uses local dates and injected now (DST-safe). History
  bounds are not completeness proof; no-history UI says unavailable, not 0%.
- [ ] Forecast only when selection includes month start through reference,
  latest data is in current month and <=7 days old, with >=7 elapsed days.
  Project signed current-month net / elapsed calendar days * days in month.
  Publish unavailable reason instead of a fabricated zero forecast.
- [ ] Prior comparison excludes current period, uses ChangeCell for deltas and
  existing SignedNet. Add explicit-period major-expense comparison producer if
  needed so DI4 does not keep the old last-month-only comparison path.
- [ ] Rewire full and HTMX responses and tool adapters to consistent semantics;
  preserve MCP omission of unsupported current-month forecast if appropriate.
- [ ] Run relevant services/handlers/MCP/render tests. Report interfaces, changed
  consumers, tests and manifest, then named review and gate.

## Task 3: DI3 — Dashboard overview

Files: handlers/dashboard, models/dashboard.go if needed; pages/dashboard.html;
components/kpis.html and supporting components; web/static/js/dashboard.js;
focused dashboard/render tests. Scope Tier2 tests,a11y,second.
Consumes DI2 period context and existing metrics/target provenance.

Reporting-adapter preflight: include summarize_spending's cash-flow output and
its shared round2 helper when enforcing the approved one-rounding-path rule.
Exact warning/fixtures are in .swarm/DI-run/briefs/task-3-brief.md. No stored values or engine edits;
enumerate headline, verdict, modal and MCP consumers before implementation.

- [ ] Test actual renderer for selected-period income/net spending/cash flow,
  signed refunds, fractional cents, absent plan/data and stale anchors.
- [ ] Four primary figures: recorded income, net spending, cash-flow balance,
  spending versus plan. Same period throughout; labeled monthly equivalents
  only in detail. Preserve healthcare and separately modeled costs/provenance.
- [ ] Negative cash-flow explanation is “Spending not covered by recorded
  income”; positive is “Recorded income above spending”. Explicitly note this
  does not measure portfolio withdrawals. Avoid unsupported sustainability claim.
- [ ] Retain account dates, two useful spending/budget charts and navigable
  details. Link to Insights with selected range and correct anchor/filter.
- [ ] Use existing tokens/components, responsive layout and accessible links.
  Regenerate Tailwind only if new classes require it. Test Go/render, review
  desktop/mobile/both themes, write manifest/report; named review and gate.

## Task 4: DI4 — Insights investigation

Files: handlers/insights and view-model helpers/tests; pages/insights.html;
insights components and web/static/js/insights.js; render tests.
Scope Tier2 tests,a11y,second. Consumes DI1 groups and DI2 explicit comparisons.

- [ ] Test rendered comparison bounds, largest three contributors (absolute
  dollar difference, stable category tie-break), no-history and no-findings.
- [ ] Order: period context; What changed; Review these transactions; three
  recurring groups; supporting charts/tables. Replace repeated summary tiles.
- [ ] Findings preview <=5, hash+type dedup, full count and usable all-results
  destination. Reuse existing anomaly and price-creep evidence; no invented
  findings, savings promises, or confidence. Preserve category+date drilldowns.
- [ ] Merchant names first, Major Expense labels secondary; estimated annual
  and monthly values explicitly distinct from actual spending. Historical
  expected payments anchored to reference, not presented as overdue today.
- [ ] Scope recurring row totals consistently; all retained rows accessible.
  Verify HTMX refresh replaces context and findings as well as chart/table.
- [ ] Run renderer/handler/service tests and responsive/theme probes; write
  report/manifest, named review and gate.

## Task 5: DI5 — Integration and demonstration

Files: focused integration/render regression tests and .swarm/DI-run evidence.
Scope originally Tier2; escalated to Tier3 oracle + tests,a11y,second after
two failures, with the narrow user-authorized attempt4 reopening documented
in the canonical SPEC. No speculative features.

- [ ] Check Dashboard/Insights same-date agreement including historical/custom
  ranges, refunds, incomplete month, no history, missing plan, empty data.
- [ ] Verify all new drilldowns and HTMX updates; confirm SSR and dynamic
  values use same producer and rounding, and selected range survives navigation.
- [ ] Run full build/vet/test/staticcheck plus CSS freshness and existing agents2
  smoketest. Full-site a11y in both themes; review only new defects as blockers.
- [ ] Capture 390/768/1440px with synthetic dataset and inspect; use isolated
  synthetic copies for mutating probes. No real data access for test fixtures.
- [ ] Named independent tests/a11y/second, gate check DI5, done, stats verbatim.
  Final reviewed code commit; demonstrate on :8081 with synthetic data. No main
  server restart, shared-branch push or merge without separate authorization.

## Rulings / persistence

User addition DI6: refresh_pages tool, scope and tests in
.swarm/DI-run/DI6-brief.md. Tier2 tests,a11y. It may run beside DI1 because its
server-wiring/new-refresh-script territory is disjoint; no tree-copy checks
until both workers freeze. DI5 final integration also covers DI6 and direct demo
MCP data-path verification. Lead retains all git-state ownership.

Ruling: user AGENTS gate, named checker lanes, hard stops and durable evidence
override the generic SDD review/cleanup mechanics. Keep .swarm/DI-run committed,
never delete it at finish. Cost if wrong: additional review overhead, no loss
of traceability. Tasks begin from validated 69e484a; baseline full build/vet/test/
staticcheck and precommit quality hook passed in the preceding turn.

The five detailed dispatch briefs are preserved verbatim in
.swarm/DI-run/briefs/ rather than relying on ignored working notes. Later
canonical SPEC rulings and DI5.4-brief.md govern the final narrow reopening;
DI5.2/DI5.3 briefs and failed evidence remain historical records. The user's
subsequent merge/main-deployment authorization remains conditional on complete
acceptance. The ledger and named gate-backed reports are completion authority;
the original planning checkboxes above are not a separate acceptance ledger.
