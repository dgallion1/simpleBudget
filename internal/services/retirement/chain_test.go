package retirement

import (
	"testing"

	"budget2/internal/models"
)

func intPtr(v int) *int { return &v }

func TestRebaseIncomeSources(t *testing.T) {
	sources := []models.IncomeSource{
		{Name: "SS", StartMonth: 0, EndMonth: nil, Amount: 2000},
		{Name: "Part-time", StartMonth: 24, EndMonth: intPtr(60), Amount: 1000},
		{Name: "Expired", StartMonth: 0, EndMonth: intPtr(12), Amount: 500},
	}
	result := rebaseIncomeSources(sources, 36)
	if len(result) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(result))
	}
	if result[0].StartMonth != 0 || result[0].EndMonth != nil {
		t.Errorf("SS: start=%d, end=%v", result[0].StartMonth, result[0].EndMonth)
	}
	if result[1].StartMonth != 0 || result[1].EndMonth == nil || *result[1].EndMonth != 24 {
		t.Errorf("Part-time: start=%d, end=%v", result[1].StartMonth, result[1].EndMonth)
	}
}

func TestRebaseExpenseSources(t *testing.T) {
	sources := []models.ExpenseSource{
		{Name: "Gym", StartYear: 0, EndYear: 0, Amount: 100},
		{Name: "Tuition", StartYear: 2, EndYear: 5, Amount: 500},
		{Name: "Expired", StartYear: 0, EndYear: 1, Amount: 200},
	}
	result := rebaseExpenseSources(sources, 3)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].StartYear != 0 {
		t.Errorf("Gym StartYear: expected 0, got %d", result[0].StartYear)
	}
	if result[1].EndYear != 2 {
		t.Errorf("Tuition EndYear: expected 2, got %d", result[1].EndYear)
	}
}

func TestRebaseBigTicketItems(t *testing.T) {
	items := []models.BigTicketItem{
		{Name: "Home Sale", Year: 5, Amount: 200000},
		{Name: "Past Event", Year: 1, Amount: 50000},
		{Name: "At Transition", Year: 3, Amount: 100000},
	}
	result := rebaseBigTicketItems(items, 3)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Year != 0 {
		t.Errorf("At Transition: expected year 0, got %d", result[0].Year)
	}
	if result[1].Year != 2 {
		t.Errorf("Home Sale: expected year 2, got %d", result[1].Year)
	}
}

func TestRebaseRothConversion(t *testing.T) {
	config := &models.RothConversionConfig{Enabled: true, AnnualAmount: 50000, StartYear: 2, EndYear: 8}
	result := rebaseRothConversion(config, 3)
	if result == nil || result.StartYear != 0 || result.EndYear != 5 {
		t.Errorf("expected start=0, end=5, got %+v", result)
	}
}

func TestRebaseRothConversion_ExpiredDisabled(t *testing.T) {
	config := &models.RothConversionConfig{Enabled: true, AnnualAmount: 50000, StartYear: 0, EndYear: 2}
	result := rebaseRothConversion(config, 3)
	if result != nil && result.Enabled {
		t.Error("expected disabled for expired conversion")
	}
}

func TestSensitivity_ChainPropagated(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	calcChain := NewCalculatorWithChain(primary, []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	})
	calcNoChain := NewCalculator(primary)

	sensChain := calcChain.CalculateSensitivity()
	sensNoChain := calcNoChain.CalculateSensitivity()

	if len(sensChain) == 0 || len(sensNoChain) == 0 {
		t.Fatal("expected sensitivity results")
	}

	// Chain raises expenses at age 70, so final balances should differ even when both survive.
	// LongevityYears is only non-zero when the portfolio fails, so compare FinalBalance instead.
	anyDifferent := false
	for i := range sensChain {
		if sensChain[i].FinalBalance != sensNoChain[i].FinalBalance {
			anyDifferent = true
			break
		}
	}
	if !anyDifferent {
		t.Error("expected at least one sensitivity scenario to differ with chain")
	}
}

func TestPrepareChainedSettings(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.SpouseAge = 58

	linked := models.DefaultWhatIfSettings()
	linked.CurrentAge = 70
	linked.SpouseAge = 68
	linked.MonthlyLivingExpenses = 3000

	result := prepareChainedSettings(linked, primary, 10)
	if result.CurrentAge != 60 {
		t.Errorf("CurrentAge: expected 60, got %d", result.CurrentAge)
	}
	if result.SpouseAge != 58 {
		t.Errorf("SpouseAge: expected 58, got %d", result.SpouseAge)
	}
	if result.MonthlyLivingExpenses != 3000 {
		t.Errorf("Expenses: expected 3000, got %f", result.MonthlyLivingExpenses)
	}
}
