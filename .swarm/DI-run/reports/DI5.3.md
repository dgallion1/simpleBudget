# DI5.3 author report — BLOCKED, third-attempt HARD STOP

Lead upheld the hard stop after the durable green candidate failed.
No more source edits, retries, green attempts, merge or deployment are authorized.
The already-running full checks and immutable oracle have finished.
Worker server127.0.0.1:18861 (PID2778275/session80877) was stopped immediately
after the oracle; ss confirms no listener. No active worker test remains.
This is failure evidence, NOT a PASS verdict or gate acceptance.
Further work requires explicit user direction through the lead.

## Remaining failure and cause

After the original editing input is replaced by a real main HTMX swap,
dismissal or epoch reset while either notice action owns focus removes the
notice/status but leaves document.activeElement as BODY. The unsaved synthetic
value remains unchanged. Reproduces at390/768/1440 in both themes.

The failing removeNotice fallback sets main tabindex=-1, focuses main, then
immediately removes tabindex when the previous attribute was absent. It does
not retain a focusable fallback through that transition. The browser's resulting
active element is BODY, as recorded in the durable test and independent oracle.
Thus the fallback implementation does not meet the rewritten focus contract.
No corrective edit was attempted after this failure or the lead's hard stop.

Durable candidate run:228 assertions,24 failures (four per viewport/theme):
- dismiss from Refresh page after original input replacement;
- dismiss from Keep editing after original input replacement;
- epoch reset from Refresh page after original input replacement;
- epoch reset from Keep editing after original input replacement.

Evidence: DI5.3/green/focus-ownership.json. The directory name identifies the
intended TDD green phase; its actual result is FAIL, exit1. It was not overwritten.
Exact surviving button focus through main inner/outer swaps and settlement,
connected-original-input return, outside-focus non-theft, cancelled/delayed
swaps and no-resurrection checks pass in that same trace. Those successes do
not excuse the fallback failures.

## Test-first work and bounded source

Complete brief, canonical latest rulings, ACCESSIBILITY.md, DI5.2 report,
named a11y verdict and oracle/calibration files were read. TDD was used.
No disposable prototype implementation was read, copied or adopted.
Only page-refresh.js production changed. No CSS, templates, financial
producers, settings, schemas, git, ledger, verdict or spec edits.

The attempted correction captures notice focus at actual HTMX subtree cleanup,
conditionally restores a detached surviving action at remount, and uses one
ownership-aware removal path. It avoids stealing newly established external
focus. The main fallback in that removal path is the remaining defect above.

Existing page-refresh.test.cjs retains all10 groups/assertions; its minimal DOM
double gained contains so it can exercise the removal code. Real-browser
ownership assertions are in the new DI5-focus-ownership.cjs, not that double.

Durable RED before production edits:
rtk proxy env DI5_BASE=http://127.0.0.1:18861
DI5_OUTPUT=/tmp/DI5-final-worker.btR0cu/di53-evidence/red node
/home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/reports/DI5-focus-ownership.cjs
Session30387, exit1:228 assertions,66 expected focus failures.
DI5.3/red/focus-ownership.json preserves all results, including exact phase/node
identity observations. No harness-error correction or acceptance waiver.

Candidate GREEN after the single implementation change:
same command with DI5_OUTPUT ending /green and the script in the isolated
snapshot. Session88741, exit1:228 assertions,24 remaining fallback failures.
No subsequent production revision or green retry.

## Immutable oracle — once on exact failed source

Command:
rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-final-worker.btR0cu
DI5_BASE=http://127.0.0.1:18861
DI5_OUTPUT=/tmp/DI5-final-worker.btR0cu/di53-evidence/oracle
bash /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/tier3/DI5/accept.sh

The command was launched by an RTK-wrapped Node logging wrapper.
Session58437, exit1. All750 transition assertions ran over nine consumers,
both themes and all three widths. Six failures, all Dashboard:
"dismiss safely removes while Refresh page focused" after original input
replacement. Each records active=BODY, inside=false, count=0, status=false,
value=unsaved.744 assertions pass. No ORACLE PASS was emitted.

Exact command/environment/exit: DI5.3/worker-oracle-command.json.
Full output: DI5.3/worker-oracle.log.
Full matrix: DI5.3/oracle/focus-transitions.json.

The oracle stopped at its first failing stage. Its later VM/F1-obstruction/
layout/lifecycle stages did NOT execute in this run. Standalone VM10/10 did
execute before the hard stop. Earlier strict F1 tests/evidence remain retained;
no fresh F1 or full accessibility acceptance is claimed for this failed source.
No additional routine suites were started after lead upheld the hard stop.

Oracle hashes remain exactly calibrated:
- accept.sh: df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f
- focus-transitions.cjs: a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239

No oracle edit, report.md inside tier3/DI5, or worker oracle.3.log write.
The worker log is separate from lead-owned acceptance evidence.

## Monetary probe promotion and completed checks

New cmd/server/cross_money_di5_test.go promotes the narrow independent probe
from /tmp/DI5-second-prep.vQcE3l/cmd/server/di5_second_cross_money_test.go.
Only attribution comments and gofmt formatting were added; assertions retained.
TestDI5SecondCrossMoney checks four fixtures: split charges, split credits,
2.675 tie and displayed-neutral cash flow; four actual HTTP surfaces plus
assembled MCP top1/top10, date sentinels, two-month rendered-cent reconciliation
and no negative zero. Temporary data/backup/import directories only.
Original17 Go files/22 top-level tests remain byte-identical.
DI5.1's14-family promotion map remains preserved; this adds cross-surface
combination coverage rather than changing earlier assertions.

Focused promotion:
rtk proxy go test -count=1 -v ./cmd/server -run '^TestDI5SecondCrossMoney$'
Session85530 exit0, all four cases. Full log DI5.3/cross-money.log.

Existing VM:
rtk proxy node --test web/static/js/page-refresh.test.cjs —exit0,10/10.

Full sequence already running at hard stop, session9166, completed exit0:
- rtk proxy go build -buildvcs=false ./...
- rtk proxy go vet ./...
- rtk proxy staticcheck ./...
- rtk proxy go test -count=1 ./...
- rtk proxy go test -count=1 -v ./internal/services/insights
  ./internal/handlers/dashboard ./internal/handlers/insights ./internal/templates
  ./internal/services/mcpsvc/spend ./internal/services/mcpsvc/ledger ./cmd/server
  -run '^TestDI5'
- rtk proxy make COMMIT=di5-frozen css-verify
- rtk proxy bash smoketest/gate/run_tests.sh in supplied agents2 worktree;
  final line ALL PASS.

All exact argv/cwd/timestamps/exits and full logs:
DI5.3/verification/commands.json and siblings. These green checks do not
override the user-visible fallback failure or the last-attempt hard stop.

## Frozen preservation, hashes and handoff

Root /tmp/DI5-final-worker.btR0cu was copied ONLY from immutable complete
/tmp/budget2-final-DI5.2.wv3Nf7, then explicit correction/test files overlaid.
No whole live-tree copy. Dereferenced authorized synthetic fixture has its own
di53-runtime/data, di53-runtime/backups, di53-runtime/imports under this root.
Calibration runtime/backup paths were not reused. No8080/8081 contact.

All373 prior manifest paths retained.371 byte-identical; only already-owned
page-refresh.js and page-refresh.test.cjs changed. All earlier reports,
manifests, evidence, browser scripts and17 Go files unchanged.
692 baseline source files compared; snapshot/live match and no unowned source
delta. Full inventories: DI5.3/prior373.json, baseline-sources.json,
freeze-sources.json. New source hashes are in correction-hashes.json.

Failed frozen source SHA256:
- page-refresh.js: 7449f662a72c0af64f6a6bde63ea1e06ae9b16de80560e6aaafd0d388bd2f081
- page-refresh.test.cjs: 3660e4074d9e054d2e2b8ac4866dfbf8cea7d7948c5e7bad7770eb9374e3f91d
- cross_money_di5_test.go: e160b0cd9f2efb8073d948b017eb5ba7e7e5bd4fa01e28f32ad11c093b1f29a5
- DI5-focus-ownership.cjs: 29f5f0748a6ae27235b1fa09d30e28dc6e875f2765f11aac09d6597469861fe9

Cumulative dual manifests contain 396 paths, including all373 prior paths,
18 new evidence artifacts, new test/report paths and the manifests themselves:
.swarm/DI-run/manifests/DI5.3.files and .swarm/manifests/DI5.3.files.

Source is frozen FAILED. No retry4, merge, deploy, PASS verdict or acceptance.
Lead/user explicit direction is required; worker takes no further action.
