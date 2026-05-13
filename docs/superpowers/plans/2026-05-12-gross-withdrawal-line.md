# Gross Withdrawal Line Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Gross Withdrawal Needed to Close Gap" mini-section to the WhatIf Monthly Budget Analysis panel showing per-source (Tax-Deferred, Taxable, Roth) gross withdrawal amounts when there is a shortfall.

**Architecture:** Extend `models.BudgetFitAnalysis` with ten new fields (five per section). Compute them inside `internal/services/retirement/analysis/budget_fit.go` by reusing the existing `estimateTaxSnapshot` closure to simulate an additional withdrawal and diff the resulting tax. Render three new rows in `web/templates/components/whatif/budget-analysis.html` under each section's "Required Rate" grid; rows are hidden entirely when the gap is ≤ 0.

**Tech Stack:** Go 1.21+, html/template, HTMX 2.0, Tailwind CSS. Tests use the project's `newTestCalc` helper and `renderer.RenderToString` pattern.

**Spec:** `docs/superpowers/specs/2026-05-12-gross-withdrawal-line-design.md`

---

## Task 0: Run gitnexus impact analysis

**Why:** CLAUDE.md mandates impact analysis before editing any symbol. The two symbols at risk are `BudgetFitAnalysis` (model struct) and `BudgetFit` (analysis function). Adding fields and computing new outputs is additive but the analyzer should confirm blast radius.

- [ ] **Step 1: Verify GitNexus index freshness**

Run:
```bash
npx gitnexus status
```
If stale, run `npx gitnexus analyze`.

- [ ] **Step 2: Impact analysis on BudgetFitAnalysis struct**

Use MCP tool:
```
gitnexus_impact({target: "BudgetFitAnalysis", direction: "upstream"})
```
Expected: callers are render code paths + JSON marshaling. Adding fields is backward-compatible.

- [ ] **Step 3: Impact analysis on BudgetFit function**

Use MCP tool:
```
gitnexus_impact({target: "BudgetFit", direction: "upstream"})
```
Expected: called by `Calculator.CalculateBudgetFit` and the WhatIf handler chain. New outputs do not change existing fields.

- [ ] **Step 4: Report findings**

If either impact analysis returns HIGH or CRITICAL risk, STOP and confirm with user before proceeding. Otherwise record blast radius in a one-line note and continue.

No commit at this step.

---

## Task 1: Add fields to BudgetFitAnalysis model

**Files:**
- Modify: `internal/models/whatif.go:870-911`

- [ ] **Step 1: Add Current-section fields**

In `internal/models/whatif.go`, insert these five fields immediately after `RequiredRate` (line 883) and before the `// Breakdowns for transparency` comment block:

```go
	// Gross withdrawal needed to net MonthlyGap by funding source.
	// All zero when MonthlyGap <= 0.
	GrossWithdrawalTaxDeferred float64 `json:"gross_withdrawal_tax_deferred,omitempty"`
	MarginalRateTaxDeferred    float64 `json:"marginal_rate_tax_deferred,omitempty"` // % 0-100
	GrossWithdrawalTaxable     float64 `json:"gross_withdrawal_taxable,omitempty"`
	EffectiveRateTaxable       float64 `json:"effective_rate_taxable,omitempty"` // % 0-100
	GrossWithdrawalRoth        float64 `json:"gross_withdrawal_roth,omitempty"`
```

- [ ] **Step 2: Add Steady-State mirrors**

Immediately after `SteadyStateRate` (line 909, just before `HasSteadyState`), insert:

```go
	// Gross withdrawal needed to net SteadyStateGap by funding source.
	// All zero when SteadyStateGap <= 0.
	SteadyStateGrossWithdrawalTaxDeferred float64 `json:"steady_state_gross_withdrawal_tax_deferred,omitempty"`
	SteadyStateMarginalRateTaxDeferred    float64 `json:"steady_state_marginal_rate_tax_deferred,omitempty"`
	SteadyStateGrossWithdrawalTaxable     float64 `json:"steady_state_gross_withdrawal_taxable,omitempty"`
	SteadyStateEffectiveRateTaxable       float64 `json:"steady_state_effective_rate_taxable,omitempty"`
	SteadyStateGrossWithdrawalRoth        float64 `json:"steady_state_gross_withdrawal_roth,omitempty"`
```

- [ ] **Step 3: Verify compile**

Run:
```bash
go build ./internal/models/...
```
Expected: no errors.

- [ ] **Step 4: Run existing model tests to confirm no regression**

Run:
```bash
go test ./internal/models/...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/models/whatif.go
git commit -m "feat(models): add gross-withdrawal fields to BudgetFitAnalysis"
```

---

## Task 2: TDD — zero gap leaves all gross-withdrawal fields at zero

**Files:**
- Modify: `internal/services/retirement/calculator_expense_test.go` (append a new sub-test inside the existing `TestCalculateBudgetFit` block)
- Modify: `internal/services/retirement/analysis/budget_fit.go`

- [ ] **Step 1: Write the failing test**

Append this sub-test inside `TestCalculateBudgetFit` (after the existing `"income covers expenses so no gap"` block — around line 372):

```go
	t.Run("gross withdrawal fields zero on surplus", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Social Security", Amount: 4000, StartMonth: 0},
		}

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap > 0 {
			t.Fatalf("precondition: expected surplus, got gap %.2f", fit.MonthlyGap)
		}
		if fit.GrossWithdrawalTaxDeferred != 0 {
			t.Errorf("GrossWithdrawalTaxDeferred: want 0 on surplus, got %.2f", fit.GrossWithdrawalTaxDeferred)
		}
		if fit.GrossWithdrawalTaxable != 0 {
			t.Errorf("GrossWithdrawalTaxable: want 0 on surplus, got %.2f", fit.GrossWithdrawalTaxable)
		}
		if fit.GrossWithdrawalRoth != 0 {
			t.Errorf("GrossWithdrawalRoth: want 0 on surplus, got %.2f", fit.GrossWithdrawalRoth)
		}
		if fit.MarginalRateTaxDeferred != 0 {
			t.Errorf("MarginalRateTaxDeferred: want 0 on surplus, got %.2f", fit.MarginalRateTaxDeferred)
		}
	})
```

- [ ] **Step 2: Run test to verify it passes immediately**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/gross_withdrawal_fields_zero_on_surplus -v
```
Expected: PASS — because fields default to 0 in Go and no code sets them yet. This is a regression guard for the Task 3+ logic.

- [ ] **Step 3: Commit**

```bash
git add internal/services/retirement/calculator_expense_test.go
git commit -m "test(retirement): zero-gap leaves gross-withdrawal fields at 0"
```

---

## Task 3: TDD — Roth gross withdrawal equals gap

**Files:**
- Modify: `internal/services/retirement/calculator_expense_test.go`
- Modify: `internal/services/retirement/analysis/budget_fit.go`

- [ ] **Step 1: Write the failing test**

Append inside `TestCalculateBudgetFit`:

```go
	t.Run("roth gross withdrawal equals gap", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 0
		s.RothPercent = 100
		s.SpendingPhaseConfig = nil

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if math.Abs(fit.GrossWithdrawalRoth-fit.MonthlyGap) > 0.01 {
			t.Errorf("GrossWithdrawalRoth: want %.2f (= gap), got %.2f", fit.MonthlyGap, fit.GrossWithdrawalRoth)
		}
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/roth_gross_withdrawal_equals_gap -v
```
Expected: FAIL — `GrossWithdrawalRoth: want ~5000, got 0.00`.

- [ ] **Step 3: Implement minimal logic**

In `internal/services/retirement/analysis/budget_fit.go`, locate the block where `result` is initialized (around line 189). Immediately after that initialization (before `if currentSnapshot.MonthlyIRMAA > 0` on line 209), add:

```go
	if monthlyGap > 0 {
		result.GrossWithdrawalRoth = monthlyGap
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/roth_gross_withdrawal_equals_gap -v
```
Expected: PASS.

- [ ] **Step 5: Run full BudgetFit suite for regression**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit -v
```
Expected: all sub-tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator_expense_test.go internal/services/retirement/analysis/budget_fit.go
git commit -m "feat(analysis): Roth gross withdrawal equals MonthlyGap"
```

---

## Task 4: TDD — Tax-deferred gross withdrawal grosses up by marginal rate (current section)

**Files:**
- Modify: `internal/services/retirement/calculator_expense_test.go`
- Modify: `internal/services/retirement/analysis/budget_fit.go`

- [ ] **Step 1: Write the failing test**

Append inside `TestCalculateBudgetFit`:

```go
	t.Run("tax-deferred gross withdrawal grosses up by marginal rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		// Some baseline ordinary income so simulated extra withdrawal lands
		// in the 22% federal bracket (single filer, ~$50k-$100k AGI).
		s.IncomeSources = []models.IncomeSource{
			{ID: "pension", Name: "Pension", Amount: 4000, StartMonth: 0},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 100
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		if s.TaxConfig == nil {
			s.TaxConfig = models.DefaultTaxConfig()
		}
		s.TaxConfig.FilingStatus = models.FilingSingle

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if fit.MarginalRateTaxDeferred <= 10 || fit.MarginalRateTaxDeferred >= 35 {
			t.Errorf("MarginalRateTaxDeferred: want between 10%% and 35%%, got %.2f", fit.MarginalRateTaxDeferred)
		}
		expectedGross := fit.MonthlyGap / (1 - fit.MarginalRateTaxDeferred/100)
		if math.Abs(fit.GrossWithdrawalTaxDeferred-expectedGross) > 0.50 {
			t.Errorf("GrossWithdrawalTaxDeferred: want %.2f (=gap/(1-rate)), got %.2f",
				expectedGross, fit.GrossWithdrawalTaxDeferred)
		}
		if fit.GrossWithdrawalTaxDeferred <= fit.MonthlyGap {
			t.Errorf("GrossWithdrawalTaxDeferred (%.2f) must exceed gap (%.2f)",
				fit.GrossWithdrawalTaxDeferred, fit.MonthlyGap)
		}
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/tax-deferred_gross_withdrawal_grosses_up_by_marginal_rate -v
```
Expected: FAIL — `MarginalRateTaxDeferred: want between 10% and 35%, got 0.00`.

- [ ] **Step 3: Implement tax-deferred gross-up**

In `internal/services/retirement/analysis/budget_fit.go`, expand the Roth-only block from Task 3 to include tax-deferred simulation. Replace:

```go
	if monthlyGap > 0 {
		result.GrossWithdrawalRoth = monthlyGap
	}
```

with:

```go
	if monthlyGap > 0 {
		result.GrossWithdrawalRoth = monthlyGap

		// Tax-deferred: simulate adding the gap to RMD (ordinary-income withdrawal).
		tdSnap := estimateTaxSnapshot(0, taxableCashFlow, monthlyRMD+monthlyGap, rothConversionThisMonth, &currentIRMALookbackMAGI)
		extraTax := tdSnap.MonthlyTax - currentSnapshot.MonthlyTax
		marginal := extraTax / monthlyGap
		if marginal < 0 {
			marginal = 0
		}
		if marginal > 0.95 {
			marginal = 0.95
		}
		result.MarginalRateTaxDeferred = marginal * 100
		result.GrossWithdrawalTaxDeferred = monthlyGap / (1 - marginal)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/tax-deferred_gross_withdrawal_grosses_up_by_marginal_rate -v
```
Expected: PASS.

- [ ] **Step 5: Run full BudgetFit suite for regression**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit -v
```
Expected: all sub-tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator_expense_test.go internal/services/retirement/analysis/budget_fit.go
git commit -m "feat(analysis): tax-deferred gross withdrawal via marginal-rate simulation"
```

---

## Task 5: TDD — Taxable gross withdrawal at year 0 ≈ gap (basis = market)

**Files:**
- Modify: `internal/services/retirement/calculator_expense_test.go`
- Modify: `internal/services/retirement/analysis/budget_fit.go`

- [ ] **Step 1: Write the failing test**

Append inside `TestCalculateBudgetFit`:

```go
	t.Run("taxable gross withdrawal equals gap at year zero", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		// At month 0, cost basis equals market value → gain fraction ~0 → gross ≈ gap.
		if math.Abs(fit.GrossWithdrawalTaxable-fit.MonthlyGap) > 0.50 {
			t.Errorf("GrossWithdrawalTaxable at year 0: want ~%.2f (= gap, basis = market), got %.2f",
				fit.MonthlyGap, fit.GrossWithdrawalTaxable)
		}
		if fit.EffectiveRateTaxable > 1.0 {
			t.Errorf("EffectiveRateTaxable at year 0: want ~0, got %.2f", fit.EffectiveRateTaxable)
		}
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/taxable_gross_withdrawal_equals_gap_at_year_zero -v
```
Expected: FAIL — `GrossWithdrawalTaxable at year 0: want ~5000, got 0.00`.

- [ ] **Step 3: Implement taxable gross-up for current section**

In the same `if monthlyGap > 0` block in `budget_fit.go` (added in Task 4), append the taxable computation after the tax-deferred block:

```go
		// Taxable: at month 0 cost basis ≈ market value, so simulated gain fraction is 0.
		// Compute via simulation for consistency with steady-state path; result will be ≈ gap.
		taxableExtra := taxableCashFlow
		taxableGainFractionCurrent := 0.0 // basis = market at month 0
		taxableExtra.CapitalGainsDistributions += monthlyGap * taxableGainFractionCurrent
		txSnap := estimateTaxSnapshot(0, taxableExtra, monthlyRMD, rothConversionThisMonth, &currentIRMALookbackMAGI)
		txExtraTax := txSnap.MonthlyTax - currentSnapshot.MonthlyTax
		txEffective := txExtraTax / monthlyGap
		if txEffective < 0 {
			txEffective = 0
		}
		if txEffective > 0.95 {
			txEffective = 0.95
		}
		result.EffectiveRateTaxable = txEffective * 100
		result.GrossWithdrawalTaxable = monthlyGap / (1 - txEffective)
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/taxable_gross_withdrawal_equals_gap_at_year_zero -v
```
Expected: PASS.

- [ ] **Step 5: Run full BudgetFit suite**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit -v
```
Expected: all sub-tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator_expense_test.go internal/services/retirement/analysis/budget_fit.go
git commit -m "feat(analysis): taxable gross withdrawal at year 0 (basis = market)"
```

---

## Task 6: TDD — Steady-state mirrors compute correctly at year 20

**Files:**
- Modify: `internal/services/retirement/calculator_expense_test.go`
- Modify: `internal/services/retirement/analysis/budget_fit.go`

- [ ] **Step 1: Write the failing test**

Append inside `TestCalculateBudgetFit`:

```go
	t.Run("steady-state gross withdrawal mirrors compute", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = []models.IncomeSource{
			// Delayed income so steady-state year > 0
			{ID: "ss", Name: "Social Security", Amount: 2000, StartMonth: 60},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 50
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.InvestmentReturn = 7
		s.SteadyStateOverrideYear = 20

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if !fit.HasSteadyState {
			t.Fatalf("precondition: HasSteadyState should be true")
		}
		if fit.SteadyStateGap <= 0 {
			t.Skipf("steady-state surplus scenario (gap=%.2f); skipping gross-up assertions", fit.SteadyStateGap)
		}
		// Roth mirror always equals gap.
		if math.Abs(fit.SteadyStateGrossWithdrawalRoth-fit.SteadyStateGap) > 0.01 {
			t.Errorf("SteadyStateGrossWithdrawalRoth: want %.2f, got %.2f",
				fit.SteadyStateGap, fit.SteadyStateGrossWithdrawalRoth)
		}
		// Tax-deferred mirror: gross > gap, marginal > 0.
		if fit.SteadyStateGrossWithdrawalTaxDeferred <= fit.SteadyStateGap {
			t.Errorf("SteadyStateGrossWithdrawalTaxDeferred (%.2f) must exceed gap (%.2f)",
				fit.SteadyStateGrossWithdrawalTaxDeferred, fit.SteadyStateGap)
		}
		if fit.SteadyStateMarginalRateTaxDeferred <= 0 {
			t.Errorf("SteadyStateMarginalRateTaxDeferred: want > 0, got %.2f",
				fit.SteadyStateMarginalRateTaxDeferred)
		}
		// Taxable mirror: gross > gap (year 20 has built-up gains).
		if fit.SteadyStateGrossWithdrawalTaxable <= fit.SteadyStateGap {
			t.Errorf("SteadyStateGrossWithdrawalTaxable (%.2f) should exceed gap (%.2f) at year 20",
				fit.SteadyStateGrossWithdrawalTaxable, fit.SteadyStateGap)
		}
		// Taxable should be cheaper than tax-deferred (LTCG vs. ordinary).
		if fit.SteadyStateGrossWithdrawalTaxable >= fit.SteadyStateGrossWithdrawalTaxDeferred {
			t.Errorf("Taxable (%.2f) should be cheaper than tax-deferred (%.2f)",
				fit.SteadyStateGrossWithdrawalTaxable, fit.SteadyStateGrossWithdrawalTaxDeferred)
		}
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/steady-state_gross_withdrawal_mirrors_compute -v
```
Expected: FAIL — all steady-state gross-withdrawal fields are 0.

- [ ] **Step 3: Implement steady-state mirrors**

In `internal/services/retirement/analysis/budget_fit.go`, locate the line:

```go
		// Calculate steady state withdrawal rate
		if s.PortfolioValue > 0 && result.SteadyStateGap > 0 {
```

(around line 302). Immediately **before** that `if` (after `result.SteadyStateGap = result.SteadyStateExpenses - result.SteadyStateNetIncome` on line 299), insert:

```go
		// Gross withdrawal mirrors at steady state (only when gap > 0).
		if result.SteadyStateGap > 0 {
			result.SteadyStateGrossWithdrawalRoth = result.SteadyStateGap

			// Tax-deferred: simulate extra ordinary withdrawal at steady state.
			tdSnapSS := estimateTaxSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, result.SteadyStateRMD+result.SteadyStateGap, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
			extraTaxSS := tdSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
			marginalSS := extraTaxSS / result.SteadyStateGap
			if marginalSS < 0 {
				marginalSS = 0
			}
			if marginalSS > 0.95 {
				marginalSS = 0.95
			}
			result.SteadyStateMarginalRateTaxDeferred = marginalSS * 100
			result.SteadyStateGrossWithdrawalTaxDeferred = result.SteadyStateGap / (1 - marginalSS)

			// Taxable: gain fraction grows with time (smooth approximation 1 - (1+r)^-years).
			gainFractionSS := 1.0 - math.Pow(1.0+taxableAnnualReturn/100.0, -yearsToSteadyState)
			if gainFractionSS < 0 {
				gainFractionSS = 0
			}
			taxableExtraSS := steadyStateTaxableCashFlow
			taxableExtraSS.CapitalGainsDistributions += result.SteadyStateGap * gainFractionSS
			txSnapSS := estimateTaxSnapshot(steadyStateMonth, taxableExtraSS, result.SteadyStateRMD, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
			txExtraTaxSS := txSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
			txEffectiveSS := txExtraTaxSS / result.SteadyStateGap
			if txEffectiveSS < 0 {
				txEffectiveSS = 0
			}
			if txEffectiveSS > 0.95 {
				txEffectiveSS = 0.95
			}
			result.SteadyStateEffectiveRateTaxable = txEffectiveSS * 100
			result.SteadyStateGrossWithdrawalTaxable = result.SteadyStateGap / (1 - txEffectiveSS)
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit/steady-state_gross_withdrawal_mirrors_compute -v
```
Expected: PASS (or SKIP if scenario produces a surplus — that's an acceptable acknowledgment in the test).

- [ ] **Step 5: Run full BudgetFit suite**

Run:
```bash
go test ./internal/services/retirement/ -run TestCalculateBudgetFit -v
```
Expected: all sub-tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator_expense_test.go internal/services/retirement/analysis/budget_fit.go
git commit -m "feat(analysis): steady-state gross-withdrawal mirrors via simulation"
```

---

## Task 7: Render gross-withdrawal rows in the budget analysis template

**Files:**
- Modify: `web/templates/components/whatif/budget-analysis.html`

- [ ] **Step 1: Add the Current-section gross-withdrawal block**

In `web/templates/components/whatif/budget-analysis.html`, locate the closing `</div>` of the `<div class="grid grid-cols-2 gap-4 text-center">` block in `whatif-budget-analysis` (line 129). Immediately after that closing `</div>` (and before the section's outer closing `</div>` on line 130), insert:

```html
            {{if gt .Analysis.BudgetFit.MonthlyGap 0.0}}
            <div class="mt-4 pt-3 border-t border-gray-200 dark:border-gray-700">
                <p class="text-xs font-semibold text-gray-600 dark:text-gray-300 uppercase tracking-wide mb-2">Gross Withdrawal Needed to Close Gap</p>
                <div class="text-sm space-y-1">
                    <div class="flex justify-between">
                        <span class="text-gray-500 dark:text-gray-400">From Tax-Deferred</span>
                        <span>
                            <span class="text-red-600 dark:text-red-400 font-medium">{{formatMoney .Analysis.BudgetFit.GrossWithdrawalTaxDeferred}}/mo</span>
                            <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">({{printf "%.0f%%" .Analysis.BudgetFit.MarginalRateTaxDeferred}} marginal)</span>
                        </span>
                    </div>
                    <div class="flex justify-between">
                        <span class="text-gray-500 dark:text-gray-400">From Taxable</span>
                        <span>
                            <span class="text-amber-600 dark:text-amber-400 font-medium">{{formatMoney .Analysis.BudgetFit.GrossWithdrawalTaxable}}/mo</span>
                            <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">(~{{printf "%.0f%%" .Analysis.BudgetFit.EffectiveRateTaxable}} LTCG-equiv)</span>
                        </span>
                    </div>
                    <div class="flex justify-between">
                        <span class="text-gray-500 dark:text-gray-400">From Roth</span>
                        <span>
                            <span class="text-green-600 dark:text-green-400 font-medium">{{formatMoney .Analysis.BudgetFit.GrossWithdrawalRoth}}/mo</span>
                            <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">(no tax)</span>
                        </span>
                    </div>
                </div>
                <p class="text-xs italic text-gray-500 dark:text-gray-400 mt-2">
                    Tax-deferred grosses up to cover income tax. Taxable applies LTCG on the gain portion of the sale. Roth is a 1:1 withdrawal. A larger gross withdrawal depletes the account faster than the gap suggests.
                </p>
            </div>
            {{end}}
```

- [ ] **Step 2: Add the Steady-State mirror block**

In the same file, locate the closing `</div>` of the `<div class="grid grid-cols-2 gap-4 text-center">` inside `whatif-budget-steady-state` (line 229). Immediately after the existing "Values shown in nominal dollars" paragraph (around line 232) and the RMD-driven surplus paragraph (around line 237), but **before** the section's outer closing `</div>`, insert:

```html
    {{if gt .Analysis.BudgetFit.SteadyStateGap 0.0}}
    <div class="mt-4 pt-3 border-t border-gray-200 dark:border-gray-700">
        <p class="text-xs font-semibold text-gray-600 dark:text-gray-300 uppercase tracking-wide mb-2">Gross Withdrawal Needed to Close Gap</p>
        <div class="text-sm space-y-1">
            <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">From Tax-Deferred</span>
                <span>
                    <span class="text-red-600 dark:text-red-400 font-medium">{{formatMoney .Analysis.BudgetFit.SteadyStateGrossWithdrawalTaxDeferred}}/mo</span>
                    <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">({{printf "%.0f%%" .Analysis.BudgetFit.SteadyStateMarginalRateTaxDeferred}} marginal)</span>
                </span>
            </div>
            <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">From Taxable</span>
                <span>
                    <span class="text-amber-600 dark:text-amber-400 font-medium">{{formatMoney .Analysis.BudgetFit.SteadyStateGrossWithdrawalTaxable}}/mo</span>
                    <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">(~{{printf "%.0f%%" .Analysis.BudgetFit.SteadyStateEffectiveRateTaxable}} LTCG-equiv)</span>
                </span>
            </div>
            <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">From Roth</span>
                <span>
                    <span class="text-green-600 dark:text-green-400 font-medium">{{formatMoney .Analysis.BudgetFit.SteadyStateGrossWithdrawalRoth}}/mo</span>
                    <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">(no tax)</span>
                </span>
            </div>
        </div>
        <p class="text-xs italic text-gray-500 dark:text-gray-400 mt-2">
            Tax-deferred grosses up to cover income tax. Taxable applies LTCG on the gain portion of the sale. Roth is a 1:1 withdrawal. A larger gross withdrawal depletes the account faster than the gap suggests.
        </p>
    </div>
    {{end}}
```

- [ ] **Step 3: Verify template parses (no smoke test yet, just compile)**

Run:
```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/templates/components/whatif/budget-analysis.html
git commit -m "feat(whatif): render gross-withdrawal rows under budget analysis"
```

---

## Task 8: Template smoke test — rows render when gap > 0, hidden when gap ≤ 0

**Files:**
- Create: `internal/handlers/whatif/gross_withdrawal_render_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/handlers/whatif/gross_withdrawal_render_test.go`:

```go
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestBudgetAnalysis_GrossWithdrawalRowsRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("renders rows when MonthlyGap > 0", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					MonthlyExpenses:            5000,
					MonthlyIncome:              1000,
					MonthlyGap:                 4000,
					RequiredRate:               4.0,
					GrossWithdrawalTaxDeferred: 5479.45,
					MarginalRateTaxDeferred:    27.0,
					GrossWithdrawalTaxable:     4160.00,
					EffectiveRateTaxable:       4.0,
					GrossWithdrawalRoth:        4000.00,
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Gross Withdrawal Needed to Close Gap") {
			t.Errorf("expected gross-withdrawal heading; got: %s", truncate(out, 500))
		}
		if !strings.Contains(out, "From Tax-Deferred") {
			t.Errorf("expected From Tax-Deferred row")
		}
		if !strings.Contains(out, "From Taxable") {
			t.Errorf("expected From Taxable row")
		}
		if !strings.Contains(out, "From Roth") {
			t.Errorf("expected From Roth row")
		}
		if !strings.Contains(out, "27% marginal") {
			t.Errorf("expected marginal rate annotation; got: %s", truncate(out, 500))
		}
	})

	t.Run("hides rows on surplus", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					MonthlyExpenses: 3000,
					MonthlyIncome:   5000,
					MonthlyGap:      -2000, // surplus
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, "Gross Withdrawal Needed to Close Gap") {
			t.Errorf("expected no gross-withdrawal heading on surplus; got: %s", truncate(out, 500))
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
```

- [ ] **Step 2: Run the smoke test**

Run:
```bash
go test ./internal/handlers/whatif/ -run TestBudgetAnalysis_GrossWithdrawalRowsRender -v
```
Expected: both sub-tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/whatif/gross_withdrawal_render_test.go
git commit -m "test(whatif): smoke test for gross-withdrawal rows render gating"
```

---

## Task 9: Full regression + gitnexus_detect_changes + final commit

- [ ] **Step 1: Run full Go test suite**

Run:
```bash
go test ./...
```
Expected: all tests pass. If any fail, diagnose and fix before continuing.

- [ ] **Step 2: Run linter / formatter**

Run:
```bash
gofmt -l internal/ web/ docs/
go vet ./...
```
Expected: no output from `gofmt`, no warnings from `go vet`. Fix any issues.

- [ ] **Step 3: Run gitnexus_detect_changes**

Use MCP tool:
```
gitnexus_detect_changes()
```
Expected: changes are confined to:
- `BudgetFitAnalysis` struct (10 new fields)
- `BudgetFit` function (additive logic inside `if gap > 0` blocks)
- `whatif-budget-analysis` and `whatif-budget-steady-state` templates (new HTML blocks)
- New test file `gross_withdrawal_render_test.go`

Confirm no unintended symbols / execution flows affected.

- [ ] **Step 4: Manual browser smoke (optional but recommended)**

Start the dev server and visit `/whatif` with a scenario that produces a shortfall. Confirm:
- The new "Gross Withdrawal Needed to Close Gap" section appears under Required Rate.
- All three source lines render with sensible numbers.
- The footnote text reads correctly.
- A surplus scenario (or one with the steady-state slider in surplus territory) hides the section.

If found, fix issues with a follow-up commit before continuing.

- [ ] **Step 5: Final summary commit (if any clean-up needed)**

If steps 1-4 produced no further changes, no commit is needed. Otherwise:

```bash
git add -p   # only stage intended changes
git commit -m "chore(whatif): polish from gross-withdrawal regression sweep"
```

---

## Self-Review (run after writing this plan)

- **Spec coverage:**
  - UI placement ✓ (Tasks 7, 8)
  - 3 sources side-by-side ✓ (Tasks 3, 4, 5, 6)
  - estimateTaxSnapshot simulation ✓ (Tasks 4, 5, 6)
  - Hide on surplus ✓ (Task 2 assertion + template guards in Task 7 + Task 8 second sub-test)
  - Smooth gain-fraction approximation at steady state ✓ (Task 6)
  - Data model 10 new fields ✓ (Task 1)
  - 6 unit tests + 1 template smoke test ✓ (Tasks 2-6 and 8)
  - gitnexus impact analysis ✓ (Task 0)
  - gitnexus_detect_changes before commit ✓ (Task 9)

- **Placeholder scan:** No TBDs / TODOs / "implement later". All code blocks are complete.

- **Type consistency:**
  - `GrossWithdrawalTaxDeferred` consistent across model, computation, and template.
  - `EffectiveRateTaxable` (not `RateTaxable` or `LTCGRate`) consistent across all references.
  - `SteadyStateGrossWithdrawal*` prefix consistent.
  - `MarginalRateTaxDeferred` stored as 0-100 (matches `RequiredRate` convention); template uses `printf "%.0f%%"`. Consistent.
