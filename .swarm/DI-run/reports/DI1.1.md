# DI1 attempt 1 — implementation evidence

Task: DI1 only. Worktree: /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights.
Approved contract: .superpowers/sdd/2026-09-06-dashboard-insights/task-1-brief.md,
docs/superpowers/plans/2026-09-06-dashboard-insights.md and its referenced design;
ACCESSIBILITY.md points 1–17 read before implementation.
This is worker evidence, not a checker verdict or acceptance decision.

## Interfaces and behavior

- Added insights.ClassifyRecurring(rp models.RecurringPayment) (string, string):
  classification and human-readable reason. Values are models.RecurringSubscription
  ("subscription"), models.RecurringBill ("bill"), models.RecurringOther ("other").
- Preserved insights.IsSubscription(rp models.RecurringPayment) bool, derived
  exclusively from ClassifyRecurring.
- Classification uses each original Transaction's OriginalDescription when present,
  otherwise Transaction.Description, plus its transaction Category. With no
  Transactions, RecurringPayment.Description is the compatibility fallback.
  MajorExpenseName and cadence never supply subscription evidence.
- A small known-service list and exact subscription-specific categories provide
  positive evidence. Existing retail/bill keywords are matched on normalized word
  boundaries, with bill categories and internet/auto-loan evidence. Retail or
  conflicting subscription/bill evidence stays other. Unknowns remain other.
- RecurringPayment adds Classification and ClassificationReason JSON fields
  classification / classification_reason. DetectRecurringAt populates them for all
  results after the existing three detection passes. Its former top-20 cap is gone.
  Detection intervals, historical cutoff, freshness, average amounts and annual
  multipliers are unchanged.
- InsightsData.OtherRecurring (other_recurring) is additive. Subscriptions holds
  subscriptions; RecurringPayments holds bills. These three groups exhaust the
  detected set. TotalRecurring sums all annual estimates; MonthlyRecurring divides
  that full total by 12; MonthlySubscriptions retains its annual/12 semantics.
- MCP get_recurring adds classification and classification_reason, keeps the derived
  is_subscription boolean, and filters subscriptions_only over the full detected set.
  Its tool description now explains this contract and removes the obsolete cap caveat.
- Existing annotation continues to preserve merchant identity. Templates show
  merchant first and Major Expense second. Subscription/bill search links use the
  merchant. A minimal Other recurring spending section reuses the existing table;
  classification reasons are visible. Both insights-content and recurring-payments
  render the retained data. Final layout remains DI4.

## Before-edit consumer inspection

No callable LSP tool was exposed in tool discovery. Used gopls CLI semantic
references plus rg, with positions below referring to the pre-edit files.

Successful commands (run through rtk proxy, from this worktree):
- gopls references internal/services/insights/recurring.go:52:6
- gopls references internal/services/insights/recurring.go:219:6
- gopls references internal/models/insights.go:6:6
- gopls references internal/models/insights.go:90:6
- gopls references internal/handlers/insights/handlers.go:151:6
- gopls references internal/services/mcpsvc/spend/recurring.go:67:6
- gopls references internal/services/mcpsvc/spend/recurring.go:94:6
- gopls references internal/services/mcpsvc/spend/recurring.go:21:6
- rg -n "DetectRecurring|IsSubscription|calculateInsights|recurringRows|registerRecurring" internal --glob "!**/*_test.go"
- rg -n "RecurringPayments|Subscriptions|MajorExpenseName|recurring" web/templates/components/insights* web/templates/pages/insights.html

Limitations: initial references at handlers.go:150:6 and spend/recurring.go:73:6
were beyond the line; spend/recurring.go:111:6 and :99:6 had no identifier.
Corrected to the successful positions above before editing. A search under
internal/services/demo* found no such directory; fixtures are synthetic code built
from the approved examples, not copied household data. No incomingCalls UI tool
was available; semantic reference results and source reads established callers.

Consumer enumeration:
- IsSubscription: Insights calculateInsights and MCP spend.recurringRows, plus
  service tests. Handler now consumes detector-produced Classification; MCP rows
  and the wrapper call the same ClassifyRecurring function.
- DetectRecurringAt: DetectRecurring wrapper; Insights calculateInsights and
  RecurringPaymentsHandler (handlers/insights/handlers.go); Dashboard
  detectRecurringForDashboard (handlers/dashboard/accounts_card.go); MCP spend
  get_recurring (services/mcpsvc/spend/recurring.go); MCP ledger
  recurringForProjection (services/mcpsvc/ledger/accounts.go).
- DetectRecurring wrapper: MCP ledger recurringForProjectionDefault and tests.
- calculateInsights: Insights Page handler, shared by full page and HTMX content.
- recurringRows: get_recurring callback; registerRecurring: spend.Register.
- RecurringPayment type: the consumers above, majorexpenses.AnnotateRecurringPayments,
  accounts.Project / recurringOwnerAccount / frequencyIntervalDays, template
  fixtures and service tests. No account-service or annotation implementation edits.
- InsightsData: calculateInsights and template fixtures; template dynamic field
  consumers found with rg in web/templates/pages/insights.html.
- UI surfaces: full Insights page, insights-content HTMX fragment, subscription
  cards, bill table, added other group, standalone recurring-payments fragment.
  web/static/js/insights.js sorts existing bill data attributes; those names and
  numeric semantics remain unchanged.
- Cross-package impact of removing the cap: Dashboard and MCP ledger account
  projections receive all detected series rather than the highest-cost 20.
  Additional existing tests for these packages were run successfully.

## Red/green evidence

All commands used the stated worktree and rtk. TDD and verification-before-completion
skills guided regression-first evidence and final checks; no review dispatch.

1. Red:
   rtk proxy go test ./internal/services/insights -run TestDI1PositiveEvidence -count=1
   Exit 1. Auto Loan, Travel, Fuel, Pets, ATM Withdrawal, Dining, Home Maintenance
   and Unknown Merchant were incorrectly subscription=true, for all three Major
   Expense labels. Netflix/Spotify/Cloud Storage stayed positive controls.

2. Red:
   rtk proxy go test ./internal/handlers/insights -run TestDI1UncappedTotals -count=1
   Exit 1. Actual annual=24000 monthly=2000 subscriptions=2000;
   expected annual=25320 monthly=2110 subscriptions=10.
   This fixture has 21 higher-cost monthly series and a low-cost Netflix series.
   The final fixture names one higher-cost series Electric Company to cover bills,
   leaving 20 unknowns; hand-derived totals are unchanged.

3. Red (after shared service changes, before template changes):
   rtk proxy go test ./internal/templates -run 'TestDI1OtherRecurringVisible|TestRenderInsightsSubscriptionInitial' -count=1
   Exit 1. Other group, merchant, category, reason and amounts were absent.
   Subscription badges/full labels still preferred the Major Expense over merchant.

4. Green, run after implementation and again after formatting/import cleanup:
   rtk proxy go test ./internal/services/insights ./internal/handlers/insights ./internal/services/mcpsvc/spend ./internal/templates
   Exit 0. Final output:
   ok budget2/internal/services/insights 0.009s
   ok budget2/internal/handlers/insights 0.221s
   ok budget2/internal/services/mcpsvc/spend 0.708s
   ok budget2/internal/templates 1.138s

   New cross-consumer coverage invokes get_recurring through SDK in-memory
   transports with injected synthetic transactions: 22 complete rows, one bill,
   one subscription, 20 other rows, annual estimate 25320, monthly estimate 2110;
   subscriptions_only returns Netflix with annual estimate 120.
   Each MCP merchant/classification/reason/annual estimate matches the UI model;
   duplicates and omissions fail the test.
   Added post-implementation coverage verifies original-description precedence,
   category evidence, actual Major Expense pin annotation, word boundaries,
   conflicting evidence, and wrapper parity. These extensions were green on first
   execution; the red evidence is the behavior regressions recorded above.

   Existing cadence-only "annual domain"/"quarterly report" assertions now expect
   other; unknown "legacy gym" freshness is tested in OtherRecurring.
   The old cap test is now an exact 25-series retention test using distinct letters
   instead of numeric suffixes that merchant normalization can collapse.

5. Additional affected-consumer verification:
   rtk proxy go test ./internal/handlers/dashboard ./internal/services/mcpsvc/ledger ./internal/services/accounts
   Exit 0: dashboard 0.936s; ledger 0.335s; accounts 0.021s.

6. Build:
   rtk proxy go build ./...
   Exit 0, no output. Repeated after final Go import cleanup; exit 0.
   Completed before the DI6 concurrent-territory notice; no later broad build
   required or claimed over DI6's in-progress edits.

7. Formatting/review:
   rtk proxy gofmt -w internal/services/insights/classification.go internal/services/insights/classification_test.go internal/services/insights/recurring.go internal/services/insights/recurring_test.go internal/models/insights.go internal/handlers/insights/handlers.go internal/handlers/insights/handlers_test.go internal/handlers/insights/recurring_di1_test.go internal/services/mcpsvc/spend/recurring.go internal/templates/render_insights_subscription_test.go internal/templates/render_recurring_di1_test.go
   Exit 0. Subsequent patch separated standard-library and project imports.
   git diff --check (via rtk proxy sh -c) emitted no errors.
   Reviewed all authored production/test diffs and new files.

## Concerns and boundaries

- Classification is deliberately conservative and is not a complete merchant
  catalog. Unrecognized and conflicting evidence remains visible as other.
- Template rendering was verified through the real embedded Go renderer, including
  Unicode/fallback badges and visible unknown merchant/category/reason/amounts.
  No browser viewport/theme screenshots or live visual audit were performed;
  DI4 owns the final layout. Existing semantic classes, scoped table headers and
  keyboard-focusable scroll regions were reused; no hidden-text workarounds.
- No live dataset, configured household MCP, demo HTTP endpoint, or running app
  server was accessed or restarted. The new MCP parity probe uses only in-memory
  SDK transports. No git state changes, commits, reviewer dispatch, tree copies,
  verdicts, ledger edits or spec edits.
- apply_patch hit the documented legacy Landlock incompatibility once. Recovered
  with rtk proxy apply_patch and require_escalated, scoped to this worktree.
- Foreign files observed and left untouched include docs/demo-assistant.md and the
  pre-existing run/plan/spec files. DI6 territory acknowledged: cmd/server/main.go,
  internal/services/mcpsvc/server.go and inventory tests, internal/services/uirefresh,
  web/static/js/page-refresh.js, layouts/base.html. No overlap or tree-copy probe.

## Files created/changed

- internal/handlers/insights/handlers.go
- internal/handlers/insights/handlers_test.go
- internal/handlers/insights/recurring_di1_test.go
- internal/models/insights.go
- internal/services/insights/classification.go
- internal/services/insights/classification_test.go
- internal/services/insights/recurring.go
- internal/services/insights/recurring_test.go
- internal/services/mcpsvc/spend/recurring.go
- internal/templates/render_insights_subscription_test.go
- internal/templates/render_recurring_di1_test.go
- web/templates/pages/insights.html
- .swarm/DI-run/reports/DI1.1.md
- .swarm/DI-run/manifests/DI1.1.files

