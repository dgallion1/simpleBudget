# DI5.4 author progress — attempt 4, Tier 3

User reopened only the fallback focus lifecycle after the prior hard stop.
No implementation has been submitted. No worker verdict, acceptance, commit,
deployment, ledger edit, or oracle edit is authorized by this report.

## Requirements and ruling

Read the complete task brief, DI5.3 report, all ACCESSIBILITY.md, current
page-refresh.js, durable ownership test, and oracle/calibration instructions.
The user authorized changing only the stale durable expectation of absent
tabindex while main is focused to -1, retaining exact focus/value/region/status
assertions and adding an absent-tabindex assertion after blur. Old evidence
and the independent oracle remain unchanged.

## Phase 1 — diagnosis and test-first setup

Preparing an isolated copy solely from /tmp/DI5-final-worker.btR0cu, distinct
synthetic data/backup/import directories and a dedicated loopback port.
Ranked hypotheses: immediate tabindex removal blurs main; a subsequent HTMX
handler steals focus; main never receives focus. The browser probe will record
activeElement immediately before and after attribute removal and compare the
single variable of retaining it. The probe will be promoted before the fix.

Production scope is only the fallback block in web/static/js/page-refresh.js.
Existing removal callers are the Keep editing handler and poll epoch reset,
both in that same file; successful remount/cleanup capture is out of scope.

Initial read-only command hit a sandbox runtime incompatibility before launch;
read-only escalated commands succeeded. No approval rejection occurred.

## Phase 2 — confirmed red

Isolated root: /tmp/DI5-fourth-worker.1ptHqE. Synthetic server 127.0.0.1:18864
uses newly copied di54-runtime/data and distinct di54-runtime/backups/imports.
Baseline hashes cover all396 prior paths and693 cmd/internal/web source files.
HEAD read-only check is69e484a. No Git state changed.

Promoted DI5-fallback-lifecycle.cjs before any production edit. Red command:
`rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-fourth-worker.1ptHqE DI5_OUTPUT=/tmp/DI5-fourth-worker.1ptHqE/.swarm/DI-run/reports/DI5.4/red-probe node .swarm/DI-run/reports/DI5.4-run.cjs red-probe /tmp/DI5-fourth-worker.1ptHqE rtk proxy node .swarm/DI-run/reports/DI5-fallback-lifecycle.cjs`
Exit1;41 assertions, four real absent-tabindex fallback focus failures.
Minimal browser observation: MAIN before removal, BODY immediately afterward;
retaining the attribute leaves MAIN focused. No HTMX handler participates in
that minimized probe. This confirms the first hypothesis.

Updated only the authorized durable attribute expectation and added24 post-blur
assertions; all228 original focus/value/status/region checks remain. Its full
pre-fix red run is in progress. No production correction submitted yet.

## Phase 3 — single correction, targeted green

Durable pre-fix run completed exit1:252 assertions,24 failures, each the
documented BODY fallback symptom. Extended focused pre-fix probe completed
exit1:65 assertions, eight failures (four lost fallback focus, four transient
overwrites of existing tabindex during focus). Raw red evidence is retained.

Only the main fallback block changed. An absent tabindex receives -1 until
blur; existing values are never overwritten. A tabindex-only MutationObserver
recognizes later writes, including same-value writes, so cleanup relinquishes
ownership instead of undoing another writer. Pending mutation records cover
same-task external changes followed by blur. Blur removes its listener and
disconnects the observer. Failed focus also releases temporary ownership.

Targeted green command matches the extended red command with green-probe
output/name; exit0,65/65 assertions. Original immutable source is unchanged.
Full252 ownership checks and the complete unchanged oracle are running on the
same candidate at18864. Fresh build/vet/static/full Go/promoted/CSS and agents2
smoke checks are running. This is author evidence, not independent acceptance.

## Phase 4 — regression results so far

Ownership green completed exit0:252/252 (the original228 plus24 blur checks),
all390/768/1440 light/dark combinations. Build, vet and staticcheck exit0.
Oracle and remaining full checks are still running; no failure observed yet.

Candidate hashes:
- page-refresh.js: d1875b727a3cf990a059538e443e7f1e1dfab799da1d2672dd61d9d7e8a5e115
- DI5-focus-ownership.cjs: 1bf23e9e947ebab4ec3d645f4878312707e5a5ed4d0398a02a67ddb937bef573
- DI5-fallback-lifecycle.cjs: a13fb997db8b8125997996881b9820918095b50c010d17ba169ca818d5420e5b

Independent oracle hashes confirmed unchanged:
- accept.sh: df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f
- focus-transitions.cjs: a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239
No report.md exists in the Tier3 directory. Production diff review confirms
only the fallback block changed; durable diff matches the user's exact ruling.

## Phase 5 — completed author verification and source freeze

Single candidate correction completed. No additional production revision or
submitted retry. Source is frozen at /tmp/DI5-fourth-worker.1ptHqE and the
explicit correction paths have been applied to the69e484a task worktree.
No subagents, commits, Git-state changes, deployments, live financial tools,
ledger edits, verdicts, or lead oracle.4.log writes were performed.

Complete unchanged oracle command (exit0, final line `ORACLE PASS`):

`rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-fourth-worker.1ptHqE DI5_BASE=http://127.0.0.1:18864 DI5_OUTPUT=/tmp/DI5-fourth-worker.1ptHqE/.swarm/DI-run/reports/DI5.4/oracle node .swarm/DI-run/reports/DI5.4-run.cjs worker-oracle /tmp/DI5-fourth-worker.1ptHqE rtk proxy bash /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/tier3/DI5/accept.sh`

All750 transitions pass over nine consumers, three widths and both themes.
VM10/10 pass. All six all-scripts-active F1 traces have zero covered controls
(46/46/54 tabs per theme). All12 main/fallback layout states and six lifecycle
states pass, including refusal/acceptance, dirty/dismiss/restart, four loads,
and URL/query/hash behavior. Author log is DI5.4/worker-oracle.log, separate
from the lead's acceptance log. Fresh screenshots and AX evidence are retained.
Visually inspected the mobile dark Dashboard screenshot: notice and both
actions render normally, focus indicator visible, no overlay over the form.

Additional completed commands, all exit0, in the isolated root:
- `rtk proxy go build -buildvcs=false ./...`
- `rtk proxy go vet ./...`
- `rtk proxy staticcheck ./...`
- `rtk proxy go test -count=1 ./...`
- `rtk proxy go test -count=1 -v ./internal/services/insights ./internal/handlers/dashboard ./internal/handlers/insights ./internal/templates ./internal/services/mcpsvc/spend ./internal/services/mcpsvc/ledger ./cmd/server -run '^TestDI5'`
- `rtk proxy make COMMIT=di5-frozen css-verify`
- `rtk proxy bash smoketest/gate/run_tests.sh` in the supplied agents2 worktree,
  final line ALL PASS.

Each command's exact argv, cwd, start/end time, exit and DI5 environment is in
DI5.4/*-command.json, with corresponding full *.log output. CSS emits only the
existing outdated Browserslist metadata warning; generated CSS is up to date.

Preservation command `rtk proxy node .swarm/DI-run/reports/DI5.4-preserve.cjs verify`
exits0:394 of396 prior paths byte-identical; only page-refresh.js and the
explicitly authorized temporal durable assertion changed. All18 Go files,
earlier reports, manifests and failed evidence remain byte-identical. All693
cmd/internal/web source files match live/frozen; the only source delta against
the immutable failed source is page-refresh.js. Full per-path hashes are in
DI5.4/baseline.json and preservation.json. No whole live-tree copy occurred.

Immutable failed-source hashes rechecked:
- page-refresh.js: 7449f662a72c0af64f6a6bde63ea1e06ae9b16de80560e6aaafd0d388bd2f081
- DI5-focus-ownership.cjs: 29f5f0748a6ae27235b1fa09d30e28dc6e875f2765f11aac09d6597469861fe9

`rtk proxy git diff --check -- web/static/js/page-refresh.js` exits0.
Server PID2836394 was resolved by ss on18864 and stopped with
`rtk proxy kill -TERM 2836394`; shutdown completed. DI5.4/shutdown.log records
no listener on18864. No ports8080/8081 or live data directories were used.
The startup log's initial-snapshot-already-in-progress message is not a backup
acceptance claim; each runtime directory is isolated and no backup check was
part of this focus fix.

Cumulative manifests .swarm/DI-run/manifests/DI5.4.files and
.swarm/manifests/DI5.4.files include all396 prior paths, every new author
script, this report, generated evidence and both manifests themselves.
Implementation is complete for independent review. Tier3 acceptance still
requires the lead's frozen oracle.4.log, tests/a11y/second reviewers and gate.
