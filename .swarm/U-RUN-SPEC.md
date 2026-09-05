# SPEC.md — budget2 UI audit remediation (U run)

Run prefix: **U** (UI). Target repo: `/home/darrell/bin/ai/budget2`
(all manifest paths and globs are budget2-repo-relative). Source of truth
for the findings: the 2026-09-03 UI audit artifact
(https://claude.ai/code/artifact/40a73b41-9b02-40a1-a5b8-c2375b3dbd57,
private — embeds the user's real figures). Counts below were re-measured
on 2026-09-03 against the budget2 working tree and match the audit exactly
(45 / 20 / 548 text sizes, 30 inline scripts, 9 `!important`, h1 counts).

Previous run's spec (CC — per-person care costs, closed 2026-09-01) is
preserved in git history on master. This run's `.swarm/` lives in THIS
agents2 worktree (gitignored), as the CC run's did.

## 0. Status — APPROVED 2026-09-03

User signed off on all §5 tiers/checks and took every §7 default (D1–D8),
including merging budget2 PR #89 before branching and adopting
ACCESSIBILITY.md point 17. Mid-run, tiers move only up.

## 1. Motivation

The audit's verdict: the app is feature-complete and visually consistent;
what holds it back is (a) six layouts that render wrong at real widths,
(b) a 10 px type floor for a primary reader who is 67, and (c) page
structure that puts setup forms and long lists ahead of the task the user
came to do. Seventeen findings fold into fifteen tasks. Six touch money
surfaces and carry the `second` lane. One (Clear All) is irreversible and is
Tier 3.

## 2. Design decisions

Each subsection is the contract a worker builds to and a checker checks
against. Where a decision is still the user's, §7 records the default.

### 2a. Tokens: palette and type (U6)

**Palette.** Twelve Tailwind hue families currently carry four meanings
(indigo 390 / red 363 / amber 289 / green 255 / emerald 153 / blue 151 /
rose 148 / purple 41 / orange 15 / yellow 14 / cyan 8 / sky 2 class sites).
One semantic set, defined ONCE as CSS custom properties in
`web/static/css/styles.css` (light + `.dark` values) and exposed through
`tailwind.config.js` `theme.extend.colors` so templates name the meaning:

| Token | Meaning | Light hue (from today's dominant family) |
|-------|---------|-------------------------------------------|
| `accent` | interactive / brand | indigo |
| `positive` | income, under budget, gain | emerald |
| `negative` | outflow, over budget, loss, destructive | rose |
| `warning` | caution, stale, unresolved | amber |
| `neutral` | chrome, muted text, rules | gray |

Rules: red→negative, green→positive, blue/purple→accent, yellow/orange→
warning, cyan/sky→accent or neutral (worker picks per site, no new hue).
Acceptance counts hue families remaining in `web/templates/**`: ≤ 6
(the five above plus `white`/`black`/`transparent` utilities, which don't
count as families). Every text-on-fill pair must meet 4.5:1 in BOTH
themes — the audit did not measure light-mode contrast; U6's axe run does.

**Type (contract v2, 2026-09-03, after two same-class FAILs).** Floor of
**12 px** for label-class content and **14 px** for anything read as a
sentence, expressed as two tokens in `tailwind.config.js` `fontSize`
(`label` = 0.75rem/1rem tracking-wide; `body-sm` = 0.875rem/1.25rem).
`text-[10px]` and `text-[11px]` go to ZERO. "Label-class" is decided
MECHANICALLY by `.swarm/tier3/U6/typefloor_allowlist.py`, not by sampling:
a surviving `text-xs` element is DENIED (must become `text-body-sm` or
larger) when any of — R1 its tag is `p table tbody td details li ul ol dl
dt dd blockquote section article h1–h6`; R2 it contains a `<p>`, `<table>`,
`<li>` or heading descendant; R3 any single text run inside it (tags and
template actions are run boundaries) has ≥ 6 words or ends with a period;
R4 it renders a template variable named like prose (Rationale, Summary,
Explanation, Reason, Message, Note, Hint, Help, Blurb, Caption, Warning,
Verdict, Sentence, Advice, Insight, Tip). `th`, `label`, `button`, `input`,
`select`, `summary`, `code`, badges, eyebrows and short spans/divs stay
allowed. The script must report 0 denied elements. Tailwind must be
rebuilt (`make css`) and `make css-verify` must pass.

**Rendered bar (contract v2).** Both probes run with EVERY `<details>`
opened — collapsed panels are user-visible content and axe skips hidden
nodes: (i) zero axe `color-contrast` violations on all 9 pages in both
themes at 1440; (ii) no horizontal overflow (`documentElement.scrollWidth
≤ viewport`) on all 9 pages in both themes at 1440 and 1280. A size bump
that pushes a table past its card must be fixed at the table (wrap it in
`overflow-x-auto` with `tabindex="0" role="region"` and an accessible name,
or tighten columns), never by shrinking the type back.

### 2b. Shared partials and script extraction (U7)

New partials under `web/templates/components/shared/`:
`range-picker.html` (dates + one quick-button set; the Dashboard-only
`comparison` selector is a slot, not part of the partial), `kpi-tile.html`
(label, value, delta, optional sparkline `<canvas>`/`<svg>` slot — ONE
sparkline style), `card.html` (shell: title, optional actions slot, body),
`data-table.html` (`<table>` with `<th scope="col">`, responsive column
classes, optional sortable-header buttons). Every page that has one of
these uses the partial; the checker greps for surviving hand-rolled copies.

Scripts: the 30 inline `<script>` blocks move to `web/static/js/<page>.js`
(one file per page, behavior attached by `id`/`data-*` hooks, no inline
`onclick=`). Allowance: **≤ 5** inline blocks may remain, and the theme
bootstrap in `layouts/base.html` (must run before first paint to avoid a
flash) is one of them; each survivor gets a one-line comment saying why.
The 9 `!important` rules in `styles.css` are removed by loading
`styles.css` AFTER `tailwind.css` in `base.html` (ordering, not weight);
acceptance is zero `!important` in `styles.css` with the visual result
unchanged on the affected elements.

### 2c. One date range across pages (U8)

All four range-bearing handlers already read `start` and `end` from the
query string (dashboard, explorer, insights, majorexpenses — verified
2026-09-03). Contract: the range lives in the **query string** (the URL
says what window you are looking at; no invisible session state). The
range partial (2b) renders it; the nav links in `base.html` propagate the
current `start`/`end` when present, via one template func
(`withRange href`) — so Dashboard → Explorer keeps the window. Insights'
`preset` and Dashboard's `comparison` stay page-local. Pages without a
range (What-If, Accounts, Transfers, File Manager, Duplicates) drop the
params. MCP tools take their own explicit params and are untouched;
the checker confirms by running the MCP tool tests unchanged.

### 2d. Zero-baseline change display (U5) — contract v3 (2026-09-03, after two same-class FAILs)

Today `internal/services/insights/trends.go` sets `changePercent = ±100` when
`previous == 0` and otherwise `change / |previous| * 100`, so a $0→$X row
prints "+100.0%" and a $30→$6,931 row prints "+23004.0%". Attempts 1 and 2
failed to the same class (ruling 2026-08-29b — displayed figures that
contradict): first the dollar delta was derived from unrounded floats;
then a float-noise sum (0.10+0.20−0.30 ≈ 5e-17) in `MajorExpenseTrends`
rendered $0.00 → $0.00 as "+100.0%" with an up arrow, because the
producer classified on raw floats while the display rounded. The root
cause is that money becomes a FIGURE at the producer, and rounding must
happen there, once, before anything is derived.

**Rule 1 — round at the source.** Both producers (`CategoryTrends`,
`MajorExpenseTrends`) pass every `current` and `previous` total through
`models.RoundToCents` (the `fmt.Sprintf("%.2f")` primitive `formatMoney`
uses) immediately after summation. No unrounded money leaves a producer.

**Rule 2 — one classifier.** `ChangeDisplay(previous, current float64)
models.ChangeCell` takes the ROUNDED pair only (no percent input) and
computes everything downstream:
- `change = current − previous`.
- `pct`: if `previous == 0` → +100 when `current > 0`, −100 when
  `current < 0`, 0 when both are 0; else `change / |previous| * 100`.
- `Kind`: `previous == 0 && current > 0` → **new** (text "new");
  `previous == 0 && current == 0` → **none** (text "—", accessible text
  "no change"); `previous ≠ 0 && |previous| < ChangeDollarFloor (100)`,
  or `previous == 0 && current < 0` → **dollar** (signed, via the
  formatMoney path); otherwise → **percent** (signed, one decimal).
- `Direction`: from `pct` with the existing thresholds (> 5 up, < −5
  down, else stable); **none** rows are stable.
The cell carries `Kind, Text, Amount, Percent, Direction`. Producers set the
row's `ChangeAmount`, `ChangePercent` and `Direction` FROM the cell, so the
MCP tool, the arrow/color, and the cell share one source. Templates render
the arrow/color from the same row `Direction` (no template branches on a
raw number).

**Rule 3 — self-consistency is the tested property.** A property test
drives the REAL producer path (transactions → producer → templates at BOTH
sites) with a few thousand random pairs including fractional cents,
negatives, float-noise sums, and values within ±0.006 of the floor, and
asserts on the RENDERED strings: (i) dollar rows: Change equals Current −
Previous parsed from the displayed strings; (ii) if displayed Current ==
displayed Previous then Kind is none (prev 0) or percent "0.0%" and
Direction is stable; (iii) new rows: displayed Previous is "$0.00" and
Current ≠ "$0.00"; (iv) percent rows: the displayed percent equals the
percent recomputed from the displayed dollars, to one decimal. Named
fixtures stay: 30.005/6931.004, 0/12.345, 250.555/300.001, 99.995/150.005
(percent), 40.00/140.115 (dollar, rounding divergence), and the
MajorExpenseTrends float-noise case (0.10+0.20−0.30 with no prior
activity → "—", stable).

**MCP.** `get_trends` `change_percent`, `change_amount` and `direction`
now derive from the rounded totals: identical to before for every
cent-valued input (existing spend tests pass unchanged) and different only
where float noise previously produced a phantom ±100 / "up". Consequently
`change_amount` equals the UI's dollar delta EXACTLY (both are
rounded-current − rounded-previous). The tool description must state,
accurately against the code (checker-tests attempt-2 findings d-1..d-3):
`change_percent` is +100 when `previous_amount` is 0 and `current_amount`
is positive, −100 when it is negative, 0 when both are 0; the web UI shows
"new" for the first case, "—" ("no change") for the last, a signed dollar
delta when `previous_amount` is 0 and `current_amount` is negative or when
0 < |`previous_amount`| < $100, and a percent otherwise; and
`change_amount` is the same figure the UI's dollar delta shows. No other
threshold wording (the |previous| rule lives in ONE place — say "the
absolute value", not "under $100" without it).

Surfaces the checker ENUMERATES: `insights.html` both Change cells and
both Direction/arrow sites, `data-change` attributes (+ `sortTrendsTable`,
sort-only), MCP `get_trends`, and any other renderer of these fields.

### 2e. Navigation and one product name (U9)

Three groups in both the desktop bar and the mobile menu: **Money**
(Dashboard, Explorer, Insights, Major Expenses), **Plan** (What-If),
**Setup** (Accounts, Transfers, File Manager, Duplicates — Duplicates keeps
its unresolved-count badge). Groups are labelled (visible text at desktop,
headings in the mobile menu), keyboard-operable, `aria-current="page"` on
the active link. One product name in header, footer and `<title>`
(§7 decision; default **simpleBudget**, the name ACCESSIBILITY.md and the
repo already use).

### 2f. Dashboard verdict band and drop-zone (U10)

Net Savings moves OUT of the verdict band into the KPI row (§7 default)
with a label that says what the figure is. The figure is
`metrics.Calculate`'s `netSavings = totalIncome − |all outflows|`
(`internal/services/metrics/metrics.go:379`) over the FULL outflow set
regardless of plan-exclusion flags. The worker derives the label from
that code and cites the lines in the manifest note; `checker-content`
verifies the label against the code, not against this spec's paraphrase
(candidate: "Net savings — income minus every outflow, including
transfers and one-time"; if transfers are NOT in the outflow set the
candidate is wrong and the worker must say so). The verdict sentence and
its classification are unchanged for the same data. The CSV drop-zone
becomes a header-level button on the Dashboard page header (dashboard.html,
not base.html — U9 owns base.html); whole-page drag-and-drop still imports.

### 2g. Page structure (U11, U12, U13)

- **Major Expenses (U11):** one column, Exceptions panel first at full
  width; Definitions below as a searchable list collapsed by default; the
  delete control exists only inside an expanded definition row; the page
  title moves to the same position as every other page (above the filter).
  Existing filter and pin handler tests are the regression oracle.
- **Accounts (U12):** each account renders as a summary card (name, kind,
  matched-file count, latest anchor) with an Edit toggle that reveals the
  existing form; "Add an account" is behind a button; exactly one `<h1>`
  (page has 3 today; `components/accounts_card.html` carries one). Existing
  accounts handler tests green; handlers untouched.
- **What-If rail (U13):** removing an income source shows an Undo toast
  (button posting to the existing `/whatif/income/{id}/restore`) that
  persists until dismissed or the next full page load; the permanent
  "Recently Removed" list is removed (§7 default; note the trade-off there).
  Person card: name once (header), `Source:` shown only when not manual;
  slider value beside its label; Quick Adjust FAB reserves bottom padding so
  it never covers the last card at 1440 or 390.

### 2h. Clear All (U14 — Tier 3)

Today `hx-delete="/data/all"` with a generic `hx-confirm`, styled as a text
link beside "Load Test Data"; the handler
(`internal/handlers/backup/handlers.go:535 HandleDeleteAllData`) deletes
every `.csv` in the data dir via the storage service. Contract:

1. A secondary **button** (`<button>`, ≥ 24×24, negative token), visually
   separated from Load Test Data.
2. The confirm names the count: "Delete all N data files? …" where N is
   server-rendered.
3. **Server-side count confirmation (§7 default yes):** the request carries
   `expected_count=N`; the handler recounts and refuses with 409 and deletes
   NOTHING when the count differs or the field is missing. This makes
   "a cancelled or stale confirm deletes nothing" an executable oracle
   (curl), not a browser-only claim.
4. Encryption card collapsed by default behind a disclosure button
   (`aria-expanded`), expanded state announced.
5. Storage suite green at uid 0 and non-root (P1 precedent).

### 2i. Accessibility cluster (U15) and a proposed ACCESSIBILITY.md point

Zero clickable `div`/`span`/`tr` (15 sites today, every KPI card among
them — links navigate, buttons act; a whole-row link uses an `<a>` in the
first cell plus a row-level JS enhancement, never `onclick` on the `<tr>`).
Four overlays (kpi-detail, kpi-month-detail, major-expense-drilldown, File
Manager plaintext modal) get `role="dialog"`, `aria-modal="true"`,
`aria-labelledby`, focus moved in on open and restored on close, Tab
trapped inside, Esc closes. One `<h1>` per page (Transfers has 2).
Acceptance: axe zero violations sitewide, both themes.

**Proposed amendment to budget2 `ACCESSIBILITY.md` (user sign-off — it is
the checkers' constitution):** add point 17 so the modal contract is
citable rather than an implicit companion requirement (point-16 precedent):

> 17. Modal overlays are dialogs. Any layer that blocks the page carries
>     `role="dialog"` (or `alertdialog`), `aria-modal="true"`, an accessible
>     name, moves focus into itself on open and back to the invoking
>     control on close, traps Tab, and closes on Esc.

Checkers run against budget2's `ACCESSIBILITY.md` (16 points, plus 17 if
approved). This repo's own ACCESSIBILITY.md governs the swarm dashboard,
not budget2. No content is migrated, so no SOURCES.md.

## 3. Out of scope (this run)

Chart library changes beyond legend/axis placement; any change to what a
figure IS (only how it is laid out, labelled or floored); MCP tool
numbers; Transfers chart "Loading chart…" (unverified capture artifact);
screen-reader session testing of the modals (markup is the deliverable;
the `Accessibility Auditor` agent may be used for a manual pass after U15).

## 4. Worker constraints (paste into every dispatch)

- Repo: `/home/darrell/bin/ai/budget2`. Work ONLY in the run worktree
  `/home/darrell/bin/ai/budget2/.claude/worktrees/ui-audit` on branch
  `feat/ui-audit` (lead creates it before dispatch; workers commit nothing —
  the lead commits). The main checkout is on another session's branch and
  is OFF LIMITS: never `git checkout`, `stash`, or touch its index.
- **Never run the built budget2 binary** — any invocation, even `--help`,
  starts a server and kills the live :8080 instance. `go build ./...`,
  `go vet ./...`, `go test ./...`, `staticcheck ./...` only. For rendered
  checks use `scripts/whatif-verify.sh start 8099` (throwaway data copy,
  `/killme` teardown) or the demo instance on :8081
  (`~/bin/ai/budget2-demo/run-demo.sh`), never :8080.
- Run tests bare, or with `set -o pipefail` — never `go test … | grep`.
- Templates changed → `make css` and commit the rebuilt
  `web/static/css/tailwind.css`; `make css-verify` must pass.
- One formatter per value (`formatMoney` path), one rule per threshold,
  one source per figure. Assert on RENDERED strings.
- ACCESSIBILITY.md (budget2) points apply to every element you touch;
  both themes.
- Write your manifest to
  `<agents2 worktree>/.swarm/manifests/<task>.<attempt>.files`
  (budget2-repo-relative paths, one per line).

## 5. Task breakdown

Tier per TIERS.md (oracle, reversibility, blast radius); `second` wherever
a wrong figure on screen would be a lie. Checks name the ledger column.

| ID | Task | Fixes | Files | Tier | Checks | Why this tier | Acceptance criteria |
|----|------|-------|-------|------|--------|---------------|---------------------|
| U1 | Explorer responsive columns: hide Source and Major Expense below `md`; keep Date, Description, Amount; Description wraps | F1 | `pages/explorer.html` | 1 | a11y | Strong oracle (viewport render + axe), one page, reversible. | (a) At 390 px Date, Description, Amount visible, no horizontal scroll (`document.documentElement.scrollWidth <= 390`). (b) axe clean at 390 and 1440, both themes. (c) Column headers still `<th scope="col">`. |
| U2 | Dashboard KPI row: four columns with Budget card spanning two; ONE sparkline style | F2 | `components/kpis.html`, `static/js/charts.js` (corrected, ruling c) | 2 | a11y,second | Weak oracle (overlap is visual) and five money figures re-rendered. | (a) No text node's bounding box intersects a sparkline box at 1280, 1440, 1920 (headless probe). (b) Every KPI figure string is byte-identical to master's for the same data (verify server on a fixed data copy, curl both trees, diff the `.num` texts). (c) One sparkline implementation (grep: one draw function, one class). (d) axe clean both themes. |
| U3 | Insights: tables stop clipping; trend-chart legend clear of tick labels | F3 F4 | `pages/insights.html`, `static/js/charts.js` | 2 | a11y | Weak oracle, one page; figures re-laid-out but not re-formatted. | (a) Annual column fully visible at 1440 (no `overflow` clip; cell text bounding box inside card). (b) Legend bounding box intersects no x-tick label box. (c) Chart's data-table alternative (point 11) unchanged. (d) axe clean both themes. |
| U4 | What-If projection card on small screens | F5 | `components/whatif/projection-chart.html` | 2 | a11y | Weak oracle, one component. | (a) At 390 px both dollar-mode toggle labels render in full (no clipping/ellipsis; each label's scrollWidth ≤ clientWidth). (b) Chart x-axis visible; Plotly `responsive: true`, container full width. (c) `whatif-tabs.js` tab persistence still works (existing test or probe). (d) axe clean both themes. |
| U5 | Zero-baseline change display (§2d) | F6 | `internal/services/insights/trends.go`, `pages/insights.html`, `mcpsvc/spend/trends.go` (doc only) | 2 | tests,second | Threshold applied to a figure on multiple surfaces — split-classification class; money copy. | (a) `previous == 0` renders "new"; `0<|previous|<$100` renders signed dollar delta via `formatMoney`; else percent. (b) ONE Go function; checker enumerates §2d surfaces and proves each consumes it. (c) Table test with fractional-cent fixture asserts RENDERED strings at both template sites. (d) MCP `change_percent` numerically unchanged (existing tests pass untouched) and its description states the rule. (e) `go build/vet/test/staticcheck` green. |
| U6 | Design tokens and type floor (§2a) | F7 F8 | `static/css/styles.css`, `tailwind.config.js`, `templates/**`, rebuilt `tailwind.css` | 2 | a11y | Shared blast radius (every page); reversible; oracle is grep counts + axe. | (a) `grep -r 'text-\[1[01]px\]' web/templates` = 0. (b) Semantic tokens defined once; `tailwind.config.js` maps them; hue families in templates ≤ 6. (c) axe reports zero contrast violations on all 9 pages in BOTH themes. (d) `make css-verify` passes. (e) Sample of 20 surviving `text-xs` sites: none sentence-class. |
| U7 | Shared partials and script extraction (§2b) | F9 | `components/shared/**`, `static/js/**`, `layouts/base.html`, `styles.css` | 2 | tests,a11y | Shared blast radius; handler/template tests are the oracle. | (a) Range picker, KPI tile, card, data table partials used on every page that has one (grep: no hand-rolled duplicates). (b) Inline `<script>` blocks in templates ≤ 5, each with a why-comment. (c) `!important` in `styles.css` = 0; stylesheet order verified. (d) Full `go test ./...` green. (e) axe clean sitewide both themes. |
| U8 | One date range across pages (§2c) | F11 | `layouts/base.html` (nav links), `components/shared/range-picker.html`, the four range handlers/templates | 2 | tests,second | The range decides every figure on four pages; a wrong window is a wrong number everywhere. | (a) Set a range on Dashboard, follow nav to Explorer, Insights, Major Expenses: identical `start`/`end` in URL and rendered range text. (b) Handler tests for propagation and for pages that drop the params. (c) `comparison`/`preset` remain page-local. (d) MCP tool tests unchanged and green. (e) Checker attempts to produce a window mismatch and fails. |
| U9 | Nav grouping and one product name (§2e) | F10 | `layouts/base.html` | 1 | a11y | Strong oracle (axe + string check), one file, reversible. | (a) One name in header, footer, `<title>` (grep: zero "Financial Dashboard"/"Budget Dashboard" unless it IS the chosen name). (b) Three labelled groups at desktop and in the mobile menu. (c) Menu keyboard-operable, `aria-current` on active link, `aria-expanded` toggle intact. (d) axe clean both themes. |
| U10 | Dashboard verdict band and drop-zone (§2f) | F12 | `components/dashboard-verdict-bar.html`, `components/kpis.html`, `pages/dashboard.html` | 2 | content,second | Money copy: the label must match what the figure actually is. | (a) Net-savings label's definition confirmed against `metrics.Calculate` by checker-content (code citation). (b) Figure string unchanged for the same data. (c) Verdict sentence and class unchanged for the same data. (d) Drop-zone is a page-header button; whole-page drop still triggers import (probe). |
| U11 | Major Expenses: exceptions first (§2g) | F13 | `pages/major-expenses.html` | 2 | tests,a11y | Weak oracle, one page; filter/pin handler tests are the regression oracle (audit named a11y only — `tests` added so someone runs them). | (a) Exceptions panel renders above definitions at full width. (b) Delete control absent from collapsed rows, present inside expanded row only. (c) Page title position matches other pages. (d) majorexpenses handler tests green. (e) axe clean both themes. |
| U12 | Accounts as summary cards (§2g) | F14 F17 | `pages/accounts.html`, `components/accounts_card.html` | 2 | tests,a11y | Weak oracle, one page; account edits stay on existing handlers (audit named a11y only — `tests` added for the accounts suite). | (a) Exactly one `<h1>`. (b) Add-form hidden until requested (disclosure button, `aria-expanded`). (c) Each card shows name, kind, matched-file count, latest anchor. (d) Edit toggle reveals the existing form; accounts handler tests green. (e) axe clean both themes. |
| U13 | What-If settings rail (§2g) | F15 | `components/whatif/income-sources-list.html`, `healthcare-person.html`, `quick-adjust.html`, `static/js/whatif-*.js` | 2 | tests,a11y | Replacing "Recently Removed" changes the restore path, which is plan data. | (a) Undo restores a removed source with identical fields (handler test: remove → restore → deep-equal). (b) Toast has a focusable Undo button, `role="status"`, dismissible by keyboard. (c) Person card shows the name once; `Source:` absent when manual. (d) Slider value adjacent to label. (e) Last card not covered by FAB at 1440 and 390. (f) axe clean both themes. |
| U14 | File Manager: Clear All button + count-confirm + encryption fold (§2h) | F16 | `pages/filemanager.html`, `internal/handlers/backup/handlers.go` | 3 | tests,second | Irreversible: the control deletes data files. Oracle first (`accept.sh`), full dual lane regardless of column. | (a) `accept.sh` written and validated at both ends BEFORE dispatch. (b) Clear All is a `<button>`, separated from Load Test Data, confirm names N. (c) DELETE without `expected_count` or with a stale count → 409, zero files removed (curl oracle). (d) Matching count deletes exactly the CSVs and backup dir survives. (e) Encryption card collapsed by default. (f) Storage suite green at uid 0 and non-root; full `go test ./...` green. (g) axe clean both themes. |
| U15 | Accessibility cluster (§2i) | F17 | `components/kpi-detail.html`, `kpi-month-detail.html`, `major-expense-drilldown.html`, `pages/filemanager.html`, `pages/transfers.html`, remaining onclick sites | 2 | a11y | Strong oracle but shared blast radius (every page). | (a) `grep -rE '<(div|span|tr)[^>]*onclick' web/templates` = 0 and no `onclick=` anywhere in templates. (b) Every modal: `role="dialog"`, `aria-modal`, accessible name, focus in/out, Tab trap, Esc (probe each). (c) One `<h1>` per page (all 9). (d) axe zero violations sitewide, both themes. (e) Every data table has `<th scope="col">` headers (ruling e). |

Tier rationale summary: U1/U9 are single-file, strong-oracle, reversible →
Tier 1. Everything else is reversible pre-merge but either weak-oracle
(visual) or shared-blast-radius → Tier 2; `second` on the five tasks where
a figure or its label is re-rendered (U2, U5, U8, U10) or the action is
destructive (U14). U14 is not reversible → Tier 3.

Deviations from the audit's table (for sign-off): U11 and U12 gain `tests`
(their acceptance lines already required handler suites green; a named
checker must run them). U5 lists two template render sites, not one. U14
adds server-side count confirmation so the "cancelled confirm deletes
nothing" claim has a curl oracle. U13 lists the JS files.

## 6. Run order and dependencies

Four waves; tasks within a wave run as parallel workers.

- **Wave A — broken layouts:** U1, U2, U3, U4, U5 (independent).
- **Wave B — foundation:** U6, then U7 (partials are written in the token
  vocabulary; U7 also absorbs U2's KPI tile into the partial).
- **Wave C — shell:** U8 (needs U7's range partial), U9, U10. U9 owns
  `base.html`'s nav; U8 touches only the nav-link `href` func in it —
  dispatch U9 first, then U8 and U10 in parallel.
- **Wave D — pages:** U11, U12, U13, U14 (Tier 3, oracle authored during
  wave C), U15 last (it sweeps whatever onclick sites survive A–D).

Worker choice: U6's bulk class replacement is `worker-local` territory
(mechanical, ~600 sites) after `worker-coder` defines the tokens; the two
halves are one task, one manifest. U14 is always `worker-coder`.

## 7. Decisions — all defaults approved by the user 2026-09-03

| # | Question | Default |
|---|----------|---------|
| D1 | U10: keep Net Savings in the verdict band with a definition, or move it to the KPI row? | Move it. |
| D2 | U11: one column exceptions-first, or two columns with exceptions left? | One column, exceptions first. |
| D3 | U13: drop "Recently Removed" for an Undo toast, or keep it collapsed? Trade-off: toast-only means no restore UI after a page load (the data stays in the plan file; restore remains possible via the handler). | Toast only. |
| D4 | U5: floor below which a percent becomes a dollar delta. | previous < $100. |
| D5 | U9: the one product name. | simpleBudget. |
| D6 | U14: add server-side `expected_count` confirmation (handler change) or template-only? | Add it. |
| D7 | ACCESSIBILITY.md point 17 (modal dialogs) — adopt into budget2's standard? | Adopt before U15 dispatch. |
| D8 | Branch base: budget2 PR #89 (tax-optimizer run feedback) is OPEN and touches `layouts/base.html` and `tailwind.css`, both in U6/U7/U9's territory. Merge #89 first and branch `feat/ui-audit` from the result, or branch from master b390472 now and rebase later? | Merge #89 first. |

## 8. Tier-3 oracle plan (U14)

`.swarm/tier3/U14/accept.sh` (chmod +x), written before dispatch, run
against the verify server on a throwaway data copy:

1. Seed N CSVs; GET `/filemanager` → asserts a `<button>` whose confirm text
   contains "N" and no `<a>`/text-link Clear All.
2. `curl -X DELETE /data/all` (no field) → 409; file count still N.
3. `curl -X DELETE -d expected_count=$((N-1))` → 409; count still N.
4. `curl -X DELETE -d expected_count=N` → 200 "Deleted N files"; zero CSVs;
   backup dir intact.
5. Encryption card: disclosure button present with `aria-expanded="false"`,
   panel hidden.
6. `go test ./internal/handlers/backup/... ./internal/services/storage/...`
   green; emit `ORACLE PASS` only on the all-pass path.

Both-ends validation: a featureless tree must fail 1–3 and 5; a throwaway
prototype must pass all six, then be discarded.

## 9. Lean-experiment bookkeeping

Record every catch in §10 with the mechanism (oracle / primary checker /
second / judge / gate / worker). At run end: `swarm/gate.sh stats` and
report the first-attempt clean rate verbatim.

- **[U12.2 obs, backlog]** the anchor-form field's `aria-invalid`/`data-focus-target`
  gating still keys on `$root.ErrorField` alone (not per-account), so an
  add-anchor error flags every account's anchor field and focus lands on the
  alphabetically-first — a real WCAG 3.3.1 mis-focus, but pre-existing
  (byte-identical to master) and NOT stranding (anchor forms are outside the
  Edit toggle, always visible). `ErrorAccountID` is now populated on the anchor
  error paths, so the fix is a one-line template gate — fold into U15 or a
  follow-up.

- **[U13.2 obs, backlog]** the toast Undo button lacks `hx-sync`/`hx-disabled-elt`, so hammering many rapid CRUD cycles can double-submit or drop an Undo (both lanes: a client-timing artifact, no server race — server serializes on a private settings copy; also surfaced a pre-existing full-analysis perf pile-up under load). Add hx-sync to the Undo button in a follow-up.

## 10. Rulings

- **U-2026-09-03a** (scope ruling, lead, before any checker ran): "axe clean
  both themes" on a per-page task (U1–U4, U9–U13) means (1) zero violations
  within the elements the task touched, AND (2) zero NEW violations page-wide
  relative to the master baseline rendered from the same data. Pre-existing
  violations on untouched elements are reported as observations for U15,
  which owns the sitewide sweep — not FAILs (precedent: CC-2026-08-31c,
  ruling 2026-08-29c/d). U6, U7, U15 keep the sitewide-zero criterion as
  written. Trigger: U4's worker found pre-existing `label`, `select-name`,
  `color-contrast`, `target-size` violations in the What-If settings rail.
- **U-2026-09-03b** (catch — mechanism: WORKER report, U1 attempt 1; a
  brief-level scoping error): the audit/spec scoped U1 to the transaction
  table, but criterion (a) is page-level (no horizontal scroll at 390 px) and
  the explorer's Date Range filter block (explorer.html ~69-100: From/To
  inputs, step buttons, 1M–12M quick buttons in one non-wrapping flex row,
  652 px wide) overflows on its own. Also, hiding only Source + Major Expense
  left the table at 684 px; the worker additionally collapsed Category and
  Type below `md`, keeping Date/Description/Amount — consistent with the
  spec's intent and accepted. Ruling: U1's scope EXPANDS to the filter block
  on the same page with the minimal fix (wrap the row; quick buttons wrap
  onto their own line; every control keeps its label and ≥ 24×24 target),
  because U7/U8 replace this block with the shared range partial later.
  Tier stays 1 (one file, strong oracle). The worker stopped instead of
  guessing — the behaviour the constitution asks for.
- **U-2026-09-03e** (catch — mechanism: PRIMARY CHECKER checker-a11y, U1
  attempt 1, observation): U1's criterion (c) assumed the explorer headers
  carry `<th scope="col">`; neither master nor the branch has `scope` on ANY
  `<th>` (ACCESSIBILITY.md point 2). A pre-existing gap the audit did not
  count, so not a U1 FAIL. Added to U15's scope: every data table sitewide
  gets `<th scope="col">` (and `scope="row"` where a row header exists);
  U7's data-table partial emits it by construction.
- **U-2026-09-03f** (catch — mechanism: SECOND CHECKER, U5 attempt 2, FAIL
  CONCEDED; same class as ruling d): `MajorExpenseTrends` sums signed
  amounts with plain float addition, so 0.10+0.20−0.30 ≈ 5e-17 with no
  prior activity rendered Previous $0.00, Current $0.00, Change "+100.0%",
  arrow up — reproduced through the real producer path. Attempt 2 rounded
  inside ChangeDisplay but the producer's raw `change > 0` classification
  and raw `changePercent` pass-through survived (split classification).
  Two failures to one class ⇒ lead/spec defect (T18 precedent): §2d
  rewritten as contract v3 — round at the source, one classifier that also
  owns Direction, property test on rendered self-consistency. Attempt 3 is
  the last before the hard stop. The 200k-sample brute-force probe is
  promoted into the property test.
- **U-2026-09-03h** (escalation — mechanism: PRIMARY CHECKER checker-a11y
  twice + GATE escalate-scan; U6 → Tier 3): attempt 2 failed (e) again
  (monte-carlo rationale divs, rate-assumptions:473 wrapper — siblings of
  sites the lead had just converted) and (g) — the 12→14 px bump pushed the
  major-expenses "anomalous" table into page-level horizontal scroll.
  Two same-class failures ⇒ contract defect: "a random sample of 20 finds
  no sentence-class site" was a lottery over ~300 survivors with no
  mechanical definition. §2a rewritten (contract v2): the type floor is
  an executable allow-list (R1–R4) and the rendered bar is measured with
  every `<details>` opened. The lead's oracle (`.swarm/tier3/U6/accept.sh`,
  fail-end log `failend-validation.attempt2-tree.log`) flags 23 denied
  elements (the checker's three among them) AND, by opening the panels,
  33 light-mode + 1 dark-mode contrast failures on /major-expenses that
  neither the worker's nor the checker's axe runs had seen — hidden content
  was the gap in both. Attempt 3 is the last before a hard stop and goes to
  a worker (Tier 3 is never lead-direct).
  Addendum (PRIMARY CHECKER checker-a11y, U6 attempt 3, PASS with a
  methodology catch): /whatif keeps 4 of 5 tabs in `hidden` JS panels
  (whatif.html:195-221, whatif-tabs.js), so no oracle or checker run before
  this one had ever scanned most of U6's what-if content; the checker
  clicked every tab and found zero contrast violations on the branch (the
  baseline has 13–60 per tab). Attempt-4 oracle must activate every tab
  and open every modal before measuring, in addition to opening details.
  Addendum (PRIMARY CHECKER checker-tests, U5 attempt 2, FAIL on (d)): the
  MCP description added at attempt 1 made three claims the code
  contradicted — "new" for any zero previous (a negative current renders
  −100.0%), "under $100" without the absolute value (−$628 previous rendered
  a percent), and "change_amount via the same money formatting" (math.Round
  vs fmt rounding diverged on 5 of 2e6 samples, e.g. 4.246/686.823 →
  UI $682.57 vs 682.58). d-1 traces to §2d's own attempt-1 paraphrase — a
  second spec-level catch on this task. Folded into contract v3's MCP
  paragraph; the 4.246/686.823 pair is a named oracle fixture.
  Outcome: U5 ACCEPTED at Tier 3 attempt 3 (oracle 11/11, both lanes PASS;
  checker-second fuzzed ~200k pairs on fresh seeds, checker-tests killed all
  three mutants with the worker's own suite). Backlog from the verdicts:
  (1) `data-change` sort key is the raw percent; (2) MCP `change_percent`
  is round2 of the pct while the UI prints %.1f of it — one tenth apart on
  4 of 230 live rows, pre-existing on master; (3) `ChangeCell.Text` is
  write-only dead code (a second home for the cell text) — remove; (4) a
  "+0.0%" is constructible but not live. Candidates for a follow-up task
  after wave D; none block the row.
- **RECONCILE-2026-09-05** (merge master→feat/ui-audit; checker-second FAIL
  CONCEDED then fixed): the branch forked at PR #89, before CB7/CB8/CB9/#90/
  #91/#92 shipped. Merge kept master's shipped semantics AND the U-run UI
  (details in the merge commit body). Production code verified correct at
  render time by checker-second (signed money + tokens, #90 one-month
  stepping live on explorer+major-expenses, #91/#92 whatif, CB8 velocity,
  U-run nav/range/net-savings/keyboard). The lead's re-pin of
  TestKPIsTotalExpensesTile_...SignedNegative was WEAK — a bare
  Contains("text-positive") is always true from the Income tile; fixed to a
  fused `text-positive">-$1,234.56` assertion (color tied to the figure,
  mutation-proven: a color revert now fails), matching the explorer sibling
  pattern. Backlog (PRE-EXISTING in a61dc08, NOT merge defects, for wave D/
  U15): range-picker preset buttons dead via Go html/template `ZgotmplZ`
  attribute-name escaping (sitewide); explorer sort headers not
  keyboard-operable (U15's div/th-onclick sweep); master's new #91/#92
  whatif markup not yet U6-tokenized (hue literals in whatif-expense-rows /
  healthcare bars).
- **U-2026-09-05o** (catch — checker-a11y, U13 attempt 1, FAIL CONCEDED):
  the Undo toast (`whatif-removed-income-sources`, an hx-swap-oob div emitted
  by the SHARED `renderWhatIfResults`/`renderResultsTemplate` that EVERY
  mutation calls) keys on `.Settings.RemovedIncomeSources` (the persistent
  removed-sources store, last entry). So any action — even an unrelated
  healthcare PUT — re-surfaces a stale "Removed Rebuild Payroll" toast for a
  past-session removal, violating the task's "a past-session removal must not
  resurface" bar and ACCESSIBILITY.md #14. Fix (display-only signal, the U12
  ErrorAccountID precedent — do NOT change plan-calc/remove logic): the toast
  renders visible content ONLY when an income source was removed in the
  CURRENT request. `handleWhatIfDeleteIncome` sets a display-only
  JustRemovedIncome {ID,Name} threaded into the results-partial data; the
  toast keys on `{{with .JustRemovedIncome}}` and renders hidden/empty
  otherwise. Every other mutation (add/update/healthcare/sweep/restore) and
  initial page load render the toast hidden. Restore (Undo) does NOT
  re-show the toast. Add a render/handler test: a delete sets the signal and
  shows the toast for the just-removed source; a non-remove mutation shows no
  toast. The remove→restore lossless test and the toast a11y attributes (role
  status, focusable Undo/dismiss, Esc, focus restore) already PASS — keep
  them.
- **U-2026-09-05n** (catch — checker-a11y, U12 attempt 1, FAIL CONCEDED):
  U12 hid the per-account edit form and the add form behind disclosures. On
  an edit-form validation error the errored account's panel stayed COLLAPSED
  and focus landed on the unrelated Add form, stranding the error
  (contradicting the task's own ask #3). Root cause (pre-existing, exposed by
  the hiding): the accounts error model has NO account scoping —
  handleUpdate's error path sets neither ErrorField nor an account id, and the
  Add form's `data-focus-target` is UNCONDITIONAL, so it is always the first
  (and only) match. A correct focus/reveal is impossible without scoping.
  Ruling: "handlers untouched" bends to a DISPLAY-ONLY error-scope addition
  (not a CRUD change). Attempt-2 contract:
  1. Add `ErrorAccountID string` to the accounts page-data struct.
  2. handleUpdate error path: capture formData and set BOTH
     `data.ErrorField = formData.errorField` and `data.ErrorAccountID = id`;
     the add-anchor error paths set `ErrorAccountID` to that account too;
     handleCreate leaves it "" (the add form).
  3. Add-form fields: gate `data-focus-target` on
     `{{if and (eq .ErrorField "<f>") (eq .ErrorAccountID "")}}` — only when
     the ADD form errored.
  4. Edit-form field: gate on
     `{{if and (eq $root.ErrorField "name") (eq $root.ErrorAccountID $acct.Account.ID)}}`
     — only the erroring account.
  5. Reveal the erroring form's panel server-side: render `#acct-edit-panel-{id}`
     WITHOUT `hidden` when `eq $root.ErrorAccountID $acct.Account.ID` (and the
     add panel open when ErrorField set && ErrorAccountID==""), so exactly one
     field carries data-focus-target and the JS reveal+focus lands on it.
  Re-verify: submit an edit error → THAT account's panel opens and its field
  is focused; submit an add error → add panel opens, add field focused; other
  panels stay collapsed. Handler CRUD tests stay green + a new handler test
  for ErrorAccountID scoping.
- **U-2026-09-04m** (catch — checker-a11y, U7 attempt 2, FAIL CONCEDED;
  CONTRACT REWRITE per the two-same-class rule): attempt 2 fixed keyboard
  operability but INTRODUCED a serious nested-interactive violation (axe
  WCAG 4.1.2, absent on master) — the shared kpi-tile now emits
  `role="button"` on the outer div, and the Budget tile
  (`kpis.html:108`) passes `Clickable: true` UNCONDITIONALLY, so in its
  no-target state the role="button" container wraps a real
  `<a href="/whatif">Set a budget</a>`. Two attempts have now failed on the
  same root: the "whole tile is one button" model cannot host the Budget
  tile's dual nature (modal trigger AND, when no target is set, a link).
  Per CLAUDE.md ("two attempts to the SAME defect class ⇒ rewrite the
  contract, T18 precedent"), the fix is a contract change, not a third
  variant of the same design:
  **The Budget tile is a modal trigger ONLY when a budget target is set.**
  At `kpis.html:108` set `Clickable` to `.Metrics.HasCombinedTarget`
  (not the literal `true`). When a target is set the tile has no nested
  link and stays a `role="button"` modal trigger (its DetailKey "expenses");
  when no target is set it is a plain container whose SOLE interactive
  element is the existing `<a href="/whatif">` — no role=button ancestor,
  so no nested-interactive. The other four tiles are unchanged (pure modal
  triggers, no nested link). Verify: axe nested-interactive = 0 sitewide
  both themes; all previously-fixed controls still keyboard-operable; the
  Budget tile opens the expenses modal by keyboard WHEN a target is set and
  offers the keyboard-focusable /whatif link WHEN not. This is U7's last
  attempt before a Tier-2 hard stop.

- **U-2026-09-04l** (catch — mechanism: PRIMARY CHECKER checker-a11y, U7
  attempt 1, FAIL CONCEDED): axe passed 18/18 (9 pages × 2 themes) but a
  manual Tab-walk found the onclick→delegated-listener refactor left three
  families of controls keyboard-unreachable — the 5 shared KPI-tile cards
  (kpi-tile.html, from kpis.html), 4 Insights navigation targets
  (insights.html data-navigate-href), and 12 Insights sortable `<th
  data-sort-fn>` headers — all wired by a delegated click listener on a
  non-focusable element (`tabIndex -1`, no role). axe cannot see a
  click-listener-on-a-div, which is exactly why the lead's dispatch made
  keyboard operability a manual check. The gap is a div/th-onclick pattern
  (nominally U15's sweep), but U7 REWROTE the interaction layer on these
  exact sites, so making the controls it rewired operable belongs with the
  rewiring, not a later re-touch of the same JS. CONCEDE, attempt 2, scoped:
  ONLY the sites U7 touched — sortable headers become real `<button>`; the
  KPI tiles and Insights nav targets become keyboard-operable (native
  `<button>`/link where the markup allows, else `tabindex="0"` + `role` +
  an Enter/Space keydown handler in the page JS, focus-visible ring). Do
  NOT expand into the rest of U15's onclick inventory. The modal
  role=dialog/aria-modal gap stays U15 (checker agreed: observation, Esc
  still works). The worker's genuine pre-existing axe fixes (modal
  aria-labels, select names, scroll-region keyboard access) STAND.

- **U-2026-09-04k** (oracle hardening + catches, U6 attempt 4 accepted):
  both lanes PASS; the lead re-validated the oracle at both ends (renderError
  reverted ⇒ checks 9+10 FAIL; successRate darkening reverted ⇒ check 9
  FAILs deterministically). Two catches this attempt, both about oracle
  ROBUSTNESS not the fix (the fix is correct):
  (1) checker-a11y + checker-second independently found check 7's rendered
  contrast sweep only reaches the darkened successRate tiers when a
  stochastic Monte-Carlo draw lands in the 60–89% band, and never opened
  what-if's hidden tabs or the dashboard's HTMX modals — so the darkened
  tiers passed the oracle probabilistically while check 9 (emitter coverage)
  backstops them deterministically every run. render_probe.js hardened to
  activate every tab and open the HTMX modals before auditing (this ruling);
  (2) the reconstructed oracle's error_probe.js accounts case used the wrong
  form field names (date/amount vs anchor_date/anchor_amount) and hit a 200
  validation branch instead of renderError's 404 — the worker corrected it,
  both checkers verified the correction STRENGTHENED the case. Neither is a
  fix defect; U6 is accepted. Waves B–D unblocked.

- **U-2026-09-04j** (attempt-4 reopening + oracle extension, user-authorized
  2026-09-04): the U-run lead worktree was removed during a cleanup with
  SPEC.md uncommitted and `.swarm/` (gitignored on the agents2 side) on disk
  only; the constitution, ledger and U5/U6/U14 oracles were reconstructed
  from the session transcript (agents2 PR #10). U6 reopens as attempt 4 with
  the ruling-i scope PLUS two oracle checks the attempt-1–3 oracle lacked
  (they are why the renderError defect slipped a Tier-3 oracle):
  - **check 9** `emitter_coverage.py`: every Tailwind colour class emitted
    from Go or JS source (outside the template content globs) must have a
    rule in the built `tailwind.css`. Catches a purge of a runtime-assembled
    class directly, not via a rendered page.
  - **check 10** `error_probe.js`: the accounts/whatif/major-expenses POST
    and DELETE error paths must render a banner that is token-only (no hue
    literal), has a non-transparent background and a visible border, and
    passes axe color-contrast, in BOTH themes.
  Scope for the worker: (1) `renderError` in the three handlers → token
  classes (`bg-negative-soft`, `border-negative`, `text-negative`, matching
  the template error idiom `bg-negative-soft/border-negative`), and update
  the handler tests that pin the old `red-*` strings; (2) any other Go/JS
  colour-class emitter that check 9 flags; (3) `successRateTextClass`/
  `successRateBarClass` in render.go — darken the tiers that fail 4.5:1 on
  white (lime-600/yellow-600 on a light `num` tile) to a compliant shade,
  the SAME minimal-darkening fix ruling X8/2026-08-29e applied to the
  verdict map, so check 7 is clean regardless of which success-rate tier
  the data lands on (the attempt-1–3 oracle passed check 7 only because its
  synthetic data never hit the lime tier — a data-dependent pass this
  extension removes). Re-verify at Tier 3 (oracle + dual lane). This is the
  last attempt before a second hard stop.

- **U-2026-09-03i** (catch — mechanism: SECOND CHECKER, U6 attempt 3, FAIL
  CONCEDED; HARD STOP): `renderError()` in
  `internal/handlers/{accounts,whatif,majorexpenses}/handlers.go` builds an
  error banner with literal `red-*` classes via fmt.Sprintf. Those classes
  were only ever in the built CSS because ~20 templates used them; U6's
  sweep converted every template site to tokens, so Tailwind's content scan
  purged them and 36 error paths (missing id, not found, invalid form,
  failed save/delete) now render with no background, border or intended
  colour. Reproduced live via three POSTs. Attempt 1's worker had noticed
  the helper and declared it out of scope; the lead accepted that judgment
  — the error was the lead's: a token sweep's scope is every EMITTER of a
  colour class, not every template. No oracle check reached it (the hue
  grep scans templates; the render probe only GETs 9 pages). Third failed
  attempt (a11y, a11y, second) ⇒ hard stop per the constitution; the
  lead does NOT silently loop. Proposed resolution for the user: reopen U6
  as attempt 4 with the explicit scope "renderError literals → token
  classes in the three handlers (+ their tests), safelist audit of every
  Go/JS colour-class emitter, oracle extended with a POST error-path
  render check", lead-direct or worker, then re-verify at Tier 3.
  Wave B/C/D are blocked behind U6 (the partials must use the tokens).
- **U-2026-09-03c** (catch — mechanism: WORKER report, U2 attempt 1; brief
  error inherited from the audit): the sparkline draw function
  (`renderSparkline`/`initSparklines`) lives in `web/static/js/charts.js`,
  not `dashboard.js` as the audit and §5 named. Worker edited charts.js and
  left dashboard.js untouched — accepted. §5 row U2 files corrected below.
  Observation for U10/U7: with five cards in a four-column grid and Budget
  spanning two, row 2 holds Budget alone with two empty cells at `xl`+;
  U10 (Net Savings tile) and U7 (KPI tile partial) revisit the row's shape.
- **U-2026-09-03d** (catch — mechanism: SECOND CHECKER, U5 attempt 1, FAIL
  CONCEDED): the dollar delta was formatted from the raw float difference
  while Current and Previous are formatted from their own raw values, so
  with previous 99.995 and current 150.005 the row renders Current $150.00,
  Previous $100.00, Change +$50.01 — the displayed figures do not sum
  (ruling 2026-08-29b class). The worker's fractional-cent fixture happened
  not to trigger it, and the render test computed its expected string from
  the raw float independently, so the test proved nothing about the
  invariant. Fix contract for attempt 2: ONE rounding path — round previous
  and current to display precision first, derive the delta from the ROUNDED
  values, and the render test must assert rendered Change == rendered
  Current − rendered Previous over a fixture set that includes the
  checker's 99.995/150.005 case (promote the checker's probe, V3 pattern).
  Observation carried to backlog: `data-change` sorts by the raw percent, so
  "new"/dollar rows sort inconsistently with their displayed value.
- **U-2026-09-03g** (catch — mechanism: PRIMARY CHECKER checker-a11y, U6
  attempt 1, FAIL CONCEDED): a random 20-site sample of surviving `text-xs`
  found two sentence-class survivors (projection-chart helper copy;
  tax-optimizer's Age/Conversion table body) — an in-scope miss beside
  sites the worker had converted. The lead found the same class in seven
  more `<table class="text-xs">` bodies (major-expenses ×5,
  rate-assumptions, historical-backtest) and converted all nine plus the
  `<details>` wrapper to `text-body-sm` lead-direct under the lean
  exception (attempt 2, worker=lead; class strings only, CSS rebuilt,
  affected handler/template suites green). Seven of eight criteria had
  passed with evidence at attempt 1; the checker re-verifies at attempt 2.

