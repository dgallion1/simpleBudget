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

