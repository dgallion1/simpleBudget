# T2 — File Manager Accessibility Audit (recount)

**Date:** 2026-08-25
**Auditor:** checker-a11y (fresh audit, replaces the two prior unreconciled counts of "20" and "6")
**Page under test:** `/filemanager` (also served at `/explorer/files`; route registered
`cmd/server/main.go:267` → `explorer.HandleFileManagerPage`; routes registered in
`internal/handlers/explorer/handlers.go:100-109`)
**Template:** `web/templates/pages/filemanager.html` (1470 lines), layout
`web/templates/layouts/base.html`

## Method

- Built `budget2` from source (`go build ./cmd/server`), ran in isolation with
  `BUDGET_DATA_DIR`, `BUDGET2_BACKUP_DIR`, `BUDGET_LISTEN_ADDR=127.0.0.1:8091`
  pointed at a scratch copy of `testdata/transactions.csv` +
  `testdata/transactions_edge.csv`. Confirmed 200 OK on `GET /filemanager`.
- Automated scan: Playwright 1.56.1 (Chromium `/opt/pw-browsers/chromium-1194`,
  headless) + `@axe-core/playwright` 4.13 (`axe-core` 4.13). Ran once against
  the default (light) theme, and again after clicking the page's own
  `#theme-toggle` button (confirmed `document.documentElement.classList` gains
  `dark`, matching the app's real light/dark switch mechanism at
  `web/templates/layouts/base.html:235-251`).
- **Method caveat (material):** this sandbox's egress policy blocks
  `cdn.tailwindcss.com` (confirmed via `curl` → 403, and via the proxy status
  endpoint's `recentRelayFailures`). The app has **no local/vendored Tailwind
  build** (`web/static/vendor/` contains only `htmx.min.js`;
  `web/static/css/styles.css` is 148 lines of hand-written overrides only,
  not a compiled Tailwind stylesheet) — all utility-class styling
  (colors, spacing, `dark:` variants, focus rings) is generated **at runtime**
  by the CDN script tag at `base.html:18`. With the CDN unreachable, the
  automated scan ran against an **unstyled** page (default browser rendering:
  black text on white, giant unstyled inline SVGs), which makes axe's
  automated `color-contrast` pass/fail (102 passes, 0 violations, both
  "themes") **not valid evidence of real rendered contrast** — trivial
  black-on-white text passes automated contrast checks by default regardless
  of what color classes are actually applied. Per instructions I did not
  retry or route around the policy denial. To still deliver a genuine
  contrast/dark-mode verdict, points 7/12 below were checked **manually**:
  Tailwind v3 default hex values for every `text-*`/`dark:text-*` class
  pair actually present in the template were extracted and run through a
  WCAG relative-luminance contrast calculation. All ratios below are exact,
  reproducible arithmetic, not estimates.
- All non-color findings (DOM structure, ARIA, labels, focus-management code
  paths, table semantics) are unaffected by the CDN issue — axe reads the DOM/
  ARIA tree directly, and I independently confirmed each by reading
  `web/templates/pages/filemanager.html` at the cited line numbers.
- Manual walk of ACCESSIBILITY.md points 2, 5, 6, 9, 10, 13, 14, 15 performed
  directly against template markup, JS behavior (`hx-target`, `htmx:afterSwap`
  listeners), and a real keyboard Tab-order trace (25 stops, light theme,
  recording `outlineStyle`/`outlineWidth`/`boxShadow` at each stop).
- Server killed and all scratch files under `/tmp` removed at the end. The
  only write to the repo is this file.

## Findings

### F1 — Icon-only delete button has no accessible name
- **Point:** WCAG 2.2 AA baseline, SC 4.1.2 (Name, Role, Value) — not covered
  by a specific ACCESSIBILITY.md number; closest in spirit to point 2/3
  (interactive controls must be operable and identifiable).
- **Severity:** critical (axe `button-name`)
- **Location:** `web/templates/pages/filemanager.html:1407-1412` — `<button
  hx-delete="/explorer/files/{{urlEncode .Name}}" ...>` wraps only an `<svg>`,
  no text, `aria-label`, or `title`.
- **Raw axe node count:** 2 (one per seeded CSV row: `transactions.csv`,
  `transactions_edge.csv`; scales with row count in real data).
- **Fix direction:** add `aria-label="Delete {{.Name}}"` (or visually-hidden
  text) to the button.

### F2 — Six form controls have no programmatic label
- **Point:** 4 (every input has an associated `<label>` or `aria-label`)
- **Severity:** critical (axe `label`)
- **Locations:**
  - `filemanager.html:15-16` — CSV multi-upload `<input type="file">`
  - `filemanager.html:42` — `#restore-file-input`
  - `filemanager.html:1401-1404` — per-row "enabled" checkbox
  - `filemanager.html:343` — `#enc-password`
  - `filemanager.html:348` — `#enc-confirm`
  - `filemanager.html:440` — `#yubikey-select`
  - (also `#ssh-key-select` is a `select-name` node, see F3)
- **Raw axe node count:** 6 (live scan, unencrypted test-data state).
- **Manual addition:** a 7th instance, `#unlock-password`
  (`filemanager.html:262-264`, `placeholder="Password"` only, no `<label>`),
  only renders when `.IsLocked && .AuthMethod == "password"`; the seeded test
  data is unencrypted so this branch never executed during the live scan.
  Flagging it here so the worker brief covers all branches, not just the one
  axe happened to see.
- **Fix direction:** add `aria-label` or wrap in `<label>` for each.

### F3 — Two `<select>` elements have no accessible name
- **Point:** 4
- **Severity:** critical (axe `select-name`)
- **Locations:** `filemanager.html:440` `#ssh-key-select`,
  `filemanager.html` (~line for) `#yubikey-select`
- **Raw axe node count:** 2
- **Fix direction:** add `aria-label` (e.g. "SSH key", "YubiKey identity").

### F4 — Action-column table header has no discernible text
- **Point:** 2 (data tables use `<th>` with `scope`) — the `scope` is present
  and correct; this is the "must also carry a name" half of the same
  requirement, flagged by axe's best-practice `empty-table-header` rule.
- **Severity:** minor
- **Location:** `filemanager.html:1380` — `<th scope="col" ...></th>` (the
  delete-button column)
- **Raw axe node count:** 1
- **Fix direction:** add visually-hidden text (e.g. `<span
  class="sr-only">Actions</span>`) inside the `<th>`.

### F5 — File-list HTMX swap has no `aria-live` region and no focus restoration
- **Point:** 10 (focus restored after HTMX swap; destructive/state-changing
  actions announce result via `aria-live="polite"`)
- **Severity:** serious (manual — axe does not evaluate this)
- **Location:** `filemanager.html:221` — `<div id="file-list">` is the
  `hx-target` for both the delete button (`:1407`) and the enable/disable
  checkbox (`:1401-1404`); it carries no `aria-live` attribute. The global
  `htmx:afterSwap` handler in `web/templates/layouts/base.html:221-226` only
  restores scroll position for `#whatif-results` — there is no equivalent
  focus-restoration logic for `#file-list` anywhere in the codebase
  (confirmed: no other `htmx:afterSwap`/`afterSettle` listener touches this
  target). Concretely: click the delete button on a row → the whole table
  body is replaced → the element that had focus is removed from the DOM →
  focus silently reverts to `<body>` → a keyboard/screen-reader user loses
  their place and gets no announcement that the file was deleted or that a
  toggle changed state.
- **Raw node count:** 1 (the `#file-list` container; the same missing
  live-region/focus-restore gap applies to both actions that target it —
  delete and enable/disable toggle).
- **Fix direction:** add `aria-live="polite"` to `#file-list` (or a sibling
  status node updated by the same swap) and add an `htmx:afterSwap` handler
  that moves focus to a sensible element in the refreshed table (e.g. the
  next row's checkbox, or a "N files" summary node) when the target is
  `#file-list`.

### F6 — Five encryption-form error containers are inert to assistive tech
- **Point:** 5 (validation errors linked via `aria-describedby`; focus moves
  to, or `aria-live` announces, the first error on failed submit)
- **Severity:** serious (manual)
- **Locations:** `filemanager.html:266` `#unlock-error`, `:302`
  `#disable-encryption-error`, `:359` `#enable-encryption-error`, `:421`
  `#ssh-encryption-error`, `:469` `#yubikey-encryption-error`. All five are
  plain `<div class="hidden text-sm text-red-600 dark:text-red-400">`,
  toggled by JS (`errorDiv.classList.remove('hidden')`,
  `errorDiv.textContent = ...`, e.g. `:1253-1260`, `:1231-1232`,
  `:1335-1336`) with **no** `role="alert"`/`aria-live`, and **no**
  `aria-describedby` linking them to `#enc-password`, `#enc-confirm`,
  `#unlock-password`, etc. No JS in the file moves focus to the error or the
  offending field on failure.
- **Raw node count:** 5 (one pattern, five instances — none of this is
  axe-detectable since axe only scans the DOM state at load, before any
  validation failure fires).
- **Fix direction:** add `role="alert"` (or `aria-live="polite"`) to each
  error `<div>`, `aria-describedby` from the related input(s) to the error
  id, and `.focus()` the first invalid field on submit failure.

### F7 — Encryption password minimum length is not stated in visible text
- **Point:** 6 (required fields/input formats stated in text, not implied by
  placeholder/attribute alone)
- **Severity:** moderate (manual)
- **Location:** `filemanager.html:342-343` label "Password" / `<input
  id="enc-password" required minlength="8">`; `:347-348` label "Confirm
  Password" / `<input id="enc-confirm" required minlength="8">`. The "at
  least 8 characters" constraint exists only as the `minlength` HTML
  attribute (enforced silently by the browser) and in a JS string shown only
  **after** a failed submit (`:1253`, itself unannounced per F6) — there is
  no static visible/programmatic text stating the requirement up front.
- **Raw node count:** 2 (one pattern).
- **Fix direction:** add visible helper text under each label, e.g. "Minimum
  8 characters."

### F8 — Encryption status badge fails text contrast in light theme
- **Point:** 7 (text contrast ≥ 4.5:1) and 12 (dark-mode parity — this one
  is the mirror image: **light** theme is the one that fails)
- **Severity:** serious (manual — computed, not axe-detectable per the CDN
  caveat above)
- **Location:** `filemanager.html:247` — `<span class="text-xs {{if
  .IsEncrypted}}{{if .IsLocked}}text-amber-600 dark:text-amber-400{{else}}
  text-green-600 dark:text-green-400{{end}}{{else}}text-gray-500
  dark:text-gray-400{{end}}">` renders "Locked"/"Unlocked"/"Not encrypted".
  Computed against the card's `bg-white dark:bg-gray-800` background:
  - `text-amber-600` (#D97706) on white = **3.19:1** — fails 4.5:1 ("Locked")
  - `text-green-600` (#16A34A) on white = **3.30:1** — fails 4.5:1 ("Unlocked")
  - `dark:text-amber-400`/`dark:text-green-400` on `bg-gray-800` = 8.79:1 /
    8.42:1 — both pass comfortably in dark mode.
  - This is exactly the "visible in one theme, failing in the other" pattern
    the checker brief calls out by name.
- **Raw node count:** 2 (amber and green variants of the same span, mutually
  exclusive at render time — count as one pattern, two color instances).
- **Fix direction:** darken the light-mode status colors (e.g.
  `text-amber-700`/`text-green-700`) to clear 4.5:1 on white.

### F9 — File-size annotation fails contrast in BOTH themes
- **Point:** 7 and 12
- **Severity:** serious (manual)
- **Location:** `filemanager.html:1394` and `:1439` — `<span
  class="text-gray-400 dark:text-gray-500 ml-1">({{size}}K)</span>`. Every
  other secondary-text pair on this page uses the convention
  "darker-in-light / lighter-in-dark" (`text-gray-500 dark:text-gray-400`,
  passes both themes — see clean list). This one element has the pairing
  **backwards**.
  - `text-gray-400` (#9CA3AF) on white = **2.54:1** — fails 4.5:1 (light)
  - `dark:text-gray-500` (#6B7280) on `bg-gray-800` = **3.04:1** — fails
    4.5:1 (dark)
  - Both themes fail; this single reversed class pairing is worse than any
    other contrast issue found on the page.
- **Raw node count:** 2 (the "Data Files" list annotation and the identical
  one in the "Import from Folder" scan-results list, `:1439`).
- **Fix direction:** swap to `text-gray-500 dark:text-gray-400` to match the
  rest of the page's convention.

### F10 — Six checkboxes/radios have a focus color but no focus ring width, plus one input with no focus styling at all — focus indicator is invisible
- **Point:** 9 (focus indicator is visible; never `outline: none` without a
  working replacement)
- **Severity:** serious (manual — axe does not check focus-indicator
  visibility)
- **Root cause:** `web/static/css/styles.css:44-48`:
  ```css
  input:focus, select:focus {
      outline: none;
      ring: 2px;
      ring-color: #6366f1;
  }
  ```
  `ring` and `ring-color` are **not real CSS properties** — they are
  Tailwind-only utility-class tokens, meaningless when hand-written as plain
  CSS declarations. This rule removes the native focus outline from every
  `<input>`/`<select>` sitewide and replaces it with nothing. This is true
  regardless of whether the Tailwind CDN loads (confirmed live: the CSV
  upload file input at `filemanager.html:15-16`, which carries no
  `focus:ring-*` utility class at all, measured `outlineStyle: "none",
  outlineWidth: "0px", boxShadow: "none"` when focused via a real keyboard
  Tab in the live browser trace).
  Six more elements carry a Tailwind `focus:ring-{color}` utility class
  **without any ring-width utility** (`ring-2`, `ring`, etc.) — in Tailwind,
  the color utility only sets the `--tw-ring-color` CSS variable; the
  box-shadow that actually renders the ring comes from the width utility,
  which is absent here. So even when the CDN is reachable, these get no
  visible replacement for the removed native outline:
  - `filemanager.html:88-89` — "Delete source files after import" checkbox
  - `filemanager.html:170-171` — auto-backup enable/disable checkbox
  - `filemanager.html:374-375`, `:379-380` — "Generate new identity" /
    "Use existing identity" radios
  - `filemanager.html:1401-1404` — per-row file "enabled" checkbox
  - `filemanager.html:1434-1436` — import-scan per-file checkbox
- **Raw node count:** 7 elements (1 file input + 6 checkboxes/radios), one
  root-caused pattern.
- **Fix direction:** delete the invalid `ring`/`ring-color` declarations from
  `styles.css:44-48` (or replace with a real `box-shadow`), and add
  `focus:ring-2` (or equivalent width utility) to the six listed
  checkbox/radio elements; add a `focus:ring-2 focus:ring-indigo-500` (or
  browser-default-preserving) treatment to the file input.

### F11 — No `<header>` landmark
- **Point:** 1 (landmark regions `<main>`, `<nav>`, `<header>`)
- **Severity:** moderate (manual)
- **Location:** `web/templates/layouts/base.html:67` — the top bar is a bare
  `<nav>` with no wrapping (or sibling) `<header>` element anywhere in the
  layout. `<main>` (`:155`) and `<footer>` (`:184`) are present; `<header>`
  is absent site-wide. This is the shared layout that wraps every page
  including File Manager, so it's in-scope here; the same gap applies
  everywhere else on the site (noted so the worker brief can fix it once,
  in the layout, rather than per-page).
- **Raw node count:** 1 (the layout template; renders on every page).
- **Fix direction:** wrap the nav bar (or nav + branding) in a `<header>`
  element.

## Clean (checked, PASS)

- **Point 1** (except the `<header>` gap in F11): exactly one `<h1>`
  ("File Manager", `filemanager.html:7`); heading order h1→h2→h3 with no
  skipped levels; `<main>`/`<nav>`/`<footer>` present; `<html lang="en">` set.
- **Point 2:** all `<th>` elements carry `scope="col"`; sortable headers
  (`filemanager.html:1350,1358,1366,1374`) are real `<button>` elements
  inside the `<th>`, not bare clickable text. (Empty accessible name on one
  action-column `<th>` is F4, minor.)
- **Point 3:** no `<div onclick>`/`<span onclick>` anywhere on the page; all
  click handlers are on `<button>`/`<a>`.
- **Point 4** (partial): most inputs (e.g. `#import-delete-source`,
  `#auto-backup-toggle`, "Current Password" `:289-291`, SSH passphrase
  `:295-297`, import-scan checkboxes `:1434-1437`) have correctly associated
  `<label>` elements. (Exceptions are F2/F3.)
- **Point 7 / 12** for the majority of body text: the standard
  `text-gray-600 dark:text-gray-400` pairing (table headers, cell text) =
  7.56:1 light / 6.99:1 dark; `text-gray-500 dark:text-gray-400` (secondary
  text, most of the page) = 4.83:1 light / 6.99:1 dark. Both pass 4.5:1 in
  both themes. Delete-icon color `text-red-500 dark:text-red-400` against
  its background clears the 3:1 non-text/icon threshold in both themes
  (3.76:1 light, 6.41:1 dark). (Exceptions are F8/F9.)
- **Point 8:** encryption status is conveyed by color AND text ("Locked" /
  "Unlocked" / "Not encrypted"), not color alone.
- **Point 9** (focus order): a live 25-stop keyboard Tab trace found no
  positive `tabindex`, and DOM order matches visual order throughout the
  header, nav, and File Manager form controls. (Focus *visibility* is F10.)
- **Point 10** (partial): `#import-scan-list` (`:82`) and `#import-result`
  (`:102`, `role="status" aria-live="polite"`) are correctly wired for the
  import-scan flow. (The delete/toggle swap on `#file-list` is F5.)
- **Point 11:** not applicable — no Plotly charts on this page.
- **Point 13:** not applicable — no currency amounts appear on this page.
- **Point 14:** not applicable — no dismissible banners (unassigned-files /
  staleness) render on this page.
- **Point 15:** no motion-based-only feedback found (no spinners, no
  `animate-*` classes); only `transition-colors`/`transition-all` on
  hover/state changes, none of which is the sole carrier of any information.
  (Observation: no `prefers-reduced-motion` media query exists anywhere in
  the codebase — not a fail today since nothing currently violates it, but
  worth having before any future animation is added.)
- **Point 16:** no client-side-only suppression pattern found on this page
  (no dismiss-without-round-trip controls exist here to evaluate).

## Reconciliation: "20" vs "6" vs this recount

This recount finds **11 distinct patterns** comprising **31 raw
node/element instances** — summing each finding's stated raw count:
F1=2, F2=6, F3=2, F4=1, F5=1, F6=5, F7=2, F8=2, F9=2, F10=7, F11=1
→ **31 instances across 11 patterns**. (F2's manual 7th `#unlock-password`
instance is called out separately in F2 as a branch axe couldn't reach live,
and is not double-counted in the 31.)

- The historical **"6"** finding is explained almost exactly by the raw axe
  **pattern** count from a live automated scan alone: `button-name`,
  `label`, `select-name`, `empty-table-header` = 4 axe patterns, plus this
  recount's manual contrast work turning up what that report likely called
  "5 contrast findings" — F8 and F9 together produce 4 individual
  light/dark contrast failures (amber, green, gray×2) across 2 template
  locations, i.e. close enough to "5 contrast findings" to be the same
  discovery, undercounted by one or rounded. Combined with "1 unnamed
  button" (F1), the "6" report is best read as **6 pattern-level findings**,
  and it holds up: every pattern it named is real and still present.
- The historical **"20"** is plausible as a **per-element/per-node** count
  from an axe run that (a) had a working Tailwind CDN so `color-contrast`
  actually evaluated real colors, and (b) counted every row-level instance
  (both CSV rows × the delete-button and checkbox issues, both light+dark
  scans doubling some rows) rather than collapsing to patterns. 2
  (button-name)×2 themes-worth-of-listing + 6 (label) + 2 (select-name) + 1
  (empty-table-header) + contrast nodes for F8/F9 (4 color instances) + the
  focus-indicator instances (7, likely not caught by that prior audit if it
  relied on axe alone, since axe doesn't check focus-ring visibility) can
  plausibly sum to something in the high teens/twenties depending on exactly
  how contrast nodes were enumerated per theme. **Neither historical number
  is fabricated** — "6" reads as patterns, "20" reads as nodes from a run
  with working CSS — but neither is complete: both missed F5 (no aria-live/
  focus-restore on the file-list swap), F6 (five unannounced error regions),
  F7 (invisible required-field format text), F10's true root cause (the
  invalid `ring`/`ring-color` CSS declarations plus six under-specified
  Tailwind ring classes — a focus-indicator failure class neither prior
  audit's tooling would catch), and F11 (missing `<header>` landmark). This
  recount's **31 raw instances / 11 patterns** replaces both prior numbers.

## Out of scope, noticed (other pages / site-wide, not expanded here)

- The `<header>` landmark gap (F11) and the invalid `ring`/`ring-color`
  CSS (root cause of F10) live in shared files (`layouts/base.html`,
  `static/css/styles.css`) and affect every page on the site, not just File
  Manager.
- The entire site's visual styling (colors, dark mode, spacing, and most
  `focus:` treatments) depends unconditionally on `cdn.tailwindcss.com` at
  runtime with zero local/vendored fallback. This is outside the numbered
  ACCESSIBILITY.md points (it's an architecture/reliability risk, not a
  WCAG success criterion), so it is not scored as a FAIL here, but it is
  worth the lead's attention: any user whose network blocks that CDN (this
  sandbox's own egress policy does) gets a completely unstyled page —
  contrast, dark mode, and most focus rings all disappear at once, sitewide.
- Icon-only "Copy" buttons in the YubiKey setup panel (`filemanager.html:977,
  984, 992, 1102`) and the theme-toggle buttons (`base.html:110,128`) rely
  solely on a `title` attribute for their accessible name. This satisfies
  WCAG 4.1.2 and axe's `button-name` rule, but `title`-only names have known
  inconsistent screen-reader support in the field — noted as an observation,
  not a fail, since the constitution and axe are both silent/satisfied here.
