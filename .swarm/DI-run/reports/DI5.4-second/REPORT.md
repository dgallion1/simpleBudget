# DI5.4 independent adversarial review

Completed review, 2026-09-07. Scope: explicitly reopened fallback-focus fix
and existing regression safeguards, not unrelated backlog. No new defect found.

Root S=/tmp/DI5-fourth-second.7yOyBT, copied ONLY from immutable candidate
/tmp/DI5-fourth-worker.1ptHqE. Old reference /tmp/DI5-final-worker.btR0cu.
Read DI5.4 brief/report, canonical ruling and ACCESSIBILITY contract; prior
oracle/transition/calibration files were read completely in preparation.
Inspected complete current focus implementation and both durable browser scripts.

## Independent commands/results

Commands below ran from S; expand S to the absolute root above.

- `rtk proxy go build -buildvcs=false -o second-server ./cmd/server`: exit0.
- Started `rtk proxy env BUDGET_DEBUG=true BUDGET_LISTEN_ADDR=127.0.0.1:18876 BUDGET_DATA_DIR=S/second-runtime/data BUDGET2_BACKUP_DIR=S/second-runtime/backups BUDGET2_IMPORT_DIR=S/second-runtime/imports BUDGET_TEMPLATES_DIR=S/web/templates BUDGET_STATIC_DIR=S/web/static ./second-server`.
  New data copied with cp -aL ONLY from authorized synthetic demo fixture;
  independent backup/import dirs. No real8080/demo8081 or live data touched.
- `rtk proxy env DI5_SOURCE_ROOT=S DI5_OUTPUT=S/second-evidence/fallback node .swarm/DI-run/reports/DI5-fallback-lifecycle.cjs`: exit0,65/65.
- `rtk proxy bash -c 'set -o pipefail; DI5_SOURCE_ROOT=S DI5_BASE=http://127.0.0.1:18876 DI5_OUTPUT=S/second-evidence/oracle bash /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/tier3/DI5/accept.sh 2>&1 | tee second-evidence/oracle.log'`: exit0, final ORACLE PASS.
  All750 transitions over9 consumers/2 themes/3 widths,10 VM groups,
  strict F1 six states hits=[],12 layout states,6 lifecycle states completed.
  Lifecycle each has4 loads, preserved URL/hash, refusal/acceptance/restart/
  dismissal/clean reload; no allowlist. All-scripts-active keyboard traversal
  includes original obstructed controls,46/46/54 tabs per theme.
- `rtk proxy env DI5_BASE=http://127.0.0.1:18876 DI5_OUTPUT=S/second-evidence/ownership node .swarm/DI-run/reports/DI5-focus-ownership.cjs`: exit0,252/252.
  Logged through tee to ownership.log. Exact node/focus across actual main
  inner/outer swap and settlement, delayed/cancelled non-theft, both actions,
  connected-input and replaced-input removal, stale-remount prevention all pass.
- `rtk proxy go test -count=1 ./cmd/server -run '^TestDI5SecondCrossMoney$'`: exit0,.375s.
- `rtk proxy go test -count=1 ./internal/services/insights ./internal/handlers/dashboard ./internal/handlers/insights ./internal/templates ./internal/services/mcpsvc/spend ./internal/services/mcpsvc/ledger ./cmd/server -run '^TestDI5'`: exit0; .005/.192/.733/.016/.048/.005/.926s; go.log.

## Additional adversarial observer-delivery probe

The durable65 cases cover same-task external writes followed immediately by
blur. Independently replayed all65 with a zero-delay task between external
attribute write and blur, ensuring MutationObserver callback delivery also
relinquishes ownership, including same-value -1 writes. No source file edited.
Exact command (root/output environment as above, output fallback-async):

    rtk proxy env DI5_SOURCE_ROOT=/tmp/DI5-fourth-second.7yOyBT DI5_OUTPUT=/tmp/DI5-fourth-second.7yOyBT/second-evidence/fallback-async node -e 'const fs=require("fs"),assert=require("assert");let s=fs.readFileSync(".swarm/DI-run/reports/DI5-fallback-lifecycle.cjs","utf8");const a="await p.evaluate(v=>{\n     const main";const b="document.querySelector(\u0027#next\u0027).focus();";assert(s.includes(a)&&s.includes(b));s=s.replace(a,"await p.evaluate(async v=>{\n     const main").replace(b,"await new Promise(resolve=>setTimeout(resolve,0)); "+b);new Function("require",s)(require);'

Exit0,65 assertions,no failures. JSON in fallback-async/fallback-lifecycle.json.
Initial runner used vm.runInThisContext and failed before browser launch with
duplicate fs declaration; corrected runner isolates its lexical scope via
Function. That harness error is not a product failure or a claimed test pass.

## Scope and source proof

`diff -qr OLD/internal internal` and `diff -qr OLD/cmd cmd`: no differences.
All18 Go files remain unchanged. `diff -qr OLD/web web` identifies only
page-refresh.js. Exact unified delta is confined to MAIN fallback's temporary
tabindex lifecycle; no remount/capture/notice/confirmation/epoch refactor.
Candidate JS SHA256 d1875b727a3cf990a059538e443e7f1e1dfab799da1d2672dd61d9d7e8a5e115.
Every cmd/internal/web file cmp matches canonical frozen worktree; source.sha256
preserves inventory and `sha256sum -c` passed after all checks.
Cross-money gofmt-output comparison differs only by two attribution comments;
assertions preserved, fresh execution above.
Durable ownership diff changes only expected tabindex null to -1 while MAIN
owns focus and adds24 checks that it is removed after blur. Earlier focus,
value,region,status assertions retained. Oracle hashes remain calibrated:
accept.sh df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f;
focus-transitions a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239.

Canonical manifest474 entries:453 present in supplied immutable candidate
match canonical bytes;21 prior DI5.3 evidence/report/manifest entries are absent
from that supplied copy, not application code. No whole-live-tree copy was
performed to fill them. This is disclosed snapshot packaging, not omitted
financial/source verification or a new scope failure.

## Findings and limits

Fallback absence/existing -1/0/3, repeated cycles, transient preservation during
focus, same-task and delivered-observer external0/-1/removal all pass. Ordinary
Tab leaves fallback and cleans only owned attribute; pre-existing positive3
is preserved rather than introduced. No trap, no stale restoration observed.
ACCESSIBILITY9/10 focus and16 region/status guards pass actual browser checks.
Observed mobile light/dark screenshots show normal-flow readable notice and
visible focus outline. Direct view_image hit legacy Landlock; read-only base64
forwarding displayed those actual images. Final shutdown/hash read needed
scoped escalation for the same infrastructure issue. No policy rejection.
Not full manual WCAG/screen-reader or universal filesystem-write certification;
not a rerun of unrelated fullsite backlog. Complete relevant browser proof was
performed this attempt, not inferred from incomplete DI5.2 work.

Resolved own server PID2874872 via ss on18876; `rtk proxy kill -TERM 2874872`;
process exited0; final `rtk proxy ss -ltn 'sport = :18876'` shows no listener.
No production/Git/ledger/deploy edits or subagents. Lead owns gate/acceptance.
