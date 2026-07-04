# Gross Withdrawal Edge-Case Test Coverage (Follow-On)

**Date:** 2026-05-13
**Status:** Closed 2026-07-04 — all four edge cases covered by sub-tests in
`TestCalculateBudgetFit` (`internal/services/retirement/calculator_expense_test.go`,
five sub-tests: NIIT and SS phase-in each got one); IRMAA resolved via the
documentation option (panel footnote in
`web/templates/components/whatif/budget-analysis.html` now states that the
gross-up excludes the year-N+2 IRMAA effect of the withdrawal, and a
regression sub-test pins the lookback-MAGI semantics)
**Parent feature:** `docs/superpowers/specs/2026-05-12-gross-withdrawal-line-design.md`
**Merged on:** 2026-05-13 (`feat/gross-withdrawal-line` → `master`)

## Context

The gross-withdrawal line shipped with proof-of-correctness for the happy path
(zero/surplus gating, Roth equality, tax-deferred gross-up via marginal-rate
simulation, taxable current/steady-state behavior, template rendering) but
does not deeply exercise scenarios where the marginal rate changes nonlinearly
across tax/Medicare thresholds. The implementation handles these cases
correctly (the `estimateTaxSnapshot` simulation diff captures whatever the tax
engine produces), but the tests do not assert that behavior. This document
tracks the gaps for future test work.

## Edge Cases Not Yet Tested

### 1. Federal bracket crossing

A gap large enough to push existing income from one bracket into the next
(e.g., 22% → 24%). The expected `MarginalRateTaxDeferred` should be a
**blended** rate that reflects how much of the additional withdrawal sat in
each bracket. Current tests use scenarios that sit safely inside a single
bracket.

Test idea: pension + RMD income that lands at the top of the 22% bracket;
gap that pushes well into 24%; assert `MarginalRateTaxDeferred` ∈ (22, 24).

### 2. IRMAA cliff within the same simulation

IRMAA tiers add ~$70/mo Medicare surcharge per Medicare-eligible adult when
MAGI crosses a tier. The current implementation passes the **baseline**
lookback MAGI to both the baseline and simulated `estimateTaxSnapshot` calls,
which means cliff detection within the same simulation pair will not fire.

This is structurally correct for the way IRMAA works (IRMAA uses 2-year
lookback MAGI, so this year's withdrawal cannot affect this year's IRMAA),
but the eventual cliff that the withdrawal causes for year N+2 is invisible
to the gross-up calculation.

Resolution options:
- **Documentation only:** Note in the panel footnote that gross-up does not
  include future IRMAA cliff effects from the withdrawal itself.
- **Lookback projection:** When computing the simulated snapshot, derive a
  hypothetical year N+2 MAGI assuming the withdrawal continues. Significant
  complexity.

Recommended: documentation update first; only do the projection path if
users surface the issue.

### 3. NIIT and SS taxability phase-ins

- NIIT triggers at $200K MAGI (single) or $250K (MFJ); a withdrawal that
  pushes MAGI over this threshold introduces a 3.8% surcharge on investment
  income.
- Social Security taxability transitions at 50% → 85% as provisional income
  rises through ~$25K-$34K (single) or ~$32K-$44K (MFJ).

Both produce discontinuous marginal rate jumps that a single-point simulation
captures correctly but tests do not assert against.

Test ideas:
- Pension + SS income just under the NIIT threshold; gap large enough to
  cross; assert `MarginalRateTaxDeferred` is materially higher than the
  underlying ordinary rate.
- Modest pension income + SS with provisional income near the 50% / 85%
  threshold; assert simulated marginal rate reflects the additional taxable
  SS triggered by the withdrawal.

### 4. LTCG bracket transitions (steady-state taxable)

Long-term capital gains brackets are 0% / 15% / 20%. Crossing a boundary
during the simulation produces a blended LTCG rate. Steady-state taxable
gross-up uses a gain-fraction approximation plus a single `estimateTaxSnapshot`
diff; the resulting `SteadyStateEffectiveRateTaxable` is correct but
untested under bracket-crossing conditions.

Test idea: high-income retiree in steady-state where existing LTCG sits
at the top of the 15% bracket; gap pushes some of the simulated gain into
20%; assert effective rate is between 15 and 20.

## Acceptance Criteria for Closing This Follow-On

- At least one test scenario per edge case (4 new sub-tests in
  `TestCalculateBudgetFit`).
- For IRMAA: either a documentation update to the panel footnote OR a
  year-N+2 projection-based check (decision pending).
- All new tests use `t.Fatalf` for preconditions (no SKIPs).
- Spec updated to reflect the IRMAA-within-simulation limitation.
