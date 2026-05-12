# Tax Optimizer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a synchronous Tax Optimizer that ranks (SS claim pair × Roth conversion strategy) combinations by real ending portfolio, surfacing top-5 alternatives with Monte Carlo refinement on the what-if page.

**Architecture:** Mirrors the SS Portfolio Optimizer pattern — eligibility gate → settings clone via `prepare.From` → `engine.Run` deterministic projections → `analysis.MonteCarlo` top-5 refinement. New file `analysis/tax_optimizer.go` (orchestration) + `analysis/tax_optimizer_strategies.go` (Roth strategy enumeration). One small additive engine field: `RothConversionConfig.PerYearOverrides` (projection-year-offset map; nil = pre-existing behavior).

**Tech Stack:** Go 1.x, standard library, internal `models` / `engine` / `analysis` / `prepare` packages, existing chi-router HTMX handler layer, Go templates.

**Spec:** `docs/superpowers/specs/2026-05-12-tax-optimizer-design.md`

---

## File Structure

**Create:**
- `internal/services/retirement/analysis/tax_optimizer.go` — orchestration, eligibility, scoring, MC refinement, settings cloning, top-K SS pair extraction
- `internal/services/retirement/analysis/tax_optimizer_strategies.go` — Roth strategy enumeration (ladder + bracket-fill) + `estimateOtherTaxableIncome` helper
- `internal/services/retirement/analysis/tax_optimizer_test.go` — unit tests for orchestration, eligibility, scoring, MC refinement
- `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` — unit tests for strategy enumeration and income estimation
- `web/templates/whatif/_tax_optimizer.html` — panel template (best card + top-5 table + ineligibility message)

**Modify:**
- `internal/models/whatif.go` — add `TaxOptimizerAnalysis`, `TaxOptimizerCandidate`, `RothOptimizerStrategy`, `RothStrategyKind` types; extend `RothConversionConfig` with `PerYearOverrides`; extend `WhatIfAnalysis` with `TaxOptimizer` field
- `internal/services/retirement/engine/loop_helpers.go` — `RothConversionAmountForYear` consults `PerYearOverrides` when non-nil
- `internal/services/retirement/engine/tax_test.go` — cover `PerYearOverrides` engine behavior + backwards-compat
- `internal/services/retirement/orchestrator.go` — attach `TaxOptimizer` result after `SSPortfolioWithSeed`
- `internal/handlers/whatif/handlers.go` — pass `TaxOptimizerAnalysis` to template context (likely already passed via `WhatIfAnalysis`; verify partial inclusion)
- `web/templates/whatif/whatif.html` (or the equivalent layout file the handler renders) — include `_tax_optimizer.html` partial below SS Portfolio section

---

## Task 1: Extend `RothConversionConfig` with `PerYearOverrides`

**Why first:** Every bracket-fill strategy depends on this field. Engine change is small and additive.

**Files:**
- Modify: `internal/models/whatif.go` (`RothConversionConfig` struct, ~line 1183)
- Modify: `internal/services/retirement/engine/loop_helpers.go` (`RothConversionAmountForYear`, ~line 126)
- Modify: `internal/services/retirement/engine/tax_test.go` (add backwards-compat + override tests)

- [ ] **Step 1: Write the failing test for per-year override**

Add to `internal/services/retirement/engine/tax_test.go`:

```go
func TestRothConversionAmountForYear_PerYearOverride(t *testing.T) {
    s := &models.WhatIfSettings{
        RothConversion: &models.RothConversionConfig{
            Enabled:      true,
            AnnualAmount: 50_000,
            StartYear:    0,
            EndYear:      10,
            PerYearOverrides: map[int]float64{
                2: 75_000,
                3: 90_000,
            },
        },
    }
    // Year 2 → override wins.
    if got := engine.RothConversionAmountForYear(s, 2, 1_000_000); got != 75_000 {
        t.Errorf("year 2 override: got %v, want 75000", got)
    }
    // Year 3 → override wins.
    if got := engine.RothConversionAmountForYear(s, 3, 1_000_000); got != 90_000 {
        t.Errorf("year 3 override: got %v, want 90000", got)
    }
    // Year 4 → no override, falls back to AnnualAmount.
    if got := engine.RothConversionAmountForYear(s, 4, 1_000_000); got != 50_000 {
        t.Errorf("year 4 fallback: got %v, want 50000", got)
    }
    // Override capped to availableTaxDeferred.
    if got := engine.RothConversionAmountForYear(s, 2, 60_000); got != 60_000 {
        t.Errorf("override capped to available: got %v, want 60000", got)
    }
}

func TestRothConversionAmountForYear_BackwardsCompat(t *testing.T) {
    // Nil PerYearOverrides → identical to previous behavior.
    s := &models.WhatIfSettings{
        RothConversion: &models.RothConversionConfig{
            Enabled:      true,
            AnnualAmount: 50_000,
            StartYear:    1,
            EndYear:      5,
        },
    }
    cases := []struct {
        year, want float64
    }{
        {0, 0},     // before StartYear
        {1, 50_000},
        {3, 50_000},
        {5, 50_000},
        {6, 0},     // after EndYear
    }
    for _, c := range cases {
        if got := engine.RothConversionAmountForYear(s, int(c.year), 1_000_000); got != c.want {
            t.Errorf("year %v: got %v, want %v", c.year, got, c.want)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/engine/ -run TestRothConversionAmountForYear_PerYearOverride -v`
Expected: FAIL — `PerYearOverrides` field doesn't exist yet.

- [ ] **Step 3: Add `PerYearOverrides` to `RothConversionConfig`**

In `internal/models/whatif.go` around line 1183-1189:

```go
// RothConversionConfig models annual Roth conversions
type RothConversionConfig struct {
    Enabled      bool    `json:"enabled"`
    AnnualAmount float64 `json:"annual_amount"` // Fixed amount to convert per year
    StartYear    int     `json:"start_year"`    // Year to begin conversions (0 = now)
    EndYear      int     `json:"end_year"`      // Year to stop conversions (0 = indefinite)

    // PerYearOverrides is keyed by projection-year offset (same key
    // semantics as StartYear/EndYear). When non-nil and a key is
    // present for the current year, the engine uses that override
    // amount instead of AnnualAmount. Used by the Tax Optimizer to
    // model variable-amount strategies (e.g. bracket-fill). Never
    // persisted on user-saved scenarios; constructed in-memory only.
    PerYearOverrides map[int]float64 `json:"per_year_overrides,omitempty"`
}
```

- [ ] **Step 4: Modify `RothConversionAmountForYear` to consult overrides**

In `internal/services/retirement/engine/loop_helpers.go` at ~line 126-137, replace the function body's final `return` with:

```go
func RothConversionAmountForYear(s *models.WhatIfSettings, currentYear int, availableTaxDeferred float64) float64 {
    if s.RothConversion == nil || !s.RothConversion.Enabled || availableTaxDeferred <= 0 {
        return 0
    }
    if currentYear < s.RothConversion.StartYear {
        return 0
    }
    if s.RothConversion.EndYear != 0 && currentYear > s.RothConversion.EndYear {
        return 0
    }
    amount := s.RothConversion.AnnualAmount
    if override, ok := s.RothConversion.PerYearOverrides[currentYear]; ok {
        amount = override
    }
    return math.Min(amount, availableTaxDeferred)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine/ -run TestRothConversionAmountForYear -v`
Expected: PASS, all subtests.

Run: `go test ./internal/services/retirement/engine/...`
Expected: PASS (no regressions; backwards-compat preserved).

- [ ] **Step 6: Commit**

```bash
git add internal/models/whatif.go internal/services/retirement/engine/loop_helpers.go internal/services/retirement/engine/tax_test.go
git commit -m "$(cat <<'EOF'
feat(engine): add RothConversionConfig.PerYearOverrides

Adds a per-year-offset override map to RothConversionConfig. When
non-nil, RothConversionAmountForYear prefers the override for the
current year; falls back to AnnualAmount otherwise. Nil map is
backwards-compatible with all existing scenarios.

Foundation for the Tax Optimizer's bracket-fill strategies, which
need per-year variable conversion amounts.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add Tax Optimizer model types

**Why next:** Every subsequent task references these types. Tests in later tasks can't compile without them.

**Files:**
- Modify: `internal/models/whatif.go` (add new types; extend `WhatIfAnalysis`)
- Test: `internal/models/models_extra_test.go` (or new `models_tax_optimizer_test.go`)

- [ ] **Step 1: Write the failing test for type defaults**

Create `internal/models/models_tax_optimizer_test.go`:

```go
package models

import (
    "testing"
)

func TestTaxOptimizerAnalysis_ZeroValue(t *testing.T) {
    var a TaxOptimizerAnalysis
    if a.Eligible {
        t.Error("zero-value Eligible should be false")
    }
    if a.CandidatesScored != 0 {
        t.Error("zero-value CandidatesScored should be 0")
    }
    if a.Top != nil {
        t.Error("zero-value Top should be nil slice")
    }
}

func TestRothStrategyKind_Constants(t *testing.T) {
    cases := map[RothStrategyKind]string{
        RothStrategyNone:        "none",
        RothStrategyLadder:      "ladder",
        RothStrategyBracketFill: "bracket_fill",
    }
    for k, want := range cases {
        if string(k) != want {
            t.Errorf("RothStrategyKind constant: got %q, want %q", string(k), want)
        }
    }
}

func TestWhatIfAnalysis_HasTaxOptimizerField(t *testing.T) {
    a := WhatIfAnalysis{TaxOptimizer: &TaxOptimizerAnalysis{Eligible: true}}
    if a.TaxOptimizer == nil || !a.TaxOptimizer.Eligible {
        t.Error("WhatIfAnalysis.TaxOptimizer should hold a pointer to TaxOptimizerAnalysis")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/models/ -run TestTaxOptimizerAnalysis -v`
Expected: FAIL — types don't exist yet.

- [ ] **Step 3: Add types to `internal/models/whatif.go`**

Append near the bottom of `internal/models/whatif.go` (after existing analysis types):

```go
// RothStrategyKind names a Roth conversion strategy family.
type RothStrategyKind string

const (
    RothStrategyNone        RothStrategyKind = "none"
    RothStrategyLadder      RothStrategyKind = "ladder"
    RothStrategyBracketFill RothStrategyKind = "bracket_fill"
)

// RothOptimizerStrategy describes a Roth conversion strategy in a form
// the Tax Optimizer can apply to the engine without mutating saved
// settings.
type RothOptimizerStrategy struct {
    Kind          RothStrategyKind `json:"kind"`
    AnnualAmount  float64          `json:"annual_amount,omitempty"`  // ladder only
    TargetBracket float64          `json:"target_bracket,omitempty"` // bracket_fill only; e.g. 0.22
    StartAge      int              `json:"start_age"`
    EndAge        int              `json:"end_age"`
    Label         string           `json:"label"` // human-readable, e.g. "$100k/yr to RMD age"
}

// TaxOptimizerCandidate is one (SS pair, Roth strategy) configuration
// and its scored outcome.
type TaxOptimizerCandidate struct {
    PrimaryClaimAge int                   `json:"primary_claim_age"`
    SpouseClaimAge  int                   `json:"spouse_claim_age,omitempty"`
    RothStrategy    RothOptimizerStrategy `json:"roth_strategy"`

    // Deterministic projection scores.
    EndingPortfolioReal float64 `json:"ending_portfolio_real"`
    LifetimeTaxReal     float64 `json:"lifetime_tax_real"`
    PeakMarginalBracket float64 `json:"peak_marginal_bracket"`
    TotalRothConverted  float64 `json:"total_roth_converted"`

    // Monte Carlo refinement; zero-valued for non-top-5 entries.
    MCSurvivalRate     float64 `json:"mc_survival_rate,omitempty"`
    MCMedianEndingReal float64 `json:"mc_median_ending_real,omitempty"`
}

// TaxOptimizerAnalysis is the per-scenario recommendation produced by
// analysis.TaxOptimizer. Always non-nil when produced via RunFull;
// Eligible=false carries IneligibleReason for UI rendering.
type TaxOptimizerAnalysis struct {
    Eligible         bool   `json:"eligible"`
    IneligibleReason string `json:"ineligible_reason,omitempty"`

    Baseline TaxOptimizerCandidate   `json:"baseline"`
    Best     TaxOptimizerCandidate   `json:"best"`
    Top      []TaxOptimizerCandidate `json:"top"`

    MonteCarloRuns   int `json:"monte_carlo_runs"`
    CandidatesScored int `json:"candidates_scored"`
}
```

- [ ] **Step 4: Add `TaxOptimizer` field to `WhatIfAnalysis`**

Locate the `WhatIfAnalysis` struct in `internal/models/whatif.go` and add a new field at the end (before the closing brace):

```go
    // TaxOptimizer holds the Tax Optimizer recommendation. May be nil
    // when no analysis has been run; carries Eligible=false with a
    // reason when the scenario doesn't qualify.
    TaxOptimizer *TaxOptimizerAnalysis `json:"tax_optimizer,omitempty"`
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/models/ -run "TestTaxOptimizerAnalysis|TestRothStrategyKind|TestWhatIfAnalysis_HasTaxOptimizer" -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS (additive type changes; no regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/models/whatif.go internal/models/models_tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(models): add Tax Optimizer types

Adds TaxOptimizerAnalysis, TaxOptimizerCandidate, RothOptimizerStrategy,
and RothStrategyKind. Extends WhatIfAnalysis with a TaxOptimizer field.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Ladder strategy enumeration

**Why next:** Self-contained, no external dependencies, exercises the new types. Bracket-fill builds on the same enumeration pattern in Task 4.

**Files:**
- Create: `internal/services/retirement/analysis/tax_optimizer_strategies.go`
- Create: `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`

- [ ] **Step 1: Write the failing test for ladder enumeration**

Create `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`:

```go
package analysis

import (
    "fmt"
    "testing"

    "budget2/internal/models"
)

func TestEnumerateLadderStrategies_DefaultShape(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:      67,
        ProjectionYears: 31,
        SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
    }
    strategies := enumerateLadderStrategies(s)

    // Expect 7 amounts × 5 windows = 35 candidates, minus dedups for
    // currentAge==65 cases (pre-IRMAA window has end<=start). With
    // currentAge==67, pre-IRMAA window (a → 65) is invalid → 7 × 4 = 28.
    if len(strategies) == 0 {
        t.Fatal("expected non-empty strategy slice")
    }

    // All have Kind=ladder.
    for _, st := range strategies {
        if st.Kind != models.RothStrategyLadder {
            t.Errorf("expected Kind=ladder, got %q", st.Kind)
        }
    }

    // All windows respect currentAge.
    for _, st := range strategies {
        if st.StartAge < s.CurrentAge {
            t.Errorf("window starts before currentAge: %+v", st)
        }
        if st.EndAge <= st.StartAge {
            t.Errorf("invalid window: %+v", st)
        }
    }

    // Each strategy has a non-empty Label.
    for _, st := range strategies {
        if st.Label == "" {
            t.Errorf("missing Label: %+v", st)
        }
    }
}

func TestEnumerateLadderStrategies_SkipsZeroAmountDups(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:      60,
        ProjectionYears: 35,
        SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
    }
    strategies := enumerateLadderStrategies(s)

    zeroCount := 0
    for _, st := range strategies {
        if st.AnnualAmount == 0 {
            zeroCount++
        }
    }
    // Only one $0 candidate (no-conversion baseline) — not one per window.
    if zeroCount != 1 {
        t.Errorf("expected exactly one $0 ladder candidate, got %d", zeroCount)
    }
}

func TestEnumerateLadderStrategies_LabelsAreStable(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:      67,
        ProjectionYears: 31,
        SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 70},
    }
    strategies := enumerateLadderStrategies(s)

    // Find the $100k/yr to RMD age candidate and assert its label.
    var found *models.RothOptimizerStrategy
    for i, st := range strategies {
        if st.AnnualAmount == 100_000 && st.EndAge == 73 {
            found = &strategies[i]
            break
        }
    }
    if found == nil {
        t.Fatal("expected to find $100k/yr 67→73 candidate")
    }
    want := "$100k/yr 67→73"
    if found.Label != want {
        t.Errorf("label: got %q, want %q", found.Label, want)
    }
}

// Helper used by later tests; defined here so Task 3 compiles cleanly.
func formatAmountLabel(amount float64) string {
    if amount == 0 {
        return "No conversion"
    }
    return fmt.Sprintf("$%dk", int(amount/1000))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run TestEnumerateLadderStrategies -v`
Expected: FAIL — function not defined.

- [ ] **Step 3: Implement ladder enumeration**

Create `internal/services/retirement/analysis/tax_optimizer_strategies.go`:

```go
package analysis

import (
    "fmt"

    "budget2/internal/models"
)

// Constants tuned per design spec. All in one block for easy adjustment.
var (
    taxOptimizerLadderAmounts      = []float64{0, 25_000, 50_000, 75_000, 100_000, 150_000, 200_000}
    taxOptimizerBracketFillTargets = []float64{0.12, 0.22, 0.24}
)

// strategyWindow names a (startAge, endAge) anchor used for both ladder
// and bracket-fill enumeration. Centralizes the four/five anchor points
// so labels stay consistent across families.
type strategyWindow struct {
    StartAge int
    EndAge   int
    Anchor   string // human-readable end-anchor: "5yr", "SS", "IRMAA", "RMD", "mid"
}

// strategyWindows returns the windows that are valid for s (i.e.
// EndAge > StartAge, given CurrentAge and the user's SS claim age).
// Order is stable for test assertions.
func strategyWindows(s *models.WhatIfSettings) []strategyWindow {
    a := s.CurrentAge
    var ssClaim int
    if s.SocialSecurity != nil {
        ssClaim = s.SocialSecurity.ClaimAge
    }
    candidates := []strategyWindow{
        {a, a + 5, "5yr"},
        {a, ssClaim, "SS"},
        {a, 65, "IRMAA"},
        {a, 73, "RMD"},
        {a + 5, a + 10, "mid"},
    }
    out := make([]strategyWindow, 0, len(candidates))
    for _, w := range candidates {
        if w.EndAge > w.StartAge {
            out = append(out, w)
        }
    }
    return out
}

// enumerateLadderStrategies returns the ladder family of Roth
// conversion strategies: cross-product of taxOptimizerLadderAmounts and
// strategyWindows(s), with all-but-one zero-amount duplicates removed.
func enumerateLadderStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
    windows := strategyWindows(s)
    out := make([]models.RothOptimizerStrategy, 0, len(windows)*len(taxOptimizerLadderAmounts))

    zeroEmitted := false
    for _, amount := range taxOptimizerLadderAmounts {
        for _, w := range windows {
            if amount == 0 {
                if zeroEmitted {
                    continue
                }
                zeroEmitted = true
            }
            out = append(out, models.RothOptimizerStrategy{
                Kind:         models.RothStrategyLadder,
                AnnualAmount: amount,
                StartAge:     w.StartAge,
                EndAge:       w.EndAge,
                Label:        formatLadderLabel(amount, w),
            })
            if amount == 0 {
                break // single "No conversion" candidate; window is irrelevant.
            }
        }
    }
    return out
}

func formatLadderLabel(amount float64, w strategyWindow) string {
    if amount == 0 {
        return "No conversion"
    }
    return fmt.Sprintf("$%dk/yr %d→%d", int(amount/1000), w.StartAge, w.EndAge)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run TestEnumerateLadderStrategies -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer_strategies_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): tax optimizer ladder strategy enumeration

Generates ladder-family Roth conversion strategies: cross-product of
seven dollar amounts × valid (currentAge, anchor) windows. Removes
zero-amount duplicates (one "No conversion" baseline suffices).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `estimateOtherTaxableIncome` helper for bracket-fill

**Why now:** Bracket-fill enumeration (Task 5) needs this estimator. Worth isolating so the closed-form estimate has its own test surface.

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go`
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`:

```go
func TestEstimateOtherTaxableIncome_PreSSAndPreRMD(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         60,
        ProjectionYears:    30,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit: 3_000,
            FRA:        67,
            ClaimAge:   67,
        },
    }
    // Year 0: age 60. No SS yet, no RMD yet. Expect 0 (no other income sources).
    got := estimateOtherTaxableIncome(s, 0)
    if got > 1.0 {
        t.Errorf("year 0 pre-SS pre-RMD expected ~0, got %v", got)
    }
}

func TestEstimateOtherTaxableIncome_PostSS(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:      60,
        ProjectionYears: 30,
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit: 3_000,
            FRA:        67,
            ClaimAge:   67,
            COLARate:   0.02,
        },
    }
    // Year 7: age 67, claim at 67. SS benefit ≈ 3000 * 12 = 36000 (taxable portion may be less,
    // but estimator includes the full gross for simplicity).
    got := estimateOtherTaxableIncome(s, 7)
    if got < 20_000 || got > 50_000 {
        t.Errorf("year 7 (post-SS) expected ~$36k, got %v", got)
    }
}

func TestEstimateOtherTaxableIncome_PostRMD(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         60,
        ProjectionYears:    30,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        InvestmentReturn:   6,
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit: 3_000, FRA: 67, ClaimAge: 67, COLARate: 0.02,
        },
    }
    // Year 13: age 73, first RMD year. Expect SS + meaningful RMD.
    got := estimateOtherTaxableIncome(s, 13)
    if got < 60_000 {
        t.Errorf("year 13 (post-RMD) expected SS+RMD ≥ $60k, got %v", got)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run TestEstimateOtherTaxableIncome -v`
Expected: FAIL — function not defined.

- [ ] **Step 3: Implement the estimator**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies.go`:

```go
// estimateOtherTaxableIncome returns a closed-form estimate of taxable
// income (excluding Roth conversion itself) at the given projection-year
// offset. Used by bracket-fill candidate pre-computation. Approximate
// by design — the optimizer scores on the engine's actual projection
// result, not on this estimate.
//
// Includes: SS benefit (gross, when claimed) + ordinary fixed-income
// sources from settings + estimated RMD when age ≥ 73 + a small
// taxable-account dividend estimate.
func estimateOtherTaxableIncome(s *models.WhatIfSettings, projectionYear int) float64 {
    if s == nil {
        return 0
    }
    age := s.CurrentAge + projectionYear
    total := 0.0

    // Social Security.
    if s.SocialSecurity != nil && s.SocialSecurity.ClaimAge > 0 && age >= s.SocialSecurity.ClaimAge {
        cola := s.SocialSecurity.COLARate
        if cola == 0 {
            cola = 0.02
        }
        yearsSinceClaim := age - s.SocialSecurity.ClaimAge
        monthly := s.SocialSecurity.FRABenefit
        // Approximate COLA escalation.
        for i := 0; i < yearsSinceClaim; i++ {
            monthly *= 1 + cola
        }
        total += monthly * 12
    }

    // Ordinary income sources (fixed-amount monthly). The engine has
    // richer logic; this estimator only needs to be directionally
    // correct, so include any "fixed" income source active for this
    // year.
    for _, src := range s.IncomeSources {
        if src.IncomeType != "fixed" {
            continue
        }
        startMonth := src.StartMonth
        endMonth := src.EndMonth
        currentMonth := projectionYear * 12
        if currentMonth < startMonth {
            continue
        }
        if endMonth != nil && currentMonth >= *endMonth {
            continue
        }
        total += src.Amount * 12
    }

    // RMD: rough estimate. After age 73 use a simple 4% of estimated
    // tax-deferred balance (corresponds roughly to the IRS Uniform
    // Lifetime divisor at age 73 = 26.5 → factor ≈ 3.77%).
    if age >= 73 {
        taxDeferredNow := s.PortfolioValue * (s.TaxDeferredPercent / 100.0)
        // Compound forward at investment return as a rough estimate.
        rate := s.InvestmentReturn / 100.0
        if rate <= 0 {
            rate = 0.06
        }
        balance := taxDeferredNow
        for i := 0; i < projectionYear; i++ {
            balance *= 1 + rate
        }
        total += balance * 0.04
    }

    // Taxable-account qualified dividends (small, but include for
    // bracket-fill realism).
    taxableNow := s.PortfolioValue * (1.0 - s.TaxDeferredPercent/100.0 - s.RothPercent/100.0)
    if s.TaxableDividendYield > 0 {
        total += taxableNow * (s.TaxableDividendYield / 100.0)
    }

    return total
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run TestEstimateOtherTaxableIncome -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer_strategies_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): estimate other taxable income for bracket-fill

Closed-form estimator: SS (gross with COLA) + fixed-income sources +
4%-of-projected-balance RMD when age ≥ 73 + taxable-account dividends.
Used by bracket-fill candidate pre-computation in the Tax Optimizer.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Bracket-fill enumeration + combined `enumerateRothStrategies`

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go`
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`:

```go
func TestEnumerateBracketFillStrategies_DefaultShape(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         67,
        ProjectionYears:    31,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67},
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
    }
    strategies := enumerateBracketFillStrategies(s)

    for _, st := range strategies {
        if st.Kind != models.RothStrategyBracketFill {
            t.Errorf("expected Kind=bracket_fill, got %q", st.Kind)
        }
        if st.TargetBracket < 0.10 || st.TargetBracket > 0.40 {
            t.Errorf("unexpected TargetBracket: %v", st.TargetBracket)
        }
        if st.Label == "" {
            t.Errorf("missing Label: %+v", st)
        }
    }
}

func TestEnumerateBracketFillStrategies_SkipsAllZeroCandidates(t *testing.T) {
    // Scenario where current age ≥ all window ends (no valid windows).
    s := &models.WhatIfSettings{
        CurrentAge:      80,
        ProjectionYears: 5,
        SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 70},
        TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingSingle},
    }
    strategies := enumerateBracketFillStrategies(s)
    if len(strategies) != 0 {
        t.Errorf("expected 0 bracket-fill strategies for age-80 scenario, got %d", len(strategies))
    }
}

func TestEnumerateRothStrategies_CombinesFamiliesAndBaseline(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         67,
        ProjectionYears:    31,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67},
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
    }
    all := enumerateRothStrategies(s)

    // Must include at least one ladder, one bracket-fill (when bracket
    // ceilings allow), and exactly one no-conversion baseline.
    var ladders, brackets, baselines int
    for _, st := range all {
        switch st.Kind {
        case models.RothStrategyLadder:
            if st.AnnualAmount == 0 {
                baselines++
            } else {
                ladders++
            }
        case models.RothStrategyBracketFill:
            brackets++
        }
    }
    if baselines != 1 {
        t.Errorf("expected exactly 1 baseline (no-conversion) candidate, got %d", baselines)
    }
    if ladders < 1 {
        t.Error("expected at least one non-zero ladder candidate")
    }
    if brackets < 1 {
        t.Error("expected at least one bracket-fill candidate")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run "TestEnumerateBracketFill|TestEnumerateRothStrategies" -v`
Expected: FAIL — functions not defined.

- [ ] **Step 3: Add bracket-fill ceilings table + enumerator**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies.go`:

```go
// bracketTopFor returns the top of the given target marginal bracket
// for the filing status. Returns (ceiling, ok). ok=false signals an
// unknown filing status — caller should skip the bracket-fill family.
//
// Values are 2024 IRS thresholds, in nominal dollars. Acceptable
// approximation for optimization (the optimizer ranks on engine
// output, which uses the engine's full tax tables; this table is only
// used to set per-year conversion targets).
func bracketTopFor(status models.FilingStatus, target float64) (float64, bool) {
    table := map[models.FilingStatus]map[float64]float64{
        models.FilingSingle: {
            0.12: 47_150,
            0.22: 100_525,
            0.24: 191_950,
        },
        models.FilingMarriedJoint: {
            0.12: 94_300,
            0.22: 201_050,
            0.24: 383_900,
        },
        models.FilingMarriedSeparate: {
            0.12: 47_150,
            0.22: 100_525,
            0.24: 191_950,
        },
        models.FilingHeadOfHousehold: {
            0.12: 63_100,
            0.22: 100_500,
            0.24: 191_950,
        },
    }
    rows, ok := table[status]
    if !ok {
        return 0, false
    }
    ceiling, ok := rows[target]
    return ceiling, ok
}

// enumerateBracketFillStrategies generates bracket-fill candidates
// (cross-product of target brackets × valid windows). For each, the
// PerYearOverrides map is pre-computed via estimateOtherTaxableIncome.
// Candidates that compute to zero conversion for every year in the
// window are skipped (they'd duplicate the no-conversion baseline).
func enumerateBracketFillStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
    if s.TaxConfig == nil {
        return nil
    }
    windows := strategyWindows(s)
    if len(windows) == 0 {
        return nil
    }

    out := make([]models.RothOptimizerStrategy, 0, len(windows)*len(taxOptimizerBracketFillTargets))
    for _, target := range taxOptimizerBracketFillTargets {
        ceiling, ok := bracketTopFor(s.TaxConfig.FilingStatus, target)
        if !ok {
            return nil // unknown filing status: skip entire family
        }
        for _, w := range windows {
            if !bracketFillProducesNonZero(s, w, ceiling) {
                continue
            }
            out = append(out, models.RothOptimizerStrategy{
                Kind:          models.RothStrategyBracketFill,
                TargetBracket: target,
                StartAge:      w.StartAge,
                EndAge:        w.EndAge,
                Label:         formatBracketFillLabel(target, w),
            })
        }
    }
    return out
}

func bracketFillProducesNonZero(s *models.WhatIfSettings, w strategyWindow, ceiling float64) bool {
    startProjYear := w.StartAge - s.CurrentAge
    endProjYear := w.EndAge - s.CurrentAge
    if startProjYear < 0 {
        startProjYear = 0
    }
    for y := startProjYear; y < endProjYear; y++ {
        other := estimateOtherTaxableIncome(s, y)
        if ceiling-other > 1 {
            return true
        }
    }
    return false
}

func formatBracketFillLabel(target float64, w strategyWindow) string {
    return fmt.Sprintf("Fill %.0f%% bracket, %d→%d", target*100, w.StartAge, w.EndAge)
}

// enumerateRothStrategies returns the full candidate set: ladder family
// + bracket-fill family. Order is stable for test reproducibility.
func enumerateRothStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
    out := enumerateLadderStrategies(s)
    out = append(out, enumerateBracketFillStrategies(s)...)
    return out
}

// rothStrategyToConfig produces the RothConversionConfig that, when
// substituted into settings, makes the engine apply the strategy.
// Ladder strategies translate to a fixed AnnualAmount across the
// window. Bracket-fill strategies translate to a PerYearOverrides map
// pre-computed via estimateOtherTaxableIncome.
func rothStrategyToConfig(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) *models.RothConversionConfig {
    if strat.Kind == models.RothStrategyNone || strat.AnnualAmount == 0 && strat.Kind == models.RothStrategyLadder {
        return &models.RothConversionConfig{Enabled: false}
    }
    startProjYear := strat.StartAge - s.CurrentAge
    endProjYear := strat.EndAge - s.CurrentAge
    if startProjYear < 0 {
        startProjYear = 0
    }

    cfg := &models.RothConversionConfig{
        Enabled:   true,
        StartYear: startProjYear,
        EndYear:   endProjYear - 1, // inclusive end
    }

    switch strat.Kind {
    case models.RothStrategyLadder:
        cfg.AnnualAmount = strat.AnnualAmount
    case models.RothStrategyBracketFill:
        ceiling, ok := bracketTopFor(s.TaxConfig.FilingStatus, strat.TargetBracket)
        if !ok {
            return &models.RothConversionConfig{Enabled: false}
        }
        overrides := make(map[int]float64, endProjYear-startProjYear)
        for y := startProjYear; y < endProjYear; y++ {
            other := estimateOtherTaxableIncome(s, y)
            conv := ceiling - other
            if conv < 0 {
                conv = 0
            }
            overrides[y] = conv
        }
        cfg.PerYearOverrides = overrides
    }
    return cfg
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run "TestEnumerateBracketFill|TestEnumerateRothStrategies|TestEnumerateLadder|TestEstimateOther" -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer_strategies_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): bracket-fill enumeration + strategy aggregator

Adds the bracket-fill Roth strategy family (target bracket × valid
window) with PerYearOverrides pre-computed from
estimateOtherTaxableIncome. enumerateRothStrategies combines ladder
and bracket-fill families. rothStrategyToConfig maps a strategy to
a RothConversionConfig the engine consumes.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Eligibility + settings cloning helpers

**Files:**
- Create: `internal/services/retirement/analysis/tax_optimizer.go`
- Create: `internal/services/retirement/analysis/tax_optimizer_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/analysis/tax_optimizer_test.go`:

```go
package analysis

import (
    "testing"

    "budget2/internal/models"
)

func TestTaxOptimizerEligible_HappyPath(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         67,
        ProjectionYears:    31,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
    }
    ok, reason := taxOptimizerEligible(s)
    if !ok {
        t.Errorf("expected eligible, got reason=%q", reason)
    }
}

func TestTaxOptimizerEligible_Rejections(t *testing.T) {
    base := func() *models.WhatIfSettings {
        return &models.WhatIfSettings{
            CurrentAge:         67,
            ProjectionYears:    31,
            PortfolioValue:     2_000_000,
            TaxDeferredPercent: 80,
            TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
        }
    }

    cases := []struct {
        name   string
        mutate func(*models.WhatIfSettings)
    }{
        {"no_tax_config", func(s *models.WhatIfSettings) { s.TaxConfig = nil }},
        {"empty_filing_status", func(s *models.WhatIfSettings) { s.TaxConfig.FilingStatus = "" }},
        {"tax_deferred_too_small", func(s *models.WhatIfSettings) {
            s.PortfolioValue = 100_000
            s.TaxDeferredPercent = 50 // → $50k tax-deferred, below $100k
        }},
        {"post_rmd_age", func(s *models.WhatIfSettings) { s.CurrentAge = 73 }},
        {"projection_too_short", func(s *models.WhatIfSettings) { s.ProjectionYears = 4 }},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            s := base()
            c.mutate(s)
            ok, reason := taxOptimizerEligible(s)
            if ok {
                t.Errorf("expected ineligible, got ok=true")
            }
            if reason == "" {
                t.Errorf("expected non-empty reason")
            }
        })
    }
}

func TestTaxOptimizerEligible_Boundaries(t *testing.T) {
    // age 72 = eligible, age 73 = not eligible
    s72 := &models.WhatIfSettings{
        CurrentAge: 72, ProjectionYears: 10,
        PortfolioValue: 1_000_000, TaxDeferredPercent: 50,
        TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
    }
    if ok, _ := taxOptimizerEligible(s72); !ok {
        t.Error("age 72 should be eligible")
    }
    s73 := *s72
    s73.CurrentAge = 73
    if ok, _ := taxOptimizerEligible(&s73); ok {
        t.Error("age 73 should be ineligible")
    }

    // tax-deferred exactly $100k = eligible; $99,999 = not eligible
    s100k := &models.WhatIfSettings{
        CurrentAge: 60, ProjectionYears: 30,
        PortfolioValue: 200_000, TaxDeferredPercent: 50,
        TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
    }
    if ok, _ := taxOptimizerEligible(s100k); !ok {
        t.Error("$100k tax-deferred should be eligible")
    }
    sBelow := *s100k
    sBelow.PortfolioValue = 199_999
    if ok, _ := taxOptimizerEligible(&sBelow); ok {
        t.Error("$99,999.50 tax-deferred should be ineligible")
    }
}

func TestCloneSettingsWithSSAndRoth_AppliesOverrides(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:         67,
        SpouseAge:          54,
        ProjectionYears:    31,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit: 3000, FRA: 67, ClaimAge: 67, SpouseClaimAge: 62,
        },
        RothConversion: &models.RothConversionConfig{
            Enabled: true, AnnualAmount: 50_000, StartYear: 0, EndYear: 10,
        },
    }
    strat := models.RothOptimizerStrategy{
        Kind:         models.RothStrategyLadder,
        AnnualAmount: 100_000,
        StartAge:     67,
        EndAge:       73,
    }

    prepared, ok := cloneSettingsWithSSAndRoth(s, 70, 67, strat)
    if !ok {
        t.Fatal("expected clone to succeed")
    }

    cloned := prepared.Settings()
    if cloned.SocialSecurity.ClaimAge != 70 {
        t.Errorf("primary claim age: got %d, want 70", cloned.SocialSecurity.ClaimAge)
    }
    if cloned.SocialSecurity.SpouseClaimAge != 67 {
        t.Errorf("spouse claim age: got %d, want 67", cloned.SocialSecurity.SpouseClaimAge)
    }
    if cloned.RothConversion.AnnualAmount != 100_000 {
        t.Errorf("Roth amount: got %v, want 100000", cloned.RothConversion.AnnualAmount)
    }

    // Original must be unchanged.
    if s.SocialSecurity.ClaimAge != 67 {
        t.Error("original ClaimAge mutated")
    }
    if s.RothConversion.AnnualAmount != 50_000 {
        t.Error("original RothConversion mutated")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run "TestTaxOptimizerEligible|TestCloneSettingsWithSSAndRoth" -v`
Expected: FAIL — file doesn't exist yet.

- [ ] **Step 3: Create `tax_optimizer.go` with the helpers**

Create `internal/services/retirement/analysis/tax_optimizer.go`:

```go
// Tax Optimizer: ranks (SS claim pair × Roth strategy) candidates by
// real ending portfolio. See
// docs/superpowers/specs/2026-05-12-tax-optimizer-design.md.
package analysis

import (
    "fmt"

    "budget2/internal/models"
    "budget2/internal/services/retirement/prepare"
)

// Tax Optimizer tuning constants — single block for easy adjustment.
const (
    taxOptimizerEligibilityMinTaxDeferred    = 100_000.0
    taxOptimizerEligibilityMaxStartAge       = 73
    taxOptimizerEligibilityMinProjectionYears = 5
    taxOptimizerTopSSPairs                   = 3
    taxOptimizerTopFinalists                 = 5
    taxOptimizerMonteCarloRuns               = 32
)

// taxOptimizerEligible reports whether the scenario qualifies for the
// optimizer. Returns (false, reason) when ineligible; reason is the
// user-facing string rendered in the panel.
func taxOptimizerEligible(s *models.WhatIfSettings) (bool, string) {
    if s == nil {
        return false, "No scenario loaded."
    }
    if s.TaxConfig == nil || s.TaxConfig.FilingStatus == "" {
        return false, "Set tax filing status to enable optimization."
    }
    taxDeferred := s.PortfolioValue * (s.TaxDeferredPercent / 100.0)
    if taxDeferred < taxOptimizerEligibilityMinTaxDeferred {
        return false, fmt.Sprintf("Tax-deferred balance too small to optimize ($%.0f).", taxDeferred)
    }
    if s.CurrentAge >= taxOptimizerEligibilityMaxStartAge {
        return false, "Optimizer requires pre-RMD horizon."
    }
    if s.ProjectionYears < taxOptimizerEligibilityMinProjectionYears {
        return false, "Projection too short to optimize."
    }
    return true, ""
}

// cloneSettingsWithSSAndRoth returns a prepared snapshot identical to s
// except for the SS claim ages and Roth conversion config. The deep
// copy in prepare.From handles slice/pointer aliasing for the rest of
// the struct. Pattern mirrors cloneSettingsWithClaimAges in ss.go.
func cloneSettingsWithSSAndRoth(s *models.WhatIfSettings, primaryClaimAge, spouseClaimAge int, strat models.RothOptimizerStrategy) (prepare.PreparedSettings, bool) {
    if s == nil {
        return prepare.PreparedSettings{}, false
    }
    cfg := *s
    if s.SocialSecurity != nil {
        ssCopy := *s.SocialSecurity
        ssCopy.ClaimAge = primaryClaimAge
        ssCopy.SpouseClaimAge = spouseClaimAge
        cfg.SocialSecurity = &ssCopy
    }
    cfg.RothConversion = rothStrategyToConfig(s, strat)
    return perturbAndPrepare(&cfg), true
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run "TestTaxOptimizerEligible|TestCloneSettingsWithSSAndRoth" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): tax optimizer eligibility + settings cloning

taxOptimizerEligible gates the optimizer with explicit reasons for
each rejection. cloneSettingsWithSSAndRoth produces a prepared
snapshot with SS pair + Roth strategy overrides applied, mirroring
the existing cloneSettingsWithClaimAges pattern in ss.go.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `scoreCandidate` + `topKSSPairs`

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go`
- Modify: `internal/services/retirement/analysis/tax_optimizer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_test.go`:

```go
import (
    // (existing imports)
    "budget2/internal/services/retirement/engine"
)

func TestScoreCandidate_PopulatesFields(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{
        Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
    })
    if !ok {
        t.Fatal("clone failed")
    }
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    cand := scoreCandidate(eng, in, 67, 62, models.RothOptimizerStrategy{
        Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
    })

    if cand.PrimaryClaimAge != 67 {
        t.Errorf("PrimaryClaimAge: got %d", cand.PrimaryClaimAge)
    }
    if cand.SpouseClaimAge != 62 {
        t.Errorf("SpouseClaimAge: got %d", cand.SpouseClaimAge)
    }
    if cand.EndingPortfolioReal <= 0 {
        t.Errorf("EndingPortfolioReal not populated: %v", cand.EndingPortfolioReal)
    }
    if cand.LifetimeTaxReal < 0 {
        t.Errorf("LifetimeTaxReal negative: %v", cand.LifetimeTaxReal)
    }
}

func TestTopKSSPairs_ExtractsBestPairs(t *testing.T) {
    ss := &models.SSPortfolioAnalysis{
        PrimaryOptions: []models.SSPortfolioOption{
            {ClaimAge: 67, SurvivalRate: 0.85},
            {ClaimAge: 70, SurvivalRate: 0.90}, // best
            {ClaimAge: 65, SurvivalRate: 0.80},
        },
        SpouseOptions: []models.SSPortfolioOption{
            {ClaimAge: 62, SurvivalRate: 0.88},
            {ClaimAge: 67, SurvivalRate: 0.91}, // best
        },
        OptimalPrimaryAge: 70,
        OptimalSpouseAge:  67,
    }
    currentPrimary, currentSpouse := 67, 62

    pairs := topKSSPairs(ss, currentPrimary, currentSpouse, 3)
    if len(pairs) == 0 {
        t.Fatal("expected at least one pair")
    }
    // Best from each axis must appear.
    foundBest := false
    for _, p := range pairs {
        if p.Primary == 70 && p.Spouse == 67 {
            foundBest = true
        }
    }
    if !foundBest {
        t.Error("expected (70, 67) joint-best pair in result")
    }
    if len(pairs) > 3 {
        t.Errorf("expected ≤3 pairs, got %d", len(pairs))
    }
}

func TestTopKSSPairs_NilFallsBackToCurrent(t *testing.T) {
    pairs := topKSSPairs(nil, 67, 62, 3)
    if len(pairs) != 1 {
        t.Fatalf("expected single-pair fallback, got %d", len(pairs))
    }
    if pairs[0].Primary != 67 || pairs[0].Spouse != 62 {
        t.Errorf("fallback pair: got (%d, %d), want (67, 62)", pairs[0].Primary, pairs[0].Spouse)
    }
}

// minimalEligibleSettings returns a tax-optimizer-eligible scenario
// useful across tests in this file. Defined once here to keep tests
// DRY.
func minimalEligibleSettings(_ *testing.T) *models.WhatIfSettings {
    return &models.WhatIfSettings{
        CurrentAge:         67,
        SpouseAge:          54,
        ProjectionYears:    20,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        RothPercent:        0,
        InvestmentReturn:   6,
        InflationRate:      3,
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit:       4_100,
            FRA:              67,
            SpouseFRABenefit: 1_500,
            SpouseFRA:        67,
            ClaimAge:         67,
            SpouseClaimAge:   62,
            COLARate:         0.02,
        },
        RothConversion: &models.RothConversionConfig{
            Enabled: true, AnnualAmount: 50_000, StartYear: 0,
        },
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run "TestScoreCandidate|TestTopKSSPairs" -v`
Expected: FAIL.

- [ ] **Step 3: Add `scoreCandidate`, `topKSSPairs`, and `ssPair` to `tax_optimizer.go`**

Append to `internal/services/retirement/analysis/tax_optimizer.go`:

```go
import (
    // (existing imports)
    "math"
    "sort"

    "budget2/internal/services/retirement/engine"
)
```

(Then append the functions below — adjust the import block above as needed.)

```go
// ssPair holds one (primary, spouse) claim-age combination.
type ssPair struct {
    Primary int
    Spouse  int
}

// topKSSPairs returns up to k joint SS pairs to search. When ss is nil
// or empty, returns a single fallback pair using the user's current
// settings. Otherwise composes pairs from each axis's top survival
// candidates plus the joint optimum.
func topKSSPairs(ss *models.SSPortfolioAnalysis, currentPrimary, currentSpouse, k int) []ssPair {
    if ss == nil || (len(ss.PrimaryOptions) == 0 && len(ss.SpouseOptions) == 0) {
        return []ssPair{{Primary: currentPrimary, Spouse: currentSpouse}}
    }
    if k <= 0 {
        k = 1
    }

    primaryRanked := append([]models.SSPortfolioOption{}, ss.PrimaryOptions...)
    sort.SliceStable(primaryRanked, func(i, j int) bool {
        return primaryRanked[i].SurvivalRate > primaryRanked[j].SurvivalRate
    })
    spouseRanked := append([]models.SSPortfolioOption{}, ss.SpouseOptions...)
    sort.SliceStable(spouseRanked, func(i, j int) bool {
        return spouseRanked[i].SurvivalRate > spouseRanked[j].SurvivalRate
    })

    pickPrimary := func(i int) int {
        if i < len(primaryRanked) {
            return primaryRanked[i].ClaimAge
        }
        return currentPrimary
    }
    pickSpouse := func(i int) int {
        if i < len(spouseRanked) {
            return spouseRanked[i].ClaimAge
        }
        return currentSpouse
    }

    // Build top-K by zipping ranks; always include the optimum pair
    // reported by SSPortfolio as the first entry.
    seen := map[ssPair]bool{}
    out := make([]ssPair, 0, k)
    addPair := func(p ssPair) {
        if seen[p] {
            return
        }
        seen[p] = true
        out = append(out, p)
    }

    if ss.OptimalPrimaryAge > 0 || ss.OptimalSpouseAge > 0 {
        opt := ssPair{
            Primary: ss.OptimalPrimaryAge,
            Spouse:  ss.OptimalSpouseAge,
        }
        if opt.Primary == 0 {
            opt.Primary = currentPrimary
        }
        if opt.Spouse == 0 {
            opt.Spouse = currentSpouse
        }
        addPair(opt)
    }
    for i := 0; len(out) < k; i++ {
        if i >= len(primaryRanked) && i >= len(spouseRanked) {
            break
        }
        addPair(ssPair{Primary: pickPrimary(i), Spouse: pickSpouse(i)})
    }
    if len(out) == 0 {
        out = append(out, ssPair{Primary: currentPrimary, Spouse: currentSpouse})
    }
    return out
}

// projectionToCandidate extracts scoring fields from a finished
// projection. Returns a candidate with the "failed projection"
// sentinel EndingPortfolioReal == -math.MaxFloat64 for nil/empty/NaN
// projections so callers can drop the candidate while still counting
// it. PeakMarginalBracket and TotalRothConverted require explainability
// fields the engine does not expose in Phase 1; both remain zero
// and the UI renders "—" when zero.
func projectionToCandidate(proj *models.ProjectionResult, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
    cand := models.TaxOptimizerCandidate{
        PrimaryClaimAge: primaryClaim,
        SpouseClaimAge:  spouseClaim,
        RothStrategy:    strat,
    }
    if proj == nil || len(proj.YearlySummaries) == 0 {
        cand.EndingPortfolioReal = -math.MaxFloat64
        return cand
    }
    last := proj.YearlySummaries[len(proj.YearlySummaries)-1]
    ending := last.EndingBalanceReal
    if math.IsNaN(ending) || math.IsInf(ending, 0) {
        cand.EndingPortfolioReal = -math.MaxFloat64
        return cand
    }
    cand.EndingPortfolioReal = ending
    for _, ys := range proj.YearlySummaries {
        cand.LifetimeTaxReal += ys.Taxes
    }
    return cand
}

// scoreCandidate runs a deterministic projection for the given (SS
// pair, Roth strategy) override and returns the scored candidate.
func scoreCandidate(eng *engine.Engine, in engine.Input, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
    cloned, ok := cloneSettingsWithSSAndRoth(in.Prepared.Settings(), primaryClaim, spouseClaim, strat)
    if !ok {
        return models.TaxOptimizerCandidate{
            PrimaryClaimAge:     primaryClaim,
            SpouseClaimAge:      spouseClaim,
            RothStrategy:        strat,
            EndingPortfolioReal: -math.MaxFloat64,
        }
    }
    cellInput := engine.Input{Prepared: cloned, Chain: in.Chain, Hooks: in.Hooks}
    proj := eng.Run(cellInput)
    return projectionToCandidate(proj, primaryClaim, spouseClaim, strat)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run "TestScoreCandidate|TestTopKSSPairs" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): tax optimizer scoring + top-K SS pair extraction

scoreCandidate runs a deterministic projection with overridden SS pair
and Roth strategy, returning a populated TaxOptimizerCandidate. NaN/Inf
and nil-projection paths coerce score to -MaxFloat64. topKSSPairs
extracts up to k joint SS pairs from SSPortfolioAnalysis, with a
single-pair fallback for nil/empty inputs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Main `TaxOptimizer` orchestration (deterministic-only)

Wires baseline → top-K pairs → Roth enumeration → score grid → top-5 sort, **without MC**. MC arrives in Task 9.

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go`
- Modify: `internal/services/retirement/analysis/tax_optimizer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_test.go`:

```go
func TestTaxOptimizer_IneligibleReturnsReason(t *testing.T) {
    s := &models.WhatIfSettings{
        CurrentAge:     67,
        ProjectionYears: 31,
        PortfolioValue: 50_000, // too small
    }
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    result := TaxOptimizerWithSeed(eng, in, nil, 42)

    if result == nil {
        t.Fatal("expected non-nil result for ineligible scenario")
    }
    if result.Eligible {
        t.Error("expected Eligible=false")
    }
    if result.IneligibleReason == "" {
        t.Error("expected non-empty IneligibleReason")
    }
}

func TestTaxOptimizer_EligibleProducesTop5(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    result := TaxOptimizerWithSeed(eng, in, nil, 42)

    if result == nil || !result.Eligible {
        t.Fatalf("expected eligible result, got %+v", result)
    }
    if len(result.Top) == 0 {
        t.Fatal("expected non-empty Top")
    }
    if len(result.Top) > taxOptimizerTopFinalists {
        t.Errorf("Top length %d exceeds finalists cap %d", len(result.Top), taxOptimizerTopFinalists)
    }

    // Top must be sorted descending by EndingPortfolioReal (before MC).
    for i := 1; i < len(result.Top); i++ {
        if result.Top[i].EndingPortfolioReal > result.Top[i-1].EndingPortfolioReal {
            t.Errorf("Top not sorted desc: index %d > %d", i, i-1)
        }
    }

    if result.Best.EndingPortfolioReal != result.Top[0].EndingPortfolioReal {
        t.Error("Best should match Top[0]")
    }

    if result.CandidatesScored == 0 {
        t.Error("CandidatesScored should be > 0")
    }
}

func TestTaxOptimizer_BaselineMatchesCurrent(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    result := TaxOptimizerWithSeed(eng, in, nil, 42)
    if result == nil || !result.Eligible {
        t.Fatal("expected eligible")
    }
    if result.Baseline.PrimaryClaimAge != s.SocialSecurity.ClaimAge {
        t.Errorf("Baseline.PrimaryClaimAge: got %d, want %d", result.Baseline.PrimaryClaimAge, s.SocialSecurity.ClaimAge)
    }
    if result.Baseline.SpouseClaimAge != s.SocialSecurity.SpouseClaimAge {
        t.Errorf("Baseline.SpouseClaimAge: got %d, want %d", result.Baseline.SpouseClaimAge, s.SocialSecurity.SpouseClaimAge)
    }
}

func TestTaxOptimizer_Deterministic(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    r1 := TaxOptimizerWithSeed(eng, in, nil, 42)
    r2 := TaxOptimizerWithSeed(eng, in, nil, 42)

    if r1 == nil || r2 == nil {
        t.Fatal("expected both results non-nil")
    }
    if len(r1.Top) != len(r2.Top) {
        t.Fatalf("top length mismatch: %d vs %d", len(r1.Top), len(r2.Top))
    }
    for i := range r1.Top {
        if r1.Top[i].PrimaryClaimAge != r2.Top[i].PrimaryClaimAge ||
            r1.Top[i].SpouseClaimAge != r2.Top[i].SpouseClaimAge ||
            r1.Top[i].RothStrategy.Label != r2.Top[i].RothStrategy.Label {
            t.Errorf("non-deterministic at index %d", i)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run TestTaxOptimizer -v`
Expected: FAIL — `TaxOptimizerWithSeed` not defined.

- [ ] **Step 3: Implement `TaxOptimizer` and `TaxOptimizerWithSeed`**

Append to `internal/services/retirement/analysis/tax_optimizer.go`:

```go
// TaxOptimizer runs the Tax Optimizer and returns a recommendation.
// Always synchronous. Eligibility is gated; ineligible scenarios
// return a non-nil result with Eligible=false and IneligibleReason set.
// Uses the auto-seed convention (seed=0) for Monte Carlo refinement.
func TaxOptimizer(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis) *models.TaxOptimizerAnalysis {
    return TaxOptimizerWithSeed(eng, in, ss, 0)
}

// TaxOptimizerWithSeed is TaxOptimizer with an explicit Monte Carlo
// seed for deterministic tests. seed=0 means auto-seed.
func TaxOptimizerWithSeed(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis, seed int64) *models.TaxOptimizerAnalysis {
    settings := in.Prepared.Settings()
    if ok, reason := taxOptimizerEligible(settings); !ok {
        return &models.TaxOptimizerAnalysis{
            Eligible:         false,
            IneligibleReason: reason,
        }
    }

    currentPrimary, currentSpouse := 0, 0
    var currentRoth models.RothOptimizerStrategy
    if settings.SocialSecurity != nil {
        currentPrimary = settings.SocialSecurity.ClaimAge
        currentSpouse = settings.SocialSecurity.SpouseClaimAge
    }
    if settings.RothConversion != nil && settings.RothConversion.Enabled {
        currentRoth = models.RothOptimizerStrategy{
            Kind:         models.RothStrategyLadder,
            AnnualAmount: settings.RothConversion.AnnualAmount,
            StartAge:     settings.CurrentAge + settings.RothConversion.StartYear,
            EndAge:       settings.CurrentAge + settings.RothConversion.EndYear,
            Label:        "Current scenario",
        }
        if currentRoth.EndAge <= currentRoth.StartAge {
            currentRoth.EndAge = settings.CurrentAge + settings.ProjectionYears
        }
    } else {
        currentRoth = models.RothOptimizerStrategy{
            Kind: models.RothStrategyNone, Label: "Current (no conversions)",
        }
    }

    // Baseline is scored from the user's saved input directly — not via
    // a reconstructed strategy clone — so its metrics exactly match
    // what the rest of the page shows for this scenario.
    baselineProj := eng.Run(in)
    baseline := projectionToCandidate(baselineProj, currentPrimary, currentSpouse, currentRoth)

    pairs := topKSSPairs(ss, currentPrimary, currentSpouse, taxOptimizerTopSSPairs)
    strategies := enumerateRothStrategies(settings)

    scored := make([]models.TaxOptimizerCandidate, 0, len(pairs)*len(strategies))
    for _, p := range pairs {
        for _, strat := range strategies {
            cand := scoreCandidate(eng, in, p.Primary, p.Spouse, strat)
            if cand.EndingPortfolioReal == -math.MaxFloat64 {
                continue // drop failed projections from Top but still count them
            }
            scored = append(scored, cand)
        }
    }

    sort.SliceStable(scored, func(i, j int) bool {
        return scored[i].EndingPortfolioReal > scored[j].EndingPortfolioReal
    })

    finalists := scored
    if len(finalists) > taxOptimizerTopFinalists {
        finalists = finalists[:taxOptimizerTopFinalists]
    }

    result := &models.TaxOptimizerAnalysis{
        Eligible:         true,
        Baseline:         baseline,
        Top:              finalists,
        CandidatesScored: len(pairs) * len(strategies),
        // MonteCarloRuns wired in Task 9.
    }
    if len(finalists) > 0 {
        result.Best = finalists[0]
    }
    return result
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run TestTaxOptimizer -v`
Expected: PASS, all subtests.

Run: `go test ./internal/services/retirement/analysis/...`
Expected: PASS (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): tax optimizer orchestration (deterministic ranking)

TaxOptimizer + TaxOptimizerWithSeed run the full pipeline: baseline
score → top-K SS pairs × Roth strategies → sorted top-5 candidates.
Ineligibility returns a non-nil result with a user-facing reason. MC
refinement of top-5 lands in the next task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Monte Carlo top-5 refinement

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go`
- Modify: `internal/services/retirement/analysis/tax_optimizer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_test.go`:

```go
func TestTaxOptimizer_MCRefinementPopulatesFields(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    result := TaxOptimizerWithSeed(eng, in, nil, 42)
    if result == nil || !result.Eligible {
        t.Fatal("expected eligible")
    }
    if result.MonteCarloRuns == 0 {
        t.Errorf("MonteCarloRuns should be set to %d", taxOptimizerMonteCarloRuns)
    }
    if len(result.Top) == 0 {
        t.Fatal("expected non-empty Top")
    }
    for i, cand := range result.Top {
        if cand.MCMedianEndingReal == 0 {
            t.Errorf("Top[%d].MCMedianEndingReal should be populated after MC", i)
        }
        if cand.MCSurvivalRate <= 0 || cand.MCSurvivalRate > 1 {
            t.Errorf("Top[%d].MCSurvivalRate out of [0,1]: %v", i, cand.MCSurvivalRate)
        }
    }
}

func TestTaxOptimizer_MCDeterministicWithSeed(t *testing.T) {
    s := minimalEligibleSettings(t)
    prep := perturbAndPrepare(s)
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    r1 := TaxOptimizerWithSeed(eng, in, nil, 42)
    r2 := TaxOptimizerWithSeed(eng, in, nil, 42)

    if r1 == nil || r2 == nil || len(r1.Top) != len(r2.Top) {
        t.Fatal("expected same length results")
    }
    for i := range r1.Top {
        // MC values must match to a small epsilon under a fixed seed.
        if math.Abs(r1.Top[i].MCMedianEndingReal-r2.Top[i].MCMedianEndingReal) > 1 {
            t.Errorf("MCMedianEndingReal differs at index %d: %v vs %v",
                i, r1.Top[i].MCMedianEndingReal, r2.Top[i].MCMedianEndingReal)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run TestTaxOptimizer_MC -v`
Expected: FAIL — MC fields not populated.

- [ ] **Step 3: Implement MC refinement**

Insert before the final `return result` in `TaxOptimizerWithSeed` (in `tax_optimizer.go`):

```go
    // Monte Carlo refinement of top finalists. Reuses the existing
    // analysis.MonteCarlo entry point with a small budget.
    for i := range finalists {
        mcCloned, ok := cloneSettingsWithSSAndRoth(settings,
            finalists[i].PrimaryClaimAge,
            finalists[i].SpouseClaimAge,
            finalists[i].RothStrategy,
        )
        if !ok {
            continue
        }
        mcInput := engine.Input{Prepared: mcCloned, Chain: in.Chain, Hooks: in.Hooks}
        mc := MonteCarlo(eng, mcInput, taxOptimizerMonteCarloRuns, seed)
        if mc == nil || mc.Stats == nil {
            continue
        }
        finalists[i].MCSurvivalRate = mc.Stats.SuccessRate
        finalists[i].MCMedianEndingReal = mc.Stats.MedianBalance
    }

    // Re-sort by MC median ending balance (tiebreaker).
    sort.SliceStable(finalists, func(i, j int) bool {
        return finalists[i].MCMedianEndingReal > finalists[j].MCMedianEndingReal
    })

    result.MonteCarloRuns = taxOptimizerMonteCarloRuns
    result.Top = finalists
    if len(finalists) > 0 {
        result.Best = finalists[0]
    }
```

> **Replace** the previous `result := ...` block ending so that `result.Top = finalists` and `result.Best` are set **after** the MC pass. The cleanest shape:

```go
    result := &models.TaxOptimizerAnalysis{
        Eligible:         true,
        Baseline:         baseline,
        CandidatesScored: len(pairs) * len(strategies),
    }

    // ... MC refinement loop here ...

    result.MonteCarloRuns = taxOptimizerMonteCarloRuns
    result.Top = finalists
    if len(finalists) > 0 {
        result.Best = finalists[0]
    }
    return result
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run TestTaxOptimizer -v`
Expected: PASS (both deterministic + MC tests).

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(analysis): tax optimizer Monte Carlo top-5 refinement

Adds a small-budget MC pass over the deterministic-ranked top-5
finalists. Re-sorts by MC median ending balance and reports per-
candidate survival rate. Seed-deterministic for tests.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Wire `TaxOptimizer` into the orchestrator

**Files:**
- Modify: `internal/services/retirement/orchestrator.go`

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/analysis/tax_optimizer_integration_test.go`:

```go
package analysis_test

import (
    "testing"

    "budget2/internal/models"
    "budget2/internal/services/retirement"
    "budget2/internal/services/retirement/engine"
    "budget2/internal/services/retirement/prepare"
)

func TestRunFull_AttachesTaxOptimizer(t *testing.T) {
    s := &models.WhatIfSettings{
        ScenarioName:       "test",
        StartDate:          "2026-01",
        CurrentAge:         67,
        SpouseAge:          54,
        ProjectionYears:    20,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        InvestmentReturn:   6,
        InflationRate:      3,
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
        SocialSecurity: &models.SocialSecurityConfig{
            FRABenefit: 4100, FRA: 67, SpouseFRABenefit: 1500, SpouseFRA: 67,
            ClaimAge: 67, SpouseClaimAge: 62, COLARate: 0.02,
        },
        RothConversion: &models.RothConversionConfig{
            Enabled: true, AnnualAmount: 50_000, StartYear: 0,
        },
    }
    prep, err := prepare.From(s)
    if err != nil {
        t.Fatalf("prepare.From: %v", err)
    }
    in := engine.Input{Prepared: prep}
    eng := engine.New()

    analysis := retirement.RunFull(eng, in)
    if analysis == nil {
        t.Fatal("RunFull returned nil")
    }
    if analysis.TaxOptimizer == nil {
        t.Fatal("TaxOptimizer not attached to WhatIfAnalysis")
    }
    if !analysis.TaxOptimizer.Eligible {
        t.Errorf("expected eligible; reason=%q", analysis.TaxOptimizer.IneligibleReason)
    }
}

func TestRunFull_TaxOptimizerIneligibleStillNonNil(t *testing.T) {
    s := &models.WhatIfSettings{
        StartDate:          "2026-01",
        CurrentAge:         75, // post-RMD
        ProjectionYears:    10,
        PortfolioValue:     2_000_000,
        TaxDeferredPercent: 80,
        TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingSingle},
        SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67, COLARate: 0.02},
    }
    prep, err := prepare.From(s)
    if err != nil {
        t.Fatalf("prepare.From: %v", err)
    }
    in := engine.Input{Prepared: prep}

    analysis := retirement.RunFull(engine.New(), in)
    if analysis == nil || analysis.TaxOptimizer == nil {
        t.Fatal("expected non-nil TaxOptimizer even when ineligible")
    }
    if analysis.TaxOptimizer.Eligible {
        t.Error("expected Eligible=false for age 75")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/analysis/ -run TestRunFull_AttachesTaxOptimizer -v`
Expected: FAIL — `TaxOptimizer` field never populated by `RunFull`.

- [ ] **Step 3: Wire the call in `orchestrator.go`**

In `internal/services/retirement/orchestrator.go`, modify `runFullWithSeed` to add the optimizer call after the SS portfolio attachment. Replace the SS block (lines 58-65 in current file) and the final return with:

```go
    var ssAnalysis *models.SSComparisonAnalysis
    settings := in.Prepared.Settings()
    if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
        ssAnalysis = analysis.SSAnalysis(in)
        if ssAnalysis != nil && SSPortfolioEligible(settings) {
            ssAnalysis.Portfolio = analysis.SSPortfolioWithSeed(eng, in, ssAnalysis, mcSeed)
        }
    }

    var taxOptimizer *models.TaxOptimizerAnalysis
    {
        var ssPortfolio *models.SSPortfolioAnalysis
        if ssAnalysis != nil {
            ssPortfolio = ssAnalysis.Portfolio
        }
        taxOptimizer = analysis.TaxOptimizerWithSeed(eng, in, ssPortfolio, mcSeed)
    }

    return &models.WhatIfAnalysis{
        Settings:                 settings,
        Projection:               proj,
        ProjectionExplainability: explainability,
        BudgetFit:                budgetFit,
        PresentValue:             presentValue,
        Sustainability:           sustainability,
        Sensitivity:              sensitivity,
        FailurePoints:            failurePoints,
        MonteCarlo:               monteCarlo,
        RMD:                      rmd,
        HistoricalBacktest:       backtest,
        SocialSecurity:           ssAnalysis,
        TaxOptimizer:             taxOptimizer,
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/services/retirement/analysis/ -run TestRunFull -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/orchestrator.go internal/services/retirement/analysis/tax_optimizer_integration_test.go
git commit -m "$(cat <<'EOF'
feat(retirement): attach Tax Optimizer to RunFull output

The orchestrator now calls analysis.TaxOptimizerWithSeed after the SS
portfolio analysis runs, passing the SS portfolio result so the
optimizer can extract top-K SS pairs. The TaxOptimizer field is
always populated; Eligible=false carries the user-facing reason.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: UI panel template + handler wiring

**Files:**
- Create: `web/templates/whatif/_tax_optimizer.html`
- Modify: the layout template the what-if handler renders (locate via `internal/handlers/whatif/handlers.go`)
- Modify: `internal/handlers/whatif/handlers_test.go` (smoke render test)

- [ ] **Step 1: Locate the what-if template entry point**

Run: `grep -rn "_ss_portfolio\|tmpl\.Execute\|ExecuteTemplate" internal/handlers/whatif/handlers.go | head -20`

The output points to the template the handler renders. Identify the partial-inclusion site for the SS Portfolio panel — the Tax Optimizer panel goes immediately below.

- [ ] **Step 2: Create `_tax_optimizer.html`**

Create `web/templates/whatif/_tax_optimizer.html`:

```html
{{ define "tax_optimizer" }}
{{ with .TaxOptimizer }}
<section class="card tax-optimizer">
  <h2>Tax Optimizer</h2>
  {{ if not .Eligible }}
    <p class="muted">{{ .IneligibleReason }}</p>
  {{ else }}
    <div class="best">
      <h3>Best strategy (Monte Carlo refined)</h3>
      <p class="strategy-label">{{ .Best.RothStrategy.Label }}</p>
      <p class="ss-pair">SS: primary {{ .Best.PrimaryClaimAge }}, spouse {{ .Best.SpouseClaimAge }}</p>
      <ul class="metrics">
        <li>Ending portfolio (real): ${{ formatMoney .Best.EndingPortfolioReal }}
          <span class="delta">{{ deltaVs .Best.EndingPortfolioReal $.TaxOptimizer.Baseline.EndingPortfolioReal }}</span>
        </li>
        <li>Lifetime tax (real): ${{ formatMoney .Best.LifetimeTaxReal }}</li>
        <li>Monte Carlo survival: {{ printf "%.0f%%" (mul .Best.MCSurvivalRate 100.0) }}</li>
        <li>MC median ending: ${{ formatMoney .Best.MCMedianEndingReal }}</li>
      </ul>
    </div>

    <h3>Top {{ len .Top }} alternatives</h3>
    <table class="top-alternatives">
      <thead>
        <tr>
          <th>Strategy</th>
          <th>SS pair</th>
          <th>End portfolio (real)</th>
          <th>Lifetime tax</th>
          <th>Δ vs baseline</th>
          <th>MC survival</th>
        </tr>
      </thead>
      <tbody>
        {{ range $i, $c := .Top }}
        <tr {{ if eq $i 0 }}class="winner"{{ end }}>
          <td>{{ $c.RothStrategy.Label }}</td>
          <td>{{ $c.PrimaryClaimAge }}/{{ $c.SpouseClaimAge }}</td>
          <td>${{ formatMoney $c.EndingPortfolioReal }}</td>
          <td>${{ formatMoney $c.LifetimeTaxReal }}</td>
          <td>{{ deltaVs $c.EndingPortfolioReal $.TaxOptimizer.Baseline.EndingPortfolioReal }}</td>
          <td>{{ printf "%.0f%%" (mul $c.MCSurvivalRate 100.0) }}</td>
        </tr>
        {{ end }}
      </tbody>
    </table>

    <p class="caveat">
      ⓘ Recommendations are read-only. Edit your scenario manually to apply.
      Survivor-spouse filing-status changes are not modeled in Phase&nbsp;1.
    </p>
  {{ end }}
</section>
{{ end }}
{{ end }}
```

> **Template helper functions:** If `formatMoney`, `deltaVs`, and `mul` are not already registered in the template's `FuncMap`, add them. Check `internal/templates/funcs.go` (or equivalent) for the existing function map.
>
> **Helper functions to add** (in the existing funcmap registration file):
>
> ```go
> "formatMoney": func(v float64) string {
>     return fmt.Sprintf("%.0f", v)
> },
> "deltaVs": func(value, baseline float64) string {
>     delta := value - baseline
>     sign := "+"
>     if delta < 0 {
>         sign = "−"
>         delta = -delta
>     }
>     return fmt.Sprintf("%s$%.0f", sign, delta)
> },
> "mul": func(a, b float64) float64 { return a * b },
> ```
>
> Skip any helper already present.

- [ ] **Step 3: Include the partial in the what-if layout**

In the layout template the handler renders (likely `web/templates/whatif/whatif.html`), add immediately below the SS Portfolio panel inclusion:

```html
{{ template "tax_optimizer" . }}
```

- [ ] **Step 4: Add handler render smoke test**

In `internal/handlers/whatif/handlers_test.go`, add:

```go
func TestWhatIfHandler_RendersTaxOptimizerPanel(t *testing.T) {
    // Setup: load minimal eligible settings, run handler, assert that
    // the rendered HTML contains "Tax Optimizer" and either a best-
    // strategy block or an ineligibility reason. Follow the existing
    // handler-test bootstrap pattern in this file.

    // (Mirror the pattern from existing render tests, e.g.
    // TestWhatIfHandler_RendersSSPortfolioPanel if one exists.)
    body := renderWhatIfHandler(t, eligibleTestSettings())
    if !strings.Contains(body, "Tax Optimizer") {
        t.Error("expected rendered HTML to contain 'Tax Optimizer'")
    }
}
```

> **Note:** the exact bootstrap (`renderWhatIfHandler`, `eligibleTestSettings`) depends on local test helpers. Replace with whatever the existing render tests use; do not invent a new harness.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/handlers/whatif/... -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Visual check in browser**

The CLAUDE.md guidance says: "For UI or frontend changes, start the dev server and use the feature in a browser before reporting the task as complete."

Run: `make run` (or the project's standard dev-server command)

Open the what-if page in the browser. Confirm:
- The Tax Optimizer panel renders below the SS Portfolio panel
- For an eligible scenario: best card + top-5 table show with non-empty values
- For an ineligible scenario: a one-line reason renders in place
- Numbers look plausible (no NaN, no negative ending portfolios for sane inputs)

If the UI is broken, **do not** report the task complete — fix the rendering first.

- [ ] **Step 7: Commit**

```bash
git add web/templates/whatif/_tax_optimizer.html web/templates/whatif/whatif.html internal/templates/funcs.go internal/handlers/whatif/handlers_test.go
# Adjust paths to match where you actually edited.
git commit -m "$(cat <<'EOF'
feat(whatif): render Tax Optimizer panel

Adds the Tax Optimizer panel partial below the SS Portfolio panel on
the what-if page. Renders the best strategy + top-5 alternatives table
when eligible, or a one-line reason when not. Adds template helpers
formatMoney/deltaVs/mul if not already present.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Spec-Coverage Self-Check

| Spec section | Tasks |
|--------------|-------|
| `RothConversionConfig.PerYearOverrides` | Task 1 |
| `TaxOptimizerAnalysis` / `TaxOptimizerCandidate` / `RothOptimizerStrategy` / `RothStrategyKind` | Task 2 |
| `WhatIfAnalysis.TaxOptimizer` field | Task 2 |
| Ladder enumeration (~35 candidates) | Task 3 |
| `estimateOtherTaxableIncome` | Task 4 |
| Bracket-fill enumeration (~9 candidates) | Task 5 |
| `enumerateRothStrategies` aggregator | Task 5 |
| `rothStrategyToConfig` | Task 5 |
| Eligibility gate (4 rules + boundaries) | Task 6 |
| `cloneSettingsWithSSAndRoth` | Task 6 |
| `scoreCandidate` (NaN/Inf coercion) | Task 7 |
| `topKSSPairs` (incl. nil fallback) | Task 7 |
| Baseline + top-K SS × Roth grid + sort + MC refinement | Tasks 8–9 |
| Orchestrator wiring | Task 10 |
| UI panel + template helpers + handler test | Task 11 |

## Deferred (carried into implementation per spec)

- **PeakMarginalBracket / TotalRothConverted accurate population:** Engine `ProjectionResult` does not expose marginal-rate-per-year or per-year Roth conversion totals as first-class fields. Phase 1 leaves these as `0` in `TaxOptimizerCandidate` and the UI renders `—`. Promote when the explainability layer extends.
- **RMD estimate fidelity in `estimateOtherTaxableIncome`:** Uses a simple 4%-of-projected-balance estimate. If `TestEnumerateRothStrategies` or downstream rankings show drift, swap for `engine/rmd.go` helpers in a follow-up.
- **Survivor-spouse modeling:** explicitly Phase 2 per spec.
- **Withdrawal-order strategy:** explicitly Phase 2 per spec.
