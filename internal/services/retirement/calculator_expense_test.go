package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
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

		calc := newTestCalc(t, s)

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

		calc := newTestCalc(t, s)

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

		calc := newTestCalc(t, s)

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

		calc := newTestCalc(t, s)

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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

	t.Run("withdrawal mix sums to gap (proportional to allocation)", func(t *testing.T) {
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
		s.TaxDeferredPercent = 60
		s.RothPercent = 10
		// Taxable = 30
		s.SpendingPhaseConfig = nil

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}

		// Net per bucket matches allocation share.
		if math.Abs(fit.NetWithdrawalTaxDeferred-fit.MonthlyGap*0.6) > 0.01 {
			t.Errorf("NetWithdrawalTaxDeferred: want %.2f (60%% of gap), got %.2f",
				fit.MonthlyGap*0.6, fit.NetWithdrawalTaxDeferred)
		}
		if math.Abs(fit.NetWithdrawalTaxable-fit.MonthlyGap*0.3) > 0.01 {
			t.Errorf("NetWithdrawalTaxable: want %.2f (30%% of gap), got %.2f",
				fit.MonthlyGap*0.3, fit.NetWithdrawalTaxable)
		}
		if math.Abs(fit.NetWithdrawalRoth-fit.MonthlyGap*0.1) > 0.01 {
			t.Errorf("NetWithdrawalRoth: want %.2f (10%% of gap), got %.2f",
				fit.MonthlyGap*0.1, fit.NetWithdrawalRoth)
		}
		// Net amounts sum to the gap — the property the UI reports as "Total (closes gap)".
		total := fit.NetWithdrawalTaxDeferred + fit.NetWithdrawalTaxable + fit.NetWithdrawalRoth
		if math.Abs(total-fit.MonthlyGap) > 0.01 {
			t.Errorf("net mix total: want %.2f (=gap), got %.2f", fit.MonthlyGap, total)
		}
	})

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

	t.Run("steady-state withdrawal mix sums to gap with tax overhead on TD/TX", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 12000
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
		// Mixed allocation so all three buckets get a share.
		s.TaxDeferredPercent = 50
		s.RothPercent = 25
		// Taxable = 25
		s.SpendingPhaseConfig = nil
		s.InvestmentReturn = 5
		s.SteadyStateOverrideYear = 15

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if !fit.HasSteadyState {
			t.Fatalf("precondition: HasSteadyState should be true")
		}
		if fit.SteadyStateGap <= 0 {
			t.Fatalf("precondition: expected positive steady-state gap, got %.2f", fit.SteadyStateGap)
		}

		// Net amounts follow allocation: 50% TD, 25% TX, 25% Roth.
		gap := fit.SteadyStateGap
		if math.Abs(fit.SteadyStateNetWithdrawalTaxDeferred-gap*0.5) > 0.01 {
			t.Errorf("SteadyStateNetWithdrawalTaxDeferred: want %.2f, got %.2f", gap*0.5, fit.SteadyStateNetWithdrawalTaxDeferred)
		}
		if math.Abs(fit.SteadyStateNetWithdrawalTaxable-gap*0.25) > 0.01 {
			t.Errorf("SteadyStateNetWithdrawalTaxable: want %.2f, got %.2f", gap*0.25, fit.SteadyStateNetWithdrawalTaxable)
		}
		if math.Abs(fit.SteadyStateNetWithdrawalRoth-gap*0.25) > 0.01 {
			t.Errorf("SteadyStateNetWithdrawalRoth: want %.2f, got %.2f", gap*0.25, fit.SteadyStateNetWithdrawalRoth)
		}
		// Net amounts must sum to the gap.
		total := fit.SteadyStateNetWithdrawalTaxDeferred + fit.SteadyStateNetWithdrawalTaxable + fit.SteadyStateNetWithdrawalRoth
		if math.Abs(total-gap) > 0.01 {
			t.Errorf("net mix total: want %.2f (=gap), got %.2f", gap, total)
		}

		// Roth: no tax → gross == net.
		if math.Abs(fit.SteadyStateGrossWithdrawalRoth-fit.SteadyStateNetWithdrawalRoth) > 0.01 {
			t.Errorf("SteadyStateGrossWithdrawalRoth: want %.2f (=net), got %.2f",
				fit.SteadyStateNetWithdrawalRoth, fit.SteadyStateGrossWithdrawalRoth)
		}
		// Tax-deferred: gross > net (income tax), marginal > 0.
		if fit.SteadyStateGrossWithdrawalTaxDeferred <= fit.SteadyStateNetWithdrawalTaxDeferred {
			t.Errorf("SteadyStateGrossWithdrawalTaxDeferred (%.2f) must exceed net (%.2f)",
				fit.SteadyStateGrossWithdrawalTaxDeferred, fit.SteadyStateNetWithdrawalTaxDeferred)
		}
		if fit.SteadyStateMarginalRateTaxDeferred <= 0 {
			t.Errorf("SteadyStateMarginalRateTaxDeferred: want > 0, got %.2f",
				fit.SteadyStateMarginalRateTaxDeferred)
		}
		// Taxable at steady state: gross > net (LTCG on accrued gains).
		if fit.SteadyStateGrossWithdrawalTaxable <= fit.SteadyStateNetWithdrawalTaxable {
			t.Errorf("SteadyStateGrossWithdrawalTaxable (%.2f) should exceed net (%.2f) at steady state",
				fit.SteadyStateGrossWithdrawalTaxable, fit.SteadyStateNetWithdrawalTaxable)
		}
		// Per-dollar-net, taxable should be cheaper than tax-deferred (LTCG vs. ordinary).
		txCostPerNet := (fit.SteadyStateGrossWithdrawalTaxable - fit.SteadyStateNetWithdrawalTaxable) / fit.SteadyStateNetWithdrawalTaxable
		tdCostPerNet := (fit.SteadyStateGrossWithdrawalTaxDeferred - fit.SteadyStateNetWithdrawalTaxDeferred) / fit.SteadyStateNetWithdrawalTaxDeferred
		if txCostPerNet >= tdCostPerNet {
			t.Errorf("taxable tax cost (%.2f%%) should be lower than tax-deferred (%.2f%%)", txCostPerNet*100, tdCostPerNet*100)
		}
		// At year 15 with 5% taxable return, LTCG ≈ 7-9% effective.
		if fit.SteadyStateEffectiveRateTaxable < 3 || fit.SteadyStateEffectiveRateTaxable > 15 {
			t.Errorf("SteadyStateEffectiveRateTaxable: want in [3, 15] for year 15 / 5%% scenario, got %.2f",
				fit.SteadyStateEffectiveRateTaxable)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		// 10000*12 / 100000 * 100 = 120%
		if fit.RequiredRate < 100 {
			t.Errorf("required rate should be very high (>100%%), got %.2f", fit.RequiredRate)
		}
	})

	// Edge-case follow-ons from
	// docs/superpowers/specs/2026-05-13-gross-withdrawal-edge-cases-followon.md:
	// the gross-up marginal rate under bracket/threshold crossings. All
	// scenarios pin InflationRate=0 so the bundled 2024 federal tables (and
	// 2026 IRMAA tiers) apply un-inflated regardless of the plan's StartDate.

	t.Run("federal bracket crossing yields blended 22/24 marginal rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_500_000
		s.MonthlyLivingExpenses = 11000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		// $9,000/mo pension → $108,000/yr ordinary; single taxable income
		// 108,000 − 14,600 = 93,400 — inside the 22% bracket with $7,125 of
		// headroom below the $100,525 ceiling. MAGI stays under the $109,000
		// IRMAA tier-1 threshold so no surcharge muddies the baseline.
		s.IncomeSources = []models.IncomeSource{
			{ID: "pension", Name: "Pension", Amount: 9000, StartMonth: 0},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 100
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.TaxConfig.FilingStatus = models.FilingSingle

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if fit.MonthlyIRMAA != 0 {
			t.Fatalf("precondition: baseline MAGI must sit under IRMAA tier 1, got surcharge %.2f", fit.MonthlyIRMAA)
		}
		// The annualized TD withdrawal (~$40k) burns the $7,125 of 22%
		// headroom and lands the rest in 24% → strictly blended marginal.
		if fit.MarginalRateTaxDeferred <= 22.2 || fit.MarginalRateTaxDeferred >= 23.9 {
			t.Errorf("MarginalRateTaxDeferred: want blended rate strictly inside (22.2, 23.9), got %.2f",
				fit.MarginalRateTaxDeferred)
		}
		wantGross := fit.NetWithdrawalTaxDeferred / (1 - fit.MarginalRateTaxDeferred/100)
		if math.Abs(fit.GrossWithdrawalTaxDeferred-wantGross) > 0.50 {
			t.Errorf("GrossWithdrawalTaxDeferred: want %.2f (=net/(1-rate)), got %.2f",
				wantGross, fit.GrossWithdrawalTaxDeferred)
		}
	})

	t.Run("IRMAA cliff crossed by withdrawal does not fire within same-year gross-up", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_500_000
		s.MonthlyLivingExpenses = 12000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		// $8,800/mo pension → MAGI $105,600, just under the single-filer
		// IRMAA tier-1 threshold ($109,000 in the bundled 2026 table). The
		// simulated withdrawal (~$53k/yr) pushes MAGI through tiers 1 AND 2.
		s.IncomeSources = []models.IncomeSource{
			{ID: "pension", Name: "Pension", Amount: 8800, StartMonth: 0},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 100
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.TaxConfig.FilingStatus = models.FilingSingle

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if fit.MonthlyIRMAA != 0 {
			t.Fatalf("precondition: baseline MAGI must sit under IRMAA tier 1, got surcharge %.2f", fit.MonthlyIRMAA)
		}
		// IRMAA keys off 2-year-lookback MAGI, which the gross-up pins to the
		// baseline in BOTH snapshots — so the tier crossings the withdrawal
		// causes for year N+2 must NOT leak into this year's marginal rate.
		// A fired tier-1 cliff alone would add ~2 points (95.70*12/annualNet);
		// the rate must stay inside the pure 22/24 income-tax blend.
		if fit.MarginalRateTaxDeferred <= 22.0 || fit.MarginalRateTaxDeferred >= 24.0 {
			t.Errorf("MarginalRateTaxDeferred: want pure income-tax blend in (22, 24) — IRMAA cliff must not fire — got %.2f",
				fit.MarginalRateTaxDeferred)
		}
	})

	t.Run("SS taxability phase-in raises marginal rate above ordinary bracket", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 800_000
		s.MonthlyLivingExpenses = 6000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		// $24k pension + $24k SS → provisional income $36k, past the single
		// upper threshold ($34k): baseline taxable SS is 4,500 + 0.85*2,000 =
		// $6,200 of the $24k benefit — squarely mid-phase-in. Baseline
		// taxable income $15,600 sits in the 12% bracket.
		s.IncomeSources = []models.IncomeSource{
			{ID: "pension", Name: "Pension", Amount: 2000, StartMonth: 0},
			{ID: "ss", Name: "Social Security", Amount: 2000, StartMonth: 0},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 100
		s.RothPercent = 0
		s.SpendingPhaseConfig = nil
		s.TaxConfig.FilingStatus = models.FilingSingle

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if fit.TaxableSocialSecurityPct <= 0 || fit.TaxableSocialSecurityPct >= 85 {
			t.Fatalf("precondition: baseline taxable-SS%% must be mid-phase-in (0, 85), got %.2f",
				fit.TaxableSocialSecurityPct)
		}
		// Each simulated withdrawal dollar drags up to $0.85 of additional SS
		// into taxable income until the 85%-of-benefits cap, so the marginal
		// rate must land far above the 12% ordinary bracket the baseline sits
		// in — even though the withdrawal itself never leaves 12/22.
		if fit.MarginalRateTaxDeferred <= 15 || fit.MarginalRateTaxDeferred >= 30 {
			t.Errorf("MarginalRateTaxDeferred: want SS-phase-in-amplified rate in (15, 30), got %.2f",
				fit.MarginalRateTaxDeferred)
		}
		wantGross := fit.NetWithdrawalTaxDeferred / (1 - fit.MarginalRateTaxDeferred/100)
		if math.Abs(fit.GrossWithdrawalTaxDeferred-wantGross) > 0.50 {
			t.Errorf("GrossWithdrawalTaxDeferred: want %.2f (=net/(1-rate)), got %.2f",
				wantGross, fit.GrossWithdrawalTaxDeferred)
		}
	})

	t.Run("NIIT threshold crossing surcharges the marginal rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 4_000_000
		s.MonthlyLivingExpenses = 16000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		// $150k pension + ~$40k qualified dividends from the $2M taxable
		// bucket (2% yield) → baseline MAGI ≈ $190k, under the $200k single
		// NIIT threshold. The simulated TD withdrawal crosses it, exposing
		// the dividends to the 3.8% surcharge on the excess.
		s.IncomeSources = []models.IncomeSource{
			{ID: "pension", Name: "Pension", Amount: 12500, StartMonth: 0},
		}
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		s.TaxDeferredPercent = 50
		s.RothPercent = 0
		// Taxable = 50% → $2M
		s.TaxableDividendYield = 2.0
		s.TaxableCapitalGainsDistributionRate = 0
		s.SpendingPhaseConfig = nil
		s.TaxConfig.FilingStatus = models.FilingSingle

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if fit.MonthlyGap <= 0 {
			t.Fatalf("precondition: expected positive gap, got %.2f", fit.MonthlyGap)
		}
		if fit.MonthlyNIIT != 0 {
			t.Fatalf("precondition: baseline MAGI must sit under the NIIT threshold, got NIIT %.2f/mo", fit.MonthlyNIIT)
		}
		// Ordinary income alone sits flat in the 24% bracket; the NIIT
		// phase-in on investment income must push the marginal rate
		// discontinuously above it.
		if fit.MarginalRateTaxDeferred <= 24.2 || fit.MarginalRateTaxDeferred >= 30 {
			t.Errorf("MarginalRateTaxDeferred: want NIIT-surcharged rate in (24.2, 30), got %.2f",
				fit.MarginalRateTaxDeferred)
		}
	})

	t.Run("steady-state LTCG 0/15 bracket crossing yields blended effective rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_300_000
		s.MonthlyLivingExpenses = 8000
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.IncomeSources = nil
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.CurrentAge = 65
		// All-taxable so the steady-state gap is closed entirely by taxable
		// sales whose simulated gain crosses the 0% LTCG ceiling ($47,025).
		s.TaxDeferredPercent = 0
		s.RothPercent = 0
		s.TaxableDividendYield = 1.0
		s.TaxableCapitalGainsDistributionRate = 0.5
		s.SpendingPhaseConfig = nil
		s.TaxConfig.FilingStatus = models.FilingSingle
		s.InvestmentReturn = 5
		s.SteadyStateOverrideYear = 15

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		if !fit.HasSteadyState {
			t.Fatalf("precondition: HasSteadyState should be true")
		}
		if fit.SteadyStateGap <= 0 {
			t.Fatalf("precondition: expected positive steady-state gap, got %.2f", fit.SteadyStateGap)
		}
		if fit.SteadyStateNIIT != 0 {
			t.Fatalf("precondition: NIIT must stay out of this scenario, got %.2f/mo", fit.SteadyStateNIIT)
		}
		// Year-15 gain fraction is 1 − 1.05⁻¹⁵ ≈ 0.519, so an all-0% LTCG
		// simulation prices the withdrawal at 0% effective and an all-15% one
		// at ≈7.8%. Baseline distributions leave only partial 0%-bracket
		// headroom, so the crossing must land strictly between.
		if fit.SteadyStateEffectiveRateTaxable <= 0.5 || fit.SteadyStateEffectiveRateTaxable >= 7.5 {
			t.Errorf("SteadyStateEffectiveRateTaxable: want blended 0/15 crossing rate in (0.5, 7.5), got %.2f",
				fit.SteadyStateEffectiveRateTaxable)
		}
		wantGross := fit.SteadyStateNetWithdrawalTaxable / (1 - fit.SteadyStateEffectiveRateTaxable/100)
		if math.Abs(fit.SteadyStateGrossWithdrawalTaxable-wantGross) > 0.50 {
			t.Errorf("SteadyStateGrossWithdrawalTaxable: want %.2f (=net/(1-rate)), got %.2f",
				wantGross, fit.SteadyStateGrossWithdrawalTaxable)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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
		s.CurrentAge = 75 // Above engine.RMDStartAge (73)
		// F-078: keep Persons[0].BirthMonth in sync with CurrentAge so
		// engine.RMDApplies/engine.RMDAgeForCalendarYear see a birth year that's actually
		// >= the SECURE 2.0 RMD age this calendar year.
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
		s.TaxDeferredPercent = 80
		s.RothPercent = 10
		s.SpendingPhaseConfig = nil
		// Use MFJ so the ~$16k annual RMD is fully absorbed by the MFJ standard
		// deduction (~$29k), making netRMD == monthlyRMD and keeping the
		// partial-coverage assertion simple.
		s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}

		calc := newTestCalc(t, s)
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
		// F-078: sync BirthMonth so engine.RMDApplies sees the right birth year.
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
		s.TaxDeferredPercent = 90
		s.RothPercent = 5
		s.SpendingPhaseConfig = nil

		calc := newTestCalc(t, s)
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
		// F-078: sync BirthMonth so engine.RMDApplies sees the right birth year.
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
		s.TaxDeferredPercent = 70
		s.RothPercent = 10
		s.SpendingPhaseConfig = nil
		s.IncomeSources = []models.IncomeSource{
			{ID: "ss", Name: "Social Security", Amount: 3000, StartMonth: 0},
		}

		calc := newTestCalc(t, s)
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
		s.SteadyStateOverrideYear = 3 // view at year 3 (when SS kicks in)

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
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

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		// Auto-detection: latest active source = Annuity at month 60 (year 5).
		// Short-term (start=24, end=120) is still active at 24, so it counts,
		// but Annuity at 60 is the latest.
		wantYear := 5.0
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

		calc := newTestCalc(t, s)
		fit := calc.CalculateBudgetFit()

		// Bad Source has EndMonth (10) <= StartMonth (48), so it's ignored.
		// Auto-detected min steady state should be Pension's start = year 2.
		if math.Abs(fit.MinSteadyStateYear-2.0) > 0.01 {
			t.Errorf("MinSteadyStateYear: want 2.0, got %.2f", fit.MinSteadyStateYear)
		}
	})
}

func TestFindSteadyStateMonth_ProjectedSocialSecurity(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-04"
	s.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: "1961-04", Role: models.PersonRolePrimary},
	}
	prepare.ComputeAges(s)
	s.IncomeSources = nil
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000,
		FRA:        67,
		ClaimAge:   70,
	}

	calc := newTestCalc(t, s)
	fit := calc.CalculateBudgetFit()

	// Auto-detection of the latest SS claim age should yield year 5 (month 60).
	if math.Abs(fit.MinSteadyStateYear-5.0) > 0.01 {
		t.Fatalf("MinSteadyStateYear: want 5.0, got %.2f", fit.MinSteadyStateYear)
	}
}

// F-065: engine.RebaseLivingExpensesAtTransition must use net inflation
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

	chain := []engine.PreparedChainLink{
		preparedLink(t, "", 75, linked),
	}

	calc := newTestCalcWithChain(t, primary, chain)
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

	wantNetFactor := math.Pow(1.02, 10) // (1.02)^10 ≈ 1.21899
	want := 10000 * wantNetFactor       // ≈ 12189.94
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
