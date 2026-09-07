# Confirmed run-introduced focus obstruction — evidence, not DI5 verdict

The worker's four named obstructions are real, reproducible with all application scripts active, and not pre-existing to original 69e484a. They must not be dismissed merely because the accepted DI3-era baseline has them. This finding supersedes the preparation report's earlier statement that no new-to-run issue had been established. No source fix or acceptance verdict is written.

## Actual focused-state evidence

Isolated current accepted-DI4.2 server, actual Dashboard handlers/templates and all production scripts/CSS, /dashboard?start=2026-08-01&end=2026-08-28. Synthetic data only. Set From to 2026-07-01; actual input event marks dirty. Only /api/ui-refresh JSON is intercepted to provide controlled epoch/revision; focus event triggers production polling. No fake notice markup or removed application script. Tab normally from date input until refresh control. Real date/HTMX handling remains active.

Both light and dark reproduce identical geometry (viewport height 900):

| Width | Focused control | Focused rectangle x/y/w/h | Notice rectangle x/y/w/h | scrollY |
|---|---|---|---|---|
| 390 | Income details | 33 / 852 / 96.5625 / 20 | 24 / 738 / 350 / 146 | 0 |
| 390 | Budget details | 33 / 763 / 94.90625 / 20 | 24 / 738 / 350 / 146 | 563 |
| 768 | Cash-flow details | 33 / 847 / 111.1875 / 20 | 24 / 762 / 728 / 122 | 0 |
| 768 | Healthcare details | 392 / 795 / 118.40625 / 20 | 24 / 762 / 728 / 122 | 502 |

Each focused component rectangle lies fully within the opaque notice. Nine elementFromPoint samples per rectangle all hit notice descendants, not the focused control. Focused screenshots visually inspected: neither the control text nor its focus indicator is visible. These exact rectangles match worker evidence in /tmp/DI5-integration.hcuAxN/di5-evidence/refresh/obscured-*.json. Thus stripping scripts in the worker refresh harness did NOT cause these four results.

One additional sampled hit, 'Plan explanation and comparison', is explicitly NOT counted: its rectangle begins at x=16 while notice begins x=24. Nine interior samples alone incorrectly suggest complete coverage; the left edge/disclosure marker remains exposed. Full rectangle and visual inspection, not hit-test count alone, determine classification.

## Attribution beyond accepted baseline

Three independent immutable inputs, four width/theme states each:

- Original 69e484a, git archive into /tmp/DI5-origin-a11y.Qrxr81: no page-refresh.js, no refresh endpoint/notice markup; no corresponding obstruction in actual Tab traces.
- Accepted pre-DI3 /tmp/budget2-DI3-base.rhoTJM, copied into /tmp/DI5-preDI3-a11y.L68g1L: already includes DI6. Same refresh-notice code, actual dirty refresh notice triggered, no completely covered controls in these traces.
- Current accepted DI4.2 /tmp/DI5-final-a11y.JYHU8N: four named obstructions in both themes.

Byte proof (artifacts/obstruction/provenance.json):
- Original and pre-DI3 components/kpis.html identical SHA256 0459b5834574108a8212e33696663e243661fb09eec39405f245e428437c1cd0.
- Current kpis.html b59581cea9b37fdb4efd2f169c176c31312ecf067f5f20616e5600912ea4b5f3.
- Pre-DI3 and current page-refresh.js identical e4f515e7adcd11c4bad42d5ec1207a37809203e19899c25591d9574cb3e5aca8; absent original.

DI3 replaced large focusable KPI tiles with smaller detail buttons/link and rearranged layout. The fixed covering overlay belongs to DI6. This is a DI3–DI6 integration regression, observable after DI3, rather than a defect existing before the DI run. Exact implicated markup: components/kpis.html:12 Income details, :24 Cash-flow details, Budget details and Healthcare details in that same template. Overlay source: web/static/js/page-refresh.js:39 fixed bottom positioning and z-50, :72 append to body; no adjustment of focused content position or dismissal on its receiving focus. Read-only git diff 69e484a recorded in artifacts/obstruction/original-diff.txt. No attribution to DI4 copy correction or DI5 test files.

## Standard and significance

Constitution point 9 requires visible keyboard focus; WCAG 2.2 AA 2.4.11 requires a focused component not be entirely hidden by author-created content. The refresh notice appears from a system refresh request, not a disclosure explicitly opened by this keyboard user. The user-opened-content exception therefore does not excuse it; later reaching Keep editing is not sufficient. W3C specifically identifies sticky notifications fully covering focused components as failures: https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html .

This is a concrete final-integration accessibility blocker, not a broad polish suggestion. Prior accepted reviews missed this cross-feature state; their PASSes do not establish absence relative to the original run base. Lead must adjudicate/fix under the run process. No DI5 verdict until requested freeze.

## Reproduction and artifacts

Commands: rtk proxy node focus-obstruction.cjs; rtk proxy node focus-preDI3.cjs, cwd /tmp/DI5-final-a11y.JYHU8N. Original built via rtk proxy go build -o origin-server ./cmd/server from git archive 69e484a; pre-DI3 built similarly as pre-server. Dedicated origins current18799/original18800/preDI318801, independent synthetic data/import/backup dirs, same BUDGET_DEBUG/templates/static configuration as preparation. Never 8080/8081, no external/write requests, uploads or user browser session.

artifacts/obstruction/results.json contains original/current full Tab trace, rectangles, scroll positions and per-point hit targets; preDI3.json contains counterfactual pre-DI3 trace with actual notice; verified.json separates fully contained components from partial hit-test candidate; current-*-390/768-*.png screenshots; provenance.json and original-diff.txt. Harnesses reusable for durable promotion by authorized worker. No subagent, source/git/ledger edit or deployment. All three temporary servers stopped after collection.
