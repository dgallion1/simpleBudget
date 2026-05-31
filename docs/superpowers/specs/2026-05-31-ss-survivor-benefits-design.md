# Social Security Survivor Benefits — Claiming Comparison

**Date:** 2026-05-31
**Status:** Approved (design), reviewed 2026-05-31
**Scope:** What-if page → Social Security Claiming Comparison card only.

## Problem

The Social Security claiming comparison (`analysis.SSAnalysis`) and the
projection both ignore survivor benefits. When one spouse dies, U.S.
Social Security pays the survivor the **larger** of the two benefits and
the smaller one stops — so household SS drops from `A + B` to `max(A, B)`.

Because the higher earner's benefit becomes the survivor's benefit for
the rest of the last-survivor lifetime, **delaying the higher earner's
claim raises the floor the surviving spouse lives on**. The current model
shows none of this, so it understates the value of the higher earner
delaying.

## Decision (scope)

Two scoping decisions were made during brainstorming:

1. **Surface:** the claiming-comparison analysis only. No mortality is
   introduced into the projection (Budget Fit / Present Value / Monte
   Carlo are unchanged). This avoids an assumption-heavy, three-loop
   change.
2. **Manifestation:** *inform*, not *decide*. Show the survivor benefit
   each claim age locks in, plus a callout quantifying the gain from
   delaying to 70. The "best age" recommendation (cumulative-at-85) is
   **unchanged**. No mortality/horizon assumption is required.

## Out of scope (YAGNI)

- No mortality tables or probability-weighting.
- No changes to the projection, Budget Fit, Present Value, or Monte Carlo.
- No survivor-aware cumulative columns.
- No change to the recommended claim age.
- No new survivor-benefit modeling for single filers (survivor benefits
  require a spouse).

## Domain rules modeled

The **survivor record amount** a deceased worker's record produces, by
the age the deceased had claimed:

- Claimed **at or after FRA**: survivor inherits the actual benefit,
  including any delayed-retirement credits (no cap).
- Claimed **before FRA**: survivor benefit is the reduced benefit, but
  **floored at 82.5% of the deceased's PIA** (the RIB-LIM rule, 20 CFR
  §404.338). This floor is why the survivor column differs from the
  plain "monthly benefit" column at ages ~62–64.

In practice the surviving spouse receives the larger of their own
retirement benefit and the applicable survivor benefit. This design
focuses the visible comparison on the **higher-PIA worker's record**
(ties → primary), because that is the record whose delay most commonly
raises the survivor floor and is the clearest policy lever in the
existing claiming comparison. The lower-PIA worker's claim age does not
raise the higher-PIA survivor record amount, so no survivor column is
shown on the lower-PIA table.

This is a deterministic, current-dollars-at-claim figure (consistent with
the existing `MonthlyBenefit` column). COLA growth and lifetime
accumulation are intentionally not applied — the cumulative columns and
best-age logic are untouched.

Reviewed limitation: this is the deceased worker record amount before
any reduction tied to the surviving spouse's own survivor-claiming age.
The feature does not model death timing or whether the survivor claims a
widow(er) benefit before their own survivor FRA.

## Calculation (`internal/services/retirement/analysis/ss.go`)

New pure helper:

```go
// SurvivorBenefitForClaimAge returns the monthly Social Security survivor
// benefit a worker's record produces if claimed at claimAge, in
// current (claim-date) dollars. Per 20 CFR §404.338: a record claimed
// before FRA floors the survivor benefit at 82.5% of PIA (RIB-LIM);
// claimed at/after FRA the survivor inherits the full benefit including
// delayed-retirement credits.
func SurvivorBenefitForClaimAge(pia float64, fra, claimAge int) float64 {
    fra = NormalizedSSFRA(fra)
    adjusted := AdjustedSSBenefit(pia, fra, claimAge)
    if claimAge < fra {
        return math.Max(adjusted, 0.825*pia)
    }
    return adjusted
}
```

In `SSAnalysis`, after the existing primary/spouse option tables are
built:

- Determine the higher-PIA worker from the already-computed `primaryPIA`
  vs `spousePIA` (ties → primary). Only proceed when `s.HasSpouse()` is
  true and both PIAs are positive.
- Populate `SurvivorMonthlyBenefit` on each option of the **higher
  earner's** option slice via `SurvivorBenefitForClaimAge(higherPIA,
  higherFRA, option.ClaimAge)`.
- Populate the household survivor summary on `SSComparisonAnalysis`
  (below) only when the higher-PIA worker has a valid configured claim
  age. The callout anchors on that worker's **currently selected** claim
  age vs 70:
  - `SurvivorBenefitAtSelected = SurvivorBenefitForClaimAge(higherPIA, higherFRA, selectedClaimAge)`
  - `SurvivorBenefitAt70 = SurvivorBenefitForClaimAge(higherPIA, higherFRA, 70)`
  - `SurvivorDelayGainPct = (At70 - AtSelected) / AtSelected * 100`, clamped
    to ≥ 0; 0 when the higher earner already claims at 70.
  - `HasSurvivorDelayUpside = selected < 70 && not already claiming`.

`selectedClaimAge` is the higher earner's configured claim age
(`ss.ClaimAge` or `ss.SpouseClaimAge`). Treat it as usable only when
`ValidSSClaimAge(selectedClaimAge)` is true. When it is unset (`0`,
"Analysis only"), still populate the survivor column but suppress the
callout by leaving `HasSurvivorCallout=false`.

When that earner is already claiming
(`ValidSSClaimAge(selectedClaimAge) && selectedClaimAge <= currentAge`),
the benefit is locked: show `SurvivorBenefitAtSelected` (their actual)
and set `HasSurvivorDelayUpside=false`.

## Model additions (`internal/models/whatif.go`)

```go
// SSClaimingOption gains:
SurvivorMonthlyBenefit float64 `json:"survivor_monthly_benefit,omitempty"`

// SSComparisonAnalysis gains a survivor summary:
HasSurvivorAnalysis      bool    `json:"has_survivor_analysis,omitempty"`
HasSurvivorCallout       bool    `json:"has_survivor_callout,omitempty"`
SurvivorHigherEarnerIsSpouse bool `json:"survivor_higher_earner_is_spouse,omitempty"`
SurvivorSelectedClaimAge int     `json:"survivor_selected_claim_age,omitempty"`
SurvivorSelectedAgeLocked bool   `json:"survivor_selected_age_locked,omitempty"`
SurvivorBenefitAtSelected float64 `json:"survivor_benefit_at_selected,omitempty"`
SurvivorBenefitAt70       float64 `json:"survivor_benefit_at_70,omitempty"`
SurvivorDelayGainPct      float64 `json:"survivor_delay_gain_pct,omitempty"`
HasSurvivorDelayUpside    bool    `json:"has_survivor_delay_upside,omitempty"`
```

`SurvivorMonthlyBenefit` is set only on the higher earner's options, so a
template can show the column for the table that has non-zero values.

## UI (`web/templates/components/whatif/social-security.html`)

Rendered only when `HasSurvivorAnalysis` is true (household has a spouse
and two benefits):

- A **"Survivor benefit"** column on the higher earner's comparison
  table (primary or spouse table, whichever is the higher earner).
- A **callout** box near the comparison only when
  `HasSurvivorCallout` is true:
  - With upside: *"[You / Your spouse] are the higher earner — this
    benefit becomes the surviving spouse's for life. Delaying from age
    {selected} to 70 raises the survivor benefit from ${AtSelected} to
    ${At70}/mo (+{gain}%)."*
  - Already at 70 / already claiming: *"[You / Your spouse] are the
    higher earner; the surviving spouse's benefit is ${AtSelected}/mo."*

No form inputs are added.

Template gating:

- Primary table shows the survivor column when
  `HasSurvivorAnalysis && !SurvivorHigherEarnerIsSpouse`.
- Spouse table shows the survivor column when
  `HasSurvivorAnalysis && SurvivorHigherEarnerIsSpouse`.
- The callout uses `SurvivorSelectedClaimAge` instead of reading raw
  settings, so unset claim-age selections do not produce age `0` copy.

## Testing

Unit tests in `analysis` (and a render assertion in `handlers/whatif` if
that package already has SS render tests):

- `SurvivorBenefitForClaimAge`:
  - Early claim below the floor (e.g., 62, FRA 67) returns `0.825*pia`,
    not the deeper-reduced benefit.
  - Early claim above the floor returns the adjusted (reduced) benefit.
  - FRA claim returns PIA exactly.
  - Age 70 returns the DRC-increased benefit (> PIA).
- `SSAnalysis` survivor wiring:
  - Primary is higher earner → `SurvivorMonthlyBenefit` populated on
    `Options`, not `SpouseOptions`; `SurvivorHigherEarnerIsSpouse=false`.
  - Spouse is higher earner → populated on `SpouseOptions`;
    `SurvivorHigherEarnerIsSpouse=true`.
  - Single filer (no spouse) → `HasSurvivorAnalysis=false`, no fields set.
  - `SurvivorDelayGainPct` correct for a selected age < 70 with upside;
    `HasSurvivorDelayUpside=false` and gain=0 when already claiming or
    selected==70.
  - Unset selected claim age (`0`) still populates the higher-earner
    survivor column, but `HasSurvivorCallout=false` and
    `SurvivorSelectedClaimAge=0`.
- Regression: existing best-age / cumulative assertions unchanged
  (the feature must not move `BestAge`/`SpouseBestAge`).

Template/render tests:

- Primary higher-PIA case renders the survivor column in the primary
  table only.
- Spouse higher-PIA case renders the survivor column in the spouse table
  only.
- Analysis-only claim age does not render "age 0" survivor callout text.

## Source Checks

- SSA's CFR copy of
  [20 CFR §404.338](https://www.ssa.gov/OP_Home/cfr20/404/404-0338.htm)
  documents the early-claim survivor floor as the larger of the
  worker's reduced amount or 82.5% of PIA.
- SSA's OASDI program reference states that widow(er) benefits include
  delayed-retirement credits earned by the deceased worker:
  <https://www.ssa.gov/policy/docs/statcomps/supplement/2024/oasdi.html>.

## Files touched

- `internal/services/retirement/analysis/ss.go` — helper + wiring.
- `internal/models/whatif.go` — struct fields.
- `web/templates/components/whatif/social-security.html` — column + callout.
- Tests: `internal/services/retirement/analysis/ss_test.go`
  (+ a handler render test if applicable).

## Verification gate

`go build ./... && go vet ./... && go test ./... && staticcheck ./internal/services/retirement/...`
plus a visual check of the SS card via the run/verify flow.
