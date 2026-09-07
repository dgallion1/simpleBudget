# DI5 attempt4 independent accessibility evidence

Checker: checker-a11y. Family: anthropic. Tier3. Result: PASS for the user-reopened fallback-focus correction and regression guards. Previous F1/F2 verdicts and attempt3 failure evidence remain upheld historical records. This report does not accept other lanes or authorize deployment, and does not claim that every legacy page is fully WCAG-certified.

Durable retention: all current raw checker-evidence/, artifacts/ and independent probes are copied to .swarm/DI-run/reports/DI5.4-a11y/, with original69/preDI3 F1 and DI5.2 F2 attribution evidence alongside. diff -qr and cmp verified equality to the completed scratch run; no tests were repeated solely for retention. That folder's README records rerun prerequisites and temporary-root dependencies.

## Inputs, scope and isolation

Read the full DI5.4 brief, canonical approved design/latest September7 reopening ruling, author report, all17 ACCESSIBILITY.md points, original oracle/calibration, current fallback and durable tests. Root SPEC is absent; the approved design is docs/superpowers/specs/2026-09-06-dashboard-insights-design.md. Reopened scope is fallback ownership and regression safety, not unrelated backlog.

Independent scratch S=/tmp/DI5-4-a11y.xokOQB, created only with a fresh cp -a of authorized immutable /tmp/DI5-fourth-worker.1ptHqE. Old comparison O=/tmp/DI5-final-worker.btR0cu. No whole live-tree copy. Synthetic /tmp/budget2-DI-demo.mfLral/data copied with cp -aL to S/checker-runtime/data, with separate checker-runtime/imports and backups. Isolated actual Go handler/template server18873; actual-render artifact listener18874. Browser requests allowed only loopback GET/HEAD, except fully intercepted synthetic fixtures; no uploads/form writes/real sessions or real/demo8080/8081. No application source, git, ledger, financial data or deployment change.

Browser skill's previously authorized fresh-context headless fallback was used. The audit playbook kept the pass bounded and evidence-focused; its broader design/performance scoring and repair suggestions were not substituted for the constitution or the narrow reopening. Its read-only detector on the changed JS returned []. The completion-verification skill required actual command completion before this report. Known legacy-Landlock failure required scoped escalated RTK/apply_patch for scratch probes and checker evidence, not production.

## Independent preservation proof

Command: `rtk proxy node check-preservation.cjs`, cwd S, exit0, repeated after browser checks. Output checker-evidence/preservation.json.

- Current production page-refresh.js SHA256 d1875b727a3cf990a059538e443e7f1e1dfab799da1d2672dd61d9d7e8a5e115, matching declared candidate and canonical frozen bytes.
- All693 files under cmd/internal/web compared against O: only page-refresh.js differs. All18 prior Go test files unchanged. Templates, CSS, successful remount/cleanup/notice/confirmation/epoch code are not altered by this correction.
- Reviewed `diff -u O/web/static/js/page-refresh.js web/static/js/page-refresh.js`: only the fallback block changes. Absent tabindex gets owned -1; focus retains it until blur. Existing values are untouched. Attribute-only observer plus takeRecords handles later external writes, including same-value writes before blur. Cleanup disconnects/removes its listener and does not erase externally owned values. No newly created positive tabindex.
- Reviewed durable test diff: only `r.tabindex===null` becomes `r.tabindex==='-1'` while focused;24 post-blur checks are added. Original focus/value/status/region assertions remain intact.
- Cumulative manifest has474 entries and includes all396 prior entries.453 files are present in both supplied candidate and checker copy and equal canonical bytes. The21 missing files are older .swarm attempt3 evidence/manifests, NOT production or tests; they remain in the canonical directory.375 prior paths can be compared directly to O, with only the two authorized differences. All396 canonical prior hashes also match the pre-correction recorded baseline except those two. The latter21 association is to recorded baseline hashes, not a claim that the supplied immutable directory contained them. No missing source was patched in or silently copied from live.
- Oracle hashes independently pinned: accept.sh df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f; focus-transitions.cjs a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239.
- Prior full-site preparation P=/tmp/DI5-final-a11y.JYHU8N:678 source/config/standard hashes compared; only page-refresh.js and its test differ. Thus prior detailed unchanged Dashboard modal/chart and Insights populated chart/disclosure traces can be associated explicitly. Fresh full-site captures below were nevertheless executed.
- tailwind.css remains f09162945444f745061980d04d5d3eafd8c7c9d67c178381837555429adb6c5c.

## Executed commands and results

All commands cwd S unless indicated. Environment aliases S/O here are explanatory; full absolute paths are in scripts/logs and expanded below for the oracle. Cached Playwright /home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright, Chrome /home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome, Node24.12.0, installed axe-cli/chromedriver were used without downloads.

Build: `rtk proxy go build -buildvcs=false -o checker-server ./cmd/server`, exit0. Server command:

```
rtk proxy env BUDGET_DEBUG=true BUDGET_LISTEN_ADDR=127.0.0.1:18873 BUDGET_DATA_DIR=/tmp/DI5-4-a11y.xokOQB/checker-runtime/data BUDGET2_BACKUP_DIR=/tmp/DI5-4-a11y.xokOQB/checker-runtime/backups BUDGET2_IMPORT_DIR=/tmp/DI5-4-a11y.xokOQB/checker-runtime/imports BUDGET_TEMPLATES_DIR=/tmp/DI5-4-a11y.xokOQB/web/templates BUDGET_STATIC_DIR=/tmp/DI5-4-a11y.xokOQB/web/static ./checker-server
```

1. Complete unchanged oracle, independently launched against this server:

```
rtk proxy bash -lc 'set -o pipefail; DI5_SOURCE_ROOT=/tmp/DI5-4-a11y.xokOQB DI5_BASE=http://127.0.0.1:18873 DI5_OUTPUT=/tmp/DI5-4-a11y.xokOQB/checker-evidence/oracle bash /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights/.swarm/DI-run/tier3/DI5/accept.sh |& tee checker-evidence/oracle.log'
```

Exit0; last line ORACLE PASS.750 transition assertions across all nine consumers, both themes390/768/1440; zero failures. Exact surviving button focus after real main inner/outer replacement, settlement, cancelled/delayed non-theft, unrelated partial non-theft, dismissal/epoch ownership and no protected-transition reload pass. The oracle confirms the server JS equals the declared source. It uses labeled synthetic editing controls on real pages to avoid unrelated autosave writes; actual Dashboard inputs are additionally covered by the strict obstruction/lifecycle tests and independent probe below.

Included VM10/10; strict all-scripts-active F1 six states, hits=[] with46/46/54 Tab stops by width in each theme;12 main/body-fallback layout states; six lifecycle states, refusal/dismissal/restart/acceptance/clean reload true, URL/query/hash retained, four expected loads, no obstructions. No baseline allowlist. Raw evidence under checker-evidence/oracle/{focus-transitions.json,obstruction,layout,lifecycle} and oracle.log. These are this checker's executions, not copied lead-green summaries.

2. `rtk proxy bash -lc 'set -o pipefail; DI5_SOURCE_ROOT=/tmp/DI5-4-a11y.xokOQB DI5_BASE=http://127.0.0.1:18873 DI5_OUTPUT=/tmp/DI5-4-a11y.xokOQB/checker-evidence/ownership node .swarm/DI-run/reports/DI5-focus-ownership.cjs |& tee checker-evidence/ownership.log'`: exit0,252/252,42 per theme/width state. Existing228 assertions and24 blur checks pass.

3. `rtk proxy bash -lc 'set -o pipefail; DI5_SOURCE_ROOT=/tmp/DI5-4-a11y.xokOQB DI5_OUTPUT=/tmp/DI5-4-a11y.xokOQB/checker-evidence/fallback node .swarm/DI-run/reports/DI5-fallback-lifecycle.cjs |& tee checker-evidence/fallback.log'`: exit0,65/65. Absent/pre-existing -1/0/3 attributes, repeated fallback, Tab exit, existing value during focus, later changed/removed/same-value attribute with same-task blur all pass. Fixture positive3 is preserved existing state, not a new positive tab stop introduced by the repair.

4. Independently reran that SAME65-assertion script with DI5_SOURCE_ROOT=/tmp/DI5-final-worker.btR0cu and DI5_OUTPUT=S/checker-evidence/old-fallback; captured old-fallback.log with the same pipefail wrapper. Exit1, eight actual failures: four lost absent-tabindex fallback focus and four transient existing0/3 overwrites. Minimal trace observes MAIN before immediate removal, BODY after; retaining -1 leaves MAIN. No HTMX handler participates in that minimal diagnosis. Old source was not modified; this is a calibrated counterfactual, not a new candidate failure.

5. `rtk proxy bash -lc 'set -o pipefail; node independent-fallback.cjs |& tee checker-evidence/independent-fallback.log'`: exit0,12/12 actual Dashboard states (dismiss/epoch x light/dark x390/768/1440). This separate checker probe loads all page scripts, focuses an actual date input, then a notice action; real HTMX outerHTML uses captured actual main bytes without the notice, explicitly carrying the synthetic edited date. Exact original button retains focus after150ms settlement. Keyboard Enter dismissal or epoch reset moves focus to current MAIN with -1 still present after100ms; status/region are detached and edited date remains. Tab exits to a meaningful control inside main and removes the temporary attribute. Screenshots and state records are checker-evidence/fallback-*.png and independent-fallback.json. No server persistence claim is inferred from the deliberately supplied replacement date. This probe is available for later durable promotion.

## Fresh full-site final pass

Reused only the independently authored baseline/manual/CLI/responsive harnesses from P, copied into S with mechanical dedicated-port changes18797/18798 ->18873/18874. Their behavior was read before execution; no production modifications. Actual handlers and real built stylesheet, not invented HTML.

- `rtk proxy node baseline.cjs`:18 captures, nine routes x both themes1440x900, allHTTP200; no page/load/request errors or blocked requests. Exact routes: /dashboard, /explorer, /insights, /major-expenses, /whatif, /accounts, /transfers, /filemanager, /duplicates. Initial requests have no added query; each meta file records final URL. Captures serialize actual populated DOM after scripts execute; scripts removed only from saved CLI pages. Assets served unchanged on18874.
- `rtk proxy node run-axe.cjs`: exit0, npx --no-install @axe-core/cli against all18 actual-render URLs. Exact argv saved in artifacts/axe-command.json, complete raw artifacts/axe.json and axe-cli.log. Tags wcag2a,wcag2aa,wcag21a,wcag21aa,wcag22aa; chrome-1440.sh preserves the viewport argument; installed chromedriver. Zero violations,20 incomplete rule results/464 node occurrences (contrast plus Explorer table relationship). Incomplete is not a confirmed violation or a silently cleared manual check.
- `rtk proxy node manual.cjs`: exit0,18 route/theme states,65 Tab presses each; explicit label-content-name-mismatch rule zero. Fresh labels/hidden-text/chart trace/table/focus-outline metadata saved in artifacts/manual.json. Nine inherited explicit-label gaps remain (see below). No mutating controls activated. Conditional Filemanager plaintext modal absent; not fabricated or marked exercised.
- `rtk proxy node responsive.cjs`: exit0,12 Dashboard/Insights states at390/768/1440, light/dark; oneh1 and document width equals viewport in every state. Selected March1–August28 no-prior-history Insights state has no comparison chart; do not call this fresh populated-chart coverage.
- Batched fresh contact-light.png/contact-dark.png composed from all nine1440 screenshots and inspected. Independently inspected actual MAIN fallback screenshots at390 in both themes. No repeated polishing cycle. Full per-page and responsive screenshots, AX trees, headings, landmark and animation records retained in artifacts/.

## All17 constitution criteria: evidence and boundaries

1. Structure: fresh baseline metadata and responsive assertions; exactly oneh1 per page, no skipped visible heading levels, main/nav/header present. Explorer deliberately omits footer (not required by point1); inherited observation, not failure.
2. Tables: fresh header/axe inventory; preserved native sorting/table relationships. Explorer th-has-data-cells remains incomplete. Prior detailed Insights keyboard/sort evidence applies via exact source equality; no table changes.
3. Controls: fresh65-Tab role/native-control inventory on all pages; notice native actions and Tab/Enter behavior pass oracle and independent probe. No destructive action activation claim.
4. Labels: fresh label-name rule zero; explicit association inventory still finds original Explorer1, MajorExpenses1, What-If7 gaps. Not concealed by default axe placeholder fallback.
5. Validation: saved live/error/describedby/form metadata inspected; no prohibited failed writes submitted. Refresh refusal/confirmation and announcement lifecycle traced. Universal validation certification is not claimed.
6. Required/formats: fresh visible input metadata and inherited What-If placeholder context retained; no new fields or changed form instructions.
7. Contrast: both-theme real-CSS axe and visually inspected theme captures; raw incomplete contrast records preserved. Notice/button styling is byte-identical to previously measured DI5.2 styling; only fallback ownership changes. MAIN identity and subsequent visible keyboard control were traced independently. Prior changed Dashboard/Insights chart/color detail evidence associates through unchanged files; no blanket assertion that automation certifies every conditional/offscreen composited color.
8. Noncolor meaning: fresh AX/status/sign prose preserves textual outcomes, and notice warning/actions remain literal text. No new color-only messaging.
9. Keyboard/focus: fresh all-page Tab traces, strict F1 no-obstruction six states, and exact-button/MAIN/Tab-exit probe pass. No trap or newly positive tabindex. Existing external attributes are preserved, not normalized away.
10. HTMX:750 oracle plus252 ownership and separate actual-render probe pass main inner/outer, settlement, partial, cancellation/delay, original-input replacement, dismissal/reset fallback and non-theft. F2 and remaining fallback regression are corrected.
11. Chart alternatives: fresh data/adjacent table inventory; five Dashboard charts have alternatives. Current no-history Insights chart absence intentional; prior detailed populated Insights/Dashboard table-value evidence remains associated (/tmp/DI4-a11y.m9xka6, /tmp/DI4-2-a11y.kTazMP, /tmp/DI3-a11y.mBCqsi). What-If equivalence remains original-source observation below, not waived by merely finding its annual table.
12. Theme parity:18 fresh route/theme captures plus both-theme six-state refresh guards,12 responsive states and fallback states. Charts' current theme metadata preserved. No CSS/template/theme delta from attempt3.
13. Money/signs: fresh AX sign/context inventory; all financial production and18 Go tests byte-identical. This lane does not replace financial tests or claim lead race results as its own.
14. Banners: oracle lifecycle traces cover notice dismissal, new state/revision, hidden-tab deferral, epoch reset and confirmation. Same region/status preserved through remount; repeated partials do not recreate notice.
15. Motion: reduced-motion emulated; no active animations in the settled18 captures. Existing transient-loader observation remains state-limited. Fallback uses existing immediate visibility handling, no new animation or motion-only feedback.
16. AT parity: independent probe and ownership checks retain exact status/region through remount and remove both on dismissal/reset; no stale resurrection. Hidden-text inventory reviewed, contextual sr-only captions not treated as junk solely for being hidden. Node identity/removal evidence is not a claim of measured screen-reader speech counts.
17. Modals: notice remains nonmodal. No new dialog code; prior independently exercised Dashboard modal focus/trap/Escape/return remains applicable via exact template/JS equality. No absent conditional Filemanager modal or forbidden write workflow is asserted freshly exercised.

## Attribution / non-blocking observations

O1 Original69 attribution preserved, not merely postDI3 matching: checker-evidence/preservation.json legacy entries rehash the nine previously proven original69 source files. Explorer search (pages/explorer.html:15-17), MajorExpenses search (pages/major-expenses.html:53), and What-If quick-add source templates (income-sources-list.html:155, expense-sources-list.html:96, bigticket-card.html:109) equal their original69 whole-file hashes. These nine label gaps predate this run. What-If projection-chart.html, income-chart.html, projection-breakdown.html and styles.css also retain original hashes; chart-table equivalence and inherited motion caveats remain backlog observations. Fresh current What-If trace inventory does not claim there is no annual table anywhere.

O2 Historical caught regressions stay upheld: P/FOCUS-ATTRIBUTION.md and artifacts/obstruction preserve original69/preDI3/prior-final full-rectangle/hit-point comparisons for F1; it was run-introduced, not excused by acceptedDI3. DI5.2 main-focus.cjs old/new counterfactual preserves F2. Attempt3 fallback is freshly red under command4. All are repaired under current guards; their old verdicts are untouched. Read-only master diff previously showed page-refresh.js absent from master; an empty tracked diff was never used as evidence that these new-script defects were legacy.

O3 Supplied candidate evidence-package omission:21 old .swarm paths absent from immutable copy but present canonically, explicitly listed as null snapshot hashes in preservation.json. This does not invalidate tested production equality or authorize a live-tree copy. Lead should retain those canonical artifacts for gate/history. No application asset or test required to execute this review was missing.

O4 Zero axe violations is not full manual WCAG certification.20 incomplete checks, no actual screen-reader speech measurement, no destructive submissions, and conditional-state coverage limits are explicit above. These are not baseline exemptions for the focus guards: those run strictly, all scripts active where required, without allowlists.

Shutdown: resolved only checker PIDs2875510 (server18873) and2876668 (artifact listener18874), then `rtk proxy kill -TERM 2875510 2876668`. Scoped ss check found neither listener. Other reviewers' processes were not touched. Startup's initial-snapshot-in-progress warning is not backup acceptance evidence; runtime data/backups/imports were isolated.

Final decision: PASS within the explicitly reopened fallback-focus contract and preserved accessibility regression guards, with fresh full-site review and attributed backlog observations. Lead alone owns overall Tier3 gate/acceptance/deployment.
