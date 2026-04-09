# SS Portfolio-Aware Claiming Age Optimizer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a synchronous portfolio-impact analysis to the SS Claiming Age Optimizer that runs Monte Carlo simulations across claiming-age combinations and shows which claiming strategy best protects portfolio survival.

**Architecture:** When the SS form is submitted and at least one person has a valid claim age, run a Monte Carlo grid (vary each configured person's claiming age 62-70 while holding the other fixed). Attach the results to the existing `SSComparisonAnalysis.Portfolio` field. Display a new panel below the existing cumulative comparison tables showing survival rate, median ending balance, and delta vs the baseline selection.

**Tech Stack:** Go (retirement calculator, Monte Carlo engine), Go HTML templates, HTMX (existing pattern)

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/services/retirement/social_security.go` | Modify | Relax `SSPortfolioEligible`, add `RunSSPortfolioAnalysis` |
| `internal/services/retirement/social_security_test.go` | Modify | Tests for eligibility, grid computation, optimal selection, delta calculation |
| `internal/models/whatif.go` | Modify | Remove `Ready`/`Error` from `SSPortfolioAnalysis` (sync doesn't need them) |
| `internal/services/retirement/calculator.go` | Modify | Call `RunSSPortfolioAnalysis` in `RunFullAnalysis` when eligible |
| `internal/handlers/whatif/handlers_test.go` | Modify | Integration test: SS handler populates portfolio analysis |
| `web/templates/components/whatif/social-security.html` | Modify | Add portfolio impact panel below existing tables |

---

### Task 1: Relax SSPortfolioEligible for Single-Person Use

**Files:**
- Modify: `internal/services/retirement/social_security.go:41-58`
- Modify: `internal/services/retirement/social_security_test.go`

The current `SSPortfolioEligible` requires both claim ages set and both persons configured. Relax it to require at least one person with a valid claim age and matching FRA benefit.

- [ ] **Step 1: Write failing tests for relaxed eligibility**

Add to `internal/services/retirement/social_security_test.go`:

```go
func TestSSPortfolioEligible(t *testing.T) {
	base := func() *models.WhatIfSettings {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			SpouseFRABenefit: 154,
			SpouseFRA:        67,
		}
		return s
	}

	t.Run("nil settings", func(t *testing.T) {
		if SSPortfolioEligible(nil) {
			t.Error("nil settings should not be eligible")
		}
	})

	t.Run("nil social security config", func(t *testing.T) {
		s := base()
		s.SocialSecurity = nil
		if SSPortfolioEligible(s) {
			t.Error("nil SS config should not be eligible")
		}
	})

	t.Run("no claim ages set", func(t *testing.T) {
		s := base()
		if SSPortfolioEligible(s) {
			t.Error("no claim ages should not be eligible")
		}
	})

	t.Run("only primary claim age set", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 67
		if !SSPortfolioEligible(s) {
			t.Error("primary-only should be eligible")
		}
	})

	t.Run("only spouse claim age set with benefit", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		if !SSPortfolioEligible(s) {
			t.Error("spouse-only should be eligible")
		}
	})

	t.Run("both claim ages set", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 67
		s.SocialSecurity.SpouseClaimAge = 62
		if !SSPortfolioEligible(s) {
			t.Error("both set should be eligible")
		}
	})

	t.Run("primary claim age below current age", func(t *testing.T) {
		s := base()
		s.CurrentAge = 70
		s.SocialSecurity.ClaimAge = 65
		if SSPortfolioEligible(s) {
			t.Error("claim age below current age should not be eligible")
		}
	})

	t.Run("spouse claim age below spouse age", func(t *testing.T) {
		s := base()
		s.SpouseAge = 65
		s.SocialSecurity.SpouseClaimAge = 62
		if SSPortfolioEligible(s) {
			t.Error("spouse claim age below spouse age should not be eligible")
		}
	})

	t.Run("no spouse — primary only", func(t *testing.T) {
		s := base()
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		}
		s.SpouseAge = 0
		s.SocialSecurity.SpouseFRABenefit = 0
		s.SocialSecurity.ClaimAge = 67
		if !SSPortfolioEligible(s) {
			t.Error("single person with claim age should be eligible")
		}
	})

	t.Run("primary benefit zero with claim age set", func(t *testing.T) {
		s := base()
		s.SocialSecurity.FRABenefit = 0
		s.SocialSecurity.ClaimAge = 67
		if SSPortfolioEligible(s) {
			t.Error("zero primary benefit should not be eligible even with claim age")
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestSSPortfolioEligible -v`
Expected: FAIL — "primary-only should be eligible", "spouse-only should be eligible", "single person with claim age should be eligible"

- [ ] **Step 3: Rewrite SSPortfolioEligible**

Replace the existing `SSPortfolioEligible` function in `internal/services/retirement/social_security.go:41-58`:

```go
func SSPortfolioEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	ss := s.SocialSecurity

	primaryOK := ss.FRABenefit > 0 && validSSClaimAge(ss.ClaimAge) &&
		s.CurrentAge > 0 && ss.ClaimAge >= max(62, s.CurrentAge)

	spouseOK := s.HasSpouse() && ss.SpouseFRABenefit > 0 &&
		validSSClaimAge(ss.SpouseClaimAge) &&
		s.SpouseAge > 0 && ss.SpouseClaimAge >= max(62, s.SpouseAge)

	return primaryOK || spouseOK
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestSSPortfolioEligible -v`
Expected: PASS (all sub-tests)

- [ ] **Step 5: Run full test suite to check for regressions**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/social_security.go internal/services/retirement/social_security_test.go
git commit -m "feat: relax SSPortfolioEligible to allow single-person portfolio analysis"
```

---

### Task 2: Clean Up SSPortfolioAnalysis Model (Remove Async Fields)

**Files:**
- Modify: `internal/models/whatif.go:1344-1353`

The `Ready` and `Error` fields were designed for async polling. Since we're going synchronous, remove them.

- [ ] **Step 1: Remove Ready and Error fields from SSPortfolioAnalysis**

In `internal/models/whatif.go`, replace the `SSPortfolioAnalysis` struct:

```go
type SSPortfolioAnalysis struct {
	PrimaryOptions       []SSPortfolioOption `json:"primary_options"`
	SpouseOptions        []SSPortfolioOption `json:"spouse_options"`
	OptimalPrimaryAge    int                 `json:"optimal_primary_age"`
	OptimalSpouseAge     int                 `json:"optimal_spouse_age"`
	OptimalSurvivalRate  float64             `json:"optimal_survival_rate"`
	BaselineSurvivalRate float64             `json:"baseline_survival_rate"`
}
```

- [ ] **Step 2: Verify build**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success (nothing references `Ready` or `Error` yet)

- [ ] **Step 3: Commit**

```bash
git add internal/models/whatif.go
git commit -m "refactor: remove async Ready/Error fields from SSPortfolioAnalysis"
```

---

### Task 3: Implement RunSSPortfolioAnalysis

**Files:**
- Modify: `internal/services/retirement/social_security.go`
- Modify: `internal/services/retirement/social_security_test.go`

This is the core computation. For each valid claiming age of the configured person(s), clone the settings, override the claim age, build a calculator, and run a reduced Monte Carlo simulation.

- [ ] **Step 1: Write failing tests for RunSSPortfolioAnalysis**

Add to `internal/services/retirement/social_security_test.go`:

```go
func TestRunSSPortfolioAnalysis(t *testing.T) {
	base := func() *models.WhatIfSettings {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.TaxDeferredPercent = 60
		s.ProjectionYears = 30
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			SpouseFRABenefit: 154,
			SpouseFRA:        67,
		}
		return s
	}

	t.Run("not eligible returns nil", func(t *testing.T) {
		s := base()
		// No claim ages set
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result != nil {
			t.Error("should return nil when not eligible")
		}
	})

	t.Run("spouse only — varies spouse ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		if len(result.PrimaryOptions) != 0 {
			t.Errorf("primary options should be empty, got %d", len(result.PrimaryOptions))
		}
		if len(result.SpouseOptions) == 0 {
			t.Fatal("spouse options should not be empty")
		}
		// Spouse is 54, so all ages 62-70 should be present
		if len(result.SpouseOptions) != 9 {
			t.Errorf("expected 9 spouse options, got %d", len(result.SpouseOptions))
		}
		// Each option should have a survival rate between 0 and 100
		for _, opt := range result.SpouseOptions {
			if opt.SurvivalRate < 0 || opt.SurvivalRate > 100 {
				t.Errorf("age %d: survival rate %.1f out of range", opt.ClaimAge, opt.SurvivalRate)
			}
			if opt.ClaimAge < 62 || opt.ClaimAge > 70 {
				t.Errorf("unexpected claim age %d", opt.ClaimAge)
			}
		}
		// Optimal spouse age should be set
		if result.OptimalSpouseAge < 62 || result.OptimalSpouseAge > 70 {
			t.Errorf("optimal spouse age %d out of range", result.OptimalSpouseAge)
		}
		// Baseline survival rate should match the selected age (62)
		if result.BaselineSurvivalRate < 0 || result.BaselineSurvivalRate > 100 {
			t.Errorf("baseline survival rate %.1f out of range", result.BaselineSurvivalRate)
		}
	})

	t.Run("primary only — varies primary ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 67
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		if len(result.SpouseOptions) != 0 {
			t.Errorf("spouse options should be empty, got %d", len(result.SpouseOptions))
		}
		// Primary is 67, so only ages 67-70 should be present
		if len(result.PrimaryOptions) != 4 {
			t.Errorf("expected 4 primary options (67-70), got %d", len(result.PrimaryOptions))
		}
		for _, opt := range result.PrimaryOptions {
			if opt.ClaimAge < 67 {
				t.Errorf("age %d should not be below current age 67", opt.ClaimAge)
			}
		}
	})

	t.Run("both set — varies both", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 67
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		if len(result.PrimaryOptions) == 0 {
			t.Error("primary options should not be empty")
		}
		if len(result.SpouseOptions) == 0 {
			t.Error("spouse options should not be empty")
		}
	})

	t.Run("delta vs baseline is computed", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		// The baseline age is 62 — its delta should be 0
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge == 62 {
				if opt.DeltaSurvivalRate != 0 {
					t.Errorf("baseline age delta should be 0, got %.2f", opt.DeltaSurvivalRate)
				}
			}
		}
	})

	t.Run("optimal age tiebreak favors younger", func(t *testing.T) {
		// This is a property test: if two ages have the same survival rate,
		// the one with higher median balance wins; if tied again, younger wins.
		// We can't control MC output easily, so just verify the optimal age
		// is within the valid range.
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		if result.OptimalSpouseAge < 62 || result.OptimalSpouseAge > 70 {
			t.Errorf("optimal spouse age %d out of range", result.OptimalSpouseAge)
		}
	})

	t.Run("monthly benefit matches SS comparison table", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis()
		if result == nil {
			t.Fatal("should return analysis")
		}
		// Get the expected benefits from the SS comparison table
		ssAnalysis := c.RunSSAnalysis()
		if ssAnalysis == nil || len(ssAnalysis.SpouseOptions) == 0 {
			t.Fatal("SS analysis should have spouse options")
		}
		// Build a map of expected monthly benefits by age
		expected := make(map[int]float64)
		for _, opt := range ssAnalysis.SpouseOptions {
			expected[opt.ClaimAge] = opt.MonthlyBenefit
		}
		for _, opt := range result.SpouseOptions {
			if exp, ok := expected[opt.ClaimAge]; ok {
				if !withinTolerance(opt.MonthlyBenefit, exp, 0.01) {
					t.Errorf("age %d: monthly benefit %.2f != expected %.2f",
						opt.ClaimAge, opt.MonthlyBenefit, exp)
				}
			}
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestRunSSPortfolioAnalysis -v`
Expected: FAIL — `RunSSPortfolioAnalysis` method does not exist

- [ ] **Step 3: Implement RunSSPortfolioAnalysis**

Add to `internal/services/retirement/social_security.go`:

```go
const ssPortfolioMonteCarloRuns = 250

// RunSSPortfolioAnalysis runs Monte Carlo simulations across valid claiming-age
// combinations and returns portfolio survival metrics for each. It varies each
// configured person's claiming age (62-70, skipping ages below current age) while
// holding the other person's age at the selected value.
func (c *Calculator) RunSSPortfolioAnalysis() *models.SSPortfolioAnalysis {
	if !SSPortfolioEligible(c.Settings) {
		return nil
	}

	ss := c.Settings.SocialSecurity
	ssAnalysis := c.RunSSAnalysis()
	if ssAnalysis == nil {
		return nil
	}

	result := &models.SSPortfolioAnalysis{}

	// Determine which persons to vary
	primaryActive := ss.FRABenefit > 0 && validSSClaimAge(ss.ClaimAge) &&
		c.Settings.CurrentAge > 0 && ss.ClaimAge >= max(62, c.Settings.CurrentAge)
	spouseActive := c.Settings.HasSpouse() && ss.SpouseFRABenefit > 0 &&
		validSSClaimAge(ss.SpouseClaimAge) &&
		c.Settings.SpouseAge > 0 && ss.SpouseClaimAge >= max(62, c.Settings.SpouseAge)

	// Build benefit lookup from SS analysis
	primaryBenefitByAge := make(map[int]float64)
	for _, opt := range ssAnalysis.Options {
		primaryBenefitByAge[opt.ClaimAge] = opt.MonthlyBenefit
	}
	spouseBenefitByAge := make(map[int]float64)
	for _, opt := range ssAnalysis.SpouseOptions {
		spouseBenefitByAge[opt.ClaimAge] = opt.MonthlyBenefit
	}

	// Run grid for primary (vary primary age, hold spouse at selected)
	if primaryActive {
		minAge := max(62, c.Settings.CurrentAge)
		for age := minAge; age <= 70; age++ {
			mc := c.runPortfolioCellMC(age, ss.SpouseClaimAge)
			result.PrimaryOptions = append(result.PrimaryOptions, models.SSPortfolioOption{
				ClaimAge:            age,
				MonthlyBenefit:      primaryBenefitByAge[age],
				SurvivalRate:        mc.Stats.SuccessRate,
				MedianEndingBalance: mc.Stats.MedianBalance,
				P10EndingBalance:    mc.Stats.Percentile10,
				P90EndingBalance:    mc.Stats.Percentile90,
			})
		}
	}

	// Run grid for spouse (vary spouse age, hold primary at selected)
	if spouseActive {
		minAge := max(62, c.Settings.SpouseAge)
		for age := minAge; age <= 70; age++ {
			mc := c.runPortfolioCellMC(ss.ClaimAge, age)
			result.SpouseOptions = append(result.SpouseOptions, models.SSPortfolioOption{
				ClaimAge:            age,
				MonthlyBenefit:      spouseBenefitByAge[age],
				SurvivalRate:        mc.Stats.SuccessRate,
				MedianEndingBalance: mc.Stats.MedianBalance,
				P10EndingBalance:    mc.Stats.Percentile10,
				P90EndingBalance:    mc.Stats.Percentile90,
			})
		}
	}

	// Find baseline survival rate (at currently selected ages)
	result.BaselineSurvivalRate = c.findBaselineSurvival(result, ss.ClaimAge, ss.SpouseClaimAge)

	// Find optimal ages and compute deltas
	c.computePortfolioOptimalAndDeltas(result)

	return result
}

// runPortfolioCellMC clones the settings with the given claim ages and runs
// a reduced Monte Carlo simulation.
func (c *Calculator) runPortfolioCellMC(primaryClaimAge, spouseClaimAge int) *models.MonteCarloAnalysis {
	clone := c.cloneSettingsWithClaimAges(primaryClaimAge, spouseClaimAge)
	cellCalc := NewCalculator(clone)
	return cellCalc.RunMonteCarloSimulation(ssPortfolioMonteCarloRuns)
}

// cloneSettingsWithClaimAges creates a copy of settings with overridden SS claim ages.
// Uses JSON round-trip for a safe deep copy.
func (c *Calculator) cloneSettingsWithClaimAges(primaryClaimAge, spouseClaimAge int) *models.WhatIfSettings {
	data, _ := json.Marshal(c.Settings)
	var clone models.WhatIfSettings
	_ = json.Unmarshal(data, &clone)

	// Restore non-JSON derived fields
	clone.CurrentAge = c.Settings.CurrentAge
	clone.SpouseAge = c.Settings.SpouseAge

	if clone.SocialSecurity != nil {
		if primaryClaimAge > 0 {
			clone.SocialSecurity.ClaimAge = primaryClaimAge
		}
		if spouseClaimAge > 0 {
			clone.SocialSecurity.SpouseClaimAge = spouseClaimAge
		}
	}
	return &clone
}

func (c *Calculator) findBaselineSurvival(result *models.SSPortfolioAnalysis, primaryAge, spouseAge int) float64 {
	for _, opt := range result.PrimaryOptions {
		if opt.ClaimAge == primaryAge {
			return opt.SurvivalRate
		}
	}
	for _, opt := range result.SpouseOptions {
		if opt.ClaimAge == spouseAge {
			return opt.SurvivalRate
		}
	}
	return 0
}

func (c *Calculator) computePortfolioOptimalAndDeltas(result *models.SSPortfolioAnalysis) {
	bestSurvival := -1.0
	bestMedian := -1.0
	bestAgeSum := 999

	// Find optimal primary age
	for i := range result.PrimaryOptions {
		opt := &result.PrimaryOptions[i]
		if isBetterPortfolioOption(opt.SurvivalRate, opt.MedianEndingBalance, opt.ClaimAge,
			bestSurvival, bestMedian, bestAgeSum) {
			bestSurvival = opt.SurvivalRate
			bestMedian = opt.MedianEndingBalance
			bestAgeSum = opt.ClaimAge
			result.OptimalPrimaryAge = opt.ClaimAge
		}
	}

	bestSurvival = -1.0
	bestMedian = -1.0
	bestAgeSum = 999

	// Find optimal spouse age
	for i := range result.SpouseOptions {
		opt := &result.SpouseOptions[i]
		if isBetterPortfolioOption(opt.SurvivalRate, opt.MedianEndingBalance, opt.ClaimAge,
			bestSurvival, bestMedian, bestAgeSum) {
			bestSurvival = opt.SurvivalRate
			bestMedian = opt.MedianEndingBalance
			bestAgeSum = opt.ClaimAge
			result.OptimalSpouseAge = opt.ClaimAge
		}
	}

	// Set optimal survival rate (best from either person)
	if result.OptimalPrimaryAge > 0 {
		for _, opt := range result.PrimaryOptions {
			if opt.ClaimAge == result.OptimalPrimaryAge {
				result.OptimalSurvivalRate = opt.SurvivalRate
			}
		}
	}
	if result.OptimalSpouseAge > 0 {
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge == result.OptimalSpouseAge {
				if opt.SurvivalRate > result.OptimalSurvivalRate {
					result.OptimalSurvivalRate = opt.SurvivalRate
				}
			}
		}
	}

	// Compute deltas vs baseline
	for i := range result.PrimaryOptions {
		result.PrimaryOptions[i].DeltaSurvivalRate = result.PrimaryOptions[i].SurvivalRate - result.BaselineSurvivalRate
	}
	for i := range result.SpouseOptions {
		result.SpouseOptions[i].DeltaSurvivalRate = result.SpouseOptions[i].SurvivalRate - result.BaselineSurvivalRate
	}
}

// isBetterPortfolioOption returns true if the candidate is better than the current best.
// Ranking: highest survival rate > highest median balance > youngest age (lower sum).
func isBetterPortfolioOption(survivalRate, medianBalance float64, age int,
	bestSurvival, bestMedian float64, bestAge int) bool {
	if survivalRate > bestSurvival {
		return true
	}
	if survivalRate == bestSurvival && medianBalance > bestMedian {
		return true
	}
	if survivalRate == bestSurvival && medianBalance == bestMedian && age < bestAge {
		return true
	}
	return false
}
```

Note: You need to add `"encoding/json"` to the imports at the top of `social_security.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestRunSSPortfolioAnalysis -v -timeout 120s`
Expected: PASS (all sub-tests). Note: this test runs MC simulations, so it takes a few seconds.

- [ ] **Step 5: Run full test suite**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/social_security.go internal/services/retirement/social_security_test.go
git commit -m "feat: implement RunSSPortfolioAnalysis with MC grid computation"
```

---

### Task 4: Wire Portfolio Analysis into RunFullAnalysis

**Files:**
- Modify: `internal/services/retirement/calculator.go:2989-3008`
- Modify: `internal/handlers/whatif/handlers_test.go`

Add the portfolio call in `RunFullAnalysis` after the SS analysis completes. This avoids infinite recursion (since `RunSSPortfolioAnalysis` calls `RunSSAnalysis` internally for benefit lookup).

- [ ] **Step 1: Write failing integration test**

Add to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIfSocialSecurity_PopulatesPortfolio(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed with persons so ages resolve
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1_000_000
	settings.MonthlyLivingExpenses = 5000
	settings.TaxDeferredPercent = 60
	settings.ProjectionYears = 30
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
	}
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       4100,
		FRA:              66,
		COLARate:         0.02,
		SpouseFRABenefit: 154,
		SpouseFRA:        67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}

	form := url.Values{
		"fra_benefit":        {"4100"},
		"fra":                {"66"},
		"cola_rate":          {"2.0"},
		"spouse_fra_benefit": {"154"},
		"spouse_fra":         {"67"},
		"spouse_claim_age":   {"62"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	// The response is rendered HTML (no renderer in test = JSON fallback).
	// Parse the JSON response to check for portfolio data.
	var pageData models.WhatIfPageData
	if err := json.NewDecoder(w.Body).Decode(&pageData); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if pageData.Analysis == nil || pageData.Analysis.SocialSecurity == nil {
		t.Fatal("expected SS analysis in response")
	}
	if pageData.Analysis.SocialSecurity.Portfolio == nil {
		t.Fatal("expected portfolio analysis in response")
	}
	portfolio := pageData.Analysis.SocialSecurity.Portfolio
	if len(portfolio.SpouseOptions) == 0 {
		t.Error("expected spouse portfolio options")
	}
	if portfolio.OptimalSpouseAge < 62 || portfolio.OptimalSpouseAge > 70 {
		t.Errorf("optimal spouse age %d out of range", portfolio.OptimalSpouseAge)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/whatif/ -run TestHandleWhatIfSocialSecurity_PopulatesPortfolio -v -timeout 120s`
Expected: FAIL — portfolio is nil

- [ ] **Step 3: Add portfolio analysis call to RunFullAnalysis**

In `internal/services/retirement/calculator.go`, inside `RunFullAnalysis()`, after the `ssAnalysis` block (around line 2992), add the portfolio call:

Replace:
```go
	var ssAnalysis *models.SSComparisonAnalysis
	if c.Settings.SocialSecurity != nil && c.Settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = c.RunSSAnalysis()
	}

	return &models.WhatIfAnalysis{
```

With:
```go
	var ssAnalysis *models.SSComparisonAnalysis
	if c.Settings.SocialSecurity != nil && c.Settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = c.RunSSAnalysis()
		if ssAnalysis != nil && SSPortfolioEligible(c.Settings) {
			ssAnalysis.Portfolio = c.RunSSPortfolioAnalysis()
		}
	}

	return &models.WhatIfAnalysis{
```

Also update `handleWhatIfSocialSecurity` in `handlers.go`. The handler currently calls `calc.RunFullAnalysis()` (line 2561), so the portfolio analysis will be included automatically. No handler changes needed.

- [ ] **Step 4: Run integration test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/whatif/ -run TestHandleWhatIfSocialSecurity_PopulatesPortfolio -v -timeout 120s`
Expected: PASS

- [ ] **Step 5: Run full handler test suite**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/whatif/ -count=1 -timeout 180s`
Expected: PASS

- [ ] **Step 6: Run full project test suite**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./... -count=1 -timeout 180s`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/calculator.go internal/handlers/whatif/handlers_test.go
git commit -m "feat: wire SS portfolio analysis into RunFullAnalysis"
```

---

### Task 5: Add Portfolio Impact Panel to Template

**Files:**
- Modify: `web/templates/components/whatif/social-security.html`

Add the portfolio-aware panel below the existing SS comparison content. Only render when portfolio data exists.

- [ ] **Step 1: Add portfolio impact section to the results template**

In `web/templates/components/whatif/social-security.html`, inside the `whatif-social-security-results` template, just before the final closing `</div>` and `{{end}}` (before line 211), add:

```html
    {{/* Portfolio Impact Analysis */}}
    {{if .Analysis.SocialSecurity.Portfolio}}
    {{$portfolio := .Analysis.SocialSecurity.Portfolio}}
    <div class="mt-4 pt-3 border-t dark:border-gray-700">
        <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Portfolio Impact Analysis</h4>
        <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">Monte Carlo simulation showing how each claiming age affects portfolio survival. This accounts for investment returns on early-claimed benefits.</p>

        {{if gt $portfolio.OptimalSurvivalRate 0.0}}
        <div class="text-xs bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-200 dark:border-indigo-800 rounded-md p-2 mb-3">
            Portfolio-optimal:
            {{if gt $portfolio.OptimalPrimaryAge 0}}You at {{$portfolio.OptimalPrimaryAge}}{{end}}{{if and (gt $portfolio.OptimalPrimaryAge 0) (gt $portfolio.OptimalSpouseAge 0)}}, {{end}}{{if gt $portfolio.OptimalSpouseAge 0}}spouse at {{$portfolio.OptimalSpouseAge}}{{end}}
            — {{printf "%.1f" $portfolio.OptimalSurvivalRate}}% survival
            {{if ne $portfolio.OptimalSurvivalRate $portfolio.BaselineSurvivalRate}}
            (vs {{printf "%.1f" $portfolio.BaselineSurvivalRate}}% at current selection)
            {{end}}
        </div>
        {{end}}

        {{if $portfolio.PrimaryOptions}}
        <div class="overflow-x-auto mb-3">
            <table class="w-full text-sm">
                <thead>
                    <tr class="border-b dark:border-gray-600">
                        <th class="text-left py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Age</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Monthly</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Survival</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Median End</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">10th/90th</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Delta</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $portfolio.PrimaryOptions}}
                    <tr class="border-b dark:border-gray-700 {{if eq .ClaimAge $portfolio.OptimalPrimaryAge}}bg-green-50 dark:bg-green-900/20{{end}}">
                        <td class="py-2 px-2 font-medium text-gray-800 dark:text-gray-200">
                            {{.ClaimAge}}
                            {{if eq .ClaimAge $portfolio.OptimalPrimaryAge}}
                            <span class="text-xs text-green-600 dark:text-green-400">&#9733;</span>
                            {{end}}
                        </td>
                        <td class="py-2 px-2 text-right text-gray-800 dark:text-gray-200">${{formatNumber .MonthlyBenefit}}</td>
                        <td class="py-2 px-2 text-right text-gray-800 dark:text-gray-200">{{printf "%.1f" .SurvivalRate}}%</td>
                        <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300">{{formatMoney .MedianEndingBalance}}</td>
                        <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300 text-xs">{{formatMoney .P10EndingBalance}} / {{formatMoney .P90EndingBalance}}</td>
                        <td class="py-2 px-2 text-right {{if gt .DeltaSurvivalRate 0.0}}text-green-600 dark:text-green-400{{else if lt .DeltaSurvivalRate 0.0}}text-amber-600 dark:text-amber-400{{else}}text-gray-500 dark:text-gray-400{{end}}">
                            {{if gt .DeltaSurvivalRate 0.0}}+{{end}}{{printf "%.1f" .DeltaSurvivalRate}}%
                        </td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}

        {{if $portfolio.SpouseOptions}}
        <div class="overflow-x-auto">
            {{if $portfolio.PrimaryOptions}}<h5 class="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Spouse Portfolio Impact</h5>{{end}}
            <table class="w-full text-sm">
                <thead>
                    <tr class="border-b dark:border-gray-600">
                        <th class="text-left py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Age</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Monthly</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Survival</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Median End</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">10th/90th</th>
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Delta</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $portfolio.SpouseOptions}}
                    <tr class="border-b dark:border-gray-700 {{if eq .ClaimAge $portfolio.OptimalSpouseAge}}bg-green-50 dark:bg-green-900/20{{end}}">
                        <td class="py-2 px-2 font-medium text-gray-800 dark:text-gray-200">
                            {{.ClaimAge}}
                            {{if eq .ClaimAge $portfolio.OptimalSpouseAge}}
                            <span class="text-xs text-green-600 dark:text-green-400">&#9733;</span>
                            {{end}}
                        </td>
                        <td class="py-2 px-2 text-right text-gray-800 dark:text-gray-200">${{formatNumber .MonthlyBenefit}}</td>
                        <td class="py-2 px-2 text-right text-gray-800 dark:text-gray-200">{{printf "%.1f" .SurvivalRate}}%</td>
                        <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300">{{formatMoney .MedianEndingBalance}}</td>
                        <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300 text-xs">{{formatMoney .P10EndingBalance}} / {{formatMoney .P90EndingBalance}}</td>
                        <td class="py-2 px-2 text-right {{if gt .DeltaSurvivalRate 0.0}}text-green-600 dark:text-green-400{{else if lt .DeltaSurvivalRate 0.0}}text-amber-600 dark:text-amber-400{{else}}text-gray-500 dark:text-gray-400{{end}}">
                            {{if gt .DeltaSurvivalRate 0.0}}+{{end}}{{printf "%.1f" .DeltaSurvivalRate}}%
                        </td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}

        <p class="text-xs text-gray-400 dark:text-gray-500 mt-2">&#9733; Best portfolio survival. Based on {{250}} Monte Carlo simulations per claiming age. Above tables show cumulative benefits; this section shows portfolio survival impact.</p>
    </div>
    {{end}}
```

- [ ] **Step 2: Verify the app builds and templates parse**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 3: Manually verify in browser**

Start the app: `cd /home/darrell/bin/ai/budget2 && go run .`

1. Navigate to the What-If page
2. In SS Claiming Age Optimizer, set Spouse Claim Age to 62
3. Click "Compare Claiming Ages"
4. Verify the "Portfolio Impact Analysis" panel appears below the existing tables
5. Verify it shows 9 rows (ages 62-70) with survival rates, median balance, and deltas
6. Verify the optimal age is starred in green
7. Verify the banner shows the portfolio-optimal recommendation

- [ ] **Step 4: Commit**

```bash
git add web/templates/components/whatif/social-security.html
git commit -m "feat: add portfolio impact panel to SS optimizer UI"
```

---

### Task 6: Final Regression Test and Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./... -count=1 -timeout 180s`
Expected: PASS (all packages)

- [ ] **Step 2: Verify build**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 3: Run vet and staticcheck if available**

Run: `cd /home/darrell/bin/ai/budget2 && go vet ./...`
Expected: No issues

- [ ] **Step 4: Review diff for unintended changes**

Run: `cd /home/darrell/bin/ai/budget2 && git diff --stat HEAD~5`
Expected: Only the files listed in the file structure table above should be modified.
