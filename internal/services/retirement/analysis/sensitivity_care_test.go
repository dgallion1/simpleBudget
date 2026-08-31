package analysis

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// CC1 fix 2: the "Higher Healthcare" sensitivity scenario must scale
// CareMonthlyCost along with CurrentMonthlyCost/MedicareMonthlyCost/
// ACACostAfterEmployer, not silently exclude care from the stress test.

// careSensitivitySettings returns settings with one HealthcarePerson whose
// care cost is active from month 0 (CareStartAge == CurrentAge, year-based
// fallback since HealthcarePersons[0].BirthMonth is left unset), so care
// dollars show up in every projected month within the short horizon used
// here.
func careSensitivitySettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 70)
	s.CurrentAge = 70
	s.PortfolioValue = 3_000_000
	s.MonthlyLivingExpenses = 3_000
	s.MonthlyHealthcare = 0
	s.HealthcareStartYears = 0
	s.ProjectionYears = 5
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
			CareStartAge:          70,
			CareMonthlyCost:       5_000,
		},
	}
	return s
}

// TestSensitivityHigherHealthcare_ScalesCare verifies the "Higher
// Healthcare" scenario's FinalBalance reflects a CareMonthlyCost scaled by
// 1.5x, exactly as CurrentMonthlyCost/MedicareMonthlyCost/
// ACACostAfterEmployer already are. It cross-checks the scenario's
// FinalBalance against a projection run on settings scaled by hand, and
// separately proves the scaling is load-bearing (an otherwise-identical
// projection that leaves CareMonthlyCost unscaled must differ), so the
// test cannot pass vacuously.
func TestSensitivityHigherHealthcare_ScalesCare(t *testing.T) {
	s := careSensitivitySettings()
	proj, in := runProj(t, s)
	eng := engine.New()

	results := SensitivityWithBaseline(eng, in, proj, BudgetFit(in, proj))
	var higherHC *models.SensitivityResult
	for i := range results {
		if results[i].Scenario.Name == "Higher Healthcare" {
			higherHC = &results[i]
		}
	}
	if higherHC == nil {
		t.Fatal("Higher Healthcare scenario missing from results")
	}

	// Hand-build the fully scaled settings (mirroring sensitivity.go's
	// monthly_healthcare case) and confirm the scenario's FinalBalance
	// matches a direct projection against them.
	fullyScaled := *s
	fullyScaled.HealthcarePersons = append([]models.HealthcarePerson{}, s.HealthcarePersons...)
	for j := range fullyScaled.HealthcarePersons {
		fullyScaled.HealthcarePersons[j].CurrentMonthlyCost *= 1.5
		fullyScaled.HealthcarePersons[j].MedicareMonthlyCost *= 1.5
		fullyScaled.HealthcarePersons[j].ACACostAfterEmployer *= 1.5
		fullyScaled.HealthcarePersons[j].CareMonthlyCost *= 1.5
	}
	wantProj, _ := runProj(t, &fullyScaled)

	if diff := higherHC.FinalBalance - wantProj.FinalBalance; diff > 1 || diff < -1 {
		t.Fatalf("Higher Healthcare FinalBalance=%f, want %f (settings with CareMonthlyCost scaled 1.5x)", higherHC.FinalBalance, wantProj.FinalBalance)
	}

	// Prove the CareMonthlyCost scaling is load-bearing: a projection that
	// scales everything except CareMonthlyCost must land on a different
	// FinalBalance, or this test would pass even with fix 2 reverted.
	careUnscaled := *s
	careUnscaled.HealthcarePersons = append([]models.HealthcarePerson{}, s.HealthcarePersons...)
	for j := range careUnscaled.HealthcarePersons {
		careUnscaled.HealthcarePersons[j].CurrentMonthlyCost *= 1.5
		careUnscaled.HealthcarePersons[j].MedicareMonthlyCost *= 1.5
		careUnscaled.HealthcarePersons[j].ACACostAfterEmployer *= 1.5
		// CareMonthlyCost deliberately left unscaled.
	}
	unscaledProj, _ := runProj(t, &careUnscaled)
	if wantProj.FinalBalance == unscaledProj.FinalBalance {
		t.Fatal("test setup invalid: scaling CareMonthlyCost had no effect on FinalBalance")
	}
}
