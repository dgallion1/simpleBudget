# Property Tax Field Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated property-tax expense field with its own inflation rate to `WhatIfSettings`, mirroring the existing `MonthlyHealthcare` + `HealthcareInflation` pattern. Property tax flows through the engine as an essential, inflation-adjusted recurring expense.

**Architecture:** Two new fields on `WhatIfSettings` (`MonthlyPropertyTax`, `PropertyTaxInflation`). Form-spec entry, handler write, persistence default for the inflation rate, engine inclusion in `TotalExpenses` and `CalculateExpenseBreakdown` as essential. UI lives in the Portfolio & Expenses card under Monthly Living Expenses. No federal tax interaction (engine uses standard deduction only — no itemized/SALT modeling).

**Tech Stack:** Go 1.x. No new dependencies. Templates: html/template, HTMX OOB swap.

**Spec:** This file is both spec and plan — the design mirrors the established `MonthlyHealthcare`/`HealthcareInflation` pattern (`internal/models/whatif.go:86,117,453,708,728`), so a separate spec doc would be redundant.

---

## Preconditions

1. Branch `feat/property-tax-field` exists off `dev`. Confirm with `git rev-parse --abbrev-ref HEAD`.
2. `go test ./...` is green on the branch.
3. PR #5 (`feat/scenario-completeness`) is independent and can land before or after this PR.

## File structure

```
internal/models/
└── whatif.go                                modified: 2 new fields, defaults

internal/services/retirement/
├── settings.go                              modified: load default for PropertyTaxInflation, applySettingsUpdates writes
└── (no other changes here)

internal/services/retirement/engine/
└── expense.go                               modified: TotalExpenses + CalculateExpenseBreakdown add property tax

internal/handlers/whatif/
└── form_spec.go                             modified: 2 new field specs

web/templates/components/whatif/
└── portfolio-settings.html                  modified: 2 new inputs (amount + inflation)

internal/services/retirement/
└── property_tax_test.go                     NEW: end-to-end + unit tests
```

## Per-commit invariants

After every task:
- `go build ./...` succeeds
- `go test ./...` passes
- `go vet ./...` clean
- Pre-commit hook passes

---

## Task 0: Branch setup

Already complete. Branch `feat/property-tax-field` exists off `dev` (commit `8606f1f`). Skip to Task 1.

---

## Task 1: Add model fields + defaults + persistence load default (TDD)

**Files:**
- Modify: `internal/models/whatif.go`
- Modify: `internal/services/retirement/settings.go`
- Test: Create `internal/services/retirement/property_tax_test.go`

- [ ] **Step 1: Write failing tests.**

Create `/home/darrell/bin/ai/budget2/internal/services/retirement/property_tax_test.go`:

```go
package retirement

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestDefaultWhatIfSettings_PropertyTaxFields(t *testing.T) {
	s := models.DefaultWhatIfSettings()

	if s.MonthlyPropertyTax != 0 {
		t.Errorf("MonthlyPropertyTax default = %v, want 0", s.MonthlyPropertyTax)
	}
	if s.PropertyTaxInflation != 4.0 {
		t.Errorf("PropertyTaxInflation default = %v, want 4.0", s.PropertyTaxInflation)
	}
}

func TestInitializeLoadedSettings_PropertyTaxInflation(t *testing.T) {
	t.Run("legacy file with zero PropertyTaxInflation gets defaulted to 4.0", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			PropertyTaxInflation: 0,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.PropertyTaxInflation != 4.0 {
			t.Errorf("PropertyTaxInflation = %v, want 4.0 (defaulted)", settings.PropertyTaxInflation)
		}
	})

	t.Run("non-zero PropertyTaxInflation is preserved", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			PropertyTaxInflation: 5.5,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.PropertyTaxInflation != 5.5 {
			t.Errorf("PropertyTaxInflation = %v, want 5.5 (preserved)", settings.PropertyTaxInflation)
		}
	})

	t.Run("MonthlyPropertyTax is preserved verbatim (no defaulting)", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			MonthlyPropertyTax: 800,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.MonthlyPropertyTax != 800 {
			t.Errorf("MonthlyPropertyTax = %v, want 800 (preserved)", settings.MonthlyPropertyTax)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./internal/services/retirement/ -run "DefaultWhatIfSettings_PropertyTax|InitializeLoadedSettings_PropertyTax" -v`
Expected: FAIL — fields don't exist on the struct.

- [ ] **Step 3: Add fields to WhatIfSettings.**

Edit `/home/darrell/bin/ai/budget2/internal/models/whatif.go`. The `Expenses` block lives around line 84-87:
```go
	// Expenses
	MonthlyLivingExpenses float64 `json:"monthly_living_expenses"` // Base monthly expenses
	MonthlyHealthcare     float64 `json:"monthly_healthcare"`      // Monthly healthcare costs (legacy)
	HealthcareStartYears  int     `json:"healthcare_start_years"`  // Years until healthcare starts (legacy)
```

Add a new line directly under `HealthcareStartYears`:

```go
	MonthlyPropertyTax    float64 `json:"monthly_property_tax"`    // Monthly property tax on primary residence
```

The `Rates` block lives around line 116-117:
```go
	InflationRate                       float64 `json:"inflation_rate"`                                // Annual inflation
	HealthcareInflation                 float64 `json:"healthcare_inflation"`                          // Healthcare inflation (legacy, for single-person model)
```

Add a new line directly under `HealthcareInflation`:

```go
	PropertyTaxInflation                float64 `json:"property_tax_inflation"`                        // Property tax inflation (default 4%; reflects assessment growth above CPI)
```

- [ ] **Step 4: Set defaults in DefaultWhatIfSettings.**

In the same file, the `DefaultWhatIfSettings()` function is around line 702-758. The relevant block (around line 706-728) sets default values. Add `PropertyTaxInflation: 4.0` to the literal — the natural place is right after `HealthcareInflation: 6.0,` on line 728:

```go
		HealthcareInflation:             6.0,
		PropertyTaxInflation:            4.0,
```

`MonthlyPropertyTax` does not need to appear in the literal — its zero default of 0 is correct (no assumed property tax).

- [ ] **Step 5: Default PropertyTaxInflation in initializeLoadedSettings.**

Edit `/home/darrell/bin/ai/budget2/internal/services/retirement/settings.go`. The function `initializeLoadedSettings` is around line 150. After the existing `Persons` nil-check block and the TaxConfig block (added by PR #5 — may not be present on this branch since we branched from dev pre-merge), insert near the bottom of the function (just before the closing `}`):

```go
	if settings.PropertyTaxInflation == 0 {
		settings.PropertyTaxInflation = 4.0
	}
```

Place this AFTER the existing checks. The `0 → 4.0` defaulting matches how the legacy file pattern works for other inflation rates — e.g., the existing `taxable_qualified_dividend_percent` defaulting block at line 185.

- [ ] **Step 6: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/ -run "DefaultWhatIfSettings_PropertyTax|InitializeLoadedSettings_PropertyTax" -v`
Expected: PASS — all three test cases green.

- [ ] **Step 7: Run full suite.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages pass.

- [ ] **Step 8: Commit.**

```bash
git add internal/models/whatif.go internal/services/retirement/settings.go internal/services/retirement/property_tax_test.go
git commit -m "feat(model): add MonthlyPropertyTax + PropertyTaxInflation fields

Mirrors the MonthlyHealthcare + HealthcareInflation pattern. Default
inflation is 4% (typical for property assessments — higher than CPI).
Legacy files without PropertyTaxInflation get defaulted on load.
MonthlyPropertyTax has no default; user enters their bill."
```

---

## Task 2: Form spec + handler write

**Files:**
- Modify: `internal/handlers/whatif/form_spec.go`
- Modify: `internal/services/retirement/settings.go`
- Test: append to `internal/services/retirement/property_tax_test.go`

- [ ] **Step 1: Append handler test.**

Append to `internal/services/retirement/property_tax_test.go`:

```go
func TestApplySettingsUpdates_PropertyTax(t *testing.T) {
	t.Run("monthly_property_tax writes to settings", func(t *testing.T) {
		settings := &models.WhatIfSettings{}
		updates := map[string]interface{}{"monthly_property_tax": 750.0}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.MonthlyPropertyTax != 750 {
			t.Errorf("MonthlyPropertyTax = %v, want 750", settings.MonthlyPropertyTax)
		}
	})

	t.Run("property_tax_inflation writes to settings", func(t *testing.T) {
		settings := &models.WhatIfSettings{}
		updates := map[string]interface{}{"property_tax_inflation": 5.5}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.PropertyTaxInflation != 5.5 {
			t.Errorf("PropertyTaxInflation = %v, want 5.5", settings.PropertyTaxInflation)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/services/retirement/ -run "ApplySettingsUpdates_PropertyTax" -v`
Expected: FAIL — keys not handled.

- [ ] **Step 3: Add form spec entries.**

Edit `/home/darrell/bin/ai/budget2/internal/handlers/whatif/form_spec.go`. Append two entries to `settingsFormSpec` (the slice — append before the closing `}`, after the last existing entry):

```go
	{Name: "monthly_property_tax", Kind: fieldFloat, ParseLabel: "monthly property tax",
		HasBounds: true, Min: 0, Max: 50000,
		BoundsMsg: "Monthly property tax must be between 0 and 50000"},
	{Name: "property_tax_inflation", Kind: fieldFloat, ParseLabel: "property tax inflation",
		HasBounds: true, Min: 0, Max: 15,
		BoundsMsg: "Property tax inflation must be between 0 and 15%"},
```

- [ ] **Step 4: Add handler-write entries.**

Edit `/home/darrell/bin/ai/budget2/internal/services/retirement/settings.go`. The function `applySettingsUpdates` is around line 886. After the `monthly_healthcare` block (around line 918-920) — i.e., right after:

```go
	if v, ok := updates["monthly_healthcare"].(float64); ok {
		settings.MonthlyHealthcare = v
	}
```

Insert the property-tax write:

```go
	if v, ok := updates["monthly_property_tax"].(float64); ok {
		settings.MonthlyPropertyTax = v
	}
```

Then find the `healthcare_inflation` write block (around line 967-969) — i.e., right after:

```go
	if v, ok := updates["healthcare_inflation"].(float64); ok {
		settings.HealthcareInflation = v
	}
```

Insert the property-tax inflation write:

```go
	if v, ok := updates["property_tax_inflation"].(float64); ok {
		settings.PropertyTaxInflation = v
	}
```

- [ ] **Step 5: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/ -run "ApplySettingsUpdates_PropertyTax" -v`
Expected: PASS — both subtests green.

- [ ] **Step 6: Run full suite.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: green.

- [ ] **Step 7: Commit.**

```bash
git add internal/handlers/whatif/form_spec.go internal/services/retirement/settings.go internal/services/retirement/property_tax_test.go
git commit -m "feat(whatif): wire property tax through form and persistence handler

Two new form fields: monthly_property_tax (0-50000), property_tax_inflation
(0-15%). applySettingsUpdates merges both into the settings struct."
```

---

## Task 3: Engine — include property tax in monthly expenses

**Files:**
- Modify: `internal/services/retirement/engine/expense.go`
- Test: append to `internal/services/retirement/property_tax_test.go`

- [ ] **Step 1: Append engine test.**

Append to `internal/services/retirement/property_tax_test.go`:

```go
func TestTotalExpenses_PropertyTaxIncluded(t *testing.T) {
	t.Run("MonthlyPropertyTax adds to total expenses with own inflation rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.MonthlyPropertyTax = 800
		s.PropertyTaxInflation = 4.0
		s.InflationRate = 3.0
		s.SpendingDeclineRate = 0
		s.HealthcareInflation = 0

		// Month 0: living=5000, propertyTax=800, total=5800
		got := engine.TotalExpenses(s, 0)
		want := 5000.0 + 800.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 0 TotalExpenses = %v, want %v", got, want)
		}

		// Month 12 (1 year): living grows at 3% → 5150, propertyTax grows at 4% → 832, total=5982
		got12 := engine.TotalExpenses(s, 12)
		expectedLiving := 5000.0 * math.Pow(1.03, 1)
		expectedPropertyTax := 800.0 * math.Pow(1.04, 1)
		want12 := expectedLiving + expectedPropertyTax
		if math.Abs(got12-want12) > 1.0 {
			t.Errorf("month 12 TotalExpenses = %v, want ~%v", got12, want12)
		}
	})

	t.Run("Zero MonthlyPropertyTax has no effect", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.MonthlyPropertyTax = 0
		s.HealthcareInflation = 0
		s.SpendingDeclineRate = 0

		got := engine.TotalExpenses(s, 0)
		if math.Abs(got-5000) > 0.01 {
			t.Errorf("month 0 TotalExpenses = %v, want 5000 (no property tax)", got)
		}
	})
}

func TestCalculateExpenseBreakdown_PropertyTaxIsEssential(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 5000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 600
	s.HealthcareInflation = 0
	s.SpendingDeclineRate = 0

	bd := engine.CalculateExpenseBreakdown(s, 0)
	wantEssential := 5600.0 // living + property tax
	if math.Abs(bd.Essential-wantEssential) > 0.01 {
		t.Errorf("Essential = %v, want %v (living + property tax)", bd.Essential, wantEssential)
	}
	if bd.Discretionary != 0 {
		t.Errorf("Discretionary = %v, want 0", bd.Discretionary)
	}
}
```

The test imports needed at the top of the file (add to existing imports):
```go
import (
	"encoding/json"
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/services/retirement/ -run "TotalExpenses_PropertyTax|CalculateExpenseBreakdown_PropertyTax" -v`
Expected: FAIL — engine doesn't include property tax yet.

- [ ] **Step 3: Add property tax helper to engine/expense.go.**

Edit `/home/darrell/bin/ai/budget2/internal/services/retirement/engine/expense.go`. After the `livingExpensesAtMonth` function (currently ending with the `return s.MonthlyLivingExpenses * compoundedFactorFromPercent(...)` block, around line 26), add a new helper:

```go
// propertyTaxAtMonth returns the inflation-adjusted property tax for the
// given month. Property tax has its own inflation rate (default 4% in
// DefaultWhatIfSettings) that typically runs higher than CPI to reflect
// assessment growth on top of levy increases.
func propertyTaxAtMonth(s *models.WhatIfSettings, month int) float64 {
	if s.MonthlyPropertyTax <= 0 {
		return 0
	}
	return s.MonthlyPropertyTax * compoundedFactorFromPercent(s.PropertyTaxInflation, float64(month))
}
```

- [ ] **Step 4: Wire property tax into TotalExpenses.**

In the same file, the function `TotalExpenses` is around line 28-50. Inside the function body, after `healthcareExpenses := s.GetTotalHealthcareCost(month)` and before the `for _, source := range s.ExpenseSources` loop, add:

```go
	propertyTax := propertyTaxAtMonth(s, month)
```

Then update the return statement (currently `return livingExpenses + healthcareExpenses`) to include property tax:

```go
	return livingExpenses + healthcareExpenses + propertyTax
```

- [ ] **Step 5: Wire property tax into CalculateExpenseBreakdown as essential.**

In the same file, the function `CalculateExpenseBreakdown` is around line 53. Inside the function body, after `healthcareExpenses := s.GetTotalHealthcareCost(month)` and before `essential := livingExpenses + healthcareExpenses`, add:

```go
	propertyTax := propertyTaxAtMonth(s, month)
```

Then update the essential assignment:

```go
	essential := livingExpenses + healthcareExpenses + propertyTax
```

- [ ] **Step 6: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/ -run "TotalExpenses_PropertyTax|CalculateExpenseBreakdown_PropertyTax" -v`
Expected: PASS — all three subtests green.

- [ ] **Step 7: Run full suite + check for regression.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: green. The default `MonthlyPropertyTax` is 0 so no existing test should regress.

If a downstream test breaks (e.g. a calculator parity test compares totals), it likely means the test fixture had `PropertyTaxInflation` defaulted to 4 by `initializeLoadedSettings` while the comparison expected 0. Diagnose; the engine change should be additive (only fires when `MonthlyPropertyTax > 0`).

- [ ] **Step 8: Commit.**

```bash
git add internal/services/retirement/engine/expense.go internal/services/retirement/property_tax_test.go
git commit -m "feat(engine): include MonthlyPropertyTax in monthly expenses

TotalExpenses and CalculateExpenseBreakdown now add inflation-
adjusted property tax to the essential bucket. Property tax uses its
own inflation rate (PropertyTaxInflation, default 4%) — reflecting
assessment growth above CPI — independently of the global rate."
```

---

## Task 4: UI — Portfolio & Expenses card inputs

**Files:**
- Modify: `web/templates/components/whatif/portfolio-settings.html`

- [ ] **Step 1: Add property tax input row.**

Edit `/home/darrell/bin/ai/budget2/web/templates/components/whatif/portfolio-settings.html`. Locate the `Monthly Living Expenses` block (currently lines 52-62, ending with the `<p class="text-xs ...">` describing inflation behaviour). Immediately AFTER its closing `</div>` (and BEFORE the `Projection Years` block at line 64), insert:

```html
        <div class="grid grid-cols-2 gap-3">
            <div>
                <label for="monthly-property-tax-input" class="block text-sm font-medium text-gray-700 dark:text-gray-300">Monthly Property Tax</label>
                <input type="number" id="monthly-property-tax-input" name="monthly_property_tax"
                    value="{{printf "%.0f" .Settings.MonthlyPropertyTax}}"
                    min="0" max="50000" step="50"
                    class="mt-1 block w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
                <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Annual bill ÷ 12. Leave 0 if you rent or own outright with no tax.</p>
            </div>
            <div>
                <label for="property-tax-inflation-input" class="block text-sm font-medium text-gray-700 dark:text-gray-300">Property Tax Inflation %</label>
                <input type="number" id="property-tax-inflation-input" name="property_tax_inflation"
                    value="{{printf "%.1f" .Settings.PropertyTaxInflation}}"
                    min="0" max="15" step="0.1"
                    class="mt-1 block w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
                <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Annual rate (default 4%). Tracks assessment + levy growth.</p>
            </div>
        </div>
```

- [ ] **Step 2: Verify template loads.**

Run: `go build ./... && go test ./internal/templates/...`
Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add web/templates/components/whatif/portfolio-settings.html
git commit -m "feat(whatif/ui): add property tax inputs to Portfolio & Expenses card

Two side-by-side inputs under Monthly Living Expenses: monthly amount
and inflation rate. Helper text explains the annual-÷-12 input
convention and the higher-than-CPI default for the inflation rate."
```

---

## Task 5: End-to-end regression test

**File:** Append to `internal/services/retirement/property_tax_test.go` (or create a new file if preferred for the e2e test).

- [ ] **Step 1: Append e2e regression.**

Append to `/home/darrell/bin/ai/budget2/internal/services/retirement/property_tax_test.go`:

```go
func TestPropertyTaxAffectsProjection(t *testing.T) {
	build := func(monthly float64) *models.WhatIfAnalysis {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5_000
		s.MonthlyPropertyTax = monthly
		s.PropertyTaxInflation = 4.0
		s.StartDate = "2026-01"
		s.Persons = []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1960-01", Name: "You"},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, ClaimAge: 67}

		prepared := prepare.MustFrom(t, s)
		return RunFull(engine.New(), engine.Input{Prepared: prepared})
	}

	withPT := build(800)
	withoutPT := build(0)

	if withPT == nil || withoutPT == nil || withPT.Projection == nil || withoutPT.Projection == nil {
		t.Fatal("RunFull returned nil projection")
	}

	// Final-year balance should be lower with property tax.
	withPTFinal := withPT.Projection.Months[len(withPT.Projection.Months)-1].PortfolioBalance
	withoutPTFinal := withoutPT.Projection.Months[len(withoutPT.Projection.Months)-1].PortfolioBalance

	if !(withoutPTFinal > withPTFinal) {
		t.Errorf("expected $800/mo property tax to lower final balance; got with=%v without=%v",
			withPTFinal, withoutPTFinal)
	}
}
```

This requires `prepare.MustFrom` (from existing test scaffolding) — it's used in `calculator_state_tax_test.go` and others. If `prepare.MustFrom` is not in scope (the package is `retirement`, the helper might live in a sibling package or in test helpers), inspect `helpers_test.go` for the right import. Confirm the helper exists by:

```
grep -rn "func MustFrom" internal/services/retirement/
```

- [ ] **Step 2: Run the test.**

Run: `go test ./internal/services/retirement/ -run TestPropertyTaxAffectsProjection -v`
Expected: PASS.

If `withoutPTFinal == withPTFinal` (FAIL), the engine isn't actually consuming the field — STOP and diagnose.

- [ ] **Step 3: Run full suite.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: green.

- [ ] **Step 4: Commit.**

```bash
git add internal/services/retirement/property_tax_test.go
git commit -m "test(retirement): regression for property tax affecting projection

End-to-end: a scenario with \$800/mo property tax produces a strictly
lower final portfolio balance than the same scenario with \$0
property tax. Pins the engine integration."
```

---

## Task 6: Open the pull request

- [ ] **Step 1: Push branch.**

Run: `git push -u origin feat/property-tax-field`
Expected: push succeeds.

- [ ] **Step 2: Create PR against dev.**

Run:
```bash
gh pr create --base dev --title "feat(whatif): dedicated property tax field with own inflation rate" --body "$(cat <<'EOF'
## Summary
- New \`MonthlyPropertyTax\` and \`PropertyTaxInflation\` fields on \`WhatIfSettings\`, mirroring the existing \`MonthlyHealthcare\` + \`HealthcareInflation\` pattern
- Default inflation rate 4% (typical for property assessments — runs higher than CPI)
- Form spec, handler write, persistence default, engine inclusion, UI input row
- Property tax is treated as an essential expense (cannot be reduced under guardrails)
- End-to-end regression: scenario with property tax produces lower final balance

## Why
Users in property-tax states (NY, NJ, IL, TX, etc.) need a way to model recurring property tax separately from generic expenses, with its own inflation rate. The existing free-form \`ExpenseSources\` works but uses the global inflation rate (default 3%), understating long-horizon property-tax growth.

## Out of scope
- SALT cap interaction with federal tax — engine uses standard deduction only; no itemized modeling
- Senior STAR / property-tax-freeze exemptions — too jurisdiction-specific to model generally
- Multiple properties — single field for primary residence

## Test plan
- [x] \`go test ./...\` green
- [x] Unit: defaults populated, persistence loads correctly, handler writes both fields
- [x] Engine: property tax adds to TotalExpenses with its own inflation rate; lands in essential bucket
- [x] E2E: \$800/mo property tax produces lower final portfolio balance than \$0
- [ ] Manual: set property tax in UI, verify projection numbers shift

## Plan
\`docs/superpowers/plans/2026-05-09-property-tax-field.md\`

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 3: Note PR URL.**

No commit needed.

---

## Self-review checklist

Before marking the plan done:

1. **Spec coverage:**
   - [ ] Model fields → Task 1
   - [ ] Form spec + handler write → Task 2
   - [ ] Engine integration → Task 3
   - [ ] UI inputs → Task 4
   - [ ] E2E regression → Task 5
   - [ ] PR creation → Task 6

2. **Placeholder scan:** No "TBD", "implement appropriate" — all steps show concrete code.

3. **Type consistency:** Field names `MonthlyPropertyTax` and `PropertyTaxInflation` used consistently. JSON tags `monthly_property_tax` and `property_tax_inflation`. Form field names match. Helper function `propertyTaxAtMonth` matches across engine and test.

4. **Out-of-scope deferred:** SALT cap, senior exemptions, multiple properties — explicitly listed in the PR description as out of scope.
