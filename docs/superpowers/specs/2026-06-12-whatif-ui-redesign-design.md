# What-If Page UI Redesign — Design

**Date:** 2026-06-12
**Status:** Approved (brainstorm session with visual companion)
**Scope:** `web/templates/pages/whatif.html`, `web/templates/components/whatif/*`, small new JS for tabs; no engine/handler logic changes beyond template data already available.

## Problem

The what-if page is one very long scroll: ~11 settings cards stacked in the left
column and ~13 result sections stacked in the right column. Three pains, all
confirmed by the user:

1. **Find things faster** — locating a specific result section means scrolling
   through everything.
2. **Visual polish** — dense, flat hierarchy; figures don't align; verdict is
   buried inside Budget Analysis.
3. **Understand results** — the numbers exist but the page never says plainly
   whether the plan works.

## Decision summary

Chosen via mockup comparison: **tabbed results workspace (B) + sticky verdict
bar (A)**, styled with **modern-fintech verdict hero (C) + financial-terminal
monospace numerals (B)**.

## 1. Structure

The two-column grid stays (`lg:grid-cols-3`, settings 1/3, results 2/3).

New elements:

- A **sticky verdict bar** rendered above the grid, below the scenario
  switcher. Sticky on scroll (`position: sticky; top: 0`), condensing to a
  single line. Always visible while editing settings so every HTMX-triggered
  recalculation is immediately legible.
- The results column becomes a **5-tab workspace**. Tabs are client-side
  show/hide; all panels remain in the DOM.
- Settings cards regroup into **3 collapsible groups**.

## 2. Tab mapping

Existing result section templates map to tabs as follows (no section is
removed; Overview gets new composition):

| Tab | Contents |
|-----|----------|
| **Overview** | Verdict hero, 4 KPI tiles (monthly gap @ selected year, MC success %, est. taxes/yr, end balance), projection chart (`whatif-projection-chart`), top alerts (derived from failure points + guardrail events, deep-linking into other tabs), suggested withdrawal mix summary, completeness banner (`whatif-completeness`) |
| **Cash Flow** | `whatif-budget-analysis` (full, incl. at-year-N slider), `whatif-income-chart`, `whatif-projection-breakdown`, `whatif-present-value` |
| **Risk** | `whatif-monte-carlo`, `whatif-historical-backtest`, `whatif-sensitivity`, `whatif-failure-points`, `whatif-guardrail-events` |
| **Taxes & RMD** | `whatif-rmd`; IRMAA/tax figures that already render inside other sections stay with their parent section — no new IRMAA section is created |
| **Strategies** | `whatif-social-security-results`, `whatif-tax-optimizer-results` (this is where Roth conversion recommendations live — there is no separate Roth results template) |

The projection chart appears on Overview only (single canvas; no duplicate
chart instances).

## 3. Settings groups (left column)

| Group | Cards |
|-------|-------|
| **Money In/Out** | portfolio-settings, income-card, healthcare-card, bigticket-card, expense-card |
| **Assumptions** | rate-assumptions, spending-phases |
| **Strategies** | guardrails, roth-conversion, social-security config, scenario-chain |

Each group is a labeled container; each card within keeps its existing
template and HTMX targets but gains a collapse toggle. Collapsed state
persists in `localStorage` keyed by card id.

## 4. Verdict bar

Content, computed from data already present in the render context:

- **One plain-English sentence:** e.g. "Funded through 2049 — spending covered
  for 23 of 38 years." Variants: fully funded ("Funded for all 38 years"),
  shortfall from day one ("Underfunded from the start — gap $X/mo today").
- **Three figures** (monospace): monthly gap @ selected year, Monte Carlo
  success %, required additional withdrawal rate.
- **Health tint:** green (funded full horizon, MC ≥ threshold), amber
  (depletes within horizon or MC marginal), red (gap now / early depletion).
  Exact thresholds chosen during implementation from existing engine outputs;
  must reuse whatever classification Budget Analysis already applies rather
  than inventing a parallel one.

## 5. Visual system

- **Verdict hero / KPI tiles:** tinted surfaces with matching borders
  (green/red/neutral families per the approved mockup), rounded corners,
  small uppercase label + large monospace value.
- **Numerals:** every figure uses `font-mono tabular-nums` so columns align.
  Applied via a shared utility class (e.g. `.num`), not ad-hoc.
- **Color semantics (unchanged, enforced):** costs — taxes, IRMAA, NIIT,
  shortfalls — are red; income/positive green; informational gray. This is an
  existing hard requirement.
- **Dark + light mode** both fully supported, as today (Tailwind `dark:`
  variants).
- **Shared tokens:** the tile/hero/numeral patterns are defined once
  (component templates + utility classes) so dashboard/insights can adopt
  them later. No changes to other pages in this effort.

## 6. Technical approach

- **Tabs are pure client-side show/hide.** All five panels render in
  `whatif-results` as they do today, wrapped in panel divs. Because hidden
  panels stay in the DOM, every existing HTMX OOB swap and full
  `#whatif-results` swap keeps working unmodified — including updates to
  sections in non-active tabs.
- **Tab persistence:** active tab stored in `localStorage` keyed by scenario
  filename; restored on page load and after `htmx:afterSwap` on
  `#whatif-results` (full results swaps replace the panel markup, so the JS
  re-applies the active tab class afterward).
- **Charts in hidden panels:** Chart.js cannot size a canvas inside
  `display:none`. The projection chart lives on Overview (default tab); any
  chart placed in a non-default tab must initialize/resize on first tab
  reveal. Implementation must handle the "income chart in Cash Flow tab"
  case: lazy-init or `chart.resize()` on tab activation.
- **New files:** one JS file (tab + collapse logic), one or two new component
  templates (verdict bar, tab shell). Existing component templates move
  between wrappers but their internal HTMX attributes and element ids do not
  change.
- **No Go logic changes** except: handlers may need to pass already-computed
  values (e.g. depletion year, MC success) to the verdict bar template — read
  from existing analysis results, no new engine computation.

## 7. Comprehension layer

- Each results section gets a one-line plain-English subtitle under its
  header (e.g. Monte Carlo: "If markets repeat history's ups and downs, your
  plan survives X% of 1,000 simulations").
- Overview alerts deep-link: clicking "Portfolio depletes in 2049" switches
  to the Risk tab and scrolls to failure points.
- Verdict sentence is the canonical "does it work" answer; KPI tiles answer
  "how close is it".

## 8. Out of scope

- Restyling dashboard, insights, explorer, filemanager, major-expenses pages
  (follow-on work using the same tokens).
- Any change to projection math, handlers' analysis flow, or the Quick Adjust
  drawer's behavior (it gets the visual token pass only if trivial).
- Mobile-specific redesign (current responsive behavior must not regress:
  single column stacking on small screens, tabs scrollable horizontally).

## 9. Testing

- Update existing whatif render tests for the new structure (these handlers
  are high-priority for coverage).
- New render tests: verdict bar present with correct sentence/figures for a
  funded plan, a depleting plan, and an underfunded-now plan; each tab panel
  contains its mapped section templates; settings groups contain their mapped
  cards.
- Existing OOB-swap render tests must pass unchanged (ids preserved).
- Manual verification in both dark and light mode, plus one HTMX interaction
  per tab to confirm OOB updates land in hidden panels.
