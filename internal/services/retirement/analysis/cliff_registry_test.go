package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// medicareAgeScenario draws down a large tax-deferred balance while both
// spouses are on Medicare, so RMDs push MAGI toward the IRMAA tiers.
func medicareAgeScenario() *models.WhatIfSettings {
	s := taxableScenario()
	s.PortfolioValue = 4_000_000
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint, Age65Count: 1}
	return s
}

// preMedicareScenario retires well before 65, so no IRMAA cliff applies.
func preMedicareScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 55
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 50
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 5000
	s.ProjectionYears = 5 // ends before anyone reaches 65
	return s
}

// TestCliff_PopulatedThroughTheProjection is the plumbing check: a
// Medicare-age household must carry per-year headroom to the next step-cost
// threshold, and the figures must be internally consistent.
func TestCliff_PopulatedThroughTheProjection(t *testing.T) {
	proj, _ := runProj(t, medicareAgeScenario())

	populated := 0
	for _, ys := range proj.YearlySummaries {
		if ys.NextCliffLabel == "" {
			continue
		}
		populated++

		if ys.NextCliffAnnualCost <= 0 {
			t.Errorf("year %d: %q costs %.2f to cross; a cliff must have a step cost",
				ys.Year, ys.NextCliffLabel, ys.NextCliffAnnualCost)
		}
		if ys.NextCliffHeadroom < 0 {
			t.Errorf("year %d: headroom %.2f is negative; NextCliff reports only "+
				"thresholds not yet crossed", ys.Year, ys.NextCliffHeadroom)
		}
	}

	if populated == 0 {
		t.Fatal("no year carried a cliff for a household on Medicare with a large " +
			"tax-deferred balance; the engine plumbing is broken")
	}
}

// TestCliff_NoneBeforeMedicare — IRMAA is the only step-cost threshold this
// engine models, so a household that never reaches 65 inside the horizon has
// no cliff to report. (The ACA 400%-FPL cliff, which WOULD apply to this
// household, is not modelled at all.)
func TestCliff_NoneBeforeMedicare(t *testing.T) {
	proj, in := runProj(t, preMedicareScenario())

	for _, ys := range proj.YearlySummaries {
		if ys.NextCliffLabel != "" {
			t.Errorf("year %d reported cliff %q for a pre-Medicare household",
				ys.Year, ys.NextCliffLabel)
		}
	}
	if tax := BuildTax(proj, in); tax != nil && tax.NearestCliff != nil {
		t.Errorf("NearestCliff = %+v; want nil before anyone is on Medicare", tax.NearestCliff)
	}
}

// TestCliff_NearestIsTheTightestYear — the surfaced figure must be the year
// with the LEAST headroom, since that is where a change in timing is worth
// the most.
func TestCliff_NearestIsTheTightestYear(t *testing.T) {
	proj, in := runProj(t, medicareAgeScenario())
	tax := BuildTax(proj, in)

	if tax == nil || tax.NearestCliff == nil {
		t.Fatal("expected a nearest cliff for a Medicare-age household with large RMDs")
	}

	wantHeadroom := math.Inf(1)
	for _, ys := range proj.YearlySummaries {
		if ys.NextCliffLabel != "" && ys.NextCliffHeadroom < wantHeadroom {
			wantHeadroom = ys.NextCliffHeadroom
		}
	}
	if math.Abs(tax.NearestCliff.Headroom-wantHeadroom) > 0.01 {
		t.Errorf("NearestCliff.Headroom = %.2f, want the minimum across years %.2f",
			tax.NearestCliff.Headroom, wantHeadroom)
	}

	// It must name a real year of the projection, not a relative index.
	if tax.NearestCliff.Year < 2000 {
		t.Errorf("NearestCliff.Year = %d; want a calendar year", tax.NearestCliff.Year)
	}
	if tax.NearestCliff.AnnualCost <= 0 {
		t.Errorf("NearestCliff.AnnualCost = %.2f; crossing must cost something",
			tax.NearestCliff.AnnualCost)
	}

	t.Logf("nearest cliff: %d (age %d), %.0f under %s, costing %.0f",
		tax.NearestCliff.Year, tax.NearestCliff.Age, tax.NearestCliff.Headroom,
		tax.NearestCliff.Label, tax.NearestCliff.AnnualCost)
}
