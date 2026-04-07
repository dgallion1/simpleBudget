# SS Combined Household Optimizer

## Problem

The SS Claiming Age Optimizer analyzes each spouse independently, finding the best claiming age for each in isolation. This misses the key interaction: the lower-earning spouse may receive a spousal top-up to 50% of the higher earner's PIA, which depends on both spouses' claiming decisions. The optimal household strategy requires evaluating all combinations together.

## Design Decisions

- **Approach B (separate function):** Combined analysis is a new `SSCombinedAnalysis` function, keeping individual analysis untouched.
- **Spousal top-up only (no survivor benefits):** Models the 50% spousal benefit top-up. Survivor benefits can be added later.
- **Top-N list (not full grid):** Show top 10 combinations ranked by combined cumulative at 85, plus a recommendation callout. Individual tables remain as-is.
- **Primary's age milestones:** Combined cumulative anchored to primary's age 80/85/90. Spouse's collection years offset by the age difference.
- **Spouse FRA selector:** Add a FRA dropdown (66/67) for spouse in the config form.

## Data Model

### New struct in `internal/models/whatif.go`

```go
type SSCombinedOption struct {
    YourClaimAge    int     `json:"your_claim_age"`
    SpouseClaimAge  int     `json:"spouse_claim_age"`
    YourMonthly     float64 `json:"your_monthly"`
    SpouseMonthly   float64 `json:"spouse_monthly"`
    CombinedMonthly float64 `json:"combined_monthly"`
    CombinedAt80    float64 `json:"combined_at_80"`
    CombinedAt85    float64 `json:"combined_at_85"`
    CombinedAt90    float64 `json:"combined_at_90"`
}
```

### Added to `SSComparisonAnalysis`

```go
CombinedOptions    []SSCombinedOption `json:"combined_options,omitempty"`
CombinedBestYou    int                `json:"combined_best_you,omitempty"`
CombinedBestSpouse int                `json:"combined_best_spouse,omitempty"`
```

## Spousal Top-Up Logic

New function in `social_security.go`:

```go
func SpousalTopUp(spouseOwnBenefit, higherPIA float64, spouseFRA, spouseClaimAge int) float64
```

1. Calculate 50% of the higher earner's PIA.
2. If spouse's own benefit >= that amount, no top-up -- return spouse's own benefit.
3. If spouse claims at or after their FRA: top-up to full 50% of higher PIA.
4. If spouse claims before FRA: reduce the 50% using the same early-claiming reduction formula (months before FRA x reduction factor).
5. Return the higher of: spouse's own benefit, or the reduced spousal benefit.

The higher earner is determined by comparing PIAs. If PIAs are equal, no top-up applies. Only the lower earner can receive the top-up.

## Combined Analysis Function

New function in `social_security.go`:

```go
func SSCombinedAnalysis(
    yourPIA float64, yourFRA, yourAge int,
    spousePIA float64, spouseFRA, spouseAge int,
    colaRate float64,
) ([]SSCombinedOption, int, int)
```

1. Determine who is the higher earner by comparing PIAs.
2. Iterate all valid combinations: your claiming ages (max of 62, yourAge) through 70 x spouse claiming ages (max of 62, spouseAge) through 70.
3. For each combination:
   - Calculate each person's adjusted benefit via `AdjustedSSBenefit`.
   - Apply `SpousalTopUp` to the lower earner.
   - Compute combined cumulative at primary's age 80/85/90. Spouse's collection years are offset by the age difference. COLA applied independently from each person's claiming age.
4. Rank all combinations by CombinedAt85 descending.
5. Return top 10, plus the best combination's claiming ages.

## UI Changes

### Config Form (`social-security.html`)

- Add a FRA dropdown (66/67) for spouse inside the existing `{{if .Settings.HasSpouse}}` block, matching the primary FRA dropdown style.

### Results (`social-security.html`)

- Keep both individual tables exactly as-is.
- Add a "Combined Household Strategy" section below containing:
  - Recommendation callout: "Optimal strategy: You claim at X, spouse claims at Y -- combined $Z/mo"
  - Table of top 10 combinations: Your Age | Spouse Age | Your Monthly | Spouse Monthly | Combined Monthly | By 80 | By 85 | By 90
  - Best row highlighted green.
  - Footer note explaining spousal top-up when applicable.

### Handler (`handlers.go`)

- Parse `spouse_fra` form field and store in `settings.SocialSecurity.SpouseFRA`.

## Testing

### `social_security_test.go`

- `TestSpousalTopUp`:
  - Spouse benefit already exceeds 50% of higher PIA (no top-up)
  - Spouse claims at FRA, benefit below 50% (full top-up)
  - Spouse claims early, benefit below 50% (reduced top-up)
  - Equal PIAs (no top-up)

- `TestSSCombinedAnalysis`:
  - Both same age, different PIAs -- verify top-up applied to lower earner
  - Different ages -- verify cumulative offsets correctly
  - Both age 70 -- only one combination returned
  - Results sorted by CombinedAt85 descending
  - Top 10 limit respected

### `handlers_test.go`

- Verify `spouse_fra` form field is parsed and stored.
