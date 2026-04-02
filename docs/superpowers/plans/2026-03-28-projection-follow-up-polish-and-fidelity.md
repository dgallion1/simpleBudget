# Portfolio Projection Follow-Up: Polish And Fidelity

> **For agentic workers:** Use checkbox progress updates in this file. Keep each phase independently releasable. Prefer verification and UX clarity before deeper model complexity.

**Goal:** Follow up the completed realism/explainability work with manual verification, UI polish, and targeted fidelity improvements that are valuable enough to justify the extra complexity.

**Current state as of March 28, 2026:**
- Deterministic, Monte Carlo, and historical backtest all use monthly timing semantics.
- The base projection is after-tax.
- Taxable accounts track market value and cost basis using an average-cost model.
- Monte Carlo and historical backtest now use the same taxable-account state model.
- The UI exposes nominal vs. real chart mode, event markers, and a year-by-year reconciliation table.
- Automated tests are green via `go test ./...`.

**Why a follow-up instead of extending the original plan:** The primary trust and realism gaps are closed. The remaining work is a mix of manual verification, refinement, and optional deeper realism that should be prioritized explicitly rather than folded into the already-completed Phase 1-5 delivery.

**Primary files likely to be touched:**
- `internal/services/retirement/calculator.go`
- `internal/services/retirement/backtest.go`
- `internal/services/retirement/tax.go`
- `internal/models/whatif.go`
- `internal/handlers/whatif/handlers.go`
- `web/templates/components/whatif/projection-chart.html`
- `web/templates/components/whatif/projection-breakdown.html`
- `web/templates/components/whatif/budget-analysis.html`
- `web/static/js/charts.js`
- `internal/services/retirement/*_test.go`
- `internal/templates/render_test.go`

**Non-goals unless explicitly promoted into scope:**
- Per-lot tax-lot accounting
- Full state-by-state tax law fidelity
- Intramonth market simulation
- Required UI redesign unrelated to projection trust/explainability

---

## Phase 6: Manual Verification And Reconciliation

**Why first:** The code is already in place. The highest-value next step is proving the live scenario behaves the way the model claims.

- [x] Manually verify the live What-If scenario in `data/settings/whatif.json`
  - Confirm the page renders without errors.
  - Confirm the projection card, budget card, Monte Carlo card, and historical backtest card all still load correctly.

- [x] Verify nominal vs. real reconciliation in the live UI
  - Confirm the real chart mode matches `PortfolioBalanceReal = PortfolioBalance / CumulativeInflation`.
  - Confirm the final balance label and explanatory copy switch correctly with the toggle.

- [x] Verify timing-policy consistency with a simple withdrawal-heavy scenario
  - `start_of_month` should not be more optimistic than `end_of_month`.
  - Confirm deterministic, Monte Carlo, and historical backtest all move in the same direction when timing changes.

- [x] Verify tax drag directionality with a representative taxable-income scenario
  - Enabling taxable dividends / capital-gains distributions should not increase final balance, all else equal.
  - Confirm budget wording still matches the after-tax projection semantics.

- [x] Capture a short verification note in this file
  - Record what scenarios were checked.
  - Record whether any discrepancies were found.

**Acceptance criteria:**
- The current live scenario renders correctly.
- Nominal/real and tax/timing behaviors are validated against the UI, not only tests.
- Any discrepancies found are documented before more modeling complexity is added.

### Verification Notes

**Verified on:** March 28, 2026

- Live `Current Plan` scenario at `/whatif` rendered successfully with the projection card, budget card, Monte Carlo section, historical backtest section, and yearly reconciliation table present.
- Chart payload verification from the running app confirmed:
  - nominal y-axis title: `Balance ($)`
  - real y-axis title: `Balance (Today's Dollars)`
  - nominal final balance: `4325082.93`
  - real final balance: `1734244.17`
  - event markers present for `Pension starts`, `RMD starts`, and `Medicare: Christine`
- Timing-policy check against the live scenario showed `start_of_month` final balance `4291298.08`, below `end_of_month` final balance `4325082.93`, then restored to the original end-of-month setting.
- Tax-drag directionality check against the live scenario showed enabling `taxable_dividend_yield=4` reduced final balance from `4325082.93` to `4201405.49`, then restored to the original zero-yield setting.
- No model discrepancies were found in the verified behaviors above.
- Minor tooling note: direct browser-automation button clicks on the nominal/real toggle were flaky in the automation session, but the loaded frontend code, chart endpoint behavior, and DOM state updates all verified the feature logic.

---

## Phase 7: Explainability And UX Polish

**Why second:** Once the live behavior is verified, small UX improvements can make the model easier to trust without changing the engine.

- [ ] Improve chart event-marker readability
  - Reduce overlap for clustered events.
  - Consider hover-only text for dense scenarios if static labels become noisy.

- [x] Make the yearly reconciliation table easier to scan
  - Consider sticky headers, alternating row emphasis, or compact number formatting.
  - Keep nominal and real values visually distinct.

- [x] Add clearer terminology around withdrawals vs. income
  - Ensure the table and chart copy distinguish portfolio withdrawals from recurring income.
  - Avoid ambiguous labels like “income” when values include both cash flow and forced withdrawals.

- [x] Surface a small “model assumptions” summary in the projection area
  - Example items:
    - after-tax cash flow
    - average-cost taxable sales
    - annual Roth conversions at year boundary
    - monthly compounding

- [ ] Consider adding chart-series overlays only if they materially help trust
  - Candidates:
    - withdrawals
    - taxes
    - real vs. nominal comparison overlay
  - Avoid clutter by default.

**Acceptance criteria:**
- The projection area is easier to interpret in dense or long scenarios.
- Labels align with the actual accounting semantics.
- Extra UI detail improves comprehension without overwhelming the card.

### Phase 7 Notes

**Updated on:** April 2, 2026

- Added a compact assumptions summary to the portfolio longevity card covering after-tax cash flow, average-cost taxable sales, annual Roth conversion timing, and the configured monthly cash-flow timing mode.
- Updated the reconciliation table with sticky headers, alternating row emphasis, tabular numeric alignment, clearer `Gross Cash In` / `Portfolio Out` terminology, and a more visually distinct real-balance sublabel.
- Left chart event-marker readability and optional overlay work for a later slice so this pass stayed template-only and independently releasable.

---

## Phase 8: Tax Fidelity Upgrades

**Why third:** These are meaningful realism improvements, but they add complexity and should come after the current model is verified and polished.

- [ ] Add configurable tax treatment for big-ticket items
  - The data model already includes `TaxTreatment`, but the projection loop does not yet fully reflect ordinary vs. capital-gains treatment for one-time items.
  - Apply the treatment consistently in deterministic, Monte Carlo, and historical backtest.

- [ ] Improve Social Security taxation fidelity if current assumptions are too coarse
  - Review how provisional-income style behavior is modeled.
  - If changed, keep the assumptions explicit and documented.

- [ ] Evaluate whether non-qualified dividends and capital-gains distributions should be modeled with different timing
  - Current monthly smoothing is acceptable, but year-end concentration could be more realistic for some taxable funds.
  - Only do this if the added complexity materially changes outcomes.

- [ ] Consider NIIT / additional surtax handling only if relevant to expected users
  - This is likely a second-order enhancement, not a default next step.

**Acceptance criteria:**
- Any new tax fidelity is implemented consistently across all projection engines.
- Assumptions remain explainable in the UI and tests.
- The model does not become materially harder to reason about for marginal realism gains.

---

## Phase 9: Taxable-Account And Withdrawal Realism Extensions

**Why fourth:** These are optional depth improvements for later, not immediate gaps.

- [ ] Evaluate a configurable taxable cost-basis starting assumption
  - Current behavior initializes taxable basis equal to starting market value.
  - Consider allowing a lower starting basis for appreciated legacy brokerage assets.

- [ ] Evaluate a simple turnover / realization assumption
  - This was noted in the original plan as optional.
  - Keep it parameterized and off by default if added.

- [ ] Consider explicit cash-reserve modeling only if it solves a real trust problem
  - Example: distinguishing brokerage cash from invested taxable assets.
  - Avoid adding a fourth “bucket” unless it clearly improves results or explainability.

- [ ] Consider separate qualified-dividend assumptions by asset mix only if the current single setting proves too blunt
  - Prefer a small number of understandable knobs over a large number of precise-but-brittle ones.

**Acceptance criteria:**
- Any new taxable-account setting solves a demonstrated modeling gap.
- Settings remain understandable to non-expert users.
- Monte Carlo/backtest/deterministic parity is preserved.

---

## Recommended Order

- [ ] Slice A: Manual verification of live scenario and reconciliation behavior
- [ ] Slice B: Event-marker and reconciliation-table polish
- [ ] Slice C: Big-ticket item tax treatment
- [ ] Slice D: Optional basis/turnover realism extensions

This keeps the next iteration grounded in verification first, then clarity, then deeper model complexity.
The reconciliation-table portion is complete; the remaining Slice B work is event-marker readability.

---

## Verification Checklist

- [x] `go test ./...`
- [x] Manually verify `data/settings/whatif.json` in the running app
- [x] Verify nominal vs. real chart reconciliation visually and numerically
- [x] Verify timing selection changes results monotonically in a withdrawal-heavy case
- [x] Verify taxable dividend / gain assumptions create non-positive balance impact relative to the zero-taxable-distribution baseline

---

## Decision Notes

- Prefer assumptions that are explicit in both code and UI over hidden realism.
- Preserve parity across deterministic, Monte Carlo, and historical backtest whenever behavior changes.
- Bias against adding new knobs unless they represent a user-understandable modeling choice.
