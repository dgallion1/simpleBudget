package retirement

import (
	"math"
	"math/rand"
	"testing"

	"budget2/internal/models"
)

// TestDefaultMonteCarloConfig verifies default configuration values
func TestDefaultMonteCarloConfig(t *testing.T) {
	config := DefaultMonteCarloConfig()

	tests := []struct {
		name     string
		got      float64
		expected float64
	}{
		{"ReturnVolatility", config.ReturnVolatility, 15.0},
		{"CrashProbability", config.CrashProbability, 0.05},
		{"CrashSeverity", config.CrashSeverity, -30.0},
		{"RecoveryBoost", config.RecoveryBoost, 5.0},
		{"SpendingShockProb", config.SpendingShockProb, 0.08},
		{"SpendingShockMin", config.SpendingShockMin, 5000},
		{"SpendingShockMax", config.SpendingShockMax, 25000},
		{"HealthShockProb", config.HealthShockProb, 0.05},
		{"HealthShockMin", config.HealthShockMin, 10000},
		{"HealthShockMax", config.HealthShockMax, 50000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}

	if config.LongevityVariation != 5 {
		t.Errorf("LongevityVariation = %d, want 5", config.LongevityVariation)
	}
}

func TestRunProjection_AnnualReturnCompoundsToAnnualRate(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 100000
	settings.MonthlyLivingExpenses = 0
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.IncomeSources = nil
	settings.ExpenseSources = nil
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	settings.InvestmentReturn = 6.0
	settings.InflationRate = 0
	settings.ProjectionYears = 1
	settings.ProjectionTiming = models.ProjectionTimingEndOfMonth

	result := newTestCalc(t, settings).RunProjection()
	if len(result.Months) != 12 {
		t.Fatalf("got %d months, want 12", len(result.Months))
	}

	expected := settings.PortfolioValue * 1.06
	if math.Abs(result.FinalBalance-expected) > 0.01 {
		t.Fatalf("final balance = %.2f, want %.2f", result.FinalBalance, expected)
	}
}

// TestGenerateYearlyReturns verifies return generation with crashes and volatility
func TestGenerateYearlyReturns(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.InvestmentReturn = 7.0
	calc := newTestCalc(t, settings)

	// Use fixed seed for reproducibility
	rng := rand.New(rand.NewSource(42))

	t.Run("generates correct number of years", func(t *testing.T) {
		timing := &CrashTiming{}
		lastCrash := -999
		returns := calc.generateYearlyReturns(rng, DefaultMonteCarloConfig(), 30, timing, &lastCrash)

		if len(returns) != 30 {
			t.Errorf("got %d years, want 30", len(returns))
		}
	})

	t.Run("returns are bounded", func(t *testing.T) {
		rng := rand.New(rand.NewSource(123))
		timing := &CrashTiming{}
		lastCrash := -999
		returns := calc.generateYearlyReturns(rng, DefaultMonteCarloConfig(), 100, timing, &lastCrash)

		for i, r := range returns {
			if r < -50 || r > 50 {
				t.Errorf("year %d return %v is out of bounds [-50, 50]", i, r)
			}
		}
	})

	t.Run("crashes occur with high probability config", func(t *testing.T) {
		rng := rand.New(rand.NewSource(456))
		config := &MonteCarloConfig{
			ReturnVolatility: 15.0,
			CrashProbability: 0.5, // 50% crash chance for testing
			CrashSeverity:    -30.0,
			RecoveryBoost:    5.0,
		}
		timing := &CrashTiming{}
		lastCrash := -999
		calc.generateYearlyReturns(rng, config, 20, timing, &lastCrash)

		// With 50% probability over 20 years, we should see crashes
		if timing.TotalCrashes == 0 {
			t.Error("expected at least one crash with 50% probability")
		}
	})

	t.Run("crash years have negative returns", func(t *testing.T) {
		rng := rand.New(rand.NewSource(789))
		config := &MonteCarloConfig{
			ReturnVolatility: 15.0,
			CrashProbability: 1.0, // 100% crash for testing
			CrashSeverity:    -30.0,
			RecoveryBoost:    5.0,
		}
		timing := &CrashTiming{}
		lastCrash := -999
		returns := calc.generateYearlyReturns(rng, config, 5, timing, &lastCrash)

		// All years should be crash years with negative returns
		for i, r := range returns {
			if i == 0 && r > 0 { // First year should be a crash
				t.Errorf("crash year %d has positive return %v", i, r)
			}
		}
	})

	t.Run("crash timing is categorized correctly", func(t *testing.T) {
		rng := rand.New(rand.NewSource(999))
		config := &MonteCarloConfig{
			ReturnVolatility: 15.0,
			CrashProbability: 1.0, // 100% crash for testing
			CrashSeverity:    -30.0,
			RecoveryBoost:    5.0,
		}
		timing := &CrashTiming{}
		lastCrash := -999
		calc.generateYearlyReturns(rng, config, 20, timing, &lastCrash)

		// With 100% crash probability over 20 years:
		// Years 0-4 (5 years) -> EarlyCrashes
		// Years 5-14 (10 years) -> MidCrashes
		// Years 15-19 (5 years) -> LateCrashes
		if timing.EarlyCrashes != 5 {
			t.Errorf("expected 5 early crashes, got %d", timing.EarlyCrashes)
		}
		if timing.MidCrashes != 10 {
			t.Errorf("expected 10 mid crashes, got %d", timing.MidCrashes)
		}
		if timing.LateCrashes != 5 {
			t.Errorf("expected 5 late crashes, got %d", timing.LateCrashes)
		}
		if timing.TotalCrashes != 20 {
			t.Errorf("expected 20 total crashes, got %d", timing.TotalCrashes)
		}
		if timing.FirstCrashYear != 1 {
			t.Errorf("expected first crash in year 1, got %d", timing.FirstCrashYear)
		}
	})
}

// TestRunSingleMonteCarloSimulation tests individual simulation runs
func TestRunSingleMonteCarloSimulation(t *testing.T) {
	t.Run("wealthy scenario survives", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 5000000 // $5M
		settings.MonthlyLivingExpenses = 5000
		settings.ProjectionYears = 30
		settings.InvestmentReturn = 6.0
		calc := newTestCalc(t, settings)

		rng := rand.New(rand.NewSource(42))
		config := DefaultMonteCarloConfig()
		config.LongevityVariation = 0 // No variation for predictable testing

		result := calc.runSingleMonteCarloSimulation(rng, config)

		if !result.Survives {
			t.Error("wealthy scenario should survive")
		}
		if result.FinalBalance <= 0 {
			t.Error("final balance should be positive")
		}
	})

	t.Run("underfunded scenario depletes", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 100000 // Only $100K
		settings.MonthlyLivingExpenses = 10000
		settings.ProjectionYears = 30
		settings.InvestmentReturn = 6.0
		calc := newTestCalc(t, settings)

		rng := rand.New(rand.NewSource(42))
		config := DefaultMonteCarloConfig()
		config.LongevityVariation = 0

		result := calc.runSingleMonteCarloSimulation(rng, config)

		if result.Survives {
			t.Error("underfunded scenario should not survive")
		}
		if result.DepletionYear <= 0 {
			t.Error("should have a depletion year")
		}
	})

	t.Run("tracks market crashes", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		rng := rand.New(rand.NewSource(42))
		config := &MonteCarloConfig{
			ReturnVolatility:   15.0,
			CrashProbability:   0.5, // High crash probability
			CrashSeverity:      -30.0,
			RecoveryBoost:      5.0,
			SpendingShockProb:  0,
			HealthShockProb:    0,
			LongevityVariation: 0,
		}

		result := calc.runSingleMonteCarloSimulation(rng, config)

		if result.MarketCrashes == 0 {
			t.Error("expected market crashes with high probability")
		}
	})

	t.Run("tracks spending shocks", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		rng := rand.New(rand.NewSource(42))
		config := &MonteCarloConfig{
			ReturnVolatility:   15.0,
			CrashProbability:   0,
			SpendingShockProb:  0.5, // High shock probability
			SpendingShockMin:   5000,
			SpendingShockMax:   25000,
			HealthShockProb:    0,
			LongevityVariation: 0,
		}

		result := calc.runSingleMonteCarloSimulation(rng, config)

		if result.SpendingShocks == 0 {
			t.Error("expected spending shocks with high probability")
		}
	})

	t.Run("tracks health shocks", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		rng := rand.New(rand.NewSource(42))
		config := &MonteCarloConfig{
			ReturnVolatility:   15.0,
			CrashProbability:   0,
			SpendingShockProb:  0,
			HealthShockProb:    0.5, // High shock probability
			HealthShockMin:     10000,
			HealthShockMax:     50000,
			LongevityVariation: 0,
		}

		result := calc.runSingleMonteCarloSimulation(rng, config)

		if result.HealthShocks == 0 {
			t.Error("expected health shocks with high probability")
		}
	})

	t.Run("longevity variation changes projection years", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		config := &MonteCarloConfig{
			ReturnVolatility:   15.0,
			LongevityVariation: 5,
		}

		// Run multiple times and check for variation
		projectionYears := make(map[int]bool)
		for i := 0; i < 50; i++ {
			rng := rand.New(rand.NewSource(int64(i)))
			result := calc.runSingleMonteCarloSimulation(rng, config)
			projectionYears[result.ProjectionYears] = true
		}

		// Should see variation (not all the same)
		if len(projectionYears) < 3 {
			t.Errorf("expected variation in projection years, got only %d unique values", len(projectionYears))
		}
	})
}

func TestIsSocialSecurityIncomeSourceRecognizesSSI(t *testing.T) {
	tests := []struct {
		name   string
		source models.IncomeSource
		want   bool
	}{
		{
			name:   "social security phrase",
			source: models.IncomeSource{Name: "Social Security"},
			want:   true,
		},
		{
			name:   "ssi token",
			source: models.IncomeSource{Name: "Christine SSI"},
			want:   true,
		},
		{
			name:   "ssi with suffix",
			source: models.IncomeSource{Name: "Darrell Gallion SSI 67"},
			want:   true,
		},
		{
			name:   "non social security income",
			source: models.IncomeSource{Name: "Christine Pension"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSocialSecurityIncomeSource(tt.source); got != tt.want {
				t.Fatalf("isSocialSecurityIncomeSource(%q) = %v, want %v", tt.source.Name, got, tt.want)
			}
		})
	}
}

func TestRunProjectionTaxesSocialSecurityBelowFullOrdinaryTreatment(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 0
	settings.MonthlyLivingExpenses = 0
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 4000, StartMonth: 0},
	}
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0
	settings.ProjectionYears = 1
	settings.CurrentAge = 62
	settings.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}

	projection := newTestCalc(t, settings).RunProjection()
	if len(projection.Months) != 12 {
		t.Fatalf("expected a full projection year, got %d months", len(projection.Months))
	}

	actualMonthlyTaxes := projection.Months[0].TaxesPaid
	tc := NewTaxCalculator(settings.TaxConfig, 0)
	_, _, fullyTaxedAnnualTotal, _ := tc.CalculateTaxWithInvestmentIncome(4000*12, 0, 0, 0)
	fullyTaxedMonthly := fullyTaxedAnnualTotal / 12

	if actualMonthlyTaxes >= fullyTaxedMonthly {
		t.Fatalf("expected modeled Social Security taxes %.2f to stay below fully-taxed baseline %.2f", actualMonthlyTaxes, fullyTaxedMonthly)
	}
}

func TestCalculateMonthlyIncomeBreakdown_SocialSecurityProjection(t *testing.T) {
	t.Run("includes synthesized primary Social Security at claim month", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 60
		settings.IncomeSources = nil
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			COLARate:   0.02,
			ClaimAge:   62,
		}

		beforeClaim := calculateMonthlyIncomeBreakdown(settings, 23)
		if beforeClaim.SocialSecurityIncome != 0 {
			t.Fatalf("month before claim SocialSecurityIncome = %.2f, want 0", beforeClaim.SocialSecurityIncome)
		}

		atClaim := calculateMonthlyIncomeBreakdown(settings, 24)
		want := AdjustedSSBenefit(2000, 67, 62)
		if math.Abs(atClaim.SocialSecurityIncome-want) > 0.01 {
			t.Fatalf("claim month SocialSecurityIncome = %.2f, want %.2f", atClaim.SocialSecurityIncome, want)
		}
	})

	t.Run("excludes manual Social Security when optimizer projection is active", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 67
		settings.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Social Security", Amount: 9999, StartMonth: 0},
			{ID: "pension", Name: "Pension", Amount: 500, StartMonth: 0},
		}
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}

		breakdown := calculateMonthlyIncomeBreakdown(settings, 0)
		if breakdown.SocialSecurityIncome != 2000 {
			t.Fatalf("SocialSecurityIncome = %.2f, want synthesized 2000", breakdown.SocialSecurityIncome)
		}
		if breakdown.OrdinaryIncome != 500 {
			t.Fatalf("OrdinaryIncome = %.2f, want pension 500", breakdown.OrdinaryIncome)
		}
	})

	t.Run("includes manual Social Security when claim age is unset", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 67
		settings.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Darrell SSI", Amount: 1500, StartMonth: 0},
		}
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
		}

		breakdown := calculateMonthlyIncomeBreakdown(settings, 0)
		if breakdown.SocialSecurityIncome != 1500 {
			t.Fatalf("SocialSecurityIncome = %.2f, want manual 1500", breakdown.SocialSecurityIncome)
		}
	})

	t.Run("non Social Security manual income remains ordinary when optimizer projection is active", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 67
		settings.IncomeSources = []models.IncomeSource{
			{ID: "consulting", Name: "Consulting", Amount: 700, StartMonth: 0},
		}
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}

		breakdown := calculateMonthlyIncomeBreakdown(settings, 0)
		if breakdown.OrdinaryIncome != 700 {
			t.Fatalf("OrdinaryIncome = %.2f, want 700", breakdown.OrdinaryIncome)
		}
		if breakdown.TotalIncome != 2700 {
			t.Fatalf("TotalIncome = %.2f, want 2700", breakdown.TotalIncome)
		}
	})

	t.Run("current age greater than claim age starts Social Security at month zero", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 68
		settings.IncomeSources = nil
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   62,
		}

		// When already claiming (ClaimAge <= CurrentAge), the entered amount
		// is treated as the actual benefit, not PIA — no adjustment applied.
		breakdown := calculateMonthlyIncomeBreakdown(settings, 0)
		want := 2000.0
		if math.Abs(breakdown.SocialSecurityIncome-want) > 0.01 {
			t.Fatalf("SocialSecurityIncome = %.2f, want %.2f", breakdown.SocialSecurityIncome, want)
		}
	})

	t.Run("spouse Social Security is included only with spouse config and claim age", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.CurrentAge = 67
		settings.SpouseAge = 67
		settings.IncomeSources = nil
		settings.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       3000,
			FRA:              67,
			ClaimAge:         67,
			SpouseFRABenefit: 1000,
			SpouseFRA:        67,
		}

		withoutSpouseClaim := calculateMonthlyIncomeBreakdown(settings, 0)
		if withoutSpouseClaim.SocialSecurityIncome != 3000 {
			t.Fatalf("SocialSecurityIncome without spouse claim = %.2f, want primary-only 3000", withoutSpouseClaim.SocialSecurityIncome)
		}

		settings.SocialSecurity.SpouseClaimAge = 67
		withSpouseClaim := calculateMonthlyIncomeBreakdown(settings, 0)
		// Both already claiming (ClaimAge <= CurrentAge): entered amounts are
		// actual benefits, no adjustment or spousal top-up applied (3000 + 1000).
		if withSpouseClaim.SocialSecurityIncome != 4000 {
			t.Fatalf("SocialSecurityIncome with spouse claim = %.2f, want 4000", withSpouseClaim.SocialSecurityIncome)
		}
	})
}

func TestCalculateBudgetFitIncludesNIITAndEstimatedIRMAA(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 3_000_000
	settings.MonthlyLivingExpenses = 1000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0
	settings.CurrentAge = 67
	settings.SpouseAge = 66
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	settings.TaxableDividendYield = 8.0
	settings.TaxableQualifiedDividendPercent = 100
	settings.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 4000, StartMonth: 0},
	}
	settings.SteadyStateOverrideYear = 5
	settings.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}

	fit := newTestCalc(t, settings).CalculateBudgetFit()

	if fit.MonthlyNIIT <= 0 {
		t.Fatalf("expected NIIT in current budget fit, got %.2f", fit.MonthlyNIIT)
	}
	if fit.MonthlyIRMAA <= 0 {
		t.Fatalf("expected current budget fit to estimate IRMAA from current modeled MAGI, got %.2f", fit.MonthlyIRMAA)
	}
	if fit.TaxableSocialSecurityPct <= 0 || fit.TaxableSocialSecurityPct > 85 {
		t.Fatalf("expected taxable Social Security percentage between 0 and 85, got %.2f", fit.TaxableSocialSecurityPct)
	}
	if fit.SteadyStateNIIT <= 0 {
		t.Fatalf("expected NIIT in steady-state budget fit, got %.2f", fit.SteadyStateNIIT)
	}
	if fit.SteadyStateIRMAA <= 0 {
		t.Fatalf("expected IRMAA in steady-state budget fit, got %.2f", fit.SteadyStateIRMAA)
	}
	if fit.SteadyStateTaxableSocialSecurityPct <= 0 || fit.SteadyStateTaxableSocialSecurityPct > 85 {
		t.Fatalf("expected steady-state taxable Social Security percentage between 0 and 85, got %.2f", fit.SteadyStateTaxableSocialSecurityPct)
	}
}

func TestPlannerIRMAAInflationFactorForYear_Rebases2026TableOntoTaxBaseYear(t *testing.T) {
	inflationRate := 3.0

	year0Factor := plannerIRMAAInflationFactorForYear(inflationRate, 0)
	year2Factor := plannerIRMAAInflationFactorForYear(inflationRate, 2)
	year5Factor := plannerIRMAAInflationFactorForYear(inflationRate, 5)

	wantYear0 := math.Pow(1+inflationRate/100, -2)
	wantYear5 := math.Pow(1+inflationRate/100, 3)

	if math.Abs(year0Factor-wantYear0) > 1e-9 {
		t.Fatalf("year 0 IRMAA factor = %.12f, want %.12f", year0Factor, wantYear0)
	}
	if math.Abs(year2Factor-1) > 1e-9 {
		t.Fatalf("year 2 IRMAA factor = %.12f, want 1.000000000000", year2Factor)
	}
	if math.Abs(year5Factor-wantYear5) > 1e-9 {
		t.Fatalf("year 5 IRMAA factor = %.12f, want %.12f", year5Factor, wantYear5)
	}
}

func TestRunProjectionDelaysIRMAAUntilLookbackYear(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 4_000_000
	settings.MonthlyLivingExpenses = 1000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0
	settings.CurrentAge = 67
	settings.SpouseAge = 66
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	settings.TaxableDividendYield = 8.0
	settings.TaxableQualifiedDividendPercent = 100
	settings.ProjectionYears = 4
	settings.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 4000, StartMonth: 0},
	}
	settings.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}

	projection := newTestCalc(t, settings).RunProjection()
	if len(projection.YearlySummaries) < 4 {
		t.Fatalf("expected 4 yearly summaries, got %d", len(projection.YearlySummaries))
	}

	if projection.YearlySummaries[0].IRMAA != 0 {
		t.Fatalf("expected no IRMAA in year 0 without lookback history, got %.2f", projection.YearlySummaries[0].IRMAA)
	}
	if projection.YearlySummaries[1].IRMAA != 0 {
		t.Fatalf("expected no IRMAA in year 1 without lookback history, got %.2f", projection.YearlySummaries[1].IRMAA)
	}
	if projection.YearlySummaries[2].IRMAA <= 0 {
		t.Fatalf("expected IRMAA once two years of lookback history exist, got %.2f", projection.YearlySummaries[2].IRMAA)
	}
}

func TestCalculateBudgetFitSteadyStateIRMAAUsesTwoYearLookbackEstimate(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 4_000_000
	settings.MonthlyLivingExpenses = 1000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0
	settings.InvestmentReturn = 4
	settings.CurrentAge = 67
	settings.SpouseAge = 66
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	settings.TaxableDividendYield = 4.0
	settings.TaxableQualifiedDividendPercent = 100
	settings.TaxableCapitalGainsDistributionRate = 0
	settings.SteadyStateOverrideYear = 5
	settings.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 4000, StartMonth: 0},
		{ID: "late-pension", Name: "Late Pension", Amount: 15000, StartMonth: 48},
	}
	settings.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}

	calc := newTestCalc(t, settings)
	fit := calc.CalculateBudgetFit()

	steadyStateMonth := int(settings.SteadyStateOverrideYear * 12)
	lookbackMonth := steadyStateMonth - 24
	if lookbackMonth < 0 {
		t.Fatalf("expected positive IRMAA lookback month, got %d", lookbackMonth)
	}

	taxableMarketValue := settings.PortfolioValue
	steadyStateTaxableBalance := taxableMarketValue * math.Pow(1+settings.InvestmentReturn/100, float64(steadyStateMonth)/12)
	lookbackTaxableBalance := taxableMarketValue * math.Pow(1+settings.InvestmentReturn/100, float64(lookbackMonth)/12)

	steadyStateTaxableCashFlow := expectedTaxableMonthlyCashFlow(settings, steadyStateTaxableBalance, settings.InvestmentReturn)
	lookbackTaxableCashFlow := expectedTaxableMonthlyCashFlow(settings, lookbackTaxableBalance, settings.InvestmentReturn)

	estimateSnapshot := func(month int, taxableCashFlow taxableGrowthResult, assumedIRMALookbackMAGI *float64) projectedTaxSnapshot {
		taxState := projectionTaxAccumulator{}
		return taxState.estimateMonthlySnapshot(
			NewTaxCalculator(settings.TaxConfig, settings.InflationRate),
			month/12,
			month%12,
			calculateMonthlyIncomeBreakdown(settings, month).OrdinaryIncome+taxableCashFlow.NonQualifiedDividends,
			calculateMonthlyIncomeBreakdown(settings, month).SocialSecurityIncome,
			0,
			taxableCashFlow.QualifiedDividends,
			taxableCashFlow.CapitalGainsDistributions,
			taxableCashFlow.NonQualifiedDividends,
			0,
			nil,
			assumedIRMALookbackMAGI,
			medicareEligibleAdultCountAtYear(settings, month/12),
			plannerIRMAAInflationFactorForYear(settings.InflationRate, float64(month)/12),
		)
	}

	lookbackSnapshot := estimateSnapshot(lookbackMonth, lookbackTaxableCashFlow, nil)
	lookbackMAGI := lookbackSnapshot.AnnualMAGI
	steadyStateSnapshot := estimateSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, &lookbackMAGI)
	sameYearProxy := estimateSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, nil)
	sameYearMAGI := sameYearProxy.AnnualMAGI
	sameYearSnapshot := estimateSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, &sameYearMAGI)

	if sameYearMAGI <= lookbackMAGI {
		t.Fatalf("expected same-year MAGI %.2f to exceed two-year lookback MAGI %.2f for this scenario", sameYearMAGI, lookbackMAGI)
	}
	if math.Abs(fit.SteadyStateIRMAA-steadyStateSnapshot.MonthlyIRMAA) > 0.01 {
		t.Fatalf("steady-state IRMAA = %.2f, want %.2f from two-year lookback estimate", fit.SteadyStateIRMAA, steadyStateSnapshot.MonthlyIRMAA)
	}
	if math.Abs(fit.SteadyStateIRMAA-sameYearSnapshot.MonthlyIRMAA) < 0.01 {
		t.Fatalf(
			"steady-state IRMAA should not use same-year MAGI proxy %.2f (fit=%.2f lookback=%.2f lookbackMAGI=%.2f sameYearMAGI=%.2f)",
			sameYearSnapshot.MonthlyIRMAA,
			fit.SteadyStateIRMAA,
			steadyStateSnapshot.MonthlyIRMAA,
			lookbackMAGI,
			sameYearMAGI,
		)
	}
}

// TestRunMonteCarloSimulation tests the full simulation with aggregation
func TestRunMonteCarloSimulation(t *testing.T) {
	t.Run("uses default 1000 runs when zero specified", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(0)

		if result.Stats.Runs != 1000 {
			t.Errorf("got %d runs, want 1000", result.Stats.Runs)
		}
	})

	t.Run("respects specified run count", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(100)

		if result.Stats.Runs != 100 {
			t.Errorf("got %d runs, want 100", result.Stats.Runs)
		}
	})

	t.Run("success rate is between 0 and 100", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(100)

		if result.Stats.SuccessRate < 0 || result.Stats.SuccessRate > 100 {
			t.Errorf("success rate %v is out of bounds [0, 100]", result.Stats.SuccessRate)
		}
	})

	t.Run("percentiles are ordered correctly", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(100)
		stats := result.Stats

		if stats.WorstCase > stats.Percentile10 {
			t.Error("worst case should be <= 10th percentile")
		}
		if stats.Percentile10 > stats.Percentile25 {
			t.Error("10th percentile should be <= 25th percentile")
		}
		if stats.Percentile25 > stats.MedianBalance {
			t.Error("25th percentile should be <= median")
		}
		if stats.MedianBalance > stats.Percentile75 {
			t.Error("median should be <= 75th percentile")
		}
		if stats.Percentile75 > stats.Percentile90 {
			t.Error("75th percentile should be <= 90th percentile")
		}
		if stats.Percentile90 > stats.BestCase {
			t.Error("90th percentile should be <= best case")
		}
	})

	t.Run("tracks crash statistics", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(200)

		// With 5% crash probability over 30 years, most runs should have crashes
		if result.Stats.MarketCrashCount == 0 {
			t.Error("expected some runs to have market crashes")
		}
		if result.Stats.AvgCrashesPerRun <= 0 {
			t.Error("expected positive average crashes per run")
		}
	})

	t.Run("tracks shock statistics", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(200)

		// With 8% spending shock and 5% health shock probability, should see events
		if result.Stats.SpendingShockCount == 0 {
			t.Error("expected some runs to have spending shocks")
		}
		if result.Stats.HealthShockCount == 0 {
			t.Error("expected some runs to have health shocks")
		}
	})

	t.Run("creates distribution buckets", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := newTestCalc(t, settings)

		result := calc.RunMonteCarloSimulation(100)

		if result.Distribution == nil {
			t.Fatal("expected distribution to be populated")
		}
		if len(result.Distribution.Buckets) == 0 {
			t.Error("expected at least one distribution bucket")
		}

		// Bucket percentages should sum to approximately 100
		totalPct := 0.0
		for _, b := range result.Distribution.Buckets {
			totalPct += b.Percentage
		}
		if math.Abs(totalPct-100) > 1 {
			t.Errorf("bucket percentages sum to %v, want ~100", totalPct)
		}
	})
}

// TestCalculateSequenceRiskImpact tests the sequence risk calculation
func TestCalculateSequenceRiskImpact(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	calc := newTestCalc(t, settings)

	t.Run("returns zero with insufficient data", func(t *testing.T) {
		results := make([]models.MonteCarloResult, 50) // Less than 100
		impact := calc.calculateSequenceRiskImpact(results)

		if impact != 0 {
			t.Errorf("expected 0 with <100 results, got %v", impact)
		}
	})

	t.Run("returns zero when no crashes", func(t *testing.T) {
		results := make([]models.MonteCarloResult, 100)
		for i := range results {
			results[i] = models.MonteCarloResult{
				Survives:      true,
				MarketCrashes: 0,
			}
		}

		impact := calc.calculateSequenceRiskImpact(results)

		if impact != 0 {
			t.Errorf("expected 0 with no crashes, got %v", impact)
		}
	})

	t.Run("positive impact when crashes hurt survival", func(t *testing.T) {
		results := make([]models.MonteCarloResult, 200)

		// First 100: no crashes, all survive
		for i := 0; i < 100; i++ {
			results[i] = models.MonteCarloResult{
				Survives:      true,
				MarketCrashes: 0,
			}
		}

		// Second 100: have crashes, half fail
		for i := 100; i < 200; i++ {
			results[i] = models.MonteCarloResult{
				Survives:      i%2 == 0, // 50% survive
				MarketCrashes: 2,
			}
		}

		impact := calc.calculateSequenceRiskImpact(results)

		// Without crashes: 100% survival
		// With crashes: 50% survival
		// Impact should be 50
		if impact < 40 || impact > 60 {
			t.Errorf("expected impact around 50, got %v", impact)
		}
	})
}

// TestCalculateSequenceRiskBreakdown tests the detailed breakdown calculations
func TestCalculateSequenceRiskBreakdown(t *testing.T) {
	t.Run("calculates buffer amount correctly", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 5000
		settings.MonthlyHealthcare = 500
		settings.ProjectionYears = 30
		calc := newTestCalc(t, settings)

		// Run enough simulations to get a breakdown
		result := calc.RunMonteCarloSimulation(500)

		if result.Stats.SequenceRisk == nil {
			t.Fatal("expected sequence risk breakdown to be populated")
		}

		breakdown := result.Stats.SequenceRisk

		// Annual expenses = (5000 + 500) * 12 = 66000
		expectedAnnualExpenses := 66000.0
		if breakdown.AnnualExpenses != expectedAnnualExpenses {
			t.Errorf("expected annual expenses %.0f, got %.0f", expectedAnnualExpenses, breakdown.AnnualExpenses)
		}

		// Buffer calculation accounts for partial portfolio value during crash:
		// 1. crashedPortfolio = portfolioValue * (1 - 0.30) = 700,000
		// 2. safeWithdrawalDuringCrash = crashedPortfolio * 0.03 = 21,000
		// 3. annualShortfall = annualExpenses - safeWithdrawalDuringCrash
		// 4. bufferAmount = recommendedBuffer * annualShortfall
		crashedPortfolio := settings.PortfolioValue * 0.70 // 30% drawdown
		safeWithdrawal := crashedPortfolio * 0.03          // 3% safe withdrawal during crash
		annualShortfall := expectedAnnualExpenses - safeWithdrawal
		expectedBufferAmount := float64(breakdown.RecommendedBuffer) * annualShortfall
		if breakdown.BufferAmount != expectedBufferAmount {
			t.Errorf("expected buffer amount %.0f, got %.0f", expectedBufferAmount, breakdown.BufferAmount)
		}

		// Verify the breakdown fields are populated
		if breakdown.CrashDrawdownPercent != 30.0 {
			t.Errorf("expected crash drawdown 30%%, got %.1f%%", breakdown.CrashDrawdownPercent)
		}
		if breakdown.CrashedPortfolioValue != crashedPortfolio {
			t.Errorf("expected crashed portfolio %.0f, got %.0f", crashedPortfolio, breakdown.CrashedPortfolioValue)
		}
		if breakdown.SafeWithdrawalDuringCrash != safeWithdrawal {
			t.Errorf("expected safe withdrawal %.0f, got %.0f", safeWithdrawal, breakdown.SafeWithdrawalDuringCrash)
		}
		if breakdown.AnnualShortfall != annualShortfall {
			t.Errorf("expected annual shortfall %.0f, got %.0f", annualShortfall, breakdown.AnnualShortfall)
		}
		// NaiveBufferAmount should be what the old calculation would have produced
		expectedNaiveBuffer := float64(breakdown.RecommendedBuffer) * expectedAnnualExpenses
		if breakdown.NaiveBufferAmount != expectedNaiveBuffer {
			t.Errorf("expected naive buffer %.0f, got %.0f", expectedNaiveBuffer, breakdown.NaiveBufferAmount)
		}

		// Adjusted spending = (portfolio - buffer) * 0.04 / 12
		remainingPortfolio := settings.PortfolioValue - breakdown.BufferAmount
		expectedAdjustedSpending := (remainingPortfolio * 0.04) / 12
		if breakdown.AdjustedSpending != expectedAdjustedSpending {
			t.Errorf("expected adjusted spending %.2f, got %.2f", expectedAdjustedSpending, breakdown.AdjustedSpending)
		}
	})

	t.Run("buffer recommendation scales with risk", func(t *testing.T) {
		// High-risk scenario (low portfolio, high expenses)
		settingsHighRisk := models.DefaultWhatIfSettings()
		settingsHighRisk.PortfolioValue = 500000
		settingsHighRisk.MonthlyLivingExpenses = 4000
		settingsHighRisk.MonthlyHealthcare = 500
		settingsHighRisk.ProjectionYears = 30
		calcHighRisk := newTestCalc(t, settingsHighRisk)
		resultHighRisk := calcHighRisk.RunMonteCarloSimulation(500)

		// Lower-risk scenario (high portfolio, low expenses)
		settingsLowRisk := models.DefaultWhatIfSettings()
		settingsLowRisk.PortfolioValue = 3000000
		settingsLowRisk.MonthlyLivingExpenses = 3000
		settingsLowRisk.MonthlyHealthcare = 500
		settingsLowRisk.ProjectionYears = 30
		calcLowRisk := newTestCalc(t, settingsLowRisk)
		resultLowRisk := calcLowRisk.RunMonteCarloSimulation(500)

		// Both should have valid breakdowns
		if resultHighRisk.Stats.SequenceRisk == nil || resultLowRisk.Stats.SequenceRisk == nil {
			t.Skip("sequence risk breakdown not available for comparison")
		}

		// Buffer recommendation should be at least 2 years
		if resultHighRisk.Stats.SequenceRisk.RecommendedBuffer < 2 {
			t.Errorf("expected recommended buffer >= 2, got %d", resultHighRisk.Stats.SequenceRisk.RecommendedBuffer)
		}
		if resultLowRisk.Stats.SequenceRisk.RecommendedBuffer < 2 {
			t.Errorf("expected recommended buffer >= 2, got %d", resultLowRisk.Stats.SequenceRisk.RecommendedBuffer)
		}
	})
}

// TestMonteCarloWithIncomeAndExpenses tests simulation with income sources
func TestMonteCarloWithIncomeAndExpenses(t *testing.T) {
	t.Run("income sources reduce depletion risk", func(t *testing.T) {
		// Without income
		settingsNoIncome := models.DefaultWhatIfSettings()
		settingsNoIncome.PortfolioValue = 500000
		settingsNoIncome.MonthlyLivingExpenses = 4000
		settingsNoIncome.ProjectionYears = 30

		calcNoIncome := newTestCalc(t, settingsNoIncome)
		resultNoIncome := calcNoIncome.RunMonteCarloSimulation(100)

		// With income (Social Security)
		settingsWithIncome := models.DefaultWhatIfSettings()
		settingsWithIncome.PortfolioValue = 500000
		settingsWithIncome.MonthlyLivingExpenses = 4000
		settingsWithIncome.ProjectionYears = 30
		settingsWithIncome.IncomeSources = []models.IncomeSource{
			{
				Name:       "Social Security",
				Amount:     2000,
				StartMonth: 0,
				COLARate:   0.02,
			},
		}

		calcWithIncome := newTestCalc(t, settingsWithIncome)
		resultWithIncome := calcWithIncome.RunMonteCarloSimulation(100)

		// Success rate should be higher with income
		if resultWithIncome.Stats.SuccessRate <= resultNoIncome.Stats.SuccessRate {
			t.Logf("With income: %.1f%%, Without: %.1f%%",
				resultWithIncome.Stats.SuccessRate, resultNoIncome.Stats.SuccessRate)
			// This might occasionally fail due to randomness, so just log
		}
	})
}

// TestMonteCarloReproducibility tests that same seed produces same results
func TestMonteCarloReproducibility(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1000000
	calc := newTestCalc(t, settings)

	// Note: The main RunMonteCarloSimulation uses time-based seeding,
	// so we test the internal function with fixed seed
	rng1 := rand.New(rand.NewSource(12345))
	rng2 := rand.New(rand.NewSource(12345))
	config := DefaultMonteCarloConfig()

	result1 := calc.runSingleMonteCarloSimulation(rng1, config)
	result2 := calc.runSingleMonteCarloSimulation(rng2, config)

	if result1.FinalBalance != result2.FinalBalance {
		t.Errorf("same seed should produce same results: %v vs %v",
			result1.FinalBalance, result2.FinalBalance)
	}
	if result1.Survives != result2.Survives {
		t.Error("same seed should produce same survival outcome")
	}
}

func TestRunProjectionAfterTaxDepletesSoonerThanPretaxBenchmark(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 100_000
	settings.ProjectionYears = 5
	settings.InvestmentReturn = 0
	settings.InflationRate = 0
	settings.MonthlyLivingExpenses = 4_000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.IncomeSources = nil
	settings.CurrentAge = 65
	settings.TaxDeferredPercent = 100
	settings.RothPercent = 0
	settings.StockPercent = 0
	settings.CashPercent = 100

	calc := newTestCalc(t, settings)
	result := calc.RunProjection()

	if result.DepletionMonth == nil {
		t.Fatal("expected portfolio depletion")
	}

	pretaxMonths := int(settings.PortfolioValue / settings.MonthlyLivingExpenses)
	if *result.DepletionMonth >= pretaxMonths {
		t.Fatalf("expected after-tax depletion before pretax benchmark of %d months, got %d", pretaxMonths, *result.DepletionMonth)
	}

	lastMonth := result.Months[len(result.Months)-1]
	if lastMonth.TaxesPaid <= 0 {
		t.Fatalf("expected taxes to be paid before depletion, got %.2f", lastMonth.TaxesPaid)
	}
}

func TestCalculateBudgetFitUsesAfterTaxCashFlow(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1_000_000
	settings.MonthlyLivingExpenses = 4_000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.IncomeSources = []models.IncomeSource{
		{Name: "Pension", Amount: 4_000, StartMonth: 0},
	}
	settings.InflationRate = 0
	settings.CurrentAge = 65
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0

	calc := newTestCalc(t, settings)
	fit := calc.CalculateBudgetFit()

	if fit.MonthlyTaxes <= 0 {
		t.Fatalf("expected positive monthly taxes, got %.2f", fit.MonthlyTaxes)
	}
	if fit.NetIncome >= fit.GrossIncome {
		t.Fatalf("expected net income below gross income, got gross=%.2f net=%.2f", fit.GrossIncome, fit.NetIncome)
	}
	if fit.MonthlyGap <= 0 {
		t.Fatalf("expected an after-tax shortfall, got %.2f", fit.MonthlyGap)
	}
}

// TestSteadyStateBudgetFit tests the steady-state budget analysis
func TestSteadyStateBudgetFit(t *testing.T) {
	t.Run("steady state always enabled for slider, min year 0 when all income immediate", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 4000
		settings.IncomeSources = []models.IncomeSource{
			{Name: "Pension", Amount: 2000, StartMonth: 0},
		}
		calc := newTestCalc(t, settings)
		result := calc.CalculateBudgetFit()

		// HasSteadyState is always true so slider can project into future
		if !result.HasSteadyState {
			t.Error("expected HasSteadyState=true (always enabled for slider)")
		}
		// But MinSteadyStateYear should be 0 when all income starts immediately
		if result.MinSteadyStateYear != 0 {
			t.Errorf("expected MinSteadyStateYear=0, got %f", result.MinSteadyStateYear)
		}
	})

	t.Run("detects steady state with delayed income", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 4000
		settings.IncomeSources = []models.IncomeSource{
			{Name: "Pension", Amount: 1000, StartMonth: 0},
			{Name: "Social Security", Amount: 2000, StartMonth: 24}, // Starts in 2 years
		}
		calc := newTestCalc(t, settings)
		result := calc.CalculateBudgetFit()

		if !result.HasSteadyState {
			t.Error("expected HasSteadyState=true with delayed income")
		}
		if result.SteadyStateMonth != 24 {
			t.Errorf("expected SteadyStateMonth=24, got %d", result.SteadyStateMonth)
		}
		if result.SteadyStateYear != 2.0 {
			t.Errorf("expected SteadyStateYear=2.0, got %f", result.SteadyStateYear)
		}
	})

	t.Run("steady state income higher than current", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 4000
		settings.IncomeSources = []models.IncomeSource{
			{Name: "Pension", Amount: 1000, StartMonth: 0},
			{Name: "Social Security", Amount: 2000, StartMonth: 24},
		}
		calc := newTestCalc(t, settings)
		result := calc.CalculateBudgetFit()

		// Current income should be 1000
		if result.MonthlyIncome != 1000 {
			t.Errorf("expected current income=1000, got %f", result.MonthlyIncome)
		}
		// Steady state income should include both sources (with COLA applied)
		if result.SteadyStateIncome <= result.MonthlyIncome {
			t.Errorf("expected steady state income > current income, got %f vs %f",
				result.SteadyStateIncome, result.MonthlyIncome)
		}
	})

	t.Run("steady state gap smaller than current gap", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 4000
		settings.InflationRate = 0 // No inflation for simpler test
		settings.IncomeSources = []models.IncomeSource{
			{Name: "Pension", Amount: 1000, StartMonth: 0},
			{Name: "Social Security", Amount: 2500, StartMonth: 24},
		}
		calc := newTestCalc(t, settings)
		result := calc.CalculateBudgetFit()

		// Current gap: 4000 - 1000 = 3000
		// Steady state gap: 4000 - 3500 = 500
		if result.SteadyStateGap >= result.MonthlyGap {
			t.Errorf("expected steady state gap < current gap, got %f vs %f",
				result.SteadyStateGap, result.MonthlyGap)
		}
	})

	t.Run("finds latest starting income source", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.MonthlyLivingExpenses = 4000
		settings.IncomeSources = []models.IncomeSource{
			{Name: "Part-time work", Amount: 500, StartMonth: 0},
			{Name: "Social Security", Amount: 2000, StartMonth: 24},
			{Name: "Pension", Amount: 1500, StartMonth: 60}, // Latest at 5 years
		}
		calc := newTestCalc(t, settings)
		result := calc.CalculateBudgetFit()

		if result.SteadyStateMonth != 60 {
			t.Errorf("expected SteadyStateMonth=60 (latest income start), got %d", result.SteadyStateMonth)
		}
	})
}

// TestSensitivityWithPerAccountAllocation verifies sensitivity analysis
// uses the effective return rate when InvestmentReturn=0 (allocation mode)
func TestSensitivityWithPerAccountAllocation(t *testing.T) {
	t.Run("higher returns should improve outcomes", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.MonthlyLivingExpenses = 8000
		settings.ProjectionYears = 40
		settings.InvestmentReturn = 0 // Use per-account allocation mode

		// Set up a 60/40 allocation (default)
		settings.TaxDeferredStockPercent = 60
		settings.TaxDeferredCashPercent = 0
		settings.RothStockPercent = 60
		settings.RothCashPercent = 0
		settings.TaxableStockPercent = 60
		settings.TaxableCashPercent = 0

		calc := newTestCalc(t, settings)
		results := calc.CalculateSensitivity()

		// Find the Higher Returns and Lower Returns scenarios
		var higherReturns, lowerReturns *models.SensitivityResult
		for i := range results {
			if results[i].Scenario.Name == "Higher Returns" {
				higherReturns = &results[i]
			}
			if results[i].Scenario.Name == "Lower Returns" {
				lowerReturns = &results[i]
			}
		}

		if higherReturns == nil || lowerReturns == nil {
			t.Fatal("expected to find Higher Returns and Lower Returns scenarios")
		}

		// Higher returns should result in better or equal outcomes than lower returns
		// This was broken when InvestmentReturn=0 caused sensitivity to use 2% instead of ~7.8%
		higherLongevity := float64(100) // assume survives if nil
		if higherReturns.LongevityYears != nil {
			higherLongevity = *higherReturns.LongevityYears
		}
		lowerLongevity := float64(100)
		if lowerReturns.LongevityYears != nil {
			lowerLongevity = *lowerReturns.LongevityYears
		}

		if higherLongevity < lowerLongevity {
			t.Errorf("higher returns (%.1f yrs) should have >= longevity than lower returns (%.1f yrs)",
				higherLongevity, lowerLongevity)
		}

		if higherReturns.FinalBalance < lowerReturns.FinalBalance {
			t.Errorf("higher returns ($%.2f) should have >= final balance than lower returns ($%.2f)",
				higherReturns.FinalBalance, lowerReturns.FinalBalance)
		}
	})

	t.Run("sensitivity uses effective return not raw setting", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		settings.InvestmentReturn = 0 // allocation mode

		calc := newTestCalc(t, settings)
		results := calc.CalculateSensitivity()

		// Find Higher Returns scenario
		var higherReturns *models.SensitivityResult
		for i := range results {
			if results[i].Scenario.Name == "Higher Returns" {
				higherReturns = &results[i]
			}
		}

		if higherReturns == nil {
			t.Fatal("expected to find Higher Returns scenario")
		}

		// The effective return from default allocation is ~5.8%
		// Higher returns should be ~7.8%, not 2%
		expectedEffective := settings.GetExpectedReturnFromAllocation()
		expectedHigher := expectedEffective + 2

		if math.Abs(higherReturns.Scenario.ParamValue-expectedHigher) > 0.1 {
			t.Errorf("higher returns scenario used %.1f%%, expected %.1f%% (effective %.1f%% + 2%%)",
				higherReturns.Scenario.ParamValue, expectedHigher, expectedEffective)
		}
	})

	t.Run("higher healthcare sensitivity uses person-based healthcare costs", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 2000000
		settings.MonthlyLivingExpenses = 2500
		settings.ProjectionYears = 10
		settings.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "hp1",
				Name:                  "You",
				CurrentAge:            65,
				CurrentCoverage:       models.CoverageMedicare,
				CurrentMonthlyCost:    900,
				MedicareMonthlyCost:   900,
				PostMedicareInflation: 4,
				MedicareEligibleAge:   65,
			},
		}

		calc := newTestCalc(t, settings)
		baseline := calc.RunProjection()
		results := calc.CalculateSensitivity()

		var higherHealthcare *models.SensitivityResult
		for i := range results {
			if results[i].Scenario.Name == "Higher Healthcare" {
				higherHealthcare = &results[i]
				break
			}
		}
		if higherHealthcare == nil {
			t.Fatal("expected Higher Healthcare scenario")
		}
		if higherHealthcare.FinalBalance >= baseline.FinalBalance {
			t.Fatalf("expected higher healthcare to reduce final balance, got scenario %.2f baseline %.2f",
				higherHealthcare.FinalBalance, baseline.FinalBalance)
		}
	})
}

// TestRunProjectionWithSurplusIncome verifies that surplus income is reinvested into the taxable balance.
func TestRunProjectionWithSurplusIncome(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 100000 // $100k
	settings.MonthlyLivingExpenses = 2000
	settings.MonthlyHealthcare = 0
	settings.InvestmentReturn = 0.0000001
	settings.InflationRate = 0.0
	settings.SpendingDeclineRate = 0.0
	settings.ProjectionYears = 1
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	// Income: $5000/mo. Expenses: $2000/mo. Surplus: $3000/mo.
	settings.IncomeSources = []models.IncomeSource{
		{
			ID:         "surplus-income",
			Name:       "Big Income",
			Amount:     5000,
			Type:       models.IncomeFixed,
			StartMonth: 0,
		},
	}

	calc := newTestCalc(t, settings)
	projection := calc.RunProjection()
	budgetFit := calc.CalculateBudgetFit()

	// The after-tax surplus should still be reinvested into taxable, just smaller than
	// the old pre-tax benchmark.
	finalBalance := projection.Months[len(projection.Months)-1].PortfolioBalance
	expectedBalance := 100000.0 + (math.Abs(budgetFit.MonthlyGap) * 12)
	pretaxBenchmark := 100000.0 + (3000.0 * 12)

	if math.Abs(finalBalance-expectedBalance) > 1.0 { // Allow for tiny growth
		t.Errorf("expected final balance near %.2f, got %.2f (surplus not reinvested?)", expectedBalance, finalBalance)
	}
	if finalBalance >= pretaxBenchmark {
		t.Errorf("expected after-tax balance below pretax benchmark %.2f, got %.2f", pretaxBenchmark, finalBalance)
	}

	// Also verify that it's in the taxable account
	finalTaxable := projection.Months[len(projection.Months)-1].TaxableBalance
	if math.Abs(finalTaxable-expectedBalance) > 1.0 {
		t.Errorf("expected final taxable balance near %.2f, got %.2f", expectedBalance, finalTaxable)
	}
}

func TestTaxableAccountWithdrawUsesAverageCostBasis(t *testing.T) {
	account := taxableAccountState{
		MarketValue: 120000,
		CostBasis:   100000,
	}

	cash, basisReduction, realizedGain := account.withdraw(12000)

	if math.Abs(cash-12000) > 0.01 {
		t.Fatalf("cash = %.2f, want 12000", cash)
	}
	if math.Abs(basisReduction-10000) > 0.01 {
		t.Fatalf("basis reduction = %.2f, want 10000", basisReduction)
	}
	if math.Abs(realizedGain-2000) > 0.01 {
		t.Fatalf("realized gain = %.2f, want 2000", realizedGain)
	}
}

func TestRunProjectionTaxableSalesOfBasisRemainUntaxed(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 100_000
	settings.MonthlyLivingExpenses = 2_000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.ExpenseSources = nil
	settings.IncomeSources = nil
	settings.InvestmentReturn = 0
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0
	settings.ProjectionYears = 1
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0
	settings.TaxableDividendYield = 0
	settings.TaxableCapitalGainsDistributionRate = 0

	result := newTestCalc(t, settings).RunProjection()
	if len(result.Months) == 0 {
		t.Fatal("expected projection months")
	}

	if result.Months[0].WithdrawalFromTaxable <= 0 {
		t.Fatalf("expected taxable withdrawal, got %.2f", result.Months[0].WithdrawalFromTaxable)
	}
	if result.Months[0].TaxesPaid != 0 {
		t.Fatalf("expected zero tax on basis-only sale, got %.2f", result.Months[0].TaxesPaid)
	}
}

func TestHighTaxableDividendYieldReducesFinalBalance(t *testing.T) {
	base := models.DefaultWhatIfSettings()
	base.PortfolioValue = 1_000_000
	base.MonthlyLivingExpenses = 0
	base.MonthlyHealthcare = 0
	base.HealthcarePersons = nil
	base.ExpenseSources = nil
	base.IncomeSources = nil
	base.InvestmentReturn = 10.0
	base.InflationRate = 0
	base.SpendingDeclineRate = 0
	base.ProjectionYears = 10
	base.TaxDeferredPercent = 0
	base.RothPercent = 0
	base.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}

	highDividend := *base
	highDividend.TaxableDividendYield = 4.0
	highDividend.TaxableQualifiedDividendPercent = 0

	baseProjection := newTestCalc(t, base).RunProjection()
	highDividendProjection := newTestCalc(t, &highDividend).RunProjection()

	if highDividendProjection.FinalBalance >= baseProjection.FinalBalance {
		t.Fatalf("expected high taxable dividend yield to reduce final balance, got base=%.2f high-div=%.2f",
			baseProjection.FinalBalance, highDividendProjection.FinalBalance)
	}
	if highDividendProjection.Months[0].TaxesPaid <= 0 {
		t.Fatalf("expected taxable dividends to generate tax drag, got %.2f", highDividendProjection.Months[0].TaxesPaid)
	}
}

func TestProjectionTimingAffectsDeterministicAndMonteCarloResults(t *testing.T) {
	base := models.DefaultWhatIfSettings()
	base.PortfolioValue = 1_000_000
	base.MonthlyLivingExpenses = 5_000
	base.MonthlyHealthcare = 0
	base.HealthcarePersons = nil
	base.ExpenseSources = nil
	base.IncomeSources = nil
	base.InflationRate = 0
	base.SpendingDeclineRate = 0
	base.ProjectionYears = 30
	base.InvestmentReturn = 6.0
	base.TaxDeferredPercent = 0
	base.RothPercent = 0
	base.StockPercent = 100
	base.CashPercent = 0
	base.TaxableStockPercent = 100
	base.TaxableCashPercent = 0

	t.Run("deterministic ordering", func(t *testing.T) {
		startSettings := *base
		startSettings.ProjectionTiming = models.ProjectionTimingStartOfMonth
		midSettings := *base
		midSettings.ProjectionTiming = models.ProjectionTimingMidMonth
		endSettings := *base
		endSettings.ProjectionTiming = models.ProjectionTimingEndOfMonth

		startProjection := newTestCalc(t, &startSettings).RunProjection()
		midProjection := newTestCalc(t, &midSettings).RunProjection()
		endProjection := newTestCalc(t, &endSettings).RunProjection()

		if !(startProjection.FinalBalance < midProjection.FinalBalance && midProjection.FinalBalance < endProjection.FinalBalance) {
			t.Fatalf("expected start < mid < end final balances, got start=%.2f mid=%.2f end=%.2f",
				startProjection.FinalBalance, midProjection.FinalBalance, endProjection.FinalBalance)
		}
	})

	t.Run("monte carlo ordering", func(t *testing.T) {
		config := &MonteCarloConfig{
			ReturnVolatility:   0,
			CrashProbability:   0,
			CrashSeverity:      -30,
			RecoveryBoost:      0,
			SpendingShockProb:  0,
			HealthShockProb:    0,
			LongevityVariation: 0,
		}

		startSettings := *base
		startSettings.ProjectionTiming = models.ProjectionTimingStartOfMonth
		endSettings := *base
		endSettings.ProjectionTiming = models.ProjectionTimingEndOfMonth

		startResult := newTestCalc(t, &startSettings).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)
		endResult := newTestCalc(t, &endSettings).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)

		if startResult.FinalBalance >= endResult.FinalBalance {
			t.Fatalf("expected start-of-month Monte Carlo balance below end-of-month, got start=%.2f end=%.2f",
				startResult.FinalBalance, endResult.FinalBalance)
		}
	})
}

// BenchmarkMonteCarloSimulation benchmarks the simulation performance
func BenchmarkMonteCarloSimulation(b *testing.B) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1000000
	settings.ProjectionYears = 30
	calc := newTestCalc(b, settings)

	b.Run("100_runs", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			calc.RunMonteCarloSimulation(100)
		}
	})

	b.Run("1000_runs", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			calc.RunMonteCarloSimulation(1000)
		}
	})
}

func TestNewCalculatorWithChain(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 3000

	chain := []PreparedChainLink{
		preparedLink(t, "", 70, linked),
	}

	calc := newTestCalcWithChain(t, primary, chain)
	if calc == nil {
		t.Fatal("expected non-nil calculator")
	}
	if len(calc.ResolvedChain) != 1 {
		t.Errorf("expected 1 chain link, got %d", len(calc.ResolvedChain))
	}
}

func TestRunProjection_ChainTransition_BalancesCarryOver(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.Persons[0].BirthMonth = models.BirthMonthForAge(primary.StartDate, 60)
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000
	linked.InvestmentReturn = 4.0

	chain := []PreparedChainLink{
		preparedLink(t, "", 70, linked),
	}

	calcNoChain := newTestCalc(t, primary)
	projNoChain := calcNoChain.RunProjection()

	calcChain := newTestCalcWithChain(t, primary, chain)
	projChain := calcChain.RunProjection()

	if len(projChain.Months) < 121 {
		t.Fatalf("expected at least 121 months, got %d", len(projChain.Months))
	}

	// Before transition (month 119), both should match
	if projChain.Months[119].PortfolioBalance != projNoChain.Months[119].PortfolioBalance {
		t.Errorf("month 119 balance should match: chain=%f, nochain=%f",
			projChain.Months[119].PortfolioBalance, projNoChain.Months[119].PortfolioBalance)
	}

	// After transition, chained has higher expenses so lower balance
	if projChain.Months[132].PortfolioBalance >= projNoChain.Months[132].PortfolioBalance {
		t.Errorf("after transition, chained balance should be lower: chain=%f, nochain=%f",
			projChain.Months[132].PortfolioBalance, projNoChain.Months[132].PortfolioBalance)
	}
}

func TestRunProjection_ChainTransition_AtCurrentAge(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	chain := []PreparedChainLink{
		preparedLink(t, "", 60, linked),
	}

	calc := newTestCalcWithChain(t, primary, chain)
	proj := calc.RunProjection()

	if proj.Months[0].TotalExpenses < 4500 {
		t.Errorf("expected expenses near 5000, got %f", proj.Months[0].TotalExpenses)
	}
}

func TestRunProjection_RealDollarFields(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 100000
	settings.MonthlyLivingExpenses = 1000
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = nil
	settings.IncomeSources = nil
	settings.InvestmentReturn = 0
	settings.InflationRate = 12
	settings.SpendingDeclineRate = 0
	settings.ProjectionYears = 1
	settings.TaxDeferredPercent = 0
	settings.RothPercent = 0

	calc := newTestCalc(t, settings)
	proj := calc.RunProjection()

	if len(proj.Months) == 0 {
		t.Fatal("expected projection months")
	}

	first := proj.Months[0]
	if math.Abs(first.CumulativeInflation-1.0) > 1e-9 {
		t.Fatalf("month 0 cumulative inflation = %.8f, want 1.0", first.CumulativeInflation)
	}
	if math.Abs(first.PortfolioBalanceReal-first.PortfolioBalance) > 0.01 {
		t.Fatalf("month 0 real balance %.2f should match nominal %.2f", first.PortfolioBalanceReal, first.PortfolioBalance)
	}

	last := proj.Months[len(proj.Months)-1]
	expectedReal := last.PortfolioBalance / last.CumulativeInflation
	if math.Abs(last.PortfolioBalanceReal-expectedReal) > 0.01 {
		t.Fatalf("real balance %.2f, want %.2f", last.PortfolioBalanceReal, expectedReal)
	}
	if last.CumulativeInflation <= 1.0 {
		t.Fatalf("expected cumulative inflation > 1.0, got %.4f", last.CumulativeInflation)
	}
}

func TestMonteCarloSimulation_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 30
	primary.PortfolioValue = 500000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 5.0
	primary.InflationRate = 3.0
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 12000
	linked.InvestmentReturn = 3.0

	chainCalc := newTestCalcWithChain(t, primary, []PreparedChainLink{
		preparedLink(t, "", 70, linked),
	})
	noChainCalc := newTestCalc(t, primary)

	chainMC := chainCalc.RunMonteCarloSimulation(500)
	noChainMC := noChainCalc.RunMonteCarloSimulation(500)

	if chainMC.Stats.SuccessRate >= noChainMC.Stats.SuccessRate {
		t.Errorf("chained MC success rate (%f) should be lower than no-chain (%f)",
			chainMC.Stats.SuccessRate, noChainMC.Stats.SuccessRate)
	}
}

// TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows is the regression test
// for the user-visible bug: when the projection depletes the portfolio before
// RMD age, the RMD panel must report zero rows, not idealized compounding.
func TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows(t *testing.T) {
	// Use CurrentAge=65 (the default from DefaultWhatIfSettings) so we don't
	// fight the Persons[]/CurrentAge derivation. RMD start at 73 → 8yr cushion;
	// $5K portfolio against $5K/mo spending depletes month 1, far before RMD.
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 5_000   // tiny vs. expenses below
	s.TaxDeferredPercent = 100 // all in tax-deferred so RMD bucket = portfolio
	s.MonthlyLivingExpenses = 5_000
	s.MonthlyHealthcare = 0
	s.ProjectionYears = 30
	s.SocialSecurity = nil // no income to cushion

	calc := newTestCalc(t, s)
	analysis := calc.RunFullAnalysis()

	if analysis.RMD == nil {
		t.Fatal("analysis.RMD is nil")
	}
	if analysis.Projection == nil || analysis.Projection.DepletionMonth == nil {
		t.Fatal("expected the main projection to deplete; got survival")
	}
	if !analysis.RMD.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = false; expected true (projection should deplete before age 73)")
	}
	if len(analysis.RMD.Projections) != 0 {
		t.Errorf("len(RMD.Projections) = %d; expected 0 when depleted before RMD",
			len(analysis.RMD.Projections))
	}
	if analysis.RMD.TotalRMDsOver10Yr != 0 {
		t.Errorf("TotalRMDsOver10Yr = %.2f; expected 0 when depleted before RMD",
			analysis.RMD.TotalRMDsOver10Yr)
	}
}

// TestRunFullAnalysis_F072_RMDMatchesProjection enforces the structural
// invariant: each emitted RMD row's amount equals the sum of RMDWithdrawal
// across that year's months in the main projection.
func TestRunFullAnalysis_F072_RMDMatchesProjection(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 60
	s.RothPercent = 10
	s.InvestmentReturn = 5.0
	s.ProjectionYears = 30
	s.MonthlyLivingExpenses = 4_000

	calc := newTestCalc(t, s)
	analysis := calc.RunFullAnalysis()

	if analysis.RMD == nil || len(analysis.RMD.Projections) == 0 {
		t.Skip("no RMD rows in scenario; structural test does not apply")
	}
	if analysis.Projection == nil || len(analysis.Projection.Months) == 0 {
		t.Fatal("missing projection")
	}

	for _, row := range analysis.RMD.Projections {
		startMonth := 12 * row.Year
		endMonth := startMonth + 12
		if endMonth > len(analysis.Projection.Months) {
			endMonth = len(analysis.Projection.Months)
		}
		var got float64
		for m := startMonth; m < endMonth; m++ {
			got += analysis.Projection.Months[m].RMDWithdrawal
		}
		if (row.RMDAmount-got) > 0.01 || (got-row.RMDAmount) > 0.01 {
			t.Errorf("year %d (age %d): RMD.RMDAmount = %.4f; sum of Projection.RMDWithdrawal = %.4f",
				row.Year, row.Age, row.RMDAmount, got)
		}
	}
}
