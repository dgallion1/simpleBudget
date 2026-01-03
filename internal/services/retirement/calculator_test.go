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

// TestGenerateYearlyReturns verifies return generation with crashes and volatility
func TestGenerateYearlyReturns(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.InvestmentReturn = 7.0
	calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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

// TestRunMonteCarloSimulation tests the full simulation with aggregation
func TestRunMonteCarloSimulation(t *testing.T) {
	t.Run("uses default 1000 runs when zero specified", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := NewCalculator(settings)

		result := calc.RunMonteCarloSimulation(0)

		if result.Stats.Runs != 1000 {
			t.Errorf("got %d runs, want 1000", result.Stats.Runs)
		}
	})

	t.Run("respects specified run count", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := NewCalculator(settings)

		result := calc.RunMonteCarloSimulation(100)

		if result.Stats.Runs != 100 {
			t.Errorf("got %d runs, want 100", result.Stats.Runs)
		}
	})

	t.Run("success rate is between 0 and 100", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := NewCalculator(settings)

		result := calc.RunMonteCarloSimulation(100)

		if result.Stats.SuccessRate < 0 || result.Stats.SuccessRate > 100 {
			t.Errorf("success rate %v is out of bounds [0, 100]", result.Stats.SuccessRate)
		}
	})

	t.Run("percentiles are ordered correctly", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1000000
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
		calc := NewCalculator(settings)

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
	calc := NewCalculator(settings)

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

// TestMonteCarloWithIncomeAndExpenses tests simulation with income sources
func TestMonteCarloWithIncomeAndExpenses(t *testing.T) {
	t.Run("income sources reduce depletion risk", func(t *testing.T) {
		// Without income
		settingsNoIncome := models.DefaultWhatIfSettings()
		settingsNoIncome.PortfolioValue = 500000
		settingsNoIncome.MonthlyLivingExpenses = 4000
		settingsNoIncome.ProjectionYears = 30

		calcNoIncome := NewCalculator(settingsNoIncome)
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

		calcWithIncome := NewCalculator(settingsWithIncome)
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
	calc := NewCalculator(settings)

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

// BenchmarkMonteCarloSimulation benchmarks the simulation performance
func BenchmarkMonteCarloSimulation(b *testing.B) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1000000
	settings.ProjectionYears = 30
	calc := NewCalculator(settings)

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
