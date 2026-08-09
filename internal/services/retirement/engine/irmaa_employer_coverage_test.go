package engine

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// employerCoverageScenario is a single filer who turns 65 at the start of the
// projection but keeps employer coverage for employerYears more years before
// moving to Medicare. Pension income is well above the IRMAA tier-1 threshold
// in every year, so IRMAA is charged whenever the engine believes the
// household is on Medicare.
func employerCoverageScenario(employerYears int) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{
		ID:         "primary",
		Name:       "You",
		BirthMonth: models.BirthMonthForAge("2026-01", 65),
		Role:       models.PersonRolePrimary,
	}}
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.ExpenseSources = nil
	s.ProjectionYears = 6
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.Guardrails = nil
	s.SocialSecurity = nil
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0

	// $250k/yr pension — comfortably over the single-filer IRMAA threshold.
	s.IncomeSources = []models.IncomeSource{
		{ID: "pension", Name: "Pension", Amount: 20_833, Type: models.IncomeFixed, StartMonth: 0},
	}

	s.HealthcarePersons = []models.HealthcarePerson{{
		ID:                    "hc-primary",
		Name:                  "You",
		PersonID:              "primary",
		BirthMonth:            models.BirthMonthForAge("2026-01", 65),
		CurrentAge:            65,
		CurrentCoverage:       models.CoverageEmployer,
		CurrentMonthlyCost:    400,
		MedicareMonthlyCost:   400,
		MedicareEligibleAge:   65,
		EmployerCoverageYears: employerYears,
	}}

	tc := models.DefaultTaxConfig()
	tc.FilingStatus = models.FilingSingle
	s.TaxConfig = tc
	return s
}

// TestNoIRMAAWhileOnEmployerCoverage is the falsification test for audit
// finding F-5. IRMAA is a surcharge on Medicare Part B and Part D premiums.
// Someone who stays on employer coverage past 65 is not paying those premiums,
// so there is nothing to surcharge.
//
// The expense model already knows this: GetMonthlyCostAt keeps the person on
// the employer premium until EmployerCoverageYears runs out, explicitly ahead
// of the age-based Medicare transition. But MedicareEligibleAdultCountAtYear
// only asks whether the person is 65, so the projection bills IRMAA from age
// 65 while simultaneously charging an employer premium — the two models
// contradict each other for exactly the household EmployerCoverageYears exists
// to describe.
func TestNoIRMAAWhileOnEmployerCoverage(t *testing.T) {
	const employerYears = 3 // covers ages 65, 66, 67

	proj := New().Run(Input{Prepared: prepare.MustFrom(t, employerCoverageScenario(employerYears))})
	if len(proj.YearlySummaries) != 6 {
		t.Fatalf("yearly summaries=%d, want 6", len(proj.YearlySummaries))
	}

	for y, ys := range proj.YearlySummaries {
		t.Logf("year %d: IRMAA=%.2f MAGI=%.2f", y, ys.IRMAA, ys.MAGI)
	}

	for y := 0; y < employerYears; y++ {
		if proj.YearlySummaries[y].IRMAA > 0 {
			t.Errorf("year %d: charged %.2f of IRMAA while still on employer coverage; "+
				"there is no Medicare premium to surcharge", y, proj.YearlySummaries[y].IRMAA)
		}
	}

	// Once employer coverage lapses the person is on Medicare and the
	// surcharge is correct — otherwise this test would pass by never charging
	// IRMAA at all.
	if proj.YearlySummaries[employerYears].IRMAA <= 0 {
		t.Errorf("year %d: employer coverage has ended and MAGI is above the threshold, "+
			"so IRMAA should be charged; got %.2f", employerYears, proj.YearlySummaries[employerYears].IRMAA)
	}
}

// TestIRMAAHonoursMedicareEligibleAge is the second half of F-5.
// MedicareEligibleAdultCountAtYear hardcoded 65 and ignored
// HealthcarePerson.MedicareEligibleAge, so a plan that models a later
// eligibility age was surcharged from 65 regardless of what it said.
func TestIRMAAHonoursMedicareEligibleAge(t *testing.T) {
	const eligibleAge = 67

	s := employerCoverageScenario(0) // 0 = employer coverage until Medicare
	s.HealthcarePersons[0].MedicareEligibleAge = eligibleAge

	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})

	// Primary starts at 65, so eligibility lands in projection year 2.
	for y, ys := range proj.YearlySummaries {
		t.Logf("year %d: IRMAA=%.2f", y, ys.IRMAA)
	}
	for y := 0; y < eligibleAge-65; y++ {
		if proj.YearlySummaries[y].IRMAA > 0 {
			t.Errorf("year %d: charged %.2f of IRMAA before the plan's Medicare eligible age of %d",
				y, proj.YearlySummaries[y].IRMAA, eligibleAge)
		}
	}
	if proj.YearlySummaries[eligibleAge-65].IRMAA <= 0 {
		t.Errorf("year %d: the person reaches the plan's eligible age of %d, so IRMAA should start; got %.2f",
			eligibleAge-65, eligibleAge, proj.YearlySummaries[eligibleAge-65].IRMAA)
	}
}
