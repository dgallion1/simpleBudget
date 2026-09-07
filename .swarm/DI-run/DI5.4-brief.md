# DI5.4 — user-reopened fallback focus fix, Tier3

User explicitly said Yes to reopening only the remaining focus fix after the
third-attempt hard stop. One additional submitted correction is authorized;
keep attempt number4, all old failures and Tier3. No silent further retry.

## Exact bounded contract

Only page-refresh.js fallback focus lifecycle may change. When the original
editing control was replaced, dismiss/reset from either notice action must
leave focus on the current main (or existing meaningful fallback), not BODY.
Do not remove a temporary tabindex while that fallback still owns focus.
Clean up when focus leaves; preserve existing tabindex values and do not undo
a later change made by another owner. No extra positive tab stop or focus trap.
No refactor of successful remount, cleanup capture, notice placement, text,
confirmation, request/hidden/epoch logic. No financial/template/CSS/data/schema
changes. All396 prior paths and18 Go files remain unchanged, except the one
authorized JS path; new focused tests/evidence may be added without weakening
earlier assertions. Existing tests may only be extended as needed.

Lead ruling after pre-implementation review: DI5-focus-ownership.cjs currently
requires tabindex===null WHILE main owns fallback focus. That stale constraint
conflicts with the reopened lifecycle. You may replace only that attribute
expectation with tabindex===-1 while focused, retaining all exact focus/value/
region/status assertions, and ADD a blur assertion proving attribute removal
after focus leaves. Preserve the old test/evidence in immutable DI5.3. This is
a corrected temporal expectation, not a relaxation of the focus requirement.
The independent lead oracle remains byte-identical. No implementation has been
submitted at this checkpoint; attempt4 is still the single reopened attempt.

Read first: this brief; reports/DI5.3.md; current fallback; all ACCESSIBILITY.md.
Use original .swarm/DI-run/tier3/DI5/{accept.sh,focus-transitions.cjs} unchanged,
with CALIBRATION.md and retained attempt3 red evidence. No new oracle checks
are required; existing oracle exactly reaches this defect. No report.md in
the tier3 directory. All existing refresh guarantees remain acceptance guards.

## Diagnosis/test-first

The confirmed red symptom is 24 fallback failures in the228-assertion durable
DI5-focus-ownership.cjs, and six failures in750 assertions in the lead oracle.
Hypotheses in rank order: immediate tabindex removal blurs main; a subsequent
HTMX handler steals focus; main never receives focus. Use a minimal actual
browser probe to observe activeElement before/after attribute removal, then
test one variable. Promote the probe before the fix. Cover absent and existing
tabindex, blur cleanup, repeated fallback focus, and an externally changed
attribute. Preserve the real HTMX/dismiss/epoch regression and full oracle.
Do not read/copy any deleted calibration prototype. Implement independently.

## Isolation, output and review

You are not alone: lead owns docs, ledger and Git; checkers own verdicts. Never
revert another actor's edits. No subagents of your own and no commits, Git-state
changes, deployments, tool calls against live financial data, or ports8080/8081.
Worktree /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights on
codex/dashboard-insights at69e484a. Immutable failed source is
/tmp/DI5-final-worker.btR0cu. Copy ONLY that immutable tree, then explicit
allowed correction paths. Use its synthetic data copied to distinct new data,
backup and import directories and a dedicated loopback port. Shell prefixrtk;
all edits apply_patch via approved path access. Stop test server at handoff.

Run targeted red/green, complete unchanged oracle and228-assertion durable
browser test, existing VM suite, fresh build/vet/static/full Go/CSS checks.
Report exact commands, exits, hashes and preservation proof; phase updates in
reports/DI5.4.md. Write cumulative DI5.4 manifests (include all396 DI5.3 paths
and your new files). No worker PASS verdict. Source freeze at handoff, then
lead runs oracle.4.log and independent tests,a11y,second reviews. Review is
scoped to this reopened fallback and regression safety, not unrelated backlog.
No acceptance or merge/deploy unless all named reviewers and mechanical gate
pass. On a failed submitted correction, stop and return failure evidence.
