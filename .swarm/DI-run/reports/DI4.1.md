# DI4 attempt 1 — author evidence

STATUS: DONE. Source frozen for the lead's independent tests/a11y/second reviewers and gate. This document is author evidence, not a verdict or task acceptance.

## Scope and implementation

Worktree: /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights.
Read task-4-brief.md, the approved dashboard-insights design, repository AGENTS.md and the complete ACCESSIBILITY.md (points 1–17). The two subsequent lead clarifications govern reachable partials and price-creep anchors.

The live page now orders period context, What changed, Review these transactions, three recurring groups, and supporting charts/tables. Repeated live summary tiles and the repeated pace/gauge presentation were removed. Income patterns, period-aware pace/forecast availability, category trends, anomalies and the separately scoped full-history price-creep table remain accessible.

What changed uses SignedNet of selected/prior Outflow transactions, then ReportingMoney and ChangeDisplay. It includes signed refunds, excludes transfers, and does not treat missing comparison history as zero. Contributors reuse CategoryTrend.Change and rank by absolute dollar difference with category-name ties. The grouping label identifies Major Expenses and unmatched exclusions when configured; contributor/trend links use actual Major Expense IDs in that case, category filters otherwise, with selected dates and Outflow type. The top three expressly do not reconcile all spending.

Findings detect against loaded active history. Anomalies join by actual selected transaction hash. Price creep joins the exact merchants.GroupTransactions active-negative-Outflow groups by Creep.GroupKey, including token-subset merging. It chooses the last eligible transaction of the full group by date then hash ascending order (latest date / greatest tied hash), THEN checks whether that anchor is selected. It never selects an earlier purchase to pull later evidence into a historical preview. Actual transaction amounts and first-three/last-three medians are separately labeled. The complete list is deduplicated by hash plus type before a five-item preview; remaining real items are inside native details on the same page, preserving selection. Finding IDs include type/hash; Explorer links preserve actual description/category and the selected dates. Explorer currently filters descriptions rather than accepting a unique hash parameter; no Explorer source changed.

Recurring presentation retains the raw detector, classifications and compatibility values. Display rows use ReportingMoney for payment, annual, and annual/12 monthly estimates; group/grand display totals sum those displayed rows and normalize zero. Monthly/annual estimates are explicitly separate, with one live-page rounding disclosure. All detected recurring series remain available without a row limit. Merchant is primary; Major Expense and classification reason are secondary. Expected payment dates are labeled as estimates as of the selected reference, not overdue today or confirmed obligations.

Impeccable Operate/distill guidance influenced the linear hierarchy, native disclosure, restrained incumbent tokens and removal of repeated tiles. No new fonts, dependencies, framework, palette or detector algorithm. Its detector returned [] before handoff. One initial browser capture batch, one correction batch and one confirmation batch; no further browser polishing rounds.

## Consumers and references

Semantic gopls references plus rg were used before changing existing handler symbols:
- handlers.go original :225:6 handleInsights: RegisterRoutes and Insights handler/period renderer tests.
- :287:6 handleRecurringPartial: registered /insights/recurring plus handler tests.
- :315:6 handleTrendsPartial: registered /insights/trends plus handler and DI2 tests.
- :338:6 handleTrendsChartData: registered chart route and handler/DI2 tests; inspected, production Go adapter unchanged.
- :416:6 handleIncomePartial: registered /insights/income plus handler tests.
- New buildFindings :71:6: buildInvestigation and new DI4 tests, checked before the clarified anchor adjustment. An earlier :62 probe resolved field references rather than the function and was corrected.
- JS rg enumeration: date/preset/step handlers, sort functions, page/components' delegated attributes. No callable LSP exists for this task.

Consumer contract:
- /insights full and HX responses include the entire wrapper: context, change, findings, recurring groups, chart/table.
- /insights/recurring now resolves selected dates and returns the same grouped display rows/reference as the full page; raw compatibility JSON keys remain.
- /insights/income now resolves selected dates and filters active transactions before IncomePatterns; its rendered partial includes period context.
- /insights/trends now renders dated, linked chart-alternative rows; /insights/trends/chart and /insights/velocity retain DI2's shared period algorithms.
- Chart values come directly from the existing endpoint and match the adjacent table; JS only changes presentation (orientation remains the incumbent shared chart behavior, spacing, theme markers). No browser money calculations. Selected/prior series use existing theme tokens / neutral colors and distinguish prior bars with a pattern.
- Existing raw-model template callers without Investigation/Period retain their compatibility subscription, recurring and ChangeCell rendering. These are not hidden copies on the live page.
- Dashboard, shared period sources, storage/dataloader, accounts/transfers, retirement engine, and MCP schemas/adapters were not edited.

## Red → green evidence

All shell commands used RTK, from the worktree:
1. `rtk proxy go test ./internal/handlers/insights -run TestDI4InvestigationRendered -count=1`: exit 1 before production edits. Full and HX output lacked ordered investigation sections, What changed, findings and recurring group states. Then `rtk proxy go test ./internal/handlers/insights -run 'TestDI4InvestigationRendered|TestDI2InsightsPeriodFullAndPartials' -count=1`: exit 0 after initial implementation.
2. `rtk proxy go test ./internal/handlers/insights -run TestDI4 -count=1`: exit 1 on reachable partials losing selected bounds/groups. The first income fixture had only one row per employer, below the incumbent detector's two-occurrence minimum; corrected that fixture before relying on its exclusion check.
3. `rtk proxy go test ./internal/handlers/insights -run 'TestDI4Partials|TestDI4Findings' -count=1`: exit 1 with the corrected income fixture (July leaked into August), missing recurring groups/date context, and eleven later full-history findings pulled into May. These failures preceded the corresponding fixes.
4. `rtk proxy go test ./internal/handlers/insights ./internal/templates -count=1`: handlers green; renderer initially exposed missing legacy no-Period and raw-subscription compatibility. Preserved those callers; same packages then green without changing their tests.
5. `rtk proxy go test ./internal/handlers/insights ./internal/templates ./internal/services/insights ./internal/services/anomalies ./internal/services/pricecreep ./internal/services/merchants ./internal/services/metrics -count=1`: exit 0 after the regression additions. Coverage includes rendered bounds/order/top-three, signed refunds/transfers, no-history/no-findings, eleven real creep detector groups, duplicate same-type evidence, different types on one hash, subset joins, selected end-day noon timestamps, reversed-input stability, real full-list anchors/disclosure, and fractional recurring estimates/zero normalization.
6. `rtk proxy node .swarm/DI-run/reports/DI4-browser.cjs` (stdout redirected to the synthetic temporary directory): initial exit 1. Browser-first.json records 390px scrollWidth 446, nonfocusable price-creep scrolling, and preset buttons failing after replacement. Initial chart colors also lacked the required contrast.
7. `rtk proxy env DI4_OUTPUT=/tmp/budget2-DI4-visual.0j6FTL/confirmation node .swarm/DI-run/reports/DI4-browser.cjs`: exit 0 after the single correction batch. Actual invocation used the equivalent environment assignment inside `rtk proxy sh -c` with stdout redirected. Results are browser-confirmation.json.

Additional money/ranking/render cases were added after the first renderer red phase; their results are regression coverage, not a claim that each independently failed before implementation.

## Final verification

- `rtk proxy go build ./...`: exit 0 (build.log).
- `rtk proxy go vet ./...`: exit 0 (vet.log).
- `rtk proxy staticcheck ./...`: exit 0 (staticcheck.log).
- `rtk proxy go test ./...`: exit 0; one full-suite invocation at the end (fullsuite.log, 50 package-result lines, some unchanged packages cached). These four commands actually ran inside `rtk proxy sh -c` to redirect their output into /tmp before copying evidence here; each command's own exit status was preserved.
- `rtk proxy node --check web/static/js/insights.js`: exit 0.
- `rtk proxy make css`, `rtk proxy make css-verify`: exit 0. Existing pinned Tailwind 3.4.17; existing outdated caniuse-lite advisory only. tmp/tailwind.check.css is the generated verification artifact and is included in the manifest.
- `rtk proxy /home/darrell/.codex/plugins/cache/impeccable/impeccable/4.2.0/skills/impeccable/scripts/impeccable detect --json web/templates/pages/insights.html web/templates/components/insights-investigation.html web/templates/components/anomalies-section.html web/templates/components/pricecreep-section.html web/static/js/insights.js`: exit 0, [].
- `rtk proxy git diff --check`: exit 0. No commits/index/branch/stash operations.

## Browser measurements and constitution review

Fresh headless Playwright contexts, no user sessions, reduced motion, requests restricted to http://127.0.0.1:18775. Used the explicitly authorized fallback because the lead documented trusted-process-isolation failure. Direct apply_patch and view_image hit legacy Landlock in this session; scoped escalated apply_patch and base64/cropped screenshot inspection succeeded. No automatic approval rejection.

Six confirmation screenshots: DI4.1/DI4-{light,dark}-{390,768,1440}.png. Full measurements: DI4.1/browser-confirmation.json.
- scrollWidth exactly 390, 768, 1440 in the corresponding viewport, both themes.
- Zero axe WCAG 2 A/AA, 2.1 AA and 2.2 AA tag violations in all six states.
- Explicit label-content-name-mismatch rule: zero violations in all six; not inferred from default axe.
- Chart series/table amounts compared exactly. Trace/background contrast: light 6.29:1 and 4.83:1; dark 7.36:1 and 5.78:1.
- One h1 and ordered h2/h3 hierarchy. Full and refreshed wrapper count one; selected-date findings URLs update. Date input and preset button focus restored; preset remains usable after replacement. No page errors.
- Synthetic August data has two preview findings, so its browser screenshots do not exercise a >5 disclosure. The eleven-group real-detector fixture verifies all rendered remaining items and unique anchors inside the native disclosure. This distinction is retained for independent review.

Points 1–4: landmarks retained, ordered headings, scoped table headers, native links/buttons/details, visible date labels. Points 5–6: no new validation flow; native date inputs retain explicit From/To labels and date formats. Points 7–8/12: automated text contrast, explicit chart contrast, theme parity and text/pattern equivalents. Points 9–10: keyboard scrolling, sorting, native disclosure, and post-swap control focus. Point 11: adjacent exact-value table and comparison summary. Point 13: dollar changes say increase/decrease/no dollar change; findings identify actual signed outflows; estimates are separately labeled. Points 14/16: shared DI2 freshness disclosure is retained; no new dismissible banner or live announcement/suppression channel. Point 15: loading is text; no new motion; probes request reduced motion. Point 17: no modal added.

Automated checks and author review are not a substitute for the requested independent constitution review.

## Byte comparison and isolation

Compared only the immutable /tmp/budget2-DI4-base.KPl7oY source trees with `rtk proxy diff -qr BASE/internal internal`, and likewise web and cmd. No live tree copied.
- internal: only handlers/insights/handlers.go differs; investigation.go and investigation_di4_test.go are new. Also an empty internal/config/data directory was created by configuration tests (no files, no production config changes).
- web: only static/css/tailwind.css, static/js/insights.js, templates/pages/insights.html, components/anomalies-section.html and pricecreep-section.html differ; insights-investigation.html is new.
- cmd: byte-identical (diff exit 0).
Diff exit 1 for internal/web denotes precisely those expected differences. No accepted Dashboard/DI1/DI2/DI6 production file outside the authorized Insights handler integration changed.

Only /tmp/budget2-DI-demo.mfLral/data was copied with cp -aL into /tmp/budget2-DI4-visual.0j6FTL/data. Independent import and backup directories stayed under that temporary root. BUDGET_LISTEN_ADDR=127.0.0.1:18775; BUDGET_DATA_DIR and BUDGET2_IMPORT_DIR/BUDGET2_BACKUP_DIR named those synthetic paths explicitly. Both temporary binary processes were stopped, last PID 2559687. Never read/contacted :8080/:8081, the installed connector or real household data. No deployment, subagent, ledger, spec, verdict or git-state edits.

The requested .swarm/DI-run/manifests/DI4.1.files and worker-contract .swarm/manifests/DI4.1.files carry the identical complete file inventory, including both manifests. Lead-owned docs are preserved. Source is frozen at this handoff; named reviewers and gate remain the lead's next step.
