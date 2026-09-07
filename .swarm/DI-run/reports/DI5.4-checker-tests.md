# DI5 attempt 4 — independent primary tests report

Scope: user-reopened fallback-focus correction and regression safety only. Read canonical design including September 7 reopening/temporal-test rulings, complete ACCESSIBILITY.md, DI5.4 brief, reports DI5.3/DI5.4, corrected production and browser assertions, and calibrated oracle. No acceptance waiver for earlier focus failures. No unrelated scope expansion.

## Isolation and identity

Candidate copied from immutable /tmp/DI5-fourth-worker.1ptHqE into /tmp/DI5-fourth-tests.QbdTEf. No live application source copied. Old comparison source: /tmp/DI5-final-worker.btR0cu.
All commands below ran independently, via RTK with scoped escalation for the host sandbox. Commands use snapshot cwd unless stated.
Evidence directory: /tmp/DI5-fourth-tests.QbdTEf/tmp/checker-evidence (E below).

`rtk proxy node tmp/checker-evidence/preservation.cjs` — exit 0, including rerun after tests.
693 cmd/internal/web files compared: sole changed source page-refresh.js, no additions. All 18 *_di5_test.go files identical, including cross_money_di5_test.go SHA256 e160b0cd9f2efb8073d948b017eb5ba7e7e5bd4fa01e28f32ad11c093b1f29a5.
Current JS SHA256 d1875b727a3cf990a059538e443e7f1e1dfab799da1d2672dd61d9d7e8a5e115.
E/preservation.json records every Go hash. E/production.diff contains the exact fallback-only delta.
Canonical cumulative manifests: 474 current paths; all 396 previous included. Twenty-one prior report/manifest artifacts are absent from BOTH worker snapshots; this is distinguished from source preservation, not described as 396 byte-identical files. Among present prior files, only JS and the authorized ownership-test temporal assertion differ.

`rtk proxy diff -u /tmp/DI5-final-worker.btR0cu/.swarm/DI-run/reports/DI5-focus-ownership.cjs .swarm/DI-run/reports/DI5-focus-ownership.cjs` — expected exit 1; E/ownership.diff.
Only null→'-1' while focused plus post-blur null assertion. Original focus/value/region/status, cancellation, same-node and epoch assertions retained.

## Correction and regression results

1. `rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-fourth-tests.QbdTEf DI5_OUTPUT=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-evidence/fallback node .swarm/DI-run/reports/DI5-fallback-lifecycle.cjs` — exit 0, 65/65.
   Exact MAIN ownership after original control replacement; absent and pre-existing -1/0/3; repeated dismiss/epoch; temporary attribute remains until blur; real Tab escapes; existing values never transiently overwritten; external 0/-1/removal survives cleanup, including same-value same-task writes. E/fallback/fallback-lifecycle.json.
   Source review confirms MutationObserver delivered-record flag plus takeRecords covers both delivered and still-queued writes; observer/listener disconnect at release, with failed-focus cleanup.

2. Counterfactual: copied ONLY old JS into E's separate sibling scratch root tmp/checker-red/web/static/js. Ran the identical focused script with DI5_SOURCE_ROOT=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-red and DI5_OUTPUT=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-evidence/red.
   Expected exit 1: 8 meaningful failures (4 MAIN-focus losses, 4 pre-existing tabindex transient-overwrite cases), not harness errors. Candidate untouched. E/red.log and red/fallback-lifecycle.json.

3. `rtk proxy env DI5_BASE=http://127.0.0.1:18994 DI5_OUTPUT=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-evidence/ownership node .swarm/DI-run/reports/DI5-focus-ownership.cjs` — exit 0, 252/252, six theme/width states.
   Actual Dashboard and bundled HTMX: exact surviving button through inner/outer swap and settle; unrelated partial non-theft; cancelled/delayed swaps; other handler's chosen focus; original-control return vs replacement fallback; both actions/outside focus; dismissal/epoch AT removal and unsaved value; no resurrection/reload. E/ownership.log and ownership/focus-ownership.json.

4. `rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-fourth-tests.QbdTEf DI5_BASE=http://127.0.0.1:18994 DI5_OUTPUT=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-evidence/oracle bash /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/tier3/DI5/accept.sh` — exit 0; final line ORACLE PASS.
   Fresh independent 750/750 transitions across Dashboard, Explorer, Insights, Major Expenses, What-If, Accounts, Transfers, File Manager, Duplicates; light/dark ×390/768/1440.
   VM10/10; strict all-scripts-active F1 six states (46/46/54 tabs per theme, zero covered controls); layout12; lifecycle6 (four loads, refusal/acceptance, dismissal, restart, clean reload, URL preservation).
   E/oracle.log plus oracle/ JSON/screenshots. ACCESSIBILITY 9/10/12/14/16 exercised. This tests current correction and guards, not a fresh unrelated full-site redesign audit.
   Oracle hashes independently verified unchanged: accept.sh df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f; focus-transitions.cjs a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239.

## Financial preservation and independent quality checks

`rtk proxy node tmp/checker-evidence/continuity.cjs` — exit 0. Compares 696 recorded hashes from prior independent preparation /tmp/DI5-Go-checker.vZNpAR/tmp/checker-evidence/source-hashes.json: only refresh JS/VM differ; all original 17 promoted Go files and financial/template/CSS sources exact. E/continuity.json.
Thus prior assertion-body audit and sensitivity evidence in that directory's PREPARATION.md/go-promotions.log, plus /tmp/DI5-Go-mutant.nkqG6R mutation logs, remain associated ONLY for unchanged financial/date/retention tests. These covered all 14 promotion families, real HTTP links, signed rendered-cent reconciliation, grouping residuals, civil coverage, forecast boundaries, uncapped22 retention, and synthetic saved-file inventories. No old focus acceptance reused.

Fresh commands, all exit 0:
- `rtk proxy go build -buildvcs=false -o tmp/checker-server ./cmd/server` — E/build.log.
- `rtk proxy make COMMIT=di5-checker check` — vet, staticcheck, govulncheck, generated CSS verification, Go tests; final “all checks passed”. E/make-check.log. No called vulnerabilities; transitive advisories/outdated browserslist warning are nonblocking observations.
- `rtk proxy go test -count=1 ./...` — 50 packages, E/go-uncached.log. Fresh full-suite execution includes retained promotion/regression tests; not substituted by lead results.
- `rtk proxy go test -race -count=1 -v ./cmd/server -run TestDI5SecondCrossMoney` — four cases, E/cross-money-race.log.
  Independently read the new test assertions: actual Dashboard/KPI/Insights/full-HX plus assembled MCP top1/top10; split charges/credits, 2.675 tie, displayed-neutral cents; outside-period sentinels, two exact month labels, row cents plus adjustment, no negative zero. All pass with race detector.
- `rtk proxy bash smoketest/gate/run_tests.sh` in existing isolated /tmp/DI5-agents-smoke.9ZYqse — exit 0, all sections ALL PASS (tool session51204). No application Git state changed.

## Runtime and handoff

Built and started own binary with BUDGET_LISTEN_ADDR=127.0.0.1:18994, BUDGET_PUBLIC_URL=http://127.0.0.1:18994, BUDGET_DATA_DIR=/tmp/DI5-fourth-tests.QbdTEf/tmp/checker-runtime/data, BUDGET2_BACKUP_DIR and BUDGET2_IMPORT_DIR pointing to distinct sibling backups/imports. Data was dereferenced from candidate's synthetic di54-runtime/data. Browser requests restricted to synthetic loopback and GETs; no user browser or live8080/8081.
After checks: `rtk proxy kill -TERM 2899322`; server session96402 exited0; `rtk proxy ss -ltnp 'sport = :18994'` showed no listener. E/server.log records shutdown.
No production, Git, ledger, gate or deployment changes. Scratch audit scripts and old-source counterfactual are separate from candidate application files. No new promotion gap established: focused/durable regression already belongs to submission.

Conclusion: no blocking finding within the explicitly reopened fallback scope. Primary tests PASS; lead still owns named-review aggregation and gates.
