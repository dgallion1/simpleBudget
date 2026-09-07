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

