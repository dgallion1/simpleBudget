package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// monthlyRateFromPercent mirrors the unexported engine helper used to
// convert an annual percent rate to its monthly compounding factor minus
// one. Inlined here so the analysis test file has no retirement-package
// dependency.
func monthlyRateFromPercent(annualRatePercent float64) float64 {
	if annualRatePercent == 0 {
		return 0
	}
	return math.Pow(1+annualRatePercent/100, 1.0/12.0) - 1
}

func TestPresentValueAnnuity(t *testing.T) {
	t.Run("zero payments returns zero", func(t *testing.T) {
		got := engine.PresentValueAnnuity(1000, 5.0, 0, 0, 0)
		if got != 0 {
			t.Errorf("expected 0, got %f", got)
		}
	})

	t.Run("zero payment amount returns zero", func(t *testing.T) {
		got := engine.PresentValueAnnuity(0, 5.0, 0, 0, 12)
		if got != 0 {
			t.Errorf("expected 0, got %f", got)
		}
	})

	t.Run("no discount rate without growth", func(t *testing.T) {
		// Simple sum: 1000 * 12 = 12000
		got := engine.PresentValueAnnuity(1000, 0, 0, 0, 12)
		if math.Abs(got-12000) > 0.01 {
			t.Errorf("expected 12000, got %f", got)
		}
	})

	t.Run("no discount rate with growth", func(t *testing.T) {
		monthlyGrowth := monthlyRateFromPercent(6.0)
		expected := 0.0
		for m := 0; m < 12; m++ {
			expected += 1000 * math.Pow(1+monthlyGrowth, float64(m))
		}
		got := engine.PresentValueAnnuity(1000, 0, 6.0, 0, 12)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("no discount rate with negative growth", func(t *testing.T) {
		monthlyGrowth := monthlyRateFromPercent(-3.0)
		expected := 0.0
		for m := 0; m < 24; m++ {
			expected += 1000 * math.Pow(1+monthlyGrowth, float64(m))
		}
		got := engine.PresentValueAnnuity(1000, 0, -3.0, 0, 24)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
		if got >= 24000 {
			t.Errorf("expected declining payments below flat total, got %.2f", got)
		}
	})

	t.Run("growth equals discount rate", func(t *testing.T) {
		// When g == r, PV = payment * numPayments
		got := engine.PresentValueAnnuity(1000, 5.0, 5.0, 0, 24)
		if math.Abs(got-24000) > 0.01 {
			t.Errorf("expected 24000, got %f", got)
		}
	})

	t.Run("growing annuity", func(t *testing.T) {
		// discount=6%, growth=3%, 120 payments
		dr := 6.0
		gr := 3.0
		n := 120
		mr := monthlyRateFromPercent(dr)
		mg := monthlyRateFromPercent(gr)
		gf := (1 + mg) / (1 + mr)
		expected := 1000 * (1 - math.Pow(gf, float64(n))) / (mr - mg)

		got := engine.PresentValueAnnuity(1000, dr, gr, 0, n)
		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("regular annuity no growth", func(t *testing.T) {
		// discount=6%, no growth, 120 payments
		dr := 6.0
		n := 120
		mr := monthlyRateFromPercent(dr)
		expected := 1000 * (1 - math.Pow(1+mr, -float64(n))) / mr

		got := engine.PresentValueAnnuity(1000, dr, 0, 0, n)
		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("future start discounts back", func(t *testing.T) {
		// Get PV at start=0, then verify start=12 discounts it back 12 months
		pvAtStart := engine.PresentValueAnnuity(1000, 6.0, 0, 0, 120)
		pvFutureStart := engine.PresentValueAnnuity(1000, 6.0, 0, 12, 120)

		mr := monthlyRateFromPercent(6.0)
		expectedFuture := pvAtStart / math.Pow(1+mr, 12)
		if math.Abs(pvFutureStart-expectedFuture)/expectedFuture > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expectedFuture, pvFutureStart)
		}
	})

	t.Run("future start with zero discount rate does not discount", func(t *testing.T) {
		// With zero discount, future start shouldn't change the result
		pvNow := engine.PresentValueAnnuity(1000, 0, 0, 0, 12)
		pvLater := engine.PresentValueAnnuity(1000, 0, 0, 6, 12)
		if pvNow != pvLater {
			t.Errorf("expected same PV, got %f vs %f", pvNow, pvLater)
		}
	})
}

func TestCalculateHealthcarePV(t *testing.T) {
	t.Run("person already on Medicare", func(t *testing.T) {
		person := models.HealthcarePerson{
			Name:                  "Retiree",
			CurrentAge:            67,
			CurrentCoverage:       models.CoverageMedicare,
			CurrentMonthlyCost:    459,
			MedicareMonthlyCost:   459,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}

		got := engine.HealthcarePVForCalculator(person, 5.0, 360)
		// Should equal PVAnnuity with post-Medicare inflation for full period
		expected := engine.PresentValueAnnuity(459, 5.0, 4.0, 0, 360)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("pre-Medicare person entire projection before Medicare", func(t *testing.T) {
		person := models.HealthcarePerson{
			Name:                  "Young",
			CurrentAge:            40,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1100,
			MedicareMonthlyCost:   459,
			PreMedicareInflation:  7.0,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}

		// 10-year projection, person is 40, Medicare at 65 -> 25 years away
		// Entire projection is pre-Medicare
		totalMonths := 120
		got := engine.HealthcarePVForCalculator(person, 5.0, totalMonths)
		expected := engine.PresentValueAnnuity(1100, 5.0, 7.0, 0, totalMonths)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("pre-Medicare person transitions to Medicare during projection", func(t *testing.T) {
		person := models.HealthcarePerson{
			Name:                  "PreRetiree",
			CurrentAge:            60,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1100,
			MedicareMonthlyCost:   459,
			PreMedicareInflation:  7.0,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}

		totalMonths := 360          // 30 years
		preMedicareMonths := 5 * 12 // 60 months until Medicare

		got := engine.HealthcarePVForCalculator(person, 5.0, totalMonths)

		// Phase 1: pre-Medicare
		phase1 := engine.PresentValueAnnuity(1100, 5.0, 7.0, 0, preMedicareMonths)
		// Phase 2: post-Medicare
		postMonths := totalMonths - preMedicareMonths
		phase2 := engine.PresentValueAnnuity(459, 5.0, 4.0, preMedicareMonths, postMonths)
		expected := phase1 + phase2

		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("person exactly at Medicare age", func(t *testing.T) {
		person := models.HealthcarePerson{
			Name:                  "JustTurned65",
			CurrentAge:            65,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1100,
			MedicareMonthlyCost:   459,
			PreMedicareInflation:  7.0,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}

		// IsOnMedicare() returns true when age >= MedicareEligibleAge
		totalMonths := 240
		got := engine.HealthcarePVForCalculator(person, 5.0, totalMonths)
		expected := engine.PresentValueAnnuity(1100, 5.0, 4.0, 0, totalMonths)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})
}

func TestCalculatePresentValueAnalysis(t *testing.T) {
	t.Run("basic expenses only", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1_000_000
		settings.MonthlyLivingExpenses = 4000
		settings.MonthlyHealthcare = 0
		settings.HealthcarePersons = nil
		settings.IncomeSources = []models.IncomeSource{}
		settings.ExpenseSources = []models.ExpenseSource{}
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 1.0
		settings.ProjectionYears = 30

		result := PresentValue(engineInput(t, settings))

		// PV of expenses should be positive
		if result.PVExpenses <= 0 {
			t.Errorf("expected positive PVExpenses, got %f", result.PVExpenses)
		}

		// No income
		if result.PVIncome != 0 {
			t.Errorf("expected 0 PVIncome, got %f", result.PVIncome)
		}

		// Gap = expenses - income = expenses
		if math.Abs(result.PVGap-result.PVExpenses) > 0.01 {
			t.Errorf("expected PVGap == PVExpenses, got gap=%f expenses=%f", result.PVGap, result.PVExpenses)
		}

		// CoverageRatio = (portfolio + income) / expenses
		expectedRatio := (1_000_000 + 0) / result.PVExpenses
		if math.Abs(result.CoverageRatio-expectedRatio) > 0.001 {
			t.Errorf("expected coverage ratio %.4f, got %.4f", expectedRatio, result.CoverageRatio)
		}

		// SurplusDeficit = portfolio + income - expenses
		expectedSD := 1_000_000 - result.PVExpenses
		if math.Abs(result.SurplusDeficit-expectedSD) > 0.01 {
			t.Errorf("expected surplus/deficit %.2f, got %.2f", expectedSD, result.SurplusDeficit)
		}

		// Verify PV of living expenses matches direct calculation
		netInflation := 3.0 - 1.0
		expectedPV := engine.PresentValueAnnuity(4000, 5.0, netInflation, 0, 360)
		if math.Abs(result.PVExpenses-expectedPV)/expectedPV > 0.001 {
			t.Errorf("expected PVExpenses %.2f, got %.2f", expectedPV, result.PVExpenses)
		}
	})

	t.Run("with income sources", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 500_000
		settings.MonthlyLivingExpenses = 3000
		settings.MonthlyHealthcare = 0
		settings.HealthcarePersons = nil
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 0
		settings.ProjectionYears = 30

		endMonth := 360
		settings.IncomeSources = []models.IncomeSource{
			{
				Name:       "Social Security",
				Amount:     2000,
				StartMonth: 0,
				EndMonth:   &endMonth,
				COLARate:   0.02, // 2% as decimal
			},
		}
		settings.ExpenseSources = []models.ExpenseSource{}

		result := PresentValue(engineInput(t, settings))

		if result.PVIncome <= 0 {
			t.Errorf("expected positive PVIncome, got %f", result.PVIncome)
		}

		// COLA rate passed as COLARate*100 = 2.0
		expectedIncome := engine.PresentValueAnnuity(2000, 5.0, 2.0, 0, 360)
		if math.Abs(result.PVIncome-expectedIncome)/expectedIncome > 0.001 {
			t.Errorf("expected PVIncome %.2f, got %.2f", expectedIncome, result.PVIncome)
		}

		// Surplus should account for portfolio + income - expenses
		expectedSurplus := 500_000 + result.PVIncome - result.PVExpenses
		if math.Abs(result.SurplusDeficit-expectedSurplus) > 0.01 {
			t.Errorf("expected surplus %.2f, got %.2f", expectedSurplus, result.SurplusDeficit)
		}
	})

	t.Run("with healthcare persons", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1_000_000
		settings.MonthlyLivingExpenses = 3000
		settings.MonthlyHealthcare = 0 // Legacy field unused when persons present
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 0
		settings.ProjectionYears = 30
		settings.IncomeSources = []models.IncomeSource{}
		settings.ExpenseSources = []models.ExpenseSource{}

		settings.HealthcarePersons = []models.HealthcarePerson{
			{
				Name:                  "Primary",
				CurrentAge:            60,
				CurrentCoverage:       models.CoverageACA,
				CurrentMonthlyCost:    1100,
				MedicareMonthlyCost:   459,
				PreMedicareInflation:  7.0,
				PostMedicareInflation: 4.0,
				MedicareEligibleAge:   65,
			},
		}

		result := PresentValue(engineInput(t, settings))

		// Expenses should include both living + healthcare
		livingPV := engine.PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		healthcarePV := engine.HealthcarePVForCalculator(settings.HealthcarePersons[0], 5.0, 360)
		expectedExpenses := livingPV + healthcarePV

		if math.Abs(result.PVExpenses-expectedExpenses)/expectedExpenses > 0.001 {
			t.Errorf("expected PVExpenses %.2f, got %.2f", expectedExpenses, result.PVExpenses)
		}
	})

	t.Run("with legacy healthcare", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1_000_000
		settings.MonthlyLivingExpenses = 3000
		settings.MonthlyHealthcare = 500
		settings.HealthcareInflation = 6.0
		settings.HealthcareStartYears = 2
		settings.HealthcarePersons = nil
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 0
		settings.ProjectionYears = 30
		settings.IncomeSources = []models.IncomeSource{}
		settings.ExpenseSources = []models.ExpenseSource{}

		result := PresentValue(engineInput(t, settings))

		livingPV := engine.PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		healthcarePV := engine.PresentValueAnnuity(500, 5.0, 6.0, 24, 336)
		expectedExpenses := livingPV + healthcarePV

		if math.Abs(result.PVExpenses-expectedExpenses)/expectedExpenses > 0.001 {
			t.Errorf("expected PVExpenses %.2f, got %.2f", expectedExpenses, result.PVExpenses)
		}
	})

	t.Run("with expense sources", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 1_000_000
		settings.MonthlyLivingExpenses = 3000
		settings.MonthlyHealthcare = 0
		settings.HealthcarePersons = nil
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 0
		settings.ProjectionYears = 30
		settings.IncomeSources = []models.IncomeSource{}

		settings.ExpenseSources = []models.ExpenseSource{
			{
				Name:      "Property Tax",
				Amount:    500,
				StartYear: 0,
				EndYear:   0, // perpetual
				Inflation: true,
			},
			{
				Name:      "Car Payment",
				Amount:    400,
				StartYear: 0,
				EndYear:   5,
				Inflation: false,
			},
		}

		result := PresentValue(engineInput(t, settings))

		livingPV := engine.PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		propTaxPV := engine.PresentValueAnnuity(500, 5.0, 3.0, 0, 360) // inflation-adjusted, perpetual
		carPV := engine.PresentValueAnnuity(400, 5.0, 0, 0, 60)        // no inflation, 5 years
		expectedExpenses := livingPV + propTaxPV + carPV

		if math.Abs(result.PVExpenses-expectedExpenses)/expectedExpenses > 0.001 {
			t.Errorf("expected PVExpenses %.2f, got %.2f", expectedExpenses, result.PVExpenses)
		}
	})

	t.Run("income with no end month runs to projection end", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.PortfolioValue = 500_000
		settings.MonthlyLivingExpenses = 2000
		settings.MonthlyHealthcare = 0
		settings.HealthcarePersons = nil
		settings.DiscountRate = 5.0
		settings.InflationRate = 3.0
		settings.SpendingDeclineRate = 0
		settings.ProjectionYears = 20
		settings.ExpenseSources = []models.ExpenseSource{}

		settings.IncomeSources = []models.IncomeSource{
			{
				Name:       "Pension",
				Amount:     1500,
				StartMonth: 24,
				EndMonth:   nil, // perpetual -> runs to projection end
				COLARate:   0.03,
			},
		}

		result := PresentValue(engineInput(t, settings))

		// Duration = 240 - 24 = 216 months
		expectedIncome := engine.PresentValueAnnuity(1500, 5.0, 3.0, 24, 216)
		if math.Abs(result.PVIncome-expectedIncome)/expectedIncome > 0.001 {
			t.Errorf("expected PVIncome %.2f, got %.2f", expectedIncome, result.PVIncome)
		}
	})
}
