# DI5 accessibility baseline sidecar

Baseline evidence only. **DI5 remains pending.** This is neither a DI4 review nor a task-acceptance verdict, and it does not certify WCAG or all 17 constitution points.

Captured 2026-09-06 starting 20:17:22 UTC from accepted immutable `/tmp/budget2-DI4-base.KPl7oY`. Fresh snapshot and reusable probes: `/tmp/DI5-a11y-baseline.Yd8pno`. All machine-readable artifacts: `/tmp/DI5-a11y-baseline.Yd8pno/artifacts`.

## Bounded result

- Nine main navigation pages, each captured and automatically audited once in light and dark: **18 page/theme audits**.
- **0 axe WCAG-tagged violations; 0 page load failures.** All route responses were HTTP 200, with no redirect, recorded failed HTTP response, failed browser request, page error or blocked write/external request.
- **20 incomplete axe rule results, covering 678 node occurrences** across themes/pages. These are unresolved automated checks, not 678 confirmed defects: color contrast on every capture, plus `th-has-data-cells` on Explorer in both themes.
- **Seven bounded baseline observation groups** below. They are for later attribution and review, not new task failures or repair authorization.

## Exact route inventory and results

Inventory came only from the immutable `web/templates/layouts/base.html:66` navigation block. `/duplicates` is conditionally displayed when unresolved duplicates exist; it was included. The brand `/` link is an alias, not a separate navigation page.

Each exact route below was requested at `http://127.0.0.1:18789` without added query parameters. Final URL equaled requested URL. Columns show light/dark results.

| Route | HTTP | Axe violations | Incomplete node occurrences |
|---|---|---|---|
| `/dashboard` | 200 / 200 | 0 / 0 | contrast 91 / 91 |
| `/explorer` | 200 / 200 | 0 / 0 | contrast 13 / 13; table-header check 1 / 1 |
| `/insights` | 200 / 200 | 0 / 0 | contrast 120 / 120 |
| `/major-expenses` | 200 / 200 | 0 / 0 | contrast 13 / 13 |
| `/whatif` | 200 / 200 | 0 / 0 | contrast 34 / 34 |
| `/accounts` | 200 / 200 | 0 / 0 | contrast 12 / 12 |
| `/transfers` | 200 / 200 | 0 / 0 | contrast 29 / 29 |
| `/filemanager` | 200 / 200 | 0 / 0 | contrast 13 / 13 |
| `/duplicates` | 200 / 200 | 0 / 0 | contrast 13 / 13 |

## Isolation and actual-render method

Copied immutable baseline with `cp -a` to a fresh temp directory. Did not copy or inspect DI4's live Insights source or generated CSS. No live source, git, ledger or verdict changes; this report is the only live-worktree write. No subagents.

Built `baseline-server` from the fresh snapshot. Copied only authorized synthetic `/tmp/budget2-DI-demo.mfLral/data` with `cp -aL` into snapshot `runtime/data`; used independent `runtime/imports` and `runtime/backups`. The app listened only on `127.0.0.1:18789`. No :8080/:8081 or real-data access, user browser session, form submission, upload, deletion or edit was performed.

Browser skill was already read in this conversation; the explicitly authorized isolated headless fallback was used. Fresh context per route/theme, Chromium 153.0.8010.12, Playwright, Node v24.12.0, viewport 1440x900, `reducedMotion: reduce`. Allowed only same-origin GET/HEAD; all other requests would be blocked and recorded. Set the requested theme and emitted the application's themechange event. Waited a bounded two seconds after DOM load, then saved actual rendered DOM, accessibility tree, viewport screenshot and metadata. No modal/disclosure/form interaction or repeated polishing cycle.

The actual populated Go-template/handler DOM was serialized, with executable scripts removed, and served on a second dedicated artifact-only listener `127.0.0.1:18790`. Original snapshot CSS/assets were served unchanged. Ran **one** `npx --no-install @axe-core/cli` audit per saved route/theme, using WCAG tags `wcag2a,wcag2aa,wcag21a,wcag21aa,wcag22aa`. This audited actual rendered pages, not template source or guessed HTML/CSS. Axe-core 4.13.0 reported exit 0. Its raw environment confirms width 1440 for every audit; headless window chrome gives audit height 757, while the original captures were 1440x900. Width, theme and stylesheet are constant within the requested baseline comparison.

The Chrome launcher wrapper preserves `--window-size=1440,900` as one argument; the CLI's own comma-delimited chrome-options parser otherwise splits it. Saved `axe-command.json` is the exact executed command/argument vector.

## Baseline observations, with source attribution

All source paths/lines in this section refer to `/tmp/budget2-DI4-base.KPl7oY`, also preserved in the fresh copy. None describe unreviewed live DI4 work.

1. **B1, point 4 — Explorer explicit label association:** `web/templates/pages/explorer.html:15` renders a visible Search label without a `for` relationship to `#search-input` at line 17. DOM reports no associated label, aria-label or aria-labelledby. Placeholder supplies fallback text; zero axe violations does not establish the constitution's explicit association requirement.
2. **B2, point 4 — Major Expenses search:** `web/templates/pages/major-expenses.html:53` has placeholder text but no associated label/aria-label/aria-labelledby for `#major-expenses-search`.
3. **B3, points 4/6 — What-If quick-add fields:** initial DOM records seven visible inputs without associated labels/aria-label/aria-labelledby. Sources include `components/whatif/income-sources-list.html:155`, `expense-sources-list.html:96`, and `bigticket-card.html:109` (all under `web/templates`). These add-income, add-expense and big-ticket fields use placeholders. No validation or form behavior was exercised.
4. **B4, point 11 — Chart alternative inventory needs state-aware follow-up:** all five Dashboard charts have generated adjacent table disclosures. Transfers has a table in its chart parent. Captured Insights `#chart-trends` and What-If chart containers have neither generated `id-data-table` nor a table in their immediate parent. Insights source **does** contain a conditional CategoryTrends table after `web/templates/pages/insights.html:329`; it was not present in the captured initial state. What-If references are `components/whatif/projection-chart.html:35` and `income-chart.html:24`. This bounded structural scan does not establish that every possible alternative is absent or that present tables match every plotted value. Preserve these captured states for later comparison.
5. **B5, points 9/16 — Hidden disclosure inventory:** `web/templates/pages/major-expenses.html:131` and `:134` use `sr-only` summaries “Your Major Expenses — definitions list” and “Add a major expense.” Recorded as hidden/focusable content for later keyboard/suppression review, not adjudicated as abusive text. Accounts/Transfers also use screen-reader-only captions, required-field text and contextual names; their existence alone is not a violation.
6. **B6, point 15 — Reduced-motion loading state:** What-If light capture included a 1s `spin` and 2s `pulse` loader despite reduced-motion emulation. Dark capture had settled and contained neither. This is a bounded asynchronous loading/cache difference; duration, persistence and conformance were not investigated. Do not attribute this difference to theme CSS without further evidence.
7. **B7, structure context — Explorer footer:** `web/templates/layouts/base.html:212` intentionally omits the footer for Explorer. All pages have main/nav/header. Point 1 explicitly requires those three, so this footer note is inventory rather than a point-1 failure.

## Coverage against all 17 constitution points

This records what was observed, not a series of PASS claims.

| Point | Baseline evidence / boundary |
|---|---|
| 1 | One visible h1 per capture; visible heading lists and landmark counts saved. No skipped level apparent in captured hierarchy. Explorer footer exception above. |
| 2 | Zero visible unscoped th elements in captured DOM. Explorer axe table-header relation remains incomplete. Sorting and table interaction not exercised. |
| 3 | Native/role-button/onclick inventory saved; no activation or action-vs-navigation certification. |
| 4 | Explicit input-label inventory saved; B1–B3 identify association gaps despite fallback names/default axe outcome. |
| 5 | Live/error regions recorded where present. No failed form submissions; error announcement/focus untested. |
| 6 | Required-state metadata and source placeholder patterns recorded; formats/required instructions not exhaustively evaluated. |
| 7 | One WCAG contrast audit per page/theme; raw incomplete cases retained. No exhaustive chart-stroke, boundary, composited-color or manual contrast certification. |
| 8 | Accessibility trees preserve status/sign prose; color-only meaning was not manually tested across every state. |
| 9 | Initial clickable/focus metadata available. No Tab sweep, keyboard reachability or focus-indicator certification in this sidecar. |
| 10 | Live-region metadata saved; no HTMX input changes, destructive operations or focus-restoration tests. |
| 11 | Chart/table presence/readiness inventory recorded (B4); no plot-value/table-equivalence certification. |
| 12 | Same built assets in both themes. Populated Plotly fonts observed switching from #374151 to #e5e7eb; complete chart/control contrast parity untested. |
| 13 | Initial accessible text retains rendered signs/context; no signed-refund/currency edge-case fixtures or numeric parity testing. |
| 14 | Banner/live-region initial state captured. No dismissal, session persistence or repeat-announcement checks. |
| 15 | Reduced motion requested; visible animation metadata saved, including transient What-If loaders (B6). No prolonged observation or interaction animation checks. |
| 16 | Hidden-text/live-region inventories captured; no suppression action or visual/AT parity test. Native captions/required text are not presumed junk. |
| 17 | Initial dialog inventory saved; no modal opening, trapping, Escape or return-focus tests. |

## Artifacts and exact hashes

`artifacts/summary.json` contains counts, exact original/final routes, errors, per-theme axe counts and seven observation objects. Other reusable artifacts:

- `navigation.json`, `environment.json`, `capture-summary.json`.
- `axe.json` (complete raw violations/incomplete/passes with selectors and HTML), `axe-cli.log`, `axe-command.json`.
- Per `{route}-{theme}`: `.html`, `.ax.txt`, `.png`, `.meta.json`.
- `source-hashes.json`: 674 source/config/standard files, relative paths, byte lengths and SHA256, read from the immutable baseline and compared to the copy for source files. Source extension scope: cmd/internal/web Go, HTML, JS/CJS, CSS plus ACCESSIBILITY.md, go.mod, go.sum, Makefile, tailwind.config.js; generated internal/config/data excluded. This is a source manifest, not a real-data inventory.
- `artifact-hashes.json`: hashes of captured artifacts, including the source manifest.

Key SHA256 values for later attribution:

| Immutable path | SHA256 |
|---|---|
| `web/static/css/tailwind.css` | `e12bfb73c1d79b1a372f2e8e202ec271898963d1c0a619c3439a6d1dfa7a2637` |
| `web/static/css/styles.css` | `2128e66326f1080a237fecbb3531ec2ea3293d82ebcf0f0fbb106caae6f3caf3` |
| `web/templates/pages/insights.html` | `535aa7bcd5e46e65443bdde6e7404db82f12626a5d1a1c4001db908a5e26d45d` |
| `web/static/js/insights.js` | `ceb18537b1bbdea8691556cf130d7908c68f5ca040c22c5302dea07f768d1591` |
| `web/templates/layouts/base.html` | `16019f58797fe797debc4d181021312927b304a531a17e02bbdc368237d8777d` |
| `ACCESSIBILITY.md` | `5eaf322961a8af9da9579ccdaabd89728cb442f3eedd9b0142fc1866ebd1e0b4` |

Use immutable source/these hashes and exact saved selectors when attributing later differences. Do not compare against concurrently edited live CSS/Insights as though it were this baseline.

## Reusable harness and rerun prerequisites

Scratch harnesses preserved at snapshot root: `baseline.cjs`, `chrome-1440.sh`, `run-axe.cjs`, `summarize.cjs`. Prerequisites: the immutable baseline, authorized synthetic fixture, Go dependencies, Node, RTK, cached Playwright at `/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright`, Chromium `/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome`, installed @axe-core/cli and its chromedriver. Nothing downloaded or updated. No CSS regeneration.

To rerun, use a fresh copy of the same immutable baseline, copy these four scratch harness files, and recreate independent synthetic data/import/backup directories. `baseline.cjs` supports DI5_BASE/DI5_PLAYWRIGHT/DI5_CHROME; its immutable source and artifact-listener port are explicit constants. Keep all ports dedicated and never use 8080/8081.

Commands executed (cwd `/tmp/DI5-a11y-baseline.Yd8pno`; run server in its own session):

```bash
rtk proxy go build -o baseline-server ./cmd/server
rtk proxy env BUDGET_DEBUG=true BUDGET_LISTEN_ADDR=127.0.0.1:18789 BUDGET_DATA_DIR=/tmp/DI5-a11y-baseline.Yd8pno/runtime/data BUDGET2_BACKUP_DIR=/tmp/DI5-a11y-baseline.Yd8pno/runtime/backups BUDGET2_IMPORT_DIR=/tmp/DI5-a11y-baseline.Yd8pno/runtime/imports BUDGET_TEMPLATES_DIR=/tmp/DI5-a11y-baseline.Yd8pno/web/templates BUDGET_STATIC_DIR=/tmp/DI5-a11y-baseline.Yd8pno/web/static ./baseline-server
rtk proxy node baseline.cjs
rtk proxy chmod +x chrome-1440.sh
rtk proxy node run-axe.cjs
rtk proxy node summarize.cjs
```

Wait for `ARTIFACTS READY :18790` before running axe. `run-axe.cjs` executes the saved exact `rtk proxy npx --no-install @axe-core/cli` command across 18 URLs; its exit status is an automation result, not a task verdict. The capture server remains running until deliberately stopped. Stop both dedicated processes at handoff. The known legacy-Landlock error required scoped escalated RTK/apply_patch for final snapshot analysis and this sole live report write; no approval rejection.

Initial-state timing, hidden/closed panels, empty conditional tables, keyboard operations, screen-reader speech, live regions, forms, destructive actions and chart equivalence remain explicit limitations. No DI5 PASS verdict was written; DI4 named review still awaits its own frozen handoff.
