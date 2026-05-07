# Guardrail Effects Visibility — Design

**Date:** 2026-05-07
**Status:** Approved (brainstorming → ready for plan)
**Branch (target):** TBD (off `dev`)

## Problem

The What-If page already supports a simplified portfolio drop/rise guardrail that adjusts retirement spending each year based on portfolio performance. The mechanic works correctly, but the user-facing surface only exposes:

- **Guardrail Events panel** — lists each year a guardrail fired with the new spending *multiplier* (e.g. "Cut to 90%") and the portfolio value at the time.
- **Year-by-Year Projection table** — shows guardrail-adjusted spending in the "Spending" column, but with no indication of what spending would have been *without* guardrails or that a guardrail was responsible for the change.

The user cannot answer the obvious question — "What does enabling guardrails actually do to my monthly spending?" — without manually toggling the feature on and off and eyeballing the chart.

## Goals

1. Make the per-year **adjusted vs. planned** spending visible inline in the existing Year-by-Year table.
2. Make each Guardrail Event self-explanatory by showing the **dollar** impact (`$X/mo → $Y/mo`), not just the multiplier %.
3. Provide an **optional counterfactual overlay** on the projection chart: a dashed "without guardrails" balance series so the user can see the longevity / final-balance impact without re-running the page with the toggle off.

## Non-Goals

- No move toward the full four-rule Guyton-Klinger (2006) model. That is tracked separately at `docs/superpowers/specs/2026-05-06-full-gk-guardrails-followup.md`.
- No monthly drill-down. Guardrails evaluate at year boundaries; yearly granularity is sufficient.
- No persistence changes. All new fields are in-memory projection results.
- No change to guardrail evaluation semantics. This is purely a presentation feature.

## Architecture

Three independently shippable slices, all reading from a single projection result. Slices A and B are zero-marginal-cost (data already computed inside the projection loop); slice C is gated behind explicit user opt-in because it doubles projection compute.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Project()  (existing)                                               │
│    ├─ tracks currentLivingExpenses (planned, pre-multiplier)         │
│    ├─ applies grState.multiplier() to get adjusted spending          │
│    └─ emits ProjectionMonth + GuardrailEvent records                 │
│                                                                      │
│  ── slice A enriches the records, no behavior change ────            │
│    + ProjectionMonth.PlannedLivingExpenses                           │
│    + ProjectionMonth.GuardrailMultiplier                             │
│    + ProjectionYearSummary.PlannedExpenses                           │
│    + ProjectionYearSummary.GuardrailMultiplier                       │
│    + GuardrailEvent.MonthlySpendingBefore/After                      │
└──────────────────────────────────────────────────────────────────────┘
        │                                  │
        ▼                                  ▼
┌──────────────────────┐       ┌────────────────────────────┐
│  slice B — UI        │       │  slice C — counterfactual  │
│   • table stacked    │       │   • new endpoint           │
│     cell             │       │     /whatif/chart/         │
│   • events row       │       │     projection/no-         │
│     enrichment       │       │     guardrails             │
│   • row-fire accent  │       │   • toggle button + dashed │
│                      │       │     overlay on chart       │
└──────────────────────┘       └────────────────────────────┘
```

## Data Model Changes

### `internal/models/whatif.go`

```go
// ProjectionMonth (add):
PlannedLivingExpenses float64 `json:"planned_living_expenses,omitempty"`
GuardrailMultiplier   float64 `json:"guardrail_multiplier,omitempty"` // 1.0 when disabled

// ProjectionYearSummary (add):
PlannedExpenses     float64 `json:"planned_expenses,omitempty"`
GuardrailMultiplier float64 `json:"guardrail_multiplier,omitempty"` // value at year-end

// GuardrailEvent (add):
MonthlySpendingBefore float64 `json:"monthly_spending_before"`
MonthlySpendingAfter  float64 `json:"monthly_spending_after"`
```

`PlannedLivingExpenses` / `PlannedExpenses` is `MonthlyLivingExpenses × phaseMult × cumulativeInflation` — what the spending plan would say absent any guardrail adjustment. It does *not* include healthcare, big-ticket items, or expense-source contributions; the goal is an isolated comparison of the guardrail's direct effect on the living-expense line that the multiplier touches.

`GuardrailMultiplier` defaults to `1.0` when guardrails are disabled (`grState == nil`). The yearly summary stores the multiplier as of the end of that calendar year.

### `internal/services/retirement/calculator.go`

At `calculator.go:1141-1180` (the monthly projection loop):
1. Capture `currentLivingExpenses` (planned) into `ProjectionMonth.PlannedLivingExpenses` before the guardrail multiplier is applied.
2. Capture the active multiplier (`grState.multiplier()` if non-nil, else `1.0`) into `ProjectionMonth.GuardrailMultiplier`.
3. When emitting a `GuardrailEvent` (`calculator.go:1162-1167`), compute and attach `MonthlySpendingBefore` (= planned × prevMult) and `MonthlySpendingAfter` (= planned × newMult).

At `calculator.go:1274` (yearly aggregation):
1. Sum `month.PlannedLivingExpenses` into `currentYearSummary.PlannedExpenses` for each month in the year (parallel to the existing `Expenses` aggregation but using the planned value, plus healthcare/big-ticket/etc. for parity if those don't get the multiplier — confirm during implementation).
2. Set `currentYearSummary.GuardrailMultiplier` to the multiplier value from the last month of the year.

### New endpoint — `internal/handlers/whatif/handlers.go`

```
GET /whatif/chart/projection/no-guardrails?display_dollars=nominal|real
```

Returns the same JSON shape as the existing `/whatif/chart/projection`, but built from a projection where `s.Guardrails = nil` (or a copy with `Enabled=false`). Used only for the dashed overlay; the primary chart endpoint still reflects whatever the user has configured.

**Caching:** keyed by a hash of the relevant projection-input fields (settings JSON + scenario id), single-entry process-local cache. Re-toggling the overlay during a session is then free; any settings change invalidates. If implementation discovers no clean hash key, ship without caching — Slice C is opt-in and a single re-compute on click is acceptable.

## UI Changes

### Year-by-Year Projection table — `web/templates/components/whatif/projection-breakdown.html`

Replace the single "Spending" cell with a stacked layout:

```
$48,200          ← .Expenses (existing, post-multiplier)
$53,500 · ×0.90  ← .PlannedExpenses · multiplier badge
                   (the second line is rendered ONLY when mult ≠ 1.00)
```

Multiplier badge color: red text/bg tint for `mult < 1.00`, green for `mult > 1.00`.

When a row corresponds to a year in which a guardrail event fired (cross-reference `Projection.GuardrailEvents` by `.Year`), render a 3-px left border on the entire row: red for `cut`, green for `raise`. No full-row tint — the existing zebra striping stays the dominant visual rhythm.

### Guardrail Events panel — `web/templates/components/whatif/guardrails.html` (`whatif-guardrail-events`)

Existing line:
```
Year 2031:  Cut to 90%                              Portfolio: $812,400
```

New line:
```
Year 2031:  Cut to 90%   $5,400/mo → $4,860/mo      Portfolio: $812,400
```

`$5,400 → $4,860` rendered with red/green tint matching event type. Falls back gracefully (renders only the % change) if `MonthlySpendingBefore == 0`, e.g. for older serialized data.

### Projection Chart card — `web/templates/components/whatif/projection-chart.html`

Add a third toggle button to the right of the existing Nominal / Today's Dollars group:

```
[ Nominal | Today's Dollars ]   [ + Compare without guardrails ]
```

Behavior:
- Hidden entirely when `.Settings.Guardrails == nil || !.Settings.Guardrails.Enabled`.
- When clicked: fetches `/whatif/chart/projection/no-guardrails?display_dollars=<current>`, adds a dashed series ("Without guardrails") to the Plotly figure, button switches to pressed state.
- Caption underneath the chart updates with a one-line delta:
  ```
  Without guardrails: portfolio depletes 4 years sooner; final balance −$210,000.
  ```
  (Caption text is computed in the handler, not the template, so depletion-year math lives in Go.)

## Error Handling

- New `ProjectionMonth` and `ProjectionYearSummary` fields default to zero-value when guardrails are disabled. Templates must check `.GuardrailMultiplier != 0 && .GuardrailMultiplier != 1.0` before rendering the badge to avoid spurious "×1.00" output and avoid divide-by-zero in delta math.
- The no-guardrails endpoint returns the same error envelope as the primary chart endpoint. JS adds the dashed series only on `200`; on error, the toggle button reverts to its idle state and a console warning is logged.
- If `Projection == nil` (no projection ran yet), the breakdown table is already not rendered (existing behavior); no new branch needed.

## Testing

### Slice A (data plumbing)
- Unit test in `internal/services/retirement/`: with guardrails disabled, every `ProjectionMonth.GuardrailMultiplier == 1.0` and `PlannedLivingExpenses == GeneralExpenses` (modulo healthcare/big-ticket if they're excluded from planned).
- Unit test: with guardrails configured to fire a cut at year 5, the year-5 `GuardrailEvent.MonthlySpendingAfter == MonthlySpendingBefore × (1 - FloorCutPct/100)` within float tolerance.
- Existing guardrail tests in `guardrails_test.go` continue to pass unchanged.

### Slice B (UI)
- Handler test (template render) — render the breakdown with a fixture projection containing one cut and one raise event and assert the response body contains both the planned $ and the multiplier badge.
- Manual smoke: load `/whatif`, enable guardrails with default thresholds, click Apply, verify the table shows stacked cells in the years guardrails fired and the events panel shows `$/mo → $/mo`.

### Slice C (counterfactual)
- Handler test: `GET /whatif/chart/projection/no-guardrails` returns 200 with non-empty data when guardrails are enabled in settings, and 400 (or 200 with empty payload) when guardrails are disabled.
- Handler test: response body for the no-guardrails endpoint is independent of the configured guardrail thresholds (sanity that the projection actually disabled them).
- Manual smoke: toggle the chart overlay, see the dashed series, see the caption update.

## Slice Order & Sizing

| Slice | Files touched | Approx. LOC | Tests |
|---|---|---|---|
| A. Data plumbing | `models/whatif.go`, `services/retirement/calculator.go`, `services/retirement/guardrails.go` | ~40 | 2 unit |
| B. Table + events UI | `projection-breakdown.html`, `guardrails.html` | ~30 | 1 handler |
| C. Counterfactual endpoint + toggle | `handlers/whatif/handlers.go`, `projection-chart.html`, plus the chart-wiring JS | ~80 | 2 handler |

Total ≈ 150 LOC + tests. Slice A is pure groundwork (no user-visible change). Slices B and C each unlock independent value and can ship in either order after A.

## Open Questions

- **Planned-expenses scope:** does `PlannedExpenses` in `ProjectionYearSummary` include healthcare and big-ticket items (so it's a full apples-to-apples comparison with `Expenses`) or only the living-expense line that the multiplier touches? Recommendation: include them — they're identical in both worlds — so the only difference between `PlannedExpenses` and `Expenses` is the guardrail effect itself.
- **Counterfactual chart-toggle granularity:** does the dashed overlay need its own real/nominal toggle wiring, or does it always follow the primary chart's mode? Recommendation: always follow.

These are flagged for resolution during the writing-plans pass; both have an obvious default that the implementer can pick if no further input arrives.
