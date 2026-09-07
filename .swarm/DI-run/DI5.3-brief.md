# DI5 final attempt — Tier 3 focus ownership correction

This is the LAST permitted attempt (3). DI5.1/F1 and DI5.2/F2 were conceded
Tier-2 failures. Any failure of this attempt halts the task; do not silently
redispatch or overwrite evidence. Dispatch only after the lead records both
ends of the oracle calibration. The oracle is lead-owned and immutable during
implementation/review. Do not weaken it to fit an implementation.

## Scope and ownership

Read the canonical spec and its latest rulings, ACCESSIBILITY.md in full,
DI5.2 author report, DI5.2 checker-a11y verdict, and the Tier-3 oracle files.
Preserve every DI5.2 manifest path and prior evidence. Allowed production path:
web/static/js/page-refresh.js only. Tests: its existing .test.cjs, new narrowly
focused tests, and DI5.3 report/manifest/evidence. No financial producers,
templates, CSS, settings, tool schemas, real/demo data, Git or deployment.

## Rewritten contract: focus ownership, not just node identity

1. Preserve the current normal-flow notice at the start of main, body fallback,
   same accessible region/status nodes, text and action handlers. No overlay.
   Arrival must preserve the connected editing control and keep it visible.
2. During real HTMX innerHTML/outerHTML main replacement, a surviving focused
   Refresh page or Keep editing button retains EXACT node/focus identity after
   swap AND settlement. Restore only focus lost because its subtree detached.
   Preserve any meaningful focus the user/another handler established elsewhere.
3. Unrelated partial swaps and cancelled swaps must not reinsert/reannounce,
   refocus or scroll the notice. A user moving focus while a delayed response/
   swap is pending keeps that new focus. Avoid persistent stale capture state.
   Capturing at actual subtree cleanup is a possible design; beforeSwap plus
   correctly scoped records is also valid. Test behavior, not event spelling.
4. On dismissal OR epoch reset, invalidate obsolete restoration before removal.
   If focus belonged to a notice action, return to the connected original
   editing control. If that control was replaced, focus the current main or
   its heading as an explicitly accessible fallback; never BODY or a detached
   node. Any temporary tabindex must not remain a positive/extra tab stop.
   If focus was outside the notice, leave it there. Remove both region/status;
   no stale swap may resurrect a dismissed/reset notice.
5. Preserve all original refresh semantics: dirty values, refusal, confirmation
   before discard, clean reload once with URL/query/hash, hidden-tab deferral,
   native and overlapping HTMX submission deferral, no financial writes,
   server-scoped epoch/revision baseline, restart without reload or auto-prompt.
6. Shared-script consumer inventory: Dashboard, Explorer, Insights, Major
   Expenses, What-If, Accounts, Transfers, File Manager, Duplicates. Oracle
   exercises arrival/partial/removal on all nine, both themes and three widths;
   real Dashboard main replacements, cancellation, delayed-focus movement;
   strict F1 tab/rectangle/nine-hit-point regression; existing layout/lifecycle
   and VM guards. Existing financial tests/producers remain byte-identical.

## Implementation and evidence

Test first. Promote the new focus lifecycle cases to durable automated tests,
including reset/dismiss after original input replacement and delayed/cancelled
swap non-theft. Preserve all 17 promoted Go files/22 top-level tests unchanged.
The disposable calibration prototype is NOT implementation to copy or adopt.
Implement independently from the contract and failing tests.

The adversarial lane's final attempt2 report also preserves a new test-only
cross-surface monetary probe at
/tmp/DI5-second-prep.vQcE3l/cmd/server/di5_second_cross_money_test.go.
DI5 is the approved durable-probe promotion task: promote this narrow probe as
an additional Go test (or prove exact existing coverage), without changing any
of the original 17 files or financial production logic. Retain attribution.

Use an immutable snapshot plus explicit allowed manifest overlays, not a live
whole-tree copy. The immutable complete DI5.2 source is
/tmp/budget2-final-DI5.2.wv3Nf7; all source hashes must be recorded at freeze.
Use synthetic data copies and dedicated loopback ports, never 8080/8081.
Stop test servers at handoff. Shell commands prefixed rtk; edits apply_patch.

Run the lead oracle .swarm/DI-run/tier3/DI5/accept.sh against your frozen source
and actual matching synthetic server (DI5_SOURCE_ROOT, DI5_BASE, DI5_OUTPUT).
Report exact command/exit and evidence. Lead separately runs oracle.3.log;
its last line must be ORACLE PASS, emitted only by an all-pass oracle.
No report.md may exist inside tier3/DI5 (would invoke obsolete gate contract).
Run all VM groups plus fresh build/vet/static/full Go tests/CSS checks. Record
any calibration/test-harness corrections explicitly; do not excuse behavior.
Write cumulative DI5.3 manifest and author report, no PASS verdict.

Freeze after implementation: independent tests, a11y and adversarial second
review still required. Lead alone operates Git and the acceptance gate.
