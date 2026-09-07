# DI3 interim demo release — lead evidence

DI3 attempt2 passed tests, adversarial and accessibility reviews. After each
verdict, escalate-scan exited0 without flags. Gate check exited0:
`OK: DI3 accepted at tier 2 (attempt 2)`.

Frozen release /tmp/budget2-DI3.2-release.Tdkf02 contains immutable DI3 baseline
plus only cumulative DI3.2 manifest paths. This includes accepted DI1/DI2/DI6;
DI4 and DI5 are not implemented or accepted by this release.

Commands through RTK in that snapshot:
- `make COMMIT=69e484a-DI3.2 check`: exit0, vet/staticcheck, vulnerability scan,
  pinned CSS freshness, full Go suite and uncached accounts check. Final line
  `✓ all checks passed`; log /tmp/DI3.2-lead-check.log. The scan found no called
  vulnerable symbols, with uncalled dependency advisories remaining.
- `go build -ldflags='-X budget2/internal/version.Version=demo-dashboard -X budget2/internal/version.Commit=69e484a-DI3.2' -o budget2-demo-dashboard ./cmd/server`: exit0.
- `node --test web/static/js/page-refresh.test.cjs`: seven passed, zero failed.

Installed binary at /home/darrell/bin/ai/budget2-demo/releases/dashboard-DI3.2-20260906/budget2.
SHA256: 26472f8ff3f6ffde1976d82ef2a3584ce4306b533d370fb67ceaf494376b9667.
Prior launcher retained at /home/darrell/bin/ai/budget2-demo/backups/before-dashboard-DI3.2-20260906/run-demo.sh;
previous refresh-only binary remains available for rollback. Launcher syntax
check passed. Validated PID2428225 executable/data/listen as demo-only, then
stopped only that process. New persistent session35240 logs to
/tmp/budget2-demo-DI3.2.log. Debug assets remain disabled; binary embeds assets.

HTTP health returns 69e484a-DI3.2/status ok. User's exact Dashboard URL
`/dashboard?end=2026-08-28&start=2026-03-01` renders the selected 181-day period,
new dashboard-overview and all four named headline sections, plus refresh JS.
Fresh MCP initialization, initialized notification and get_status verified
demo data_dir/settings, five CSVs and unchanged plan revision0. Session headers
are /tmp/DI3-mcp-headers; do not reuse pre-restart session IDs.

Demo refresh_pages returned requested:true/revision1; /api/ui-refresh returned
epoch89041072-fe84-4880-972d-b91f18863469/revision1. This establishes the request,
not delivery to every browser tab. User was told to reload once if still old.

Before/after inventories and SHA256 for all nine synthetic data files match,
including after refresh request. Real :8080 health remains
v1.4.0-1095-gf5e68e0/status ok. No real financial data used, no source commit,
merge, push, or real-server restart. This is interim demo deployment, not
whole-run completion. Primary/second evidence confirms money alignment; the
sole second-attempt production correction removes the import label override.

Post-deploy read-only browser confirmation: fresh isolated headless Chromium
context at 1440x1000, reduced motion, network restricted to GET/HEAD on
http://localhost:8081. User's exact March1–Aug28 URL rendered income $26,461.47,
net spending $32,057.26, cash-flow -$5,595.79 and spending-versus-plan $30.42
under. Asserted four exact primary headings, five loaded chart alternatives,
and scrollWidth1440. Screenshot /tmp/budget2-live-dashboard-DI3.2.png inspected;
no clipping. Browser exited0; no form interaction or data writes performed.

Additional read-only demo MCP checks after deployment: get_trends July1–31
returns June1–30 prior period, history_available:true, forecast_available:false;
largest Major Expense changes are Home Maintenance & Appliances +899.00,
Shopping & Household Supplies +108.31 and Cash Spending +100.00.
get_recurring(reference_date=2026-08-28) returns all22 series in3 subscriptions,
7 bills and12 other; subscriptions are Netflix, Spotify and Cloud Storage.
These verify accepted shared services in the deployed binary; final DI5 still
must compare the reorganized Insights UI with the same reference/filter.
