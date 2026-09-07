# DI3 attempt 1 — author implementation evidence

DI3 source is frozen at handoff for the lead's tests/a11y/second reviewers and gate. This report is not a verdict or acceptance. The rounding clarification was a lead contract clarification before implementation, not a failed implementation attempt.

## Scope and result

Implemented the approved Operate-mode refinement: four same-period primary figures (recorded income, net spending, cash-flow balance, spending versus plan). Living/healthcare monthly equivalents and their period amounts, target provenance, full-ledger coverage start and separately modeled costs remain visible below. The existing plan explanation and comparison are in a native disclosure. Income/expense/living/healthcare/cash-flow detail routes, month drilldowns and CSV schemas remain available. Account balances/dates and all five existing charts remain; redundant mini-sparklines were removed in favor of their retained monthly details and budget history chart.

Cash-flow language is derived from displayed values, including a neutral matched-income/spending explanation. Empty selected windows do not receive a positive verdict. Missing plan links to What-If; absent healthcare coverage has an explicit unavailable monthly equivalent. The overview states that cash flow does not measure portfolio withdrawals or retirement sustainability. Insights navigation carries the current selected start/end, including after HTMX refresh.

Dashboard uses the existing shared range-picker's wrapping/stacked options (no shared range component edit). At 390px the page now has scrollWidth 390, replacing the reported 496px baseline overflow. Native buttons replace clickable primary cards. The existing import drop zone gained keyboard activation. All five charts have adjacent native data disclosures, including the budget target reference line. They transcribe plotted series values without browser arithmetic or an independent currency rounding algorithm. The table caption identifies chart-specific scope; it makes no claim that a capped plot reconciles to a longer selected period.

Impeccable distill/operate/craft-floor guidance influenced the four-figure hierarchy, restrained use of existing tokens, visible provenance, and native disclosures. No design approval was reopened, fonts/frameworks/dependencies changed, or decorative icons introduced.

## Isolation

Worktree: `/home/darrell/bin/ai/budget2/.worktrees/dashboard-insights`. Compared read-only against `/tmp/budget2-DI3-base.rhoTJM`, which contains accepted DI1.2/DI6.2/DI2.2. No agents, commits, index/branch operations, ledger/spec/verdict edits, storage/dataloader/retirement/accounts/transfer/confirmation production changes, or live-tree copies.

Only the authorized synthetic fixture `/tmp/budget2-DI-demo.mfLral/data` was copied with `cp -aL` into `/tmp/budget2-DI3-visual.eCMIJ3/data`. The temporary server used `BUDGET_LISTEN_ADDR=127.0.0.1:18773`, `BUDGET_DATA_DIR=/tmp/budget2-DI3-visual.eCMIJ3/data`, and independent `BUDGET2_BACKUP_DIR`/`BUDGET2_IMPORT_DIR` paths under that same temporary directory. It was stopped at handoff. Neither :8080 nor :8081 was read, restarted, or modified. No active browser-session data was used.

Browser runtime bootstrap failed with `trusted Node services require process isolation; legacy Landlock is not supported for restricted sandboxes`. Used the explicitly authorized fresh-context headless Playwright fallback; requests in the evidence harness are restricted to the DI3 loopback origin. Direct apply_patch/view_image also hit legacy Landlock; scoped escalated `rtk proxy apply_patch` and base64 screenshot reads were used. No approval rejection occurred.

## Interfaces and consumer inventory

- New `metrics.ReportingMoney(float64) float64`: `models.RoundToCents` precision with positive-zero normalization. No stored/raw metric mutation.
- New `metrics.ReportingCashFlow(income, spending float64) metrics.CashFlowDisplay`: rounded raw aggregate Income/Spending anchors, their rounded Balance difference, and sign-derived Explanation. Grouping cannot change these period anchors.
- `dashboard.BuildBudgetVerdict`: additive CashFlow member; its carried NetSavings/TotalIncome use that reporting producer. Budget classification thresholds, target arithmetic and bucket variance policy remain unchanged.
- Full Dashboard and HTMX KPI responses still use accepted DI2 `dashboardPeriod`, `insights.BuildPeriodContext`, `TransactionsForPeriod`, `HasCivilDateCoverage`, and `dashboard-period-kpis`. Neither shared `period-context.html` nor Insights source changed.
- Headline figures and neutral/positive/negative explanation consume BudgetVerdict.CashFlow. Raw `.Metrics` remain raw metrics, including the JSON fallback, rather than silently changing calculations for other consumers.
- KPI detail modal: each month's income/spending/difference uses the same reporting helper. Period totals preserve rounded raw aggregates. A nonzero `RoundingAdjustment` appears once beside a complete monthly breakdown and explicitly says it is not a transaction. Living/healthcare classification and their distinct average denominators remain unchanged.
- Month drilldown: rounded income/spending and derived cash-flow total use the same helper; raw transaction rows remain unchanged.
- CSV: existing columns, month keys, selected filters and signed refunds retained. Monthly monetary values use the same canonical reporting precision; no invented total or adjustment row was added to a schema that never contained a period total.
- Cumulative cash-flow chart: accumulates raw income/spending separately and derives each plotted balance through ReportingCashFlow. This is cumulative raw-anchor rounding, not a sum of rounded days. Transfers and signed refunds/reversals preserve their existing treatment.
- Budget history, Major Expense, spending-change and Top Spending charts preserve their existing scope/calculations. Dashboard-only JS adds adjacent table alternatives for actual Plotly series and target-line shape data and updates them on replot. It does not classify, sum, or independently round money.
- MCP `summarize_spending`: period income/spending/net use ReportingCashFlow; additive `monthly_rounding_adjustment` equals canonical period expenses minus the sum of independently rounded monthly rows, with normalized zero. Description now promises monthly rows PLUS adjustment; category/merchant text discloses truncation and per-row rounding. Breakdown grouping/scope and stored values remain unchanged.
- MCP shared `round2`: delegates to ReportingMoney. Semantic references enumerated summary rows, recurring estimates, trend rows, anomalies and price-creep amounts/metadata. These consumers retain their grouping/classification and share the canonical precision; accepted DI1/DI2 source was not edited.

## Semantic references before changes

No callable LSP was available; used semantic `rtk proxy gopls references <file>:<line>:<column>` plus source/template/JS enumeration. Successful production-symbol probes (positions before edits):

- dashboard/handlers.go:139:6, :226:6, :422:6, :691:6, :809:6 — full/KPI/detail/month/export handlers; direct references are registered routes. :410:5 enumerated kpiTitles (not changed).
- dashboard/verdict.go:45:6 and :170:6 — BudgetVerdictView and BuildBudgetVerdict; full/KPI handlers plus focused renderer/service fixtures.
- mcpsvc/spend/summary.go:49:6 and :161:6 — summaryOutput and registerSummary; register.go and tool fixtures.
- mcpsvc/spend/insights.go:86:6 — shared round2; summary/recurring/trends/anomaly/price-creep consumers as above.
- metrics/metrics.go:373:6 — Calculate blast radius includes Dashboard/MCP/period adapters and many tests; raw Calculate was deliberately not edited.
- dashboard/handlers.go:1631:6 — buildCumulativeChartData; chart HTTP adapter and existing cumulative tests.
- Focused modified test/helper symbols were also inspected; a few stale/wrong coordinates returned no identifier or irrelevant references and were corrected (not treated as evidence of no callers). Two integration-test references at cmd/server/main_test.go:129:6 and :146:6 confirmed no static callers.

## Red → green commands

All commands ran with RTK in the worktree, using synthetic fixtures.

1. `rtk proxy go test ./internal/handlers/dashboard ./internal/services/mcpsvc/spend -run '^TestDI3' -count=1`: exit 1 before implementation. Renderer lacked four-figure wording, neutral/empty states and selected-range Insights links. Fractional CSV/month totals failed. MCP returned income=10.01/spend=0/net=10; 2.675 income=2.68; negative zero; no monthly adjustment. Same command exited 0 after implementation.
2. `rtk proxy go test ./internal/handlers/dashboard -run '^TestDI3Cumulative' -count=1`: exit 1 with final plotted cash flow 10.002 versus displayed 10.01. Same command exited 0 after the reporting-only chart adapter.
3. `rtk proxy go test ./internal/handlers/dashboard -run '^TestDI3Healthcare' -count=1`: exit 1 for missing no-coverage explanation; included in the final successful scoped suite.
4. `rtk proxy env DI3_RED=1 node .swarm/DI-run/reports/DI3-browser.cjs`: initially exit 1, decision charts lacked adjacent data tables. Extended probe checked the target line. Final command exited 0 across six states, including keyboard opening the disclosure. A probe initially used innerText on a CLOSED disclosure; opening it corrected that harness error. A stale embedded binary also caused an interim repeat failure; rebuilt only the DI3 runtime.

Fixtures cover 10.006 income/0.004 spending; 2.675 precision; negative and matched cash flow; signed refunds; negative zero; selected-month filtering; missing plan/selection/healthcare coverage; and two 0.004 monthly expenses -> period 0.01, rows 0.00/0.00, adjustment 0.01. Existing real-render refund, account-date, target-provenance, coverage-mutation, plan-exclusion, year-history, modal and CSV tests remain exercised.

Legacy assertions were adapted to the approved layout: four labels and native buttons; visible provenance rather than title-only text; monthly detail and budget-history routes rather than removed mini-sparklines; full-ledger coverage date now visible even when it predates selection; zero adjustment metadata in existing modal JSON. The budget tint test still guards the same threshold, using observed-data fixtures and the new zero-static-tint baseline. The no-plan verdict fixture now supplies its actual expense component instead of an inconsistent raw NetSavings alone.

## Final verification

- `rtk proxy go test ./internal/handlers/dashboard ./internal/services/metrics ./internal/services/mcpsvc/spend ./internal/templates -count=1`: exit 0 after final production changes.
- `rtk proxy go build ./...`, `rtk proxy go vet ./...`, `rtk proxy staticcheck ./...`: exit 0.
- `rtk proxy go test ./...`: initial full run failed only two cmd/server Dashboard assertions expecting old Total Income/Total Expenses labels. Updated only those Dashboard render checks to the four approved labels. `rtk proxy go test ./cmd/server -run '^TestDashboard' -count=1` then exited 0; final full `go test ./...` exited 0 (successful unchanged packages cached). Final log: `DI3.1/go-test.log`.
- `rtk proxy node --check web/static/js/dashboard.js`: exit 0. `rtk proxy node --test web/static/js/page-refresh.test.cjs`: all seven existing DI6 regressions succeeded.
- `rtk proxy make css` used existing pinned Tailwind 3.4.17. `rtk proxy make css-verify`: exit 0. Only the existing outdated caniuse-lite advisory appeared; no dependency update was made.
- `rtk proxy node .swarm/DI-run/reports/DI3-browser.cjs`: final exit 0 at 390/768/1440 in light and dark. Exactly four primary headings; scroll widths equal viewport widths, no overflow descendants; five chart disclosures; native cash-flow modal open/Tab trap/Escape/focus return; keyboard import picker; selected-range HTMX focus and Insights URL update. Initial later filechooser timeout resolved by explicitly clearing each empty chooser in the harness. One initial capture batch and one confirmation batch; no further visual polishing rounds.
- `rtk proxy env DI3_A11Y=1 node .swarm/DI-run/reports/DI3-browser.cjs`: exit 0; axe WCAG 2 A/AA, 2.1 AA, 2.2 AA tags reported zero violations in six viewport/theme states. This is automation evidence, not independent constitution certification. Summary: `DI3.1/axe.json`.
- `rtk proxy /home/darrell/.codex/plugins/cache/impeccable/impeccable/4.2.0/skills/impeccable/scripts/impeccable detect --json web/templates/pages/dashboard.html web/templates/components/kpis.html web/templates/components/kpi-detail.html web/static/js/dashboard.js`: exit 0, `[]`.
- `rtk proxy git diff --check`: exit 0. Read-only `git diff --no-index --name-only` against immutable baseline cmd/internal/web enumerated exactly the 18 source/test/generated-CSS paths in the manifest. DI1/DI2/DI6 source and shared period context remain unchanged from that baseline.

## Accessibility and review notes

Read all 17 constitution points. The inspected page has one h1 and retained landmarks; four h2 primary labels and h2/h3 budget hierarchy; native actions/navigation and labeled date controls; signed cash-flow/refund explanations; inherited theme tokens; no hidden-text workaround. Adjacent chart data tables use scoped headers and keyboard disclosures. Existing modal focus management remains in place and was exercised. Existing banner dismissal/refresh behavior and reduced-motion paths were retained; DI6's seven client tests remained green. New no-coverage/empty states are visible text. Automated axe is not proof of chart-stroke contrast or every pre-existing workflow; named reviewers remain responsible for that independent check.

Screenshots in `DI3.1/{light,dark}-{390,768,1440}.png` were inspected. No viewport overflow, clipped figures, stacked-card nesting, added fonts, or palette replacement. `DI3.1/viewports.json` contains exact measured dimensions and keyboard results. Budget comparison remains the existing living/healthcare comparison, explicitly excluding separately modeled costs; it is not advertised as an all-spending target. Chart alternatives intentionally expose plotted numeric values, including any precision present in those series, rather than claim that an incomplete plot sums to a period headline.

The manifest includes every DI3-created/changed repository file and generated evidence. No PASS verdict or accepted state was written. The lead owns independent review, ledger, escalation and gate decisions.
