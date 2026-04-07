# SS Optimizer Projection Integration

## Problem

Social Security income currently enters the projection only through manually-added income sources (e.g., "Darrell Gallion SSI - $4000/mo"). The SS Claiming Age Optimizer is a standalone analysis tool that compares claiming ages 62-70 but does not feed into the projection. This creates two problems:

1. Manual amounts may not reflect the actual SSA-adjusted benefit for a given claiming age
2. Users must manually keep income sources in sync with optimizer insights

## Solution

Wire the SS optimizer config directly into the projection loop so it automatically generates correct SS income based on PIA, chosen claiming age, FRA, and COLA. Manual SS income sources are excluded when the optimizer is active.

## Model Changes

### SocialSecurityConfig (internal/models/whatif.go)

Add two fields:

```go
ClaimAge       int `json:"claim_age,omitempty"`        // Primary claiming age (62-70)
SpouseClaimAge int `json:"spouse_claim_age,omitempty"` // Spouse claiming age (62-70)
```

## Projection Integration

### calculateMonthlyIncomeBreakdown (internal/services/retirement/calculator.go)

When `SocialSecurity` is configured with `ClaimAge > 0`:

1. Skip any income sources matching `isSocialSecurityIncomeSource()` to prevent double-counting
2. Calculate SS income for the current month:
   - **Primary**: Use `AdjustedSSBenefit(PIA, FRA, ClaimAge)` for the base monthly amount
   - **Spouse**: Use the higher of own PIA or 50% of primary PIA (spousal benefit). Cap effective claiming age at spouse's FRA (no delayed retirement credits for spousal benefits).
   - Apply COLA from the claiming month onward: `baseBenefit * (1 + COLARate)^(yearsFromClaim)`
3. Add to `SocialSecurityIncome` in the breakdown

### Start Month Calculation

- Primary: `max(0, (ClaimAge - CurrentAge) * 12)`
- Spouse: `max(0, (SpouseClaimAge - SpouseAge) * 12)`

If the person's current age exceeds their claiming age, SS income starts at month 0 (already collecting).

## Handler Changes

### handleWhatIfSocialSecurity (internal/handlers/whatif/handlers.go)

Parse two new form fields: `claim_age` and `spouse_claim_age` (integers 62-70). Save to `SocialSecurityConfig`.

## UI Changes

### Optimizer Form (web/templates/components/whatif/social-security.html)

1. Add "Claim Age" dropdown (62-70, plus empty/unset option) below the FRA field for primary
2. Add "Spouse Claim Age" dropdown (62-70, plus empty/unset option) below the spouse FRA field
3. Below each claim age dropdown, show a summary line: "Projected benefit: $X,XXX/mo at age Y" (computed client-side or server-side)
4. When optimizer is active and manual SS income sources exist, show a note: "SS income is now calculated from the optimizer. Manual SS income sources are excluded from the projection."

### Summary Line Calculation

- Primary: `AdjustedSSBenefit(PIA, FRA, ClaimAge)` formatted as currency
- Spouse: Use the higher of own PIA or 50% of primary PIA, then apply `AdjustedSSBenefit(effectivePIA, spouseFRA, min(SpouseClaimAge, spouseFRA))` formatted as currency

## What Doesn't Change

- The comparison table still shows all ages 62-70 with cumulative breakdowns
- The best-age star and early-claim advisory note remain
- The optimizer works as a standalone analysis tool in addition to driving the projection
- `isSocialSecurityIncomeSource()` name-matching logic is unchanged

## Testing

- Unit test: `calculateMonthlyIncomeBreakdown` returns correct SS income for various claim ages, including spouse with spousal benefit
- Unit test: manual SS income sources are excluded when optimizer is active
- Unit test: start month calculation with current age > claim age (already collecting)
- Unit test: COLA applied correctly from claim month
- Handler test: claim_age and spouse_claim_age parsed and saved
- Existing SS optimizer tests continue to pass
