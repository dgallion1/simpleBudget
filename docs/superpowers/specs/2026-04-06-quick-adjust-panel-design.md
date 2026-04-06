# Quick Adjust Floating Panel

## Problem

The what-if page spreads the most frequently adjusted controls across several cards in the left column:

- `Portfolio & Expenses`
- `Rate Assumptions`
- `Retirement Spending Phases`
- `Healthcare Costs`

Once a user scrolls down to lower cards, the results in `#whatif-results` are no longer visible. The current layout works, but it forces users to choose between changing inputs and seeing the effect.

## Goal

Add a floating quick-adjust panel that gives users fast access to the highest-frequency sliders without changing the existing page layout or adding new server endpoints.

The panel is a secondary control surface. The inline cards remain the canonical UI and continue to own HTMX form submission, validation, and dynamic server-rendered content.

## Non-Goals

- Do not replace or remove the existing inline cards.
- Do not create duplicate server handlers for panel controls.
- Do not mirror every control on the page. V1 is slider-focused.
- Do not attempt a separate mobile-specific redesign in this pass. Hide the panel below `lg` unless implementation proves trivial.

## Proposed UX

### Entry Point

- Add a fixed toggle button in the bottom-right corner on `lg` screens and above.
- Use a compact sliders/tune icon plus a short label such as `Quick Adjust`.
- Button opens and closes the panel.

### Panel Shell

- Render as a floating popover anchored above the toggle button.
- Width: about `20rem`
- Max height: about `65vh`
- Internal content scrolls independently.
- Visual treatment should clearly separate it from page content: elevated surface, rounded corners, strong shadow, dark neutral palette is fine if it matches the current theme well.
- Close on:
  - toggle button click
  - outside click
  - `Escape`

### Tabs

V1 tabs should map to the current template structure:

1. `Portfolio`
   - Portfolio value
   - Monthly living expenses
   - Projection years
   - Portfolio range dropdown
2. `Rates`
   - Inflation rate
   - Spending decline rate, only when spending phases are disabled
   - Investment return override
   - Delay tax-deferred withdrawals
3. `Phases`
   - One spending multiplier slider per configured phase
4. `Healthcare`
   - Per person:
     - monthly cost
     - employer coverage years, when applicable
     - ACA cost after employer, when applicable
     - pre-Medicare inflation, when applicable
     - Medicare monthly cost, when applicable
     - post-Medicare inflation

The panel should not include account-allocation inputs, age inputs, scenario management, or add/remove forms in V1.

## Architecture

### Canonical Source of Truth

The inline controls remain canonical. The floating panel mirrors them.

That matters because the current what-if page already has form ownership and HTMX behavior distributed across existing templates:

- `portfolio-settings.html` posts to `/whatif/settings`
- `rate-assumptions.html` posts to `/whatif/settings`
- `spending-phases.html` posts to `/whatif/spending-phases`
- each healthcare person card submits to `/whatif/healthcare/{id}`

The panel should not post directly to those endpoints with duplicate forms. Instead, panel controls should proxy changes into the existing inline controls and let the existing forms submit exactly as they do now.

### Sync Model

#### Panel -> Inline

When a panel control changes:

1. Find the matching inline control.
2. Copy the new value into the inline control.
3. Fire the same client-side behavior the inline control expects:
   - `input` for live label updates
   - `change` for HTMX form submission
4. Reuse existing helper functions where needed:
   - `updatePortfolioRange()`
   - `updatePhaseSliderLabel()`
   - `updateSpendingPreview()`
   - `updateInvestmentReturnDisplay()`

This keeps all network behavior flowing through the current inline forms and avoids a second submission path that could drift.

#### Inline -> Panel

Panel controls should stay in sync from the browser, not from duplicated server-rendered OOB labels.

Use shared client-side sync that:

- listens for `input` and `change` events on canonical inline controls
- updates the matching panel control and display label immediately
- re-runs after HTMX swaps so dynamic content stays wired up

Recommended events to hook:

- `DOMContentLoaded`
- `htmx:afterSwap`
- `htmx:oobAfterSwap`

### Control Mapping

The current templates are inconsistent about IDs:

- some sliders already have IDs
- some mirrored controls do not
- several labels depend on `nextElementSibling`, which is too brittle for a second UI surface

Before adding the panel, give mirrored inline controls stable hooks via `data-quick-adjust-key` and, where needed, `data-quick-adjust-display`.

Examples:

- `portfolio_value`
- `monthly_living_expenses`
- `projection_years`
- `inflation_rate`
- `spending_decline_rate`
- `investment_return`
- `tax_deferred_delay_years`
- `phase:0:multiplier`
- `healthcare:<personID>:current_monthly_cost`

Panel controls use the same key through `data-quick-adjust-mirror`.

This is preferable to a `qp-` ID convention because it avoids duplicating the full inline DOM naming scheme and makes dynamic controls easier to bind.

## Dynamic Sections

### Spending Phases Tab

The phases list is dynamic and already changes through HTMX.

Implementation approach:

- Render the panel tab content from the same phase data used by `whatif-spending-phases`.
- Add a dedicated container such as `#quick-adjust-phases-content`.
- When phases are added, removed, reset, or toggled, refresh that container after the HTMX response.

The easiest path is to return an additional OOB fragment for the panel phases content alongside the existing phase updates. That keeps the panel structure server-rendered while still using browser-side value sync between requests.

### Healthcare Tab

Healthcare controls are even more dynamic because each person has:

- a separate server endpoint
- conditional fields based on coverage type
- per-person IDs

Use the same pattern as phases:

- render a dedicated `#quick-adjust-healthcare-content` container
- populate it from the current healthcare persons list
- refresh it via OOB when healthcare persons are added, updated, removed, or when their coverage type changes

Do not try to keep the healthcare tab purely static with JS-only show/hide logic. The existing server-rendered conditional fields should remain the source of truth.

## Template Layout

### New Templates

- `web/templates/components/whatif/quick-adjust.html`
  - panel shell
  - toggle button
  - tab buttons
  - compact mirrored control markup
  - subtemplates for phases and healthcare tab bodies
- `web/templates/components/whatif/quick-adjust-scripts.html`
  - panel open/close behavior
  - outside click and escape handling
  - canonical-to-mirror sync
  - mirror-to-canonical proxy behavior
  - rebinding after HTMX swaps

### Existing Files to Update

- `web/templates/pages/whatif.html`
  - render `whatif-quick-adjust` outside the main grid so it floats independently
  - include `whatif-quick-adjust-scripts`
  - add OOB targets for quick-adjust dynamic tab content only
- `web/templates/components/whatif/portfolio-settings.html`
  - add stable hooks to mirrored controls
  - extend `updatePortfolioRange()` so it keeps the panel range select and mirror display in sync
- `web/templates/components/whatif/rate-assumptions.html`
  - add stable hooks to mirrored sliders and labels
  - avoid relying on anonymous sibling spans for mirrored values
- `web/templates/components/whatif/spending-phases.html`
  - add stable hooks for phase multiplier controls
  - emit OOB replacement for quick-adjust phase content
  - extend `updatePhaseSliderLabel()` so mirrored labels can update from the same helper
- `web/templates/components/whatif/healthcare-card.html`
  - emit OOB replacement for quick-adjust healthcare content
- `web/templates/components/whatif/healthcare-person.html`
  - add stable hooks for mirrored per-person controls

### CSS

Tailwind utility classes should be enough for V1. Do not add global CSS unless a specific layering or animation issue cannot be solved locally in template classes.

## Interaction Details

### Tab Defaults

- Default active tab: `Portfolio`
- Active tab is client-side only.
- Open/closed state is client-side only.
- No persistence across full page reloads.

### Accessibility

- Toggle button should expose `aria-expanded` and `aria-controls`.
- Tab buttons should have clear active state and keyboard focus styles.
- `Escape` closes the panel and returns focus to the toggle button.
- Keep text labels visible. Do not rely on icon-only sliders.

### Small Screens

V1 recommendation: hide the floating panel below `lg`.

Rationale:

- the page already collapses into a single-column layout
- a floating overlay adds complexity on small screens
- the main usability problem is loss of visibility between left-column controls and right-column results on larger layouts

If a mobile treatment is later needed, build it as a bottom sheet variant rather than forcing the desktop popover to work everywhere.

## Why This Design

The important design constraint is that the what-if page already uses existing inline forms as the contract between browser and server. A mirrored panel is only safe if it proxies into those controls instead of duplicating submission logic.

That gives us:

- no new handlers
- no duplicated validation path
- no risk that panel posts diverge from inline form payloads
- minimal server changes outside dynamic OOB partials for healthcare and phases

## Verification

1. Open the what-if page on a desktop-width viewport.
2. Open the quick-adjust panel and confirm the `Portfolio` tab is selected.
3. Change portfolio value in the panel and verify:
   - the inline slider updates immediately
   - the existing `/whatif/settings` flow runs
   - results update
4. Change monthly living expenses in the panel and verify the spending preview still reacts correctly.
5. Change inflation and investment return in the `Rates` tab and verify existing inline displays stay correct.
6. If phases are disabled, verify spending decline appears in the panel. If phases are enabled, verify it does not.
7. Add, remove, and edit spending phases inline and verify the `Phases` tab refreshes and remains functional.
8. Add, remove, and edit healthcare people inline and verify the `Healthcare` tab refreshes, including coverage-specific fields.
9. Change a mirrored inline slider directly and verify the panel updates without a page reload.
10. Press `Escape`, click outside, and re-click the toggle button to verify close behavior.
11. Verify the button and panel are hidden below `lg`.

## Implementation Notes

Post-review fixes applied to the initial implementation:

1. **Panel max-height constraint**: `max-h-[65vh]` is on the panel shell (not the scrollable content area), with `flex flex-col` layout so the tab bar is `flex-shrink-0` and the content area is `flex-1 overflow-y-auto`. This prevents the panel from overflowing the viewport on shorter screens.

2. **Investment return display — single authority**: `updateInvestmentReturnDisplay()` in `rate-assumptions.html` is the sole formatter for `investment_return` displays. The `formatQuickAdjustDisplay()` function in `quick-adjust-scripts.html` skips the `investment-return` format case to avoid a race condition where both functions write different content to the same element.

3. **Panel-aware color classes**: `updateInvestmentReturnDisplay()` checks `display.closest('#quick-adjust-panel')` to decide whether to emit dark-panel-only classes (e.g. `text-green-400`) or dual-mode classes (e.g. `text-green-600 dark:text-green-400`). Uses DOM methods (`createElement`/`appendChild`) instead of `innerHTML`.
