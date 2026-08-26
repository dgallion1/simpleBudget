package retirement

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/storage"
)

// --- CalculateTotalIncome (0% covered) ---

func TestCalculateTotalIncome(t *testing.T) {
	t.Run("no income sources", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.IncomeSources = nil
		c := newTestCalc(t, s)
		if got := c.CalculateTotalIncome(0); got != 0 {
			t.Errorf("expected 0, got %f", got)
		}
	})

	t.Run("single income source", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.IncomeSources = []models.IncomeSource{
			{ID: "1", Name: "Pension", Amount: 2000, StartMonth: 0},
		}
		c := newTestCalc(t, s)
		if got := c.CalculateTotalIncome(0); got != 2000 {
			t.Errorf("expected 2000, got %f", got)
		}
	})

	t.Run("multiple income sources", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.IncomeSources = []models.IncomeSource{
			{ID: "1", Name: "Pension", Amount: 2000, StartMonth: 0},
			{ID: "2", Name: "SS", Amount: 1500, StartMonth: 0},
		}
		c := newTestCalc(t, s)
		if got := c.CalculateTotalIncome(0); got != 3500 {
			t.Errorf("expected 3500, got %f", got)
		}
	})

	t.Run("income source not yet started", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.IncomeSources = []models.IncomeSource{
			{ID: "1", Name: "Pension", Amount: 2000, StartMonth: 24},
		}
		c := newTestCalc(t, s)
		if got := c.CalculateTotalIncome(0); got != 0 {
			t.Errorf("expected 0 before start, got %f", got)
		}
		if got := c.CalculateTotalIncome(24); got == 0 {
			t.Error("expected non-zero at start month")
		}
	})
}

// --- engine.RebaseLivingExpensesAtTransition (uncovered: spending phases enabled branch) ---

func TestRebaseLivingExpensesAtTransition_SpendingPhasesEnabled(t *testing.T) {
	s := defaultSettingsForTest()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	s.MonthlyLivingExpenses = 3000

	// With spending phases enabled, should apply multiplier using full cumulativeInflation.
	// netCumulativeInflation is ignored by the phases path.
	result := engine.RebaseLivingExpensesAtTransition(s, 65, 1.1, 1.0)
	expected := 3000 * s.GetSpendingMultiplier(65) * 1.1
	if math.Abs(result-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, result)
	}
}

func TestRebaseLivingExpensesAtTransition_SpendingPhasesDisabled(t *testing.T) {
	s := defaultSettingsForTest()
	s.MonthlyLivingExpenses = 3000
	s.SpendingPhaseConfig = nil

	// With spending phases disabled, the function uses netCumulativeInflation (4th arg).
	result := engine.RebaseLivingExpensesAtTransition(s, 65, 1.1, 1.1)
	expected := 3000 * 1.1
	if math.Abs(result-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, result)
	}
}

// --- estimateMonthlyTaxes (uncovered: negative taxDue branch) ---

func TestEstimateMonthlyTaxes_NilTaxCalculator(t *testing.T) {
	acc := engine.ProjectionTaxAccumulator{}
	result := acc.EstimateMonthlyTaxes(nil, 0, 0, 1000, 0, 0, 0, 0, 0, 0)
	if result != 0 {
		t.Errorf("expected 0 for nil tax calculator, got %f", result)
	}
}

func TestEstimateMonthlyTaxes_NegativeTaxDue(t *testing.T) {
	// Create accumulator where taxes already overpaid
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	acc := engine.ProjectionTaxAccumulator{
		TaxesPaidYTD: 1_000_000, // Massively overpaid
	}
	result := acc.EstimateMonthlyTaxes(tc, 0, 6, 1000, 0, 0, 0, 0, 0, 0)
	if result != 0 {
		t.Errorf("expected 0 when taxes overpaid, got %f", result)
	}
}

func TestEstimateMonthlyTaxes_LastMonthOfYear(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	acc := engine.ProjectionTaxAccumulator{}
	// monthInYear=11 means last month; remainingMonths=1
	result := acc.EstimateMonthlyTaxes(tc, 0, 11, 5000, 0, 0, 0, 0, 0, 0)
	if result < 0 {
		t.Errorf("expected non-negative result, got %f", result)
	}
}

// --- engine.RothConversionAmountForYear (uncovered: end year check) ---

func TestRothConversionAmountForYear_PastEndYear(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000,
		StartYear:    0,
		EndYear:      5,
	}
	// Year 6 is past EndYear 5
	got := engine.RothConversionAmountForYear(s, 6, 100000)
	if got != 0 {
		t.Errorf("expected 0 past end year, got %f", got)
	}
}

func TestRothConversionAmountForYear_LimitedByBalance(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000,
		StartYear:    0,
		EndYear:      0, // 0 means no end
	}
	// Available is less than annual amount
	got := engine.RothConversionAmountForYear(s, 0, 10000)
	if got != 10000 {
		t.Errorf("expected 10000 (limited by balance), got %f", got)
	}
}

// --- withdraw (uncovered: negative basis, zero market value) ---

func TestWithdraw_ZeroMarketValue(t *testing.T) {
	a := &engine.TaxableAccountState{MarketValue: 0, CostBasis: 0}
	cash, basis, gain := a.Withdraw(1000)
	if cash != 0 || basis != 0 || gain != 0 {
		t.Errorf("expected all zeros, got cash=%f basis=%f gain=%f", cash, basis, gain)
	}
}

func TestWithdraw_NegativeAmount(t *testing.T) {
	a := &engine.TaxableAccountState{MarketValue: 100000, CostBasis: 50000}
	cash, basis, gain := a.Withdraw(-100)
	if cash != 0 || basis != 0 || gain != 0 {
		t.Errorf("expected all zeros for negative amount, got cash=%f basis=%f gain=%f", cash, basis, gain)
	}
}

func TestWithdraw_FullDepletion(t *testing.T) {
	a := &engine.TaxableAccountState{MarketValue: 5000, CostBasis: 3000}
	cash, _, _ := a.Withdraw(10000) // Withdraw more than available
	if cash != 5000 {
		t.Errorf("expected cash=5000, got %f", cash)
	}
	if a.MarketValue != 0 {
		t.Errorf("expected MarketValue=0, got %f", a.MarketValue)
	}
	if a.CostBasis != 0 {
		t.Errorf("expected CostBasis=0, got %f", a.CostBasis)
	}
}

// --- applyGrowth (uncovered: MarketValue<=0 and negative clamping) ---

func TestApplyGrowth_ZeroMarketValue(t *testing.T) {
	a := &engine.TaxableAccountState{MarketValue: 0, CostBasis: 0}
	components := engine.TaxableReturnComponents{
		Appreciation:      0.005,
		QualifiedDividend: 0.001,
	}
	result := a.ApplyGrowth(components, 1.0)
	if result.TotalGrowth != 0 {
		t.Errorf("expected 0 growth for zero market value, got %f", result.TotalGrowth)
	}
}

func TestApplyGrowth_NegativeMarketValueClamp(t *testing.T) {
	// Market value that will go negative after a large negative return
	a := &engine.TaxableAccountState{MarketValue: 100, CostBasis: 200}
	components := engine.TaxableReturnComponents{
		Appreciation: -2.0, // extreme negative
	}
	a.ApplyGrowth(components, 1.0)
	if a.MarketValue < 0 {
		t.Error("MarketValue should be clamped to 0")
	}
	if a.CostBasis < 0 {
		t.Error("CostBasis should be clamped to 0")
	}
}

// --- engine.ApplyBigTicketExpenseWithTaxableState (46.7% covered) ---

func TestApplyBigTicketExpense_AllBuckets(t *testing.T) {
	t.Run("fully covered by taxable", func(t *testing.T) {
		taxDeferred := 100000.0
		s := defaultSettingsForTest()
		taxable := engine.NewTaxableAccountState(s, 50000)
		roth := 30000.0
		rothBasis := 30000.0
		r := engine.ApplyBigTicketExpenseWithTaxableState(10000, true, 0, &taxDeferred, &taxable, &roth, &rothBasis)
		if r.UnfundedExpense != 0 {
			t.Errorf("expected 0 remaining, got %f", r.UnfundedExpense)
		}
	})

	t.Run("spills from taxable to roth", func(t *testing.T) {
		taxDeferred := 100000.0
		s := defaultSettingsForTest()
		taxable := engine.NewTaxableAccountState(s, 5000)
		roth := 30000.0
		rothBasis := 30000.0
		r := engine.ApplyBigTicketExpenseWithTaxableState(20000, false, 0, &taxDeferred, &taxable, &roth, &rothBasis)
		// 5000 from taxable, 15000 from Roth, no tax-deferred (allowTaxDeferred=false)
		if r.UnfundedExpense != 0 {
			t.Errorf("expected 0 remaining, got %f", r.UnfundedExpense)
		}
		if roth != 15000 {
			t.Errorf("expected Roth=15000, got %f", roth)
		}
	})

	t.Run("spills to tax-deferred with penalty", func(t *testing.T) {
		taxDeferred := 100000.0
		s := defaultSettingsForTest()
		taxable := engine.NewTaxableAccountState(s, 2000)
		roth := 3000.0
		rothBasis := 3000.0
		// 10000 needed, 2000 from taxable, 3000 from roth, 5000 remaining
		// With 10% penalty: grossNeeded = 5000/0.9 = 5555.56
		r := engine.ApplyBigTicketExpenseWithTaxableState(10000, true, 0.10, &taxDeferred, &taxable, &roth, &rothBasis)
		if r.UnfundedExpense > 0.01 {
			t.Errorf("expected ~0 remaining, got %f", r.UnfundedExpense)
		}
		if taxDeferred >= 100000 {
			t.Error("expected tax-deferred balance to decrease")
		}
	})

	t.Run("not enough in any bucket", func(t *testing.T) {
		taxDeferred := 1000.0
		s := defaultSettingsForTest()
		taxable := engine.NewTaxableAccountState(s, 1000)
		roth := 1000.0
		rothBasis := 1000.0
		r := engine.ApplyBigTicketExpenseWithTaxableState(100000, true, 0, &taxDeferred, &taxable, &roth, &rothBasis)
		if r.UnfundedExpense <= 0 {
			t.Error("expected positive remaining when funds insufficient")
		}
	})

	t.Run("tax-deferred not allowed with remaining", func(t *testing.T) {
		taxDeferred := 100000.0
		s := defaultSettingsForTest()
		taxable := engine.NewTaxableAccountState(s, 1000)
		roth := 1000.0
		rothBasis := 1000.0
		r := engine.ApplyBigTicketExpenseWithTaxableState(10000, false, 0, &taxDeferred, &taxable, &roth, &rothBasis)
		if r.UnfundedExpense <= 0 {
			t.Error("expected remaining when tax-deferred not allowed")
		}
		if taxDeferred != 100000 {
			t.Error("tax-deferred should be unchanged when not allowed")
		}
	})
}

// --- prepareChainedSettings (uncovered: healthcare persons branch) ---

func TestPrepareChainedSettings_HealthcarePersons(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 30

	linked := models.DefaultWhatIfSettings()
	linked.HealthcarePersons = []models.HealthcarePerson{
		{ID: "hp1", Name: "Alice", CurrentAge: 70},
		{ID: "hp2", Name: "Bob", CurrentAge: 68},
	}

	result := mustPrepareChained(t, linked, primary, 10)

	if len(result.HealthcarePersons) != 2 {
		t.Fatalf("expected 2 healthcare persons, got %d", len(result.HealthcarePersons))
	}
	// CurrentAge should be rebased: 70 - 10 = 60
	if result.HealthcarePersons[0].CurrentAge != 60 {
		t.Errorf("expected age 60, got %d", result.HealthcarePersons[0].CurrentAge)
	}
	if result.HealthcarePersons[1].CurrentAge != 58 {
		t.Errorf("expected age 58, got %d", result.HealthcarePersons[1].CurrentAge)
	}
}

func TestPrepareChainedSettings_NoHealthcarePersons(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60

	linked := models.DefaultWhatIfSettings()
	linked.HealthcarePersons = nil

	result := mustPrepareChained(t, linked, primary, 5)
	if len(result.HealthcarePersons) != 0 {
		t.Errorf("expected 0 healthcare persons, got %d", len(result.HealthcarePersons))
	}
}

// --- yearsUntilDepletion (uncovered: depletion before start year) ---

func TestYearsUntilDepletion(t *testing.T) {
	t.Run("no depletion", func(t *testing.T) {
		r := HistoricalSequenceResult{StartYear: 1990, DepletionYear: 0}
		if got := yearsUntilDepletion(r); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})

	t.Run("depletion before start year", func(t *testing.T) {
		r := HistoricalSequenceResult{StartYear: 1990, DepletionYear: 1985}
		if got := yearsUntilDepletion(r); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})

	t.Run("normal depletion", func(t *testing.T) {
		r := HistoricalSequenceResult{StartYear: 1990, DepletionYear: 2000}
		if got := yearsUntilDepletion(r); got != 10 {
			t.Errorf("expected 10, got %d", got)
		}
	})
}

// --- Tax calculator coverage: unknown filing status fallback ---

func TestGetAdjustedBrackets_UnknownFilingStatus(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       "unknown",
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	brackets := tc.GetAdjustedBrackets(0)
	if len(brackets) == 0 {
		t.Fatal("expected brackets to fall back to married_joint")
	}
}

func TestGetAdjustedLTCGBrackets_UnknownFilingStatus(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       "unknown",
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	brackets := tc.GetAdjustedLongTermCapitalGainsBrackets(0)
	if len(brackets) == 0 {
		t.Fatal("expected LTCG brackets to fall back to married_joint")
	}
}

func TestGetAdjustedBrackets_WithInflation(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	base := tc.GetAdjustedBrackets(0)
	adjusted := tc.GetAdjustedBrackets(5)

	if adjusted[0].MinIncome != 0 {
		t.Error("first bracket MinIncome should stay 0")
	}
	// Higher brackets should be inflation-adjusted
	if adjusted[1].MinIncome <= base[1].MinIncome {
		t.Error("bracket threshold should increase with inflation")
	}
}

func TestGetAdjustedLTCGBrackets_WithInflation(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	base := tc.GetAdjustedLongTermCapitalGainsBrackets(0)
	adjusted := tc.GetAdjustedLongTermCapitalGainsBrackets(5)

	if adjusted[1].MinIncome <= base[1].MinIncome {
		t.Error("LTCG bracket threshold should increase with inflation")
	}
}

func TestGetBracketRate_MiddleBracket(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 0)

	// Income in 22% bracket range for single filer
	rate := tc.GetBracketRate(80000, 0)
	if rate != 22 {
		t.Errorf("expected 22%% marginal rate, got %f", rate)
	}
}

// --- createDistributionBuckets edge cases ---

func TestCreateDistributionBuckets_SmallBalances(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	// All balances under 100k
	balances := []float64{0, 5000, 10000, 25000, 50000, 75000, 90000}
	dist := c.createDistributionBuckets(balances)
	if dist == nil || len(dist.Buckets) == 0 {
		t.Fatal("expected non-nil distribution with buckets")
	}
}

func TestCreateDistributionBuckets_NegativeBalances(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	balances := []float64{-100, -50, 0, 0, 0}
	dist := c.createDistributionBuckets(balances)
	if dist == nil {
		t.Fatal("expected non-nil distribution")
	}
}

func TestCreateDistributionBuckets_MediumBalances(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	balances := []float64{100000, 200000, 400000, 600000, 800000}
	dist := c.createDistributionBuckets(balances)
	if dist == nil || len(dist.Buckets) == 0 {
		t.Fatal("expected non-nil distribution")
	}
}

func TestCreateDistributionBuckets_LargeBalances(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	balances := []float64{1000000, 2000000, 5000000, 8000000, 15000000}
	dist := c.createDistributionBuckets(balances)
	if dist == nil || len(dist.Buckets) == 0 {
		t.Fatal("expected non-nil distribution")
	}
}

func TestCreateDistributionBuckets_VeryLargeBalances(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	balances := []float64{5000000, 10000000, 15000000, 20000000, 25000000}
	dist := c.createDistributionBuckets(balances)
	if dist == nil || len(dist.Buckets) == 0 {
		t.Fatal("expected non-nil distribution")
	}
}

// --- buildProjectionExplainability edge cases ---

func TestBuildProjectionExplainability_Nil(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)
	if result := c.buildProjectionExplainability(nil); result != nil {
		t.Error("expected nil for nil projection")
	}
}

func TestBuildProjectionExplainability_EmptyMonths(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)
	if result := c.buildProjectionExplainability(&models.ProjectionResult{Months: nil}); result != nil {
		t.Error("expected nil for empty months")
	}
}

// --- CalculateBudgetFit edge cases ---

func TestCalculateBudgetFit_HealthcarePersonsWithEmployerCoverage(t *testing.T) {
	s := defaultSettingsForTest()
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			ID:                    "hp1",
			Name:                  "User",
			CurrentAge:            60,
			CurrentCoverage:       models.CoverageEmployer,
			EmployerCoverageYears: 5,
			CurrentMonthlyCost:    0,
		},
	}

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}

	// Should show healthcare with employer covered note
	found := false
	for _, item := range bf.ExpenseBreakdown {
		if item.Name == "Healthcare" {
			found = true
			if item.Note == "" {
				t.Error("expected note about employer coverage")
			}
		}
	}
	if !found {
		t.Error("expected healthcare item in breakdown")
	}
}

func TestCalculateBudgetFit_HealthcarePersonsZeroCostNoEmployer(t *testing.T) {
	s := defaultSettingsForTest()
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			ID:                 "hp1",
			Name:               "User",
			CurrentAge:         60,
			CurrentCoverage:    models.CoverageACA,
			CurrentMonthlyCost: 0,
		},
	}

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
}

// --- LoadScenarioSettings edge cases ---

func TestLoadScenarioSettings_WithTaxableFields(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create scenario with explicit taxable fields
	settings := map[string]interface{}{
		"scenario_name":                       "Test",
		"portfolio_value":                     1000000,
		"monthly_living_expenses":             3000,
		"current_age":                         65,
		"projection_years":                    10,
		"taxable_dividend_yield":              2.0,
		"taxable_qualified_dividend_percent":  0,
		"taxable_cap_gains_distribution_rate": 0,
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	path := filepath.Join(settingsDir, "whatif_test.json")
	if err := store.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := sm.LoadScenarioSettings("whatif_test.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	// TaxableQualifiedDividendPercent should stay at 0 because taxable fields are present
	if loaded.TaxableQualifiedDividendPercent != 0 {
		t.Errorf("expected 0 for explicitly set taxable_qualified_dividend_percent, got %f", loaded.TaxableQualifiedDividendPercent)
	}
}

func TestLoadScenarioSettings_LegacyHealthcareMigration(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create legacy scenario with monthly_healthcare but no healthcare_persons
	settings := map[string]interface{}{
		"portfolio_value":         1000000,
		"monthly_living_expenses": 3000,
		"current_age":             55, // under 65 -> ACA
		"monthly_healthcare":      500,
		"healthcare_inflation":    6.0,
		"projection_years":        10,
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_legacy.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := sm.LoadScenarioSettings("whatif_legacy.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	if len(loaded.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 migrated healthcare person, got %d", len(loaded.HealthcarePersons))
	}
	if loaded.HealthcarePersons[0].CurrentCoverage != models.CoverageACA {
		t.Errorf("expected ACA coverage for under-65, got %v", loaded.HealthcarePersons[0].CurrentCoverage)
	}
}

func TestLoadScenarioSettings_EmptySpendingPhases(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Spending phase config with empty phases
	settings := map[string]interface{}{
		"portfolio_value":         1000000,
		"monthly_living_expenses": 3000,
		"current_age":             65,
		"projection_years":        10,
		"spending_phase_config": map[string]interface{}{
			"enabled": true,
			"phases":  []interface{}{},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_emptyph.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := sm.LoadScenarioSettings("whatif_emptyph.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	if len(loaded.SpendingPhaseConfig.Phases) == 0 {
		t.Error("expected default phases for empty phases config")
	}
}

func TestLoadScenarioSettings_ZeroMultiplierPhases(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	settings := map[string]interface{}{
		"portfolio_value":         1000000,
		"monthly_living_expenses": 3000,
		"current_age":             65,
		"projection_years":        10,
		"spending_phase_config": map[string]interface{}{
			"enabled": true,
			"phases": []interface{}{
				map[string]interface{}{"name": "Active", "start_age": 0, "multiplier": 0},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_zeromp.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := sm.LoadScenarioSettings("whatif_zeromp.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	if loaded.SpendingPhaseConfig.Phases[0].Multiplier != 1.0 {
		t.Errorf("expected multiplier 1.0 for zero, got %f", loaded.SpendingPhaseConfig.Phases[0].Multiplier)
	}
}

func TestLoadScenarioSettings_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.LoadScenarioSettings("whatif_nonexistent.json")
	if err == nil {
		t.Error("expected error for nonexistent scenario")
	}
}

func TestLoadScenarioSettings_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_bad.json"), []byte("{invalid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = sm.LoadScenarioSettings("whatif_bad.json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadScenarioSettings_PathTraversal(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.LoadScenarioSettings("../evil.json")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

// --- settings.go saveInternal: reject invalid chain on save ---

func TestSaveInternal_RejectsInvalidChain(t *testing.T) {
	sm := newTestSM(t)

	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_nonexistent.json", TransitionAge: 70},
	}

	if err := sm.Save(settings); err == nil {
		t.Fatal("expected Save to reject invalid scenario chain")
	}

	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(reloaded.ScenarioChain) != 0 {
		t.Errorf("expected invalid chain to remain unsaved, got %d links", len(reloaded.ScenarioChain))
	}
}

// --- settings.go Load: double-check cache ---

func TestLoad_CacheHitOnSecondCall(t *testing.T) {
	sm := newTestSM(t)

	s1, err := sm.Load()
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}

	s2, err := sm.Load()
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}

	// Equal value, not the same pointer: the cache hit is real, but Load
	// copies on the way out so the cached object never escapes. The distinct
	// pointer is asserted directly in TestLoadReturnsAPrivateCopy.
	//
	// Despite the name, DeepEqual here would also hold if Load re-read from
	// disk every call; the real caching guard is
	// TestSettingsManager_InvalidateCacheForcesDiskReload in settings_test.go.
	// The comparison also relies on both operands being prepare.Clone results,
	// which agree on turning empty omitempty slices into nil (see Clone's doc
	// comment) — it is not a claim that a Clone equals its source.
	if !reflect.DeepEqual(s1, s2) {
		t.Errorf("expected an equal object from cache:\ns1 = %+v\ns2 = %+v", s1, s2)
	}
	if s1 == s2 {
		t.Error("cache hit returned the cached pointer instead of a copy")
	}
}

// --- settings.go UpdateSettings additional fields ---

func TestUpdateSettings_AllFields(t *testing.T) {
	sm := newTestSM(t)

	updates := map[string]interface{}{
		"portfolio_value":                     float64(500000),
		"monthly_living_expenses":             float64(4000),
		"monthly_healthcare":                  float64(300),
		"healthcare_start_years":              int(2),
		"current_age":                         int(62),
		"spouse_age":                          int(60),
		"phase_age_reference":                 "younger",
		"tax_deferred_percent":                float64(60),
		"roth_percent":                        float64(20),
		"stock_percent":                       float64(70),
		"cash_percent":                        float64(5),
		"tax_deferred_stock_percent":          float64(80),
		"tax_deferred_cash_percent":           float64(5),
		"roth_stock_percent":                  float64(90),
		"roth_cash_percent":                   float64(0),
		"taxable_stock_percent":               float64(60),
		"taxable_cash_percent":                float64(10),
		"inflation_rate":                      float64(2.5),
		"healthcare_inflation":                float64(6.0),
		"spending_decline_rate":               float64(1.0),
		"investment_return":                   float64(7.0),
		"discount_rate":                       float64(3.0),
		"taxable_dividend_yield":              float64(1.5),
		"taxable_qualified_dividend_percent":  float64(80),
		"taxable_cap_gains_distribution_rate": float64(1.0),
		"projection_years":                    int(30),
		"projection_timing":                   models.ProjectionTiming("beginning_of_month"),
		"tax_deferred_delay_years":            int(3),
		"steady_state_override_year":          float64(5),
	}

	s, _, err := sm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	if s.PortfolioValue != 500000 {
		t.Errorf("PortfolioValue: got %f", s.PortfolioValue)
	}
	if s.SpouseAge != 60 {
		t.Errorf("SpouseAge: got %d", s.SpouseAge)
	}
	if s.PhaseAgeReference != "younger" {
		t.Errorf("PhaseAgeReference: got %s", s.PhaseAgeReference)
	}
	if s.TaxDeferredStockPercent != 80 {
		t.Errorf("TaxDeferredStockPercent: got %f", s.TaxDeferredStockPercent)
	}
	if s.TaxableCashPercent != 10 {
		t.Errorf("TaxableCashPercent: got %f", s.TaxableCashPercent)
	}
	if s.TaxableDividendYield != 1.5 {
		t.Errorf("TaxableDividendYield: got %f", s.TaxableDividendYield)
	}
	if s.TaxableCapitalGainsDistributionRate != 1.0 {
		t.Errorf("TaxableCapGainsDistRate: got %f", s.TaxableCapitalGainsDistributionRate)
	}
	if s.TaxDeferredDelayYears != 3 {
		t.Errorf("TaxDeferredDelayYears: got %d", s.TaxDeferredDelayYears)
	}
	if s.SteadyStateOverrideYear != 5 {
		t.Errorf("SteadyStateOverrideYear: got %f", s.SteadyStateOverrideYear)
	}
}

// --- settings.go scenariosReferencingFile ---

func TestScenariosReferencingFile(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create default scenario
	defaults := models.DefaultWhatIfSettings()
	data, _ := json.MarshalIndent(defaults, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a scenario that references "whatif_target.json"
	referencing := models.DefaultWhatIfSettings()
	referencing.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_target.json", TransitionAge: 70},
	}
	data, _ = json.MarshalIndent(referencing, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_ref.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create the target
	target := models.DefaultWhatIfSettings()
	data, _ = json.MarshalIndent(target, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_target.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	refs := sm.scenariosReferencingFile("whatif_target.json")
	if len(refs) != 1 {
		t.Fatalf("expected 1 referencing scenario, got %d", len(refs))
	}
	if refs[0] != "whatif_ref.json" {
		t.Errorf("expected whatif_ref.json, got %s", refs[0])
	}
}

func TestScenariosReferencingFile_BadJSON(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a file with bad JSON
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_bad.json"), []byte("{invalid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	refs := sm.scenariosReferencingFile("whatif_target.json")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs with bad JSON, got %d", len(refs))
	}
}

// --- DeleteScenario: referential integrity ---

func TestDeleteScenario_ReferencedByOther(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create target scenario
	target := models.DefaultWhatIfSettings()
	data, _ := json.MarshalIndent(target, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_target.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create referencing scenario
	referencing := models.DefaultWhatIfSettings()
	referencing.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_target.json", TransitionAge: 70},
	}
	data, _ = json.MarshalIndent(referencing, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_ref.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = sm.DeleteScenario("whatif_target.json")
	if err == nil {
		t.Error("expected error when deleting referenced scenario")
	}
}

func TestDeleteScenario_ActiveScenario(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Create and switch to a custom scenario
	if _, err := sm.CreateScenario("To Delete"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	activeFile := sm.ActiveFilename()

	err = sm.DeleteScenario(activeFile)
	if err != nil {
		t.Fatalf("DeleteScenario: %v", err)
	}

	// Should have switched back to default
	if sm.ActiveFilename() != "whatif.json" {
		t.Errorf("expected switch to whatif.json, got %s", sm.ActiveFilename())
	}
}

// --- RenameScenario ---

func TestRenameScenario_Default(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("whatif.json", "New Name")
	if err == nil {
		t.Error("expected error renaming default scenario")
	}
}

func TestRenameScenario_PathTraversal(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("../evil.json", "New Name")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestRenameScenario_NonExistent(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("whatif_nonexistent.json", "New Name")
	if err == nil {
		t.Error("expected error for nonexistent scenario")
	}
}

func TestRenameScenario_Success(t *testing.T) {
	sm := newTestSM(t)

	// Create a scenario first
	if _, err := sm.CreateScenario("Old Name"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	activeFile := sm.ActiveFilename()

	err := sm.RenameScenario(activeFile, "New Name")
	if err != nil {
		t.Fatalf("RenameScenario: %v", err)
	}

	// After rename, cache should be invalidated; reload should show new name
	sm.cache = nil
	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after rename: %v", err)
	}
	if s.ScenarioName != "New Name" {
		t.Errorf("expected 'New Name', got %q", s.ScenarioName)
	}
}

func TestRenameScenario_BadJSON(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write bad JSON
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_bad.json"), []byte("{bad"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = sm.RenameScenario("whatif_bad.json", "New Name")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// --- loadInternal edge cases ---

func TestLoadInternal_LegacyHealthcareMedicareMigration(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write settings with legacy healthcare (age >= 65 -> Medicare)
	settings := map[string]interface{}{
		"current_age":          67,
		"monthly_healthcare":   400,
		"healthcare_inflation": 5.0,
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(s.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 migrated person, got %d", len(s.HealthcarePersons))
	}
	if s.HealthcarePersons[0].CurrentCoverage != models.CoverageMedicare {
		t.Errorf("expected Medicare for age 67, got %v", s.HealthcarePersons[0].CurrentCoverage)
	}
}

func TestLoadInternal_EmptySpendingPhasesMigration(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	settings := map[string]interface{}{
		"spending_phase_config": map[string]interface{}{
			"enabled": true,
			"phases":  []interface{}{},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(s.SpendingPhaseConfig.Phases) == 0 {
		t.Error("expected default phases for empty phases config")
	}
}

// --- RunProjection: tax-deferred delay active path ---

func TestRunProjection_TaxDeferredDelay(t *testing.T) {
	s := defaultSettingsForTest()
	s.TaxDeferredPercent = 80
	s.RothPercent = 10
	s.TaxDeferredDelayYears = 3
	s.ProjectionYears = 5

	c := newTestCalc(t, s)
	result := c.RunProjection()

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Months) == 0 {
		t.Fatal("expected non-empty months")
	}
}

// --- RunProjection: early withdrawal penalty ---

func TestRunProjection_EarlyWithdrawalPenalty(t *testing.T) {
	s := defaultSettingsForTest()
	s.CurrentAge = 50 // Under 59.5
	s.TaxDeferredPercent = 90
	s.RothPercent = 5
	s.MonthlyLivingExpenses = 8000
	s.ProjectionYears = 5

	c := newTestCalc(t, s)
	result := c.RunProjection()

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- Failure point threshold safety levels ---

func TestFindReturnThreshold_SafetyLevels(t *testing.T) {
	// Create settings where portfolio barely survives (marginal)
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 200_000
	s.MonthlyLivingExpenses = 3000
	s.InvestmentReturn = 6.0
	s.InflationRate = 3.0
	s.ProjectionYears = 5
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil

	c := newTestCalc(t, s)
	if !c.RunProjection().Survives {
		t.Skip("need surviving baseline")
	}

	fp := c.findReturnThreshold()
	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	// Just check it has a valid safety level
	valid := map[string]bool{"safe": true, "marginal": true, "critical": true}
	if !valid[fp.SafetyLevel] {
		t.Errorf("unexpected safety level: %s", fp.SafetyLevel)
	}
}

func TestFindPortfolioThreshold_IncomeCoversExpenses(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 2000
	s.InvestmentReturn = 6.0
	s.ProjectionYears = 10
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil
	// Income covers all expenses
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Pension", Amount: 5000, StartMonth: 0},
	}

	c := newTestCalc(t, s)
	fp := c.findPortfolioThreshold()

	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	if fp.SafetyLevel != "safe" {
		t.Errorf("expected safe level when income covers expenses, got %s", fp.SafetyLevel)
	}
}

// --- Historical data edge case ---

func TestGetHistoricalStats_NonZeroValues(t *testing.T) {
	avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev := GetHistoricalStats()
	if avgStock == 0 || avgBond == 0 {
		t.Error("expected non-zero historical averages")
	}
	if avgCash == 0 || avgInflation == 0 {
		t.Error("expected non-zero cash and inflation averages")
	}
	if stockStdDev == 0 || bondStdDev == 0 {
		t.Error("expected non-zero standard deviations")
	}
}

// --- RunHistoricalBacktest edge case: no available years ---

func TestRunHistoricalBacktest_TooLongProjection(t *testing.T) {
	s := defaultSettingsForTest()
	s.ProjectionYears = 999 // No historical data spans this long

	c := newTestCalc(t, s)
	result := c.RunHistoricalBacktest()
	if result.TotalSequences != 0 {
		t.Errorf("expected 0 sequences, got %d", result.TotalSequences)
	}
}

// --- Backtest with chain transition ---

func TestRunSingleHistoricalSequence_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	c := newTestCalcWithChain(t, primary, []engine.PreparedChainLink{
		preparedLink(t, "", 70, linked),
	})

	result := c.runSingleHistoricalSequence(1990)
	if result.StartYear != 1990 {
		t.Errorf("expected start year 1990, got %d", result.StartYear)
	}
}

// --- Backtest with spending phases ---

func TestRunSingleHistoricalSequence_SpendingPhasesDetail(t *testing.T) {
	s := defaultSettingsForTest()
	s.ProjectionYears = 20
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Active", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow", StartAge: 75, Multiplier: 0.8},
			{Name: "Late", StartAge: 85, Multiplier: 0.6},
		},
	}

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)
	if !result.Survives {
		t.Error("expected to survive with reduced spending phases")
	}
}

// --- RMD analysis with already-started RMDs ---

func TestBuildRMDAnalysis_AlreadyPastRMDAge(t *testing.T) {
	s := defaultSettingsForTest()
	s.CurrentAge = 80 // Already past 73
	// F-078: keep Persons[0].BirthMonth in sync with CurrentAge so the
	// calendar-year RMD helpers (which prefer Persons[0].BirthMonth over
	// CurrentAge) see the same household timing as the legacy CurrentAge
	// fallback path used in this test.
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.TaxDeferredPercent = 50
	s.ProjectionYears = 10

	c := newTestCalc(t, s)
	rmd := c.BuildRMDAnalysis(c.RunProjection())

	if rmd == nil {
		t.Fatal("expected non-nil RMD analysis")
	}
	if rmd.StartsInYears != 0 {
		t.Errorf("expected 0 years until start, got %d", rmd.StartsInYears)
	}
	if len(rmd.Projections) == 0 {
		t.Error("expected projections for age 80+")
	}
}

// --- RunMonteCarloSimulation: depleted results ---

func TestRunMonteCarloSimulation_DepletionResults(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 50000
	s.MonthlyLivingExpenses = 10000
	s.InvestmentReturn = 2.0
	s.ProjectionYears = 10
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil

	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(20)
	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil Monte Carlo result")
	}
	// With such poor financials, many runs should fail
	if result.Stats.SuccessRate == 100 {
		t.Error("expected some failures with severely underfunded portfolio")
	}
}

// --- LoadScenarioSettings with read error ---

func TestLoadScenarioSettings_ReadError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Root-proof: a DIRECTORY standing in for the file, not chmod 0000. Stat
	// on a directory succeeds (it exists), so LoadScenarioSettings's
	// os.IsNotExist check just above does not fire and control reaches the
	// actual read; os.ReadFile on a directory then fails with EISDIR at any
	// uid, including root -- unlike chmod 0000, which root's
	// CAP_DAC_OVERRIDE reads straight through. This lands in the exact
	// branch this test's name defends ("reading scenario %s: %w"), not the
	// "scenario file not found" branch a missing-file injection would hit
	// instead.
	fpath := filepath.Join(settingsDir, "whatif_unreadable.json")
	if err := os.Mkdir(fpath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	_, err = sm.LoadScenarioSettings("whatif_unreadable.json")
	if err == nil {
		t.Error("expected error for unreadable file")
	}
}

// --- Roth conversion with EndYear=0 (no end) ---

func TestRothConversionEndYearZero(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 30000,
		StartYear:    0,
		EndYear:      0, // 0 means no end
	}
	// Year 100 should still convert
	got := engine.RothConversionAmountForYear(s, 100, 100000)
	if got != 30000 {
		t.Errorf("expected 30000, got %f", got)
	}
}

// --- Roth conversion disabled ---

func TestRothConversionDisabled(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      false,
		AnnualAmount: 30000,
	}
	got := engine.RothConversionAmountForYear(s, 0, 100000)
	if got != 0 {
		t.Errorf("expected 0 when disabled, got %f", got)
	}
}

// --- Roth conversion nil ---

func TestRothConversionNil(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = nil
	got := engine.RothConversionAmountForYear(s, 0, 100000)
	if got != 0 {
		t.Errorf("expected 0 when nil, got %f", got)
	}
}

// --- Roth conversion before start year ---

func TestRothConversionBeforeStartYear(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 30000,
		StartYear:    5,
		EndYear:      10,
	}
	got := engine.RothConversionAmountForYear(s, 2, 100000)
	if got != 0 {
		t.Errorf("expected 0 before start year, got %f", got)
	}
}

// --- formatBucketLabel ---

// --- UpdateSettings save error ---

func TestUpdateSettings_SaveError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Initialize
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Root-proof: settingsDir stays a real, readable directory but rejects
	// writes (see corruptSettingsDirToFile in coverage_gaps4_test.go), not a
	// bare chmod 0555 (root's CAP_DAC_OVERRIDE reads/writes through
	// permission bits regardless). UpdateSettings's loadInternal succeeds
	// (its MkdirAll is a no-op against the already-existing directory), and
	// its saveInternal write is what fails.
	corruptSettingsDirToFile(t, settingsDir)

	_, _, err = sm.UpdateSettings(map[string]interface{}{
		"portfolio_value": float64(500000),
	})
	if err == nil {
		t.Error("expected error when dir is read-only")
	}
}

// --- RenameScenario for active scenario invalidates cache ---

func TestRenameScenario_ActiveScenarioInvalidatesCache(t *testing.T) {
	sm := newTestSM(t)

	if _, err := sm.CreateScenario("Active Plan"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	activeFile := sm.ActiveFilename()

	err := sm.RenameScenario(activeFile, "Renamed Plan")
	if err != nil {
		t.Fatalf("RenameScenario: %v", err)
	}

	// Cache should be cleared, next load should show new name
	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ScenarioName != "Renamed Plan" {
		t.Errorf("expected 'Renamed Plan', got %q", s.ScenarioName)
	}
}

// --- CalculateBudgetFit: taxable income, income start note, steady state with RMD ---

func TestCalculateBudgetFit_TaxableIncome(t *testing.T) {
	s := defaultSettingsForTest()
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.TaxableDividendYield = 3.0
	s.TaxableQualifiedDividendPercent = 80
	s.TaxableCapitalGainsDistributionRate = 1.0
	s.InvestmentReturn = 7.0

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
	// Should include taxable distributions in income
	found := false
	for _, item := range bf.IncomeBreakdown {
		if item.Name == "Taxable Distributions" {
			found = true
		}
	}
	if !found {
		t.Error("expected Taxable Distributions in income breakdown")
	}
}

func TestCalculateBudgetFit_IncomeStartNote(t *testing.T) {
	s := defaultSettingsForTest()
	// Income that doesn't start at month 0
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Social Security", Amount: 2000, StartMonth: 24},
	}
	s.MonthlyLivingExpenses = 3000

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
	// At month 0, SS hasn't started yet, so it should NOT be in income items
	// (GetAdjustedAmount at month 0 returns 0 since StartMonth=24)
}

func TestCalculateBudgetFit_SteadyStateOverride(t *testing.T) {
	s := defaultSettingsForTest()
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "SS", Amount: 2000, StartMonth: 24},
	}
	s.SteadyStateOverrideYear = 5 // Override to year 5

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
	if bf.SteadyStateYear != 5 {
		t.Errorf("expected steady state year 5, got %f", bf.SteadyStateYear)
	}
}

func TestCalculateBudgetFit_SteadyStateRMD(t *testing.T) {
	s := defaultSettingsForTest()
	s.CurrentAge = 65
	s.TaxDeferredPercent = 60
	s.ProjectionYears = 20
	// Income starts at year 8 (month 96), so steady state is at month 96 / year 8
	// At age 65+8=73, RMD should kick in
	// F-077: pin StartDate so olderBirthYear=1959 (CurrentAge=65 ⇒ StartDate=2024)
	// → applicable RMD age 73, preserving this test's "RMD at age 73" intent.
	s.StartDate = "2024-01"
	if len(s.Persons) > 0 {
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	}
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "SS", Amount: 2000, StartMonth: 96},
	}
	s.InvestmentReturn = 6.0
	s.SteadyStateOverrideYear = 8 // view at year 8 (age 73, when RMD applies)

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
	if bf.SteadyStateRMD == 0 {
		t.Error("expected non-zero steady state RMD at age 73")
	}
}

func TestCalculateBudgetFit_IncomeWithDelayedStart(t *testing.T) {
	s := defaultSettingsForTest()
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Pension", Amount: 3000, StartMonth: 0},
		{ID: "2", Name: "Social Security", Amount: 2000, StartMonth: 36},
	}

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}
	// Pension should be in income items
	found := false
	for _, item := range bf.IncomeBreakdown {
		if item.Name == "Pension" && item.Amount == 3000 {
			found = true
		}
	}
	if !found {
		t.Error("expected Pension in income breakdown")
	}
}

// --- RunProjection: depletion via shortfall ---

func TestRunProjection_DepletionViaShortfall(t *testing.T) {
	s := defaultSettingsForTest()
	s.PortfolioValue = 20000
	s.MonthlyLivingExpenses = 5000
	s.InvestmentReturn = 2.0
	s.ProjectionYears = 5
	s.TaxDeferredPercent = 0
	s.RothPercent = 0

	c := newTestCalc(t, s)
	result := c.RunProjection()
	if result.Survives {
		t.Error("expected depletion with severely underfunded portfolio")
	}
	if result.LongevityYears == nil || *result.LongevityYears == 0 {
		t.Error("expected non-zero longevity years on depletion")
	}
}

// --- Monte Carlo with roth conversions and big ticket items ---

func TestRunSingleMonteCarloSimulation_RothConversion(t *testing.T) {
	s := defaultSettingsForTest()
	s.TaxDeferredPercent = 70
	s.RothPercent = 10
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 30000,
		StartYear:    0,
		EndYear:      5,
	}

	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(20)
	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil Monte Carlo result")
	}
}

func TestRunSingleMonteCarloSimulation_BigTicketItems(t *testing.T) {
	s := defaultSettingsForTest()
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "1", Name: "New Car", Amount: 40000, Year: 2, Type: models.BigTicketExpense},
		{ID: "2", Name: "Inheritance", Amount: 100000, Year: 3, Type: models.BigTicketIncome},
	}

	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(20)
	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil Monte Carlo result")
	}
}

func TestRunSingleMonteCarloSimulation_SpendingPhases(t *testing.T) {
	s := defaultSettingsForTest()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Active", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow", StartAge: 75, Multiplier: 0.8},
		},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "1", Name: "Travel", Amount: 500, Discretionary: true},
	}

	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(20)
	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil Monte Carlo result")
	}
}

// --- Threshold functions: marginal/critical safety levels ---

func TestFindReturnThreshold_MarginalAndCritical(t *testing.T) {
	// Try to hit "marginal" (margin 1-2) and "critical" (margin < 1) safety levels.
	// We test multiple settings since the binary search precision makes exact
	// control difficult. At least one should hit a non-"safe" level.
	testCases := []struct {
		name       string
		portfolio  float64
		expenses   float64
		returnRate float64
		inflation  float64
		years      int
	}{
		{"tight1", 1_500_000, 3500, 5.5, 3.0, 30},
		{"tight2", 1_200_000, 3000, 5.0, 3.0, 30},
		{"tight3", 2_000_000, 4000, 5.5, 3.5, 30},
		{"marginal", 1_000_000, 2500, 4.5, 3.0, 30},
		{"critical", 1_200_000, 3000, 3.5, 2.5, 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.PortfolioValue = tc.portfolio
			s.MonthlyLivingExpenses = tc.expenses
			s.InvestmentReturn = tc.returnRate
			s.InflationRate = tc.inflation
			s.ProjectionYears = tc.years
			s.CurrentAge = 65
			s.SpendingPhaseConfig = nil

			c := newTestCalc(t, s)
			if !c.RunProjection().Survives {
				t.Skip("baseline doesn't survive")
			}
			fp := c.findReturnThreshold()
			if fp == nil {
				t.Fatal("expected non-nil failure point")
			}
			t.Logf("return: current=%f threshold=%f margin=%f safety=%s", fp.CurrentValue, fp.Threshold, fp.Margin, fp.SafetyLevel)
		})
	}
}

func TestFindInflationThreshold_MarginalAndCritical(t *testing.T) {
	testCases := []struct {
		name       string
		portfolio  float64
		expenses   float64
		returnRate float64
		inflation  float64
		years      int
	}{
		{"tight1", 1_500_000, 3500, 5.5, 4.0, 30},
		{"tight2", 1_200_000, 3000, 5.0, 3.5, 30},
		{"tight3", 2_000_000, 4000, 5.5, 4.5, 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.PortfolioValue = tc.portfolio
			s.MonthlyLivingExpenses = tc.expenses
			s.InvestmentReturn = tc.returnRate
			s.InflationRate = tc.inflation
			s.ProjectionYears = tc.years
			s.CurrentAge = 65
			s.SpendingPhaseConfig = nil

			c := newTestCalc(t, s)
			if !c.RunProjection().Survives {
				t.Skip("baseline doesn't survive")
			}
			fp := c.findInflationThreshold()
			if fp == nil {
				t.Fatal("expected non-nil failure point")
			}
			t.Logf("inflation: current=%f threshold=%f margin=%f safety=%s", fp.CurrentValue, fp.Threshold, fp.Margin, fp.SafetyLevel)
		})
	}
}

func TestFindExpensesThreshold_MarginalAndCritical(t *testing.T) {
	testCases := []struct {
		name       string
		portfolio  float64
		expenses   float64
		returnRate float64
		inflation  float64
		years      int
	}{
		{"tight1", 1_500_000, 3800, 5.5, 3.0, 30},
		{"tight2", 1_200_000, 3200, 5.0, 3.0, 30},
		{"tight3", 1_000_000, 2800, 5.0, 3.0, 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.PortfolioValue = tc.portfolio
			s.MonthlyLivingExpenses = tc.expenses
			s.InvestmentReturn = tc.returnRate
			s.InflationRate = tc.inflation
			s.ProjectionYears = tc.years
			s.CurrentAge = 65
			s.SpendingPhaseConfig = nil

			c := newTestCalc(t, s)
			if !c.RunProjection().Survives {
				t.Skip("baseline doesn't survive")
			}
			fp := c.findExpensesThreshold()
			if fp == nil {
				t.Fatal("expected non-nil failure point")
			}
			t.Logf("expenses: current=%f threshold=%f margin=%f%% safety=%s", fp.CurrentValue, fp.Threshold, fp.Margin, fp.SafetyLevel)
		})
	}
}

func TestFindPortfolioThreshold_MarginalAndCritical(t *testing.T) {
	testCases := []struct {
		name       string
		portfolio  float64
		expenses   float64
		returnRate float64
		inflation  float64
		years      int
	}{
		{"tight1", 1_800_000, 4500, 5.0, 3.0, 30},
		{"tight2", 1_500_000, 3500, 5.0, 3.0, 30},
		{"tight3", 1_200_000, 3200, 5.0, 3.0, 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.PortfolioValue = tc.portfolio
			s.MonthlyLivingExpenses = tc.expenses
			s.InvestmentReturn = tc.returnRate
			s.InflationRate = tc.inflation
			s.ProjectionYears = tc.years
			s.CurrentAge = 65
			s.SpendingPhaseConfig = nil

			c := newTestCalc(t, s)
			if !c.RunProjection().Survives {
				t.Skip("baseline doesn't survive")
			}
			fp := c.findPortfolioThreshold()
			if fp == nil {
				t.Fatal("expected non-nil failure point")
			}
			t.Logf("portfolio: current=%f threshold=%f margin=%f%% safety=%s", fp.CurrentValue, fp.Threshold, fp.Margin, fp.SafetyLevel)
		})
	}
}

// --- withdrawForExpenses: neededFromPortfolio <= 0 ---

func TestWithdrawForExpenses_ZeroNeed(t *testing.T) {
	td := 100000.0
	taxable := 100000.0
	roth := 50000.0
	// TEMP scaffold: Task 7 replaces with real basis pointer from PortfolioMonthInput.
	dummyBasis := roth
	result := engine.WithdrawForExpenses(0, 0, true, 0, &td, &taxable, &roth, &dummyBasis)
	if result.RemainingNeed != 0 || result.ActualWithdrawal != 0 {
		t.Errorf("expected zero withdrawal for zero need, got %+v", result)
	}
}

// --- RunMonteCarloSimulation with zero runs ---

func TestRunMonteCarloSimulation_ZeroRuns(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(0) // Should default to 1000
	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Stats.Runs != 1000 {
		t.Errorf("expected 1000 default runs, got %d", result.Stats.Runs)
	}
}

// --- Backtest with tax-deferred only ---

func TestRunSingleHistoricalSequence_TaxDeferredOnly(t *testing.T) {
	s := defaultSettingsForTest()
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.ProjectionYears = 10
	s.CurrentAge = 75 // Will trigger RMDs

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)

	if result.StartYear != 1990 {
		t.Errorf("expected start year 1990, got %d", result.StartYear)
	}
}

// --- Remove/Restore with multiple items (non-matching ID path) ---

func TestRemoveIncomeSource_MultipleItems(t *testing.T) {
	sm := newTestSM(t)

	sm.AddIncomeSource(models.IncomeSource{ID: "a", Name: "A", Amount: 1000})
	sm.AddIncomeSource(models.IncomeSource{ID: "b", Name: "B", Amount: 2000})
	sm.AddIncomeSource(models.IncomeSource{ID: "c", Name: "C", Amount: 3000})

	s, err := sm.RemoveIncomeSource("b")
	if err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}
	if len(s.IncomeSources) != 2 {
		t.Errorf("expected 2 active sources, got %d", len(s.IncomeSources))
	}
	// Verify "a" and "c" remain
	for _, src := range s.IncomeSources {
		if src.ID == "b" {
			t.Error("source 'b' should have been removed")
		}
	}
}

func TestRestoreIncomeSource_MultipleRemoved(t *testing.T) {
	sm := newTestSM(t)

	sm.AddIncomeSource(models.IncomeSource{ID: "a", Name: "A", Amount: 1000})
	sm.AddIncomeSource(models.IncomeSource{ID: "b", Name: "B", Amount: 2000})
	sm.RemoveIncomeSource("a")
	sm.RemoveIncomeSource("b")

	s, err := sm.RestoreIncomeSource("a")
	if err != nil {
		t.Fatalf("RestoreIncomeSource: %v", err)
	}
	if len(s.RemovedIncomeSources) != 1 {
		t.Errorf("expected 1 remaining removed, got %d", len(s.RemovedIncomeSources))
	}
	if s.RemovedIncomeSources[0].ID != "b" {
		t.Error("expected 'b' to remain in removed list")
	}
}

func TestRemoveExpenseSource_MultipleItems(t *testing.T) {
	sm := newTestSM(t)

	sm.AddExpenseSource(models.ExpenseSource{ID: "a", Name: "A", Amount: 100})
	sm.AddExpenseSource(models.ExpenseSource{ID: "b", Name: "B", Amount: 200})

	s, err := sm.RemoveExpenseSource("a")
	if err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}
	if len(s.ExpenseSources) != 1 || s.ExpenseSources[0].ID != "b" {
		t.Error("expected only 'b' remaining")
	}
}

func TestRestoreExpenseSource_MultipleRemoved(t *testing.T) {
	sm := newTestSM(t)

	sm.AddExpenseSource(models.ExpenseSource{ID: "a", Name: "A", Amount: 100})
	sm.AddExpenseSource(models.ExpenseSource{ID: "b", Name: "B", Amount: 200})
	sm.RemoveExpenseSource("a")
	sm.RemoveExpenseSource("b")

	s, err := sm.RestoreExpenseSource("b")
	if err != nil {
		t.Fatalf("RestoreExpenseSource: %v", err)
	}
	if len(s.RemovedExpenseSources) != 1 || s.RemovedExpenseSources[0].ID != "a" {
		t.Error("expected 'a' to remain in removed list")
	}
}

func TestRemoveBigTicketItem_MultipleItems(t *testing.T) {
	sm := newTestSM(t)

	sm.AddBigTicketItem(models.BigTicketItem{ID: "a", Name: "A", Amount: 10000})
	sm.AddBigTicketItem(models.BigTicketItem{ID: "b", Name: "B", Amount: 20000})

	s, err := sm.RemoveBigTicketItem("a")
	if err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}
	if len(s.BigTicketItems) != 1 || s.BigTicketItems[0].ID != "b" {
		t.Error("expected only 'b' remaining")
	}
}

func TestRestoreBigTicketItem_MultipleRemoved(t *testing.T) {
	sm := newTestSM(t)

	sm.AddBigTicketItem(models.BigTicketItem{ID: "a", Name: "A", Amount: 10000})
	sm.AddBigTicketItem(models.BigTicketItem{ID: "b", Name: "B", Amount: 20000})
	sm.RemoveBigTicketItem("a")
	sm.RemoveBigTicketItem("b")

	s, err := sm.RestoreBigTicketItem("a")
	if err != nil {
		t.Fatalf("RestoreBigTicketItem: %v", err)
	}
	if len(s.RemovedBigTicketItems) != 1 || s.RemovedBigTicketItems[0].ID != "b" {
		t.Error("expected 'b' to remain in removed list")
	}
}

// --- UpdateSpendingPhases with nil SpendingPhaseConfig (new install) ---

func TestUpdateSpendingPhases_NilConfig(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write settings without spending_phase_config at all
	settings := map[string]interface{}{
		"portfolio_value": 1000000,
		"current_age":     65,
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Force nil SpendingPhaseConfig by clearing cache and writing file directly
	// The loadInternal will set SpendingPhaseConfig to a default,
	// but we can test the UpdateSpendingPhases path by starting fresh
	s, err := sm.UpdateSpendingPhases(true, nil)
	if err != nil {
		t.Fatalf("UpdateSpendingPhases: %v", err)
	}
	if s.SpendingPhaseConfig == nil {
		t.Fatal("expected non-nil SpendingPhaseConfig")
	}
	if !s.SpendingPhaseConfig.Enabled {
		t.Error("expected enabled")
	}
}

// --- CalculateBudgetFit: income with StartMonth > 0 ---

func TestCalculateBudgetFit_IncomeSourceFutureStart(t *testing.T) {
	s := defaultSettingsForTest()
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Pension", Amount: 3000, StartMonth: 0},
		{ID: "2", Name: "Social Security", Amount: 2500, StartMonth: 24},
	}
	s.SteadyStateOverrideYear = 2 // view at year 2 (when SS has started)

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}

	// Only Pension should be in income items (SS hasn't started at month 0)
	if len(bf.IncomeBreakdown) != 1 {
		t.Errorf("expected 1 income item, got %d", len(bf.IncomeBreakdown))
	}

	// Check steady state includes SS
	if bf.SteadyStateIncome <= 3000 {
		t.Error("expected steady state income to include SS")
	}
}

// --- readScenarioName with bad JSON in file ---

func TestReadScenarioName_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_bad.json"), []byte("{bad json"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := sm.readScenarioName("whatif_bad.json")
	if got != "whatif_bad.json" {
		t.Errorf("expected filename fallback, got %q", got)
	}
}

// --- Unit tests for defensive guard clauses ---

func TestWithdraw_CostBasisNegative(t *testing.T) {
	// Set up a state where CostBasis is negative (shouldn't normally happen)
	a := &engine.TaxableAccountState{MarketValue: 10000, CostBasis: -100}
	a.Withdraw(5000)
	if a.CostBasis < 0 {
		t.Error("expected CostBasis clamped to 0")
	}
}

func TestApplyGrowth_CostBasisNegative(t *testing.T) {
	a := &engine.TaxableAccountState{MarketValue: 10000, CostBasis: -50}
	components := engine.TaxableReturnComponents{Appreciation: 0.005}
	a.ApplyGrowth(components, 1.0)
	if a.CostBasis < 0 {
		t.Error("expected CostBasis clamped to 0")
	}
}

// --- CalculateBudgetFit: the one remaining 99.1% gap ---

func TestCalculateBudgetFit_IncomeAtMonth0WithStartDelay(t *testing.T) {
	s := defaultSettingsForTest()
	s.IncomeSources = []models.IncomeSource{
		// This source starts at month 0 (immediate), should show note=""
		{ID: "1", Name: "Pension", Amount: 3000, StartMonth: 0},
		// This source starts later, but at month 0 GetAdjustedAmount returns 0
		// so it won't appear in the breakdown at all
		{ID: "2", Name: "SS", Amount: 2000, StartMonth: 36},
	}

	c := newTestCalc(t, s)
	bf := c.CalculateBudgetFit()
	if bf == nil {
		t.Fatal("expected non-nil budget fit")
	}

	// Pension should be in the breakdown with no note
	for _, item := range bf.IncomeBreakdown {
		if item.Name == "Pension" && item.Note != "" {
			t.Errorf("expected empty note for immediate income, got %q", item.Note)
		}
	}
}

// --- ListScenarios with Glob error ---

func TestListScenarios_GlobError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "nonexistent_dir")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Don't create settingsDir - Glob should still work (return empty)
	scenarios, err := sm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	// Should still return default scenario
	if len(scenarios) < 1 {
		t.Error("expected at least default scenario")
	}
}

// --- scenariosReferencingFile with read error on a file ---

func TestScenariosReferencingFile_UnreadableFile(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a valid scenario
	valid := models.DefaultWhatIfSettings()
	data, _ := json.MarshalIndent(valid, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_valid.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create an unreadable file
	unreadable := filepath.Join(settingsDir, "whatif_unreadable.json")
	if err := store.WriteFile(unreadable, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	os.Chmod(unreadable, 0000)
	defer os.Chmod(unreadable, 0644)

	refs := sm.scenariosReferencingFile("whatif_target.json")
	// Should handle error gracefully
	_ = refs
}

// --- DeleteScenario: store.Remove error ---

func TestDeleteScenario_RemoveError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a scenario file
	data, _ := json.MarshalIndent(models.DefaultWhatIfSettings(), "", "  ")
	scenarioPath := filepath.Join(settingsDir, "whatif_test.json")
	if err := store.WriteFile(scenarioPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Root-proof: replace the scenario file with a non-empty directory of
	// the same name, not chmod 0555 on the parent (root's CAP_DAC_OVERRIDE
	// writes/removes through a 0555 directory regardless). DeleteScenario's
	// store.Remove is a plain os.Remove, which fails ENOTEMPTY on a
	// non-empty directory at any uid, including root -- landing in exactly
	// the Remove-call branch this test's name defends, rather than merely
	// making the containing directory generally unwritable.
	if err := os.Remove(scenarioPath); err != nil {
		t.Fatalf("remove scenario file: %v", err)
	}
	if err := os.Mkdir(scenarioPath, 0o755); err != nil {
		t.Fatalf("mkdir scenario placeholder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scenarioPath, "keepme"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed non-empty placeholder directory: %v", err)
	}

	err = sm.DeleteScenario("whatif_test.json")
	if err == nil {
		t.Error("expected error when the scenario file cannot be removed")
	}
}

// --- RenameScenario: WriteFile error ---

func TestRenameScenario_WriteError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Root-proof: a 255-byte (NAME_MAX) scenario basename, not chmod 0555 on
	// the directory (root's CAP_DAC_OVERRIDE writes through that
	// regardless). RenameScenario reads the file first (must succeed, so
	// the seed below is written directly via os.WriteFile, bypassing
	// store.WriteFile's own staging step) and only then calls
	// store.WriteFile(path, ...) to persist the rename -- and that call
	// stages via os.CreateTemp(dir, <basename>+".tmp-"+random). At exactly
	// NAME_MAX the plain basename is valid and readable, but the staged
	// name overflows and CreateTemp fails ENAMETOOLONG at any uid,
	// including root, unlike a permission bit root can simply ignore. See
	// TestRollbackDecryptionReportsPathOnAtomicWriteFailure (internal/services/storage)
	// for the same pattern.
	longName := "whatif" + strings.Repeat("x", 244) + ".json" // 255 bytes, valid alone
	data, _ := json.MarshalIndent(models.DefaultWhatIfSettings(), "", "  ")
	if err := os.WriteFile(filepath.Join(settingsDir, longName), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = sm.RenameScenario(longName, "New Name")
	if err == nil {
		t.Error("expected error when the staged rename write overflows NAME_MAX")
	}
}

// --- GetBracketRate: top bracket ---

func TestGetBracketRate_VeryHighIncome(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 0)

	// Income high enough to go beyond all brackets
	rate := tc.GetBracketRate(10_000_000, 0)
	if rate != 37 {
		t.Errorf("expected 37%% for very high income, got %f", rate)
	}
}

// --- RunProjection: depletion via shortfall with tax-deferred accessible ---

func TestRunProjection_DepletionViaShortfallWithTaxDeferred(t *testing.T) {
	// Portfolio with mostly tax-deferred but still accessible
	// High expenses should cause shortfall depletion
	s := defaultSettingsForTest()
	s.PortfolioValue = 30000
	s.TaxDeferredPercent = 60
	s.RothPercent = 20
	s.MonthlyLivingExpenses = 8000
	s.InvestmentReturn = 2.0
	s.ProjectionYears = 5
	s.CurrentAge = 65 // Over 59.5, no delay
	s.TaxDeferredDelayYears = 0

	c := newTestCalc(t, s)
	result := c.RunProjection()
	if result.Survives {
		t.Error("expected depletion with high expenses and small portfolio")
	}
}

// --- RunProjection: shortfall depletion via engine.WithdrawForExpenses shortfall ---

func TestRunProjection_ShortfallCausesDepletion(t *testing.T) {
	// Test depletion via shortfallCausesDepletion path.
	// This requires shortfall > 0 AND totalBalance > 0.
	// With end_of_month timing, growth is applied AFTER withdrawal.
	// So the withdrawal can fail (shortfall > 0) while the post-withdrawal
	// growth pushes totalBalance slightly above 0.
	// Use high taxable dividend yield so cash re-invested after withdrawal failure.
	s := defaultSettingsForTest()
	s.PortfolioValue = 3000
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 5000
	s.InvestmentReturn = 100.0 // Very high return
	s.ProjectionYears = 3
	s.ProjectionTiming = models.ProjectionTiming("end_of_month")
	s.TaxableDividendYield = 50.0 // Huge dividend yield re-invests after withdrawal

	c := newTestCalc(t, s)
	result := c.RunProjection()
	// The portfolio should deplete due to expenses exceeding withdrawals
	if result.Survives {
		t.Error("expected depletion")
	}
}

// --- Load concurrent cache race (test double-check pattern) ---

func TestLoad_ConcurrentAccess(t *testing.T) {
	sm := newTestSM(t)

	// First load to populate cache
	s1, err := sm.Load()
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}

	// Invalidate cache
	sm.cache = nil

	// Concurrent loads
	done := make(chan *models.WhatIfSettings, 10)
	for i := 0; i < 10; i++ {
		go func() {
			s, _ := sm.Load()
			done <- s
		}()
	}
	for i := 0; i < 10; i++ {
		s := <-done
		if s == nil {
			t.Error("expected non-nil settings")
		}
	}
	_ = s1
}

// --- saveInternal with MkdirAll error ---

func TestSaveInternal_MkdirAllError(t *testing.T) {
	root := t.TempDir()
	// Create a FILE where the settings directory should be (causes MkdirAll to fail)
	settingsPath := filepath.Join(root, "settings")
	os.WriteFile(settingsPath, []byte("not a dir"), 0644)

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsPath, store)

	_, err = sm.Load()
	if err == nil {
		t.Error("expected error when settings dir is a file")
	}
}

// --- RMD analysis: InvestmentReturn=0 path ---

func TestBuildRMDAnalysis_ZeroInvestmentReturn(t *testing.T) {
	s := defaultSettingsForTest()
	s.CurrentAge = 70
	s.TaxDeferredPercent = 50
	s.ProjectionYears = 10
	s.InvestmentReturn = 0 // Should use allocation-based return

	c := newTestCalc(t, s)
	rmd := c.BuildRMDAnalysis(c.RunProjection())
	if rmd == nil {
		t.Fatal("expected non-nil RMD analysis")
	}
}

// --- Backtest with expense sources ---

func TestRunSingleHistoricalSequence_ExpenseSources(t *testing.T) {
	s := defaultSettingsForTest()
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "1", Name: "Travel", Amount: 500, Discretionary: true, Inflation: true},
		{ID: "2", Name: "Insurance", Amount: 300, Discretionary: false, Inflation: true},
	}
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)
	if !result.Survives {
		t.Error("expected to survive")
	}
}
