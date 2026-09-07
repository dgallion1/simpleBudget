## Task 5: DI5 — Integration and demonstration

Files: focused integration/render regression tests and .swarm/DI-run evidence.
Scope Tier2 tests,a11y,second; no speculative features.

Read-only baseline audit now available at
.swarm/DI-run/reports/DI5-a11y-baseline.md, with rerunnable harness/artifacts
in /tmp/DI5-a11y-baseline.Yd8pno. It covers nine nav routes in both themes,
records unresolved/manual checks and baseline input/alternative/loading issues,
and is NOT a DI5 PASS. Use exact snapshot hashes/selectors for attribution;
rerun final-site checks on the frozen final build, not concurrently edited CSS.

Probe promotion (user contract): preserve meaningful independent probes as
durable tests instead of leaving their evidence only under /tmp. Some failing
probes are already promoted in DI1/DI2/DI6; avoid duplicates. Remaining useful
coverage is now inventoried precisely in .swarm/DI-run/reports/DI5-promotion-map.md
(14 deduplicated families, exact existing assertions, helpers and safety
adaptations). Read that map first; do not clone already-durable cases or the
old repo-testdata root used by the throwaway saved-file probe. Remaining useful
sources include DI1 boundary/alias cases in
/tmp/DI1-second-attempt2.T0x6Cm/internal/services/insights/di1_attempt2_adversarial_test.go;
DI2 calendar/forecast matrix in /tmp/DI2-second-attempt2.OXW9PF/internal/services/insights/di2_second_period_test.go;
DI2 coverage matrix in that same directory's di2_second_coverage_test.go;
and the refresh saved-data preservation probe in
/tmp/di6-checker-Oa0W6B/cmd/server/checker_di6_probe_test.go.
DI3 independent rendered-cent residual matrix and MCP grouping-invariance cases:
/tmp/DI3-second.O8fPRh/internal/handlers/dashboard/di3_second_render_test.go and
/tmp/DI3-second.O8fPRh/internal/services/mcpsvc/spend/di3_second_grouping_test.go.
DI3 primary exact rendered components/detail/month/CSV residual probes:
/tmp/DI3-checker-tests.eHC5LI/internal/handlers/dashboard/zz_checker_di3_test.go;
all plotted/table-cell parity probe in that snapshot's tmp/checker-browser.cjs.
The DI3 a11y Label-in-Name failure probe is being promoted in DI3 attempt2;
avoid duplicating it. Its original source is /tmp/DI3-a11y.mBCqsi/focused.cjs.
Primary DI1/DI2 retained renderer/consumer probes are documented in their
verdicts; adapt assertions to the final approved layout, preserving tested
behavior/values rather than obsolete labels. Inspect for already-covered cases
before importing. Browser traces may be preserved as reproducible test-only
scripts with explicit prerequisites; never add a production dependency or
silently claim unavailable browser checks passed. Record promotion/coverage
mapping in the integration report and run the promoted tests.

- [ ] Check Dashboard/Insights same-date agreement including historical/custom
  ranges, refunds, incomplete month, no history, missing plan, empty data.
  Lead verified live demo get_trends for July1–31 versus June1–30, history
  available, no current-month forecast. Expected largest Major Expense dollar
  contributors from that tool: Home Maintenance & Appliances +899.00
  (1199.00 vs300.00), Shopping & Household Supplies +108.31 (201.15 vs92.84),
  Cash Spending +100.00 (100.00 vs0.00). Use this real synthetic-demo example
  for final same-date UI/MCP alignment; reverify if demo data changes.
  Live demo get_recurring(reference_date=2026-08-28) returned 22 series:
  3 subscriptions (Netflix, Spotify, Cloud Storage), 7 bills, 12 other.
  Preserve these as same-reference UI/tool demo checks, not production rules.
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

Ruling: user AGENTS gate, named checker lanes, hard stops and durable evidence
override the generic SDD review/cleanup mechanics. Keep .swarm/DI-run committed,
never delete it at finish. Cost if wrong: additional review overhead, no loss
of traceability. Tasks begin from validated 69e484a; baseline full build/vet/test/
staticcheck and precommit quality hook passed in the preceding turn.
