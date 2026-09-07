# DI2 attempt 2 — F1 repair evidence

## Scope
Conceded year-history F1 only. Both attempt-one reviews reported this same defect; no additional findings were supplied. Task brief and ACCESSIBILITY.md read before edits. No templates, JS, CSS, year AddDate bounds, comparison policies, forecast rules, DI1 classification, or DI6 files changed in this attempt. No commits, git-state changes, subagents, server/live-data access, baseline mutations, ledger/spec/verdict edits, or whole-tree copies.

## Cause and interface
The year override compared raw transaction timestamps with civil-midnight bounds, incorrectly rejecting noon on the first prior day. Both callers now use:
`insights.HasCivilDateCoverage(ts *models.TransactionSet, start, end time.Time) bool`

This exported predicate uses active rows and the existing shared calendarDate normalization for range and first/last evidence dates. It returns false for empty data or zero/reversed bounds, and otherwise checks inclusive civil-date enclosure. It does not claim import completeness. BuildPeriodContext and dashboardPeriod's year override are its consumers. There is no local duplicate normalization in the dashboard adapter. Existing year AddDate bounds and HistoryReason handling are unchanged. Other exported interfaces and consumer inventory remain as documented in DI2.1.md.

## Pre-edit inspection
No callable LSP was available. CLI semantic references plus rg:
- `rtk proxy gopls references internal/handlers/dashboard/period.go:14:6` exited 0: full page, KPI partial, chart, major drilldown, KPI detail/month/export.
- `rtk proxy gopls references internal/services/insights/period.go:49:6` exited 0: dashboard/insights adapters, MCP trends, service/renderer tests.
- `rtk proxy rg -n 'HistoryAvailable =|func BuildPeriodContext|func calendarDate' internal/services/insights/period.go internal/handlers/dashboard/period.go` exited 0: identified normalized producer and raw year override.

## Durable regression and red/green
Promoted /tmp/DI2-second.ArN3VU/internal/handlers/dashboard/di2_second_year_test.go into the repository at the same relative path. Test bodies and fixture values retained; gofmt only changed formatting. Tests exercise the real adapter, prior $100 expense, UTC/west/east midnight/noon controls, and actual shared-template rendering. The original metadata-only counterfactual remains as a diagnostic control, not a substitute for assertions on actual output.

Before production edits:
`rtk proxy go test ./internal/handlers/dashboard -run '^TestDI2SecondYear' -v -count=1`
Exit 1: UTC noon, west midnight/noon, east noon rejected covered prior history; midnight UTC/east controls succeeded. Actual rendering contained false “Not enough history to compare”.

After the shared predicate fix and scoped gofmt:
- Same exact targeted command exited 0: all six zone/hour cases and real-render test succeeded.
- `rtk proxy go test ./internal/services/insights ./internal/handlers/insights ./internal/handlers/dashboard ./internal/services/mcpsvc/spend ./internal/templates -count=1` exited 0. Package durations: 0.008s, 0.314s, 0.918s, 0.718s, 1.180s.
- `rtk proxy go build ./...` exited 0.

No live browser was used; rendered output is verified by the promoted embedded-template test and existing full/HTMX renderer suite.

## Delta and manifest
Read-only comparisons with frozen attempt-one source:
- `rtk proxy git diff --no-index /tmp/DI2-second.ArN3VU/internal/services/insights/period.go internal/services/insights/period.go`: expected exit 1, shared helper plus one call replacement.
- `rtk proxy git diff --no-index /tmp/DI2-second.ArN3VU/internal/handlers/dashboard/period.go internal/handlers/dashboard/period.go`: expected exit 1, one call replacement.

Cumulative comparisons with immutable accepted baseline:
- `rtk proxy git diff --no-index --stat /tmp/budget2-DI2-base.USPhZp/internal internal`: expected exit 1, 15 Go files.
- `rtk proxy git diff --no-index --stat /tmp/budget2-DI2-base.USPhZp/web web`: expected exit 1, unchanged three DI2 templates.
DI2.2.files includes all 18 cumulative source/test/template paths and four attempt-one/two evidence files. No scratch service checker probe was imported. Authoritative snapshots remain untouched.

## Handoff
DI2 attempt-two source frozen for fresh independent review. No CSS regeneration needed for this Go-only fix; none performed. Review acceptance remains with the lead and named gates.
