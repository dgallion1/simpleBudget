# Tax Optimizer Conversion Amounts View — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the per-year Roth conversion amounts the Tax Optimizer computed for each Top-5 row, so users can see (and translate to the scenario form) the exact dollar plan implied by a recommended strategy.

**Architecture:** Add `YearlyConversion` model type + `PerYearConversions` slice on `TaxOptimizerCandidate`. Factor the existing per-year math out of `rothStrategyToConfig` into a shared helper `strategyYearlyConversions`. Populate the slice at scoring time (`scoreCandidate`, and baseline construction). Render an inline `<details>` disclosure per row in the optimizer panel, with a `conversionSummary` template helper formatting Avg/Min/Max/Total.

**Tech Stack:** Go 1.x, html/template, HTMX, Tailwind (existing patterns in the repo).

**Spec:** `docs/superpowers/specs/2026-05-12-tax-optimizer-conversion-amounts-design.md`

---

## File Map

- **Modify** `internal/models/whatif.go` — add `YearlyConversion` type, extend `TaxOptimizerCandidate` with `PerYearConversions`.
- **Modify** `internal/services/retirement/analysis/tax_optimizer_strategies.go` — add `strategyYearlyConversions`; refactor `rothStrategyToConfig` to use it.
- **Modify** `internal/services/retirement/analysis/tax_optimizer.go` — attach `PerYearConversions` in `scoreCandidate` and on the baseline in `TaxOptimizerWithSeed`.
- **Modify** `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` — helper unit tests + drift-guard.
- **Modify** `internal/services/retirement/analysis/tax_optimizer_test.go` — `scoreCandidate` + baseline integration tests.
- **Modify** `internal/templates/render.go` — register `conversionSummary` helper (import `budget2/internal/models`).
- **Modify** `internal/templates/render_helpers_test.go` — unit tests for `conversionSummary` + funcmap presence.
- **Modify** `web/templates/components/whatif/tax-optimizer.html` — render disclosure sub-row per strategy row.
- **Modify** `internal/handlers/whatif/handlers_test.go` — extend `TestTaxOptimizerPanel_EligibleRendersStrategyAndTable` with disclosure assertions.

---

## Pre-Flight (do these once before starting Task 1)

- [ ] **Run gitnexus impact analysis on the symbols you'll edit.** Per CLAUDE.md, before any edit. From the repo root:

```bash
# These are MCP tool calls — if the agent platform exposes gitnexus_impact directly,
# call it; otherwise the CLI equivalent prints to stdout.
# Expected risk per spec: low/additive for all four.
```

  Targets to check: `TaxOptimizerCandidate`, `scoreCandidate`, `rothStrategyToConfig`, `getFuncMap`. If any returns HIGH or CRITICAL risk, **STOP** and flag it before proceeding.

- [ ] **Confirm the repo builds and tests pass on the current branch.**

```bash
go build ./...
go test ./internal/models/... ./internal/services/retirement/... ./internal/templates/... ./internal/handlers/whatif/...
```

  Expected: PASS. If anything is already red, do not proceed — flag the failure first.

---

## Task 1: Add `YearlyConversion` model + `strategyYearlyConversions` helper

**Files:**
- Modify: `internal/models/whatif.go` (insert new type near line 1316, just before `TaxOptimizerCandidate`)
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go` (add helper near the existing `rothStrategyToConfig` at line 321)
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` (add tests at end of file)

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`:

```go
func TestStrategyYearlyConversions_Ladder(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 35,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 75_000,
		StartAge:     67,
		EndAge:       72, // exclusive — 5 years (67..71)
	}
	got := strategyYearlyConversions(s, strat)

	if len(got) != 5 {
		t.Fatalf("len: got %d, want 5", len(got))
	}
	for i, yc := range got {
		wantAge := 67 + i
		if yc.Age != wantAge {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, wantAge)
		}
		if yc.Amount != 75_000 {
			t.Errorf("entry %d: Amount=%v, want 75000", i, yc.Amount)
		}
	}
}

func TestStrategyYearlyConversions_BracketFill_MFJ(t *testing.T) {
	s := eligibleBase()
	s.CurrentAge = 60
	s.ProjectionYears = 35
	// Push SS out so the estimator sees "no SS yet" for early years.
	s.SocialSecurity.ClaimAge = 67
	s.SocialSecurity.SpouseClaimAge = 67
	s.SpouseAge = 58
	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.24,
		StartAge:      60,
		EndAge:        63, // 3 years: 60, 61, 62
	}

	got := strategyYearlyConversions(s, strat)
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}

	ceiling, _ := bracketTopFor(models.FilingMarriedJoint, 0.24)
	for i, yc := range got {
		projYear := i // startProjYear=0 because StartAge==CurrentAge
		other := estimateOtherTaxableIncome(s, projYear)
		want := ceiling - other
		if want < 0 {
			want = 0
		}
		if yc.Amount != want {
			t.Errorf("year %d (age %d): Amount=%v, want %v", projYear, yc.Age, yc.Amount, want)
		}
	}
}

func TestStrategyYearlyConversions_NoConversion(t *testing.T) {
	s := eligibleBase()
	got := strategyYearlyConversions(s, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestStrategyYearlyConversions_ZeroAmountLadder(t *testing.T) {
	s := eligibleBase()
	got := strategyYearlyConversions(s, models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 0,
		StartAge:     67,
		EndAge:       72,
	})
	if got != nil {
		t.Errorf("expected nil for zero-amount ladder, got %v", got)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

```bash
go test ./internal/services/retirement/analysis/ -run TestStrategyYearlyConversions -v
```

Expected: compile error (`undefined: strategyYearlyConversions` and `undefined: models.YearlyConversion` if you reference it). If the helper compiles but produces wrong results, that's fine too — the next steps will fix it.

- [ ] **Step 3: Add the `YearlyConversion` type to `internal/models/whatif.go`**

Insert immediately above the `TaxOptimizerCandidate` struct (currently at line 1317):

```go
// YearlyConversion is one year's planned Roth conversion as part of an
// optimizer strategy. Age is the primary's age in that year; Amount is
// the dollar amount converted in that year, in nominal dollars.
type YearlyConversion struct {
	Age    int     `json:"age"`
	Amount float64 `json:"amount"`
}
```

- [ ] **Step 4: Extend `TaxOptimizerCandidate` with the new field**

Modify the struct (currently lines 1319-1336 in `internal/models/whatif.go`) to add `PerYearConversions` as the final field, just before the closing brace:

```go
	// PerYearConversions is the year-by-year conversion plan implied by
	// RothStrategy. Empty when the strategy is the no-conversion baseline.
	// Ladder strategies produce uniform Amount across the window;
	// bracket-fill strategies size each year to (bracket ceiling − other
	// estimated taxable income for that year).
	PerYearConversions []YearlyConversion `json:"per_year_conversions,omitempty"`
```

- [ ] **Step 5: Implement `strategyYearlyConversions`**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies.go` (after `rothStrategyToConfig`):

```go
// strategyYearlyConversions returns the per-year conversion amounts
// implied by strat. Returns nil for the no-conversion baseline (none-kind
// or a zero-amount ladder). For ladder strategies every entry shares
// strat.AnnualAmount. For bracket-fill strategies each entry equals
// the bracket ceiling minus estimateOtherTaxableIncome for that year
// (clamped to zero). Mirrors the math in rothStrategyToConfig so the
// displayed amounts match what the engine actually applied.
func strategyYearlyConversions(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) []models.YearlyConversion {
	if strat.Kind == models.RothStrategyNone {
		return nil
	}
	if strat.Kind == models.RothStrategyLadder && strat.AnnualAmount == 0 {
		return nil
	}

	startProjYear := strat.StartAge - s.CurrentAge
	endProjYear := strat.EndAge - s.CurrentAge
	if startProjYear < 0 {
		startProjYear = 0
	}
	if endProjYear <= startProjYear {
		return nil
	}

	out := make([]models.YearlyConversion, 0, endProjYear-startProjYear)

	switch strat.Kind {
	case models.RothStrategyLadder:
		for y := startProjYear; y < endProjYear; y++ {
			out = append(out, models.YearlyConversion{
				Age:    s.CurrentAge + y,
				Amount: strat.AnnualAmount,
			})
		}
	case models.RothStrategyBracketFill:
		if s.TaxConfig == nil {
			return nil
		}
		ceiling, ok := bracketTopFor(s.TaxConfig.FilingStatus, strat.TargetBracket)
		if !ok {
			return nil
		}
		for y := startProjYear; y < endProjYear; y++ {
			other := estimateOtherTaxableIncome(s, y)
			conv := ceiling - other
			if conv < 0 {
				conv = 0
			}
			out = append(out, models.YearlyConversion{
				Age:    s.CurrentAge + y,
				Amount: conv,
			})
		}
	default:
		return nil
	}
	return out
}
```

- [ ] **Step 6: Run the tests and verify they pass**

```bash
go test ./internal/services/retirement/analysis/ -run TestStrategyYearlyConversions -v
```

Expected: all four tests PASS.

- [ ] **Step 7: Run the full analysis package to confirm no regressions**

```bash
go test ./internal/services/retirement/analysis/
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/models/whatif.go \
        internal/services/retirement/analysis/tax_optimizer_strategies.go \
        internal/services/retirement/analysis/tax_optimizer_strategies_test.go
git commit -m "$(cat <<'EOF'
feat(taxopt): add strategyYearlyConversions helper + YearlyConversion model

Adds the per-year conversion plan as data on TaxOptimizerCandidate so
the UI can later expose the dollar amounts implied by each recommended
strategy. Helper mirrors the math already in rothStrategyToConfig; the
refactor that unifies the two follows in a separate commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Refactor `rothStrategyToConfig` to use the helper

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies.go` (refactor `rothStrategyToConfig`, currently lines 321-364)
- Modify: `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` (add drift-guard test)

- [ ] **Step 1: Write the drift-guard failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_strategies_test.go`:

```go
func TestRothStrategyToConfig_MatchesYearlyConversions(t *testing.T) {
	// Drift-guard: rothStrategyToConfig and strategyYearlyConversions
	// must produce identical per-year amounts. This locks the shared-
	// math invariant — if either path diverges, this test fails.
	s := eligibleBase()
	s.CurrentAge = 60
	s.ProjectionYears = 35
	s.SocialSecurity.ClaimAge = 67
	s.SocialSecurity.SpouseClaimAge = 67
	s.SpouseAge = 58

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.24,
		StartAge:      60,
		EndAge:        67,
	}

	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil || cfg.PerYearOverrides == nil {
		t.Fatal("expected non-nil PerYearOverrides for bracket-fill")
	}

	got := strategyYearlyConversions(s, strat)
	if len(got) == 0 {
		t.Fatal("expected non-empty YearlyConversions")
	}

	for _, yc := range got {
		projYear := yc.Age - s.CurrentAge
		want, ok := cfg.PerYearOverrides[projYear]
		if !ok {
			t.Errorf("projYear %d (age %d) missing from PerYearOverrides", projYear, yc.Age)
			continue
		}
		if yc.Amount != want {
			t.Errorf("projYear %d (age %d): YearlyConversion=%v, PerYearOverrides=%v",
				projYear, yc.Age, yc.Amount, want)
		}
	}

	// Symmetric check: every override key must have a YearlyConversion.
	if got, want := len(got), len(cfg.PerYearOverrides); got != want {
		t.Errorf("entry count mismatch: YearlyConversions=%d, PerYearOverrides=%d", got, want)
	}
}
```

- [ ] **Step 2: Run the test and verify it PASSES**

```bash
go test ./internal/services/retirement/analysis/ -run TestRothStrategyToConfig_MatchesYearlyConversions -v
```

Expected: PASS. Both code paths use the same math today (just copy-pasted), so the test should already pass before refactoring. Its real value is locking the invariant for the refactor that follows.

- [ ] **Step 3: Refactor `rothStrategyToConfig` to call the helper**

Replace the existing function body in `internal/services/retirement/analysis/tax_optimizer_strategies.go` (currently lines 321-364) with:

```go
func rothStrategyToConfig(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) *models.RothConversionConfig {
	if strat.Kind == models.RothStrategyNone {
		return &models.RothConversionConfig{Enabled: false}
	}
	if strat.Kind == models.RothStrategyLadder && strat.AnnualAmount == 0 {
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
		EndYear:   endProjYear - 1, // inclusive end-year semantics
	}

	switch strat.Kind {
	case models.RothStrategyLadder:
		cfg.AnnualAmount = strat.AnnualAmount
	case models.RothStrategyBracketFill:
		yearly := strategyYearlyConversions(s, strat)
		if yearly == nil {
			return &models.RothConversionConfig{Enabled: false}
		}
		overrides := make(map[int]float64, len(yearly))
		for _, yc := range yearly {
			overrides[yc.Age-s.CurrentAge] = yc.Amount
		}
		cfg.PerYearOverrides = overrides
	}
	return cfg
}
```

- [ ] **Step 4: Run drift-guard + existing tests**

```bash
go test ./internal/services/retirement/analysis/ -run "TestRothStrategyToConfig|TestStrategyYearlyConversions|TestCloneSettingsWithSSAndRoth" -v
```

Expected: all PASS, including pre-existing `TestCloneSettingsWithSSAndRoth_PreservesBracketFillOverrides` (proves the refactor preserves runtime behavior).

- [ ] **Step 5: Run the full analysis package**

```bash
go test ./internal/services/retirement/analysis/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer_strategies.go \
        internal/services/retirement/analysis/tax_optimizer_strategies_test.go
git commit -m "$(cat <<'EOF'
refactor(taxopt): unify per-year conversion math through strategyYearlyConversions

rothStrategyToConfig now delegates the bracket-fill per-year loop to
strategyYearlyConversions, so the UI-facing conversion plan and the
engine-applied overrides always agree. Drift-guard test locks the
invariant.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Populate `PerYearConversions` in `scoreCandidate`

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go` (extend `scoreCandidate` at lines 217-230)
- Modify: `internal/services/retirement/analysis/tax_optimizer_test.go` (add new test)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_test.go` (after `TestScoreCandidate_PopulatesFields`, currently ending ~line 230):

```go
func TestScoreCandidate_PopulatesPerYearConversions(t *testing.T) {
	// NOTE on age handling: cloneSettingsWithSSAndRoth → perturbAndPrepare →
	// prepare.From runs ComputeAges, which DERIVES CurrentAge from
	// StartDate + the primary Person's BirthMonth. This silently overrides
	// any in-memory s.CurrentAge mutation. Read the post-prep CurrentAge
	// from prep.Settings() instead of relying on eligibleBase()'s value.
	s := eligibleBase()

	prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
	})
	if !ok {
		t.Fatal("clone failed")
	}
	currentAge := prep.Settings().CurrentAge
	if currentAge <= 0 {
		t.Fatalf("prepared CurrentAge unset: %d", currentAge)
	}

	// 5-year ladder window starting at the post-prep CurrentAge so no
	// startProjYear clamping occurs.
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 60_000,
		StartAge:     currentAge,
		EndAge:       currentAge + 5, // exclusive — 5 entries
	}

	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 67, 62, strat)

	if len(cand.PerYearConversions) != 5 {
		t.Fatalf("PerYearConversions: got %d entries, want 5", len(cand.PerYearConversions))
	}
	for i, yc := range cand.PerYearConversions {
		if want := currentAge + i; yc.Age != want {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, want)
		}
		if yc.Amount != 60_000 {
			t.Errorf("entry %d: Amount=%v, want 60000", i, yc.Amount)
		}
	}
}

func TestScoreCandidate_NoneStrategyHasEmptyPerYearConversions(t *testing.T) {
	s := eligibleBase()
	prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})
	if !ok {
		t.Fatal("clone failed")
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 67, 62, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})

	if cand.PerYearConversions != nil {
		t.Errorf("expected nil PerYearConversions for none strategy, got %v", cand.PerYearConversions)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

```bash
go test ./internal/services/retirement/analysis/ -run "TestScoreCandidate_PopulatesPerYearConversions|TestScoreCandidate_NoneStrategyHasEmptyPerYearConversions" -v
```

Expected: `TestScoreCandidate_PopulatesPerYearConversions` FAILS (`PerYearConversions: got 0 entries, want 5`). The "none" test passes coincidentally (zero-value slice is nil).

- [ ] **Step 3: Modify `scoreCandidate` to populate the field**

In `internal/services/retirement/analysis/tax_optimizer.go`, replace `scoreCandidate` (currently lines 217-230) with:

```go
func scoreCandidate(eng *engine.Engine, in engine.Input, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
	settings := in.Prepared.Settings()
	cloned, ok := cloneSettingsWithSSAndRoth(settings, primaryClaim, spouseClaim, strat)
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
	cand := projectionToCandidate(proj, primaryClaim, spouseClaim, strat)
	cand.PerYearConversions = strategyYearlyConversions(settings, strat)
	return cand
}
```

Note: `settings` (the original input settings) is passed to `strategyYearlyConversions`, matching the existing flow in `cloneSettingsWithSSAndRoth → rothStrategyToConfig` which also receives the original `s`. This keeps the displayed amounts identical to what the engine applied.

- [ ] **Step 4: Run the tests and verify they pass**

```bash
go test ./internal/services/retirement/analysis/ -run "TestScoreCandidate" -v
```

Expected: PASS.

- [ ] **Step 5: Run the full analysis package**

```bash
go test ./internal/services/retirement/analysis/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go \
        internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(taxopt): attach PerYearConversions to scored candidates

Every candidate returned by scoreCandidate now carries the per-year
conversion plan implied by its RothStrategy, ready for the UI to
display alongside the row.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Populate baseline `PerYearConversions` in `TaxOptimizerWithSeed`

**Files:**
- Modify: `internal/services/retirement/analysis/tax_optimizer.go` (extend `TaxOptimizerWithSeed` near line 289-290)
- Modify: `internal/services/retirement/analysis/tax_optimizer_test.go` (add new test)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/tax_optimizer_test.go`:

```go
func TestTaxOptimizerWithSeed_PopulatesBaselinePerYearConversions(t *testing.T) {
	// When the user's saved scenario has an enabled Roth conversion,
	// the baseline candidate must surface the per-year amounts so the
	// UI's "Show conversion amounts" disclosure renders for the
	// baseline row when (if) we later expose it.
	//
	// NOTE: prepare.From → ComputeAges derives CurrentAge from
	// StartDate + BirthMonth, overriding in-memory mutations. Read the
	// post-prep CurrentAge from prep.Settings() for assertions.
	s := eligibleBase()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 40_000,
		StartYear:    0,
		EndYear:      4, // inclusive — 5 years
	}

	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()
	preparedCurrentAge := prep.Settings().CurrentAge

	result := TaxOptimizerWithSeed(eng, in, nil, 12345)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible result, got %+v", result)
	}
	if len(result.Baseline.PerYearConversions) != 5 {
		t.Fatalf("Baseline.PerYearConversions: got %d entries, want 5",
			len(result.Baseline.PerYearConversions))
	}
	for i, yc := range result.Baseline.PerYearConversions {
		if yc.Amount != 40_000 {
			t.Errorf("entry %d: Amount=%v, want 40000", i, yc.Amount)
		}
		if want := preparedCurrentAge + i; yc.Age != want {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, want)
		}
	}
}

func TestTaxOptimizerWithSeed_BaselineEmptyForDisabledRoth(t *testing.T) {
	s := eligibleBase()
	s.RothConversion = &models.RothConversionConfig{Enabled: false}

	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 12345)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible result, got %+v", result)
	}
	if result.Baseline.PerYearConversions != nil {
		t.Errorf("expected nil baseline PerYearConversions for disabled Roth, got %v",
			result.Baseline.PerYearConversions)
	}
}
```

- [ ] **Step 2: Run the tests and verify the populated case fails**

```bash
go test ./internal/services/retirement/analysis/ -run "TestTaxOptimizerWithSeed_PopulatesBaselinePerYearConversions|TestTaxOptimizerWithSeed_BaselineEmptyForDisabledRoth" -v
```

Expected: `TestTaxOptimizerWithSeed_PopulatesBaselinePerYearConversions` FAILS. The disabled-Roth test passes coincidentally.

- [ ] **Step 3: Modify `TaxOptimizerWithSeed` to attach the baseline slice**

In `internal/services/retirement/analysis/tax_optimizer.go`, find the existing block (currently lines 289-290):

```go
	baselineProj := eng.Run(in)
	baseline := projectionToCandidate(baselineProj, currentPrimary, currentSpouse, currentRoth)
```

Add one line directly after it:

```go
	baseline.PerYearConversions = strategyYearlyConversions(settings, currentRoth)
```

- [ ] **Step 4: Run the tests and verify they pass**

```bash
go test ./internal/services/retirement/analysis/ -run "TestTaxOptimizerWithSeed_" -v
```

Expected: PASS.

- [ ] **Step 5: Run the full analysis package**

```bash
go test ./internal/services/retirement/analysis/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/analysis/tax_optimizer.go \
        internal/services/retirement/analysis/tax_optimizer_test.go
git commit -m "$(cat <<'EOF'
feat(taxopt): populate baseline PerYearConversions

The baseline candidate (user's saved scenario) now carries the per-year
conversion plan implied by its current Roth config, so any future UI
that exposes the baseline can render the same disclosure.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Add `conversionSummary` template helper

**Files:**
- Modify: `internal/templates/render.go` (add helper near `formatMoney` ~line 412, register in `getFuncMap` ~line 67)
- Modify: `internal/templates/render_helpers_test.go` (add tests, update funcmap test)

- [ ] **Step 1: Write the failing helper tests**

Append to `internal/templates/render_helpers_test.go` (after `TestFormatMoney`, before `TestFormatNumber`):

```go
func TestConversionSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []models.YearlyConversion
		want string
	}{
		{
			name: "empty returns empty string",
			in:   nil,
			want: "",
		},
		{
			name: "single entry, whole-dollar total",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 5_400},
			},
			want: "Avg $5,400  ·  Min $5,400  ·  Max $5,400  ·  Total $5,400 over 1 year",
		},
		{
			name: "multi-entry, K-abbreviated total",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 50_000},
				{Age: 68, Amount: 50_000},
				{Age: 69, Amount: 50_000},
			},
			want: "Avg $50,000  ·  Min $50,000  ·  Max $50,000  ·  Total $150K over 3 years",
		},
		{
			name: "multi-entry, M-abbreviated total with varying amounts",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 320_400},
				{Age: 68, Amount: 310_200},
				{Age: 69, Amount: 300_600},
				{Age: 70, Amount: 291_500},
				{Age: 71, Amount: 283_000},
				{Age: 72, Amount: 275_100},
			},
			// Sum = 1_780_800 → "$1.78M". Avg = 296_800.
			want: "Avg $296,800  ·  Min $275,100  ·  Max $320,400  ·  Total $1.78M over 6 years",
		},
		{
			name: "sub-$10K total uses whole-dollar formatting",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 2_500},
				{Age: 68, Amount: 4_000},
			},
			want: "Avg $3,250  ·  Min $2,500  ·  Max $4,000  ·  Total $6,500 over 2 years",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conversionSummary(tt.in)
			if got != tt.want {
				t.Errorf("conversionSummary:\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}
```

You will also need to add an import for `budget2/internal/models` at the top of the test file. Add it to the existing `import (...)` block.

- [ ] **Step 2: Run the tests and verify they fail**

```bash
go test ./internal/templates/ -run TestConversionSummary -v
```

Expected: compile error — `undefined: conversionSummary` and possibly `undefined: models.YearlyConversion` import in test.

- [ ] **Step 3: Add the `models` import to `internal/templates/render.go`**

Modify the existing `import (...)` block (currently lines 8-25 of `internal/templates/render.go`) to add:

```go
	"budget2/internal/models"
```

Place it alphabetically near the existing `"budget2/internal/services/retirement"` line.

- [ ] **Step 4: Implement `conversionSummary`**

Append to `internal/templates/render.go` (after `formatMoney`, around line 442):

```go
// conversionSummary formats a one-line Avg/Min/Max/Total summary for a
// slice of Roth conversion amounts. Avg/Min/Max use whole-dollar
// comma-separated formatting (no cents) because conversion amounts are
// inherently approximate at the dollar level. Total uses M-abbreviation
// for ≥ $1M (two decimals) and K-abbreviation for ≥ $10K (no decimals);
// smaller totals use whole-dollar formatting.
//
// Returns "" for an empty slice — the template gates the disclosure on
// the slice being non-empty, so this is a defensive belt-and-suspenders.
func conversionSummary(items []models.YearlyConversion) string {
	if len(items) == 0 {
		return ""
	}
	var total float64
	minA := items[0].Amount
	maxA := items[0].Amount
	for _, it := range items {
		total += it.Amount
		if it.Amount < minA {
			minA = it.Amount
		}
		if it.Amount > maxA {
			maxA = it.Amount
		}
	}
	avg := total / float64(len(items))

	yearWord := "years"
	if len(items) == 1 {
		yearWord = "year"
	}

	return fmt.Sprintf("Avg %s  ·  Min %s  ·  Max %s  ·  Total %s over %d %s",
		formatWholeDollars(avg),
		formatWholeDollars(minA),
		formatWholeDollars(maxA),
		formatAbbreviatedTotal(total),
		len(items),
		yearWord,
	)
}

// formatWholeDollars renders v as "$X,XXX" with no cents.
func formatWholeDollars(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	whole := int64(v + 0.5) // round half-up
	formatted := fmt.Sprintf("%d", whole)
	var result strings.Builder
	for i, c := range formatted {
		if i > 0 && (len(formatted)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	if negative {
		return "-$" + result.String()
	}
	return "$" + result.String()
}

// formatAbbreviatedTotal renders v as "$X.YYM" for ≥ $1M, "$XK" for
// ≥ $10K, otherwise "$X,XXX" (no cents). Used only by conversionSummary
// — keep it scoped tightly; do not promote without considering the
// existing formatMoney/formatNumber callers.
func formatAbbreviatedTotal(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	var s string
	switch {
	case v >= 1_000_000:
		s = fmt.Sprintf("$%.2fM", v/1_000_000)
	case v >= 10_000:
		s = fmt.Sprintf("$%dK", int64(v/1_000+0.5))
	default:
		s = formatWholeDollars(v)
		if negative {
			return s // already has leading "-$"
		}
		return s
	}
	if negative {
		return "-" + s
	}
	return s
}
```

- [ ] **Step 5: Register `conversionSummary` in `getFuncMap`**

In `internal/templates/render.go`, in `getFuncMap()` (around line 67-113), add the new key. Insert it right after the `formatMoney` line:

```go
		"conversionSummary":                   conversionSummary,
```

- [ ] **Step 6: Update the funcmap presence test**

In `internal/templates/render_helpers_test.go`, find `TestGetFuncMap` (around line 336) and add `"conversionSummary"` to the `expectedKeys` slice. The relevant slice currently reads:

```go
expectedKeys := []string{
    "formatMoney", "formatNumber", "formatPercent", "formatDate", "formatDateTime",
    ...
}
```

Insert `"conversionSummary"` near `"formatMoney"`:

```go
expectedKeys := []string{
    "formatMoney", "conversionSummary", "formatNumber", "formatPercent", "formatDate", "formatDateTime",
    ...
}
```

- [ ] **Step 7: Run the tests and verify they pass**

```bash
go test ./internal/templates/ -run "TestConversionSummary|TestGetFuncMap" -v
```

Expected: PASS.

- [ ] **Step 8: Run the full templates package**

```bash
go test ./internal/templates/
```

Expected: PASS. If `go build ./...` fails because the helper references `models.YearlyConversion` before Task 1 ran, that's a sign the task order was scrambled — Task 1 must be complete first.

- [ ] **Step 9: Commit**

```bash
git add internal/templates/render.go internal/templates/render_helpers_test.go
git commit -m "$(cat <<'EOF'
feat(templates): add conversionSummary helper for taxopt panel

Formats a one-line Avg/Min/Max/Total summary of a YearlyConversion
slice. Avg/Min/Max use whole-dollar comma formatting; Total uses
M-/K-abbreviation. Used by the upcoming Tax Optimizer per-row
disclosure.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Render the disclosure in `tax-optimizer.html` + handler test

**Files:**
- Modify: `web/templates/components/whatif/tax-optimizer.html` (insert sub-row inside `{{range}}` loop, currently lines 74-100)
- Modify: `internal/handlers/whatif/handlers_test.go` (extend `TestTaxOptimizerPanel_EligibleRendersStrategyAndTable` at line 8527)

- [ ] **Step 1: Extend the existing handler test to assert disclosure markup**

In `internal/handlers/whatif/handlers_test.go`, modify `TestTaxOptimizerPanel_EligibleRendersStrategyAndTable` (currently lines 8527-8594). Replace the `Top` slice and the assertion block with:

```go
			Top: []models.TaxOptimizerCandidate{
				{
					PrimaryClaimAge:     70,
					EndingPortfolioReal: 1_200_000,
					LifetimeTaxReal:     180_000,
					MCSurvivalRate:      92.5,
					RothStrategy: models.RothOptimizerStrategy{
						Kind:  models.RothStrategyLadder,
						Label: "$80k/yr to RMD age",
					},
					PerYearConversions: []models.YearlyConversion{
						{Age: 67, Amount: 80_000},
						{Age: 68, Amount: 80_000},
						{Age: 69, Amount: 80_000},
					},
				},
				{
					PrimaryClaimAge:     68,
					EndingPortfolioReal: 1_100_000,
					LifetimeTaxReal:     210_000,
					MCSurvivalRate:      89.0,
					RothStrategy: models.RothOptimizerStrategy{
						Kind:  models.RothStrategyBracketFill,
						Label: "Fill to 22% bracket",
					},
					PerYearConversions: []models.YearlyConversion{
						{Age: 67, Amount: 250_000},
						{Age: 68, Amount: 245_000},
					},
				},
			},
		},
	}

	out, err := renderer.RenderToString("whatif-tax-optimizer-results", map[string]interface{}{
		"Analysis": analysis,
		"Settings": analysis.Settings,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	for _, want := range []string{
		"Tax Optimizer",
		"$80k/yr to RMD age",
		"Fill to 22% bracket",
		"Show conversion amounts",  // disclosure summary text
		">67<",                     // an age cell from PerYearConversions
		"$80,000",                  // a ladder amount rendered via formatMoney
		"$250,000",                 // a bracket-fill amount
		"Avg ",                     // summary line opening
		"Total ",                   // summary line total marker
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered panel missing %q; body snippet: %s", want, out[:min(len(out), 800)])
		}
	}
}
```

- [ ] **Step 2: Run the handler test and verify it fails**

```bash
go test ./internal/handlers/whatif/ -run TestTaxOptimizerPanel_EligibleRendersStrategyAndTable -v
```

Expected: FAIL — the template doesn't yet render the disclosure, so substrings like `"Show conversion amounts"` and `">67<"` won't appear.

- [ ] **Step 3: Modify the template to render the disclosure**

In `web/templates/components/whatif/tax-optimizer.html`, find the strategy row inside the `{{range $i, $c := $to.Top}}` loop (currently lines 74-100). Just before the closing `{{end}}` of that range (currently line 100), insert a sibling `<tr>`:

```html
                {{if $c.PerYearConversions}}
                <tr class="border-b dark:border-gray-700 {{if eq $i 0}}bg-green-50 dark:bg-green-900/20{{end}}">
                    <td colspan="{{if gt $to.MonteCarloRuns 0}}7{{else}}5{{end}}" class="px-2 pb-2">
                        <details class="text-xs text-gray-600 dark:text-gray-300">
                            <summary class="cursor-pointer hover:text-indigo-600 dark:hover:text-indigo-400">
                                Show conversion amounts
                            </summary>
                            <div class="mt-2 pl-4">
                                <table class="text-xs">
                                    <thead>
                                        <tr>
                                            <th class="text-left pr-4 font-medium">Age</th>
                                            <th class="text-right font-medium">Conversion</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {{range $c.PerYearConversions}}
                                        <tr>
                                            <td class="pr-4">{{.Age}}</td>
                                            <td class="text-right">{{formatMoney .Amount}}</td>
                                        </tr>
                                        {{end}}
                                    </tbody>
                                </table>
                                <p class="mt-2 text-gray-500 dark:text-gray-400">
                                    {{conversionSummary $c.PerYearConversions}}
                                </p>
                            </div>
                        </details>
                    </td>
                </tr>
                {{end}}
```

The colspan is computed inline (`7` if MC ran, `5` otherwise) — matches the existing column count logic that the surrounding template already uses for the conditional `<th>`s.

- [ ] **Step 4: Run the handler test and verify it passes**

```bash
go test ./internal/handlers/whatif/ -run TestTaxOptimizerPanel_EligibleRendersStrategyAndTable -v
```

Expected: PASS.

- [ ] **Step 5: Run the full handler and template packages**

```bash
go test ./internal/handlers/whatif/ ./internal/templates/
```

Expected: PASS.

- [ ] **Step 6: Run the entire test suite to confirm no regressions**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Verify the rendered output by eyeballing it (optional, recommended for UI work)**

```bash
go build ./...
# Start the app per the project's usual command (e.g. `go run ./cmd/budget2` or whatever appears in README), then:
# 1. Open the whatif page in a browser
# 2. Run the Tax Optimizer if not already shown
# 3. For each Top-5 row, confirm the "Show conversion amounts" disclosure expands to a small table + summary line
# 4. Confirm the "No conversion" baseline row has NO disclosure
```

If the UI doesn't render as expected (e.g. layout breaks, colspan wrong on narrow viewport), capture the issue and address before committing.

- [ ] **Step 8: Run gitnexus_detect_changes (per CLAUDE.md)**

```bash
# Via MCP gitnexus tool: confirm changes affect only expected symbols/flows.
```

Expected scope: `TaxOptimizerCandidate`, `scoreCandidate`, `TaxOptimizerWithSeed`, `rothStrategyToConfig`, new `strategyYearlyConversions`, new `conversionSummary`, the optimizer panel template. If anything unexpected shows up, investigate before committing.

- [ ] **Step 9: Commit**

```bash
git add web/templates/components/whatif/tax-optimizer.html \
        internal/handlers/whatif/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(taxopt): per-row 'Show conversion amounts' disclosure

Each Top-5 row in the optimizer panel now exposes its per-year Roth
conversion plan via a native <details> disclosure: small age→amount
table plus an Avg/Min/Max/Total summary line. Lets users translate
a recommended strategy into the scenario form's fixed-amount field.

Closes the design in docs/superpowers/specs/2026-05-12-tax-optimizer-conversion-amounts-design.md

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Verification Summary

After Task 6 is committed, the feature should:

1. **Render an expandable disclosure on every non-baseline row** of the Tax Optimizer table — both ladder and bracket-fill strategies.
2. **Expand to a small two-column table** (Age | Conversion) showing each year's amount.
3. **Display a summary line** in the format `Avg $X · Min $Y · Max $Z · Total $T over N years`.
4. **Skip the disclosure** for the "No conversion" baseline row (because `PerYearConversions` is empty).
5. **Match the engine's actual converted amounts** by virtue of the drift-guard test locking `strategyYearlyConversions` to `rothStrategyToConfig`.
6. **Compile and pass `go test ./...`** with no regressions in other packages.

## Rollback

If the feature needs to be reverted, the commits land in a clean stack — `git revert` the most recent six commits in reverse order, or `git reset --hard <commit-before-task-1>` if no other work has landed since. The model field addition is backward-compatible with persisted scenarios (it's an in-memory analysis field, never persisted).
