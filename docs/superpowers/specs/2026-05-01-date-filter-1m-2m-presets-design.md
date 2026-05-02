# Add 1M and 2M Date-Filter Presets

**Date:** 2026-05-01
**Status:** Approved
**Scope:** Frontend only

## Problem

The four pages with date-range filters currently jump from "All" / "YTD" to a minimum window of 3 months. Users analyzing very recent activity have no preset for the natural "last month" or "last two months" windows and must adjust the date inputs manually.

## Goal

Add `1M` and `2M` preset buttons to every page that already exposes month-based date-range presets, slotted between the longest-fixed-window/YTD anchor and the existing `3M` button. Behavior, styling, and selection-state highlighting must match the surrounding presets exactly.

## Affected Pages

| Page | Existing presets | New ordering |
|---|---|---|
| `web/templates/pages/dashboard.html` | YTD, 3M, 6M, 12M, All | YTD, **1M**, **2M**, 3M, 6M, 12M, All |
| `web/templates/pages/explorer.html` | ← 3M, 6M, 12M, All → | ← **1M**, **2M**, 3M, 6M, 12M, All → |
| `web/templates/pages/insights.html` | 3M, 6M, 12M, All | **1M**, **2M**, 3M, 6M, 12M, All |
| `web/templates/pages/major-expenses.html` | ← 3M, 6M, 12M, All → | ← **1M**, **2M**, 3M, 6M, 12M, All → |

(`←` and `→` denote the step-back/step-forward arrow buttons that flank the preset row on those two pages.)

## Per-File Changes

### `web/templates/pages/dashboard.html`

Add two `<button type="button">` elements directly after the `YTD` button (line ~70) and before the `3M` button (line ~72):

- `onclick="setPreset('1m')"`, `data-preset="1m"`, label `1M`
- `onclick="setPreset('2m')"`, `data-preset="2m"`, label `2M`

Class list: copy verbatim from the adjacent `3M` button so light/dark idle styles match. The active-state styling is applied imperatively in JS — no template change needed for that.

### `web/static/js/dashboard.js`

In `setPreset(preset)` (line 72–96), add two cases to the switch:

```js
case '1m':
    start.setMonth(start.getMonth() - 1);
    break;
case '2m':
    start.setMonth(start.getMonth() - 2);
    break;
```

The button-highlight loop at lines 102–111 keys off `btn.dataset.preset` and needs no change.

### `web/templates/pages/explorer.html`

Add two `<button>` elements before the `3M` button (line ~85) and after the step-back arrow (line ~79):

- `onclick="setDateRange(1)"`, `data-months="1"`, label `1M`
- `onclick="setDateRange(2)"`, `data-months="2"`, label `2M`

Class list: copy from adjacent `3M` button. `setDateRange(months)` (line 253) already accepts any month count, so no JS change is needed for the click path.

In `detectSelectedDateRange` (line 307), update the iteration on line 328:

```js
for (const months of [1, 2, 3, 6, 12]) {
```

This makes the active-state highlight pick up `1M` and `2M` after an HTMX swap or first paint.

### `web/templates/pages/insights.html`

Add two `<button>` elements before the `3M` button (line ~34):

- `onclick="setInsightPreset('1m')"`, `data-preset="1m"`, label `1M`
- `onclick="setInsightPreset('2m')"`, `data-preset="2m"`, label `2M`

Class list: copy verbatim from the existing `3M` button, **including the `{{if eq .Preset "1m"}}…` / `"2m"` Go-template ternary** so the server-rendered active state highlights correctly when a `?preset=1m` URL is loaded directly.

In `setInsightPreset` (line 558), add cases to the switch (line 565):

```js
case '1m':
    start.setMonth(start.getMonth() - 1);
    break;
case '2m':
    start.setMonth(start.getMonth() - 2);
    break;
```

### `web/templates/pages/major-expenses.html`

Mirrors the explorer pattern but uses the page-prefixed `meSetDateRange(months)` helper. Add two `<button>` elements before the `3M` button (line ~28), after the step-back arrow:

- `onclick="meSetDateRange(1)"`, `data-months="1"`, label `1M`
- `onclick="meSetDateRange(2)"`, `data-months="2"`, label `2M`

Class list: copy verbatim from adjacent `3M` button (`date-range-btn px-3 py-1 text-sm …`).

In `meDetectSelectedDateRange` (line 195), update:

```js
for (const months of [1, 2, 3, 6, 12]) {
```

## Backend

No changes. The insights handler (`internal/handlers/insights/handlers.go:832`) reads `preset` as an opaque pass-through string used only for active-state highlighting; it does not validate against an allow-list. Other pages don't read the preset value server-side at all (date inputs carry the actual range).

## Styling

No new CSS, no new design tokens. Every new button copies its class list verbatim from the adjacent `3M` button on the same page so:

- Idle / hover states match in light and dark mode
- Active-state class swap (whether imperative JS or Go-template ternary) applies to the new buttons by the same code path
- Spacing inside the existing `flex gap-1` container is automatic

## Verification

1. `make build` — confirm all four templates still parse
2. `go test ./...` — sanity check (no Go code is touched, but expected to remain green)
3. Manual browser pass on each of the four pages:
   - Click `1M`: start date ≈ today − 1 month, end date ≈ today, `1M` highlighted as active
   - Click `2M`: start date ≈ today − 2 months, `2M` highlighted
   - Click `3M`: still works, `3M` highlighted (regression check)
   - On explorer + major-expenses: after clicking `1M`, click step-forward `→` then step-back `←` and confirm highlight reappears correctly post-HTMX-swap
   - Light + dark mode visual parity with `3M`
4. On insights only: load `/insights?preset=1m` directly and confirm the server-rendered active state highlights `1M` (validates the Go-template ternary)

## Risk

Low. Pure additive UI; no shared Go symbol changes; no DB / file format / handler signature impact. The `1`-month subtraction has the same edge-case behavior as the existing `3`/`6`/`12`-month subtractions (JavaScript `Date.setMonth` clamps to the last valid day of the target month), and that behavior already ships in production.

## Out of Scope

- Adding presets to other date pickers in the app that aren't already in the month-preset family (e.g., transaction-detail filters, scenario date inputs)
- Renaming the `12M` preset to `1Y` or otherwise restructuring the existing labels
- Server-side validation of the `preset` query parameter
- Persisting the user's last-used preset across sessions
