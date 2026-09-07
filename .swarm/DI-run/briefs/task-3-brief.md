## Task 3: DI3 — Dashboard overview

Files: handlers/dashboard, models/dashboard.go if needed; pages/dashboard.html;
components/kpis.html and supporting components; web/static/js/dashboard.js;
focused dashboard/render tests. Scope Tier2 tests,a11y,second.
Consumes DI2 period context and existing metrics/target provenance.

Lead preflight: enumerate the MCP summarize_spending consumer too. Its
summary.go currently rounds income, expenses and raw net independently through
spend.round2; that helper uses math.Round, whereas formatMoney uses the exact
models.RoundToCents path. The approved shared monetary-display contract permits
the minimal summary/shared-rounding adapter edits needed for parity. Preserve
raw ledger semantics and every breakdown's documented scope. Do not silently
change the retirement engine or stored values. Add an actual rendered fixture
with income 10.006 and expense 0.004: the shown cash-flow difference must equal
the shown components; also test 2.675 at the UI/MCP boundary, signed refunds and
negative zero. A displayed zero cash-flow balance must not claim income is
above/below spending solely because an unrounded float has a sign; use a neutral
matched-income/spending explanation from the same displayed-value producer.
Enumerate headline, verdict, detail/modal and tool consumers,
not just the new tile. If the right scope exceeds these reporting adapters,
stop for lead direction before editing it.

Lead rounding ruling: preserve period income/spending as canonical rounded raw
period aggregates, independent of grouping. Derive displayed cash flow from
those rounded period components. Do not round each transaction or silently
move pennies into a month. Round monthly rows independently with the same
precision helper; if a complete monthly breakdown is claimed to reconcile,
expose the residual explicitly as a rounding adjustment (not a transaction or
invented month). For MCP add a monthly_rounding_adjustment field and correct
the description to say monthly rows plus that adjustment equal total_expenses.
For a displayed complete breakdown with a period total, show the nonzero
adjustment once; never imply the last-six-month chart covers a longer window.
Category/merchant top-N descriptions must not promise exact reconciliation;
explain truncation and per-row rounding. No export schema or stored-data change
is required merely to invent a total where none was shown. Fixture: two months
each 0.004 => period 0.01, monthly rows 0.00/0.00, adjustment 0.01.

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
