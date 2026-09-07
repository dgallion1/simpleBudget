# DI5.1 author report — frozen, BLOCKED

DI5.1 test-only implementation and evidence are frozen for named review.
Product acceptance is BLOCKED by the confirmed DI3–DI6 refresh-notice focus
regression. Lead CONCEDES; named a11y FAIL has been requested. No production
fix, git operation, deployment, ledger/spec edit, PASS verdict or gate acceptance
was performed by this worker. Lead owns the correction scope and fresh review.

## Confirmed blocker — supersedes earlier checkpoint attribution

The refresh notice completely hides focused Dashboard controls at 390/768px
in BOTH themes. Independent review proved the same failures with all production
scripts active and proved they are absent from original 69e484a and pre-DI3.
This is a real run-introduced DI3–DI6 integration regression, not legacy/out of
scope. Constitution point 9 / WCAG 2.2 AA 2.4.11 are implicated.

| Width | Focused control | Focus rect x/y/w/h | Notice rect x/y/w/h |
|---|---|---|---|
| 390 | Income details | 33/852/96.5625/20 | 24/738/350/146 |
| 390 | Budget details | 33/763/94.90625/20 | 24/738/350/146 |
| 768 | Cash-flow details | 33/847/111.1875/20 | 24/762/728/122 |
| 768 | Healthcare details | 392/795/118.40625/20 | 24/762/728/122 |

All geometry is at height900 and repeats in light/dark. Worker evidence:
DI5.1/refresh/obscured-*.json/png and refresh.json. Independent proof is copied
VERBATIM to DI5.1/independent-focus-attribution.md from
/tmp/DI5-final-a11y.JYHU8N/FOCUS-ATTRIBUTION.md. Its associated all-scripts-active
traces, hit tests, screenshots and original/pre-DI3 counterfactuals remain in
/tmp/DI5-final-a11y.JYHU8N/artifacts/obstruction.

The accepted /tmp/budget2-DI4-base.KPl7oY already includes DI3. Matching it proves
only unchanged by DI5, NOT pre-existence to the run. The refresh harness's strict
default absence-of-obstruction check fails. Its optional DI5_REFRESH_BASELINE
comparison returned exit0 with identical obstruction geometry; that result is
DIAGNOSTIC ONLY and MUST NOT be used as task acceptance or a baseline allowlist.
The current diagnostic harness is preserved for strengthening during correction.

## Territory and provenance

Snapshot /tmp/DI5-integration.hcuAxN was copied ONLY from immutable
/tmp/budget2-DI4-base.KPl7oY, overlaid with ONLY cumulative DI4.2 manifest paths,
then explicitly owned DI5 files. No whole-live-tree copy. Source provenance
verified 674 baseline source/config/standard files and all ten frozen DI4
application/test/CSS paths. Non-DI4 baseline source remained byte-identical;
DI4 paths matched live frozen source. See DI5.1/provenance/provenance.json.

New files: 17 Go test files, seven reproducible evidence scripts, this report,
the artifact bundle and complete dual manifests. No pre-existing application
file was edited, including existing DI4 tests and CSS. Canonical design,
ACCESSIBILITY.md, promotion map, baseline audit and all DI4.1/DI4.2 verdicts
were read. Canonical cap clarification is honored: recurring results remain
uncapped; the old20 cap is not asserted as a production rule.

Only authorized synthetic /tmp/budget2-DI-demo.mfLral/data was copied with
dereferencing, into separate temporary runtime data/import/backup directories.
Mutating test fixtures use temporary storage. Saved-file probe avoids the old
repository-testdata helper. Servers used :18851 final and :18852 accepted
baseline; neither :8080 nor :8081 was contacted. Both worker servers were
terminated at handoff. No installation or dependency download.

## Promotion/coverage map

The source key references below are the exact roots/files in
.swarm/DI-run/reports/DI5-promotion-map.md. All Go entries are executed by the
final promoted-test command and full suite; all named browser scripts were
executed against the isolated frozen build. Tests are new DI5 files because
existing files were outside worker territory.

| Family | Durable file and exact test / evidence |
|---|---|
| M01 S1 descriptor/alias boundary | internal/services/insights/classification_di5_test.go — TestDI5EvidenceBoundaries; original13 cases ×4 aliases plus NETFLIX.COM/NotNetflix category boundaries; exact kind, reason, wrapper parity |
| M02 S1 mixed cadence | internal/services/insights/recurring_cadence_di5_test.go — TestDI5CadenceAndMerchantRetention; .99 monthly/12.34 quarterly/25.50 yearly, three distinct merchants and annual values |
| M03 P1 final recurring renderer | internal/handlers/insights/recurring_render_di5_test.go — TestDI5RecurringRenderedRetention; rendered2110/25320, subscription10/count1, twenty other merchants,22 annotations/reasons/links; full and final standalone groups; decoded query dates/type |
| M04 P1 projections | internal/handlers/dashboard/recurring_di5_test.go and internal/services/mcpsvc/ledger/recurring_di5_test.go — TestDI5ProjectionRetention in each package; all22,1/1/20,25320, anchor5000/minimum2890 through dashboard and both ledger adapters |
| M05 S2 calendar | internal/services/insights/period_matrix_di5_test.go — TestDI5CalendarMatrix; completed months/every MTD day2023–2027, spring/fall DST and actual boundary-row inclusion |
| M06 S2/P2 forecast | same period_matrix file — TestDI5ForecastNoBorrowing; forecast_bounds_di5_test.go — TestDI5CivilNowAndForecastBounds; 297 matrix combinations, LA late civil day, future transaction suppression, early end/no borrowing, one-day filter |
| M07 S2 coverage | internal/services/insights/period_coverage_di5_test.go — TestDI5CivilCoverageBoundaries;81 enclosure combinations repeated with nonmidnight bounds; suppression/empty/invalid/one-day coverage |
| M08 P2 rendered periods | internal/templates/period_boundaries_di5_test.go — TestDI5RenderedBoundaries; final insights-trends-partial with actual GroupIDs binding;31vs28 clamp/evidence caveat, -50/30/-80, zero/tiny-credit forecast/no negative zero |
| M09 P2 consumers | internal/handlers/dashboard/period_consumers_di5_test.go — TestDI5DashboardConsumers;11 route byte comparisons, frozen Feb15 clock, January refund -50, prior-year999 excluded and CSV; JSON fallback is explicitly NOT rendered UI proof |
| M10 S3/P3 money | internal/handlers/dashboard/reporting_di5_test.go — TestDI5RenderedResiduals, TestDI5MonthlyRenderedResidual, TestDI5MoneySurfaces; integer printed cents, four two-month residual fixtures, exact zero rows/±.01 adjustment, four full/partial/detail/month/CSV tuples/no-plan guidance |
| M11 S3 MCP | internal/services/mcpsvc/spend/grouping_di5_test.go — TestDI5GroupingInvariant;16 split/topN/signed fractional cases, period anchors, monthly adjustment reconciliation/no negative zero |
| M12 P3/A3 browser | DI5-dashboard-browser.cjs; every series/label/value cell of exactly five charts including target shape, before and after actual HTMX replot; scoped headers; open-disclosure page overflow; actual Tab/ArrowRight when wrapper overflows; all five dialogs forward/back Tab/Escape/return; focused date/link |
| M13 P6 saved files | cmd/server/saved_data_di5_test.go — TestDI5SavedDataUnchanged; synthetic t.TempDir data and independent backup/import directories; nonempty inventory and SHA256 unchanged after three actual assembled MCP refresh calls plus mounted route reads |
| M14 P6/A6 refresh | DI5-refresh-browser.cjs; fresh actual rendered HTML/CSS with only refresh script active; exact URL/query/hash, clean reload/no loop, actual dirty input/refusal, keyboard dismissal/status/focus, restart, acceptance/clean signal in six states. Confirmed focus obstruction BLOCKS acceptance; exact accepted-baseline comparison is diagnostic only |

Additional DI4 promotions, consolidated without the already-durable uncapped
wording probe:
- internal/handlers/insights/http_links_di5_test.go:
  TestDI5HTTPFindingsLinks follows EVERY actual rendered finding URL through
  Explorer and checks its transaction hash; TestDI5GroupingLinksAndPartials
  follows actual Major Expense IDs (not label-as-ID), merchant links and all
  partials with selected ranges; TestDI5RenderedFractional checks exact signed
  aggregates and independently rounded grand recurring estimates.
- internal/handlers/insights/estimate_sums_di5_test.go:
  TestDI5RenderedEstimateSums parses printed group cells and headers into cents
  for all three groups, with payment ties2.675/.005 and monthly2/annual12 cents.
- internal/handlers/insights/pricecreep_di5_test.go:
  TestDI5CreepTieMedianAndHistory checks real hash tie, actual27 vs median20,
  suppression/transfer exclusions, hash+type dedup and full-history anchoring.
- DI5-findings-browser.cjs uses the same HTTP test's optional temporary
  findings.html, not another duplicate fixture test: five preview/six remaining,
  Enter/Tab/Space, open/closed AX parity and all six viewport/theme states.

Already-durable DI1 exact bank/UI/MCP, DI2 year/period cases, DI3 Label-in-Name,
DI6 route/VM restart cases and DI4 recurring limit wording were retained without
duplicate promotion. Existing DI3-label-name.cjs was freshly executed (explicit
axe rule plus Enter/Space filechooser selecting no files), DI5.1/label-name.json.

## Final integration and execution

cmd/server/same_date_di5_test.go — TestDI5SameDateEdgeMatrix:
historical/custom/refund/incomplete month/no history/empty selection/empty data,
actual Dashboard/full KPI partial/Insights/HX money and selected ranges;
assembled MCP summarize_spending exact same net and get_trends same dates,
history and unavailable historical forecast. Missing-plan actuals/What-If
guidance is separately exercised in TestDI5MoneySurfaces.

DI5-integration-browser.cjs reverified synthetic-demo July1–31 vs June1–30:
both pages net $5,757.29, history available, no current-month forecast;
Home Maintenance & Appliances1199 vs300 (+899.00), Shopping & Household
Supplies201.15 vs92.84 (+108.31), Cash Spending100 vs0 (+100.00).
August28 reference:22 recurring series,3 subscriptions (Netflix, Spotify,
Cloud Storage),7 bills,12 other. These are fixture/demo checks, not production
rules. Actual desktop/mobile navigation and HTMX preserve dates and focus.
Six viewport/theme runs completed.

Final command vectors, cwd, timestamps and exits are preserved in
DI5.1/verification/commands.json, with individual full logs:
- rtk proxy go build -buildvcs=false ./... — exit0.
- rtk proxy go vet ./... — exit0.
- rtk proxy staticcheck ./... — exit0.
- rtk proxy go test -count=1 ./... — exit0.
- rtk proxy go test -count=1 -v ./internal/services/insights
  ./internal/handlers/dashboard ./internal/handlers/insights ./internal/templates
  ./internal/services/mcpsvc/spend ./internal/services/mcpsvc/ledger ./cmd/server
  -run '^TestDI5' — exit0.
- rtk proxy make COMMIT=di5-frozen css-verify — exit0; CSS current.
- rtk proxy bash smoketest/gate/run_tests.sh in the supplied agents2 worktree
  — exit0, ALL PASS.

DI5-verify.cjs is the replay runner. COMMIT override avoids Makefile git
inspection; build disables VCS stamping. Browser command outcomes are in
DI5.1/browser-execution.json. Green tests/axe are NOT product acceptance given
the confirmed focus blocker.

Author/harness corrections during this attempt: imported regex S1007 corrected
without changing behavior; final trends fixture supplied required GroupIDs;
mobile navigation explicitly opened; URL dates compared by parsed parameters;
scroll wrapper asserted to exist; post-replot parity added. No checker result
was rewritten or production behavior changed to make tests pass.

## Accessibility and visual limits

DI5-a11y-browser.cjs audited nine navigation routes in both themes:
18 captures, zero WCAG-tagged axe violations/page errors,20 incomplete rule
results/458 node occurrences retained. Zero axe does not detect/excuse the
confirmed focused-state obstruction. Unlabelled Explorer #search-input, Major
Expenses #major-expenses-search and seven What-If quick-add inputs match the
supplied accepted-baseline observations; original-run attribution is not
independently claimed by this worker. Contrast/table-header incompletes remain
unresolved. No native screen-reader/speech-recognition or full validation audit
is claimed. No prolonged reduced-motion certification.

Dashboard six full-page captures and readable Insights comparison/recurring/
supporting viewport captures were visually inspected. Content wraps and remains
inside the page; narrow recurring tables intentionally scroll horizontally.
Full extremely tall Insights captures could not be displayed by the image
viewer; readable viewport captures were produced and inspected instead.
Exact source hashes, actual DOM/AX/axe records and screenshots are retained.

## Reproduction and handoff

Browser prerequisites: cached Playwright (DI5_PLAYWRIGHT), Chromium
(DI5_CHROME), axe (DI5_AXE); defaults are explicit in scripts. No downloads.
Each script rejects non-loopback and8080/8081; fresh contexts only.
Use a fresh immutable baseline+DI4.2+DI5 overlay and copied synthetic data,
with independent data/import/backup paths and dedicated server port.
Set DI5_BASE, DI5_OUTPUT; a11y/refresh/provenance/verify also use DI5_SOURCE_ROOT.
findings script requires DI5_FINDINGS, produced by TestDI5HTTPFindingsLinks with
DI5_ARTIFACT_DIR pointing to an existing temporary directory.
verify requires DI5_AGENTS_ROOT. provenance requires DI5_LIVE_ROOT and the
documented immutable source-hash inventory.

Do NOT use DI5_REFRESH_BASELINE comparison as acceptance. Strengthen/preserve
the strict obstruction assertion against the scoped production correction and
the independent all-scripts-active probe. Lead has requested a11y FAIL and will
authorize the correction and fresh review. Named tests/a11y/second verdicts,
gate check DI5, done/stats, reviewed commit and deployment remain lead-owned.
No further suites were started after the freeze instruction.

Evidence is preserved under .swarm/DI-run/reports/DI5.1; complete path inventories
are .swarm/DI-run/manifests/DI5.1.files and .swarm/manifests/DI5.1.files.
Runtime helper failure (legacy-Landlock incompatibility) was handled only via
scoped approval-reviewed RTK/apply_patch/read-only screenshot calls; no
auto-review rejection or bypass. No worker-owned server remains running.
