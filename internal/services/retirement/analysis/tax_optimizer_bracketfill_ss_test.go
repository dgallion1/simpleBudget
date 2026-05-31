package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// Bracket-fill conversion sizing must account for the conversion's own effect
// on Social Security taxability. The engine folds a Roth conversion into other
// income BEFORE the §86 taxable-SS calc, so a conversion pushes more SS into
// the taxable range. Sizing the conversion naively as (ceiling − other) — with
// `other`'s taxable SS computed without the conversion — overshoots the target
// bracket. The sizing must solve for the conversion that lands taxable ordinary
// income ON the ceiling.
//
// Scenario from the bug report: single filer, age 67, $36k SS, no other income.
// estimateOtherTaxableIncome ≈ 0 (modest SS alone is non-taxable), so a naive
// 12% fill converts ~the full ceiling; the engine then makes up to ~85% of the
// SS taxable, pushing ordinary taxable income well above the 12% ceiling.
func TestBracketFill_AccountsForConversionDrivenSSTaxation(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1] // single filer
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 67)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	s.ProjectionYears = 10
	// $36k/yr SS, claimed at 67 = current age; COLA pinned to 0 so gross SS is
	// a flat $36k across the window.
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 67, COLARate: 0, COLARateSet: true,
	}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.12,
		StartAge:      67,
		EndAge:        69, // years 0,1 — pre-RMD, SS already claimed
	}
	got := strategyYearlyConversions(ps, strat)
	if len(got) == 0 {
		t.Fatal("expected bracket-fill conversions")
	}

	const grossSS = 36_000.0
	tc := engine.NewTaxCalculator(ps.TaxConfig, ps.InflationRate)
	tc.Age65Count = 1
	for i, yc := range got {
		projYear := i
		ceiling, ok := inflatedBracketTopForYear(ps, 0.12, projYear)
		if !ok {
			t.Fatalf("year %d: no inflated 12%% ceiling", projYear)
		}
		stdDed := tc.GetAdjustedStandardDeduction(engine.YearsFromTaxBase(ps, projYear))
		// Engine-equivalent taxable ordinary income at the chosen conversion:
		// the conversion is part of the §86 provisional income.
		resulting := yc.Amount + tc.CalculateTaxableSocialSecurity(grossSS, yc.Amount, 0, 0) - stdDed

		if math.Abs(resulting-ceiling) > 1.0 {
			t.Errorf("year %d: conversion %.0f drives taxable ordinary income to %.0f, want it to land on the 12%% ceiling %.0f "+
				"(sizing ignores SS made taxable by the conversion itself)", projYear, yc.Amount, resulting, ceiling)
		}
		if yc.Amount >= ceiling {
			t.Errorf("year %d: conversion %.0f should be below the ceiling %.0f (SS taxability consumes bracket room)",
				projYear, yc.Amount, ceiling)
		}
	}
}
