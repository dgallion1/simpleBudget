# SS Optimizer Projection Integration

## Problem

Social Security income currently enters the projection only through manually-added income sources, such as "Darrell Gallion SSI - $4000/mo". The SS Claiming Age Optimizer is a standalone analysis tool that compares claiming ages 62-70 but does not feed into the projection. This creates three problems:

1. Manual amounts may not reflect the actual SSA-adjusted benefit for a given claiming age.
2. Users must manually keep income sources in sync with optimizer insights.
3. Manual SS sources can conflict with optimizer results once the optimizer becomes the intended source of truth.

## Solution

Store the selected claiming ages in the SS optimizer config and have the projection loop synthesize SS income from that config. When optimizer-driven projection is active, manual SS income sources are ignored to prevent double counting, while all non-SS manual income sources continue to work as they do today.

This spec builds on the existing optimizer code in `internal/services/retirement/social_security.go`. If the combined household optimizer from `2026-04-07-ss-combined-optimizer-design.md` is implemented first, reuse its spousal top-up helper rather than creating a second spouse-benefit calculation.

## Activation Rules

Optimizer-driven projection is active when:

1. `settings.SocialSecurity != nil`.
2. `settings.SocialSecurity.FRABenefit > 0`.
3. `settings.SocialSecurity.ClaimAge` is between 62 and 70.

When active:

1. Skip manual income sources where `isSocialSecurityIncomeSource(source)` returns true.
2. Add primary SS income based on `ClaimAge`.
3. Add spouse SS income only when `settings.HasSpouse()` is true, `SpouseFRABenefit > 0`, and `SpouseClaimAge` is between 62 and 70.

When inactive, keep existing behavior exactly as-is: classify manual SS sources as `SocialSecurityIncome` for tax treatment, and classify other manual sources as `OrdinaryIncome`.

## Model Changes

### `SocialSecurityConfig` (`internal/models/whatif.go`)

Add selected claiming ages:

```go
type SocialSecurityConfig struct {
    FRABenefit       float64 `json:"fra_benefit"`                    // Monthly PIA (benefit at FRA)
    FRA              int     `json:"fra"`                            // Full retirement age (default 67)
    COLARate         float64 `json:"cola_rate"`                      // Annual COLA as decimal (default 0.02)
    SpouseFRABenefit float64 `json:"spouse_fra_benefit,omitempty"`   // Spouse PIA if applicable
    SpouseFRA        int     `json:"spouse_fra,omitempty"`           // Spouse FRA
    ClaimAge         int     `json:"claim_age,omitempty"`            // Primary claiming age, 62-70; 0 means unset
    SpouseClaimAge   int     `json:"spouse_claim_age,omitempty"`     // Spouse claiming age, 62-70; 0 means unset
}
```

Keep `0` as the unset value so existing saved scenarios remain compatible and so users can keep the optimizer as analysis-only until they explicitly choose a claiming age.

## Projection Integration

### `calculateMonthlyIncomeBreakdown` (`internal/services/retirement/calculator.go`)

Update the function to:

1. Detect optimizer activation once at the top of the function.
2. Iterate existing manual income sources as today, except skip `isSocialSecurityIncomeSource(source)` when optimizer projection is active.
3. Add synthesized primary and spouse SS income to `breakdown.SocialSecurityIncome`.
4. Recompute `breakdown.TotalIncome` as ordinary plus SS income.

Expected shape:

```go
func calculateMonthlyIncomeBreakdown(s *models.WhatIfSettings, month int) monthlyIncomeBreakdown {
    breakdown := monthlyIncomeBreakdown{}
    useOptimizerSS := socialSecurityProjectionActive(s)

    for _, source := range s.IncomeSources {
        amount := source.GetAdjustedAmount(month)
        if amount <= 0 {
            continue
        }
        if isSocialSecurityIncomeSource(source) {
            if !useOptimizerSS {
                breakdown.SocialSecurityIncome += amount
            }
            continue
        }
        breakdown.OrdinaryIncome += amount
    }

    if useOptimizerSS {
        breakdown.SocialSecurityIncome += projectedSocialSecurityIncome(s, month)
    }

    breakdown.TotalIncome = breakdown.OrdinaryIncome + breakdown.SocialSecurityIncome
    return breakdown
}
```

### Helper Functions

Put the SS projection helpers in `internal/services/retirement/social_security.go` so they can share `AdjustedSSBenefit`, the capped spousal logic, and any future combined-household helper.

Recommended helpers:

```go
func socialSecurityProjectionActive(s *models.WhatIfSettings) bool
func projectedSocialSecurityIncome(s *models.WhatIfSettings, month int) float64
func projectedSSBenefitForMonth(baseMonthly float64, colaRate float64, monthsSinceClaim int) float64
func claimStartMonth(currentAge, claimAge int) int
```

`claimStartMonth` should use the app's current integer-age model:

```go
func claimStartMonth(currentAge, claimAge int) int {
    if claimAge <= currentAge {
        return 0
    }
    return (claimAge - currentAge) * 12
}
```

This is intentionally month-level within the current model but not birthday-month precise. The unified persons model stores birth months, so a later enhancement can make this exact without changing this spec's public config shape.

## Benefit Calculation

### Defaults

Normalize defaults before calculations:

1. Primary FRA defaults to 67 when unset.
2. Spouse FRA defaults to 67 when unset.
3. COLA defaults to `0.02` when unset.

### Primary Benefit

For the primary claimant:

1. If `month < claimStartMonth(CurrentAge, ClaimAge)`, benefit is `0`.
2. Base monthly benefit is `AdjustedSSBenefit(FRABenefit, FRA, ClaimAge)`.
3. COLA applies starting after the claim month.

Use monthly compounding against the annual COLA rate so projection months are not stepped only once per year:

```go
monthsSinceClaim := month - claimStartMonth(settings.CurrentAge, ss.ClaimAge)
benefit := baseMonthly * math.Pow(1+colaRate, float64(monthsSinceClaim)/12.0)
```

If the claimant is already past the configured claim age at projection start, the benefit starts at month 0. Do not back-apply historical COLA before the projection start; the configured PIA and claim age are the scenario inputs.

### Spouse Benefit

For the spouse:

1. If `settings.HasSpouse()` is false, return `0`.
2. If `SpouseFRABenefit <= 0` or `SpouseClaimAge` is unset, return `0`.
3. If `month < claimStartMonth(SpouseAge, SpouseClaimAge)`, return `0`.
4. Calculate the spouse's benefit using the same simplified spousal-benefit semantics as the optimizer.

Preferred implementation after the combined optimizer exists is to apply the spousal top-up to the lower-PIA person, not always to the spouse:

```go
primaryMonthly := AdjustedSSBenefit(ss.FRABenefit, fra, ss.ClaimAge)
spouseMonthly := AdjustedSSBenefit(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)

if ss.FRABenefit > ss.SpouseFRABenefit {
    spouseMonthly = SpousalTopUp(spouseMonthly, ss.FRABenefit, spouseFRA, ss.SpouseClaimAge)
} else if ss.SpouseFRABenefit > ss.FRABenefit {
    primaryMonthly = SpousalTopUp(primaryMonthly, ss.SpouseFRABenefit, fra, ss.ClaimAge)
}
```

If the combined optimizer helper does not exist yet, add one shared helper and update both projection and optimizer code to use it. Do not leave projection with a separate "higher of own PIA or 50% of primary PIA" calculation, because that diverges from the combined optimizer design and incorrectly assumes the primary person is always the higher earner.

For spousal benefit modeling, delayed retirement credits do not apply to the spousal component past the spouse FRA. Preserve that cap in the shared helper.

## Handler Changes

### `handleWhatIfSocialSecurity` (`internal/handlers/whatif/handlers.go`)

Parse and save two new optional form fields:

1. `claim_age`
2. `spouse_claim_age`

Validation:

1. Accept only integers 62-70.
2. Treat empty or invalid values as unset (`0`) rather than preserving a stale previous selection.
3. Continue defaulting `fra` and `cola_rate` as today.
4. Continue clearing `settings.SocialSecurity` when `FRABenefit <= 0`.

## UI Changes

### Optimizer Form (`web/templates/components/whatif/social-security.html`)

1. Add a "Claim Age" dropdown for the primary claimant below the primary FRA field.
2. Add a "Spouse Claim Age" dropdown below the spouse FRA field inside the spouse block.
3. Include an empty/unset option in both dropdowns so the optimizer can remain analysis-only.
4. Render options 62-70.
5. Show a summary line below each selected claim age: `Projected benefit: $X,XXX/mo at age Y`.
6. When optimizer projection is active and manual SS income sources exist, show a note: `SS income is calculated from the optimizer. Manual SS income sources are excluded from the projection.`

### Summary Line Calculation

The summary line can be server-side or client-side, but it must use the same calculation as projection:

1. Primary: `AdjustedSSBenefit(FRABenefit, FRA, ClaimAge)`.
2. Spouse: shared spousal top-up helper with spouse own adjusted benefit and primary PIA.
3. Do not show a summary line when the relevant claim age is unset.

## What Does Not Change

1. The comparison table still shows all valid claiming ages 62-70.
2. The best-age star and early-claim advisory note remain.
3. The optimizer continues to work as a standalone analysis tool when claim age is unset.
4. `isSocialSecurityIncomeSource()` name-matching logic is unchanged.
5. Manual non-SS income sources are unchanged.
6. Manual SS income sources are still used when optimizer projection is inactive.

## Edge Cases

1. Current age is greater than claim age: start synthesized SS income at month 0.
2. Current age is less than 62 and claim age is 62: start at `(62 - CurrentAge) * 12`.
3. Spouse age is unset despite `HasSpouse()` being true: use `settings.SpouseAge`; if it is `0`, skip spouse projection rather than treating spouse as age 0.
4. `COLARate == 0` means use the default 2%, matching existing optimizer behavior. A true 0% COLA is not representable until the config gains an explicit "was set" flag.
5. Manual SS income with non-SS income in the same scenario: skip only the SS-like manual source when optimizer projection is active.
6. Projection and optimizer should not create duplicate spouse benefit formulas.

## Testing

### Unit Tests

Add or update tests in `internal/services/retirement/calculator_test.go` and `internal/services/retirement/social_security_test.go`:

1. `calculateMonthlyIncomeBreakdown` includes synthesized primary SS at the claim month.
2. Manual SS income sources are excluded when optimizer projection is active.
3. Manual SS income sources are still included when claim age is unset.
4. Non-SS manual income remains ordinary income when optimizer projection is active.
5. Current age greater than claim age starts SS at month 0.
6. COLA applies from claim month using monthly exponent math.
7. Spouse SS is included only when spouse config and spouse claim age are present.
8. Spousal benefit uses the shared spousal top-up semantics and caps spousal delayed credits past FRA.

### Handler Tests

Add or update tests in `internal/handlers/whatif/handlers_test.go`:

1. Valid `claim_age` and `spouse_claim_age` are parsed and saved.
2. Empty claim age fields clear any previous claim-age selections.
3. Invalid claim ages outside 62-70 are treated as unset.
4. Existing spouse FRA parsing remains covered.

### Regression Tests

1. Existing SS optimizer tests continue to pass.
2. Existing projection tax tests still classify synthesized SS as `SocialSecurityIncome`, not `OrdinaryIncome`.
3. Existing manual-income projections are unchanged when the optimizer claim age is unset.
