# CB6.2 manifest (attempt 2)

Attempt 1 files unchanged except `web/static/js/charts.js` (see below). All
attempt-1 content (CB6-1, CB6-2, CB6-4, CB6-5, and their tests) is
unmodified in this attempt; see `.swarm/manifests/CB6.1.manifest.md` for
those items.

## CB6-3 a11y fix (checker-a11y FAIL, attempt 1, conceded)

Finding: `web/static/js/charts.js` (both drillable-row sites, breakdown
~238 and credits ~308 pre-fix) combined `hover:bg-gray-50
dark:hover:bg-gray-700` with `focus-visible:ring-indigo-500`. In dark
mode, ring-indigo-500 (#6366f1) against the row's own HOVER background
gray-700 (#374151) computes to 2.31:1 — below ACCESSIBILITY.md #7's
unconditional 3:1 floor for UI component boundaries (WCAG 1.4.11).

### Root cause

The card both rows sit in is `bg-white dark:bg-gray-800`
(`web/templates/pages/dashboard.html` line ~143). `dark:hover:bg-gray-700`
(#374151) is LIGHTER than the card's own #1f2937 (gray-800) background —
so hovering a row in dark mode lightened the background under the ring,
which is what dropped the ring/background contrast below the floor.

### Fix

Changed `dark:hover:bg-gray-700` to `dark:hover:bg-gray-900` (#111827) at
both row sites (identical strings, kept identical per the finding's
instruction). `dark:hover:bg-gray-900` was already a literal class token
elsewhere in the tree (`web/templates/components/whatif/quick-adjust.html`)
so no `make css` regeneration was required — confirmed via `make
css-verify` after the change ("tailwind.css is up to date").

I deliberately did NOT pick `dark:hover:bg-gray-800` (bit-for-bit identical
to the card's own background, and also already vendored) even though it
clears the 3:1 floor (3.29:1) on its own: matching the row's hover to the
card's resting background would make dark-mode hover visually
indistinguishable from non-hover, regressing the interactive affordance
for mouse users while fixing the number on paper. `-900` genuinely
darkens (visible hover feedback, distinct from the card's `-800`) and
clears the floor with more margin.

### Contrast arithmetic (hex values pulled from the served
`web/static/css/tailwind.css`, WCAG relative-luminance formula, computed
myself — not assumed from Tailwind's documented default palette)

Colors used, confirmed present in the compiled stylesheet:
- `focus-visible:ring-indigo-500` -> `rgb(99 102 241)` = `#6366f1`
- `hover:bg-gray-50` (light, unchanged) -> `rgb(249 250 251)` = `#f9fafb`
- `dark:hover:bg-gray-700` (OLD, removed) -> `rgb(55 65 81)` = `#374151`
- `dark:hover:bg-gray-900` (NEW) -> `rgb(17 24 39)` = `#111827`
- `dark:bg-gray-800` (card's own dark background, non-hover baseline) ->
  `rgb(31 41 55)` = `#1f2937`
- page background (light, non-hover baseline) -> `#ffffff`

Computed ratios (script: relative luminance per WCAG 2.x, `(L1+0.05)/
(L2+0.05)` with L1 >= L2):
- indigo-500 vs OLD dark hover gray-700: **2.31:1** — FAIL (reproduces the
  checker's reported number exactly).
- indigo-500 vs NEW dark hover gray-900: **3.97:1** — PASS (>= 3:1 floor).
- indigo-500 vs dark non-hover (card bg-800, unaffected by this change):
  **3.29:1** — PASS (matches the checker's "3.29:1" dark non-hover figure;
  re-verified untouched).
- indigo-500 vs light hover gray-50 (unaffected by this change): **4.27:1**
  — PASS (matches the checker's "4.27:1" figure).
- indigo-500 vs light non-hover (white page bg, unaffected by this
  change): **4.47:1** — PASS (matches the checker's "4.47:1" figure).

All three passing baselines the checker reported (4.47/4.27/3.29) recompute
identically post-fix — confirming light theme was not touched and the
already-passing dark non-hover state is unaffected. The only number that
changed is the failing one, from 2.31:1 to 3.97:1.

### Path taken on the "class not vendored" constraint

Not applicable: `dark:hover:bg-gray-900` was already present in the
compiled `web/static/css/tailwind.css` (used elsewhere in the tree), so no
`make css` regeneration was needed. `make css-verify` still reports
"tailwind.css is up to date" (no rebuild-and-commit step required).

## Verification (attempt 2)

- `make check` (vet, staticcheck, govulncheck, css-verify, test) — all
  green; `make css-verify` -> "tailwind.css is up to date".
- `go test -count=1 ./...` — every package `ok`, no failures (ran
  uncached, not relying on `make check`'s cached run).
- `go build ./...` / `go vet ./...` — clean (implied by `make check`'s
  `vet`/`static` steps; also re-confirmed directly).

## Scope note

Same five files as attempt 1; the only content change in this attempt is
the two identical `dark:hover:bg-gray-700` -> `dark:hover:bg-gray-900`
edits in `web/static/js/charts.js` (plus an explanatory comment at each
site). No other file touched.
