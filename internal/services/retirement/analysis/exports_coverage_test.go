package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func TestExportedAnalysisWrappersAndScore(t *testing.T) {
	score := Score(&models.ProjectionResult{Survives: true}, &models.BudgetFitAnalysis{RequiredRate: 3})
	if score.Score != 100 {
		t.Fatalf("Score=%d, want 100", score.Score)
	}

	if got := YearsUntilDepletion(HistoricalSequenceResult{StartYear: 1970, DepletionYear: 1975}); got != 5 {
		t.Fatalf("YearsUntilDepletion=%d, want 5", got)
	}
	if got := YearsUntilDepletion(HistoricalSequenceResult{StartYear: 1970, DepletionYear: 1969}); got != 0 {
		t.Fatalf("pre-start depletion years=%d, want 0", got)
	}

	options := []models.SSPortfolioOption{
		{ClaimAge: 70, SurvivalRate: 80, MedianEndingBalance: 500_000},
		{ClaimAge: 67, SurvivalRate: 90, MedianEndingBalance: 400_000},
		{ClaimAge: 62, SurvivalRate: 90, MedianEndingBalance: 400_000},
	}
	best, ok := BestSSPortfolioOption(options)
	if !ok || best.ClaimAge != 62 {
		t.Fatalf("best option=%#v ok=%v, want earliest tie at 62", best, ok)
	}
	if _, ok := BestSSPortfolioOption(nil); ok {
		t.Fatal("empty SS option list should not return a best option")
	}
	if got := CumulativeBenefit(1000, 67, 69, 0); got != 24_000 {
		t.Fatalf("cumulative benefit=%v, want 24000", got)
	}
}

func TestBuildExplainabilityUsesSummariesAndFallback(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}

	if got := BuildExplainability(nil, in); got != nil {
		t.Fatalf("nil projection explainability=%#v, want nil", got)
	}

	proj := &models.ProjectionResult{
		Months: []models.ProjectionMonth{
			{Month: 0, PortfolioBalance: 100_000, PortfolioBalanceReal: 100_000, CumulativeInflation: 1, GrossIncome: 1_000, TaxesPaid: 100, TotalExpenses: 700, NetWithdrawal: 200, PortfolioGrowth: 300},
			{Month: 12, PortfolioBalance: 105_000, PortfolioBalanceReal: 101_000, CumulativeInflation: 1.04, GrossIncome: 2_000, TaxesPaid: 300, TotalExpenses: 800, NetWithdrawal: 100, PortfolioGrowth: 400},
		},
	}
	explain := BuildExplainability(proj, in)
	if explain == nil {
		t.Fatal("BuildExplainability returned nil")
	}
	if got, want := len(explain.YearlySummaries), 2; got != want {
		t.Fatalf("fallback summaries=%d, want %d", got, want)
	}
	if explain.TotalTaxes != 400 || explain.TotalGrossIncome != 3_000 {
		t.Fatalf("fallback totals taxes=%v gross=%v, want 400/3000", explain.TotalTaxes, explain.TotalGrossIncome)
	}
	if explain.InflationLossPercent <= 0 {
		t.Fatalf("inflation loss=%v, want positive", explain.InflationLossPercent)
	}

	proj.YearlySummaries = []models.ProjectionYearSummary{{Year: 0, GrossIncome: 500, Taxes: 50}}
	explain = BuildExplainability(proj, in)
	if got := len(explain.YearlySummaries); got != 1 {
		t.Fatalf("explicit summaries=%d, want 1", got)
	}
	if explain.TaxShareOfGrossCashFlow != 10 {
		t.Fatalf("tax share=%v, want 10", explain.TaxShareOfGrossCashFlow)
	}
}

func TestSensitivityAndMonteCarloExportedWrappers(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.PortfolioValue = 500_000
	s.MonthlyLivingExpenses = 1_500
	s.MonthlyHealthcare = 250
	s.ProjectionYears = 3
	s.InvestmentReturn = 5

	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	results := Sensitivity(engine.New(), in)
	if got, want := len(results), 6; got != want {
		t.Fatalf("sensitivity results=%d, want %d", got, want)
	}
	for _, result := range results {
		if result.Scenario.Name == "" {
			t.Fatalf("sensitivity result missing scenario name: %#v", result)
		}
	}

	config := DefaultMonteCarloConfig()
	config.CrashProbability = 0
	config.SpendingShockProb = 0
	config.HealthShockProb = 0
	rng := rand.New(rand.NewSource(1))
	single := RunSingleMonteCarloSimulation(in, rng, config)
	if single.ProjectionYears == 0 {
		t.Fatalf("single MC result missing projection years: %#v", single)
	}

	impact := CalculateSequenceRiskImpact([]models.MonteCarloResult{
		{Survives: true, EarlyCrashes: 1},
		{Survives: false, LateCrashes: 1},
	})
	if impact < 0 {
		t.Fatalf("sequence risk impact=%v, want non-negative", impact)
	}

	buckets := CreateDistributionBuckets([]float64{100_000, 200_000, 300_000})
	if buckets == nil || len(buckets.Buckets) == 0 {
		t.Fatalf("distribution buckets=%#v, want non-empty", buckets)
	}

	timing := &CrashTiming{}
	lastCrashYear := -1
	returns := GenerateYearlyReturns(s, rand.New(rand.NewSource(2)), config, 4, timing, &lastCrashYear)
	if len(returns) != 4 {
		t.Fatalf("yearly returns=%d, want 4", len(returns))
	}
}
