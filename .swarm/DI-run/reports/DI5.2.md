# DI5.2 author report — DONE, frozen for independent review

Scoped correction and verification complete; no worker PASS verdict or gate
acceptance is claimed. Fresh named tests/a11y/second review and the lead's
mandatory gates are still required. All worker checks have finished; dedicated
synthetic server127.0.0.1:18853 (PID2690935/session64235) was terminated and
ss confirmed no listener. No hanging worker command remains.

## Repair and preserved scope

Only existing application source changed: web/static/js/page-refresh.js.
The notice now precedes main content in normal flow, with a body-first
normal-flow fallback. Its existing text, semantic named region/status, buttons,
confirmation, dismissal and epoch/revision handling remain intact.
Insertion scrolls the same focused control into view only when needed;
it never focuses the notice. HTMX afterSwap remounts a detached pending notice
using the same region/status nodes and handlers. Connected notices are not
reinserted on unrelated partial updates. No modal, financial/data producer,
template, CSS, settings or tool schema change.

web/static/js/page-refresh.test.cjs retains the original seven VM groups and
adds three placement/focus/lifecycle groups. DI5-refresh-browser.cjs retains its
lifecycle assertions and now requires zero obstructions unconditionally.
DI5_REFRESH_BASELINE is no longer consulted. Old diagnostic evidence is retained
only as historical evidence; it is not an acceptance allowlist.

All193 DI5.1 manifest paths remain in the cumulative manifest (373 paths).
192 are byte-identical; only the explicitly authorized refresh browser harness
changed. All17 Go test files and their22 top-level tests are unchanged.
DI5.1's report, original dual manifests and166 evidence artifacts are unchanged.
Preservation proof: DI5.2/DI5.1-before.json and
DI5.2/provenance/preservation.json; rerunnable DI5.2-provenance.cjs.

## Meaningful red then green

TDD was followed: the promoted independent real-page probe and new VM assertions
ran before production edits. The original seven VM groups passed; three new
behavioral assertions failed, exit1 (DI5.2/red-vm.log).
An initial missing-region assertion was made explicit instead of allowing an
incidental property-access error; the retained red log has assertion failures.

DI5-focus-obstruction.cjs promotes the independent
/tmp/DI5-final-a11y.JYHU8N/focus-obstruction.cjs. Production scripts/HTMX stay
active. Only read-only /api/ui-refresh responses are mocked. Real dirty input,
ordinary Tab traversal, full rectangle containment and all nine topmost-hit
samples are retained; a center-point-only result is not accepted.

RED command (exit1, session97625):
rtk proxy env DI5_BASE=http://127.0.0.1:18853
DI5_OUTPUT=/tmp/DI5-correction.CqUG2q/evidence/red node
.swarm/DI-run/reports/DI5-focus-obstruction.cjs
The script was invoked by its absolute live path in that initial command.
Six states completed before the strict assertion failed.

| Width | Fully covered controls, BOTH themes | Focus rect x/y/w/h | Notice rect x/y/w/h |
|---|---|---|---|
| 390 | Income details | 33/852/96.5625/20 | 24/738/350/146 |
| 390 | Budget details | 33/763/94.90625/20 | 24/738/350/146 |
| 768 | Cash-flow details | 33/847/111.1875/20 | 24/762/728/122 |
| 768 | Healthcare details | 392/795/118.40625/20 | 24/762/728/122 |

Eight total occurrences;1440 had none. Each whole control is contained and
all nine samples hit the notice. Exact traces/screenshots: DI5.2/red.
Attribution remains a real DI3–DI6 integration regression, absent original
69e484a/pre-DI3 per the named verdict and retained independent evidence.
Matching accepted post-DI3 baseline never excused it.

GREEN: same command with evidence/green on corrected frozen source, exit0.
Final strengthened rerun session96016 also exited0, explicitly requiring every
conceded control to appear in the keyboard trace. Both themes at390/768/1440:
zero fully obscured controls,46/46/54 Tab events respectively, focused input
identity/value retained on arrival, notice keyboard reachable.
Full geometry/hit samples and notice AX trees: DI5.2/green.
All six final notice screenshots were visually inspected: readable wrapping,
normal-flow placement above Dashboard heading, and visible keyboard outlines.

## Promotion and guarantee mapping

DI5.1 report's M01–M14 and additional DI4 promotion map remains the canonical
exact durable-test mapping for this cumulative attempt. M01–M13 and all extra
Go probes are unchanged and freshly executed by the full/promoted suites.
M12's full Dashboard/browser coverage was also freshly rerun.
M14 is extended, without duplicating DI1/DI2/DI3/DI4/DI6 durable probes:

| Guarantee | Durable test/evidence |
|---|---|
| All-scripts-active conceded focus regression | DI5-focus-obstruction.cjs; DI5.2/red and green, six states each |
| Main-first/fallback placement, no focus stealing, conditional scroll | page-refresh.test.cjs: notice precedes main content without stealing focus; displaced input is kept visible; no-main fallback precedes body content and does not scroll an already visible control |
| Same region/status across swaps, no duplicates | page-refresh.test.cjs: HTMX replacement restores the same pending region/status once; partial swaps do not reannounce |
| Real displaced input, actual HTMX inner/outer swaps | DI5-refresh-layout.cjs; DI5.2/layout/results.json and12 screenshots, main/fallback x six states |
| Dirty refusal/acceptance, exact URL/query/hash, dismissal/AT removal, restart, clean reload/no loop | strict DI5-refresh-browser.cjs; DI5.2/refresh/refresh.json, six states |
| Hidden/coalesced signals, native/overlapping HTMX submission deferral, polling overlap, restart | Original seven page-refresh.test.cjs groups retained; DI5.2/green-vm.log |
| Server isolation/schema/routes/synthetic saved files | Existing DI6 server tests and unchanged TestDI5SavedDataUnchanged; fresh full/promoted logs |

The real layout fixture deliberately starts an editable input at the viewport
edge. Normal-flow arrival actually requires110px scrolling at390 and62px at
768/1440; the identical focused input remains exposed at all nine samples.
Real HTMX partial/main replacement retains the identical notice and status.
The main-replacement response is synthetic, not a claim that HTMX preserves
arbitrary unsaved server-rendered values. The shared listener itself does not
write or clear dirty content.
The layout fixture also fails on frozen pre-correction source (exit1,
fixed versus static), then passes12 states on the correction (exit0).
This supplemental calibration occurred after the primary test-first red.

## Fresh final verification

Snapshot: /tmp/DI5-correction.CqUG2q, assembled ONLY from immutable
/tmp/budget2-DI4-base.KPl7oY + DI4.2 cumulative manifest + frozen DI5.1 manifest,
then explicit correction files. No live whole-tree copy.
Authorized synthetic demo data was dereferenced into independent runtime
data/import/backup directories; no8080/8081 or real-data fixture access.
674 baseline source/config/standard files were checked; beyond DI4.2 only the
two scoped refresh source/test files differ. All DI4 source matches frozen
live source. Full hashes: DI5.2/final-source-hashes.json.

Old page-refresh.js:
e4f515e7adcd11c4bad42d5ec1207a37809203e19899c25591d9574cb3e5aca8
Corrected page-refresh.js:
97cb7c8c57d9bf9a885a8d0f86fea68a2a3d5f8377c071ed906997220c3e2ecf
Unchanged tailwind.css:
f09162945444f745061980d04d5d3eafd8c7c9d67c178381837555429adb6c5c
Unchanged DI3 kpis.html:
b59581cea9b37fdb4efd2f169c176c31312ecf067f5f20616e5600912ea4b5f3

Commands in isolated snapshot; every final command below exited0:
- rtk proxy node --test web/static/js/page-refresh.test.cjs —10/10.
- rtk proxy go build -buildvcs=false ./...
- rtk proxy go vet ./...
- rtk proxy staticcheck ./...
- rtk proxy go test -count=1 ./... —50 packages, including server/uirefresh.
- rtk proxy go test -count=1 -v ./internal/services/insights
  ./internal/handlers/dashboard ./internal/handlers/insights ./internal/templates
  ./internal/services/mcpsvc/spend ./internal/services/mcpsvc/ledger ./cmd/server
  -run '^TestDI5'
- rtk proxy make COMMIT=di5-frozen css-verify —CSS current; no regeneration.
- rtk proxy bash smoketest/gate/run_tests.sh in the supplied agents2 worktree:
  final line exactly ALL PASS.

DI5-verify.cjs ran the seven-command full sequence, session96409 exit0.
Exact command vectors/cwd/timestamps/exits and full logs:
DI5.2/verification/commands.json and sibling logs.
COMMIT override avoids Makefile git inspection; VCS stamping disabled.

Browser commands use rtk proxy env DI5_BASE=http://127.0.0.1:18853,
DI5_OUTPUT=/tmp/DI5-correction.CqUG2q/evidence/<name>, then node
.swarm/DI-run/reports/<script>.cjs. Refresh/a11y additionally set
DI5_SOURCE_ROOT=/tmp/DI5-correction.CqUG2q. Layout needs only source root/output.
Final observed command sessions/exits:
- DI5-focus-obstruction, green:96016, exit0.
- DI5-refresh-browser, refresh:29104, exit0.
- DI5-refresh-layout, layout:35125, exit0.
- DI5-a11y-browser, a11y:93454, exit0.
- DI5-dashboard-browser, dashboard:13644, exit0.
- DI5-integration-browser, integration:89042, exit0.

Dashboard rerun covered all five chart/table value sequences before/after HTMX,
five dialog keyboard traps/Escape/return, disclosures and preserved date focus.
Synthetic same-date rerun retained July net5757.29; contributors899.00/108.31/100;
Aug28 recurring22=3 subscriptions/7 bills/12 other, both pages/tools.
These remain synthetic demonstration values, not production rules.

Full-site audit: nine routes x two themes,18 captures, zero automated WCAG-tagged
violations and zero page errors.20 incomplete rule results/458 node occurrences
remain explicitly unresolved; no blanket/manual accessibility certification.
Existing unlabelled/alternative/contrast observations remain as recorded in
DI5.1 and the baseline audit; this is not an allowance for focus obstruction.
No native screen-reader/speech-recognition certification is claimed.
Notice AX nodes and same-node status/removal assertions establish DOM/AX
behavior, not proof of a specific screen reader's spoken announcements.

## Reproduction and frozen handoff

Cached Playwright/Chromium paths are explicit in scripts; no dependency install.
Use fresh synthetic isolated copies and dedicated loopback ports only.
Replay provenance with DI5_SOURCE_ROOT, DI5_LIVE_ROOT, DI5_OUTPUT; it expects
evidence/DI5.1-before.json in that isolated root (retained in this bundle).
DI5.1's full14-family map and evidence remain preserved and cumulative.

Complete manifests: .swarm/DI-run/manifests/DI5.2.files and
.swarm/manifests/DI5.2.files, identical 373-path inventories including themselves.
Author review used read-only diff and exact source hashes, no git operations.
TDD and verification-before-completion skills informed red/green and final
evidence checks. All edits were scoped approval-reviewed apply_patch; generated
artifacts were copied from the isolated root. No rejected action was bypassed.
No production work remains planned. Lead owns fresh named reviews, gate check,
done/stats, commit/merge/deployment; no worker verdict, ledger or spec write.
