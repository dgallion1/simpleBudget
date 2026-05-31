# Bracket-Fill Roth-Earnings Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop bracket-fill Roth-conversion sizing from overshooting the target tax bracket when the engine withdraws taxable Roth *earnings* (non-qualified, under 59½) during the conversion window.

**Architecture:** Thread the engine's per-year `TaxableRothEarnings` back into the existing closed-form bracket-fill solver as a known ordinary-income term, and iterate size→run→re-size to a self-consistent fixed point inside `scoreCandidate`. In the common case (no Roth earnings in conversion years) the loop terminates after one engine run with byte-identical behavior to today.

**Tech Stack:** Pure Go. All changes in `internal/services/retirement/analysis`. No engine changes — `ProjectionYearSummary.TaxableRothEarnings` already exists and the optimizer already holds the `ProjectionResult`. Tests use the `runProj`/`engineInput` helpers (`analysis/helpers_test.go`).

**Spec:** `docs/superpowers/specs/2026-05-31-bracketfill-roth-earnings-correction-design.md`

**Per CLAUDE.md:** assess blast radius with `LSP incomingCalls` before editing each symbol; finish with a green `go build ./... && go vet ./... && go test ./... && staticcheck ./...`.

---

## File Structure

- **Modify** `internal/services/retirement/analysis/tax_optimizer_strategies.go`
  - `bracketFillIncomeForYear` — gains a `rothEarnings float64` parameter, folded into `ordinary`.
  - `strategyYearlyConversions` — gains a `feedback map[int]float64` parameter (proj-year → Roth earnings), passes `feedback[y]` per year.
  - `rothStrategyToConfig` — gains a `feedback map[int]float64` parameter, passes it through.
  - New unexported helpers: `harvestRothEarnings`, `bracketFillProjYearWindow`, `maxAbsFeedbackDelta`, `relaxFeedback`, plus tuning consts.
- **Modify** `internal/services/retirement/analysis/tax_optimizer.go`
  - `cloneSettingsWithSSAndRoth` — gains a `feedback map[int]float64` parameter, passes it to `rothStrategyToConfig`.
  - `scoreCandidate` — wraps size+run in the iteration loop; uses the converged feedback for both the scored projection and the disclosed `PerYearConversions`.
- **Create** `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go`
  - Solver unit test, helper unit tests, integration (corner) test, regression (common-case) test.

---

## Task 1: Fold a Roth-earnings term into the bracket-fill solver

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go` (`bracketFillIncomeForYear` ~line 118; call sites at ~274, ~414, ~525)
- Test: `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go` (create)

- [ ] **Step 1: Assess blast radius (CLAUDE.md mandate)**

Run `LSP incomingCalls` on `bracketFillIncomeForYear` (cursor on the function name at its definition). Confirm the only callers are `estimateOtherTaxableIncome`, `bracketFillProducesNonZero`, and `strategyYearlyConversions` (all in this file). Report them. If any caller lives outside `analysis`, stop and flag it.

- [ ] **Step 2: Write the failing solver test**

Create `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go`:

```go
package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Folding a positive Roth-earnings term into bracketFillIncomeForYear must
// shrink the sized conversion so that taxable ordinary income INCLUDING the
// earnings lands on the bracket ceiling. With no Social Security in play, the
// shrink equals the earnings exactly (earnings displace conversion dollar-for-
// dollar in the ordinary bracket).
func TestBracketFillIncomeForYear_FoldsRothEarningsIntoOrdinary(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1] // single filer
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 55)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	// No SS benefit in the window: isolates the earnings→ordinary fold.
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 0, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	ceiling, ok := inflatedBracketTopForYear(ps, 0.22, 0)
	if !ok {
		t.Fatal("no inflated 22% ceiling for year 0")
	}

	const earnings = 20_000.0
	convNoEarn := bracketFillIncomeForYear(ps, 0, 0).bracketFillConversion(ceiling)
	convWithEarn := bracketFillIncomeForYear(ps, 0, earnings).bracketFillConversion(ceiling)

	if !(convWithEarn < convNoEarn) {
		t.Fatalf("earnings should shrink the conversion: noEarn=%.0f withEarn=%.0f", convNoEarn, convWithEarn)
	}
	// Ordinary income (incl. earnings) at the chosen conversion lands on the ceiling.
	got := bracketFillIncomeForYear(ps, 0, earnings).taxableOrdinaryIncome(convWithEarn)
	if math.Abs(got-ceiling) > 1.0 {
		t.Fatalf("ordinary incl earnings should land on ceiling: got=%.0f ceiling=%.0f", got, ceiling)
	}
	// With no SS, the shrink equals the earnings exactly.
	if d := (convNoEarn - convWithEarn) - earnings; math.Abs(d) > 1.0 {
		t.Fatalf("conversion should shrink by exactly the earnings; off by %.2f", d)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails (does not compile)**

Run: `go test ./internal/services/retirement/analysis/ -run TestBracketFillIncomeForYear_FoldsRothEarningsIntoOrdinary -v`
Expected: build failure — `not enough arguments in call to bracketFillIncomeForYear` (it currently takes 2 args, the test passes 3).

- [ ] **Step 4: Add the `rothEarnings` parameter to `bracketFillIncomeForYear`**

In `tax_optimizer_strategies.go`, change the signature and fold the term into `ordinary`. Update the doc comment's first line to mention the new term.

Change the signature line:
```go
func bracketFillIncomeForYear(s *models.WhatIfSettings, projectionYear int) bracketFillIncome {
```
to:
```go
// rothEarnings is the taxable portion of non-qualified Roth EARNINGS withdrawn
// in this projection year (engine's TaxableRothEarnings). It is ordinary income
// that both fills the bracket and raises §86 provisional income, so it is folded
// into `ordinary`. Pass 0 when no engine feedback is available (the gate and the
// pre-engine estimate do this).
func bracketFillIncomeForYear(s *models.WhatIfSettings, projectionYear int, rothEarnings float64) bracketFillIncome {
```

Then, immediately before the `return bracketFillIncome{` at the end of the function, add:
```go
	// Non-qualified Roth earnings are ordinary income (engine: loop_helpers.go:290).
	ordinary += rothEarnings
```

- [ ] **Step 5: Update the three existing call sites to pass 0**

- In `estimateOtherTaxableIncome` (~line 274):
  `taxable := bracketFillIncomeForYear(s, projectionYear).taxableOrdinaryIncome(0)`
  →
  `taxable := bracketFillIncomeForYear(s, projectionYear, 0).taxableOrdinaryIncome(0)`
- In `bracketFillProducesNonZero` (~line 414):
  `if bracketFillIncomeForYear(s, y).bracketFillConversion(ceiling) > 1 {`
  →
  `if bracketFillIncomeForYear(s, y, 0).bracketFillConversion(ceiling) > 1 {`
- In `strategyYearlyConversions` (~line 525):
  `conv := bracketFillIncomeForYear(s, y).bracketFillConversion(ceiling)`
  →
  `conv := bracketFillIncomeForYear(s, y, 0).bracketFillConversion(ceiling)`

- [ ] **Step 6: Run the solver test (passes) and the package suite (still green)**

Run: `go test ./internal/services/retirement/analysis/ -run TestBracketFillIncomeForYear_FoldsRothEarningsIntoOrdinary -v`
Expected: PASS.

Run: `go test ./internal/services/retirement/analysis/`
Expected: ok (all existing tests still pass — passing 0 preserves behavior).

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go
git commit -m "feat(analysis): bracket-fill solver accepts a Roth-earnings ordinary term

bracketFillIncomeForYear gains a rothEarnings parameter folded into ordinary
income (fills the bracket and §86 provisional income, matching the engine).
All current call sites pass 0, so behavior is unchanged.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Thread per-year feedback through the sizing call chain

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go` (`strategyYearlyConversions` ~486, `rothStrategyToConfig` ~440)
- Modify: `internal/services/retirement/analysis/tax_optimizer.go` (`cloneSettingsWithSSAndRoth` ~82; call sites at ~88, ~250, ~314)

This task is a pure plumbing change: add a `feedback map[int]float64` parameter that defaults to `nil` everywhere. With `nil` feedback, `feedback[y]` is `0`, so behavior stays identical. Verified by the existing suite.

- [ ] **Step 1: Assess blast radius (CLAUDE.md mandate)**

Run `LSP incomingCalls` on `strategyYearlyConversions`, `rothStrategyToConfig`, and `cloneSettingsWithSSAndRoth`. Confirm callers:
- `strategyYearlyConversions`: `rothStrategyToConfig` (~464), `tax_optimizer.go` (~250 disclosure, ~314 baseline disclosure).
- `rothStrategyToConfig`: `cloneSettingsWithSSAndRoth` (~88).
- `cloneSettingsWithSSAndRoth`: `scoreCandidate` (~234).
Report them. All should be inside `analysis`. Also check `*_test.go` for direct callers (update them in this task if present).

- [ ] **Step 2: Add `feedback` to `strategyYearlyConversions`**

Change the signature:
```go
func strategyYearlyConversions(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) []models.YearlyConversion {
```
to:
```go
// feedback maps projection-year offset → taxable Roth earnings observed from a
// prior engine run; it is folded into each year's ordinary income so the solve
// accounts for earnings that fill the bracket. Pass nil for the uncorrected
// (pre-engine) sizing.
func strategyYearlyConversions(s *models.WhatIfSettings, strat models.RothOptimizerStrategy, feedback map[int]float64) []models.YearlyConversion {
```
And in the bracket-fill branch change:
```go
			conv := bracketFillIncomeForYear(s, y, 0).bracketFillConversion(ceiling)
```
to:
```go
			conv := bracketFillIncomeForYear(s, y, feedback[y]).bracketFillConversion(ceiling)
```

- [ ] **Step 3: Add `feedback` to `rothStrategyToConfig` and pass it through**

Change the signature:
```go
func rothStrategyToConfig(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) *models.RothConversionConfig {
```
to:
```go
func rothStrategyToConfig(s *models.WhatIfSettings, strat models.RothOptimizerStrategy, feedback map[int]float64) *models.RothConversionConfig {
```
And in its bracket-fill branch change:
```go
		yearly := strategyYearlyConversions(s, strat)
```
to:
```go
		yearly := strategyYearlyConversions(s, strat, feedback)
```

- [ ] **Step 4: Add `feedback` to `cloneSettingsWithSSAndRoth` and pass it through**

In `tax_optimizer.go`, change the signature:
```go
func cloneSettingsWithSSAndRoth(s *models.WhatIfSettings, primaryClaimAge, spouseClaimAge int, strat models.RothOptimizerStrategy) (prepare.PreparedSettings, bool) {
```
to:
```go
func cloneSettingsWithSSAndRoth(s *models.WhatIfSettings, primaryClaimAge, spouseClaimAge int, strat models.RothOptimizerStrategy, feedback map[int]float64) (prepare.PreparedSettings, bool) {
```
And change:
```go
	cfg.RothConversion = rothStrategyToConfig(candidate, strat)
```
to:
```go
	cfg.RothConversion = rothStrategyToConfig(candidate, strat, feedback)
```

- [ ] **Step 5: Update the remaining call sites to pass `nil`**

- `scoreCandidate` (~234):
  `cloned, ok := cloneSettingsWithSSAndRoth(settings, primaryClaim, spouseClaim, strat)`
  →
  `cloned, ok := cloneSettingsWithSSAndRoth(settings, primaryClaim, spouseClaim, strat, nil)`
- `scoreCandidate` disclosure (~250):
  `cand.PerYearConversions = strategyYearlyConversions(candidateSettingsForSS(settings, primaryClaim, spouseClaim), strat)`
  →
  `cand.PerYearConversions = strategyYearlyConversions(candidateSettingsForSS(settings, primaryClaim, spouseClaim), strat, nil)`
- Baseline disclosure (~314):
  `baseline.PerYearConversions = strategyYearlyConversions(settings, currentRoth)`
  →
  `baseline.PerYearConversions = strategyYearlyConversions(settings, currentRoth, nil)`
- Any `*_test.go` callers found in Step 1: add a trailing `nil` argument.

- [ ] **Step 6: Build and run the full analysis suite (unchanged behavior)**

Run: `go build ./... && go test ./internal/services/retirement/analysis/`
Expected: builds clean; all tests pass (nil feedback ⇒ identical behavior).

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer.go
git commit -m "refactor(analysis): thread bracket-fill earnings feedback through sizing

Add a feedback map[int]float64 (proj-year -> Roth earnings) parameter to
strategyYearlyConversions, rothStrategyToConfig, and cloneSettingsWithSSAndRoth.
All call sites pass nil; behavior unchanged. Prepares the iteration in scoreCandidate.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Convergence helpers

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go` (append helpers + consts)
- Test: `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go`

- [ ] **Step 1: Write failing helper tests**

Append to `tax_optimizer_bracketfill_earnings_test.go`:

```go
func TestHarvestRothEarnings_WindowOnly(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{TaxableRothEarnings: 100}, // y0 — before window
			{TaxableRothEarnings: 0},   // y1
			{TaxableRothEarnings: 250}, // y2 — in window
			{TaxableRothEarnings: 400}, // y3 — in window
			{TaxableRothEarnings: 900}, // y4 — after window
		},
	}
	got := harvestRothEarnings(proj, 2, 4) // [2,4)
	if len(got) != 2 || got[2] != 250 || got[3] != 400 {
		t.Fatalf("expected {2:250,3:400}, got %v", got)
	}
}

func TestMaxAbsFeedbackDelta(t *testing.T) {
	a := map[int]float64{2: 250, 3: 400}
	b := map[int]float64{2: 250, 3: 380, 5: 30}
	if d := maxAbsFeedbackDelta(a, b); math.Abs(d-30) > 1e-9 {
		t.Fatalf("want 30 (the key-5 difference), got %v", d)
	}
	if d := maxAbsFeedbackDelta(nil, nil); d != 0 {
		t.Fatalf("nil vs nil should be 0, got %v", d)
	}
}

func TestRelaxFeedback_DampsTowardObserved(t *testing.T) {
	prev := map[int]float64{2: 0, 3: 100}
	observed := map[int]float64{2: 200, 3: 0}
	got := relaxFeedback(prev, observed, 0.5)
	if math.Abs(got[2]-100) > 1e-9 { // 0 + 0.5*(200-0)
		t.Fatalf("key2: want 100, got %v", got[2])
	}
	if math.Abs(got[3]-50) > 1e-9 { // 100 + 0.5*(0-100)
		t.Fatalf("key3: want 50, got %v", got[3])
	}
}
```

- [ ] **Step 2: Run the helper tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestHarvestRothEarnings_WindowOnly|TestMaxAbsFeedbackDelta|TestRelaxFeedback_DampsTowardObserved' -v`
Expected: build failure — `undefined: harvestRothEarnings` / `maxAbsFeedbackDelta` / `relaxFeedback`.

- [ ] **Step 3: Implement the helpers and tuning consts**

Append to `tax_optimizer_strategies.go` (after the bracket-fill functions). Note `math` is already imported in this package; if `go build` reports it missing in this file, add it.

```go
// Bracket-fill iterative-correction tuning. See
// docs/superpowers/specs/2026-05-31-bracketfill-roth-earnings-correction-design.md.
const (
	bracketFillMaxIterations     = 5    // engine re-runs per bracket-fill candidate
	bracketFillFeedbackTolerance = 25.0 // dollars; converged when max per-year residual is below this
	bracketFillFeedbackRelax     = 0.5  // damping factor toward the observed earnings
)

// bracketFillProjYearWindow returns the [start, end) projection-year offsets the
// strategy converts over (clamped to non-negative), mirroring the window logic
// in strategyYearlyConversions.
func bracketFillProjYearWindow(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) (int, int) {
	start := strat.StartAge - s.CurrentAge
	end := strat.EndAge - s.CurrentAge
	if start < 0 {
		start = 0
	}
	return start, end
}

// harvestRothEarnings extracts the engine's per-projection-year taxable Roth
// earnings within [startProjYear, endProjYear), keyed by projection-year offset.
// Only nonzero years are recorded.
func harvestRothEarnings(proj *models.ProjectionResult, startProjYear, endProjYear int) map[int]float64 {
	out := make(map[int]float64)
	if proj == nil {
		return out
	}
	for i, ys := range proj.YearlySummaries {
		if i < startProjYear || i >= endProjYear {
			continue
		}
		if ys.TaxableRothEarnings > 0 {
			out[i] = ys.TaxableRothEarnings
		}
	}
	return out
}

// maxAbsFeedbackDelta is the largest per-key absolute difference between two
// feedback maps (missing keys count as 0).
func maxAbsFeedbackDelta(a, b map[int]float64) float64 {
	max := 0.0
	for k, v := range a {
		if d := math.Abs(v - b[k]); d > max {
			max = d
		}
	}
	for k, v := range b {
		if _, ok := a[k]; ok {
			continue
		}
		if d := math.Abs(v); d > max {
			max = d
		}
	}
	return max
}

// relaxFeedback returns a damped update of prev toward observed:
// out[k] = prev[k] + alpha*(observed[k] - prev[k]) over the union of keys.
func relaxFeedback(prev, observed map[int]float64, alpha float64) map[int]float64 {
	out := make(map[int]float64)
	for k, v := range observed {
		out[k] = prev[k] + alpha*(v-prev[k])
	}
	for k, v := range prev {
		if _, ok := observed[k]; ok {
			continue
		}
		out[k] = v + alpha*(0-v)
	}
	return out
}
```

- [ ] **Step 4: Run the helper tests to verify they pass**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestHarvestRothEarnings_WindowOnly|TestMaxAbsFeedbackDelta|TestRelaxFeedback_DampsTowardObserved' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go
git commit -m "feat(analysis): add bracket-fill earnings convergence helpers

harvestRothEarnings, bracketFillProjYearWindow, maxAbsFeedbackDelta,
relaxFeedback, plus tuning consts for the iteration in scoreCandidate.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Iterate in `scoreCandidate` and disclose the converged conversions

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go` (`scoreCandidate` ~232-253)
- Test: `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go`

- [ ] **Step 1: Write the failing integration + regression tests**

First, add `"budget2/internal/services/retirement/engine"` and `"budget2/internal/services/retirement/prepare"` to the test file's import block (alongside the existing `math`, `testing`, and `budget2/internal/models` imports from Task 1). Then append to `tax_optimizer_bracketfill_earnings_test.go`:

```go
// adversarialOverlapSettings reproduces the measured corner: a small Roth drained
// past basis under 59½ DURING the conversion window, producing ~$23k of taxable
// Roth earnings at age 53 that the uncorrected sizer ignores.
func adversarialOverlapSettings(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1]
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 50)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 700_000
	s.TaxDeferredPercent = 78
	s.RothPercent = 12
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = []models.IncomeSource{{Type: models.IncomeFixed, Amount: 2500, StartMonth: 0}}
	s.MonthlyLivingExpenses = 8500
	s.TaxDeferredDelayYears = 6
	s.InvestmentReturn = 7
	s.ProjectionYears = 20
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}
	return s
}

// runWithOverrides prepares s, attaches per-year conversion overrides (prepare
// drops the json:"-" map, so re-attach as cloneSettingsWithSSAndRoth does), and
// runs the engine.
func runWithOverrides(t *testing.T, s *models.WhatIfSettings, overrides map[int]float64) *models.ProjectionResult {
	t.Helper()
	cfg := *s
	cfg.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: overrides}
	prepared := prepare.MustFrom(t, &cfg)
	if pset := prepared.Settings(); pset != nil {
		pset.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: overrides}
	}
	return engine.New().Run(engine.Input{Prepared: prepared})
}

func overridesFromConversions(ycs []models.YearlyConversion, currentAge int) map[int]float64 {
	out := make(map[int]float64, len(ycs))
	for _, yc := range ycs {
		out[yc.Age-currentAge] = yc.Amount
	}
	return out
}

// actualTaxableOrdinary reuses the solver's own definition of taxable ordinary
// income (incl. Roth earnings) to evaluate what the engine actually produced for
// a conversion year: ordinary (incl. engine earnings) + conversion + taxable SS −
// std deduction.
func actualTaxableOrdinary(ps *models.WhatIfSettings, projYear int, conversion, engineEarnings float64) float64 {
	return bracketFillIncomeForYear(ps, projYear, engineEarnings).taxableOrdinaryIncome(conversion)
}

const overshootYear = 3 // age 53 in adversarialOverlapSettings

// The uncorrected sizer overshoots the 12% ceiling in the overlap year; the
// iterative scoreCandidate eliminates it.
func TestScoreCandidate_EliminatesRothEarningsOvershoot(t *testing.T) {
	s := adversarialOverlapSettings(t)
	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyBracketFill, TargetBracket: 0.12, StartAge: 50, EndAge: 59,
	}
	ceiling, ok := inflatedBracketTopForYear(ps, 0.12, overshootYear)
	if !ok {
		t.Fatal("no inflated 12% ceiling")
	}

	// Uncorrected baseline (today's behavior): size with nil feedback, run engine.
	uncYCs := strategyYearlyConversions(ps, strat, nil)
	uncOverrides := overridesFromConversions(uncYCs, ps.CurrentAge)
	uncProj := runWithOverrides(t, s, uncOverrides)
	uncEarn := uncProj.YearlySummaries[overshootYear].TaxableRothEarnings
	uncActual := actualTaxableOrdinary(ps, overshootYear, uncOverrides[overshootYear], uncEarn)
	if uncActual <= ceiling+100 {
		t.Fatalf("expected uncorrected to overshoot the ceiling; actual=%.0f ceiling=%.0f (corner not reproduced)", uncActual, ceiling)
	}

	// Corrected: iterative scoreCandidate (SS primary claim 70 matches settings).
	cand := scoreCandidate(engine.New(), in, 70, 0, strat)
	corrOverrides := overridesFromConversions(cand.PerYearConversions, ps.CurrentAge)
	corrProj := runWithOverrides(t, s, corrOverrides)
	corrEarn := corrProj.YearlySummaries[overshootYear].TaxableRothEarnings
	corrActual := actualTaxableOrdinary(ps, overshootYear, corrOverrides[overshootYear], corrEarn)

	if corrActual > ceiling+bracketFillFeedbackTolerance {
		t.Fatalf("overshoot not eliminated: actual=%.0f ceiling=%.0f (uncorrected was %.0f)", corrActual, ceiling, uncActual)
	}
	// Sanity: the corrected conversion shrank in the overshoot year.
	if corrOverrides[overshootYear] >= uncOverrides[overshootYear] {
		t.Fatalf("corrected conversion should shrink: corr=%.0f unc=%.0f", corrOverrides[overshootYear], uncOverrides[overshootYear])
	}
}

// In a normal scenario (ample Roth, no earnings withdrawn in conversion years),
// the iterative path must produce IDENTICAL conversions to the uncorrected sizer.
func TestScoreCandidate_NoChangeWhenNoRothEarnings(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1]
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 55)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 70
	s.RothPercent = 25
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	s.MonthlyLivingExpenses = 6000
	s.TaxDeferredDelayYears = 0
	s.InvestmentReturn = 6
	s.ProjectionYears = 20
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()
	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyBracketFill, TargetBracket: 0.12, StartAge: 55, EndAge: 65,
	}

	want := strategyYearlyConversions(ps, strat, nil)
	cand := scoreCandidate(engine.New(), in, 70, 0, strat)
	if len(cand.PerYearConversions) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(cand.PerYearConversions), len(want))
	}
	for i := range want {
		if math.Abs(cand.PerYearConversions[i].Amount-want[i].Amount) > 0.01 {
			t.Fatalf("year %d differs: iterative=%.2f uncorrected=%.2f (should be identical with no Roth earnings)",
				want[i].Age, cand.PerYearConversions[i].Amount, want[i].Amount)
		}
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestScoreCandidate_EliminatesRothEarningsOvershoot|TestScoreCandidate_NoChangeWhenNoRothEarnings' -v`
Expected: `TestScoreCandidate_EliminatesRothEarningsOvershoot` FAILS at the final assertion ("overshoot not eliminated") because `scoreCandidate` still sizes once with nil feedback. `TestScoreCandidate_NoChangeWhenNoRothEarnings` PASSES already (no earnings ⇒ nothing to correct) — that is expected and confirms the regression guard.

- [ ] **Step 3: Rewrite `scoreCandidate` to iterate**

Replace the body of `scoreCandidate` (`tax_optimizer.go` ~232-253) with the iteration. Keep the signature unchanged.

```go
func scoreCandidate(eng *engine.Engine, in engine.Input, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
	settings := in.Prepared.Settings()

	fail := func() models.TaxOptimizerCandidate {
		return models.TaxOptimizerCandidate{
			PrimaryClaimAge:     primaryClaim,
			SpouseClaimAge:      spouseClaim,
			RothStrategy:        strat,
			EndingPortfolioReal: -math.MaxFloat64,
		}
	}

	maxIter := 1
	startProjYear, endProjYear := 0, 0
	if strat.Kind == models.RothStrategyBracketFill {
		maxIter = bracketFillMaxIterations
		startProjYear, endProjYear = bracketFillProjYearWindow(settings, strat)
	}

	var feedback map[int]float64 // pass 0: nil == today's behavior
	var scoredProj *models.ProjectionResult
	var usedFeedback map[int]float64
	bestResidual := math.MaxFloat64

	for iter := 0; iter < maxIter; iter++ {
		cloned, ok := cloneSettingsWithSSAndRoth(settings, primaryClaim, spouseClaim, strat, feedback)
		if !ok {
			return fail()
		}
		proj := eng.Run(engine.Input{Prepared: cloned, Chain: in.Chain, Hooks: in.Hooks})

		if strat.Kind != models.RothStrategyBracketFill {
			scoredProj, usedFeedback = proj, feedback
			break
		}

		observed := harvestRothEarnings(proj, startProjYear, endProjYear)
		residual := maxAbsFeedbackDelta(feedback, observed)
		// Keep the most self-consistent iterate (smallest residual = smallest
		// unaccounted overshoot) as the fallback if we never fully converge.
		if residual < bestResidual {
			bestResidual, scoredProj, usedFeedback = residual, proj, feedback
		}
		if residual < bracketFillFeedbackTolerance {
			break // converged: this conversion accounts for the engine's earnings
		}
		feedback = relaxFeedback(feedback, observed, bracketFillFeedbackRelax)
	}

	if scoredProj == nil {
		return fail()
	}

	cand := projectionToCandidate(scoredProj, primaryClaim, spouseClaim, strat)
	// Disclosure must mirror the engine input: size the displayed amounts with the
	// SAME converged feedback that produced the scored projection.
	cand.PerYearConversions = strategyYearlyConversions(
		candidateSettingsForSS(settings, primaryClaim, spouseClaim), strat, usedFeedback)
	return cand
}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestScoreCandidate_EliminatesRothEarningsOvershoot|TestScoreCandidate_NoChangeWhenNoRothEarnings' -v`
Expected: both PASS. If `TestScoreCandidate_EliminatesRothEarningsOvershoot` still reports a residual overshoot, the iteration is not converging — adjust `bracketFillFeedbackRelax` (try 0.7 then 1.0) and/or raise `bracketFillMaxIterations` (try 8); these constants were explicitly left for TDD tuning. Do NOT loosen `bracketFillFeedbackTolerance` to force a pass.

- [ ] **Step 5: Run the whole analysis suite (no regressions)**

Run: `go test ./internal/services/retirement/analysis/`
Expected: ok — existing optimizer tests unaffected (common-case behavior is unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go
git commit -m "fix(analysis): iterate bracket-fill sizing to account for Roth earnings

scoreCandidate now sizes -> runs the engine -> folds observed TaxableRothEarnings
back into the solver -> re-sizes, to a self-consistent fixed point. Eliminates the
bracket overshoot when a small Roth is drained past basis under 59.5 during the
conversion window. Converges in one engine run (identical behavior) when no Roth
earnings are withdrawn in conversion years.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Update CLAUDE.md and final verification

**Files:**
- Modify: `CLAUDE.md` (Roth bracket-fill gotcha, ~lines 81-88)

- [ ] **Step 1: Extend the bracket-fill gotcha note**

In `CLAUDE.md`, at the end of the existing Roth bracket-fill bullet (after "Add any new taxable component to `bracketFillIncomeForYear` to match the engine."), append:

```
  Non-qualified Roth EARNINGS withdrawals (`TaxableRothEarnings`) are also
  ordinary income, but they depend on the conversions themselves (a conversion
  adds Roth basis that Pub 590-B basis-first ordering consumes before earnings).
  So `scoreCandidate` sizes iteratively: size → run engine → fold the per-year
  `TaxableRothEarnings` back into `bracketFillIncomeForYear` via the `feedback`
  map → re-size to convergence. This only changes sizing in the corner where a
  small Roth is drained past basis under 59½ during the conversion window; with
  no such earnings it converges in one engine run (behavior unchanged).
```

- [ ] **Step 2: Full verification suite (CLAUDE.md mandate)**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all green.

- [ ] **Step 3: Confirm the diff scope**

Run: `git diff --stat master`
Expected: only `internal/services/retirement/analysis/tax_optimizer.go`, `internal/services/retirement/analysis/tax_optimizer_strategies.go`, `internal/services/retirement/analysis/tax_optimizer_bracketfill_earnings_test.go`, `CLAUDE.md`, and the two `docs/superpowers/` files. No engine files touched.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude.md): note iterative bracket-fill Roth-earnings correction

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review notes (for the implementer)

- **Spec coverage:** Task 1 = solver term; Tasks 1–2 = threading; Task 3 = convergence helpers + safety (min-residual fallback, tolerance, max-iter, damping); Task 4 = the loop + disclosure consistency + both the corner and common-case tests; Task 5 = CLAUDE.md + verification. The "no engine changes" and "byte-identical common case" properties are enforced by `TestScoreCandidate_NoChangeWhenNoRothEarnings` and the Step-3 diff check.
- **Type consistency:** the feedback map is `map[int]float64` everywhere; helper names are exactly `harvestRothEarnings`, `bracketFillProjYearWindow`, `maxAbsFeedbackDelta`, `relaxFeedback`; consts are `bracketFillMaxIterations`, `bracketFillFeedbackTolerance`, `bracketFillFeedbackRelax`.
- **If `incomingCalls` surfaces an unexpected caller** of any modified function (especially outside `analysis`), stop and report before proceeding — the blast radius assumption would be wrong.
```
