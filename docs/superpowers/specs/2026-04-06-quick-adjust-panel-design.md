# Quick Adjust Floating Panel

## Context

The what-if page has 17+ sliders spread across 5 settings cards in the left column. When adjusting sliders in lower cards (healthcare, rate assumptions, spending phases), the results in the right column scroll out of view. Users want to tweak sliders while keeping results visible — without redesigning the page layout.

## Design

A floating panel that provides quick access to all sliders from anywhere on the page. The existing inline sliders remain in their cards. Both sets stay in sync.

### Panel UI

- **Toggle button**: Fixed to bottom-right corner of viewport. Sliders icon. Dark background (`bg-gray-800`), rounded, subtle shadow.
- **Panel**: Expands above the toggle button. ~320px wide, max-height ~60vh with overflow scroll. Dark theme (`bg-gray-800 text-white`) to visually distinguish from page content. Rounded corners, drop shadow.
- **Category tabs**: Horizontal tab pills across the top of the panel:
  1. **Portfolio** — Portfolio value, Monthly expenses, Projection years
  2. **Rates** — Inflation, Spending decline, Return override, Tax-deferred delay
  3. **Phases** — Phase multiplier sliders (one per configured phase)
  4. **Healthcare** — Per-person: monthly cost, inflation, employer years, medicare cost (conditional on coverage type)
- **Slider layout**: Compact vertical list within each tab. Each slider shows: label, current value (right-aligned), and a range input. Tighter spacing than the inline cards.
- **Close**: Click toggle button again, or click outside panel.

### Syncing

The floating panel sliders are mirrors of the inline sliders, wired to the same HTMX endpoints.

**IDs**: Each panel slider gets a `qp-` prefixed ID mirroring the inline slider's ID (e.g., `qp-inflation-rate-slider` mirrors `inflation-rate-slider`). Display labels get the same prefix pattern (e.g., `qp-inflation-rate-display`).

**Panel → Inline sync (JS)**:
When a panel slider's `oninput` fires:
1. Update the corresponding inline slider's `.value`
2. Update the inline slider's display label
3. The panel slider fires the same `hx-post` to the same endpoint with the same `name` attribute

**Inline → Panel sync (OOB)**:
Server responses already include OOB swap targets for display elements. Extend the OOB `<template>` blocks in `whatif-results` to include `qp-` prefixed elements. When the server responds to any slider change, it sends OOB updates for both the inline and panel versions.

**No server-side handler changes needed.** The panel sliders POST to the same endpoints with the same form field names. Only the response templates need additional OOB targets.

### Dynamic Content

- **Healthcare tab**: Content is rebuilt when the `healthcare-persons-list` OOB update fires. Add a `qp-healthcare-tab-content` div that gets OOB-swapped with the current healthcare sliders.
- **Phases tab**: Content is rebuilt when spending phases change. Add a `qp-phases-tab-content` div that gets OOB-swapped.
- **Portfolio range dropdown**: The panel includes a compact version of the range dropdown. When it changes, it updates both sliders' min/max/step via the same JS function (`updatePortfolioRange`).

### Panel State

Purely client-side:
- Open/closed state toggled by button click
- Active tab remembered in a JS variable (resets on page load)
- No server persistence needed — it's a transient control surface

## Files to Modify

- `web/templates/pages/whatif.html` — Include new `whatif-quick-adjust` template, and add `qp-` OOB swap targets in the existing `whatif-results` `<template>` block (all OOB swaps are centralized here)
- `web/templates/components/whatif/quick-adjust.html` — **New file**: floating panel template with toggle button, tabs, and all slider mirrors
- `web/templates/components/whatif/quick-adjust-scripts.html` — **New file**: JS for panel toggle, tab switching, and bidirectional slider sync
- `web/static/css/styles.css` — Minimal additions for panel positioning if Tailwind classes aren't sufficient

### Existing Code to Reuse

- `updatePortfolioRange()` in `whatif-portfolio-scripts` — extend to also update `qp-portfolio-slider`
- `updatePhaseSliderLabel()` in `whatif-spending-phases-scripts` — extend to also update `qp-` phase sliders
- `updateSpendingPreview()` in `whatif-spending-preview-scripts` — no changes needed (it reads from the canonical slider)
- OOB pattern in `whatif-results` template — follow the same `<template>` + `hx-swap-oob="true"` pattern for panel elements

## Verification

1. Open the what-if page, scroll to the bottom of settings
2. Click the floating button — panel should appear with Portfolio tab active
3. Adjust portfolio slider in panel — results update, inline slider syncs
4. Switch to Rates tab, adjust inflation — results update, inline slider syncs
5. Adjust an inline slider directly — panel slider value syncs on next server response
6. Add/remove a healthcare person — Healthcare tab content updates
7. Test on mobile viewport — panel should be full-width or hidden on very small screens
8. Test dark mode — panel already dark, ensure no contrast issues with page
9. Close panel, verify no leftover state issues
