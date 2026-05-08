package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestPresentValue(t *testing.T) {
	t.Run("zero periods returns future value unchanged", func(t *testing.T) {
		got := PresentValue(10000, 5.0, 0)
		if got != 10000 {
			t.Errorf("expected 10000, got %f", got)
		}
	})

	t.Run("negative periods returns future value unchanged", func(t *testing.T) {
		got := PresentValue(10000, 5.0, -5)
		if got != 10000 {
			t.Errorf("expected 10000, got %f", got)
		}
	})

	t.Run("zero rate returns future value unchanged", func(t *testing.T) {
		got := PresentValue(10000, 0, 12)
		if got != 10000 {
			t.Errorf("expected 10000, got %f", got)
		}
	})

	t.Run("negative rate returns future value unchanged", func(t *testing.T) {
		got := PresentValue(10000, -2.0, 12)
		if got != 10000 {
			t.Errorf("expected 10000, got %f", got)
		}
	})

	t.Run("normal discounting", func(t *testing.T) {
		got := PresentValue(10000, 6.0, 12)
		expected := 10000 / 1.06
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("multi-year discounting", func(t *testing.T) {
		// 5% annual rate, 60 months (5 years)
		got := PresentValue(50000, 5.0, 60)
		monthlyRate := monthlyCompoundFactorFromPercent(5.0) - 1
		expected := 50000 / math.Pow(1+monthlyRate, 60)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})
}

func TestPresentValueAnnuity(t *testing.T) {
	t.Run("zero payments returns zero", func(t *testing.T) {
		got := PresentValueAnnuity(1000, 5.0, 0, 0, 0)
		if got != 0 {
			t.Errorf("expected 0, got %f", got)
		}
	})

	t.Run("zero payment amount returns zero", func(t *testing.T) {
		got := PresentValueAnnuity(0, 5.0, 0, 0, 12)
		if got != 0 {
			t.Errorf("expected 0, got %f", got)
		}
	})

	t.Run("no discount rate without growth", func(t *testing.T) {
		// Simple sum: 1000 * 12 = 12000
		got := PresentValueAnnuity(1000, 0, 0, 0, 12)
		if math.Abs(got-12000) > 0.01 {
			t.Errorf("expected 12000, got %f", got)
		}
	})

	t.Run("no discount rate with growth", func(t *testing.T) {
		monthlyGrowth := monthlyCompoundFactorFromPercent(6.0) - 1
		expected := 0.0
		for m := 0; m < 12; m++ {
			expected += 1000 * math.Pow(1+monthlyGrowth, float64(m))
		}
		got := PresentValueAnnuity(1000, 0, 6.0, 0, 12)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("no discount rate with negative growth", func(t *testing.T) {
		monthlyGrowth := monthlyCompoundFactorFromPercent(-3.0) - 1
		expected := 0.0
		for m := 0; m < 24; m++ {
			expected += 1000 * math.Pow(1+monthlyGrowth, float64(m))
		}
		got := PresentValueAnnuity(1000, 0, -3.0, 0, 24)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
		if got >= 24000 {
			t.Errorf("expected declining payments below flat total, got %.2f", got)
		}
	})

	t.Run("growth equals discount rate", func(t *testing.T) {
		// When g == r, PV = payment * numPayments
		got := PresentValueAnnuity(1000, 5.0, 5.0, 0, 24)
		if math.Abs(got-24000) > 0.01 {
			t.Errorf("expected 24000, got %f", got)
		}
	})

	t.Run("growing annuity", func(t *testing.T) {
		// discount=6%, growth=3%, 120 payments
		dr := 6.0
		gr := 3.0
		n := 120
		mr := monthlyCompoundFactorFromPercent(dr) - 1
		mg := monthlyCompoundFactorFromPercent(gr) - 1
		gf := (1 + mg) / (1 + mr)
		expected := 1000 * (1 - math.Pow(gf, float64(n))) / (mr - mg)

		got := PresentValueAnnuity(1000, dr, gr, 0, n)
		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("regular annuity no growth", func(t *testing.T) {
		// discount=6%, no growth, 120 payments
		dr := 6.0
		n := 120
		mr := monthlyCompoundFactorFromPercent(dr) - 1
		expected := 1000 * (1 - math.Pow(1+mr, -float64(n))) / mr

		got := PresentValueAnnuity(1000, dr, 0, 0, n)
		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("future start discounts back", func(t *testing.T) {
		// Get PV at start=0, then verify start=12 discounts it back 12 months
		pvAtStart := PresentValueAnnuity(1000, 6.0, 0, 0, 120)
		pvFutureStart := PresentValueAnnuity(1000, 6.0, 0, 12, 120)

		mr := monthlyCompoundFactorFromPercent(6.0) - 1
		expectedFuture := pvAtStart / math.Pow(1+mr, 12)
		if math.Abs(pvFutureStart-expectedFuture)/expectedFuture > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expectedFuture, pvFutureStart)
		}
	})

	t.Run("future start with zero discount rate does not discount", func(t *testing.T) {
		// With zero discount, future start shouldn't change the result
		pvNow := PresentValueAnnuity(1000, 0, 0, 0, 12)
		pvLater := PresentValueAnnuity(1000, 0, 0, 6, 12)
		if pvNow != pvLater {
			t.Errorf("expected same PV, got %f vs %f", pvNow, pvLater)
		}
	})
}

func TestCalculateHealthcarePV(t *testing.T) {
	t.Run("person already on Medicare", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		settings.DiscountRate = 5.0
		calc := newTestCalc(t, settings)

		person := models.HealthcarePerson{
			Name:                  "Retiree",
			CurrentAge:            67,
			CurrentCoverage:       models.CoverageMedicare,
			CurrentMonthlyCost:    459,
			MedicareMonthlyCost:   459,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}

		got := calc.calculateHealthcarePV(person, 5.0, 360)
		// Should equal PVAnnuity with post-Medicare inflation for full period
		expected := PresentValueAnnuity(459, 5.0, 4.0, 0, 360)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("pre-Medicare person entire projection before Medicare", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		calc := newTestCalc(t, settings)

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
		got := calc.calculateHealthcarePV(person, 5.0, totalMonths)
		expected := PresentValueAnnuity(1100, 5.0, 7.0, 0, totalMonths)
		if math.Abs(got-expected) > 0.01 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("pre-Medicare person transitions to Medicare during projection", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		calc := newTestCalc(t, settings)

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

		got := calc.calculateHealthcarePV(person, 5.0, totalMonths)

		// Phase 1: pre-Medicare
		phase1 := PresentValueAnnuity(1100, 5.0, 7.0, 0, preMedicareMonths)
		// Phase 2: post-Medicare
		postMonths := totalMonths - preMedicareMonths
		phase2 := PresentValueAnnuity(459, 5.0, 4.0, preMedicareMonths, postMonths)
		expected := phase1 + phase2

		if math.Abs(got-expected)/expected > 0.001 {
			t.Errorf("expected %.2f, got %.2f", expected, got)
		}
	})

	t.Run("person exactly at Medicare age", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		calc := newTestCalc(t, settings)

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
		got := calc.calculateHealthcarePV(person, 5.0, totalMonths)
		expected := PresentValueAnnuity(1100, 5.0, 4.0, 0, totalMonths)
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

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
		expectedPV := PresentValueAnnuity(4000, 5.0, netInflation, 0, 360)
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

		if result.PVIncome <= 0 {
			t.Errorf("expected positive PVIncome, got %f", result.PVIncome)
		}

		// COLA rate passed as COLARate*100 = 2.0
		expectedIncome := PresentValueAnnuity(2000, 5.0, 2.0, 0, 360)
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

		// Expenses should include both living + healthcare
		livingPV := PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		healthcarePV := calc.calculateHealthcarePV(settings.HealthcarePersons[0], 5.0, 360)
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

		livingPV := PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		healthcarePV := PresentValueAnnuity(500, 5.0, 6.0, 24, 336)
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

		livingPV := PresentValueAnnuity(3000, 5.0, 3.0, 0, 360)
		propTaxPV := PresentValueAnnuity(500, 5.0, 3.0, 0, 360) // inflation-adjusted, perpetual
		carPV := PresentValueAnnuity(400, 5.0, 0, 0, 60)        // no inflation, 5 years
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

		calc := newTestCalc(t, settings)
		result := calc.CalculatePresentValueAnalysis()

		// Duration = 240 - 24 = 216 months
		expectedIncome := PresentValueAnnuity(1500, 5.0, 3.0, 24, 216)
		if math.Abs(result.PVIncome-expectedIncome)/expectedIncome > 0.001 {
			t.Errorf("expected PVIncome %.2f, got %.2f", expectedIncome, result.PVIncome)
		}
	})
}
