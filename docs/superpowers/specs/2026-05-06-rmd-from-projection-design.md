# RMD analysis driven by actual projection — design

**Date:** 2026-05-06
**Branch:** `feat/whatif-fixes`
**Audit finding:** F-072 (new — to be added)
**Severity:** MEDIUM (user-visible contradiction in core analysis page)

## Problem

`CalculateRMDAnalysis` (`internal/services/retirement/rmd.go:132-214`) computes
RMD projections in complete isolation from `RunProjection`. It seeds with
`s.PortfolioValue * s.TaxDeferredPercent / 100`, then loops year-by-year applying
investment growth and the RMD withdrawal — but nothing else. It never accounts
for living expenses, healthcare, taxes, or big-ticket spending that drains the
portfolio in the real projection.

Result: when a user's scenario depletes the portfolio in (for example) under two
years, the RMD card cheerfully renders the tax-deferred bucket compounding for
ten years and quotes RMDs against balances that do not exist in the user's plan.
This is a direct, user-visible contradiction with the rest of the What-If page.

The card also currently disclaims *"Scenario chain is not yet applied to this
panel."* That gap goes away once the panel reads from the chained projection.

## Goal

Drive the RMD panel and `RMDAnalysis` model entirely from the output of
`RunProjection`, so the panel reports what actually happens to the tax-deferred
bucket in the user's plan. Eliminate the shadow path. No idealized view.

## Non-goals

- Per-person tax-deferred buckets and per-person RMD start ages. The current
  model lumps both spouses into one bucket and uses the older person's age. That
  remains; out of scope.
- Changing the IRS Uniform Lifetime Table values or the SECURE 2.0 age-75
  transition. F-032 already addressed the latter.
- Any change to the main projection's RMD math. F-049 already corrected the
  reinvestment cost basis. The projection is treated here as authoritative.

## Architecture

### Before

```
RunFullAnalysis
├── projection := RunProjection()
└── rmd := CalculateRMDAnalysis()      // reads s.PortfolioValue independently
```

### After

```
RunFullAnalysis
├── projection := RunProjection()
└── rmd := BuildRMDAnalysis(projection) // reads projection.Months[]
```

`CalculateRMDAnalysis` is removed. Callers updated.

`BuildRMDAnalysis` is a method on `*Calculator` so it retains access to
`c.Settings` (filing status, ages, start year for SECURE 2.0 transition).

## Data sources

The main projection already carries everything the RMD panel needs, per
`internal/models/whatif.go:865-895`:

- `ProjectionMonth.TaxDeferredBalance` — end-of-month tax-deferred balance.
- `ProjectionMonth.RMDWithdrawal` — actual forced RMD withdrawn that month
  (already capped at remaining balance, already aware of bucket depletion).
- `ProjectionResult.DepletionMonth` — month index where total portfolio hits
  zero (nil if it survives).

### Sampling rule

For RMD-year `Y` (counted from projection start month 0):

- **Start-of-year `TaxDeferredBal`:**
  - If `Y == 0`: `projection.Months[0].TaxDeferredBalance` — the initial value.
  - Else: `projection.Months[12*Y - 1].TaxDeferredBalance` — end of prior year.
  - Bounds: if `12*Y - 1 >= len(Months)`, the projection ended before this
    RMD year; do not emit a row.
- **`RMDAmount` for the year:** sum of `Months[m].RMDWithdrawal` for
  `m` in `[12*Y, 12*Y + 12)`, clipped to `len(Months)`.
- **`RMDPercent`:** the IRS Uniform Lifetime factor (`100 / factor`). Kept as
  the table value, not the actual realized percent. Footer note explains.
- **`LifeExpFactor`:** the IRS table factor for `age` (existing
  `GetLifeExpectancyFactor`).

### Year iteration

Iterate `Y` from `0` to `min(s.ProjectionYears, len(Months)/12)`. Emit a row
when `age = olderAge + Y >= effectiveStartAge`. Cap the emitted slice at 20
rows (matches existing model-side behavior). The template renders only the
first 10 rows of the slice (matches existing template behavior at
`rmd.html:52`). `TotalRMDsOver10Yr` continues to be the sum of the first 10
emitted rows' `RMDAmount` values — i.e. the first 10 actual-RMD years from
the projection.

## Rendering rules

| Scenario | Emit | Banner / footer | KPI: 10-Yr Total |
|---|---|---|---|
| Depletion **before** first RMD year | empty `Projections` | banner: *"Portfolio depletes in year X (age Y) — no RMDs would apply in this scenario."* | `$0` |
| Depletion **during** RMD years | rows for years before the depletion year only | footer: *"Portfolio depletes in year X (age Y); subsequent RMD years not shown."* | sum of actual RMDs across emitted rows |
| No depletion | up to 10 rendered rows (table cap) | no banner; standard footer | sum of actual RMDs across first 10 RMD years |
| `TaxDeferredPercent == 0` | empty `Projections` | unchanged "no tax-deferred accounts" path | `$0` |

The KPI card always shows `TaxDeferredValue` as the configured initial
tax-deferred amount (`s.PortfolioValue * s.TaxDeferredPercent / 100`). This is
what the user configured and gives meaningful context even in the
depleted-before-RMD case. (Renaming this KPI is out of scope.)

The `"Scenario chain is not yet applied to this panel"` disclaimer in
`web/templates/components/whatif/rmd.html:9` is removed, since the projection
is chain-aware.

A new footer line is added under the table:

> *"RMD % is the IRS Uniform Lifetime factor; RMD amount is the projected
> withdrawal."*

This avoids confusion when an actual RMD is smaller than the IRS-table percent
implies (e.g., partial-year coverage, or balance reduced by other expenses
before the RMD was taken).

## Data model

`models.RMDAnalysis` (`internal/models/whatif.go`) — additions:

```go
type RMDAnalysis struct {
    // existing fields unchanged...
    StartsInYears     int                `json:"starts_in_years"`
    StartAge          int                `json:"start_age"`
    CurrentAge        int                `json:"current_age"`
    TaxDeferredValue  float64            `json:"tax_deferred_value"`
    Projections       []RMDProjection    `json:"projections"`
    TotalRMDsOver10Yr float64            `json:"total_rmds_over_10yr"`

    // new fields
    DepletionYear     *int               `json:"depletion_year,omitempty"`     // year index of portfolio depletion, nil if survives
    DepletionAge      *int               `json:"depletion_age,omitempty"`      // older-person age at depletion year
    DepletedBeforeRMD bool               `json:"depleted_before_rmd"`          // true when DepletionYear < first RMD year
}
```

`models.RMDProjection` — fields unchanged. Their **values** now come from the
projection rather than the standalone math. `TaxDeferredBal` is the
start-of-year sample as defined above.

## Template changes

`web/templates/components/whatif/rmd.html`:

1. Remove the scenario-chain disclaimer (line 9).
2. Add a new branch above the existing table for `.Analysis.RMD.DepletedBeforeRMD`:
   render the depleted-before-RMD banner, suppress the table.
3. Add a footer note for `.Analysis.RMD.DepletionYear` when set and
   `not .Analysis.RMD.DepletedBeforeRMD`: render the during-RMD-years footer.
4. Add the IRS-vs-actual percent footer line under the table (always, when
   table renders).
5. The empty-state branches for `TaxDeferredPercent <= 0` and
   `StartsInYears > ProjectionYears` remain.

No changes to handler signatures — `Analysis.RMD` is already on the page model.

## Tests

### Unit (new) — `internal/services/retirement/rmd_test.go`

Build a `*ProjectionResult` fixture inline (not via `RunProjection`) so the
RMD-from-projection logic is tested in isolation.

1. **`TestBuildRMDAnalysis_DepletionBeforeRMD`** — projection with depletion at
   month 24, `olderAge=60`, `effectiveStartAge=73`. Expect: empty `Projections`,
   `DepletedBeforeRMD == true`, `DepletionYear == 2`,
   `TotalRMDsOver10Yr == 0`.
2. **`TestBuildRMDAnalysis_DepletionDuringRMD`** — projection that depletes at
   month `12*15` (year 15), `olderAge=70`, start age 73. Expect: 2 rows
   (years 13 and 14), `DepletedBeforeRMD == false`,
   `DepletionYear == 15`, `TotalRMDsOver10Yr == sum of those 2 row amounts`.
3. **`TestBuildRMDAnalysis_FullTenYears_NoDepletion`** — projection 30 years,
   `olderAge=72`, start age 73, surviving. Expect: 10 rows, balances and
   amounts pulled from the fixture months exactly.
4. **`TestBuildRMDAnalysis_TaxDeferredPercentZero`** — empty projections,
   no banner, no error.
5. **`TestBuildRMDAnalysis_StartAge75_Secure20`** — start year 2034,
   `olderAge=72`. Expect zero rows (age never reaches 75 in fixture window
   when projection is short) OR rows starting at age 75 (when projection is
   long enough). Confirms F-032 still honored.
6. **`TestBuildRMDAnalysis_AlreadyAtRMDAge`** — `olderAge=75`, year 0 already
   at RMD age. Expect first row uses `Months[0].TaxDeferredBalance` for
   start-of-year balance.
7. **`TestBuildRMDAnalysis_RMDPercentIsTableValue`** — assert
   `RMDPercent == 100/factor` even when actual RMDAmount in the fixture is
   smaller than that percent of balance. Documents the table-vs-actual choice.

### Integration

8. Add or extend a calculator-level test: run `RunFullAnalysis` on a settings
   fixture that depletes early. Assert
   `analysis.RMD.DepletedBeforeRMD == true` and
   `len(analysis.RMD.Projections) == 0`. This is the regression test for the
   reported user bug.
9. Reuse one of the existing integration scenarios (the standard
   "balanced retirement" fixture in `calculator_test.go`) to assert
   `analysis.RMD.Projections[i].RMDAmount` equals the sum of
   `analysis.Projection.Months[12*y..12*(y+1)].RMDWithdrawal`. This is the
   structural invariant that defines correctness.

### Removed / migrated

- The current `CalculateRMDAnalysis` and any test that exercises its
  isolated-math behavior (`rmd_tax_test.go` will be the main one) are deleted
  or rewritten to drive from a projection fixture. Any test that asserts
  `currentBalance` compounding past the actual depletion is intentionally
  obsolete.
- `EffectiveRMDStartAge` and `parseStartYear` are kept (used by
  `BuildRMDAnalysis` for SECURE 2.0). `rmdGrowthFractions` is removed (no
  longer needed; the projection already applies F-035 timing internally).

## Migration / risk

- `CalculateRMDAnalysis` is a public method on `*Calculator`. Searching the
  repo confirms it has exactly one caller in production code
  (`calculator.go:3068`). Tests under `internal/services/retirement/` are the
  only other callers.
- Method renamed to `BuildRMDAnalysis(projection *models.ProjectionResult)`.
  Callers in tests updated. No external API change (handlers only consume
  `analysis.RMD`).
- New `RMDAnalysis` fields are additive; existing JSON consumers are
  unaffected (omitempty).
- Risk: a fixture-projection in tests can drift from real `RunProjection`
  behavior. Mitigated by integration tests #8 and #9 which use real
  projections.

## Files touched

- `internal/services/retirement/rmd.go` — replace
  `CalculateRMDAnalysis` with `BuildRMDAnalysis(projection)`; delete
  `rmdGrowthFractions`.
- `internal/services/retirement/calculator.go` — line 3068, pass projection.
- `internal/models/whatif.go` — add `DepletionYear`, `DepletionAge`,
  `DepletedBeforeRMD` to `RMDAnalysis`.
- `web/templates/components/whatif/rmd.html` — banners, footer note, remove
  scenario-chain disclaimer.
- `internal/services/retirement/rmd_test.go` (new) — eight unit tests above.
- `internal/services/retirement/rmd_tax_test.go` — migrate or delete cases
  exercising removed math.
- `internal/services/retirement/calculator_test.go` (or the appropriate
  existing file) — integration tests #8 and #9.
- `docs/whatif-math-audit-2026-05-05.md` — append F-072 in Findings table and
  Findings ledger; mark resolved by this PR.

## Out of scope (deferred)

- Per-person tax-deferred buckets / per-person RMD ages.
- Surfacing actual realized RMD percent (table% always shown; actual is
  derivable from `RMDAmount / TaxDeferredBal` if a future story needs it).
- Charting the tax-deferred trajectory directly inside the RMD card. The
  yearly table covers it adequately for the depletion contradiction.
