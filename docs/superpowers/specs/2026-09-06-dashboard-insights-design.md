# Dashboard and Insights: household decisions

Status: approved by user on 2026-09-06 ("Yes continue"), including task tiers
and seven-day freshness policy. Baseline: 69e484a; branch codex/dashboard-insights.
Scope approved in conversation: commit UIF1 first, then plan accuracy fixes,
Dashboard improvements, and Insights restructuring using the synthetic demo.
Accessibility constitution: repository ACCESSIBILITY.md, points 1–17, unchanged.

## Product purpose

For a retired couple, Dashboard answers “Where do we stand for this period?”
Insights answers “What changed, and what deserves review?” What-If remains the
place for long-term projections and retirement sustainability. A cash shortfall
is not evidence of an actual investment withdrawal or an unsafe retirement.

Keep Go templates, HTMX, existing chart library and semantic color tokens.
Preserve the current visual identity, light/dark parity, keyboard interactions,
and the corrected subscription badges. No new framework or chart library.
Use neutral, factual language: “spending exceeded recorded income” rather than
“bad savings”; “estimated” and “detected” where observations are inferred.

## Alternatives and recommendation

1. Cosmetic polish alone is fast but leaves misleading subscription and date
   semantics. It does not satisfy the information needs raised by the user.
2. Recommended: fix shared information semantics, then reorganize the two
   existing pages around household questions. Keep calculations centralized.
3. A new unified financial cockpit would require new navigation and migration
   decisions. Defer it; the two existing pages can serve distinct purposes.

## Existing evidence and boundaries

- insights/recurring.go IsSubscription classifies monthly/yearly/quarterly and
  ongoing payments as subscriptions unless description exclusions catch them.
  Demo travel, auto loan, cash and fuel demonstrate false positives.
- insights/trends.go SpendingVelocity reads time.Now for month/day counts even
  when its input is a historical range. The demo ends August 28 but displays
  September days remaining. Do not claim the forecast describes August.
- DetectRecurringAt already supports historical truncation and freshness.
  Reuse it. The get_recurring tool calls IsSubscription; get_trends calls
  SpendingVelocity but deliberately omits wall-clock forecast fields.
- MajorExpenseName is a category label, not merchant identity. Several unrelated
  merchants may share it. Keep merchant names visible and category secondary.
- Dashboard already has signed spending, plan targets, healthcare variance,
  account anchors, charts, and cash flow. Reuse those sources; no new browser
  arithmetic or changes to refund/transfer accounting.

## Page inventory and information order

### Dashboard — overview

1. Shared period controls, explicit range, latest transaction date, and data
   freshness. Say “transactions through”, not “complete through”: transaction
   dates cannot prove every account has been imported.
2. Four primary figures: recorded income, net spending, cash-flow balance, and
   spending versus plan. Every amount uses the same selected period. Monthly
   equivalents, if retained, have an explicit secondary label.
3. Explain a negative balance as “Spending not covered by recorded income”.
   Show a positive balance as “Recorded income above spending”. Never call the
   difference a measured portfolio withdrawal. Keep transfers excluded.
4. Budget details: living, healthcare, and separately modeled costs with target
   provenance. Do not invent property-tax targets or add excluded costs twice.
5. Accounts with anchor dates; stale balances remain dated observations.
6. Spending by Major Expense and monthly budget history, followed by links to
   relevant Insights findings and their transactions. Remove redundant headline
   tiles only when their information is preserved elsewhere.

### Insights — investigation

1. Same period and freshness context as Dashboard.
2. “What changed” with current/prior ranges, total dollar difference and the
   three largest contributing categories. Rank by absolute dollar difference,
   stable category-name tie break. No percentage-only findings.
3. “Review these transactions”: existing unusual-amount and price-creep evidence,
   deduplicated by transaction hash plus finding type. Show at most five in the
   preview, full count and a route to all. Empty means no detected findings,
   not a guarantee of financial health.
4. Detected subscriptions, recurring bills, and other recurring spending as
   distinct groups. Merchant first, Major Expense category second. Estimates
   of monthly/annual cost are not actual selected-period spending.
5. Supporting trends, income patterns and detailed tables below the findings.
   Upcoming payments are estimates, with evidence date and confidence, not
   confirmed obligations. Historical views say “expected after [reference]”.

## Shared accuracy rules

### Subscription classification

Use positive evidence for a subscription (known subscription service or a
subscription-specific original transaction category). Recurrence by itself is
insufficient. Unknown series remain “other recurring spending”, not discarded.
Recognize Netflix, Spotify and Cloud Storage in the demo; do not use demo-only
household labels as production classification rules. Mortgage, loans, tax,
insurance, utilities and internet access belong to recurring bills; groceries,
cash, pets, fuel, dining, travel and home maintenance are not subscriptions.
Expose the same classification and reason to UI and get_recurring; preserve
the existing is_subscription boolean as a derived compatibility field.
Do not classify solely from a user-renamable Major Expense name. Do not merge
different merchants because they have the same category. At the design baseline
the detector capped at 20; accepted DI1 removed that cap. Keep all detected
series available and describe totals and any presentation limits truthfully.

### Periods, comparisons and freshness

Use calendar dates in the app timezone and an injected clock in tests. Derive
an inclusive selected period once. For a completed calendar month, compare the
prior calendar month. For current month-to-date, compare the same elapsed days
of the prior month (clamp to its last day and explicitly label differing day
counts). Other ranges compare the immediately preceding equal-length window.
Missing historical coverage produces “Not enough history to compare”, not zero.
The first/last transaction dates are evidence bounds, not import-completeness
proof. Label comparison completeness limitations.

Show “Latest transaction: [date]”. Approved policy: after seven calendar days,
show a neutral stale-data notice and suppress the current-month forecast.
Historical ranges never show a forecast for the wall-clock current month.
Current-month forecast is permitted only when the selected range includes that
month, the latest transaction is in it, data is no more than seven days old,
and at least seven elapsed calendar days are observed from month start.
Label “Estimated [month] total through [reference date]”; denominator uses
calendar elapsed days, not the dates of the first/last purchase. Otherwise
show the unavailable reason. This is a pace estimate, not a bills forecast.

Signed net spending, refunds, transfer exclusion, zero baselines and negative
zero protections remain intact. Reuse ChangeCell and shared formatters. Round
component display values through one source before deriving any displayed
sum/difference. Apply the same reference period and classifications in every
UI partial, chart, export and connected-tool consumer; enumerate them per task.

### Empty and uncertain states

No transactions: import guidance, no green verdict. No plan: show actuals and
link to What-If; do not invent a target. No balance anchor: unavailable, not $0.
Unknown income type: recorded income, not assumed Social Security or withdrawals.
No recurring evidence: no detected payments, not zero future commitments.

## Tiered tasks and acceptance

| ID | Deliverable / likely files | Tier and checks | Acceptance |
|---|---|---|---|
| DI1 | Recurring classification; internal/services/insights/recurring.go, internal/models/insights.go, handlers/insights/handlers.go, services/mcpsvc/spend/recurring.go and tests | 2 tests,second: displayed monetary classifications cross consumers | Demo false positives excluded; Netflix/Spotify/cloud retained; unknowns retained separately; merchant identity and sum scope preserved; UI/tool parity fixtures |
| DI2 | Shared period/forecast context; models/insights.go, services/insights/trends.go, handlers/insights, dashboard handlers, MCP trends adapter and tests | 2 tests,second: time boundaries change monetary comparisons | Frozen Sept 6/Aug 28 demo has no September forecast; historical, partial, stale, empty, leap-year and refund fixtures; explicit ranges and consistent partials/tool semantics |
| DI3 | Dashboard overview; handlers/dashboard, models/dashboard.go, services/metrics adapters, pages/dashboard.html, components/kpis.html and dashboard.js | 2 tests,a11y,second: financial presentation | Same-period amounts, no invented withdrawals, target provenance retained; data gaps visible; cent-level rendered reconciliation; keyboard and both themes at 390/768/1440px |
| DI4 | Insights investigation layout; handlers/insights, pages/insights.html, insights JS and associated components | 2 tests,a11y,second: ranked monetary findings | Deterministic top contributors and evidence links retain date/category filters; merchant/category distinction; no overlap/double-counted totals; empty states; same viewport/theme checks |
| DI5 | Integration and demo acceptance; focused regression fixtures and run evidence | 3 oracle + tests,a11y,second: escalated after two failures (see Rulings) | Both pages agree for identical filters; full response and HTMX parity; synthetic edge cases; no real-data writes; refresh focus lifecycle; gate acceptance per task and final run gate |
| DI6 | User-added force refresh; server-scoped MCP refresh_pages, shared page listener and focused tests | 2 tests,a11y: UI refresh coordination without financial writes | Clean visible tabs reload on request; hidden tabs check on return; unsaved edits require confirmation; server instances stay isolated; restart/revision and saved-data-preservation checks |

Order: DI1 then DI2, DI3 and DI4 after the shared contracts settle, DI5 last.
DI1 and DI2 overlap models/handlers; serialize them. Do not run workers in a
shared editing territory. Before dispatch record exact manifests and incoming
call/reference analysis. Copy existing critical.globs and test.globs; a matched
production critical path escalates automatically and requires an oracle before
worker dispatch. Do not edit storage, retirement engine or migrations in this run.

## Release and review

UIF1 is a separate commit. Implement the new work in an isolated codex branch.
Use a dereferenced synthetic data copy with independent data/import/backup paths
for write tests; never copy a live-data symlink unchanged. Demonstrate on :8081.
No real-plan sync, data migration, merge or live-server replacement in Phase 0.
Keep demo guardrails and $1M household assumptions unchanged.

Each task needs its named non-author checks and gate.sh check before acceptance.
The run requires gate.sh done, gate.sh stats (reported verbatim), full-site a11y
and the agents2 contract smoketest. Record catches with mechanism in Rulings.

## Review decision

Approve the information order, conservative classification, seven-day freshness
policy, comparison rules and tiers together before implementation. Existing
ACCESSIBILITY.md is the second Phase 0 document and remains authoritative.

## Rulings

2026-09-06 DI4 evidence anchor: price-creep preview uses the latest eligible
transaction in the full-history detector group, then applies the selected
date filter. Do not attach later group evidence to an earlier purchase merely
to fit the selected dates. Distinguish its recent median from the actual
transaction amount. Cost: some historical previews omit price-creep groups;
their clearly labeled full-history supporting evidence remains accessible.

2026-09-06 DI4 reporting preflight: use existing ChangeDisplay and DI2
periods for the overall change and ranked contributors, with grouping scope
explicit rather than pretending top-three contributions fully reconcile.
Recurring estimate row money shares MCP precision; displayed estimate totals
sum displayed rows, with separately rounded month/year values disclosed once.
Raw detector/compatibility values stay intact. Cost: occasional penny
differences from raw aggregate estimates, explicitly identified; actual-period
anchors continue to follow DI3's separate raw-aggregate rule.

2026-09-06 interim demo release: user could not see the new Dashboard on
:8081 because only DI6 was deployed. After DI3 named checks and gate pass,
publish an isolated Dashboard demo build before DI4/DI5 finish. Preserve all
synthetic data and the real :8080 process, and retain rollback binaries.
Cost: an extra reversible demo release; this does not accept the whole run.

2026-09-06 DI3 attempt 1: CONCEDE accessibility checker F1. The new import
drop-zone accessible name omitted its visible "click to browse" label,
violating WCAG 2.5.3. Mechanism: named accessibility checker's focused
rendered label-content-name-mismatch probe in both themes; default axe tags
missed this rule. Correct only the label relationship and add a durable
regression. Money, layout and existing chart palette remain unchanged.

2026-09-06 DI3 rounding contract clarification (worker preflight, not a failed
attempt): two 0.004 monthly amounts round individually to 0.00 while their
period aggregate rounds to 0.01. Preserve rounded raw period anchors; derive
cash flow from rounded income/spending. A complete displayed monthly breakdown
reconciles via an explicit rounding adjustment, never fabricated transactions,
month reassignment, or grouping-dependent period totals. Add the corresponding
MCP adjustment metadata and correct its exact-sum promise; truncated category/
merchant breakdowns must disclose their scope and rounding limitations.

2026-09-06 DI2 attempt 1: CONCEDE year-control history coverage bug. The
new Dashboard override compared raw timestamps with civil-date boundaries,
so a noon transaction on the first prior day falsely hid the comparison.
Mechanism: lead preflight candidate, independently reproduced by second
checker in adapter and rendered output; midnight control passes and legacy
comparison confirms this is introduced. Preserve the existing year AddDate
range policy. Use one shared civil-date coverage predicate and promote the
checker time/zone and rendered probes to durable regression coverage.

2026-09-06 run metadata: the first escalation scan rejected the ledger's
uncommented column header as a bad tier. Mechanism: gate parser. Lead corrected
the header to a comment and reran the scan before any task acceptance. This
was a lead bookkeeping defect, not an implementation/checker failure.

2026-09-06 reporting preflight: MCP spend.round2 currently uses math.Round
while the renderer's shared precision helper is models.RoundToCents. DI3's
approved cross-consumer monetary-display requirement includes minimal reporting
adapter alignment and fractional-cent rendered reconciliation fixtures. This
is a lead preflight risk, not a checker catch or a claim that an accepted
DI1 test established general fractional-cent reconciliation.

2026-09-06 DI6 attempt 1: CONCEDE generated CSS drift. The new script's
outline token makes the pinned stylesheet build add one utility. Mechanism:
lead full integration check, independently reproduced by primary checker;
worker class-presence audit did not establish build freshness. Grant DI6
ownership of web/static/css/tailwind.css regeneration for the correction.
Primary checker also proved the restart regression test survives removal of
epoch handling although the actual implementation works. Promote its pending
notice/new-epoch probe to durable coverage; no behavior redesign is needed.

2026-09-06 DI1 attempt 1: CONCEDE second checker F1/F2. Exact synthetic bank
descriptions for Cloud Storage and AutoLoan Finance were not recognized by the
shared classifier. Primary fixtures used friendly names and missed the original
bank-description path. Mechanism: second checker executable UI/MCP probes,
independently reproduced by the primary checker.
Promote both exact-row probes to durable regression tests in attempt 2.

2026-09-06 DI6 addition approved by user: add server-scoped refresh_pages MCP
tool, preserving unsaved form input and never signaling the other server.
Tier 2 tests,a11y; exact scope is .swarm/DI-run/DI6-brief.md. DI5 includes
integration checks for refresh and demo-instance isolation.

2026-09-06 user addition: demonstrate assistant help with the demo household.
DI5 includes direct demo MCP read parity and data-path verification; see
docs/demo-assistant.md. Installed connector remains pointed at real data.
Source inspection confirmed the classification default and wall-clock/historical
forecast mismatch; these are design inputs.

2026-09-06 DI4 attempt 1: CONCEDE F1. Primary and second checkers independently
proved full, HX and standalone recurring views render all 22 series while
claiming a nonexistent 20-series detector cap. Mechanism: lead preflight,
confirmed by primary and second executable rendered-output probes; a11y also
reported the wording as a content observation. Retention and accessibility pass.
Attempt 2 corrects both user-facing claims and the author report, preserving
uncapped retention, and promotes the 22-series rendered regression. No detector
or financial behavior changes are authorized by this correction.

2026-09-06 deployment authorization: user explicitly requested 'When its done
go ahead and merge it and deploy to the main site'. After independent reviews,
final integration and mandatory gates pass, lead may commit, merge/push and
deploy the reviewed build to main. Preserve real data/settings, retain a
rollback binary, and verify health and build identity after deployment. No
demo fixture may replace main data. Workers/reviewers do not deploy.

2026-09-06 DI5 attribution clarification: final integration covers DI1–DI6.
An observation matching an accepted DI3/DI4 snapshot establishes that DI5 did
not introduce it, but does not establish that it predates the whole run.
Potential run-introduced failures require attribution to original69e484a or
another explicit approved scope ruling; distinguish real user-visible defects
from probe artifacts before adjudication. No production fix is authorized
solely by a center-point focus-obstruction diagnostic.

2026-09-06 DI5 attempt1: CONCEDE checker-a11y F1. The DI6 fixed refresh
notice fully covers DI3 detail-link focus at390/768 in both themes. Mechanism:
worker integration probe exposed the symptom; lead challenged attribution to
an already-modified baseline; named accessibility checker independently proved
full coverage with all scripts active and original/pre-DI3 counterfactuals.
This is a run-introduced integration regression, not an accepted legacy issue.
Attempt2 may change only shared refresh notice presentation/lifecycle tests
and necessary generated CSS, preserving every financial/data producer. Render
the notice in normal page flow near the main heading rather than overlaying
controls. Preserve current focus and keep it visible after insertion without
focusing the notice; preserve confirmation, dismissal/AT parity, dirty edits,
server isolation, epoch/revision behavior and URL. Promote strict all-scripts-
active keyboard obstruction regression; never allowlist the four failures.
Same Tier2 tests,a11y,second; all three fresh lanes and final gates required.

2026-09-06 DI5 attempt2: CONCEDE checker-a11y F2. Main HTMX replacement
retains/remounts the same notice nodes but loses focus from Keep editing to
BODY. Old script retains that focus in the exact counterfactual. Mechanism:
named accessibility checker's actual-render main-focus.cjs; existing green
node-identity tests and primary review did not test focused notice ownership.
Both first failures occurred at Tier2. Required escalation now raises DI5 to
Tier3. The next dispatch is attempt3, the last permitted attempt at any tier;
if it fails, halt and report to the user. Do not silently retry beyond it.

These two focus failures reveal a lead/contract defect: visibility and node
identity were specified separately without a complete focus-transition matrix.
Before the final dispatch, rewrite that contract and author/calibrate an
executable oracle on the failing frozen source and a passing throwaway
prototype. It must distinguish outside-input focus, each notice action's
focus, unrelated partial swaps, main inner/outer replacements, dismissal,
epoch reset, and existing refresh/visibility/data-preservation guarantees.
Preserve all earlier evidence. No acceptance allowlist or overwritten verdict.

2026-09-06 DI5 final-attempt contract rewrite (lead/spec correction):
DI5.3-brief.md now governs focus ownership across arrival, actual subtree
removal/remount and notice dismissal/reset. Exact surviving button identity,
no theft from a newly focused control, cancelled/delayed-swap behavior and a
meaningful fallback after the original input is replaced are explicit. Shared
refresh consumers are all nine navigation pages; financial producers are not
changed by this repair. The lead-authored executable tier3/DI5/accept.sh combines
the real-page transition matrix with the strict original obstruction test and
existing layout/lifecycle/VM tests. Both-end calibration precedes dispatch.
The first prototype calibration exposed an oracle fixture error: serializing a
cloned input retained its original value attribute, not its edited value. The
synthetic replacement now explicitly carries the expected unsaved value; the
whole corrected oracle is re-calibrated at both ends. This is lead/oracle
calibration, not a worker attempt or permission to waive actual data loss.

2026-09-06 DI5 attempt3 HARD STOP. The worker's durable real-browser GREEN
run found remaining fallback focus loss: dismiss/reset from either notice
action after replacement of the original editing input leaves BODY focused.
Mechanism: worker's promoted focus-ownership regression, not an independent
PASS review. Same-button remount and cancelled/delayed non-theft cases now pass,
but that does not waive the remaining failure. This is the third failed
attempt overall (first two Tier2, third Tier3). Lead upholds the mandatory halt.
No additional production edits/retry, acceptance, commit, merge or deployment.
Finish only current failure evidence and stop the isolated test server; user
direction is required before any new repair scope/attempt can be authorized.

Final F3 evidence: worker durable browser regression228 assertions/24 failures;
unchanged lead-authored oracle750 assertions/six fallback failures, exit1,
no ORACLE PASS. Mechanism attribution: worker's durable test caught it first,
oracle independently specified assertions confirmed it. Main fallback loses
focus when its temporary tabindex is immediately removed. Full author report
and immutable failed source hashes are retained in reports/DI5.3.md. Dedicated
test servers stopped; both live sites retain their prior healthy builds.

2026-09-07 user explicitly answered Yes to reopening ONLY the remaining focus
fix after the hard stop. This authorizes one narrowly scoped additional
submission, DI5 attempt4, retaining Tier3 and all prior failed evidence. It is
not an unrestricted retry loop or a reset of the ledger's attempt history.
Only the fallback in page-refresh.js and targeted regression tests may change.
The fallback must remain focused when the notice is removed after the original
editing control has been replaced; any temporary tabindex is cleaned up only
after focus leaves, preserving a pre-existing tabindex and not clobbering an
attribute later changed by another owner. No change to money, templates, CSS,
notice placement/copy, refresh semantics or data/settings. The existing oracle
is unchanged and retains its calibrated both-end evidence; attempt3's exact
source is a further confirmed red end. The reopened scope governs acceptance;
unrelated discoveries are backlog observations, not scope expansion. If this
submitted correction fails, halt and report again. The user's earlier merge/
main-site deployment authorization remains contingent on complete acceptance.

2026-09-07 pre-implementation contract catch (fresh worker, not failed attempt):
DI5-focus-ownership.cjs line79 required absent tabindex at the instant fallback
main owns focus, conflicting with cleanup-on-blur. Lead read the complete test
and upholds the contradiction. Authorized correction: expect temporary -1 while
main is focused and separately require removal after blur; retain exact focus,
saved-value, region/status and all other assertions. Old evidence stays intact.
No change to the independent calibrated oracle. Mechanism: fresh worker's
contract review caught this test-level assumption before a production edit.
