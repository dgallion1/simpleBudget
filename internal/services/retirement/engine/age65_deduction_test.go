package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// age65Scenario is a married household, both filers over 65, drawing living
// expenses from a tax-deferred balance so every year produces taxable income.
// age65Count is what the plan happens to carry in TaxConfig.
func age65Scenario(age65Count int) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: models.BirthMonthForAge("2026-01", 68), Role: models.PersonRolePrimary},
		{ID: "spouse", Name: "Spouse", BirthMonth: models.BirthMonthForAge("2026-01", 67), Role: models.PersonRoleSpouse},
	}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 6_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.ProjectionYears = 2
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.Guardrails = nil
	s.SocialSecurity = nil
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0

	tc := models.DefaultTaxConfig()
	tc.FilingStatus = models.FilingMarriedJoint
	tc.Age65Count = age65Count
	s.TaxConfig = tc
	return s
}

// TestAge65DeductionIsDerivedNotRead is the falsification test for audit
// finding F-3. GetAdjustedStandardDeduction correctly adds the age-65
// additional standard deduction, but it reads TaxCalculator.Age65Count, which
// the engine copies verbatim from the static TaxConfig.Age65Count JSON field.
// Nothing in the UI or the handlers ever writes that field, and both shipped
// scenarios carry 0 — so for every saved plan the engine silently drops the
// deduction, while the tax optimizer derives the count per year and applies it.
//
// The count is a function of the filers' ages and filing status, all of which
// the engine already knows. A plan that leaves the field at its shipped 0 must
// therefore be taxed identically to one that spells out the correct 2.
func TestAge65DeductionIsDerivedNotRead(t *testing.T) {
	asShipped := New().Run(Input{Prepared: prepare.MustFrom(t, age65Scenario(0))})
	spelledOut := New().Run(Input{Prepared: prepare.MustFrom(t, age65Scenario(2))})

	if len(asShipped.YearlySummaries) != 2 || len(spelledOut.YearlySummaries) != 2 {
		t.Fatalf("yearly summaries: shipped=%d spelled-out=%d, want 2 each",
			len(asShipped.YearlySummaries), len(spelledOut.YearlySummaries))
	}

	for y := range asShipped.YearlySummaries {
		got := asShipped.YearlySummaries[y].Taxes
		want := spelledOut.YearlySummaries[y].Taxes
		if want <= 0 {
			t.Fatalf("year %d produced no taxes; the scenario cannot exercise F-3", y)
		}
		if math.Abs(got-want) > 1 {
			t.Errorf("year %d: plan with age_65_count=0 paid %.2f, plan with age_65_count=2 paid %.2f (overcharged %.2f).\n"+
				"  Both filers are over 65; the count is derivable from the ages the engine already has.",
				y, got, want, got-want)
		}
	}
}
