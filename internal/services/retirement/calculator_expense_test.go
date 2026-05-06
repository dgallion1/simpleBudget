package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestCalculateTotalExpenses(t *testing.T) {
	t.Run("base expenses only", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 4000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.SpendingPhaseConfig = nil
		s.InflationRate = 3.0
		s.SpendingDeclineRate = 1.0

		calc := NewCalculator(s)

		// Month 0: no inflation applied
		got := calc.CalculateTotalExpenses(0)
		if math.Abs(got-4000) > 0.01 {
			t.Errorf("month 0: want 4000, got %.2f", got)
		}

		// Month 12 (year 1): net inflation = (3-1)/100 = 0.02
		got = calc.CalculateTotalExpenses(12)
		want := 4000 * math.Pow(1.02, 1)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 12: want %.2f, got %.2f", want, got)
		}

		// Month 6: net inflation compounds monthly instead of stair-stepping annually
		got = calc.CalculateTotalExpenses(6)
		want = 4000 * math.Pow(1.02, 0.5)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 6: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("with healthcare persons pre and post Medicare", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.SpendingPhaseConfig = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.ExpenseSources = nil

		// Person aged 60, ACA coverage, transitions to Medicare at 65
		s.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "p1",
				Name:                  "Primary",
				CurrentAge:            60,
				CurrentCoverage:       models.CoverageACA,
				CurrentMonthlyCost:    1000,
				MedicareMonthlyCost:   400,
				PreMedicareInflation:  0,
				PostMedicareInflation: 0,
				MedicareEligibleAge:   65,
			},
		}

		calc := NewCalculator(s)

		// Month 0: pre-Medicare, ACA cost
		got := calc.CalculateTotalExpenses(0)
		want := 3000.0 + 1000.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 0: want %.2f, got %.2f", want, got)
		}

		// Month 60 (year 5): age 65, should be on Medicare
		got = calc.CalculateTotalExpenses(60)
		want = 3000.0 + 400.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 60: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("with expense sources inflation and no inflation", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 2000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.SpendingPhaseConfig = nil
		s.InflationRate = 5.0
		s.SpendingDeclineRate = 5.0 // net inflation = 0 for living expenses
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "e1", Name: "Inflating", Amount: 500, StartYear: 0, EndYear: 0, Inflation: true},
			{ID: "e2", Name: "Fixed", Amount: 300, StartYear: 0, EndYear: 0, Inflation: false},
		}

		calc := NewCalculator(s)

		// Month 0: living=2000, inflating=500, fixed=300
		got := calc.CalculateTotalExpenses(0)
		if math.Abs(got-2800) > 0.01 {
			t.Errorf("month 0: want 2800, got %.2f", got)
		}

		// Month 24 (year 2): living stays 2000 (net inflation=0), inflating grows, fixed stays
		got = calc.CalculateTotalExpenses(24)
		wantInflating := 500 * math.Pow(1.05, 2)
		want := 2000 + wantInflating + 300
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 24: want %.2f, got %.2f", want, got)
		}

		got = calc.CalculateTotalExpenses(6)
		wantInflating = 500 * math.Pow(1.05, 0.5)
		want = 2000 + wantInflating + 300
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 6: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("with spending phases enabled", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 4000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.InflationRate = 0
		s.CurrentAge = 65
		s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
				{Name: "Slow-Go", StartAge: 75, Multiplier: 0.80},
			},
		}

		calc := NewCalculator(s)

		// Month 0: age 65, Go-Go phase, multiplier 1.0
		got := calc.CalculateTotalExpenses(0)
		if math.Abs(got-4000) > 0.01 {
			t.Errorf("month 0: want 4000, got %.2f", got)
		}

		// Month 120 (year 10): age 75, Slow-Go phase, multiplier 0.80
		got = calc.CalculateTotalExpenses(120)
		want := 4000 * 0.80
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 120: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("discretionary expense gets phase multiplier", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 1000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.InflationRate = 0
		s.CurrentAge = 75
		s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "Slow-Go", StartAge: 0, Multiplier: 0.50},
			},
		}
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "d1", Name: "Travel", Amount: 600, Discretionary: true},
			{ID: "e1", Name: "Insurance", Amount: 400, Discretionary: false},
		}

		calc := NewCalculator(s)
		got := calc.CalculateTotalExpenses(0)
		// living=1000*0.5=500, travel=600*0.5=300, insurance=400 (non-discretionary, no phase multiplier)
		want := 500.0 + 300.0 + 400.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("want %.2f, got %.2f", want, got)
		}
	})
}

func TestCalculateExpenseBreakdown(t *testing.T) {
	t.Run("essential and discretionary categorization", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.SpendingPhaseConfig = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "d1", Name: "Dining", Amount: 500, Discretionary: true},
			{ID: "e1", Name: "Utilities", Amount: 200, Discretionary: false},
		}

		calc := NewCalculator(s)
		bd := calc.CalculateExpenseBreakdown(0)

		// Essential = living(3000) + utilities(200)
		if math.Abs(bd.Essential-3200) > 0.01 {
			t.Errorf("essential: want 3200, got %.2f", bd.Essential)
		}
		if math.Abs(bd.Discretionary-500) > 0.01 {
			t.Errorf("discretionary: want 500, got %.2f", bd.Discretionary)
		}
		if math.Abs(bd.Total-3700) > 0.01 {
			t.Errorf("total: want 3700, got %.2f", bd.Total)
		}
	})

	t.Run("with healthcare persons", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 2000
		s.MonthlyHealthcare = 0
		s.SpendingPhaseConfig = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.ExpenseSources = nil
		s.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "p1",
				Name:                  "Test",
				CurrentAge:            70,
				CurrentCoverage:       models.CoverageMedicare,
				CurrentMonthlyCost:    450,
				PostMedicareInflation: 0,
				MedicareEligibleAge:   65,
			},
		}

		calc := NewCalculator(s)
		bd := calc.CalculateExpenseBreakdown(0)

		// Healthcare is essential: living(2000) + healthcare(450)
		if math.Abs(bd.Essential-2450) > 0.01 {
			t.Errorf("essential: want 2450, got %.2f", bd.Essential)
		}
		if bd.Discretionary != 0 {
			t.Errorf("discretionary: want 0, got %.2f", bd.Discretionary)
		}
	})

	t.Run("with spending phases and discretionary", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 2000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.InflationRate = 0
		s.CurrentAge = 80
		s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "No-Go", StartAge: 0, Multiplier: 0.50},
			},
		}
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "d1", Name: "Travel", Amount: 1000, Discretionary: true},
			{ID: "e1", Name: "Tax", Amount: 500, Discretionary: false},
		}

		calc := NewCalculator(s)
		bd := calc.CalculateExpenseBreakdown(0)

		// Essential = living(2000*0.5=1000) + tax(500, no phase multiplier on non-discretionary expense source)
		// Discretionary = travel(1000*0.5=500, phase multiplier applied)
		if math.Abs(bd.Essential-1500) > 0.01 {
			t.Errorf("essential: want 1500, got %.2f", bd.Essential)
		}
		if math.Abs(bd.Discretionary-500) > 0.01 {
			t.Errorf("discretionary: want 500, got %.2f", bd.Discretionary)
		}
		if math.Abs(bd.Total-2000) > 0.01 {
			t.Errorf("total: want 2000, got %.2f", bd.Total)
		}
	})

	t.Run("with inflation at later month", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 1000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.SpendingPhaseConfig = nil
		s.InflationRate = 10.0
		s.SpendingDeclineRate = 0
		s.ExpenseSources = nil

		calc := NewCalculator(s)
		bd := calc.CalculateExpenseBreakdown(12) // year 1

		// Simple decline: netInflation = (10-0)/100 = 0.10
		want := 1000 * math.Pow(1.10, 1)
		if math.Abs(bd.Essential-want) > 0.01 {
			t.Errorf("essential: want %.2f, got %.2f", want, bd.Essential)
		}
	})
}

func TestCalculateBudgetFit(t *testing.T) {
	t.Run("positive gap with required rate", func(t *testing.T) {
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

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyExpenses < 4999 {
			t.Errorf("monthly expenses: want ~5000, got %.2f", fit.MonthlyExpenses)
		}
		if fit.MonthlyGap <= 0 {
			t.Errorf("monthly gap should be positive, got %.2f", fit.MonthlyGap)
		}
		if fit.RequiredRate <= 0 {
			t.Errorf("required rate should be positive, got %.4f", fit.RequiredRate)
		}
		// RequiredRate = (annualGap / portfolio) * 100 = (5000*12 / 1_000_000) * 100 = 6.0
		wantRate := 6.0
		if math.Abs(fit.RequiredRate-wantRate) > 0.01 {
			t.Errorf("required rate: want %.2f, got %.2f", wantRate, fit.RequiredRate)
		}
	})

	t.Run("income covers expenses so no gap", func(t *testing.T) {
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
			{ID: "ss", Name: "Social Security", Amount: 3500, StartMonth: 0},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap >= 0 {
			t.Errorf("monthly gap should be negative (surplus), got %.2f", fit.MonthlyGap)
		}
		if fit.RequiredRate != 0 {
			t.Errorf("required rate should be 0 when income covers expenses, got %.4f", fit.RequiredRate)
		}
		if fit.MonthlyIncome < 3499 {
			t.Errorf("monthly income: want ~3500, got %.2f", fit.MonthlyIncome)
		}
	})

	t.Run("expense breakdown items populated", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 500_000
		s.MonthlyLivingExpenses = 2000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "e1", Name: "Property Tax", Amount: 400, EndYear: 5},
		}
		s.IncomeSources = []models.IncomeSource{
			{ID: "i1", Name: "Pension", Amount: 1000, StartMonth: 0},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		if len(fit.ExpenseBreakdown) < 2 {
			t.Fatalf("expected at least 2 expense breakdown items, got %d", len(fit.ExpenseBreakdown))
		}
		// First item should be living expenses
		if fit.ExpenseBreakdown[0].Name != "Living Expenses" {
			t.Errorf("first breakdown: want 'Living Expenses', got %q", fit.ExpenseBreakdown[0].Name)
		}
		// Find property tax entry
		found := false
		for _, item := range fit.ExpenseBreakdown {
			if item.Name == "Property Tax" {
				found = true
				if item.Note != "ends year 5" {
					t.Errorf("property tax note: want 'ends year 5', got %q", item.Note)
				}
			}
		}
		if !found {
			t.Error("expected Property Tax in expense breakdown")
		}

		// Income breakdown
		if len(fit.IncomeBreakdown) < 1 {
			t.Fatalf("expected at least 1 income breakdown item, got %d", len(fit.IncomeBreakdown))
		}
		if fit.IncomeBreakdown[0].Name != "Pension" {
			t.Errorf("income breakdown: want 'Pension', got %q", fit.IncomeBreakdown[0].Name)
		}
	})

	t.Run("high expenses cause large required rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 100_000
		s.MonthlyLivingExpenses = 10000
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

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// 10000*12 / 100000 * 100 = 120%
		if fit.RequiredRate < 100 {
			t.Errorf("required rate should be very high (>100%%), got %.2f", fit.RequiredRate)
		}
	})
}

func TestApplyBigTicketExpense(t *testing.T) {
	t.Run("fully funded from taxable", func(t *testing.T) {
		taxDeferred := 100000.0
		taxable := 50000.0
		roth := 20000.0

		remaining := applyBigTicketExpense(30000, false, 0, &taxDeferred, &taxable, &roth)

		if remaining != 0 {
			t.Errorf("remaining: want 0, got %.2f", remaining)
		}
		if math.Abs(taxable-20000) > 0.01 {
			t.Errorf("taxable: want 20000, got %.2f", taxable)
		}
		if taxDeferred != 100000 {
			t.Errorf("taxDeferred should be unchanged, got %.2f", taxDeferred)
		}
	})

	t.Run("spills from taxable to roth", func(t *testing.T) {
		taxDeferred := 100000.0
		taxable := 10000.0
		roth := 50000.0

		remaining := applyBigTicketExpense(30000, false, 0, &taxDeferred, &taxable, &roth)

		if remaining != 0 {
			t.Errorf("remaining: want 0, got %.2f", remaining)
		}
		if taxable != 0 {
			t.Errorf("taxable: want 0, got %.2f", taxable)
		}
		if math.Abs(roth-30000) > 0.01 {
			t.Errorf("roth: want 30000, got %.2f", roth)
		}
	})

	t.Run("allowTaxDeferred true with no penalty", func(t *testing.T) {
		taxDeferred := 100000.0
		taxable := 0.0
		roth := 0.0

		remaining := applyBigTicketExpense(25000, true, 0.0, &taxDeferred, &taxable, &roth)

		if math.Abs(remaining) > 0.01 {
			t.Errorf("remaining: want 0, got %.2f", remaining)
		}
		if math.Abs(taxDeferred-75000) > 0.01 {
			t.Errorf("taxDeferred: want 75000, got %.2f", taxDeferred)
		}
	})

	t.Run("allowTaxDeferred true with 10% penalty", func(t *testing.T) {
		taxDeferred := 100000.0
		taxable := 0.0
		roth := 0.0

		// Need 20000 in spending. With 10% penalty, effective factor = 0.9
		// grossNeeded = 20000/0.9 = 22222.22
		remaining := applyBigTicketExpense(20000, true, 0.10, &taxDeferred, &taxable, &roth)

		if math.Abs(remaining) > 0.01 {
			t.Errorf("remaining: want ~0, got %.2f", remaining)
		}
		// taxDeferred should have been reduced by grossNeeded = 20000/0.9 ≈ 22222.22
		expectedTD := 100000 - 20000/0.9
		if math.Abs(taxDeferred-expectedTD) > 0.01 {
			t.Errorf("taxDeferred: want %.2f, got %.2f", expectedTD, taxDeferred)
		}
	})

	t.Run("allowTaxDeferred false leaves remainder", func(t *testing.T) {
		taxDeferred := 100000.0
		taxable := 5000.0
		roth := 5000.0

		remaining := applyBigTicketExpense(20000, false, 0, &taxDeferred, &taxable, &roth)

		if math.Abs(remaining-10000) > 0.01 {
			t.Errorf("remaining: want 10000, got %.2f", remaining)
		}
		if taxable != 0 {
			t.Errorf("taxable: want 0, got %.2f", taxable)
		}
		if roth != 0 {
			t.Errorf("roth: want 0, got %.2f", roth)
		}
		// taxDeferred unchanged since allowTaxDeferred=false
		if taxDeferred != 100000 {
			t.Errorf("taxDeferred should be unchanged, got %.2f", taxDeferred)
		}
	})

	t.Run("penalty with limited taxDeferred balance", func(t *testing.T) {
		taxDeferred := 5000.0
		taxable := 0.0
		roth := 0.0

		// Need 20000, but only 5000 in tax-deferred with 10% penalty
		// grossNeeded = 20000/0.9 = 22222.22, capped to 5000
		// netSpending = 5000 * 0.9 = 4500
		// remaining = 20000 - 4500 = 15500
		remaining := applyBigTicketExpense(20000, true, 0.10, &taxDeferred, &taxable, &roth)

		if math.Abs(remaining-15500) > 0.01 {
			t.Errorf("remaining: want 15500, got %.2f", remaining)
		}
		if taxDeferred != 0 {
			t.Errorf("taxDeferred: want 0, got %.2f", taxDeferred)
		}
	})
}

func TestRunMonteCarloSimulationWithDiscretionary(t *testing.T) {
	t.Run("triggers adaptive spending path with discretionary expenses", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.IncomeSources = nil
		s.InflationRate = 3.0
		s.SpendingDeclineRate = 1.0
		s.InvestmentReturn = 7.0 // Explicit return avoids allocation-based calc
		s.CurrentAge = 65
		s.ProjectionYears = 30
		s.SpendingPhaseConfig = nil
		s.ExpenseSources = []models.ExpenseSource{
			{ID: "d1", Name: "Travel", Amount: 800, Discretionary: true},
			{ID: "d2", Name: "Dining", Amount: 400, Discretionary: true},
			{ID: "e1", Name: "Insurance", Amount: 300, Discretionary: false},
		}

		calc := NewCalculator(s)
		// Need at least 100 runs for calculateSequenceRiskBreakdown to return non-nil
		result := calc.RunMonteCarloSimulation(100)

		if result.Stats.Runs != 100 {
			t.Errorf("runs: want 100, got %d", result.Stats.Runs)
		}
		if result.Stats.SuccessRate < 0 || result.Stats.SuccessRate > 100 {
			t.Errorf("success rate out of bounds: %.2f", result.Stats.SuccessRate)
		}
		// With discretionary expenses, SequenceRisk.HasDiscretionary should be true
		// and the adaptive spending path should have been executed
		if result.Stats.SequenceRisk == nil {
			t.Fatal("expected SequenceRisk to be populated")
		}
		if !result.Stats.SequenceRisk.HasDiscretionary {
			t.Error("expected HasDiscretionary to be true with discretionary expense sources")
		}
		// Adaptive spending fields should be populated
		if result.Stats.SequenceRisk.MonthlyDiscretionary <= 0 {
			t.Errorf("expected positive MonthlyDiscretionary, got %.2f", result.Stats.SequenceRisk.MonthlyDiscretionary)
		}
		if result.Stats.SequenceRisk.MonthlyEssential <= 0 {
			t.Errorf("expected positive MonthlyEssential, got %.2f", result.Stats.SequenceRisk.MonthlyEssential)
		}
		// AdaptationRationale should be set (one of the four possible messages)
		if result.Stats.SequenceRisk.AdaptationRationale == "" {
			t.Error("expected AdaptationRationale to be set")
		}
	})
}

func TestCalculateBudgetFitEmployerCoverage(t *testing.T) {
	t.Run("healthcare persons with employer coverage and zero cost", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 60
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.IncomeSources = nil
		s.ExpenseSources = nil
		// Person with employer coverage, $0 current cost, 3 years remaining
		s.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "p1",
				Name:                  "Primary",
				CurrentAge:            60,
				CurrentCoverage:       models.CoverageEmployer,
				CurrentMonthlyCost:    0,
				EmployerCoverageYears: 3,
				ACACostAfterEmployer:  800,
				MedicareMonthlyCost:   400,
				PreMedicareInflation:  0,
				PostMedicareInflation: 0,
				MedicareEligibleAge:   65,
			},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Healthcare cost is $0 at month 0 (employer covered), so GetTotalHealthcareCost(0) == 0
		// The "employer covered" note path should be triggered
		foundHealthcare := false
		for _, item := range fit.ExpenseBreakdown {
			if item.Name == "Healthcare" {
				foundHealthcare = true
				if item.Amount != 0 {
					t.Errorf("healthcare amount: want 0, got %.2f", item.Amount)
				}
				// Should contain "employer covered" with years
				if item.Note != "employer covered (3 yr)" {
					t.Errorf("healthcare note: want 'employer covered (3 yr)', got %q", item.Note)
				}
			}
		}
		if !foundHealthcare {
			t.Error("expected Healthcare in expense breakdown")
		}
	})

	t.Run("healthcare persons with zero cost and no employer years", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.MonthlyHealthcare = 0
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 60
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.IncomeSources = nil
		s.ExpenseSources = nil
		// Person with employer coverage, $0 cost, 0 employer years (indefinite)
		s.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "p1",
				Name:                  "Primary",
				CurrentAge:            60,
				CurrentCoverage:       models.CoverageEmployer,
				CurrentMonthlyCost:    0,
				EmployerCoverageYears: 0,
				MedicareMonthlyCost:   400,
				PreMedicareInflation:  0,
				PostMedicareInflation: 0,
				MedicareEligibleAge:   65,
			},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		foundHealthcare := false
		for _, item := range fit.ExpenseBreakdown {
			if item.Name == "Healthcare" {
				foundHealthcare = true
				// No EmployerCoverageYears > 0, so note should be plain "employer covered"
				if item.Note != "employer covered" {
					t.Errorf("healthcare note: want 'employer covered', got %q", item.Note)
				}
			}
		}
		if !foundHealthcare {
			t.Error("expected Healthcare in expense breakdown")
		}
	})
}

func TestCalculateBudgetFitRMD(t *testing.T) {
	t.Run("RMD partial coverage of gap", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 500_000
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 75 // Above RMDStartAge (73)
		s.TaxDeferredPercent = 80
		s.RothPercent = 10
		s.SpendingPhaseConfig = nil

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Should have positive MonthlyRMD since age >= 73 and TaxDeferredPercent > 0
		if fit.MonthlyRMD <= 0 {
			t.Fatalf("expected positive MonthlyRMD, got %.2f", fit.MonthlyRMD)
		}
		// Gap before RMD = expenses - income = 5000 - 0 = 5000
		if math.Abs(fit.GapBeforeRMD-5000) > 0.01 {
			t.Errorf("GapBeforeRMD: want 5000, got %.2f", fit.GapBeforeRMD)
		}
		// RMD only partially covers the gap (500k * 80% = 400k tax-deferred)
		// At age 75, life expectancy factor ~24.6, annual RMD ~16260, monthly ~1355
		// So RMDCoverage should equal MonthlyRMD (partial coverage)
		if fit.RMDCoverage <= 0 {
			t.Errorf("expected positive RMDCoverage, got %.2f", fit.RMDCoverage)
		}
		if math.Abs(fit.RMDCoverage-fit.MonthlyRMD) > 0.01 {
			t.Errorf("RMDCoverage should equal MonthlyRMD for partial coverage: RMDCoverage=%.2f, MonthlyRMD=%.2f", fit.RMDCoverage, fit.MonthlyRMD)
		}
		if fit.ExcessRMD != 0 {
			t.Errorf("expected ExcessRMD=0 for partial coverage, got %.2f", fit.ExcessRMD)
		}
	})

	t.Run("RMD fully covers gap with excess", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 2_000_000
		s.MonthlyLivingExpenses = 1000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 75
		s.TaxDeferredPercent = 90
		s.RothPercent = 5
		s.SpendingPhaseConfig = nil

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Large tax-deferred balance at age 75 means big RMD
		// 2M * 90% = 1.8M, at age 75 factor ~24.6, annual RMD ~73170, monthly ~6097
		// Gap before RMD = 1000, so RMD fully covers it
		if fit.MonthlyRMD <= 0 {
			t.Fatalf("expected positive MonthlyRMD, got %.2f", fit.MonthlyRMD)
		}
		if math.Abs(fit.RMDCoverage-fit.GapBeforeRMD) > 0.01 {
			t.Errorf("RMDCoverage should equal GapBeforeRMD for full coverage: RMDCoverage=%.2f, GapBeforeRMD=%.2f", fit.RMDCoverage, fit.GapBeforeRMD)
		}
		if fit.ExcessRMD <= 0 {
			t.Errorf("expected positive ExcessRMD, got %.2f", fit.ExcessRMD)
		}
		monthlyTaxesBeforeRMD := fit.GapBeforeRMD - fit.MonthlyExpenses + fit.MonthlyIncome
		netRMD := fit.MonthlyRMD - (fit.MonthlyTaxes - monthlyTaxesBeforeRMD)
		wantExcess := netRMD - fit.GapBeforeRMD
		if math.Abs(fit.ExcessRMD-wantExcess) > 0.01 {
			t.Errorf("ExcessRMD: want %.2f, got %.2f", wantExcess, fit.ExcessRMD)
		}
		// RequiredRate should be 0 since RMD more than covers the gap
		if fit.RequiredRate != 0 {
			t.Errorf("RequiredRate should be 0 when RMD covers gap, got %.4f", fit.RequiredRate)
		}
	})

	t.Run("RMD excess when income already covers expenses", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 2000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 75
		s.TaxDeferredPercent = 70
		s.RothPercent = 10
		s.SpendingPhaseConfig = nil
		s.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Social Security", Amount: 3000, StartMonth: 0},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Income (3000) > Expenses (2000), no gap
		if fit.GapBeforeRMD >= 0 {
			t.Errorf("expected negative GapBeforeRMD (surplus), got %.2f", fit.GapBeforeRMD)
		}
		// All RMD is excess since income already covers expenses
		if fit.RMDCoverage != 0 {
			t.Errorf("expected RMDCoverage=0 when no gap, got %.2f", fit.RMDCoverage)
		}
		if fit.ExcessRMD <= 0 {
			t.Errorf("expected positive ExcessRMD, got %.2f", fit.ExcessRMD)
		}
		monthlyTaxesBeforeRMD := fit.GapBeforeRMD - fit.MonthlyExpenses + fit.MonthlyIncome
		netRMD := fit.MonthlyRMD - (fit.MonthlyTaxes - monthlyTaxesBeforeRMD)
		if math.Abs(fit.ExcessRMD-netRMD) > 0.01 {
			t.Errorf("ExcessRMD should equal net RMD when no gap: ExcessRMD=%.2f, netRMD=%.2f", fit.ExcessRMD, netRMD)
		}
	})
}

func TestCalculateBudgetFitIncomeStartMonth(t *testing.T) {
	t.Run("deferred income excluded from month-0 breakdown but affects steady state", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 4000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 62
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Social Security", Amount: 2500, StartMonth: 36}, // starts year 3
			{ID: "pn", Name: "Pension", Amount: 1500, StartMonth: 0},          // immediate
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Social Security starts at month 36 so GetAdjustedAmount(0) returns 0.
		// Income breakdown only includes sources with amt > 0 at month 0.
		// So only Pension should appear.
		if len(fit.IncomeBreakdown) != 1 {
			t.Fatalf("expected 1 income breakdown item, got %d", len(fit.IncomeBreakdown))
		}
		if fit.IncomeBreakdown[0].Name != "Pension" {
			t.Errorf("income breakdown[0]: want 'Pension', got %q", fit.IncomeBreakdown[0].Name)
		}
		// Monthly income at month 0 should only include Pension
		if math.Abs(fit.MonthlyIncome-1500) > 0.01 {
			t.Errorf("MonthlyIncome: want 1500, got %.2f", fit.MonthlyIncome)
		}

		// Steady state should be at month 36 (year 3) when SS kicks in
		if fit.SteadyStateMonth != 36 {
			t.Errorf("SteadyStateMonth: want 36, got %d", fit.SteadyStateMonth)
		}
		// At steady state, income should include both sources
		if fit.SteadyStateIncome < 3999 {
			t.Errorf("SteadyStateIncome: want ~4000, got %.2f", fit.SteadyStateIncome)
		}
	})

	t.Run("immediate income with StartMonth 0 has no note", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 500_000
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
			{ID: "pn", Name: "Pension", Amount: 2000, StartMonth: 0},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		if len(fit.IncomeBreakdown) != 1 {
			t.Fatalf("expected 1 income breakdown item, got %d", len(fit.IncomeBreakdown))
		}
		if fit.IncomeBreakdown[0].Note != "" {
			t.Errorf("immediate income should have empty note, got %q", fit.IncomeBreakdown[0].Note)
		}
	})
}

func TestFindSteadyStateMonthMultipleSources(t *testing.T) {
	t.Run("multiple income sources with different start months", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.ProjectionYears = 30

		endMonth := 120
		s.IncomeSources = []models.IncomeSource{
			{ID: "i1", Name: "Pension", Amount: 1000, StartMonth: 0},
			{ID: "i2", Name: "Social Security", Amount: 2000, StartMonth: 36},
			{ID: "i3", Name: "Annuity", Amount: 500, StartMonth: 60},
			{ID: "i4", Name: "Short-term", Amount: 200, StartMonth: 24, EndMonth: &endMonth},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Steady state should be at the max start month of active sources = 60 (Annuity)
		// Short-term (start=24, end=120) is still active at 24, so it counts
		// But Annuity starts at 60 which is the latest
		wantMonth := 60
		wantYear := float64(wantMonth) / 12 // 5.0
		if fit.SteadyStateMonth != wantMonth {
			t.Errorf("SteadyStateMonth: want %d, got %d", wantMonth, fit.SteadyStateMonth)
		}
		if math.Abs(fit.MinSteadyStateYear-wantYear) > 0.01 {
			t.Errorf("MinSteadyStateYear: want %.2f, got %.2f", wantYear, fit.MinSteadyStateYear)
		}
	})

	t.Run("source with EndMonth before StartMonth is ignored", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 3000
		s.ProjectionYears = 30

		// This source ends before it starts, so it should be excluded from steady state calc
		badEnd := 10
		s.IncomeSources = []models.IncomeSource{
			{ID: "i1", Name: "Pension", Amount: 1000, StartMonth: 24},
			{ID: "i2", Name: "Bad Source", Amount: 500, StartMonth: 48, EndMonth: &badEnd},
		}

		calc := NewCalculator(s)
		fit := calc.CalculateBudgetFit()

		// Bad Source has EndMonth (10) <= StartMonth (48), so it's ignored
		// Steady state should be at Pension's start = 24
		wantMonth := 24
		if fit.SteadyStateMonth != wantMonth {
			t.Errorf("SteadyStateMonth: want %d, got %d", wantMonth, fit.SteadyStateMonth)
		}
	})
}

func TestFindSteadyStateMonth_ProjectedSocialSecurity(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-04"
	s.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: "1961-04", Role: models.PersonRolePrimary},
	}
	s.ComputeAges()
	s.IncomeSources = nil
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000,
		FRA:        67,
		ClaimAge:   70,
	}

	calc := NewCalculator(s)
	fit := calc.CalculateBudgetFit()

	if fit.SteadyStateMonth != 60 {
		t.Fatalf("SteadyStateMonth: want 60, got %d", fit.SteadyStateMonth)
	}
	if math.Abs(fit.MinSteadyStateYear-5.0) > 0.01 {
		t.Fatalf("MinSteadyStateYear: want 5.0, got %.2f", fit.MinSteadyStateYear)
	}
}

func TestMeanEmptySlice(t *testing.T) {
	t.Run("empty slice returns zero", func(t *testing.T) {
		result := mean([]float64{})
		if result != 0 {
			t.Errorf("mean of empty slice: want 0, got %f", result)
		}
	})

	t.Run("single element", func(t *testing.T) {
		result := mean([]float64{42.5})
		if math.Abs(result-42.5) > 0.001 {
			t.Errorf("mean of [42.5]: want 42.5, got %f", result)
		}
	})

	t.Run("multiple elements", func(t *testing.T) {
		result := mean([]float64{10, 20, 30})
		if math.Abs(result-20) > 0.001 {
			t.Errorf("mean of [10,20,30]: want 20, got %f", result)
		}
	})
}

// F-065: rebaseLivingExpensesAtTransition must use net inflation
// (InflationRate - SpendingDeclineRate), not full inflation, when computing
// the value at a chain-scenario phase boundary. Otherwise the post-transition
// trajectory drifts upward by the decline-rate compounding error.
func TestSpendingPhaseTransition_F065_DeclineRateRespected(t *testing.T) {
	// Primary settings: 3% inflation, 1% decline → net 2%/yr
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 65
	primary.ProjectionYears = 30
	primary.MonthlyLivingExpenses = 10000
	primary.MonthlyHealthcare = 0
	primary.HealthcarePersons = nil
	primary.IncomeSources = nil
	primary.ExpenseSources = nil
	primary.InflationRate = 3.0
	primary.SpendingDeclineRate = 1.0
	primary.InvestmentReturn = 0.0
	primary.PortfolioValue = 10_000_000 // large enough to never deplete
	primary.TaxDeferredPercent = 0
	primary.RothPercent = 0
	// SpendingPhaseConfig is disabled (default), so non-phase path is used

	// Chain scenario at age 75 (year 10): same base expenses and inflation,
	// simulating a "transparent" chain that should not change the expense trajectory.
	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 10000
	linked.MonthlyHealthcare = 0
	linked.HealthcarePersons = nil
	linked.IncomeSources = nil
	linked.ExpenseSources = nil
	linked.InflationRate = 3.0
	linked.SpendingDeclineRate = 1.0
	linked.InvestmentReturn = 0.0
	linked.PortfolioValue = 10_000_000
	linked.TaxDeferredPercent = 0
	linked.RothPercent = 0

	chain := []ResolvedScenarioChainLink{
		{TransitionAge: 75, Settings: linked},
	}

	calc := NewCalculatorWithChain(primary, chain)
	result := calc.RunProjection()

	if result == nil || len(result.Months) < 122 {
		t.Fatalf("expected at least 122 months of projection, got %d", len(result.Months))
	}

	// Net inflation: 3% - 1% = 2%/yr.
	// At month 120 (first month of year 10), the chain transition fires.
	// The year-boundary rebase runs first (setting currentLivingExpenses),
	// then the m>0 block compounds it by one month at net rate.
	//
	// Correct (with fix): rebase uses net cumulative inflation (1.02)^(119/12)
	//   then m>0 applies one more month: result = 10000 × (1.02)^(120/12) = 10000 × (1.02)^10
	//   ≈ 12189.94
	//
	// Buggy (without fix): rebase uses full cumulative inflation (1.03)^(119/12)
	//   then m>0 applies net month: result ≈ 10000 × (1.03)^(119/12) × (1.02)^(1/12)
	//   ≈ 13428

	wantNetFactor := math.Pow(1.02, 10)     // (1.02)^10 ≈ 1.21899
	want := 10000 * wantNetFactor            // ≈ 12189.94
	got := result.Months[120].GeneralExpenses

	if math.Abs(got-want) > 5.00 { // ±$5 for rounding
		t.Errorf("month 120 (post-chain-transition) GeneralExpenses = %.2f; want %.2f (net inflation rebase, F-065)", got, want)
	}

	// Also verify month 119 (pre-transition) is at net inflation — baseline sanity
	// Month 119: 10000 × (1.02)^(119/12)
	wantPre := 10000 * math.Pow(1.02, 119.0/12.0)
	gotPre := result.Months[119].GeneralExpenses
	if math.Abs(gotPre-wantPre) > 2.00 {
		t.Errorf("month 119 (pre-chain-transition) GeneralExpenses = %.2f; want %.2f", gotPre, wantPre)
	}
}
