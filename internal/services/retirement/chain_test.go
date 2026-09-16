package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
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
		{Name: "Gym", StartMonth: 0, EndMonth: nil, Amount: 100},
		{Name: "Tuition", StartMonth: 2 * 12, EndMonth: intPtr(5 * 12), Amount: 500},
		{Name: "Expired", StartMonth: 0, EndMonth: intPtr(1 * 12), Amount: 200},
	}
	result := rebaseExpenseSources(sources, 3*12)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].StartMonth != 0 || result[0].EndMonth != nil {
		t.Errorf("Gym: start=%d, end=%v", result[0].StartMonth, result[0].EndMonth)
	}
	if result[1].EndMonth == nil || *result[1].EndMonth != 24 {
		t.Errorf("Tuition EndMonth: expected 24, got %v", result[1].EndMonth)
	}
}

// A transition that does not land on a year boundary rebases in months, the
// unit the schedule is actually stored in.
func TestRebaseExpenseSourcesNonYearAlignedTransition(t *testing.T) {
	sources := []models.ExpenseSource{
		{Name: "Tuition", StartMonth: 24, EndMonth: intPtr(60), Amount: 500},
	}
	result := rebaseExpenseSources(sources, 11)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].StartMonth != 13 || result[0].EndMonth == nil || *result[0].EndMonth != 49 {
		t.Errorf("Tuition: start=%d end=%v, want 13/49", result[0].StartMonth, result[0].EndMonth)
	}
}

func TestRebaseBigTicketItems(t *testing.T) {
	items := []models.BigTicketItem{
		{Name: "Home Sale", Month: 5 * 12, Amount: 200000},
		{Name: "Past Event", Month: 1 * 12, Amount: 50000},
		{Name: "At Transition", Month: 3 * 12, Amount: 100000},
	}
	result := rebaseBigTicketItems(items, 3*12)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Month != 0 {
		t.Errorf("At Transition: expected month 0, got %d", result[0].Month)
	}
	if result[1].Month != 24 {
		t.Errorf("Home Sale: expected month 24, got %d", result[1].Month)
	}
}

// Sorting and dropping are by month, so two items in the same year keep their
// real order rather than an arbitrary one.
func TestRebaseBigTicketItemsSortsByMonth(t *testing.T) {
	items := []models.BigTicketItem{
		{Name: "Later in year 3", Month: 3*12 + 7, Amount: 1},
		{Name: "One month before the transition", Month: 3*12 - 1, Amount: 2},
		{Name: "At transition", Month: 3 * 12, Amount: 3},
	}
	result := rebaseBigTicketItems(items, 3*12)
	if len(result) != 2 {
		t.Fatalf("expected 2 (the pre-transition item is dropped), got %d", len(result))
	}
	if result[0].Name != "At transition" || result[0].Month != 0 {
		t.Errorf("first = %q at month %d, want At transition at 0", result[0].Name, result[0].Month)
	}
	if result[1].Name != "Later in year 3" || result[1].Month != 7 {
		t.Errorf("second = %q at month %d, want Later in year 3 at 7", result[1].Name, result[1].Month)
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

	calcChain := newTestCalcWithChain(t, primary, []engine.PreparedChainLink{
		preparedLink(t, "", 70, linked),
	})
	calcNoChain := newTestCalc(t, primary)

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

func TestFailurePoints_ChainPropagated(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	calcChain := newTestCalcWithChain(t, primary, []engine.PreparedChainLink{
		preparedLink(t, "", 70, linked),
	})
	calcNoChain := newTestCalc(t, primary)

	fpChain := calcChain.CalculateFailurePoints()
	fpNoChain := calcNoChain.CalculateFailurePoints()

	if fpChain == nil || fpNoChain == nil {
		t.Fatal("expected non-nil failure point results")
	}
	if !fpChain.BaselineSurvives {
		t.Fatal("expected chained baseline to survive")
	}
	if !fpNoChain.BaselineSurvives {
		t.Fatal("expected non-chained baseline to survive")
	}
	if len(fpChain.FailurePoints) == 0 || len(fpNoChain.FailurePoints) == 0 {
		t.Fatal("expected failure points from both analyses")
	}

	// Chain raises expenses at age 70, so thresholds should differ.
	anyDifferent := false
	for _, fpC := range fpChain.FailurePoints {
		for _, fpN := range fpNoChain.FailurePoints {
			if fpC.ParamName == fpN.ParamName && fpC.Threshold != fpN.Threshold {
				anyDifferent = true
				break
			}
		}
		if anyDifferent {
			break
		}
	}
	if !anyDifferent {
		t.Error("expected at least one failure point threshold to differ with chain")
	}
}

func TestPrepareChainedSettings(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: "1966-04", Role: models.PersonRolePrimary},
		{ID: "spouse", Name: "Spouse", BirthMonth: "1968-04", Role: models.PersonRoleSpouse},
	}
	prepare.ComputeAges(primary)

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 3000

	result := mustPrepareChained(t, linked, primary, 10)
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

// mustPrepareChained runs prepareChainedSettings and fails the test on a
// validation error, returning the prepared snapshot.
func mustPrepareChained(t *testing.T, linked, primary *models.WhatIfSettings, transitionYear int) *models.WhatIfSettings {
	t.Helper()
	prepared, err := prepareChainedSettings(linked, primary, transitionYear)
	if err != nil {
		t.Fatalf("prepareChainedSettings: %v", err)
	}
	return prepared.Settings()
}

// expenseEndPtr returns the *int an ExpenseSource's EndMonth needs (nil means
// perpetual, so a real end month must be addressable).
func expenseEndPtr(month int) *int { return &month }
