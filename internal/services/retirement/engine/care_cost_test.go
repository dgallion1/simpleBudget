package engine

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// CC1: late-life care cost engine integration tests.

// careScenario returns a simple, deterministic settings object with one
// HealthcarePerson. CareStartAge 85 vs CurrentAge 70 puts the year-fallback
// care start at month 180 (15 years), comfortably inside the 20-year
// projection window used by these tests.
func careScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 70)
	s.CurrentAge = 70
	s.PortfolioValue = 3_000_000
	s.MonthlyLivingExpenses = 3_000
	s.MonthlyHealthcare = 0
	s.HealthcareStartYears = 0
	s.MonthlyPropertyTax = 200
	s.ProjectionYears = 20
	s.InflationRate = 3
	s.InvestmentReturn = 5
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			ID:                    "hp1",
			Name:                  "User",
			CurrentAge:            70,
			CurrentCoverage:       models.CoverageMedicare,
			CurrentMonthlyCost:    459,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
			CareStartAge:          85,
			CareMonthlyCost:       8000,
		},
	}
	return s
}

const careStartMonthForScenario = (85 - 70) * 12 // 180, year-fallback (no HealthcarePerson.BirthMonth)

func withoutCare(s *models.WhatIfSettings) *models.WhatIfSettings {
	clone := *s
	persons := make([]models.HealthcarePerson, len(s.HealthcarePersons))
	copy(persons, s.HealthcarePersons)
	for i := range persons {
		persons[i].CareStartAge = 0
		persons[i].CareMonthlyCost = 0
	}
	clone.HealthcarePersons = persons
	return &clone
}

// TestCareCost_TotalExpensesHigherAfterCareStart verifies that, using the
// full deterministic projection (engine.New().Run), TotalExpenses is
// identical between a settings object with care configured and an
// otherwise-identical one without care for months before care starts, and
// strictly higher after care starts.
func TestCareCost_TotalExpensesHigherAfterCareStart(t *testing.T) {
	withCare := careScenario()
	noCare := withoutCare(withCare)

	projWith := New().Run(Input{Prepared: prepare.MustFrom(t, withCare)})
	projNoCare := New().Run(Input{Prepared: prepare.MustFrom(t, noCare)})

	beforeMonth := careStartMonthForScenario - 1
	afterMonth := careStartMonthForScenario

	if beforeMonth < 0 || afterMonth >= len(projWith.Months) || afterMonth >= len(projNoCare.Months) {
		t.Fatalf("scenario months out of range: before=%d after=%d len(with)=%d len(noCare)=%d", beforeMonth, afterMonth, len(projWith.Months), len(projNoCare.Months))
	}

	wBefore := projWith.Months[beforeMonth].TotalExpenses
	nBefore := projNoCare.Months[beforeMonth].TotalExpenses
	if wBefore != nBefore {
		t.Fatalf("before care start (month %d): with-care TotalExpenses=%f, no-care=%f, want equal", beforeMonth, wBefore, nBefore)
	}

	wAfter := projWith.Months[afterMonth].TotalExpenses
	nAfter := projNoCare.Months[afterMonth].TotalExpenses
	if wAfter <= nAfter {
		t.Fatalf("after care start (month %d): with-care TotalExpenses=%f, no-care=%f, want with-care strictly higher", afterMonth, wAfter, nAfter)
	}
}

// TestCareCost_BreakdownPutsCareInEssential verifies CalculateExpenseBreakdown
// classifies the care-attributable expense as Essential, not Discretionary.
func TestCareCost_BreakdownPutsCareInEssential(t *testing.T) {
	withCare := careScenario()
	noCare := withoutCare(withCare)

	month := careStartMonthForScenario + 12

	bWith := CalculateExpenseBreakdown(withCare, month)
	bNoCare := CalculateExpenseBreakdown(noCare, month)

	careDelta := bWith.Essential - bNoCare.Essential
	if careDelta <= 0 {
		t.Fatalf("essential delta at month %d = %f, want positive (care cost)", month, careDelta)
	}
	if bWith.Discretionary != bNoCare.Discretionary {
		t.Fatalf("discretionary changed by enabling care: with=%f, without=%f, want equal", bWith.Discretionary, bNoCare.Discretionary)
	}

	// The essential delta should equal the care component exactly (living
	// expenses and property tax are identical between the two settings).
	wantDelta := withCare.HealthcarePersons[0].CareCostAt(month, withCare.StartDate)
	if diff := careDelta - wantDelta; diff > 0.01 || diff < -0.01 {
		t.Fatalf("essential delta=%f, want exactly the care cost %f", careDelta, wantDelta)
	}
}

// TestCareCost_NotScaledBySpendingPhaseMultiplier verifies that enabling a
// SpendingPhaseConfig with a 0.65 multiplier does not reduce the
// care-attributable expense delta: comparing (with-care minus no-care) with
// phases on vs off should be unchanged.
func TestCareCost_NotScaledBySpendingPhaseMultiplier(t *testing.T) {
	month := careStartMonthForScenario + 12

	phasesOff := careScenario()
	phasesOff.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	noCareOff := withoutCare(phasesOff)
	deltaOff := TotalExpenses(phasesOff, month) - TotalExpenses(noCareOff, month)

	phasesOn := careScenario()
	phasesOn.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Nursing", StartAge: 80, Multiplier: 0.65},
		},
	}
	noCareOn := withoutCare(phasesOn)
	deltaOn := TotalExpenses(phasesOn, month) - TotalExpenses(noCareOn, month)

	if diff := deltaOn - deltaOff; diff > 0.01 || diff < -0.01 {
		t.Fatalf("care-attributable delta changed with phases enabled: off=%f, on=%f, want equal", deltaOff, deltaOn)
	}
}

// TestCareCost_StepperAndBreakdownPathsAgree proves the stepper.go
// accumulation path (StepMonth's activeHealthcare, surfaced as
// ProjectionMonth.HealthcareExpense by the deterministic loop) and the
// expense.go accumulation path (CalculateExpenseBreakdown/TotalExpenses,
// consumed independently by the analysis package's budget-fit and Monte
// Carlo snapshots) agree on the healthcare dollar amount — including care —
// for the same month, at months straddling the care-start boundary. Both
// paths call WhatIfSettings.GetTotalHealthcareCost directly per month with
// no annual caching, so they must never diverge.
func TestCareCost_StepperAndBreakdownPathsAgree(t *testing.T) {
	s := careScenario()
	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})

	months := []int{
		careStartMonthForScenario - 1,
		careStartMonthForScenario,
		careStartMonthForScenario + 12,
	}

	for _, m := range months {
		if m < 0 || m >= len(proj.Months) {
			t.Fatalf("month %d out of projection range (len=%d)", m, len(proj.Months))
		}
		stepperHealthcare := proj.Months[m].HealthcareExpense

		breakdown := CalculateExpenseBreakdown(s, m)
		living := LivingExpensesAtMonth(s, m)
		propertyTax := PropertyTaxAtMonth(s, m)
		breakdownHealthcare := breakdown.Essential - living - propertyTax

		if diff := stepperHealthcare - breakdownHealthcare; diff > 0.01 || diff < -0.01 {
			t.Fatalf("month %d: stepper path healthcare=%f, breakdown path healthcare=%f, want agreement", m, stepperHealthcare, breakdownHealthcare)
		}

		direct := s.GetTotalHealthcareCost(m)
		if diff := stepperHealthcare - direct; diff > 0.01 || diff < -0.01 {
			t.Fatalf("month %d: stepper path healthcare=%f, GetTotalHealthcareCost=%f, want agreement", m, stepperHealthcare, direct)
		}
	}
}

// TestHealthcarePV_CareInHorizonIncreasesPV verifies HealthcarePV (used by
// analysis.PresentValue's Total-Needs/coverage-ratio panel) includes the
// discounted late-life care stream: a person with an in-horizon
// CareStartAge/CareMonthlyCost must produce a strictly higher PV than an
// otherwise-identical person with no care configured. Guards CC1 fix 1.
func TestHealthcarePV_CareInHorizonIncreasesPV(t *testing.T) {
	withCare := models.HealthcarePerson{
		CurrentAge:            70,
		CurrentCoverage:       models.CoverageMedicare,
		CurrentMonthlyCost:    459,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
	}
	noCare := withCare
	noCare.CareStartAge = 0
	noCare.CareMonthlyCost = 0

	totalMonths := 20 * 12 // 20-year horizon comfortably covers care start at (85-70)*12=180

	pvWith := HealthcarePV(withCare, 5.0, totalMonths)
	pvNoCare := HealthcarePV(noCare, 5.0, totalMonths)

	if pvWith <= pvNoCare {
		t.Fatalf("HealthcarePV with in-horizon care=%f, without care=%f, want with-care strictly higher", pvWith, pvNoCare)
	}
}

// TestHealthcarePV_CareBeyondHorizonNoChange verifies that when
// CareStartAge lies beyond the projection horizon, HealthcarePV is
// unaffected by the care fields (no care dollars ever fall inside
// totalMonths). Guards CC1 fix 1 against over-counting care that never
// occurs within the window.
func TestHealthcarePV_CareBeyondHorizonNoChange(t *testing.T) {
	totalMonths := 10 * 12 // 10-year horizon; care starts at (85-70)*12=180 (month 180), outside it

	withCare := models.HealthcarePerson{
		CurrentAge:            70,
		CurrentCoverage:       models.CoverageMedicare,
		CurrentMonthlyCost:    459,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
	}
	noCare := withCare
	noCare.CareStartAge = 0
	noCare.CareMonthlyCost = 0

	pvWith := HealthcarePV(withCare, 5.0, totalMonths)
	pvNoCare := HealthcarePV(noCare, 5.0, totalMonths)

	if diff := pvWith - pvNoCare; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("HealthcarePV with beyond-horizon care=%f, without care=%f, want identical", pvWith, pvNoCare)
	}
}
