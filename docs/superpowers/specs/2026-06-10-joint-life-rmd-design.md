# Joint Life Table II for RMDs — Design

**Date:** 2026-06-10
**Status:** Approved (checkbox default ON per user)

## Problem

The projection engine computes every RMD with the IRS Uniform Lifetime Table
(`engine/rmd.go`). Under 26 CFR §1.401(a)(9)-9(d) and Pub 590-B, an account
owner whose **spouse is the sole beneficiary and is more than 10 years
younger** must use the Joint and Last Survivor Table (Table II), which gives a
larger factor and therefore a smaller required distribution. For the user's
household (owner born 1958-11, spouse born 1971-08 — a 13-year attained-age
gap) the age-73 factor is 28.6 vs the Uniform 26.5, so the app currently
overstates the legal minimum by ~7.3% and forces phantom taxable income
through every projection year.

## Legal rule

- Eligibility: spouse is sole beneficiary for the entire year AND is more than
  10 years younger, measured by ages attained on birthdays within the
  distribution calendar year. Equivalent test: `spouseBirthYear − ownerBirthYear ≥ 11`.
- Factor: Table II value at (owner attained age, spouse attained age).
- Otherwise: Uniform Lifetime Table (unchanged current behavior).
- The household "owner" remains the older member, consistent with the app's
  single tax-deferred pool and existing `RMDAgeForCalendarYear` semantics.

## Components

### 1. Table data — `engine/joint_life_table.go` (generated)

- Band of Table II needed for RMDs: owner ages 72–120 (the regulation's
  `120+` row stored as 120) × spouse ages 18 through `owner − 11`.
- Generated from the eCFR XML of §1.401(a)(9)-9 (fetched 2026-06-10; 14,640
  cells parsed; spot checks (73,60)=28.6, (80,65)=23.8, (72,50)=36.9,
  (120+,80)=11.2 match). Generator script committed as
  `scripts/gen-joint-life-table.py`; file header cites source and fetch date.
- Lookup `jointLifeFactor(ownerAge, spouseAge int) float64` with conservative
  clamps: owner > 120 → 120 row; spouse < 18 → 18 (smaller factor → larger
  RMD, never understates the requirement); spouse > owner−11 is never asked
  (gate handles it) but clamps to owner−11 defensively.

### 2. Setting — `WhatIfSettings.SpouseSoleBeneficiary *bool`

- JSON `spouse_sole_beneficiary`. Pointer-with-accessor pattern:
  `IsSpouseSoleBeneficiary() bool` returns true when nil, so legacy settings
  files (key absent) default ON.
- Form: hidden input `value="false"` paired before the checkbox
  `value="true"` in the same template section, so the key is always present
  when its form section posts and absent in other partial posts — matching the
  partial-form preservation semantics from commit 5006586. Parse in
  `form_spec.go` following the existing `applyRMDTiming` early-return-on-absent
  shape; truthy values are `"true"`/`"on"` per existing checkbox handlers.

### 3. Engine selection — `engine/rmd.go`

- `RMDLifeFactor(s *models.WhatIfSettings, calendarYear int) float64`:
  - Joint Table II when: `s.IsSpouseSoleBeneficiary()` AND a spouse person
    with parseable BirthMonth exists AND birth-year gap ≥ 11.
  - Else Uniform (`GetLifeExpectancyFactor`) — bitwise-identical current
    behavior.
  - Owner/spouse attained ages: `calendarYear − birthYear` (existing
    convention).
  - Spouse birth year falls back to ParseStartYear(s.StartDate) − SpouseAge
    when BirthMonth is absent, mirroring `olderBirthYear`'s fallback.
- `CalculateRMDForYear(s, balance float64, calendarYear int) (amount, percent
  float64)` wraps it. Existing `CalculateRMD(balance, age)` and
  `GetLifeExpectancyFactor(age)` remain for compatibility (nil-settings and
  legacy callers).

### 4. Call-site updates

| Site | Change |
|---|---|
| `engine/loop_helpers.go:170` | `CalculateRMD(bal, RMDAgeForCalendarYear(…))` → `CalculateRMDForYear(s, bal, calendarYear)` |
| `analysis/budget_fit.go` ×3 (current, steady-state, lookback) | same substitution |
| `analysis/rmd.go:103` (RMD schedule display) | `GetLifeExpectancyFactor(age)` → `RMDLifeFactor(s, calendarYear)`; requires settings in scope (already available) |
| Monte Carlo / backtest / orchestrator | inherit via engine loop — no direct edits |
| `analysis/tax_optimizer_strategies.go` ~4% heuristic | out of scope (admitted estimate) |

### 5. UI

- Checkbox "Spouse is sole IRA beneficiary" in
  `web/templates/components/whatif/rate-assumptions.html` adjacent to the RMD
  timing select, with a one-line caption explaining the >10-years-younger rule.
- RMD schedule table caption states the table in effect: "Joint Life Table II
  (spouse sole beneficiary, >10 yrs younger)" or "Uniform Lifetime Table".
  Template needs a flag — expose `UsesJointLifeTable bool` on the RMD result
  model populated by `analysis/rmd.go`.

## Error handling

- Nil settings / no spouse / unparseable birth months → Uniform table
  (current behavior; never panic).
- Factor lookup never returns 0 for owner ≥ 72 within clamps; ages < 72 keep
  returning 0 (no RMD) exactly as today.

## Testing (TDD)

1. `jointLifeFactor` spot checks hand-typed from the regulation (≥ 12 values
   across the band, including 120+ row and band edges) — independent of the
   generator.
2. `RMDLifeFactor` selection: gap 10 → Uniform; gap 11 → Joint; checkbox off →
   Uniform; no spouse → Uniform; nil settings → Uniform.
3. Clamps: owner 121+, spouse < 18.
4. Persistence: key absent in JSON → true; explicit false round-trips; form
   post with hidden-false only → false; form post with both → true; partial
   post without the section → preserved.
5. Render: checkbox present + caption notes correct table name.
6. End-to-end: user-shaped scenario (births 1958-11 / 1971-08, checkbox on)
   asserts the age-73 projection year divides by 28.6; checkbox off divides
   by 26.5.

## Expected user-visible effect

RMDs shrink ~7–8% across the projection (e.g. age-73 RMD on $1.81M:
$68,262 → $63,250), lowering forced taxable income, taxes, and the
steady-state gap in all RMD years; the what-if RMD table shows the larger
factors and names the table used.
