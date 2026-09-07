# DI1 attempt 2 — implementation evidence

Worktree: /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights.
Cumulative baseline: 69e484a6736cbc05d57fe76c9345c4e8b15c2194
(confirmed by rtk proxy git rev-parse 69e484a).
Worker evidence only; fresh named reviews and acceptance remain with the lead.

## Authorized correction

Read task-1-brief.md, ACCESSIBILITY.md, and both DI1.1 checker verdicts.
The lead conceded F1/F2. The original attempts retained both rows but failed
to recognize their original bank descriptors. The simpler original worker
positive fixtures did not exercise the exact bank strings.

The only production change since attempt 1 is two entries in the existing
ClassifyRecurring evidence lists:
- Subscription phrase: cloudbox storage plan.
- Bill phrase: autoloan finance bill pmt.

Existing punctuation normalization and word-boundary matching recognize:
- Description="Cloud Storage";
  OriginalDescription="CLOUDBOX*STORAGE PLAN CLOUDBOX.COM CA";
  Category="Electronics & Software" -> subscription.
- Description="AutoLoan Finance";
  OriginalDescription="AUTOLOAN FINANCE BILL PMT";
  Category="Auto Payment" -> bill.

No new category-wide rule, description fallback, cadence rule, or precedence
change. OriginalDescription remains authoritative when present; edited display
aliases cannot override an unknown original merchant. Retail and conflicting
subscription/bill evidence still resolve to other.

## Durable probe promotion

Promoted the two user-specified checker files from /tmp/DI1-second.ka8SuZ:
- internal/services/insights/di1_bank_rows_test.go
- internal/handlers/insights/di1_exact_bank_parity_test.go

Kept test names, fixture strings, assertions and counterfactual controls; gofmt
is the only transformation. Verified against gofmt output of each source with
diff -u, exit 0 for both. No tree was copied.

Added TestDI1BankEvidenceGuards in classification_test.go. It exercises edited
Netflix/Cloud Storage/AutoLoan Finance aliases over unknown original evidence,
and both transaction orders for Cloudbox versus Target and Cloudbox versus
AutoLoan conflicting evidence. Existing negative and Major Expense rename tests
remain in the uncached focused suite.

## Caller inspection / interfaces

Before production edits:
- rtk proxy gopls references internal/services/insights/classification.go:13:6
  Exit 0: direct consumers are IsSubscription and DetectRecurringAt in
  services/insights/recurring.go, MCP recurringRows in
  services/mcpsvc/spend/recurring.go, and the existing classifier test.
- rtk proxy sh -c 'rg -n "ClassifyRecurring\(|IsSubscription\(" internal --glob "!*_test.go"'
  Confirms those production call sites.
- Initial :14:6 query resolved local txns instead of the function; corrected to
  :13:6 before editing. No callable LSP tool was available; CLI semantic references
  plus rg were used.

No interface changes in attempt 2. Classification/reason fields, IsSubscription
wrapper, UI grouping and MCP filtering use the same producer documented in DI1.1.
Full consumer enumeration and cumulative interfaces remain in DI1.1.md.
No changes to annotation, detector cap removal, estimates or templates this attempt.

## Actual red/green commands

All run from the stated worktree; fixtures are synthetic and MCP uses only
in-memory SDK transports.

1. Before fix:
   rtk proxy go test ./internal/services/insights ./internal/handlers/insights -run '^TestDI1AdversarialExactBank' -v -count=1
   Exit 1.
   Service: cloud storage classification=other want subscription;
   autoloan finance classification=other want bill; each annual=300.00.
   UI: subscriptions=0 bills=0 other=2 monthlySubscriptions=0.00 totalAnnual=600.00.
   MCP unfiltered: both rows other; subscriptions_only=true count=0, payments=[].
   These are behavioral failures, not compilation or fixture failures.

2. Guard baseline before fix:
   rtk proxy go test ./internal/services/insights -run '^TestDI1BankEvidenceGuards$' -count=1
   Exit 0. Alias and conflict safeguards retained from attempt 1.

3. After the two evidence additions:
   rtk proxy go test ./internal/services/insights ./internal/handlers/insights -run '^TestDI1AdversarialExactBank|^TestDI1BankEvidenceGuards$' -v -count=1
   Exit 0.
   Service: cloud storage subscription, autoloan finance bill; each annual=300.00.
   UI: subscriptions=1 bills=1 other=0 monthlySubscriptions=25.00 totalAnnual=600.00.
   MCP: bill is_subscription=false, subscription is_subscription=true;
   subscriptions_only=true returns count=1, cloud storage, annual_cost=300.
   Guards also succeeded.

4. Formatting:
   rtk proxy gofmt -w internal/services/insights/di1_bank_rows_test.go internal/handlers/insights/di1_exact_bank_parity_test.go internal/services/insights/classification_test.go
   Exit 0.

5. Exact probe preservation:
   rtk proxy bash -c 'diff -u <(gofmt /tmp/DI1-second.ka8SuZ/internal/services/insights/di1_bank_rows_test.go) internal/services/insights/di1_bank_rows_test.go && diff -u <(gofmt /tmp/DI1-second.ka8SuZ/internal/handlers/insights/di1_exact_bank_parity_test.go) internal/handlers/insights/di1_exact_bank_parity_test.go'
   Exit 0, no differences.

6. Final existing + new tests, after formatting:
   rtk proxy go test ./internal/services/insights ./internal/handlers/insights ./internal/services/mcpsvc/spend ./internal/templates -count=1
   Exit 0:
   ok budget2/internal/services/insights 0.008s
   ok budget2/internal/handlers/insights 0.232s
   ok budget2/internal/services/mcpsvc/spend 0.727s
   ok budget2/internal/templates 1.131s
   Includes exact bank probes, negative/rename/conflict guards, >20 retention
   and parity, existing cadence/estimate tests, and real Go template rendering.

7. Required build:
   rtk proxy go build ./...
   Exit 0, no output. Executed in the shared worktree with DI6 frozen; no DI6
   files were edited or copied. This is a build result, not a DI6 review.

## Scope, concerns and handoff

- No broad classifier overhaul. A small evidence list remains conservative;
  unknown evidence stays visible as other.
- Review-reception, TDD and verification skills guided reproduction before repair.
- No UI layout changes this attempt. Existing render tests succeeded; no browser
  screenshot/theme audit or live endpoint access performed.
- No git-state changes, subagents, server starts/restarts, live data, whole-tree
  copies, verdict writes, ledger/spec changes, or DI6-territory edits.
- DI1 is frozen at this handoff for fresh independent review; worker makes no
  acceptance claim and will not continue edits without reopening authorization.
- Manifest is cumulative versus 69e484a: the prior 14 DI1 paths plus these two
  promoted probes and attempt-2 report/manifest. It includes prior worker evidence,
  excludes checker-owned verdicts and all foreign DI6/run/planning files.

## Cumulative DI1 files

- internal/handlers/insights/handlers.go
- internal/handlers/insights/handlers_test.go
- internal/handlers/insights/recurring_di1_test.go
- internal/handlers/insights/di1_exact_bank_parity_test.go
- internal/models/insights.go
- internal/services/insights/classification.go
- internal/services/insights/classification_test.go
- internal/services/insights/di1_bank_rows_test.go
- internal/services/insights/recurring.go
- internal/services/insights/recurring_test.go
- internal/services/mcpsvc/spend/recurring.go
- internal/templates/render_insights_subscription_test.go
- internal/templates/render_recurring_di1_test.go
- web/templates/pages/insights.html
- .swarm/DI-run/reports/DI1.1.md
- .swarm/DI-run/manifests/DI1.1.files
- .swarm/DI-run/reports/DI1.2.md
- .swarm/DI-run/manifests/DI1.2.files
