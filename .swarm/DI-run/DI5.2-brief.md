# DI5 attempt2 — bounded refresh notice accessibility repair

Lead CONCEDES DI5.1 checker-a11y F1 under the approved visible-focus/accessible
refresh contract. Read its entire verdict, DI5.1 report and manifest, canonical
spec ruling, all ACCESSIBILITY.md, and DI6-brief.md. No new feature or financial
behavior is requested. All DI5.1 test-only work is frozen and must be retained.

Ownership: web/static/js/page-refresh.js, page-refresh.test.cjs, new focused
test files, DI5 refresh/browser regression scripts and DI5.2 report/evidence/
manifests. Generated tailwind.css only if needed for class freshness. No other
application files, data, settings, tool schema, git state or deployment.

Design: replace fixed overlay with a normal-flow notice near the main heading
(inside main before page content; safe normal-flow fallback if no main).
Retain its semantic region/status, text, buttons and confirmation behavior.
Do not steal focus. If insertion would move the current input/control out of
view, keep that same focus visibly in view; no jump to the banner. No overlay
may hide another focused control. Main/partial HTMX replacement must not lose
or duplicate the notice or its accessible announcement. Normal page scrolling
must remain usable. Keep buttons keyboard-reachable.

Test first: promote the independent all-scripts-active focus-obstruction.cjs
from /tmp/DI5-final-a11y.JYHU8N. Red must fail specifically on fully covered
Income/Budget at390 and Cash-flow/Healthcare at768, both themes. Then fix and
prove green with zero such obstructions; retain full rectangle plus hit-test
proof, not center-point-only classification. No accepted-baseline allowlist.
Cover arrival while input already focused and tab traversal, both themes,
390/768/1440. Strengthen existing DI5-refresh-browser.cjs strict default and
preserve its clean-reload/URL/hash/dirty refusal/acceptance/restart/dismissal/
focus/AT assertions. Existing seven VM groups and server tests must pass.

Maintain all existing guarantees: no data writes, dirty content unchanged on
arrival/refusal/dismissal/restart; current-focus identity; confirmation before
discarding; native/HTMX submission deferral; clean reload once; hidden-tab
deferral; epoch reset no reload; region and status removed on dismissal/reset.
Do not introduce a modal or blocking dialog to solve this nonmodal notice.

Use immutable /tmp/budget2-DI4-base.KPl7oY + DI4.2 manifest + frozen DI5.1
manifest, then explicit correction paths for isolated verification. No live
whole-tree copies. Preserve source freeze while reviewers copy manifests.
Synthetic data only, dedicated loopback ports (not8080/8081). Stop test servers
at handoff. All edits via approved apply_patch, shell prefixed rtk.

Run focused red/green and browser checks, fresh build/vet/staticcheck/full
tests/CSS verification after final correction. Write cumulative DI5.2 manifest
(all DI5.1 paths plus correction paths/evidence) and exact report with command
exits, preservation proof and no baseline-excused obstruction. No worker PASS
verdict. Lead dispatches fresh tests,a11y,second; gate decides acceptance.
