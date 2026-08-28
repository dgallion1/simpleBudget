# T15 — Conversion sweep panel on the what-if page

## Why
The Tax Optimizer ranks composite strategies, but there is no dose-response
view: "what does each flat annual Roth conversion amount do to this plan?"
The user wants a table like:

| Annual conversion | Portfolio lasts | Lifetime tax | Lifetime IRMAA |
|---|---|---|---|
| $0 | 455 mo (~38 yrs) | $3.15M | $0 |
| … | | | |

## Constraint that shaped the design (do not violate)
`retirement.RunFull` costs ~7 s (Monte Carlo + backtest + SS-grid fan-out).
The sweep must NOT call it. One deterministic `eng.Run(in)` is milliseconds;
the sweep is 9 deterministic runs, so the endpoint must complete well under
2 s. No Monte Carlo anywhere in this feature.

## Behavior
- New route `POST /whatif/conversion-sweep`, registered in
  `internal/handlers/whatif/handlers.go` alongside `/whatif/tax-optimize`.
- Handler (new file `internal/handlers/whatif/handlers_sweep.go`):
  1. Load saved settings via `retirementMgr.Load()`.
  2. Candidate amounts: 0, 25000, 50000, 75000, 100000, 125000, 150000,
     175000, 200000 — plus the saved plan's current annual conversion amount
     if it is not already in that list (insert in order, no duplicates).
  3. For each amount: deep-copy the settings (mirror the copy discipline of
     `candidateSettingsForSS` in
     `internal/services/retirement/analysis/tax_optimizer.go` — never mutate
     the loaded settings), set the Roth conversion config to that annual
     amount (enabled = amount > 0; preserve the saved start/end years), build
     engine input, run **one deterministic** `eng.Run(in)` via `getEngine()`,
     and derive per-row metrics:
     - portfolio longevity: depletion month when the projection does not
       survive (render as "N mo (~Y yrs)"), otherwise "survives" plus the
       final balance in real dollars,
     - lifetime tax: per-year `Taxes` deflated by that year's
       `CumulativeInflation`, summed (real dollars — mirrors the Tax
       Optimizer's `LifetimeTaxReal`; the original "reuse `BuildTax`
       totals" wording specified a nominal figure and was the cause of
       the attempt-1 FAIL),
     - lifetime IRMAA: per-year IRMAA, deflated and summed the same way.
  4. Candidates are independent — run them concurrently (`engine.Run` is a
     pure function of its input; the tax-optimizer fan-out is precedent).
  5. Render partial `whatif-conversion-sweep-results`.
- Template `web/templates/components/whatif/conversion-sweep.html`, mirroring
  `tax-optimizer.html`: a card with a run button (`hx-post`, panel target,
  loading indicator), results table, dark-mode classes, and the row matching
  the saved plan's current amount visually marked "current". Include the card
  on the what-if page next to the Tax Optimizer card.
- The panel must not run on page load — button-triggered only.

## Acceptance criteria
1. `make build` and `make test` pass.
2. `curl -s -X POST localhost:8080/whatif/conversion-sweep` returns 200 with
   one row per candidate amount, ordered ascending; total wall time < 2 s.
3. The row for the saved plan's current conversion amount is marked current;
   with the saved plan at $50k there are exactly 9 rows.
4. A projection that survives the horizon renders "survives" + ending real
   balance, never a fabricated depletion month.
5. Handler tests cover: row count/order, current-row marking, the
   survives-vs-depletes rendering split. A render test covers the partial
   (repo has `render_*_test.go` precedent in `internal/templates/`).
6. No changes under `internal/services/retirement/engine/**` or other
   critical globs; no `RunFull`, no Monte Carlo calls.
7. Conforms to ACCESSIBILITY.md (table headers, contrast, focus states —
   read it before writing the template).

## Tier
2 (code) — checker-tests (anthropic lane) + checker-second (adversarial).

# T16 — Make the sweep actionable

## Behavior
- Highlight two rows in the sweep results (distinctly and accessibly, not
  color-only): "least lifetime tax" and "longest-lasting portfolio" (ties:
  prefer the smaller amount). When one row wins both, one combined marker.
- Each non-current row gets an "Apply" button that saves that annual
  conversion amount to the active plan through the SAME server path the
  existing Roth conversion form uses (`POST /whatif/roth-conversion` — reuse
  its handler semantics; preserve saved start/end years; enabled = amount>0),
  then refreshes the results panel so the "current" marker moves.
- Applying is a write to the saved plan: the button must state the amount it
  applies (visible text, not icon-only) and the swap must re-render the sweep
  so the change is immediately visible. No confirmation dialog needed — the
  existing form has none and the change is one field, reversible by clicking
  another row.

## Acceptance criteria
1. `make build`, `make test`, `make css-verify` all pass.
2. Tests: marker logic (least-tax row, longest-lasting row, tie handling,
   combined marker), and an httptest flow proving Apply persists the amount
   via the real router and the re-rendered table marks the new current row.
3. Markers are not color-only (ACCESSIBILITY.md #8); Apply buttons have
   accessible names including the dollar amount.
4. No RunFull / Monte Carlo; no critical-glob changes.

## Tier
2 (code) — checker-tests + checker-second.
